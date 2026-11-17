package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	conventionsPath     = "control/conventions.yaml"
	controlHandoffGlob  = "CONTROL-*.md"
	intakeHistoryLength = 5
)

// controlBrief is a read-only takeover briefing for a new Control agent:
// live work first, history only as counts. It grants no authority.
type controlBrief struct {
	Project        string            `json:"project"`
	Path           string            `json:"path"`
	Leadfox        string            `json:"leadfox"`
	Structure      string            `json:"structure"`
	Intake         string            `json:"intake"`
	Policy         string            `json:"decision_policy"`
	Warnings       []string          `json:"warnings,omitempty"`
	ReadingOrder   []briefReading    `json:"reading_order"`
	Jobs           []briefJob        `json:"live_jobs,omitempty"`
	Drafts         []string          `json:"draft_jobs,omitempty"`
	ChangePlans    []briefChangePlan `json:"live_change_plans,omitempty"`
	Decisions      []briefDecision   `json:"open_decisions,omitempty"`
	IntakeHistory  []string          `json:"recent_intake_history,omitempty"`
	Conventions    []ownerConvention `json:"conventions,omitempty"`
	ControlHandoff *briefHandoff     `json:"control_handoff,omitempty"`
	History        briefHistory      `json:"history"`
	FirstTask      string            `json:"first_task"`
}

type briefReading struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type briefJob struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Role       string   `json:"role,omitempty"`
	ChangePlan string   `json:"change_plan,omitempty"`
	Meaning    string   `json:"meaning"`
	Attention  []string `json:"attention,omitempty"`
	Blockers   []string `json:"blockers,omitempty"`
	NextAction string   `json:"next_action"`
}

type briefChangePlan struct {
	Path      string         `json:"path"`
	Status    string         `json:"status"`
	State     string         `json:"state"`
	OpenHolds []string       `json:"open_hold_points,omitempty"`
	Jobs      map[string]int `json:"jobs,omitempty"`
}

type briefDecision struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type briefHandoff struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type briefHistory struct {
	Jobs        map[string]int `json:"jobs"`
	Decisions   map[string]int `json:"decisions"`
	ChangePlans int            `json:"settled_change_plans"`
}

