package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type projectSkillRecord struct {
	ID          string
	Version     string
	Source      string
	Scope       string
	RequiredFor string
	Adoption    string
	Path        string
	SHA256      string
}

type skillStatusEntry struct {
	ID       string `json:"id"`
	Scope    string `json:"scope"`
	Required string `json:"required_for,omitempty"`
	Status   string `json:"status"`
	Path     string `json:"path,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type skillStatusReport struct {
	Project string             `json:"project,omitempty"`
	Skills  []skillStatusEntry `json:"skills"`
}

type projectCheckReport struct {
	Project     string   `json:"project"`
	Valid       bool     `json:"valid"`
	Missing     []string `json:"missing,omitempty"`
	Invalid     []string `json:"invalid,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	IntakeValid bool     `json:"intake_valid"`
}

func runProjectCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(projectHelpText)
		return nil
	}
	if args[0] == "adopt" {
		return runProjectAdopt(args[1:])
	}
	if args[0] == "status" {
		return runProjectStatus(args[1:])
	}
	if args[0] == "schema" || args[0] == "migrate" {
		return runProjectSchemaOperation(args[0], args[1:])
	}
	if args[0] == "doctor" {
		return runProjectDoctor(args[1:])
	}
	if args[0] != "check" {
		return fmt.Errorf("unknown project operation %q; use 'leadfox project --help' for usage", args[0])
	}
	root, format, err := parseProjectCheckOptions(args[1:])
	if err != nil {
		return err
	}
	report, err := checkProject(root)
	if err != nil {
		return err
	}
	if format == "json" {
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(encoded))
	} else {
		printProjectCheckReport(report)
	}
	if !report.Valid {
		return errors.New("project structure check failed")
	}
	return nil
}

func parseProjectCheckOptions(args []string) (string, string, error) {
	root := "."
	format := "markdown"
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--format":
			if index+1 >= len(args) {
				return "", "", errors.New("--format requires markdown or json")
			}
			index++
			format = args[index]
			if format != "markdown" && format != "json" {
				return "", "", fmt.Errorf("unsupported project report format %q", format)
			}
		default:
			if strings.HasPrefix(args[index], "-") {
				return "", "", fmt.Errorf("unknown project option %q", args[index])
			}
			if root != "." {
				return "", "", errors.New("only one project path may be provided")
			}
			root = args[index]
		}
	}
	abs, err := filepath.Abs(root)
	return abs, format, err
}

func checkProject(root string) (projectCheckReport, error) {
	report := projectCheckReport{Project: root, IntakeValid: false}
	for _, path := range []string{
		"control/project.yaml", "control/project-intake.md", "control/skills.yaml", "control/decisions.yaml",
		"AGENTS.md", "CLAUDE.md", "skills/leadfox-control-plan-review/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				report.Missing = append(report.Missing, path)
			} else {
				report.Invalid = append(report.Invalid, fmt.Sprintf("%s: %v", path, err))
			}
		}
	}
	if info, err := os.Lstat(filepath.Join(root, "CLAUDE.md")); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			report.Invalid = append(report.Invalid, "CLAUDE.md is not a symlink to AGENTS.md")
		} else if target, readErr := os.Readlink(filepath.Join(root, "CLAUDE.md")); readErr != nil || target != "AGENTS.md" {
			report.Invalid = append(report.Invalid, "CLAUDE.md must point to AGENTS.md")
		}
	}
	registry, err := readProjectSkillRegistry(filepath.Join(root, "control", "skills.yaml"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		report.Invalid = append(report.Invalid, err.Error())
	}
	for _, skill := range registry {
		if skill.Scope != "project" {
			continue
		}
		skillPath := filepath.Join(root, filepath.FromSlash(skill.Path))
		content, readErr := os.ReadFile(skillPath)
		if readErr != nil {
			report.Invalid = append(report.Invalid, fmt.Sprintf("project skill %s is unavailable", skill.ID))
			continue
		}
		digest := sha256.Sum256(content)
		if skill.SHA256 == "" || hex.EncodeToString(digest[:]) != skill.SHA256 {
			report.Invalid = append(report.Invalid, fmt.Sprintf("project skill %s checksum does not match", skill.ID))
		}
	}
	if intake, intakeErr := validateIntakeFile(filepath.Join(root, "control", "project-intake.md")); intakeErr == nil {
		report.IntakeValid = intake.Valid
		if !intake.Valid {
			report.Warnings = append(report.Warnings, "project intake is incomplete; run leadfox intake validate for findings")
		}
	} else if !containsProjectString(report.Missing, "control/project-intake.md") {
		report.Invalid = append(report.Invalid, intakeErr.Error())
	}
	report.Valid = len(report.Missing) == 0 && len(report.Invalid) == 0
	return report, nil
}

