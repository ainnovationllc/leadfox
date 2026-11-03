package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type diagnosticFinding struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

type diagnosticReport struct {
	Project  string              `json:"project"`
	Status   string              `json:"status"`
	Findings []diagnosticFinding `json:"findings,omitempty"`
}

func runProjectDoctor(args []string) error {
	root, format, err := parseReportPathFormat(args)
	if err != nil {
		return err
	}
	report := buildDiagnosticReport(root)
	if format == "json" {
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(encoded))
	} else {
		printDiagnosticReport(report)
	}
	if report.Status == "findings" {
		return errors.New("project doctor found issues")
	}
	return nil
}

func buildDiagnosticReport(root string) diagnosticReport {
	report := diagnosticReport{Project: root, Status: "healthy"}
	add := func(code, severity, message, remediation string) {
		report.Findings = append(report.Findings, diagnosticFinding{Code: code, Severity: severity, Message: message, Remediation: remediation})
		if severity == "error" {
			report.Status = "findings"
		} else if report.Status == "healthy" {
			report.Status = "warnings"
		}
	}
	check, err := checkProject(root)
	if err != nil {
		add("project.check.error", "error", err.Error(), "Run the command from a project root or use an explicit project path.")
		return report
	}
	for _, missing := range check.Missing {
		add("project.missing", "error", "missing managed path: "+missing, "Run leadfox project migrate or leadfox project adopt after reviewing the dry run.")
	}
	for _, invalid := range check.Invalid {
		add("project.invalid", "error", invalid, "Repair the managed project structure and rerun project doctor.")
	}
	if !check.IntakeValid {
		add("intake.incomplete", "warning", "project intake is incomplete", "Run leadfox intake validate and complete the living intake document.")
	}
	if _, err := loadDecisionPolicy(root); err != nil {
		add("policy.invalid", "error", err.Error(), "Run leadfox policy validate.")
	}
	if _, err := loadDecisionLedger(root); err != nil {
		add("decisions.invalid", "error", err.Error(), "Run leadfox decision validate.")
	}
	if _, err := loadCapabilities(root); err != nil {
		add("capabilities.invalid", "error", err.Error(), "Run leadfox capability validate or repair the capability registry.")
	}
	if operations, operationsErr := loadOperations(root); operationsErr == nil {
		if len(findOperationalPaths(root)) > 0 {
			add("operations.stray-state", "error", "operational state or credential-shaped files are present in the development checkout", "Move them to the registered operations checkout before cleanup or cloud work.")
		}
		if !isWithin(operations.Checkout, root) {
			add("operations.checkout", "warning", "a separate operations checkout is registered", "Run stateful operations from the registered checkout and do not treat it as a disposable job worktree.")
		}
	}
	if entries, readErr := os.ReadDir(filepath.Join(root, "control", "jobs")); readErr == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || entry.Name() == "README.md" {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".md")
			packet, _, packetErr := readJob(root, id)
			if packetErr != nil {
				add("job.unreadable", "error", fmt.Sprintf("job %s: %v", id, packetErr), "Repair the job packet front matter.")
				continue
			}
			if packet.Status == "ready" {
				readiness := inspectJobReadiness(root, packet)
				for _, finding := range readiness.Findings {
					add("job."+finding.Code, "error", fmt.Sprintf("%s: %s", id, finding.Message), "Run leadfox job readiness and correct the packet before starting it.")
				}
			}
		}
	}
	return report
}

func printDiagnosticReport(report diagnosticReport) {
	fmt.Printf("Project doctor: %s\nPath: %s\n", report.Status, report.Project)
	if len(report.Findings) == 0 {
		fmt.Println("No findings.")
		return
	}
	for _, finding := range report.Findings {
		fmt.Printf("- [%s] %s: %s\n", finding.severityLabel(), finding.Code, finding.Message)
		if finding.Remediation != "" {
			fmt.Printf("  Remedy: %s\n", finding.Remediation)
		}
	}
}

func (finding diagnosticFinding) severityLabel() string { return strings.ToUpper(finding.Severity) }
