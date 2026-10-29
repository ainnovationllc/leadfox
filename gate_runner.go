package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type gateOptions struct {
	gateName string
	checkID  string
	worktree string
	format   string
	timeout  time.Duration
	showHelp bool
}

type gateFile struct {
	Gates []gate `yaml:"gates"`
}

type gate struct {
	ID          string  `yaml:"id"`
	Stage       string  `yaml:"stage"`
	Description string  `yaml:"description"`
	Manual      bool    `yaml:"manual"`
	Checks      []check `yaml:"checks"`
}

type check struct {
	ID           string `yaml:"id"`
	Description  string `yaml:"description"`
	Run          string `yaml:"run"`
	RequiresPath string `yaml:"requires_path"`
	Manual       bool   `yaml:"manual"`
	Deferred     bool   `yaml:"deferred"`
}

type checkResult struct {
	ID             string   `json:"id" yaml:"id"`
	Status         string   `json:"status" yaml:"status"`
	ExitCode       int      `json:"exit_code,omitempty" yaml:"exit_code,omitempty"`
	DurationMS     int64    `json:"duration_ms,omitempty" yaml:"duration_ms,omitempty"`
	Output         string   `json:"output,omitempty" yaml:"output,omitempty"`
	OutputText     string   `json:"output_text,omitempty" yaml:"output_text,omitempty"`
	OutputLines    []string `json:"output_lines,omitempty" yaml:"output_lines,omitempty"`
	OutputEncoding string   `json:"output_encoding,omitempty" yaml:"output_encoding,omitempty"`
	Reason         string   `json:"reason,omitempty" yaml:"reason,omitempty"`
	Attestation    string   `json:"attestation,omitempty" yaml:"attestation,omitempty"`
}

type gateResult struct {
	Gate       string        `json:"gate" yaml:"gate"`
	Worktree   string        `json:"worktree" yaml:"worktree"`
	StartedAt  time.Time     `json:"started_at" yaml:"started_at"`
	FinishedAt time.Time     `json:"finished_at" yaml:"finished_at"`
	Status     string        `json:"status" yaml:"status"`
	Checks     []checkResult `json:"checks" yaml:"checks"`
}

func runGateCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(gateHelpText)
		return nil
	}
	if args[0] != "run" {
		return fmt.Errorf("unknown gate command %q; use 'leadfox gate --help' for usage", args[0])
	}
	options, err := parseGateOptions(args[1:])
	if err != nil {
		return err
	}
	if options.showHelp {
		fmt.Print(gateHelpText)
		return nil
	}
	if options.gateName == "" {
		return errors.New("a gate name is required; use 'leadfox gate --help' for usage")
	}
	root, err := filepath.Abs(options.worktree)
	if err != nil {
		return fmt.Errorf("resolve worktree: %w", err)
	}
	result, err := executeGate(root, options)
	if err != nil {
		return err
	}
	if options.format == "json" {
		encoded, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("encode gate result: %w", marshalErr)
		}
		fmt.Println(string(encoded))
	} else {
		fmt.Print(renderGateMarkdown(result))
	}
	if result.Status == "failed" {
		return errors.New("gate failed")
	}
	return nil
}

func parseGateOptions(args []string) (gateOptions, error) {
	options := gateOptions{worktree: ".", format: "markdown", timeout: 10 * time.Minute}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "-h", "--help":
			options.showHelp = true
		case "-c", "--check":
			value, next, err := requiredOptionValue(args, index, arg)
			if err != nil {
				return options, err
			}
			options.checkID = value
			index = next
		case "-w", "--worktree":
			value, next, err := requiredOptionValue(args, index, arg)
			if err != nil {
				return options, err
			}
			options.worktree = value
			index = next
		case "-f", "--format":
			value, next, err := requiredOptionValue(args, index, arg)
			if err != nil {
				return options, err
			}
			options.format = strings.ToLower(value)
			index = next
		case "-t", "--timeout":
			value, next, err := requiredOptionValue(args, index, arg)
			if err != nil {
				return options, err
			}
			duration, parseErr := time.ParseDuration(value)
			if parseErr != nil || duration <= 0 {
				return options, fmt.Errorf("invalid timeout %q", value)
			}
			options.timeout = duration
			index = next
		default:
			if strings.HasPrefix(arg, "-") {
				return options, fmt.Errorf("unknown gate option %q", arg)
			}
			if options.gateName != "" {
				return options, errors.New("only one gate name may be provided")
			}
			options.gateName = arg
		}
	}
	if options.format != "markdown" && options.format != "json" {
		return options, fmt.Errorf("unsupported output format %q", options.format)
	}
	return options, nil
}

func requiredOptionValue(args []string, index int, option string) (string, int, error) {
	if index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
		return "", index, fmt.Errorf("%s requires a value", option)
	}
	return args[index+1], index + 1, nil
}

