package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const decisionsFileName = "control/decisions.yaml"

type projectMetadata struct {
	Project struct {
		Name    string `yaml:"name"`
		Version int    `yaml:"version"`
	} `yaml:"project"`
	Services             []interface{} `yaml:"services"`
	ExternalDependencies []interface{} `yaml:"external_dependencies"`
	Environments         []string      `yaml:"environments"`
	EntryPoints          []interface{} `yaml:"entry_points"`
	DataStores           []interface{} `yaml:"data_stores"`
}

type projectJobSummary struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Role   string `json:"role,omitempty"`
}

type projectStatusReport struct {
	Project       string              `json:"project"`
	Name          string              `json:"name,omitempty"`
	ModelVersion  int                 `json:"model_version,omitempty"`
	Structure     string              `json:"structure"`
	Intake        string              `json:"intake"`
	Policy        string              `json:"decision_policy"`
	DecisionCount int                 `json:"decision_count"`
	DecisionStats map[string]int      `json:"decision_stats"`
	Jobs          []projectJobSummary `json:"jobs,omitempty"`
	JobStats      map[string]int      `json:"job_stats"`
	GateCount     int                 `json:"gate_count"`
	CheckCount    int                 `json:"check_count"`
	Warnings      []string            `json:"warnings,omitempty"`
}

type projectContextReport struct {
	GeneratedBy string              `json:"generated_by"`
	Status      projectStatusReport `json:"status"`
	Policy      decisionPolicy      `json:"policy"`
	Decisions   []contextDecision   `json:"decisions,omitempty"`
}

// contextDecision deliberately contains decision metadata only. Free-form
// rationale, evidence and scope remain in the project ledger and are not
// copied into the compact agent context.
type contextDecision struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	DecidedBy string `json:"decided_by,omitempty"`
}

type decisionFile struct {
	Version   int               `yaml:"version"`
	Decisions []projectDecision `yaml:"decisions"`
}

type projectDecision struct {
	ID          string   `yaml:"id" json:"id"`
	Title       string   `yaml:"title" json:"title"`
	Action      string   `yaml:"action" json:"action"`
	Status      string   `yaml:"status" json:"status"`
	Scope       string   `yaml:"scope,omitempty" json:"scope,omitempty"`
	Rationale   string   `yaml:"rationale,omitempty" json:"rationale,omitempty"`
	RequestedBy string   `yaml:"requested_by,omitempty" json:"requested_by,omitempty"`
	DecidedBy   string   `yaml:"decided_by,omitempty" json:"decided_by,omitempty"`
	CreatedAt   string   `yaml:"created_at" json:"created_at"`
	UpdatedAt   string   `yaml:"updated_at" json:"updated_at"`
	ExpiresAt   string   `yaml:"expires_at,omitempty" json:"expires_at,omitempty"`
	Evidence    []string `yaml:"evidence,omitempty" json:"evidence,omitempty"`
}

var validDecisionStatuses = map[string]bool{
	"proposed": true, "approved": true, "rejected": true, "deferred": true,
}

func loadDecisionLedger(root string) (decisionFile, error) {
	content, err := os.ReadFile(filepath.Join(root, decisionsFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return decisionFile{Version: 1}, nil
		}
		return decisionFile{}, fmt.Errorf("read %s: %w", decisionsFileName, err)
	}
	var document decisionFile
	if err := yaml.Unmarshal(content, &document); err != nil {
		return decisionFile{}, fmt.Errorf("parse %s: %w", decisionsFileName, err)
	}
	if err := validateDecisionLedger(document); err != nil {
		return decisionFile{}, err
	}
	return document, nil
}

func validateDecisionLedger(document decisionFile) error {
	if document.Version != 1 {
		return fmt.Errorf("decision ledger version must be 1, got %d", document.Version)
	}
	seen := map[string]bool{}
	for _, decision := range document.Decisions {
		if !decisionIDPattern.MatchString(decision.ID) {
			return fmt.Errorf("invalid decision ID %q", decision.ID)
		}
		if seen[decision.ID] {
			return fmt.Errorf("duplicate decision ID %q", decision.ID)
		}
		seen[decision.ID] = true
		if strings.TrimSpace(decision.Title) == "" || strings.TrimSpace(decision.Action) == "" {
			return fmt.Errorf("decision %q requires title and action", decision.ID)
		}
		if !validDecisionStatuses[decision.Status] {
			return fmt.Errorf("decision %q has unsupported status %q", decision.ID, decision.Status)
		}
		if decision.Status == "approved" || decision.Status == "rejected" {
			if strings.TrimSpace(decision.DecidedBy) == "" {
				return fmt.Errorf("decision %q with status %s requires decided_by", decision.ID, decision.Status)
			}
		}
	}
	return nil
}

