package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeBriefPacket(t *testing.T, root, id, status, changePlan string) {
	t.Helper()
	packet := "---\nid: " + id + "\nstatus: " + status + "\nrole: service\nobjective: Brief test job.\nbase_branch: main\n"
	if changePlan != "" {
		packet += "change_plan: " + changePlan + "\n"
	}
	packet += "allowed_paths:\n  - services/example/**\n---\n# " + id + "\n"
	writeTestFile(t, jobPath(root, id), packet)
}

func newBriefProject(t *testing.T) string {
	t.Helper()
	root := writeProjectFixture(t)
	gitTest(t, root, "init", "-b", "main")
	gitTest(t, root, "config", "user.email", "leadfox@example.test")
	gitTest(t, root, "config", "user.name", "Leadfox Test")

	held, active, settled := "control/changes/CHG-001-held.md", "control/changes/CHG-002-active.md", "control/changes/CHG-003-done.md"
	for _, path := range []string{held, active, settled} {
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(path)), "---\nstatus: approved\n---\n# Change\n")
	}
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(changeWorkflowPath(held))), "version: 1\nhold_points:\n  - id: HOLD-1\n    description: Owner review before phase B\n    status: open\n")

	writeBriefPacket(t, root, "JOB-001", stateMerged, settled)
	writeBriefPacket(t, root, "JOB-002", stateCancelled, "")
	writeBriefPacket(t, root, "JOB-003", stateReviewPending, active)
	writeBriefPacket(t, root, "JOB-004", stateDraft, held)
	writeBriefPacket(t, root, "JOB-005", stateReady, "")

	ledger := decisionFile{Version: 1, Decisions: []projectDecision{
		{ID: "open-one", Title: "Pick a queue", Action: "technology_choice", Status: "proposed", CreatedAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-01T00:00:00Z"},
		{ID: "later-one", Title: "Production roles", Action: "documentation_updates", Status: "deferred", CreatedAt: "2026-11-02T00:00:00Z", UpdatedAt: "2026-11-02T00:00:00Z"},
		{ID: "done-one", Title: "Use Postgres", Action: "technology_choice", Status: "approved", DecidedBy: "Owner", CreatedAt: "2026-11-03T00:00:00Z", UpdatedAt: "2026-11-03T00:00:00Z"},
	}}
	if err := writeDecisionLedger(root, ledger); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, conventionsPath), "version: 1\nconventions:\n  - id: naming\n    rule: Services are named after sled dogs.\n    source: owner, 2026-11-05\n")
	writeTestFile(t, filepath.Join(root, "control", "handoffs", "CONTROL-2026-11-01.md"), "# Old handoff\n")
	writeTestFile(t, filepath.Join(root, "control", "handoffs", "CONTROL-2026-11-10.md"), "# Latest handoff\n\nJOB-003 waits on the owner's manual check.\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "fixture")
	return root
}

