package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type inspectionFinding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type jobInspectionReport struct {
	Job       string              `json:"job"`
	Operation string              `json:"operation"`
	Valid     bool                `json:"valid"`
	Status    string              `json:"status,omitempty"`
	Findings  []inspectionFinding `json:"findings,omitempty"`
}

type jobContextReport struct {
	Job              string   `json:"job"`
	Status           string   `json:"status"`
	Role             string   `json:"role"`
	Objective        string   `json:"objective"`
	BaseBranch       string   `json:"base_branch,omitempty"`
	AllowedPaths     []string `json:"allowed_paths,omitempty"`
	ExcludedPaths    []string `json:"excluded_paths,omitempty"`
	Gates            []string `json:"gates,omitempty"`
	Capabilities     []string `json:"capabilities,omitempty"`
	RiskControls     []string `json:"risk_controls,omitempty"`
	AcceptanceIDs    []string `json:"acceptance_criteria,omitempty"`
	StopBeforeReview bool     `json:"stop_before_review"`
}

func runJobInspection(operation string, args []string) error {
	id, root, format, err := parseJobInspectionOptions(args)
	if err != nil {
		return err
	}
	if id == "" {
		return errors.New("a job ID is required")
	}
	packet, _, err := readJob(root, id)
	if err != nil {
		return err
	}
	switch operation {
	case "readiness":
		report := inspectJobReadiness(root, packet)
		if err := printJobInspection(report, format); err != nil {
			return err
		}
		if !report.Valid {
			return errors.New("job is not ready")
		}
		return nil
	case "validate":
		report := inspectJobEvidence(root, packet)
		if err := printJobInspection(report, format); err != nil {
			return err
		}
		if !report.Valid {
			return errors.New("job evidence or handoff validation failed")
		}
		return nil
	case "context":
		return printJobContext(jobContextFromPacket(packet), format)
	default:
		return fmt.Errorf("unknown job inspection %q", operation)
	}
}

func parseJobInspectionOptions(args []string) (string, string, string, error) {
	id, root, format := "", ".", "markdown"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) || (args[i+1] != "markdown" && args[i+1] != "json") {
				return "", "", "", errors.New("--format requires markdown or json")
			}
			format = args[i+1]
			i++
		case "--project":
			if i+1 >= len(args) {
				return "", "", "", errors.New("--project requires a path")
			}
			root = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", "", fmt.Errorf("unknown option %q", args[i])
			}
			if id != "" {
				return "", "", "", errors.New("only one job ID may be provided")
			}
			id = args[i]
		}
	}
	if root == "." {
		root, _ = gitRoot(".")
	}
	abs, err := filepath.Abs(root)
	return id, abs, format, err
}

func inspectJobReadiness(root string, packet jobPacket) jobInspectionReport {
	report := jobInspectionReport{Job: packet.ID, Operation: "readiness", Status: packet.Status, Valid: true}
	add := func(code, message string) {
		report.Valid = false
		report.Findings = append(report.Findings, inspectionFinding{Code: code, Message: message})
	}
	if packet.Status != "ready" {
		add("status", fmt.Sprintf("job status is %q; Control must mark it ready before start", packet.Status))
	}
	if strings.TrimSpace(packet.Role) == "" {
		add("role", "job role is required")
	}
	if strings.TrimSpace(packet.Objective) == "" {
		add("objective", "job objective is required")
	}
	if len(packet.AllowedPaths) == 0 {
		add("scope", "at least one allowed path is required")
	}
	for _, path := range append(append([]string{}, packet.AllowedPaths...), packet.ExcludedPaths...) {
		if filepath.IsAbs(path) || strings.Contains(filepath.Clean(path), "..") {
			add("unsafe-path", fmt.Sprintf("job path is not project-relative: %s", path))
		}
	}
	for _, allowed := range packet.AllowedPaths {
		for _, excluded := range packet.ExcludedPaths {
			if allowed == excluded {
				add("scope-overlap", fmt.Sprintf("path is both allowed and excluded: %s", allowed))
			}
		}
	}
	if err := validateJobCapabilities(root, packet.Capabilities); err != nil {
		add("capability", err.Error())
	}
	if err := validateJobRisks(root, packet.RiskControls); err != nil {
		add("risk", err.Error())
	}
	return report
}

