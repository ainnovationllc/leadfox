package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestInitWritesRuntimeProtocolAndReferencesIt(t *testing.T) {
	files := projectFiles("example")
	if files[agentRuntimeProtocolPath] != agentRuntimeProtocolDoc || !strings.Contains(agentRuntimeProtocolDoc, "# Job runtime protocol") {
		t.Fatal("init does not write the runtime protocol document")
	}
	agents := files["AGENTS.md"]
	for _, required := range []string{agentRuntimeProtocolPath, "leadfox job check --all", "pause or block"} {
		if !strings.Contains(agents, required) {
			t.Fatalf("AGENTS.md does not mention %q", required)
		}
	}
	if !strings.Contains(files["docs/agent-roles.md"], agentRuntimeProtocolPath) {
		t.Fatal("agent roles document does not reference the runtime protocol")
	}
}

// TestRuntimeProtocolCommandsMatchTheCLI keeps the instructions agents
// follow consistent with the commands and options Leadfox accepts.
func TestRuntimeProtocolCommandsMatchTheCLI(t *testing.T) {
	commandPattern := regexp.MustCompile(`leadfox job ([a-z]+)((?: [^` + "`" + `\n]*)?)`)
	sources := map[string]string{
		"protocol":       agentRuntimeProtocolDoc,
		"packet section": jobRuntimeReportingSection("JOB-1"),
		"AGENTS.md":      projectFiles("example")["AGENTS.md"],
		"context":        strings.Join(jobReportingCommands("JOB-1"), "\n"),
	}
	found := 0
	for name, text := range sources {
		for _, match := range commandPattern.FindAllStringSubmatch(text, -1) {
			operation := match[1]
			if err := validateJobOptions(operation, jobOptions{used: map[string]bool{}}); err != nil {
				t.Errorf("%s references unknown command leadfox job %s", name, match[1])
				continue
			}
			for _, flag := range regexp.MustCompile(`--[a-z-]+`).FindAllString(match[2], -1) {
				option := strings.TrimPrefix(flag, "--")
				o := jobOptions{used: map[string]bool{option: true}}
				if err := validateJobOptions(operation, o); err != nil {
					t.Errorf("%s: leadfox job %s does not accept %s (%v)", name, match[1], flag, err)
				}
			}
			found++
		}
	}
	if found < 15 {
		t.Fatalf("expected the instructions to reference many commands, found %d", found)
	}
}

func TestNewJobPacketAndContextCarryReportingCommands(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-SEED")
	if err := jobNew(root, "JOB-NEW", jobOptions{role: "service", objective: "Add a handler.", base: "main"}); err != nil {
		t.Fatal(err)
	}
	_, body, err := readJob(root, "JOB-NEW")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"## Runtime reporting", "leadfox job runtime register JOB-NEW --runtime HOST", "leadfox job runtime event JOB-NEW --type handoff", agentRuntimeProtocolPath} {
		if !strings.Contains(body, required) {
			t.Fatalf("new job packet is missing %q:\n%s", required, body)
		}
	}
	context := jobContextFromPacket(mustReadJob(t, root, "JOB-NEW"))
	if context.Protocol != agentRuntimeProtocolPath || len(context.Reporting) == 0 || !strings.Contains(context.Reporting[0], "JOB-NEW") {
		t.Fatalf("job context lacks reporting commands: %+v", context)
	}
}

func TestProgressWithExecutionCountsAsHeartbeat(t *testing.T) {
	t0 := time.Now().UTC()
	root, _ := newRuntimeJob(t, "JOB-BEAT", t0)
	setClock(t, t0.Add(50*time.Minute))
	if err := jobProgress(root, "JOB-BEAT", jobOptions{next: "write tests", executionID: "exec-1"}); err != nil {
		t.Fatal(err)
	}
	// Progress events take their timestamp from the wall clock; record the
	// liveness check relative to it.
	events := mustJournal(t, root, "JOB-BEAT")
	last := events[len(events)-1]
	if last.Type != "progress" || last.ExecutionID != "exec-1" || last.Seq != 1 {
		t.Fatalf("progress not attributed to the execution: %+v", last)
	}
	at, _ := time.Parse(time.RFC3339, last.At)
	state := deriveRuntimeState(events, at.Add(20*time.Minute), defaultRuntimeThresholds)
	if state.LastEventType != "progress" || state.Condition != conditionStale {
		t.Fatalf("progress should reset liveness: %+v", state)
	}
	if state := deriveRuntimeState(events, at.Add(5*time.Minute), defaultRuntimeThresholds); state.Condition != conditionHealthy {
		t.Fatalf("recent progress should keep the worker healthy: %+v", state)
	}
	// Progress without an execution does not count as liveness.
	if err := jobProgress(root, "JOB-BEAT", jobOptions{next: "manual note"}); err != nil {
		t.Fatal(err)
	}
	if state := deriveRuntimeState(mustJournal(t, root, "JOB-BEAT"), at.Add(5*time.Minute), defaultRuntimeThresholds); state.LastEventType != "progress" || state.LastSeq != 1 {
		t.Fatalf("unattributed progress changed runtime state: %+v", state)
	}
	if err := jobProgress(root, "JOB-BEAT", jobOptions{next: "x", executionID: "exec-404"}); err == nil {
		t.Fatal("progress accepted for an unregistered explicit execution")
	}
}

