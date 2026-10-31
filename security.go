package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const securityHelpText = `Run the project-configured secret scanner.

Usage:
  leadfox security scan [options]

Options:
  -w, --worktree DIR   Project worktree (default: current directory)
  -f, --format FORMAT  Output markdown (default) or json
  -h, --help           Show this help message

The scanner, version, scan mode and network policy come from
control/security.yaml. Scanner reports are redacted before they are retained.
`

type securityOptions struct {
	worktree string
	format   string
	showHelp bool
}

type securityConfig struct {
	SecretScanning secretScanConfig `yaml:"secret_scanning"`
}

type secretScanConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Tool         string `yaml:"tool"`
	Binary       string `yaml:"binary,omitempty"`
	Version      string `yaml:"version"`
	Mode         string `yaml:"mode"`
	ReportPath   string `yaml:"report_path"`
	AllowNetwork bool   `yaml:"allow_network"`
}

type securityScanReport struct {
	SchemaVersion int    `json:"schema_version" yaml:"schema_version"`
	Leadfox       string `json:"leadfox" yaml:"leadfox"`
	Tool          string `json:"tool" yaml:"tool"`
	Version       string `json:"version" yaml:"version"`
	Mode          string `json:"mode" yaml:"mode"`
	AllowNetwork  bool   `json:"allow_network" yaml:"allow_network"`
	ReportPath    string `json:"report_path,omitempty" yaml:"report_path,omitempty"`
	StartedAt     string `json:"started_at" yaml:"started_at"`
	FinishedAt    string `json:"finished_at" yaml:"finished_at"`
	ExitCode      int    `json:"exit_code" yaml:"exit_code"`
	Status        string `json:"status" yaml:"status"`
	Reason        string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

func runSecurityCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(securityHelpText)
		return nil
	}
	if args[0] != "scan" {
		return fmt.Errorf("unknown security command %q; use 'leadfox security --help' for usage", args[0])
	}
	options, err := parseSecurityOptions(args[1:])
	if err != nil {
		return err
	}
	if options.showHelp {
		fmt.Print(securityHelpText)
		return nil
	}
	root, err := filepath.Abs(options.worktree)
	if err != nil {
		return fmt.Errorf("resolve worktree: %w", err)
	}
	report, scanErr := executeSecurityScan(root)
	if options.format == "json" {
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("encode security report: %w", marshalErr)
		}
		fmt.Println(string(encoded))
	} else {
		fmt.Print(renderSecurityMarkdown(report))
	}
	return scanErr
}

func parseSecurityOptions(args []string) (securityOptions, error) {
	options := securityOptions{worktree: ".", format: "markdown"}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-h", "--help":
			options.showHelp = true
		case "-w", "--worktree":
			value, next, err := requiredOptionValue(args, index, args[index])
			if err != nil {
				return options, err
			}
			options.worktree, index = value, next
		case "-f", "--format":
			value, next, err := requiredOptionValue(args, index, args[index])
			if err != nil {
				return options, err
			}
			options.format, index = strings.ToLower(value), next
		default:
			return options, fmt.Errorf("unknown security option %q", args[index])
		}
	}
	if options.format != "markdown" && options.format != "json" {
		return options, fmt.Errorf("unsupported output format %q", options.format)
	}
	return options, nil
}