// ownerConvention is a standing owner rule that is not a decision, kept in
// control/conventions.yaml and included verbatim in the briefing.
type ownerConvention struct {
	ID     string `yaml:"id" json:"id"`
	Rule   string `yaml:"rule" json:"rule"`
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

type conventionsFile struct {
	Version     int               `yaml:"version"`
	Conventions []ownerConvention `yaml:"conventions"`
}

const controlFirstTask = `Before changing anything, report to the developer: the current phase; active and held change plans with their hold points; every job that is not merged or cancelled, with its next action; open owner decisions; missing gate evidence; and any inconsistency between the intake, decisions, change plans and jobs. Then stop and wait. This briefing grants no authority: the decision policy still governs what Control may do. When the developer accepts the report, they record it with leadfox control ack --by NAME.`

func loadConventions(root string) ([]ownerConvention, error) {
	content, err := os.ReadFile(filepath.Join(root, conventionsPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var document conventionsFile
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("parse %s: %w", conventionsPath, err)
	}
	if document.Version != 1 {
		return nil, fmt.Errorf("%s version must be 1, got %d", conventionsPath, document.Version)
	}
	for index, convention := range document.Conventions {
		if strings.TrimSpace(convention.ID) == "" || strings.TrimSpace(convention.Rule) == "" {
			return nil, fmt.Errorf("%s convention %d requires id and rule", conventionsPath, index+1)
		}
	}
	return document.Conventions, nil
}

// latestControlHandoff returns the newest control/handoffs/CONTROL-*.md.
func latestControlHandoff(root string) (*briefHandoff, error) {
	matches, err := filepath.Glob(filepath.Join(root, "control", "handoffs", controlHandoffGlob))
	if err != nil || len(matches) == 0 {
		return nil, err
	}
	sort.Strings(matches)
	latest := matches[len(matches)-1]
	content, err := os.ReadFile(latest)
	if err != nil {
		return nil, err
	}
	relative, _ := filepath.Rel(root, latest)
	return &briefHandoff{Path: filepath.ToSlash(relative), Content: string(content)}, nil
}

// recentIntakeHistory returns the last rows of the intake change history.
func recentIntakeHistory(root string, count int) []string {
	content, err := os.ReadFile(filepath.Join(root, "control", "project-intake.md"))
	if err != nil {
		return nil
	}
	var rows []string
	for _, line := range strings.Split(sectionText(string(content), "Change history"), "\n") {
		cells := tableCells(line)
		for index := range cells {
			cells[index] = strings.TrimSpace(cells[index])
		}
		if len(cells) < 4 || strings.EqualFold(cells[0], "version") || strings.Trim(cells[0], "-: ") == "" {
			continue
		}
		row := strings.Join(cells, " | ")
		if len(row) > 400 {
			row = row[:400] + "…"
		}
		rows = append(rows, row)
	}
	if len(rows) > count {
		rows = rows[len(rows)-count:]
	}
	return rows
}

func buildControlBrief(root string) (controlBrief, error) {
	status, err := buildProjectStatus(root)
	if err != nil {
		return controlBrief{}, err
	}
	brief := controlBrief{
		Project: status.Name, Path: root, Leadfox: leadfoxVersion(),
		Structure: status.Structure, Intake: status.Intake, Policy: status.Policy,
		Warnings:  append([]string(nil), status.Warnings...),
		History:   briefHistory{Jobs: map[string]int{}, Decisions: map[string]int{}},
		FirstTask: controlFirstTask,
	}
	if check, err := checkProject(root); err == nil {
		for _, warning := range check.Warnings {
			if !containsProjectString(brief.Warnings, warning) {
				brief.Warnings = append(brief.Warnings, warning)
			}
		}
	}

	// Jobs: live work in detail, closed work as counts.
	packets := map[string]jobPacket{}
	for _, summary := range status.Jobs {
		packet, _, err := readJob(root, summary.ID)
		if err != nil {
			continue
		}
		packets[packet.ID] = packet
		switch {
		case packet.Status == stateMerged || packet.Status == stateCancelled:
			brief.History.Jobs[packet.Status]++
		case packet.Status == stateDraft:
			brief.Drafts = append(brief.Drafts, packet.ID)
		default:
			report := buildJobStatus(root, packet)
			job := briefJob{ID: packet.ID, Status: packet.Status, Role: packet.Role, ChangePlan: packet.ChangePlan, Meaning: report.Meaning, Attention: report.Warnings, NextAction: report.NextAction}
			for index, blocker := range report.Blockers {
				if index == 5 {
					job.Blockers = append(job.Blockers, fmt.Sprintf("… and %d more (leadfox job status %s)", len(report.Blockers)-5, packet.ID))
					break
				}
				job.Blockers = append(job.Blockers, blocker)
			}
			brief.Jobs = append(brief.Jobs, job)
		}
	}

	// Change plans: active (live jobs) or held (open hold points) in detail.
	changes, _ := filepath.Glob(filepath.Join(root, "control", "changes", "*.md"))
	sort.Strings(changes)
	var liveChanges []string
	for _, path := range changes {
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		if strings.EqualFold(filepath.Base(path), "README.md") {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		meta, _, _ := parseFrontMatter(string(content))
		plan := briefChangePlan{Path: relative, Status: meta["status"], Jobs: map[string]int{}}
		if plan.Status == "" {
			plan.Status = "unknown"
		}
		if workflow, err := loadChangeWorkflow(path); err == nil {
			for _, hold := range workflow.HoldPoints {
				if hold.Status != "released" {
					plan.OpenHolds = append(plan.OpenHolds, hold.ID+": "+hold.Description)
				}
			}
		}
		live := 0
		for _, packet := range packets {
			if packet.ChangePlan == relative {
				plan.Jobs[packet.Status]++
				if packet.Status != stateMerged && packet.Status != stateCancelled {
					live++
				}
			}
		}
		switch {
		case len(plan.OpenHolds) > 0:
			plan.State = "held"
		case live > 0:
			plan.State = "active"
		case plan.Status != "approved" && plan.Status != "unknown":
			plan.State = plan.Status
		default:
			brief.History.ChangePlans++
			continue
		}
		brief.ChangePlans = append(brief.ChangePlans, plan)
		liveChanges = append(liveChanges, relative)
	}

	// Decisions: open (proposed or deferred) in detail.
	if ledger, err := loadDecisionLedger(root); err == nil {
		for _, decision := range ledger.Decisions {
			if decision.Status == "proposed" || decision.Status == "deferred" {
				brief.Decisions = append(brief.Decisions, briefDecision{ID: decision.ID, Title: decision.Title, Action: decision.Action, Status: decision.Status, CreatedAt: decision.CreatedAt})
			} else {
				brief.History.Decisions[decision.Status]++
			}
		}
	}

	brief.IntakeHistory = recentIntakeHistory(root, intakeHistoryLength)
	if conventions, err := loadConventions(root); err != nil {
		brief.Warnings = append(brief.Warnings, err.Error())
	} else {
		brief.Conventions = conventions
	}
	if handoff, err := latestControlHandoff(root); err != nil {
		brief.Warnings = append(brief.Warnings, "read Control handoff: "+err.Error())
	} else {
		brief.ControlHandoff = handoff
	}
	brief.ReadingOrder = controlReadingOrder(root, liveChanges, brief.ControlHandoff)
	return brief, nil
}

// controlReadingOrder lists the project files a new Control should read,
// in order, keeping only those that exist.
func controlReadingOrder(root string, liveChanges []string, handoff *briefHandoff) []briefReading {
	candidates := []briefReading{
		{"AGENTS.md", "project instructions for every agent"},
		{agentRuntimeProtocolPath, "job runtime protocol: session start, dispatch, recovery"},
		{"control/decision-policy.yaml", "what Control may do alone, with a record, or only with approval"},
		{"docs/agent-roles.md", "agent roles and boundaries"},
		{conventionsPath, "standing owner conventions"},
		{"control/project-intake.md", "requirements; read the change history for the project's story"},
		{"control/project.yaml", "project model: services, environments, dependencies"},
		{"control/architecture-plan.md", "approved architecture"},
	}
	for _, change := range liveChanges {
		candidates = append(candidates, briefReading{change, "change plan with live work or open hold points"})
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(changeWorkflowPath(change)))); err == nil {
			candidates = append(candidates, briefReading{changeWorkflowPath(change), "its hold points and planned jobs"})
		}
	}
	candidates = append(candidates,
		briefReading{"control/decisions.yaml", "decision ledger; open decisions are listed below"},
	)
	if handoff != nil {
		candidates = append(candidates, briefReading{handoff.Path, "notes from the previous Control"})
	}
	candidates = append(candidates,
		briefReading{"control/gates.yaml", "required gates and their checks"},
		briefReading{"control/risk-controls.yaml", "project risk controls"},
		briefReading{"contracts", "service contracts"},
	)
	var order []briefReading
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(candidate.Path))); err == nil {
			order = append(order, candidate)
		}
	}
	return order
}

