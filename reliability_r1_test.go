package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func ignoreLocalState(t *testing.T, root string) {
	t.Helper()
	writeTestFile(t, filepath.Join(root, ".gitignore"), ".local/\n")
	gitTest(t, root, "add", ".gitignore")
	gitTest(t, root, "commit", "-m", "ignore local state")
}

func sameDirectory(t *testing.T, a, b string) bool {
	t.Helper()
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func TestEveryLegacyAndNewStateIsInterpreted(t *testing.T) {
	legacy := []string{"draft", "ready", "running", "review", "review-pending", "review-failed", "changes-requested", "accepted", "merged", "blocked", "cancelled"}
	added := []string{"paused", "worker-lost", "timed-out"}
	for _, status := range append(legacy, added...) {
		if !knownJobStatus(status) {
			t.Fatalf("status %q is not in the state table", status)
		}
		packet := jobPacket{ID: "JOB-STATE", Status: status}
		report := buildJobStatus(t.TempDir(), packet)
		if report.Meaning == "" || report.Meaning == "unrecognized status" {
			t.Fatalf("status %q has no meaning", status)
		}
		if report.NextAction == "" || report.NextAction == "inspect the job and resolve its blockers" {
			t.Fatalf("status %q has no specific next action", status)
		}
	}
	// Every state that operations can move into is a known state.
	for operation, rule := range jobOperations {
		for _, from := range rule.From {
			if !knownJobStatus(from) {
				t.Fatalf("operation %s allows unknown source state %q", operation, from)
			}
		}
		if rule.To != "" && !knownJobStatus(rule.To) {
			t.Fatalf("operation %s produces unknown state %q", operation, rule.To)
		}
	}
	report := buildJobStatus(t.TempDir(), jobPacket{ID: "JOB-ODD", Status: "mystery"})
	if report.Meaning != "unrecognized status" {
		t.Fatalf("unknown status should be reported, got %q", report.Meaning)
	}
	if err := checkJobOperation("JOB-ODD", "start", "mystery"); err == nil || !strings.Contains(err.Error(), "unrecognized status") {
		t.Fatalf("unknown status should be refused clearly: %v", err)
	}
}

func TestTransitionTable(t *testing.T) {
	cases := []struct {
		operation, from string
		allowed         bool
	}{
		{"start", "ready", true},
		{"start", "draft", false},
		{"pause", "running", true},
		{"pause", "blocked", false},
		{"resume", "paused", true},
		{"resume", "blocked", true},
		{"resume", "worker-lost", true},
		{"resume", "timed-out", true},
		{"resume", "running", false},
		{"resume", "cancelled", false},
		{"cancel", "paused", true},
		{"cancel", "worker-lost", true},
		{"cancel", "cancelled", false},
		{"cancel", "merged", false},
		{"block", "draft", true},
		{"block", "merged", false},
		{"review", "running", true},
		{"review", "worker-lost", true},
		{"review", "cancelled", false},
		{"review", "merged", false},
		{"review", "ready", false},
		{"approve", "review-pending", true},
		{"approve", "running", false},
		{"merge", "accepted", true},
		{"merge", "review", false},
		{"sync", "paused", true},
		{"sync", "worker-lost", false},
		{"progress", "running", true},
		{"progress", "ready", false},
		{"progress", "cancelled", false},
		{"progress", "merged", false},
	}
	for _, c := range cases {
		err := checkJobOperation("JOB-T", c.operation, c.from)
		if (err == nil) != c.allowed {
			t.Errorf("%s from %s: allowed=%v, error=%v", c.operation, c.from, c.allowed, err)
		}
	}
}

func TestPauseResumeAndCancelFromPause(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-P")
	worktree := startTestJob(t, root, "JOB-P")
	if err := jobPause(root, "JOB-P", jobOptions{actor: "Control"}); err == nil {
		t.Fatal("pause without a reason was accepted")
	}
	if err := jobPause(root, "JOB-P", jobOptions{actor: "Control", reason: "owner review of approach"}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	paused := mustReadJob(t, root, "JOB-P")
	report := buildJobStatus(root, paused)
	if paused.Status != "paused" || !strings.Contains(report.Meaning, "deliberately paused") || !strings.Contains(report.Meaning, "owner review of approach") {
		t.Fatalf("pause not distinguishable in status: %s / %q", paused.Status, report.Meaning)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("pause removed the worktree: %v", err)
	}
	if err := jobPause(root, "JOB-P", jobOptions{actor: "Control", reason: "again"}); err == nil {
		t.Fatal("a paused job was paused again")
	}
	if err := jobResume(root, "JOB-P", jobOptions{actor: "Control", reason: "approach confirmed"}); err != nil {
		t.Fatalf("resume from pause: %v", err)
	}
	if status := mustReadJob(t, root, "JOB-P").Status; status != "running" {
		t.Fatalf("resume left status %s", status)
	}
	if err := jobPause(root, "JOB-P", jobOptions{actor: "Control", reason: "stop for today"}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(worktree, "services", "example", "wip.go"), "package example\n")
	if err := jobTerminate(root, "JOB-P", "cancelled", jobOptions{actor: "Control", reason: "no longer needed"}); err != nil {
		t.Fatalf("cancel from pause: %v", err)
	}
	cancelled := mustReadJob(t, root, "JOB-P")
	if cancelled.Status != "cancelled" || len(cancelled.RecoveryPoints) != 1 || !cancelled.RecoveryPoints[0].Snapshot {
		t.Fatalf("cancel from pause did not preserve work: %+v", cancelled)
	}
	var path []string
	for _, transition := range cancelled.Transitions {
		path = append(path, transition.To)
	}
	if strings.Join(path, ",") != "running,paused,running,paused,cancelled" {
		t.Fatalf("transition history incomplete: %v", path)
	}
}

func TestResumeInterruptedJobWithMissingWorktreePointsToRecovery(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-GONE")
	worktree := startTestJob(t, root, "JOB-GONE")
	packet, body, err := readJob(root, "JOB-GONE")
	if err != nil {
		t.Fatal(err)
	}
	// R2 records worker-lost through job check; simulate the record here.
	appendTransition(&packet, "worker-lost", "heartbeat stale", "Leadfox", "2026-11-16T00:00:00Z")
	packet.Status = "worker-lost"
	if err := writeJob(root, packet, body); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "commit", "-am", "record worker loss")
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}
	err = jobResume(root, "JOB-GONE", jobOptions{actor: "Control"})
	if err == nil || !strings.Contains(err.Error(), "--from-branch") || !strings.Contains(err.Error(), packet.Branch) {
		t.Fatalf("resume should explain recovery from the branch: %v", err)
	}
	report := buildJobStatus(root, mustReadJob(t, root, "JOB-GONE"))
	if !strings.Contains(report.Meaning, "without a handoff") || !strings.Contains(report.Meaning, "heartbeat stale") {
		t.Fatalf("worker loss not distinguishable in status: %q", report.Meaning)
	}
}

