package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const assuranceHelpText = `Report project assurance coverage from control/assurance.yaml.

Usage:
  leadfox assurance report [options]

Options:
  -w, --worktree DIR   Project worktree (default: current directory)
  -f, --format FORMAT  Output markdown (default) or json
  -h, --help           Show this help message

The report checks whether declared evidence references exist and whether their
optional SHA-256 hashes match. It reports evidence coverage only; it does not
certify compliance or replace an auditor.
`

type assuranceOptions struct {
	worktree string
	format   string
	showHelp bool
}

type assuranceFile struct {
	Frameworks []assuranceFramework `yaml:"frameworks,omitempty"`
	Controls   []assuranceControl   `yaml:"controls,omitempty"`
}

type assuranceFramework struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

type assuranceControl struct {
	ID        string              `yaml:"id"`
	Framework string              `yaml:"framework"`
	Objective string              `yaml:"objective"`
	Owner     string              `yaml:"owner,omitempty"`
	Status    string              `yaml:"status,omitempty"`
	Evidence  []assuranceEvidence `yaml:"evidence,omitempty"`
	Notes     string              `yaml:"notes,omitempty"`
}

type assuranceEvidence struct {
	Ref      string `yaml:"ref"`
	Type     string `yaml:"type,omitempty"`
	Required bool   `yaml:"required,omitempty"`
	SHA256   string `yaml:"sha256,omitempty"`
	Note     string `yaml:"note,omitempty"`
}

type assuranceEvidenceResult struct {
	Ref      string `json:"ref" yaml:"ref"`
	Type     string `json:"type,omitempty" yaml:"type,omitempty"`
	Required bool   `json:"required" yaml:"required"`
	Status   string `json:"status" yaml:"status"`
	SHA256   string `json:"sha256,omitempty" yaml:"sha256,omitempty"`
	Expected string `json:"expected_sha256,omitempty" yaml:"expected_sha256,omitempty"`
	Note     string `json:"note,omitempty" yaml:"note,omitempty"`
}