func TestControlBriefShowsLiveWorkAndHistoryAsCounts(t *testing.T) {
	root := newBriefProject(t)
	brief, err := buildControlBrief(root)
	if err != nil {
		t.Fatal(err)
	}

	var live []string
	for _, job := range brief.Jobs {
		live = append(live, job.ID)
		if job.NextAction == "" || job.Meaning == "" {
			t.Fatalf("live job lacks meaning or next action: %+v", job)
		}
	}
	if strings.Join(live, ",") != "JOB-003,JOB-005" {
		t.Fatalf("live jobs = %v, want JOB-003 and JOB-005", live)
	}
	if strings.Join(brief.Drafts, ",") != "JOB-004" || brief.History.Jobs[stateMerged] != 1 || brief.History.Jobs[stateCancelled] != 1 {
		t.Fatalf("drafts %v or job history %v wrong", brief.Drafts, brief.History.Jobs)
	}

	states := map[string]briefChangePlan{}
	for _, plan := range brief.ChangePlans {
		states[filepath.Base(plan.Path)] = plan
	}
	if plan := states["CHG-001-held.md"]; plan.State != "held" || len(plan.OpenHolds) != 1 || !strings.Contains(plan.OpenHolds[0], "HOLD-1") {
		t.Fatalf("held change plan: %+v", plan)
	}
	if plan := states["CHG-002-active.md"]; plan.State != "active" || plan.Jobs[stateReviewPending] != 1 {
		t.Fatalf("active change plan: %+v", plan)
	}
	if _, listed := states["CHG-003-done.md"]; listed || brief.History.ChangePlans != 1 {
		t.Fatalf("settled change plan listed as live or not counted: %+v", brief.ChangePlans)
	}

	var open []string
	for _, decision := range brief.Decisions {
		open = append(open, decision.ID)
	}
	if strings.Join(open, ",") != "open-one,later-one" || brief.History.Decisions["approved"] != 1 {
		t.Fatalf("open decisions %v or history %v wrong", open, brief.History.Decisions)
	}
	if len(brief.Conventions) != 1 || brief.Conventions[0].Rule != "Services are named after sled dogs." {
		t.Fatalf("conventions not included verbatim: %+v", brief.Conventions)
	}
	if brief.ControlHandoff == nil || brief.ControlHandoff.Path != "control/handoffs/CONTROL-2026-11-10.md" || !strings.Contains(brief.ControlHandoff.Content, "manual check") {
		t.Fatalf("latest Control handoff not included: %+v", brief.ControlHandoff)
	}

	reading := map[string]bool{}
	for _, item := range brief.ReadingOrder {
		reading[item.Path] = true
	}
	for _, path := range []string{"AGENTS.md", agentRuntimeProtocolPath, conventionsPath, "control/changes/CHG-001-held.md", changeWorkflowPath("control/changes/CHG-001-held.md"), "control/changes/CHG-002-active.md", "control/handoffs/CONTROL-2026-11-10.md"} {
		if !reading[path] {
			t.Errorf("reading order lacks %s", path)
		}
	}
	if reading["control/changes/CHG-003-done.md"] {
		t.Error("reading order includes a settled change plan")
	}
	if !strings.Contains(brief.FirstTask, "Then stop and wait") || !strings.Contains(brief.FirstTask, "grants no authority") {
		t.Fatalf("first task missing: %s", brief.FirstTask)
	}
	if err := printControlBrief(brief, "markdown"); err != nil {
		t.Fatal(err)
	}
	if err := printControlBrief(brief, "json"); err != nil {
		t.Fatal(err)
	}

	// An invalid conventions file is a warning, not a failure.
	writeTestFile(t, filepath.Join(root, conventionsPath), "version: 1\nconventions:\n  - id: missing-rule\n")
	brief, err = buildControlBrief(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(brief.Warnings, "\n"), "requires id and rule") {
		t.Fatalf("invalid conventions not reported: %v", brief.Warnings)
	}
}

func TestControlHandoffDraftAndAck(t *testing.T) {
	root := newBriefProject(t)
	setClock(t, time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))

	path, err := draftControlHandoff(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if path != "control/handoffs/CONTROL-2026-11-20.md" {
		t.Fatalf("unexpected handoff path %s", path)
	}
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"## In-flight work", "- JOB-003 (review-pending):", "## Owner decisions not yet recorded", "## Known inconsistencies", conventionsPath} {
		if !strings.Contains(string(content), required) {
			t.Fatalf("handoff draft lacks %q:\n%s", required, content)
		}
	}
	if _, err := draftControlHandoff(root, ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("handoff draft overwrote an existing note: %v", err)
	}
	if _, err := draftControlHandoff(root, "20-11-2026"); err == nil {
		t.Fatal("invalid date accepted")
	}
	if brief, _ := buildControlBrief(root); brief.ControlHandoff == nil || brief.ControlHandoff.Path != path {
		t.Fatal("brief does not pick up the newest handoff")
	}

	if _, err := acknowledgeControl(root, "", "Claude", ""); err == nil {
		t.Fatal("ack accepted without --by")
	}
	first, err := acknowledgeControl(root, "Owner", "Claude Opus", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := acknowledgeControl(root, "Owner", "", "re-briefed after a restart")
	if err != nil {
		t.Fatal(err)
	}
	if first != "control-takeover-2026-11-20" || second != "control-takeover-2026-11-20-2" {
		t.Fatalf("unexpected decision IDs %s, %s", first, second)
	}
	ledger, err := loadDecisionLedger(root)
	if err != nil {
		t.Fatal(err)
	}
	decision, _ := findDecision(ledger.Decisions, first)
	if decision.Action != "agent_assignment" || decision.Status != "approved" || decision.DecidedBy != "Owner" || !strings.Contains(decision.Title, "Claude Opus") {
		t.Fatalf("takeover decision not recorded correctly: %+v", decision)
	}
	if again, _ := findDecision(ledger.Decisions, second); again.Rationale != "re-briefed after a restart" {
		t.Fatalf("note not recorded: %+v", again)
	}
}