func readProjectSkillRegistry(path string) ([]projectSkillRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var records []projectSkillRecord
	var current *projectSkillRecord
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- id:") {
			if current != nil {
				records = append(records, *current)
			}
			current = &projectSkillRecord{ID: strings.TrimSpace(strings.TrimPrefix(trimmed, "- id:"))}
			continue
		}
		if current == nil || !strings.Contains(trimmed, ":") {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"")
		switch strings.TrimSpace(parts[0]) {
		case "version":
			current.Version = value
		case "source":
			current.Source = value
		case "scope":
			current.Scope = value
		case "required_for":
			current.RequiredFor = value
		case "adoption":
			current.Adoption = value
		case "path":
			current.Path = value
		case "sha256":
			current.SHA256 = value
		}
	}
	if current != nil {
		records = append(records, *current)
	}
	return records, nil
}

func statusSkills(options skillInstallOptions) error {
	report := skillStatusReport{}
	if cwd, err := os.Getwd(); err == nil {
		report.Project = cwd
		if records, readErr := readProjectSkillRegistry(filepath.Join(cwd, "control", "skills.yaml")); readErr == nil {
			for _, skill := range records {
				if skill.Scope != "project" {
					continue
				}
				entry := skillStatusEntry{ID: skill.ID, Scope: skill.Scope, Required: skill.RequiredFor, Path: filepath.Join(cwd, filepath.FromSlash(skill.Path)), Status: "missing"}
				content, err := os.ReadFile(entry.Path)
				if err == nil {
					digest := sha256.Sum256(content)
					if skill.SHA256 != "" && hex.EncodeToString(digest[:]) == skill.SHA256 {
						entry.Status = "installed, verified"
					} else {
						entry.Status = "modified"
					}
				}
				report.Skills = append(report.Skills, entry)
			}
		}
	}
	home, err := skillHome(options)
	if err != nil {
		return err
	}
	manifest, err := loadSkillManifest(home)
	if err != nil {
		return err
	}
	entry := skillStatusEntry{ID: planningSkillID, Scope: "global", Required: "planning", Status: "missing", Detail: "optional"}
	for _, record := range manifest.Skills {
		if record.ID != planningSkillID {
			continue
		}
		entry.Status = "installed"
		if record.SHA256 == skillDigest() {
			entry.Status = "installed, verified"
		} else {
			entry.Status = "wrong version"
		}
		entry.Path = strings.Join(record.Paths, ", ")
	}
	report.Skills = append(report.Skills, entry)
	if options.format == "json" {
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Printf("Project: %s\n", report.Project)
	for _, skill := range report.Skills {
		fmt.Printf("%s\t%s\t%s\n", skill.ID, skill.Scope, skill.Status)
		if skill.Detail != "" {
			fmt.Printf("  %s\n", skill.Detail)
		}
	}
	return nil
}

func printProjectCheckReport(report projectCheckReport) {
	if report.Valid {
		fmt.Printf("Project: VALID\nPath: %s\n", report.Project)
	} else {
		fmt.Printf("Project: INVALID\nPath: %s\n", report.Project)
	}
	if len(report.Missing) > 0 {
		fmt.Println("Missing:")
		for _, item := range report.Missing {
			fmt.Printf("- %s\n", item)
		}
	}
	if len(report.Invalid) > 0 {
		fmt.Println("Invalid:")
		for _, item := range report.Invalid {
			fmt.Printf("- %s\n", item)
		}
	}
	if len(report.Warnings) > 0 {
		fmt.Println("Warnings:")
		for _, item := range report.Warnings {
			fmt.Printf("- %s\n", item)
		}
	}
	if report.IntakeValid {
		fmt.Println("Intake: valid")
	} else {
		fmt.Println("Intake: incomplete")
	}
}

func containsProjectString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

const projectHelpText = `Inspect an initialized Leadfox project.

Usage:
  leadfox project check [PATH] [options]
  leadfox project adopt PATH [--yes] [options]
  leadfox project status [PATH] [options]
  leadfox project schema [PATH] [options]
  leadfox project migrate PATH [--yes] [options]
  leadfox project doctor [PATH] [options]

Options:
  --format markdown|json  Select report format (default markdown)
  --yes                   Apply adoption; otherwise show a dry-run
  -h, --help              Show this help message

Adoption never overwrites or deletes existing paths. It creates only missing
Leadfox structure and records the created paths in control/leadfox-adoption.yaml.
`
