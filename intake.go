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

var errIntakeInvalid = errors.New("intake validation failed")

type intakeOptions struct {
	path   string
	format string
	help   bool
}

type intakeFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type intakeReport struct {
	Valid    bool            `json:"valid"`
	Path     string          `json:"path"`
	Findings []intakeFinding `json:"findings"`
}

const intakeHelpText = `Validate a project intake.

Usage:
  leadfox intake validate [PATH] [options]

Options:
  -f, --format FORMAT  markdown (default) or json
  -h, --help           Show this help message

The default path is control/project-intake.md. Exit codes are 0 for a valid
intake, 1 for validation findings, and 2 for a processing error.
`

var secretLikeIntakePattern = regexp.MustCompile(`(?i)(-----BEGIN [A-Z0-9 ]+-----|(?:sk-[A-Za-z0-9_-]{12,}|ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9-]{15,}|ya29\.[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16})|(?:password|passwd|token|api[_-]?key|client[_-]?secret)\s*[:=]\s*["']?[A-Za-z0-9_./+=-]{12,})`)

func runIntakeCommand(args []string) error {
	options, err := parseIntakeOptions(args)
	if err != nil {
		return err
	}
	if options.help {
		fmt.Print(intakeHelpText)
		return nil
	}
	if len(args) == 0 || (len(args) > 0 && args[0] != "validate") {
		return errors.New("use 'leadfox intake validate'; use 'leadfox intake --help' for usage")
	}
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve current directory: %w", err)
	}
	intakePath := options.path
	if intakePath == "" {
		intakePath = filepath.Join(root, "control", "project-intake.md")
	}
	intakePath, err = filepath.Abs(intakePath)
	if err != nil {
		return fmt.Errorf("resolve intake path: %w", err)
	}
	report, err := validateIntakeFile(intakePath)
	if err != nil {
		return err
	}
	if options.format == "json" {
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("encode intake report: %w", marshalErr)
		}
		fmt.Println(string(encoded))
	} else {
		printIntakeMarkdown(report)
	}
	if !report.Valid {
		return errIntakeInvalid
	}
	return nil
}

func parseIntakeOptions(args []string) (intakeOptions, error) {
	options := intakeOptions{format: "markdown"}
	if len(args) == 0 {
		return options, nil
	}
	if args[0] == "--help" || args[0] == "-h" {
		options.help = true
		return options, nil
	}
	if args[0] != "validate" {
		return options, fmt.Errorf("unknown intake command %q", args[0])
	}
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--help", "-h":
			options.help = true
		case "--format", "-f":
			if index+1 >= len(args) {
				return options, fmt.Errorf("%s requires markdown or json", args[index])
			}
			index++
			options.format = args[index]
		case "":
		default:
			if strings.HasPrefix(args[index], "-") {
				return options, fmt.Errorf("unknown intake option %q", args[index])
			}
			if options.path != "" {
				return options, errors.New("only one intake path may be provided")
			}
			options.path = args[index]
		}
	}
	if options.format != "markdown" && options.format != "json" {
		return options, fmt.Errorf("unsupported intake format %q", options.format)
	}
	return options, nil
}

func validateIntakeFile(file string) (intakeReport, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return intakeReport{}, fmt.Errorf("read intake: %w; run 'leadfox init' or provide a path", err)
		}
		return intakeReport{}, fmt.Errorf("read intake: %w", err)
	}
	text := string(content)
	report := intakeReport{Valid: true, Path: file, Findings: []intakeFinding{}}
	addFinding := func(code, message string) {
		report.Valid = false
		report.Findings = append(report.Findings, intakeFinding{Code: code, Severity: "error", Message: message})
	}

	sections := requiredIntakeSections()
	for _, section := range sections {
		if !strings.Contains(text, "## "+section) {
			addFinding("missing-section", fmt.Sprintf("missing required section: %s", section))
		}
	}
	for _, field := range []string{
		"Document version:", "Current stage:", "Last updated:", "Change summary:",
		"Project name:", "Project owner:", "Intake author:",
		"Problem being solved:", "Desired outcome:",
		"How we will know the first version is useful:", "Primary users:",
		"What must be protected:", "Important abuse or failure concerns:",
		"Questions that must be answered before implementation:",
	} {
		if valueForField(text, field) == "" {
			addFinding("missing-field", fmt.Sprintf("missing required field: %s", strings.TrimSuffix(field, ":")))
		}
	}
	version := valueForField(text, "Document version:")
	if version != "" {
		if parsed, parseErr := strconv.Atoi(strings.Split(version, ".")[0]); parseErr != nil || parsed < 1 || !documentVersionPattern.MatchString(version) {
			addFinding("invalid-version", "Document version must be a positive integer or dotted numeric version")
		}
	}
	stage := strings.ToLower(valueForField(text, "Current stage:"))
	allowedStages := map[string]bool{"idea": true, "experiment": true, "local development": true, "staging": true, "production": true}
	if stage != "" && !allowedStages[stage] {
		addFinding("invalid-stage", "Current stage must be idea, experiment, local development, staging, or production")
	}
	flowSection := sectionText(text, "Users and primary flow")
	if marker := strings.Index(flowSection, "Main user flow:"); marker >= 0 {
		flowSection = flowSection[marker:]
	}
	for _, item := range []struct {
		label   string
		section string
	}{
		{label: "Main user flow", section: flowSection},
		{label: "First-version capabilities", section: sectionText(text, "First-version capabilities")},
	} {
		label, section := item.label, item.section
		for _, number := range []string{"1.", "2.", "3."} {
			if !hasCompletedNumberedItem(section, number) {
				addFinding("missing-item", fmt.Sprintf("%s requires completed item %s", label, number))
			}
		}
	}
	if !hasCompletedTableRow(sectionText(text, "Services and components"), false) {
		addFinding("missing-services", "Services and components requires at least one completed row")
	}
	if !hasCompletedTableRow(sectionText(text, "Integrations and external dependencies"), true) {
		addFinding("missing-integrations", "Integrations and external dependencies requires a completed row or an explicit none entry")
	}
	if !hasCompletedTableRow(sectionText(text, "Environments"), false) || !hasCompletedEnvironment(text, "local") {
		addFinding("missing-environment", "Environments requires a completed local environment row")
	}
	if !hasCompletedChangeHistory(text) {
		addFinding("missing-history", "Change history requires an initial completed row")
	}
	if secretLikeIntakePattern.MatchString(text) {
		addFinding("secret-like-content", "intake contains a secret-like value; record only secret names or references, never values")
	}
	return report, nil
}