func TestReviewRefusedForCancelledJob(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-RC")
	startTestJob(t, root, "JOB-RC")
	if err := jobTerminate(root, "JOB-RC", "cancelled", jobOptions{actor: "Control", reason: "stop"}); err != nil {
		t.Fatal(err)
	}
	err := jobReview(root, "JOB-RC", jobOptions{commit: "HEAD", worktree: siblingPath(root, "review-rc")})
	if err == nil || !strings.Contains(err.Error(), "review requires one of") {
		t.Fatalf("a cancelled job was reviewed: %v", err)
	}
}

func TestProgressFromControlAndWorktree(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-PR")
	ignoreLocalState(t, root)
	worktree := startTestJob(t, root, "JOB-PR")
	before, err := os.ReadFile(jobPath(root, "JOB-PR"))
	if err != nil {
		t.Fatal(err)
	}

	located, err := controlRoot(worktree)
	if err != nil || !sameDirectory(t, located, root) {
		t.Fatalf("control root from the worktree resolved to %q: %v", located, err)
	}
	commit := commitInWorktree(t, worktree, "services/example/service.go", "package example\n", "first slice")
	if err := jobProgress(located, "JOB-PR", jobOptions{remaining: "handler tests", next: "write handler", gates: []string{"unit"}, actor: "service-agent"}); err != nil {
		t.Fatalf("progress from worktree: %v", err)
	}
	if err := jobProgress(root, "JOB-PR", jobOptions{blocker: "waiting for schema decision", actor: "Control"}); err != nil {
		t.Fatalf("progress from Control: %v", err)
	}

	events, malformed, err := readJournal(root, "JOB-PR")
	if err != nil || malformed != 0 || len(events) != 2 {
		t.Fatalf("journal read: %d events, %d malformed, %v", len(events), malformed, err)
	}
	first := events[0]
	if first.Commit != commit || first.Branch == "" || first.Worktree != worktree || first.Progress.Remaining != "handler tests" || first.Progress.Gates[0] != "unit" || first.EventID == "" || first.Schema != 1 {
		t.Fatalf("progress record incomplete: %+v", first)
	}
	if status := strings.TrimSpace(gitTest(t, root, "status", "--porcelain")); status != "" {
		t.Fatalf("progress left the Control checkout dirty (journal not ignored?): %s", status)
	}
	after, err := os.ReadFile(jobPath(root, "JOB-PR"))
	if err != nil || string(after) != string(before) {
		t.Fatal("progress changed the job packet; progress must not affect lifecycle records")
	}

	report := buildJobStatus(root, mustReadJob(t, root, "JOB-PR"))
	if report.Activity == nil || report.Activity.ProgressRecords != 2 || report.Activity.LastProgress.Blocker != "waiting for schema decision" || report.Activity.BranchHead != commit {
		t.Fatalf("status does not show latest progress: %+v", report.Activity)
	}
}