func executeSecurityScan(root string) (securityScanReport, error) {
	configPath := filepath.Join(root, "control", "security.yaml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return securityScanReport{}, errors.New("no control/security.yaml found")
		}
		return securityScanReport{}, fmt.Errorf("read security configuration: %w", err)
	}
	var document securityConfig
	if err := yaml.Unmarshal(content, &document); err != nil {
		return securityScanReport{}, fmt.Errorf("parse security configuration: %w", err)
	}
	config := document.SecretScanning
	if !config.Enabled {
		return securityScanReport{SchemaVersion: 1, Leadfox: leadfoxVersion(), Tool: config.Tool, Version: config.Version, Mode: config.Mode, AllowNetwork: config.AllowNetwork, Status: "disabled"}, nil
	}
	if config.Tool != "gitleaks" && config.Tool != "trufflehog" {
		return securityScanReport{}, fmt.Errorf("unsupported secret scanner %q; use gitleaks or trufflehog", config.Tool)
	}
	if config.Version == "" {
		return securityScanReport{}, errors.New("secret scanner version must be pinned in control/security.yaml")
	}
	if config.Mode == "" {
		config.Mode = "history"
	}
	if config.Mode != "history" && config.Mode != "working-tree" {
		return securityScanReport{}, fmt.Errorf("unsupported secret scan mode %q", config.Mode)
	}
	if config.ReportPath == "" {
		config.ReportPath = "control/evidence/secret-scan.json"
	}
	reportAbs, err := safeProjectPath(root, config.ReportPath)
	if err != nil {
		return securityScanReport{}, err
	}
	if err := os.MkdirAll(filepath.Dir(reportAbs), 0o755); err != nil {
		return securityScanReport{}, fmt.Errorf("create secret scan report directory: %w", err)
	}
	binary := config.Binary
	if binary == "" {
		binary = config.Tool
	}
	started := time.Now().UTC()
	if err := verifyScannerVersion(binary, config.Version); err != nil {
		return securityScanReport{SchemaVersion: 1, Leadfox: leadfoxVersion(), Tool: config.Tool, Version: config.Version, Mode: config.Mode, AllowNetwork: config.AllowNetwork, StartedAt: started.Format(time.RFC3339), FinishedAt: time.Now().UTC().Format(time.RFC3339), Status: "failed", Reason: err.Error()}, err
	}
	var command *exec.Cmd
	switch config.Tool {
	case "gitleaks":
		args := []string{"detect", "--source", root, "--redact", "--report-format", "json", "--report-path", reportAbs, "--exit-code", "1"}
		if config.Mode == "working-tree" {
			args = append(args, "--no-git")
		}
		command = exec.Command(binary, args...)
	case "trufflehog":
		if config.AllowNetwork {
			return securityScanReport{}, errors.New("trufflehog network verification must be explicitly run as external evidence, not by the local security gate")
		}
		source := "file://" + root
		args := []string{"git", source, "--json", "--no-verification", "--results=verified,unverified,unknown"}
		if config.Mode == "working-tree" {
			args = []string{"filesystem", root, "--json", "--no-verification", "--results=verified,unverified,unknown"}
		}
		command = exec.Command(binary, args...)
	}
	command.Dir = root
	output, commandErr := command.CombinedOutput()
	if config.Tool == "trufflehog" {
		if err := writeRedactedJSONReport(reportAbs, output); err != nil {
			return securityScanReport{}, err
		}
	} else if _, statErr := os.Stat(reportAbs); os.IsNotExist(statErr) {
		if err := writeRedactedJSONReport(reportAbs, output); err != nil {
			return securityScanReport{}, err
		}
	} else if err := redactJSONReport(reportAbs); err != nil {
		return securityScanReport{}, err
	}
	result := securityScanReport{SchemaVersion: 1, Leadfox: leadfoxVersion(), Tool: config.Tool, Version: config.Version, Mode: config.Mode, AllowNetwork: config.AllowNetwork, ReportPath: filepath.ToSlash(config.ReportPath), StartedAt: started.Format(time.RFC3339), FinishedAt: time.Now().UTC().Format(time.RFC3339), Status: "passed"}
	if commandErr != nil {
		result.Status = "failed"
		result.ExitCode = commandExitCode(commandErr)
		result.Reason = "secret scanner reported findings or failed to execute"
		return result, errors.New(result.Reason)
	}
	return result, nil
}

func verifyScannerVersion(binary, expected string) error {
	output, err := exec.Command(binary, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("run %s version: %w", binary, err)
	}
	if !strings.Contains(string(output), expected) {
		return fmt.Errorf("%s version does not contain pinned version %q", binary, expected)
	}
	return nil
}

func safeProjectPath(root, value string) (string, error) {
	path, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(value)))
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes the project worktree", value)
	}
	return path, nil
}

func writeRedactedJSONReport(path string, output []byte) error {
	if len(strings.TrimSpace(string(output))) == 0 {
		output = []byte("[]\n")
	}
	if err := os.WriteFile(path, output, 0o644); err != nil {
		return fmt.Errorf("write secret scan report: %w", err)
	}
	return redactJSONReport(path)
}

func redactJSONReport(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read secret scan report: %w", err)
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		encodedLines := make([]string, 0, len(lines))
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var lineValue any
			if lineErr := json.Unmarshal([]byte(line), &lineValue); lineErr != nil {
				return fmt.Errorf("secret scan report is not valid JSON: %w", err)
			}
			redactJSONValue(lineValue)
			lineBytes, marshalErr := json.Marshal(lineValue)
			if marshalErr != nil {
				return fmt.Errorf("encode secret scan report line: %w", marshalErr)
			}
			encodedLines = append(encodedLines, string(lineBytes))
		}
		return os.WriteFile(path, []byte(strings.Join(encodedLines, "\n")+"\n"), 0o644)
	}
	redactJSONValue(value)
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode secret scan report: %w", err)
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func redactJSONValue(value any) {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			lower := strings.ToLower(key)
			if lower == "secret" || lower == "raw" || lower == "match" || lower == "rawv2" {
				item[key] = "[REDACTED]"
				continue
			}
			redactJSONValue(child)
		}
	case []any:
		for _, child := range item {
			redactJSONValue(child)
		}
	}
}

func commandExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func renderSecurityMarkdown(report securityScanReport) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Secret Scan\n\n- Status: **%s**\n- Tool: `%s`\n- Pinned version: `%s`\n- Mode: `%s`\n- Network allowed: `%t`\n", strings.ToUpper(report.Status), report.Tool, report.Version, report.Mode, report.AllowNetwork)
	if report.ReportPath != "" {
		fmt.Fprintf(&builder, "- Report: `%s`\n", report.ReportPath)
	}
	if report.Reason != "" {
		fmt.Fprintf(&builder, "- Reason: %s\n", report.Reason)
	}
	builder.WriteString("\nThis scan reports possible secret exposure. Findings require remediation and rotation decisions; a clean scan is not proof that no secret has ever existed.\n")
	return builder.String()
}