func requiredIntakeSections() []string {
	return []string{"Identity", "Problem and outcome", "Users and primary flow", "First-version capabilities", "Services and components", "Integrations and external dependencies", "Data", "Environments", "Security and operational concerns", "First planning questions", "Intake notes", "Change history"}
}

func valueForField(text, field string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "* "))
		if !strings.HasPrefix(trimmed, field) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, field))
		if strings.HasPrefix(value, "<!--") || strings.Contains(value, "<!--") {
			return ""
		}
		if value == "" {
			for _, continuation := range lines[index+1:] {
				if strings.HasPrefix(strings.TrimSpace(continuation), "## ") {
					break
				}
				if strings.TrimSpace(continuation) == "" {
					continue
				}
				if len(continuation) == len(strings.TrimLeft(continuation, " \t")) {
					break
				}
				candidate := strings.TrimSpace(continuation)
				if !strings.Contains(candidate, "<!--") {
					return "list"
				}
			}
		}
		return value
	}
	return ""
}

func sectionText(text, heading string) string {
	marker := "## " + heading
	start := strings.Index(text, marker)
	if start < 0 {
		return ""
	}
	section := text[start+len(marker):]
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	return section
}

func hasCompletedNumberedItem(section, number string) bool {
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, number) {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, number))
			return value != "" && !strings.Contains(value, "<!--")
		}
	}
	return false
}

func hasCompletedTableRow(section string, allowNone bool) bool {
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || strings.Contains(trimmed, "---") || strings.Contains(trimmed, "<!--") {
			continue
		}
		if allowNone && strings.Contains(strings.ToLower(trimmed), "none") {
			return true
		}
		cells := strings.Split(trimmed, "|")
		if len(cells) >= 3 {
			filled := 0
			for _, cell := range cells[1 : len(cells)-1] {
				if strings.TrimSpace(cell) != "" {
					filled++
				}
			}
			if filled >= 3 {
				return true
			}
		}
	}
	return false
}

func hasCompletedEnvironment(text, name string) bool {
	for _, line := range strings.Split(sectionText(text, "Environments"), "\n") {
		cells := tableCells(line)
		if len(cells) >= 2 && strings.EqualFold(strings.TrimSpace(cells[0]), name) && !strings.Contains(line, "<!--") && !strings.Contains(line, "---") {
			filled := 0
			for _, cell := range cells[1:] {
				if strings.TrimSpace(cell) != "" {
					filled++
				}
			}
			if filled >= 1 {
				return true
			}
		}
	}
	return false
}

func hasCompletedChangeHistory(text string) bool {
	section := sectionText(text, "Change history")
	versionIndex := -1
	for _, line := range strings.Split(section, "\n") {
		cells := tableCells(line)
		if len(cells) == 0 || strings.Contains(line, "---") {
			continue
		}
		for index, cell := range cells {
			if strings.EqualFold(strings.TrimSpace(cell), "version") {
				versionIndex = index
				break
			}
		}
		if versionIndex >= 0 {
			break
		}
	}
	if versionIndex < 0 {
		return false
	}
	for _, line := range strings.Split(section, "\n") {
		cells := tableCells(line)
		if len(cells) <= versionIndex || strings.Contains(line, "---") || strings.Contains(line, "<!--") {
			continue
		}
		version := strings.TrimSpace(cells[versionIndex])
		if documentVersionPattern.MatchString(version) {
			filled := 0
			for _, cell := range cells {
				if strings.TrimSpace(cell) != "" {
					filled++
				}
			}
			if filled >= 2 {
				return true
			}
		}
	}
	return false
}

var documentVersionPattern = regexp.MustCompile(`^[1-9][0-9]*(?:\.[0-9]+)*$`)

func tableCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") || !strings.Contains(trimmed, "|") {
		return nil
	}
	parts := strings.Split(trimmed, "|")
	if len(parts) < 3 {
		return nil
	}
	return parts[1 : len(parts)-1]
}

func printIntakeMarkdown(report intakeReport) {
	if report.Valid {
		fmt.Printf("Intake: VALID\nPath: %s\nFindings: 0\n", report.Path)
		return
	}
	fmt.Printf("Intake: INVALID\nPath: %s\nFindings: %d\n", report.Path, len(report.Findings))
	for _, finding := range report.Findings {
		fmt.Printf("- [%s] %s\n", finding.Code, finding.Message)
	}
}