func runProjectStatus(args []string) error {
	root, format, err := parseReportPathFormat(args)
	if err != nil {
		return err
	}
	report, err := buildProjectStatus(root)
	if err != nil {
		return err
	}
	return printProjectStatus(report, format)
}

func buildProjectStatus(root string) (projectStatusReport, error) {
	report := projectStatusReport{Project: root, Structure: "valid", Intake: "incomplete", Policy: "missing", DecisionStats: map[string]int{}, JobStats: map[string]int{}}
	if content, err := os.ReadFile(filepath.Join(root, "control", "project.yaml")); err == nil {
		var metadata projectMetadata
		if yaml.Unmarshal(content, &metadata) == nil {
			report.Name, report.ModelVersion = metadata.Project.Name, metadata.Project.Version
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, fmt.Errorf("read project metadata: %w", err)
	}
	check, err := checkProject(root)
	if err != nil {
		return report, err
	}
	if !check.Valid {
		report.Structure = "invalid"
		report.Warnings = append(report.Warnings, check.Missing...)
		report.Warnings = append(report.Warnings, check.Invalid...)
	}
	if check.IntakeValid {
		report.Intake = "valid"
	}
	if _, err := loadDecisionPolicy(root); err == nil {
		report.Policy = "valid"
	} else if !strings.Contains(err.Error(), "no control/decision-policy.yaml") {
		report.Policy = "invalid"
		report.Warnings = append(report.Warnings, err.Error())
	}
	ledger, err := loadDecisionLedger(root)
	if err != nil {
		report.Warnings = append(report.Warnings, err.Error())
	} else {
		report.DecisionCount = len(ledger.Decisions)
		for _, decision := range ledger.Decisions {
			report.DecisionStats[decision.Status]++
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
				report.Warnings = append(report.Warnings, fmt.Sprintf("job %s: %v", id, packetErr))
				continue
			}
			report.Jobs = append(report.Jobs, projectJobSummary{ID: packet.ID, Status: packet.Status, Role: packet.Role})
			report.JobStats[packet.Status]++
		}
	}
	sort.Slice(report.Jobs, func(i, j int) bool { return report.Jobs[i].ID < report.Jobs[j].ID })
	if content, readErr := os.ReadFile(filepath.Join(root, "control", "gates.yaml")); readErr == nil {
		var gates struct {
			Gates []struct {
				Checks []interface{} `yaml:"checks"`
			} `yaml:"gates"`
		}
		if yaml.Unmarshal(content, &gates) == nil {
			report.GateCount = len(gates.Gates)
			for _, gate := range gates.Gates {
				report.CheckCount += len(gate.Checks)
			}
		}
	}
	return report, nil
}

func printProjectStatus(report projectStatusReport, format string) error {
	if format == "json" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Printf("Project: %s\n", report.Name)
	fmt.Printf("Path: %s\nStructure: %s\nIntake: %s\nDecision policy: %s\n", report.Project, report.Structure, report.Intake, report.Policy)
	fmt.Printf("Decisions: %d\nJobs: %d\nGates: %d (%d checks)\n", report.DecisionCount, len(report.Jobs), report.GateCount, report.CheckCount)
	if len(report.DecisionStats) > 0 {
		fmt.Printf("Decision statuses: %s\n", formatCounts(report.DecisionStats))
	}
	if len(report.JobStats) > 0 {
		fmt.Printf("Job statuses: %s\n", formatCounts(report.JobStats))
	}
	if len(report.Warnings) > 0 {
		fmt.Println("Warnings:")
		for _, warning := range report.Warnings {
			fmt.Printf("- %s\n", warning)
		}
	}
	return nil
}

func formatCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(values, ", ")
}

func runContextCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(contextHelpText)
		return nil
	}
	if args[0] != "export" {
		return fmt.Errorf("unknown context operation %q; use 'leadfox context --help' for usage", args[0])
	}
	root, format, err := parseReportPathFormat(args[1:])
	if err != nil {
		return err
	}
	status, err := buildProjectStatus(root)
	if err != nil {
		return err
	}
	policy, err := loadDecisionPolicy(root)
	if err != nil {
		return err
	}
	ledger, err := loadDecisionLedger(root)
	if err != nil {
		return err
	}
	contextDecisions := make([]contextDecision, 0, len(ledger.Decisions))
	for _, decision := range ledger.Decisions {
		contextDecisions = append(contextDecisions, contextDecision{ID: decision.ID, Action: decision.Action, Status: decision.Status, DecidedBy: decision.DecidedBy})
	}
	report := projectContextReport{GeneratedBy: "leadfox context export", Status: status, Policy: policy.DecisionPolicy, Decisions: contextDecisions}
	if format == "json" {
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Printf("# Leadfox Control Context\n\nProject: %s\nPath: %s\nStructure: %s\nIntake: %s\nDecision policy: %s\n\n## Decisions\n", status.Name, status.Project, status.Structure, status.Intake, status.Policy)
	if len(contextDecisions) == 0 {
		fmt.Println("- None recorded.")
	} else {
		for _, decision := range contextDecisions {
			fmt.Printf("- %s: %s (%s)\n", decision.ID, decision.Action, decision.Status)
		}
	}
	fmt.Println("\n## Jobs")
	if len(status.Jobs) == 0 {
		fmt.Println("- None recorded.")
	} else {
		for _, job := range status.Jobs {
			fmt.Printf("- %s: %s%s\n", job.ID, job.Status, optionalRole(job.Role))
		}
	}
	fmt.Printf("\n## Gates\n- %d gate(s), %d check(s)\n", status.GateCount, status.CheckCount)
	if len(status.Warnings) > 0 {
		fmt.Println("\n## Warnings")
		for _, warning := range status.Warnings {
			fmt.Printf("- %s\n", warning)
		}
	}
	return nil
}

func optionalRole(role string) string {
	if role == "" {
		return ""
	}
	return " (" + role + ")"
}

func parseReportPathFormat(args []string) (string, string, error) {
	root, format := ".", "markdown"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) || (args[i+1] != "markdown" && args[i+1] != "json") {
				return "", "", errors.New("--format requires markdown or json")
			}
			format = args[i+1]
			i++
		case "-h", "--help":
			return "", "", errors.New("help requested")
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", fmt.Errorf("unknown option %q", args[i])
			}
			if root != "." {
				return "", "", errors.New("only one project path may be provided")
			}
			root = args[i]
		}
	}
	abs, err := filepath.Abs(root)
	return abs, format, err
}

type decisionOptions struct {
	root, title, action, status, scope, rationale, requestedBy, decidedBy, expires, format string
	evidence                                                                               []string
}

func runDecisionCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(decisionHelpText)
		return nil
	}
	operation, id := args[0], ""
	if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		id = args[1]
	}
	options, err := parseDecisionOptions(args[1:])
	if err != nil {
		return err
	}
	if options.root == "" {
		options.root, err = gitRoot(".")
		if err != nil {
			return err
		}
	} else {
		options.root, err = filepath.Abs(options.root)
		if err != nil {
			return err
		}
	}
	ledger, err := loadDecisionLedger(options.root)
	if err != nil {
		return err
	}
	switch operation {
	case "validate":
		if id != "" {
			return errors.New("decision validate takes no ID")
		}
		fmt.Printf("Validated %s (version %d)\n", decisionsFileName, ledger.Version)
		return nil
	case "list":
		if id != "" {
			return errors.New("decision list takes no ID")
		}
		sort.Slice(ledger.Decisions, func(i, j int) bool { return ledger.Decisions[i].ID < ledger.Decisions[j].ID })
		return printDecisions(ledger.Decisions, options.root, options.format)
	case "show":
		decision, found := findDecision(ledger.Decisions, id)
		if !found {
			return fmt.Errorf("decision %q not found", id)
		}
		return printDecisions([]projectDecision{decision}, options.root, options.format)
	case "add":
		if id == "" {
			return errors.New("decision add requires an ID")
		}
		if options.title == "" || options.action == "" {
			return errors.New("decision add requires --title and --action")
		}
		if !validDecisionStatuses[options.status] {
			return fmt.Errorf("unsupported decision status %q", options.status)
		}
		if _, found := findDecision(ledger.Decisions, id); found {
			return fmt.Errorf("decision %q already exists", id)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		decision := projectDecision{ID: id, Title: options.title, Action: options.action, Status: options.status, Scope: options.scope, Rationale: options.rationale, RequestedBy: options.requestedBy, DecidedBy: options.decidedBy, CreatedAt: now, UpdatedAt: now, ExpiresAt: options.expires, Evidence: options.evidence}
		ledger.Decisions = append(ledger.Decisions, decision)
		if err := validateDecisionLedger(ledger); err != nil {
			return err
		}
		return writeDecisionLedger(options.root, ledger)
	case "approve", "reject", "defer":
		if id == "" {
			return fmt.Errorf("decision %s requires an ID", operation)
		}
		index := decisionIndex(ledger.Decisions, id)
		if index < 0 {
			return fmt.Errorf("decision %q not found", id)
		}
		if strings.TrimSpace(options.decidedBy) == "" {
			return fmt.Errorf("decision %s requires --by NAME", operation)
		}
		ledger.Decisions[index].Status = map[string]string{"approve": "approved", "reject": "rejected", "defer": "deferred"}[operation]
		ledger.Decisions[index].DecidedBy = options.decidedBy
		if options.rationale != "" {
			ledger.Decisions[index].Rationale = options.rationale
		}
		ledger.Decisions[index].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := validateDecisionLedger(ledger); err != nil {
			return err
		}
		return writeDecisionLedger(options.root, ledger)
	default:
		return fmt.Errorf("unknown decision operation %q; use 'leadfox decision --help' for usage", operation)
	}
}