func inspectJobEvidence(root string, packet jobPacket) jobInspectionReport {
	report := jobInspectionReport{Job: packet.ID, Operation: "validate", Status: packet.Status, Valid: true}
	add := func(code, message string) {
		report.Valid = false
		report.Findings = append(report.Findings, inspectionFinding{Code: code, Message: message})
	}
	if packet.ReviewCommit == "" && (packet.Status == "review" || packet.Status == "review-pending" || packet.Status == "approved" || packet.Status == "merged") {
		add("review-commit", "reviewed jobs require a reviewed commit")
	}
	if packet.ReviewCommit != "" {
		handoff := filepath.ToSlash(filepath.Join("control", "handoffs", packet.ID+".md"))
		if _, err := gitShowFile(root, packet.ReviewCommit, handoff); err != nil {
			add("handoff", fmt.Sprintf("reviewed commit does not contain %s", handoff))
		}
	}
	if packet.Status == "merged" || packet.Status == "approved" {
		if err := acceptanceCriteriaError(packet); err != nil {
			add("acceptance", err.Error())
		}
		evidenceIDs := map[string]bool{}
		for _, evidence := range packet.ExternalEvidence {
			evidenceIDs[evidence.ID] = true
		}
		for _, criterion := range packet.AcceptanceCriteria {
			if criterion.Status != "passed" && criterion.Status != "waived" {
				continue
			}
			if len(criterion.Evidence) == 0 && criterion.Status != "waived" {
				add("criterion-evidence", fmt.Sprintf("criterion %s is passed without evidence", criterion.ID))
			}
			for _, evidenceID := range criterion.Evidence {
				if !evidenceIDs[evidenceID] {
					add("criterion-evidence", fmt.Sprintf("criterion %s references missing evidence %s", criterion.ID, evidenceID))
				}
			}
		}
		for _, gate := range packet.Gates {
			verified := false
			for _, evidence := range packet.GateEvidence {
				if evidence.Gate == gate && evidence.Verified && strings.EqualFold(evidence.Status, "passed") {
					verified = true
				}
			}
			if !verified {
				add("gate-evidence", fmt.Sprintf("required gate %s has no verified passing evidence", gate))
			}
		}
	}
	return report
}

func jobContextFromPacket(packet jobPacket) jobContextReport {
	ids := make([]string, 0, len(packet.AcceptanceCriteria))
	for _, criterion := range packet.AcceptanceCriteria {
		ids = append(ids, criterion.ID)
	}
	return jobContextReport{Job: packet.ID, Status: packet.Status, Role: packet.Role, Objective: packet.Objective, BaseBranch: packet.BaseBranch, AllowedPaths: packet.AllowedPaths, ExcludedPaths: packet.ExcludedPaths, Gates: packet.Gates, Capabilities: packet.Capabilities, RiskControls: packet.RiskControls, AcceptanceIDs: ids, StopBeforeReview: packet.Status != "approved" && packet.Status != "merged"}
}

func printJobInspection(report jobInspectionReport, format string) error {
	if format == "json" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	state := "VALID"
	if !report.Valid {
		state = "INVALID"
	}
	fmt.Printf("Job %s: %s\nStatus: %s\n", report.Job, state, report.Status)
	for _, finding := range report.Findings {
		fmt.Printf("- [%s] %s\n", finding.Code, finding.Message)
	}
	return nil
}

func printJobContext(report jobContextReport, format string) error {
	if format == "json" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Printf("# Job context: %s\n\nStatus: %s\nRole: %s\nObjective: %s\n", report.Job, report.Status, report.Role, report.Objective)
	fmt.Printf("Allowed paths: %s\nExcluded paths: %s\nGates: %s\nCapabilities: %s\nRisk controls: %s\nAcceptance criteria: %s\nStop before review: %t\n", strings.Join(report.AllowedPaths, ", "), strings.Join(report.ExcludedPaths, ", "), strings.Join(report.Gates, ", "), strings.Join(report.Capabilities, ", "), strings.Join(report.RiskControls, ", "), strings.Join(report.AcceptanceIDs, ", "), report.StopBeforeReview)
	return nil
}

