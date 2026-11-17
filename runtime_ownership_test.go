package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestReplacedOrClosedExecutionIsToldToStop covers a worker that returns
// after Control recorded it lost: its reports are refused with an
// instruction to stop, except exit and disconnect, which are history.
func TestReplacedOrClosedExecutionIsToldToStop(t *testing.T) {
	t0 := time.Now().UTC()
	root, _ := newRuntimeJob(t, "JOB-ZOMBIE", t0)
	setClock(t, t0.Add(61*time.Minute))
	if err := jobCheck(root, []string{"JOB-ZOMBIE"}, false); err != nil {
		t.Fatal(err)
	}
	if status := mustReadJob(t, root, "JOB-ZOMBIE").Status; status != stateWorkerLost {
		t.Fatalf("expected worker-lost, got %s", status)
	}

	// Closed by the recorded interruption.
	err := jobRuntimeCommand(root, "heartbeat", "JOB-ZOMBIE", jobOptions{executionID: "exec-1", used: map[string]bool{}})
	if err == nil || !strings.Contains(err.Error(), "was closed when Control recorded worker-lost") || !strings.Contains(err.Error(), "stop working on this job") {
		t.Fatalf("closed execution heartbeat not refused with a stop instruction: %v", err)
	}
	if err := jobProgress(root, "JOB-ZOMBIE", jobOptions{next: "keep going", executionID: "exec-1"}); err == nil || !strings.Contains(err.Error(), "stop working") {
		t.Fatalf("closed execution progress not refused: %v", err)
	}
	t.Setenv(executionIDEnv, "exec-1")
	if err := jobProgress(root, "JOB-ZOMBIE", jobOptions{next: "keep going"}); err == nil || !strings.Contains(err.Error(), "stop working") {
		t.Fatalf("closed execution from the environment not refused: %v", err)
	}
	t.Setenv(executionIDEnv, "")

	// Replaced by a new execution after resume.
	setClock(t, time.Now().UTC())
	if err := jobResume(root, "JOB-ZOMBIE", jobOptions{actor: "Control", reason: "continue"}); err != nil {
		t.Fatal(err)
	}
	runtimeOK(t, root, "register", "JOB-ZOMBIE", jobOptions{runtime: "local", executionID: "exec-2"})
	err = jobRuntimeCommand(root, "event", "JOB-ZOMBIE", jobOptions{executionID: "exec-1", eventType: "handoff", used: map[string]bool{}})
	if err == nil || !strings.Contains(err.Error(), "was replaced by execution exec-2") {
		t.Fatalf("replaced execution handoff not refused: %v", err)
	}
	// A host may still report that the old process ended.
	runtimeOK(t, root, "event", "JOB-ZOMBIE", exitOptions(1))
	runtimeOK(t, root, "heartbeat", "JOB-ZOMBIE", jobOptions{executionID: "exec-2"})

	state := deriveRuntimeState(mustJournal(t, root, "JOB-ZOMBIE"), nowFunc(), defaultRuntimeThresholds)
	if state.ExecutionID != "exec-2" || state.Condition != conditionHealthy || state.FromReplaced != 1 || state.Late != 0 {
		t.Fatalf("replaced execution events not counted separately: %+v", state)
	}
	if text := describeRuntime(state); !strings.Contains(text, "1 event(s) from replaced executions ignored") || strings.Contains(text, "late") {
		t.Fatalf("runtime description mislabels replaced events: %s", text)
	}
	if standing, _ := executionStanding(mustReadJob(t, root, "JOB-ZOMBIE"), mustJournal(t, root, "JOB-ZOMBIE"), "exec-2"); standing != "current" {
		t.Fatalf("new execution is %s, want current", standing)
	}
}

func TestStatusGuidanceForStaleWorkerAndMissingWorktree(t *testing.T) {
	t0 := time.Now().UTC()
	root, worktree := newRuntimeJob(t, "JOB-GUIDE", t0)

	setClock(t, t0.Add(20*time.Minute))
	report := buildJobStatus(root, mustReadJob(t, root, "JOB-GUIDE"))
	if !strings.Contains(report.NextAction, "check on the worker") || !strings.Contains(report.NextAction, "job check JOB-GUIDE") {
		t.Fatalf("stale worker next action: %s", report.NextAction)
	}
	setClock(t, t0.Add(61*time.Minute))
	report = buildJobStatus(root, mustReadJob(t, root, "JOB-GUIDE"))
	if !strings.Contains(report.NextAction, "record the interruption with leadfox job check JOB-GUIDE") {
		t.Fatalf("presumed-lost next action: %s", report.NextAction)
	}

	setClock(t, t0.Add(time.Minute))
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}
	report = buildJobStatus(root, mustReadJob(t, root, "JOB-GUIDE"))
	if !strings.Contains(report.NextAction, "pause the job") || !strings.Contains(report.NextAction, "leadfox job recover JOB-GUIDE") {
		t.Fatalf("running job with missing worktree: %s", report.NextAction)
	}
	if err := jobPause(root, "JOB-GUIDE", jobOptions{actor: "Control", reason: "host restarted"}); err != nil {
		t.Fatal(err)
	}
	report = buildJobStatus(root, mustReadJob(t, root, "JOB-GUIDE"))
	if strings.Contains(report.Meaning, "worktree kept") || !strings.Contains(report.Meaning, "worktree missing") {
		t.Fatalf("paused job meaning contradicts the missing worktree: %s", report.Meaning)
	}
	if !strings.HasPrefix(report.NextAction, "the worktree is missing; restore it with leadfox job recover JOB-GUIDE") {
		t.Fatalf("paused job with missing worktree: %s", report.NextAction)
	}
}
