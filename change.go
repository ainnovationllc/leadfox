package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var errChangeInvalid = errors.New("change plan is invalid")

type changeFinding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type changeReport struct {
	Path     string          `json:"path"`
	Valid    bool            `json:"valid"`
	Findings []changeFinding `json:"findings,omitempty"`
}

func runChangeCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(changeHelpText)
		return nil
	}
	if args[0] != "validate" {
		return fmt.Errorf("unknown change operation %q; use 'leadfox change --help' for usage", args[0])
	}
	path, format, err := parseChangeOptions(args[1:])
	if err != nil {
		return err
	}
	report, err := validateChangePlan(path)
	if err != nil {
		return err
	}
	if format == "json" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
	} else {
		printChangeReport(report)
	}
	if !report.Valid {
		return errChangeInvalid
	}
	return nil
}

func parseChangeOptions(args []string) (string, string, error) {
	path := ""
	format := "markdown"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return "", "", errors.New("--format requires markdown or json")
			}
			i++
			format = args[i]
			if format != "markdown" && format != "json" {
				return "", "", fmt.Errorf("unsupported change report format %q", format)
			}
		case "--help", "-h":
			fmt.Print(changeHelpText)
			return "", "", errors.New("help requested")
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", fmt.Errorf("unknown change option %q", args[i])
			}
			if path != "" {
				return "", "", errors.New("only one change plan path may be supplied")
			}
			path = args[i]
		}
	}
	if path == "" {
		return "", "", errors.New("a change plan path is required; use 'leadfox change validate PATH'")
	}
	return path, format, nil
}

func validateChangePlan(path string) (changeReport, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return changeReport{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return changeReport{}, err
	}
	report := changeReport{Path: abs}
	add := func(code, message string) {
		report.Findings = append(report.Findings, changeFinding{Code: code, Message: message})
	}
	text := string(data)
	meta, body, ok := parseFrontMatter(text)
	if !ok {
		add("frontmatter", "change plan must begin and end its metadata block with ---")
	} else {
		for _, key := range []string{"change_id", "version", "status", "project", "change_type", "generated_by", "approval", "base_project_model", "base_architecture_plan", "base_plan_version"} {
			if strings.TrimSpace(meta[key]) == "" {
				add("missing-metadata", fmt.Sprintf("metadata field %q is required", key))
			}
		}
		if v, err := strconv.Atoi(strings.TrimSpace(meta["version"])); err != nil || v < 1 {
			add("invalid-version", "version must be a positive integer")
		}
		if v, err := strconv.Atoi(strings.TrimSpace(meta["base_plan_version"])); err != nil || v < 1 {
			add("invalid-baseline-version", "base_plan_version must be a positive integer")
		}
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(strings.TrimSpace(meta["change_id"])) {
			add("invalid-change-id", "change_id must be a stable identifier")
		}
		if !map[string]bool{"draft": true, "ready-for-review": true, "approved": true, "implemented": true, "rejected": true}[strings.ToLower(strings.TrimSpace(meta["status"]))] {
			add("invalid-status", "status must be draft, ready-for-review, approved, implemented, or rejected")
		}
		if strings.EqualFold(strings.TrimSpace(meta["approval"]), "approved") && strings.EqualFold(strings.TrimSpace(meta["status"]), "draft") {
			add("approval-status", "an approved change cannot remain in draft status")
		}
		root := projectRootForChange(abs)
		for key := range map[string]bool{"base_project_model": true, "base_architecture_plan": true} {
			ref := strings.TrimSpace(meta[key])
			if ref == "" || filepath.IsAbs(ref) || strings.Contains(ref, "..") {
				add("invalid-baseline-path", fmt.Sprintf("%s must be a safe project-relative path", key))
				continue
			}
			if _, err := os.Stat(filepath.Join(root, ref)); err != nil {
				add("missing-baseline", fmt.Sprintf("referenced %s does not exist: %s", key, ref))
			}
		}
	}
	for _, heading := range []string{"Change summary", "Affected baseline", "Proposed delta", "Compatibility and impact", "Proposed validation and jobs", "Open decisions", "Change history"} {
		if sectionText(body, heading) == "" {
			add("missing-section", fmt.Sprintf("required section %q is missing", heading))
		}
	}
	for _, label := range []string{
		"What is changing:", "Why it is changing:", "Requested by:",
		"Existing services affected:", "Existing integrations affected:", "Existing data stores affected:", "Existing environments affected:", "Existing requirements affected:", "Existing decisions or jobs affected:",
		"Existing behavior preserved:", "API or contract compatibility:", "Data migration required:", "Backfill or rollback strategy:", "Secret and credential impact:", "Security and privacy impact:", "Cost, performance, and availability impact:", "Deployment order and dependencies:",
		"Required tests and gates:", "Real-tool or external validation needed:", "Proposed jobs:", "Stop conditions:",
	} {
		if !completedLabel(body, label) {
			add("missing-field", fmt.Sprintf("required change-plan field %q is incomplete", strings.TrimSuffix(label, ":")))
		}
	}
	for _, heading := range []string{"Added", "Changed", "Removed or superseded"} {
		if !strings.Contains(body, "### "+heading) {
			add("missing-delta", fmt.Sprintf("proposed delta must include %q", heading))
		}
	}
	if !hasChangeHistory(body) {
		add("missing-history", "change history requires an initial completed row")
	}
	if secretLikeIntakePattern.MatchString(text) {
		add("secret-like-content", "change plan contains a secret-like value; record references, never secret values")
	}
	report.Valid = len(report.Findings) == 0
	return report, nil
}

func parseFrontMatter(text string) (map[string]string, string, bool) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, text, false
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, text, false
	}
	meta := map[string]string{}
	for _, line := range lines[1:end] {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			meta[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return meta, strings.Join(lines[end+1:], "\n"), true
}

func projectRootForChange(path string) string {
	dir := filepath.Dir(path)
	for {
		if info, err := os.Stat(filepath.Join(dir, "control")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(filepath.Dir(path))
		}
		dir = parent
	}
}

func completedLabel(text, label string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "-") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		}
		if !strings.HasPrefix(trimmed, label) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, label))
		return value != "" && !strings.Contains(value, "<!--")
	}
	return false
}

func hasChangeHistory(text string) bool {
	section := sectionText(text, "Change history")
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "| 1 ") || strings.HasPrefix(trimmed, "|1 ") {
			return !strings.Contains(trimmed, "<!--")
		}
	}
	return false
}

func printChangeReport(report changeReport) {
	if report.Valid {
		fmt.Printf("valid: %s\n", report.Path)
		return
	}
	fmt.Printf("invalid: %s\n", report.Path)
	for _, finding := range report.Findings {
		fmt.Printf("- [%s] %s\n", finding.Code, finding.Message)
	}
}

const changeHelpText = `Validate a proposed change to an existing project.

Usage:
  leadfox change validate [PATH] [options]

PATH is the Markdown change plan to validate. Exit 0 means valid, 1 means
findings, and 2 means the input could not be processed.

Options:
  --format markdown|json  Select diagnostics format (default markdown)
  -h, --help              Show this help message
`