type capabilityHealthReport struct {
	Status       string   `json:"status"`
	Declared     []string `json:"declared"`
	Referenced   []string `json:"referenced"`
	Unreferenced []string `json:"unreferenced,omitempty"`
	Missing      []string `json:"missing,omitempty"`
	Note         string   `json:"note"`
}

func capabilityHealth(root string, document capabilityFile) error {
	declared := make([]string, 0, len(document.Capabilities))
	declaredSet := map[string]bool{}
	for _, capability := range document.Capabilities {
		declared = append(declared, capability.ID)
		declaredSet[capability.ID] = true
	}
	sort.Strings(declared)
	referencedSet := map[string]bool{}
	if entries, err := os.ReadDir(filepath.Join(root, "control", "jobs")); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || entry.Name() == "README.md" {
				continue
			}
			packet, _, readErr := readJob(root, strings.TrimSuffix(entry.Name(), ".md"))
			if readErr == nil {
				for _, id := range packet.Capabilities {
					referencedSet[id] = true
				}
			}
		}
	}
	referenced := make([]string, 0, len(referencedSet))
	missing := []string{}
	for id := range referencedSet {
		referenced = append(referenced, id)
		if !declaredSet[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(referenced)
	sort.Strings(missing)
	unreferenced := []string{}
	for _, id := range declared {
		if !referencedSet[id] {
			unreferenced = append(unreferenced, id)
		}
	}
	report := capabilityHealthReport{Status: "healthy", Declared: declared, Referenced: referenced, Unreferenced: unreferenced, Missing: missing, Note: "Leadfox validates declarations and references; host credentials and provider availability require an agent-host check."}
	if len(missing) > 0 {
		report.Status = "invalid"
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	if report.Status != "healthy" {
		return errors.New("capability declarations have missing references")
	}
	return nil
}

type changeImpactReport struct {
	Path         string   `json:"path"`
	ChangeID     string   `json:"change_id,omitempty"`
	ChangeType   string   `json:"change_type,omitempty"`
	Status       string   `json:"status,omitempty"`
	Approval     string   `json:"approval,omitempty"`
	Affected     []string `json:"affected,omitempty"`
	Signals      []string `json:"signals,omitempty"`
	ProposedJobs string   `json:"proposed_jobs,omitempty"`
}

func runChangeImpactCommand(args []string) error {
	path, format, err := parseChangeOptions(args)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	meta, body, _ := parseFrontMatter(string(data))
	lower := strings.ToLower(string(data))
	report := changeImpactReport{Path: path, ChangeID: meta["change_id"], ChangeType: meta["change_type"], Status: meta["status"], Approval: meta["approval"], ProposedJobs: labelValue(body, "Proposed jobs")}
	for _, label := range []string{"Existing services affected", "Existing integrations affected", "Existing data stores affected", "Existing environments affected", "Existing requirements affected", "Existing decisions or jobs affected"} {
		value := labelValue(body, label)
		if value != "" {
			report.Affected = append(report.Affected, label+": "+value)
		}
	}
	for _, item := range []struct{ text, signal string }{{"secret", "security-sensitive"}, {"credential", "security-sensitive"}, {"iam", "approval-sensitive"}, {"public", "public-exposure-review"}, {"destruct", "destructive-review"}, {"cost", "cost-review"}, {"migration", "data-migration-review"}} {
		if strings.Contains(lower, item.text) && !containsInspectionString(report.Signals, item.signal) {
			report.Signals = append(report.Signals, item.signal)
		}
	}
	sort.Strings(report.Affected)
	sort.Strings(report.Signals)
	if format == "json" {
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Printf("Change: %s\nType: %s\nStatus: %s\nApproval: %s\n", report.ChangeID, report.ChangeType, report.Status, report.Approval)
	fmt.Println("Affected:")
	for _, item := range report.Affected {
		fmt.Printf("- %s\n", item)
	}
	fmt.Println("Signals:")
	for _, item := range report.Signals {
		fmt.Printf("- %s\n", item)
	}
	if report.ProposedJobs != "" {
		fmt.Printf("Proposed jobs: %s\n", report.ProposedJobs)
	}
	return nil
}

func labelValue(body, label string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		prefix := label + ":"
		if strings.HasPrefix(trimmed, prefix) {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			if value != "" && !strings.Contains(value, "<!--") {
				return value
			}
		}
	}
	return ""
}
func containsInspectionString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