func printControlBrief(brief controlBrief, format string) error {
	if format == "json" {
		encoded, err := json.MarshalIndent(brief, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Printf("# Control briefing: %s\n\n", brief.Project)
	fmt.Printf("Path: %s\nLeadfox: %s\nStructure: %s; intake: %s; decision policy: %s\n", brief.Path, brief.Leadfox, brief.Structure, brief.Intake, brief.Policy)
	fmt.Println("\nThis briefing is guidance only; it grants no authority.")
	if len(brief.Warnings) > 0 {
		fmt.Println("\n## Project warnings")
		for _, warning := range brief.Warnings {
			fmt.Printf("- %s\n", warning)
		}
	}
	fmt.Println("\n## Reading order")
	for index, item := range brief.ReadingOrder {
		fmt.Printf("%d. `%s` — %s\n", index+1, item.Path, item.Reason)
	}
	fmt.Println("\n## Live jobs")
	if len(brief.Jobs) == 0 {
		fmt.Println("- None.")
	}
	for _, job := range brief.Jobs {
		fmt.Printf("- **%s** %s (%s)%s\n", job.ID, job.Status, job.Meaning, optionalRole(job.Role))
		if job.ChangePlan != "" {
			fmt.Printf("  - change plan: %s\n", job.ChangePlan)
		}
		for _, attention := range job.Attention {
			fmt.Printf("  - attention: %s\n", attention)
		}
		for _, blocker := range job.Blockers {
			fmt.Printf("  - blocker: %s\n", blocker)
		}
		fmt.Printf("  - next: %s\n", job.NextAction)
	}
	if len(brief.Drafts) > 0 {
		fmt.Printf("\nDraft jobs (not started): %s\n", strings.Join(brief.Drafts, ", "))
	}
	fmt.Println("\n## Live change plans")
	if len(brief.ChangePlans) == 0 {
		fmt.Println("- None.")
	}
	for _, plan := range brief.ChangePlans {
		fmt.Printf("- `%s`: %s (%s)", plan.Path, plan.State, plan.Status)
		if len(plan.Jobs) > 0 {
			fmt.Printf("; jobs %s", formatCounts(plan.Jobs))
		}
		fmt.Println()
		for _, hold := range plan.OpenHolds {
			fmt.Printf("  - open hold point %s\n", hold)
		}
	}
	fmt.Println("\n## Open decisions")
	if len(brief.Decisions) == 0 {
		fmt.Println("- None.")
	}
	for _, decision := range brief.Decisions {
		fmt.Printf("- %s (%s, %s, %s): %s\n", decision.ID, decision.Status, decision.Action, decision.CreatedAt, decision.Title)
	}
	if len(brief.IntakeHistory) > 0 {
		fmt.Println("\n## Recent intake history")
		for _, row := range brief.IntakeHistory {
			fmt.Printf("- %s\n", row)
		}
	}
	fmt.Println("\n## Owner conventions")
	if len(brief.Conventions) == 0 {
		fmt.Printf("- None recorded. Standing owner rules that are not decisions belong in `%s`.\n", conventionsPath)
	}
	for _, convention := range brief.Conventions {
		source := ""
		if convention.Source != "" {
			source = " (" + convention.Source + ")"
		}
		fmt.Printf("- %s: %s%s\n", convention.ID, convention.Rule, source)
	}
	fmt.Println("\n## Previous Control handoff")
	if brief.ControlHandoff == nil {
		fmt.Println("- None. The outgoing Control can write one with leadfox control handoff draft.")
	} else {
		fmt.Printf("From `%s`:\n\n%s\n", brief.ControlHandoff.Path, strings.TrimSpace(brief.ControlHandoff.Content))
	}
	fmt.Println("\n## History")
	fmt.Printf("- Jobs: %s\n", countsOrNone(brief.History.Jobs))
	fmt.Printf("- Decisions: %s\n", countsOrNone(brief.History.Decisions))
	fmt.Printf("- Change plans with no live work: %d\n", brief.History.ChangePlans)
	fmt.Println("- Full lists: leadfox context export, leadfox job list, leadfox decision list")
	fmt.Printf("\n## First task\n\n%s\n", brief.FirstTask)
	return nil
}

func countsOrNone(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	return formatCounts(counts)
}

var handoffDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// draftControlHandoff writes a Control handoff template, pre-filled with the
// live work, for the outgoing Control or the developer to complete.
func draftControlHandoff(root, date string) (string, error) {
	if date == "" {
		date = nowFunc().UTC().Format("2006-01-02")
	}
	if !handoffDatePattern.MatchString(date) {
		return "", fmt.Errorf("--date %q must be YYYY-MM-DD", date)
	}
	relative := filepath.ToSlash(filepath.Join("control", "handoffs", "CONTROL-"+date+".md"))
	path := filepath.Join(root, filepath.FromSlash(relative))
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists; edit it or pass --date", relative)
	}
	brief, err := buildControlBrief(root)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	fmt.Fprintf(&text, "# Control handoff %s\n\nProject: %s\nOutgoing Control: \nIncoming Control: \n\n", date, brief.Project)
	text.WriteString("Complete every section; write \"None\" where nothing applies. This note is\nguidance for the next Control; it grants no authority.\n\n")
	text.WriteString("## In-flight work\n\nLive jobs when this draft was made; add what each is waiting for.\n\n")
	if len(brief.Jobs) == 0 {
		text.WriteString("- None.\n")
	}
	for _, job := range brief.Jobs {
		fmt.Fprintf(&text, "- %s (%s): %s\n", job.ID, job.Status, job.NextAction)
	}
	for _, plan := range brief.ChangePlans {
		fmt.Fprintf(&text, "- %s: %s\n", plan.Path, plan.State)
	}
	text.WriteString("\n## Owner decisions not yet recorded\n\nVerbal or chat decisions that are not in control/decisions.yaml.\n\n- \n")
	text.WriteString("\n## Open questions for the owner\n\n- \n")
	text.WriteString("\n## Known inconsistencies\n\nDifferences between the intake, decisions, change plans and jobs.\n\n- \n")
	fmt.Fprintf(&text, "\n## Conventions to add\n\nStanding owner rules that belong in %s.\n\n- \n", conventionsPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", relative, err)
	}
	defer file.Close()
	if _, err := file.WriteString(text.String()); err != nil {
		return "", err
	}
	return relative, nil
}

// acknowledgeControl records the developer's acceptance of a new Control's
// first report as an approved agent_assignment decision.
func acknowledgeControl(root, by, agent, note string) (string, error) {
	if strings.TrimSpace(by) == "" {
		return "", errors.New("control ack requires --by NAME (the developer accepting the report)")
	}
	ledger, err := loadDecisionLedger(root)
	if err != nil {
		return "", err
	}
	now := nowFunc().UTC()
	base := "control-takeover-" + now.Format("2006-01-02")
	id := base
	for suffix := 2; decisionIndex(ledger.Decisions, id) >= 0; suffix++ {
		id = fmt.Sprintf("%s-%d", base, suffix)
	}
	title := "Control takeover accepted"
	if strings.TrimSpace(agent) != "" {
		title += ": " + strings.TrimSpace(agent)
	}
	rationale := "The developer accepted the new Control's first report after leadfox control brief."
	if strings.TrimSpace(note) != "" {
		rationale = strings.TrimSpace(note)
	}
	stamp := now.Format(time.RFC3339)
	ledger.Decisions = append(ledger.Decisions, projectDecision{ID: id, Title: title, Action: "agent_assignment", Status: "approved", Rationale: rationale, DecidedBy: by, CreatedAt: stamp, UpdatedAt: stamp})
	if err := validateDecisionLedger(ledger); err != nil {
		return "", err
	}
	if err := writeDecisionLedger(root, ledger); err != nil {
		return "", err
	}
	return id, nil
}

type controlOptions struct {
	root, format, date, by, agent, note string
}

func parseControlOptions(args []string, allowed map[string]bool) (controlOptions, error) {
	options := controlOptions{format: "markdown"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			if options.root != "" {
				return options, errors.New("only one project path may be provided")
			}
			options.root = arg
			continue
		}
		name := strings.TrimPrefix(arg, "--")
		if !allowed[name] {
			return options, fmt.Errorf("unknown option %s", arg)
		}
		if i+1 >= len(args) {
			return options, fmt.Errorf("%s requires a value", arg)
		}
		value := args[i+1]
		i++
		switch name {
		case "format":
			if value != "markdown" && value != "json" {
				return options, errors.New("--format requires markdown or json")
			}
			options.format = value
		case "date":
			options.date = value
		case "by":
			options.by = value
		case "agent":
			options.agent = value
		case "note":
			options.note = value
		}
	}
	if options.root == "" {
		root, err := gitRoot(".")
		if err != nil {
			return options, err
		}
		options.root = root
	} else {
		root, err := filepath.Abs(options.root)
		if err != nil {
			return options, err
		}
		options.root = root
	}
	return options, nil
}

func runControlCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(controlHelpText)
		return nil
	}
	switch args[0] {
	case "brief":
		options, err := parseControlOptions(args[1:], map[string]bool{"format": true})
		if err != nil {
			return err
		}
		brief, err := buildControlBrief(options.root)
		if err != nil {
			return err
		}
		return printControlBrief(brief, options.format)
	case "handoff":
		if len(args) < 2 || args[1] != "draft" {
			return errors.New("use leadfox control handoff draft [PATH] [--date YYYY-MM-DD]")
		}
		options, err := parseControlOptions(args[2:], map[string]bool{"date": true})
		if err != nil {
			return err
		}
		path, err := draftControlHandoff(options.root, options.date)
		if err != nil {
			return err
		}
		fmt.Printf("Drafted %s; complete it, then commit it for the next Control.\n", path)
		return nil
	case "ack":
		options, err := parseControlOptions(args[1:], map[string]bool{"by": true, "agent": true, "note": true})
		if err != nil {
			return err
		}
		id, err := acknowledgeControl(options.root, options.by, options.agent, options.note)
		if err != nil {
			return err
		}
		fmt.Printf("Recorded decision %s (agent_assignment, approved by %s) in %s; commit it.\n", id, options.by, decisionsFileName)
		return nil
	}
	return fmt.Errorf("unknown control operation %q; use 'leadfox control --help' for usage", args[0])
}

const controlHelpText = `Onboard a Control agent.

Usage:
  leadfox control brief [PATH] [--format markdown|json]
  leadfox control handoff draft [PATH] [--date YYYY-MM-DD]
  leadfox control ack [PATH] --by NAME [--agent TEXT] [--note TEXT]

brief       Read-only takeover briefing: project health, a reading order,
            live jobs with next actions, active and held change plans, open
            decisions, recent intake history, owner conventions
            (control/conventions.yaml), the latest Control handoff, history
            as counts, and a fixed first task for the new Control.
handoff     Draft control/handoffs/CONTROL-<date>.md, pre-filled with live
            work, for the outgoing Control to complete. Never overwrites.
ack         Record the developer's acceptance of the new Control's first
            report as an approved agent_assignment decision.

The briefing and handoff are guidance and records; they grant no authority.
The decision policy still governs what Control may do.
`