func TestExecutionIDFromEnvironment(t *testing.T) {
	root, _ := newRuntimeJob(t, "JOB-ENV", time.Now().UTC())
	t.Setenv(executionIDEnv, "exec-1")
	runtimeOK(t, root, "heartbeat", "JOB-ENV", jobOptions{})
	if err := jobProgress(root, "JOB-ENV", jobOptions{next: "continue"}); err != nil {
		t.Fatal(err)
	}
	events := mustJournal(t, root, "JOB-ENV")
	if heartbeat, progress := events[len(events)-2], events[len(events)-1]; heartbeat.ExecutionID != "exec-1" || progress.ExecutionID != "exec-1" || progress.Seq <= heartbeat.Seq {
		t.Fatalf("environment execution ID not applied in order: %+v %+v", heartbeat, progress)
	}

	// A stale value from another job does not block progress, but runtime
	// events still require a registered execution.
	t.Setenv(executionIDEnv, "exec-other-job")
	if err := jobProgress(root, "JOB-ENV", jobOptions{next: "continue"}); err != nil {
		t.Fatalf("progress blocked by a stale environment value: %v", err)
	}
	events = mustJournal(t, root, "JOB-ENV")
	if last := events[len(events)-1]; last.ExecutionID != "" {
		t.Fatalf("stale environment value attached to progress: %+v", last)
	}
	if err := jobRuntimeCommand(root, "heartbeat", "JOB-ENV", jobOptions{used: map[string]bool{}}); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("heartbeat accepted a stale environment execution: %v", err)
	}
}

func TestMigrateAddsRuntimeProtocolAndCheckWarns(t *testing.T) {
	root := writeProjectFixture(t)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(agentRuntimeProtocolPath))); err != nil {
		t.Fatal(err)
	}
	legacyAgents := "# Agent instructions\n\nThis is a Leadfox-managed project.\n"
	writeTestFile(t, filepath.Join(root, "AGENTS.md"), legacyAgents)

	report, err := inspectProjectSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	if !containsProjectString(report.MissingManaged, agentRuntimeProtocolPath) || report.Status != "migration-required" {
		t.Fatalf("migration does not offer the runtime protocol: %+v", report)
	}
	check, err := checkProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(check.Warnings, "\n"), "AGENTS.md does not reference "+agentRuntimeProtocolPath) {
		t.Fatalf("project check does not warn about old agent instructions: %v", check.Warnings)
	}
	if strings.Contains(strings.Join(check.Invalid, "\n"), "AGENTS.md does not reference") {
		t.Fatal("old agent instructions must be a warning, not an invalid project")
	}

	created, _, err := applyProjectMigration(root, report)
	if err != nil || !containsProjectString(created, agentRuntimeProtocolPath) {
		t.Fatalf("migration did not create the protocol: %v %v", created, err)
	}
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(agentRuntimeProtocolPath)))
	if err != nil || string(content) != agentRuntimeProtocolDoc {
		t.Fatalf("migrated protocol differs from the built-in document: %v", err)
	}
	if agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); string(agents) != legacyAgents {
		t.Fatal("migration modified the project-owned AGENTS.md")
	}
	writeTestFile(t, filepath.Join(root, "AGENTS.md"), legacyAgents+"\nFollow docs/agent-runtime.md.\n")
	if check, _ := checkProject(root); strings.Contains(strings.Join(check.Warnings, "\n"), "AGENTS.md does not reference") {
		t.Fatalf("warning persists after AGENTS.md references the protocol: %v", check.Warnings)
	}
}