type assuranceControlResult struct {
	ID        string                    `json:"id" yaml:"id"`
	Framework string                    `json:"framework" yaml:"framework"`
	Objective string                    `json:"objective" yaml:"objective"`
	Owner     string                    `json:"owner,omitempty" yaml:"owner,omitempty"`
	Declared  string                    `json:"declared_status,omitempty" yaml:"declared_status,omitempty"`
	Coverage  string                    `json:"coverage" yaml:"coverage"`
	Evidence  []assuranceEvidenceResult `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	Missing   []string                  `json:"missing,omitempty" yaml:"missing,omitempty"`
	Notes     string                    `json:"notes,omitempty" yaml:"notes,omitempty"`
}

type assuranceReport struct {
	SchemaVersion int                      `json:"schema_version" yaml:"schema_version"`
	Leadfox       string                   `json:"leadfox" yaml:"leadfox"`
	GeneratedAt   string                   `json:"generated_at" yaml:"generated_at"`
	Worktree      string                   `json:"worktree" yaml:"worktree"`
	Controls      []assuranceControlResult `json:"controls" yaml:"controls"`
	Complete      int                      `json:"complete" yaml:"complete"`
	Incomplete    int                      `json:"incomplete" yaml:"incomplete"`
	Exceptions    int                      `json:"exceptions" yaml:"exceptions"`
	Status        string                   `json:"status" yaml:"status"`
}

func runAssuranceCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(assuranceHelpText)
		return nil
	}
	if args[0] != "report" {
		return fmt.Errorf("unknown assurance command %q; use 'leadfox assurance --help' for usage", args[0])
	}
	options, err := parseAssuranceOptions(args[1:])
	if err != nil {
		return err
	}
	if options.showHelp {
		fmt.Print(assuranceHelpText)
		return nil
	}
	root, err := filepath.Abs(options.worktree)
	if err != nil {
		return fmt.Errorf("resolve worktree: %w", err)
	}
	report, err := buildAssuranceReport(root)
	if err != nil {
		return err
	}
	if options.format == "json" {
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("encode assurance report: %w", marshalErr)
		}
		fmt.Println(string(encoded))
	} else {
		fmt.Print(renderAssuranceMarkdown(report))
	}
	return nil
}

func parseAssuranceOptions(args []string) (assuranceOptions, error) {
	options := assuranceOptions{worktree: ".", format: "markdown"}
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
			return options, fmt.Errorf("unknown assurance option %q", args[index])
		}
	}
	if options.format != "markdown" && options.format != "json" {
		return options, fmt.Errorf("unsupported output format %q", options.format)
	}
	return options, nil
}

func buildAssuranceReport(root string) (assuranceReport, error) {
	path := filepath.Join(root, "control", "assurance.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return assuranceReport{}, errors.New("no control/assurance.yaml found; declare assurance frameworks and controls before reporting")
		}
		return assuranceReport{}, fmt.Errorf("read assurance file: %w", err)
	}
	var document assuranceFile
	if err := yaml.Unmarshal(content, &document); err != nil {
		return assuranceReport{}, fmt.Errorf("parse assurance file: %w", err)
	}
	report := assuranceReport{SchemaVersion: 1, Leadfox: leadfoxVersion(), GeneratedAt: time.Now().UTC().Format(time.RFC3339), Worktree: root, Status: "complete"}
	for _, control := range document.Controls {
		result := assuranceControlResult{ID: control.ID, Framework: control.Framework, Objective: control.Objective, Owner: control.Owner, Declared: control.Status, Notes: control.Notes, Coverage: "complete"}
		if strings.EqualFold(control.Status, "exception") || strings.EqualFold(control.Status, "not_applicable") {
			result.Coverage = strings.ToLower(control.Status)
			report.Exceptions++
		} else {
			for _, evidence := range control.Evidence {
				item := inspectAssuranceEvidence(root, evidence)
				result.Evidence = append(result.Evidence, item)
				if evidence.Required && item.Status != "verified" && item.Status != "present" {
					result.Missing = append(result.Missing, evidence.Ref)
				}
			}
			if len(result.Missing) > 0 {
				result.Coverage = "incomplete"
				report.Incomplete++
			} else {
				report.Complete++
			}
		}
		report.Controls = append(report.Controls, result)
	}
	if report.Incomplete > 0 {
		report.Status = "incomplete"
	}
	return report, nil
}

func inspectAssuranceEvidence(root string, evidence assuranceEvidence) assuranceEvidenceResult {
	result := assuranceEvidenceResult{Ref: evidence.Ref, Type: evidence.Type, Required: evidence.Required, Expected: evidence.SHA256}
	if strings.HasPrefix(evidence.Ref, "http://") || strings.HasPrefix(evidence.Ref, "https://") {
		result.Status = "external-reference"
		result.Note = "external evidence must be reviewed and attested outside this local report"
		return result
	}
	path, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(evidence.Ref)))
	if err != nil {
		result.Status = "invalid-reference"
		result.Note = "evidence reference could not be resolved"
		return result
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		result.Status = "invalid-reference"
		result.Note = "evidence reference escapes the project worktree"
		return result
	}
	content, err := os.ReadFile(path)
	if err != nil {
		result.Status = "missing"
		result.Note = "evidence file is unavailable"
		return result
	}
	digest := sha256.Sum256(content)
	result.SHA256 = hex.EncodeToString(digest[:])
	if evidence.SHA256 != "" && !strings.EqualFold(evidence.SHA256, result.SHA256) {
		result.Status = "hash-mismatch"
		result.Note = "evidence exists but does not match the declared hash"
		return result
	}
	if evidence.SHA256 != "" {
		result.Status = "verified"
	} else {
		result.Status = "present"
	}
	result.Note = evidence.Note
	return result
}

func renderAssuranceMarkdown(report assuranceReport) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Assurance Coverage\n\n- Status: **%s**\n- Generated: %s\n- Controls complete: %d\n- Controls incomplete: %d\n- Exceptions/not applicable: %d\n\n", strings.ToUpper(report.Status), report.GeneratedAt, report.Complete, report.Incomplete, report.Exceptions)
	builder.WriteString("| Framework | Control | Owner | Coverage | Missing evidence |\n|---|---|---|---|---|\n")
	for _, control := range report.Controls {
		fmt.Fprintf(&builder, "| `%s` | `%s` | %s | **%s** | %s |\n", control.Framework, control.ID, control.Owner, control.Coverage, strings.Join(control.Missing, ", "))
	}
	builder.WriteString("\nThis report measures declared evidence coverage. It does not certify compliance or replace an independent audit.\n")
	return builder.String()
}