func TestRunningJobWithoutProgressIsDiagnosable(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-SILENT")
	ignoreLocalState(t, root)
	startTestJob(t, root, "JOB-SILENT")
	packet := mustReadJob(t, root, "JOB-SILENT")
	report := buildJobStatus(root, packet)
	if report.Activity == nil || !strings.Contains(report.Activity.Summary, "no progress recorded since start at") || !strings.Contains(report.Activity.Summary, "last branch commit") {
		t.Fatalf("silent running job is not diagnosable: %+v", report.Activity)
	}
	// A corrupted journal line is tolerated and reported.
	writeTestFile(t, journalPath(root, "JOB-SILENT"), "not json\n")
	report = buildJobStatus(root, packet)
	if report.Activity.MalformedLines != 1 {
		t.Fatalf("malformed journal line not reported: %+v", report.Activity)
	}
}

func TestProgressRefusals(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-REF")
	if err := jobProgress(root, "JOB-REF", jobOptions{next: "start"}); err == nil || !strings.Contains(err.Error(), "progress requires one of") {
		t.Fatalf("progress accepted for a job that has not started: %v", err)
	}
	startTestJob(t, root, "JOB-REF")
	if err := jobProgress(root, "JOB-REF", jobOptions{next: "x"}); err == nil || !strings.Contains(err.Error(), "not ignored by Git") {
		t.Fatalf("progress written to a journal Git would track: %v", err)
	}
	ignoreLocalState(t, root)
	if err := jobProgress(root, "JOB-REF", jobOptions{}); err == nil {
		t.Fatal("empty progress record accepted")
	}
	secrets := []string{
		"export TOKEN=abc123",
		"use Authorization: Bearer eyJhbGciOi",
		"password: hunter2",
		"key ghp_abcdefghijklmnopqrstuvwxyz0123",
		"AKIAABCDEFGHIJKLMNOP leaked",
		"-----BEGIN RSA PRIVATE KEY-----",
		"api_key=sk-live",
	}
	for _, value := range secrets {
		if err := jobProgress(root, "JOB-REF", jobOptions{remaining: value}); err == nil || !strings.Contains(err.Error(), "credential") {
			t.Fatalf("secret-like progress %q accepted: %v", value, err)
		}
	}
	if err := jobProgress(root, "JOB-REF", jobOptions{remaining: strings.Repeat("x", maxJournalTextLength+1)}); err == nil {
		t.Fatal("unbounded progress text accepted")
	}
	if err := jobProgress(root, "JOB-REF", jobOptions{gates: []string{"unit; rm -rf /"}}); err == nil {
		t.Fatal("invalid gate identifier accepted")
	}
	if events, _, _ := readJournal(root, "JOB-REF"); len(events) != 0 {
		t.Fatalf("refused progress was written: %+v", events)
	}
	// Ordinary engineering text is not mistaken for a secret.
	for _, value := range []string{"rotate the token cache tests", "password reset flow done", "secret scanning gate passes"} {
		if err := jobProgress(root, "JOB-REF", jobOptions{remaining: value}); err != nil {
			t.Fatalf("ordinary text %q rejected: %v", value, err)
		}
	}
}

func TestProgressOptionsAreBoundToProgress(t *testing.T) {
	for _, args := range [][]string{{"JOB-1", "--remaining", "x"}, {"JOB-1", "--show"}} {
		options, _, err := parseJobOptions(args)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateJobOptions("start", options); err == nil {
			t.Fatalf("job start accepted %v", args)
		}
		if err := validateJobOptions("progress", options); err != nil {
			t.Fatalf("job progress rejected %v: %v", args, err)
		}
	}
	operations := make([]string, 0, len(jobOperations))
	for operation := range jobOperations {
		operations = append(operations, operation)
	}
	sort.Strings(operations)
	if strings.Join(operations, ",") != "approve,attest,block,cancel,evidence,merge,pause,progress,resume,review,start,sync" {
		t.Fatalf("lifecycle operations changed without updating tests: %v", operations)
	}
}