func parseDecisionOptions(args []string) (decisionOptions, error) {
	options := decisionOptions{status: "proposed", format: "markdown"}
	for i := 0; i < len(args); i++ {
		key := args[i]
		if key == "--project" {
			if i+1 >= len(args) {
				return options, errors.New("--project requires a path")
			}
			options.root = args[i+1]
			i++
			continue
		}
		if key == "--evidence" {
			if i+1 >= len(args) {
				return options, errors.New("--evidence requires a value")
			}
			options.evidence = append(options.evidence, args[i+1])
			i++
			continue
		}
		if !strings.HasPrefix(key, "--") {
			continue
		}
		if i+1 >= len(args) {
			return options, fmt.Errorf("%s requires a value", key)
		}
		value := args[i+1]
		i++
		switch key {
		case "--title":
			options.title = value
		case "--action":
			options.action = value
		case "--status":
			options.status = value
		case "--format":
			if value != "markdown" && value != "json" {
				return options, errors.New("--format requires markdown or json")
			}
			options.format = value
		case "--scope":
			options.scope = value
		case "--rationale", "--reason":
			options.rationale = value
		case "--requested-by":
			options.requestedBy = value
		case "--decided-by", "--by":
			options.decidedBy = value
		case "--expires":
			options.expires = value
		default:
			return options, fmt.Errorf("unknown decision option %q", key)
		}
	}
	return options, nil
}
func findDecision(decisions []projectDecision, id string) (projectDecision, bool) {
	index := decisionIndex(decisions, id)
	if index < 0 {
		return projectDecision{}, false
	}
	return decisions[index], true
}
func decisionIndex(decisions []projectDecision, id string) int {
	for index, decision := range decisions {
		if decision.ID == id {
			return index
		}
	}
	return -1
}
func writeDecisionLedger(root string, ledger decisionFile) error {
	encoded, err := yaml.Marshal(ledger)
	if err != nil {
		return err
	}
	path := filepath.Join(root, decisionsFileName)
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", decisionsFileName, err)
	}
	fmt.Printf("Recorded decision ledger: %s\n", path)
	return nil
}
func printDecisions(decisions []projectDecision, root, format string) error {
	if format == "json" {
		encoded, err := json.MarshalIndent(decisions, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Println("ID\tSTATUS\tACTION\tTITLE")
	for _, decision := range decisions {
		fmt.Printf("%s\t%s\t%s\t%s\n", decision.ID, decision.Status, decision.Action, decision.Title)
	}
	return nil
}

const contextHelpText = `Export a compact, non-secret Control context.

Usage:
  leadfox context export [PATH] [--format markdown|json]
`

const decisionHelpText = `Record and inspect project decisions.

Usage:
  leadfox decision validate [--project PATH]
  leadfox decision list [--project PATH] [--format json]
  leadfox decision show ID [--project PATH] [--format json]
  leadfox decision add ID --title TEXT --action ID [options]
  leadfox decision approve|reject|defer ID --by NAME [--reason TEXT]

The ledger records governance history; it does not grant authority, contact
external services, or execute the action being decided.
`