func executeGate(root string, options gateOptions) (gateResult, error) {
	if _, err := os.Stat(filepath.Join(root, "control", "gates.yaml")); err != nil {
		if os.IsNotExist(err) {
			return gateResult{}, errors.New("no control/gates.yaml found in the selected worktree")
		}
		return gateResult{}, fmt.Errorf("inspect gate file: %w", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "control", "gates.yaml"))
	if err != nil {
		return gateResult{}, fmt.Errorf("read gates.yaml: %w", err)
	}
	var document gateFile
	if err := yaml.Unmarshal(content, &document); err != nil {
		return gateResult{}, fmt.Errorf("parse gates.yaml: %w", err)
	}
	selected, found := findGate(document.Gates, options.gateName)
	if !found {
		return gateResult{}, fmt.Errorf("gate %q was not found", options.gateName)
	}
	if selected.Manual {
		return gateResult{
			Gate:       selected.ID,
			Worktree:   root,
			StartedAt:  time.Now().UTC(),
			FinishedAt: time.Now().UTC(),
			Status:     "manual",
		}, nil
	}

	result := gateResult{Gate: selected.ID, Worktree: root, StartedAt: time.Now().UTC()}
	for _, item := range selected.Checks {
		if options.checkID != "" && item.ID != options.checkID {
			continue
		}
		result.Checks = append(result.Checks, executeCheck(root, item, options.timeout))
	}
	if len(result.Checks) == 0 {
		if options.checkID != "" {
			return gateResult{}, fmt.Errorf("check %q was not found in gate %q", options.checkID, selected.ID)
		}
		return gateResult{}, fmt.Errorf("gate %q has no executable checks", selected.ID)
	}
	result.Status = "passed"
	for _, item := range result.Checks {
		if item.Status == "failed" {
			result.Status = "failed"
			break
		}
		if item.Status == "deferred" {
			result.Status = "deferred"
		}
		if item.Status == "manual" && result.Status == "passed" {
			result.Status = "manual"
		}
	}
	result.FinishedAt = time.Now().UTC()
	return result, nil
}

func findGate(gates []gate, id string) (gate, bool) {
	for _, item := range gates {
		if item.ID == id {
			return item, true
		}
	}
	return gate{}, false
}

func executeCheck(root string, item check, timeout time.Duration) checkResult {
	started := time.Now()
	result := checkResult{ID: item.ID}
	if item.Deferred {
		result.Status = "deferred"
		result.Reason = "deferred check"
		return result
	}
	if item.Manual || strings.TrimSpace(item.Run) == "" {
		result.Status = "manual"
		result.Reason = "manual check"
		return result
	}
	if item.RequiresPath != "" {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(item.RequiresPath))); err != nil {
			if os.IsNotExist(err) {
				result.Status = "skipped"
				result.Reason = "required path does not exist"
				return result
			}
			result.Status = "failed"
			result.Reason = fmt.Sprintf("inspect required path: %v", err)
			result.ExitCode = 1
			return result
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "bash", "-c", item.Run)
	command.Dir = root
	output, err := command.CombinedOutput()
	result.Output = redactGateOutput(string(output))
	result.DurationMS = time.Since(started).Milliseconds()
	if ctx.Err() == context.DeadlineExceeded {
		result.Status = "failed"
		result.Reason = fmt.Sprintf("timed out after %s", timeout)
		result.ExitCode = 124
		return result
	}
	if err == nil {
		result.Status = "passed"
		return result
	}
	result.Status = "failed"
	result.ExitCode = 1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	}
	result.Reason = err.Error()
	return result
}

var bearerPattern = regexp.MustCompile(`(?i)(bearer\s+)[^\s]+`)

func redactGateOutput(output string) string {
	output = bearerPattern.ReplaceAllString(output, `${1}[REDACTED]`)
	lines := strings.Split(output, "\n")
	for index, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "password=") || strings.Contains(lower, "token=") || strings.Contains(lower, "secret=") {
			if equals := strings.Index(line, "="); equals >= 0 {
				lines[index] = line[:equals+1] + "[REDACTED]"
			}
		}
	}
	return strings.Join(lines, "\n")
}

func renderGateMarkdown(result gateResult) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Gate: %s\n\n", result.Gate)
	fmt.Fprintf(&builder, "- Status: **%s**\n", strings.ToUpper(result.Status))
	fmt.Fprintf(&builder, "- Worktree: `%s`\n", result.Worktree)
	fmt.Fprintf(&builder, "- Started: %s\n", result.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "- Finished: %s\n\n", result.FinishedAt.Format(time.RFC3339))
	builder.WriteString("## Checks\n\n")
	builder.WriteString("| Check | Status | Exit | Duration | Reason |\n|---|---|---:|---:|---|\n")
	for _, item := range result.Checks {
		fmt.Fprintf(&builder, "| `%s` | %s | %d | %d ms | %s |\n", item.ID, item.Status, item.ExitCode, item.DurationMS, item.Reason)
	}
	for _, item := range result.Checks {
		if item.Output == "" {
			continue
		}
		fmt.Fprintf(&builder, "\n### %s output\n\n```text\n%s\n```\n", item.ID, strings.TrimSpace(item.Output))
	}
	return builder.String()
}

func gateIDs(gates []gate) []string {
	ids := make([]string, 0, len(gates))
	for _, item := range gates {
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	return ids
}
