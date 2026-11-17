package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordingNotifier struct{ notes *[]jobNotification }

func (r recordingNotifier) Notify(root string, notification jobNotification) error {
	*r.notes = append(*r.notes, notification)
	return localNotifier{}.Notify(root, notification)
}

func captureNotifications(t *testing.T) *[]jobNotification {
	t.Helper()
	notes := &[]jobNotification{}
	previous := activeNotifier
	activeNotifier = recordingNotifier{notes: notes}
	t.Cleanup(func() { activeNotifier = previous })
	return notes
}

func setClock(t *testing.T, at time.Time) {
	t.Helper()
	previous := nowFunc
	nowFunc = func() time.Time { return at.UTC() }
	t.Cleanup(func() { nowFunc = previous })
}

func notificationsFor(notes []jobNotification, event string) []jobNotification {
	var matched []jobNotification
	for _, note := range notes {
		if note.Event == event {
			matched = append(matched, note)
		}
	}
	return matched
}

// newRuntimeJob creates a project that ignores .local/, starts JOB-ID and
// registers execution exec-1 at the given time.
func newRuntimeJob(t *testing.T, id string, at time.Time) (string, string) {
	t.Helper()
	root := newReliabilityRepo(t, id)
	ignoreLocalState(t, root)
	worktree := startTestJob(t, root, id)
	setClock(t, at)
	runtimeOK(t, root, "register", id, jobOptions{runtime: "local", executionID: "exec-1"})
	return root, worktree
}

func runtimeOK(t *testing.T, root, action, id string, o jobOptions) {
	t.Helper()
	if o.used == nil {
		o.used = map[string]bool{}
	}
	if err := jobRuntimeCommand(root, action, id, o); err != nil {
		t.Fatalf("runtime %s: %v", action, err)
	}
}

func exitOptions(code int) jobOptions {
	return jobOptions{executionID: "exec-1", eventType: "exit", exitCode: code, used: map[string]bool{"exit-code": true}}
}

func TestRuntimeEventsIdempotentAndOrdered(t *testing.T) {
	t0 := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	at := func(minutes int) string { return t0.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339) }
	events := []runtimeEvent{
		{EventID: "e1", Job: "J", Type: "register", ExecutionID: "a", Runtime: "local", Seq: 1, At: at(0)},
		{EventID: "e2", Job: "J", Type: "heartbeat", ExecutionID: "a", Seq: 3, At: at(10), Commit: "c3"},
		{EventID: "e3", Job: "J", Type: "heartbeat", ExecutionID: "a", Seq: 2, At: at(11), Commit: "c2"}, // delayed: lower seq arrives later
		{EventID: "e2", Job: "J", Type: "heartbeat", ExecutionID: "a", Seq: 9, At: at(12)},               // duplicate ID
		{EventID: "e4", Job: "J", Type: "exit", ExecutionID: "zzz", Seq: 5, At: at(12)},                  // unregistered execution
		{EventID: "e5", Job: "J", Type: "progress", At: at(13)},
	}
	thresholds := defaultRuntimeThresholds
	state := deriveRuntimeState(events, t0.Add(20*time.Minute), thresholds)
	if state.ExecutionID != "a" || state.LastSeq != 3 || state.LastCommit != "c3" || state.Late != 1 || state.Duplicates != 1 || state.Ignored != 1 {
		t.Fatalf("ordering rules not applied: %+v", state)
	}
	if state.Condition != conditionHealthy {
		t.Fatalf("10 minutes of silence should be healthy, got %s", state.Condition)
	}
	if got := deriveRuntimeState(events, t0.Add(26*time.Minute), thresholds).Condition; got != conditionStale {
		t.Fatalf("16 minutes of silence should be stale, got %s", got)
	}
	if got := deriveRuntimeState(events, t0.Add(71*time.Minute), thresholds).Condition; got != conditionPresumedLost {
		t.Fatalf("61 minutes of silence should be presumed lost, got %s", got)
	}

	// A new registration supersedes the old execution; its later events,
	// including an exit, cannot change the state.
	events = append(events,
		runtimeEvent{EventID: "e6", Job: "J", Type: "register", ExecutionID: "b", Runtime: "codex", Seq: 1, At: at(30)},
		runtimeEvent{EventID: "e7", Job: "J", Type: "exit", ExecutionID: "a", Seq: 10, At: at(31)},
		runtimeEvent{EventID: "e8", Job: "J", Type: "register", ExecutionID: "a", Seq: 11, At: at(32)},
	)
	state = deriveRuntimeState(events, t0.Add(35*time.Minute), thresholds)
	if state.ExecutionID != "b" || state.Runtime != "codex" || state.Condition != conditionHealthy || len(state.Superseded) != 1 || state.Late != 3 {
		t.Fatalf("superseded execution changed state: %+v", state)
	}
	// Deterministic: the same events give the same result.
	if again := deriveRuntimeState(events, t0.Add(35*time.Minute), thresholds); again.Condition != state.Condition || again.LastSeq != state.LastSeq {
		t.Fatal("derivation is not deterministic")
	}
}

func TestDuplicateEventIDIsIgnoredOnAppend(t *testing.T) {
	root, _ := newRuntimeJob(t, "JOB-DUP", time.Now().UTC())
	for range 2 {
		runtimeOK(t, root, "heartbeat", "JOB-DUP", jobOptions{executionID: "exec-1", eventID: "hb-7", seq: 7})
	}
	events, _, _ := readJournal(root, "JOB-DUP")
	count := 0
	for _, event := range events {
		if event.EventID == "hb-7" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate event was appended %d times", count)
	}
	runtimeOK(t, root, "heartbeat", "JOB-DUP", jobOptions{executionID: "exec-1", seq: 3})
	state := deriveRuntimeState(mustJournal(t, root, "JOB-DUP"), nowFunc(), defaultRuntimeThresholds)
	if state.LastSeq != 7 || state.Late != 1 {
		t.Fatalf("delayed lower sequence changed state: %+v", state)
	}
}

func mustJournal(t *testing.T, root, id string) []runtimeEvent {
	t.Helper()
	events, _, err := readJournal(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestWorkerExitBeforeCommitIsRecoverable(t *testing.T) {
	notes := captureNotifications(t)
	root, worktree := newRuntimeJob(t, "JOB-EXIT", time.Now().UTC())
	startCommit := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD"))
	runtimeOK(t, root, "event", "JOB-EXIT", exitOptions(1))

	report := buildJobStatus(root, mustReadJob(t, root, "JOB-EXIT"))
	if report.Status != "running" || !strings.Contains(strings.Join(report.Warnings, " "), "run leadfox job check to record worker-lost") {
		t.Fatalf("status does not flag the lost worker before check: %+v", report.Warnings)
	}
	if err := jobCheck(root, []string{"JOB-EXIT"}, false); err != nil {
		t.Fatalf("job check: %v", err)
	}
	packet := mustReadJob(t, root, "JOB-EXIT")
	if packet.Status != "worker-lost" || len(packet.Interruptions) != 1 {
		t.Fatalf("worker exit not recorded: %+v", packet)
	}
	interruption := packet.Interruptions[0]
	if interruption.ExecutionID != "exec-1" || interruption.Runtime != "local" || !strings.Contains(interruption.Reason, "exited (code 1)") || interruption.LastCommit != startCommit || !strings.Contains(interruption.RecoveryAction, "--from-branch") {
		t.Fatalf("interruption record incomplete: %+v", interruption)
	}
	lost := notificationsFor(*notes, "worker-lost")
	if len(lost) != 1 || !lost[0].AttentionRequired || lost[0].State != "worker-lost" || lost[0].Commit != startCommit || lost[0].Project == "" || lost[0].RecoveryAction == "" {
		t.Fatalf("worker-lost notification incomplete: %+v", lost)
	}
	status := buildJobStatus(root, packet)
	if status.Interruption == nil || status.NextAction != interruption.RecoveryAction {
		t.Fatalf("status does not show the recovery instruction: %+v", status)
	}
	if !refExists(t, root, "refs/heads/"+packet.Branch) {
		t.Fatal("branch lost after worker exit")
	}
	if status.Runtime == nil || status.Runtime.Condition != conditionClosed || !strings.Contains(describeRuntime(*status.Runtime), "closed by a recorded interruption") {
		t.Fatalf("closed execution not described: %+v", status.Runtime)
	}

	// Resuming closes the lost execution; job check does not mark the job
	// lost again from its old events.
	if err := jobResume(root, "JOB-EXIT", jobOptions{actor: "Control", reason: "new worker"}); err != nil {
		t.Fatalf("resume after worker loss: %v", err)
	}
	if err := jobCheck(root, []string{"JOB-EXIT"}, false); err != nil {
		t.Fatal(err)
	}
	if status := mustReadJob(t, root, "JOB-EXIT").Status; status != "running" {
		t.Fatalf("resumed job was re-marked %s by its closed execution", status)
	}
}

func TestWorkerExitAfterPartialCommitsPreservesBranch(t *testing.T) {
	root, worktree := newRuntimeJob(t, "JOB-PART", time.Now().UTC())
	partial := commitInWorktree(t, worktree, "services/example/service.go", "package example\n", "partial work")
	runtimeOK(t, root, "heartbeat", "JOB-PART", jobOptions{executionID: "exec-1"})
	runtimeOK(t, root, "event", "JOB-PART", exitOptions(0))
	if err := jobCheck(root, nil, true); err != nil {
		t.Fatal(err)
	}
	packet := mustReadJob(t, root, "JOB-PART")
	if packet.Status != "worker-lost" || packet.Interruptions[0].LastCommit != partial {
		t.Fatalf("successful exit without handoff must still be worker-lost with the last commit: %+v", packet.Interruptions)
	}
	if err := jobReissue(root, "JOB-PART", "JOB-PART2", jobOptions{actor: "Control", reason: "continue", fromBranch: true}); err != nil {
		t.Fatalf("reissue after worker loss: %v", err)
	}
	if start := mustReadJob(t, root, "JOB-PART2").StartPoint; start == nil || start.Commit != partial {
		t.Fatalf("reissue did not continue from the partial work: %+v", start)
	}
	if len(mustReadJob(t, root, "JOB-PART2").Interruptions) != 0 {
		t.Fatal("reissued job inherited interruptions")
	}
}

func TestStaleHeartbeatMarksWorkerLost(t *testing.T) {
	notes := captureNotifications(t)
	t0 := time.Now().UTC()
	root, _ := newRuntimeJob(t, "JOB-STALE", t0)

	setClock(t, t0.Add(20*time.Minute))
	report := buildJobStatus(root, mustReadJob(t, root, "JOB-STALE"))
	if report.Runtime == nil || report.Runtime.Condition != conditionStale || !strings.Contains(strings.Join(report.Warnings, " "), "silent for") {
		t.Fatalf("stale worker not reported by status: %+v %+v", report.Runtime, report.Warnings)
	}
	for range 2 {
		if err := jobCheck(root, []string{"JOB-STALE"}, false); err != nil {
			t.Fatal(err)
		}
	}
	if stale := notificationsFor(*notes, "stale-progress"); len(stale) != 1 || !stale[0].AttentionRequired {
		t.Fatalf("stale notice should be emitted exactly once: %+v", stale)
	}
	if status := mustReadJob(t, root, "JOB-STALE").Status; status != "running" {
		t.Fatalf("stale is a warning, not a transition; got %s", status)
	}

	setClock(t, t0.Add(61*time.Minute))
	if err := jobCheck(root, []string{"JOB-STALE"}, false); err != nil {
		t.Fatal(err)
	}
	packet := mustReadJob(t, root, "JOB-STALE")
	if packet.Status != "worker-lost" || !strings.Contains(packet.Interruptions[0].Reason, "worker_lost_after 1h0m0s") {
		t.Fatalf("silent worker not marked lost: %+v", packet)
	}
}

func TestRunningJobWithoutHeartbeatIsReportedStale(t *testing.T) {
	// A session that ends without any runtime event leaves only the
	// registration; silence alone is enough to detect it.
	t0 := time.Now().UTC()
	root, _ := newRuntimeJob(t, "JOB-END", t0)
	setClock(t, t0.Add(2*time.Hour))
	report := buildJobStatus(root, mustReadJob(t, root, "JOB-END"))
	if report.Runtime.Condition != conditionPresumedLost {
		t.Fatalf("ended session not detected: %+v", report.Runtime)
	}
	if err := jobCheck(root, []string{"JOB-END"}, false); err != nil {
		t.Fatal(err)
	}
	if status := mustReadJob(t, root, "JOB-END").Status; status != "worker-lost" {
		t.Fatalf("ended session left status %s", status)
	}
}

func TestHandoffEmitsCompletionEvent(t *testing.T) {
	notes := captureNotifications(t)
	root, _ := newRuntimeJob(t, "JOB-DONE", time.Now().UTC())
	runtimeOK(t, root, "event", "JOB-DONE", jobOptions{executionID: "exec-1", eventType: "handoff"})
	runtimeOK(t, root, "event", "JOB-DONE", exitOptions(0))
	setClock(t, nowFunc().Add(3*time.Hour))
	for range 2 {
		if err := jobCheck(root, []string{"JOB-DONE"}, false); err != nil {
			t.Fatal(err)
		}
	}
	if handoff := notificationsFor(*notes, "handoff"); len(handoff) != 1 || handoff[0].AttentionRequired {
		t.Fatalf("handoff notice should be emitted once: %+v", handoff)
	}
	if status := mustReadJob(t, root, "JOB-DONE").Status; status != "running" {
		t.Fatalf("handoff must not change state or imply approval; got %s", status)
	}
}

func TestDisconnectMarksWorkerLost(t *testing.T) {
	root, _ := newRuntimeJob(t, "JOB-DISC", time.Now().UTC())
	runtimeOK(t, root, "event", "JOB-DISC", jobOptions{executionID: "exec-1", eventType: "disconnect"})
	if err := jobCheck(root, []string{"JOB-DISC"}, false); err != nil {
		t.Fatal(err)
	}
	if packet := mustReadJob(t, root, "JOB-DISC"); packet.Status != "worker-lost" || !strings.Contains(packet.Interruptions[0].Reason, "disconnected") {
		t.Fatalf("disconnect not recorded: %+v", packet)
	}
}

func TestTimeoutMarksTimedOut(t *testing.T) {
	notes := captureNotifications(t)
	root := newReliabilityRepo(t, "JOB-SLOW")
	ignoreLocalState(t, root)
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(runtimeSettingsPath)), "schema_version: 1\njob_timeout: 1h\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "runtime settings")
	startTestJob(t, root, "JOB-SLOW")
	setClock(t, time.Now().UTC().Add(90*time.Minute))
	if err := jobCheck(root, []string{"JOB-SLOW"}, false); err != nil {
		t.Fatal(err)
	}
	packet := mustReadJob(t, root, "JOB-SLOW")
	if packet.Status != "timed-out" || !strings.Contains(packet.Interruptions[0].Reason, "exceeding the 1h0m0s timeout") {
		t.Fatalf("timeout not recorded: %+v", packet)
	}
	if len(notificationsFor(*notes, "timed-out")) != 1 {
		t.Fatalf("timeout notification missing: %+v", *notes)
	}
	if err := jobResume(root, "JOB-SLOW", jobOptions{actor: "Control", reason: "extend"}); err != nil {
		t.Fatalf("resume after timeout: %v", err)
	}
	// The timeout restarts from the resume.
	setClock(t, time.Now().UTC().Add(time.Minute))
	if err := jobCheck(root, []string{"JOB-SLOW"}, false); err != nil {
		t.Fatal(err)
	}
	if status := mustReadJob(t, root, "JOB-SLOW").Status; status != "running" {
		t.Fatalf("timeout did not restart after resume: %s", status)
	}
}

func TestManualJobNeverAutoMarkedLost(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-MAN")
	ignoreLocalState(t, root)
	startTestJob(t, root, "JOB-MAN")
	setClock(t, time.Now().UTC().Add(48*time.Hour))
	if err := jobCheck(root, nil, true); err != nil {
		t.Fatal(err)
	}
	packet := mustReadJob(t, root, "JOB-MAN")
	if packet.Status != "running" {
		t.Fatalf("manual job was marked %s without runtime evidence", packet.Status)
	}
	if report := buildJobStatus(root, packet); report.Runtime.Condition != conditionNoRuntime {
		t.Fatalf("manual job should report no runtime: %+v", report.Runtime)
	}
}

func TestRuntimeEventValidation(t *testing.T) {
	root, _ := newRuntimeJob(t, "JOB-VAL", time.Now().UTC())
	cases := []struct {
		name   string
		action string
		o      jobOptions
		want   string
	}{
		{"unregistered", "heartbeat", jobOptions{executionID: "exec-9"}, "not registered"},
		{"missing execution", "heartbeat", jobOptions{}, "--execution-id (or LEADFOX_EXECUTION_ID) is required"},
		{"exit without code", "event", jobOptions{executionID: "exec-1", eventType: "exit"}, "--exit-code is required"},
		{"bad type", "event", jobOptions{executionID: "exec-1", eventType: "approve"}, "--type must be"},
		{"output text", "event", jobOptions{executionID: "exec-1", eventType: "output-ref", outputRef: "line one\nline two"}, "single path or URL"},
		{"secret note", "heartbeat", jobOptions{executionID: "exec-1", note: "TOKEN=abc123"}, "credential"},
		{"register twice", "register", jobOptions{runtime: "local", executionID: "exec-1"}, "already registered"},
		{"register without runtime", "register", jobOptions{executionID: "exec-2"}, "--runtime"},
	}
	before := len(mustJournal(t, root, "JOB-VAL"))
	for _, c := range cases {
		if c.o.used == nil {
			c.o.used = map[string]bool{}
		}
		err := jobRuntimeCommand(root, c.action, "JOB-VAL", c.o)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: expected %q, got %v", c.name, c.want, err)
		}
	}
	if after := len(mustJournal(t, root, "JOB-VAL")); after != before {
		t.Fatalf("refused events were written: %d -> %d", before, after)
	}
	runtimeOK(t, root, "event", "JOB-VAL", jobOptions{executionID: "exec-1", eventType: "output-ref", outputRef: "logs/run-1.txt"})
	if err := jobTerminate(root, "JOB-VAL", "cancelled", jobOptions{actor: "Control", reason: "stop"}); err != nil {
		t.Fatal(err)
	}
	if err := jobRuntimeCommand(root, "heartbeat", "JOB-VAL", jobOptions{executionID: "exec-1", used: map[string]bool{}}); err == nil {
		t.Fatal("runtime event accepted for a cancelled job")
	}
}

func TestReliabilityOutputsContainNoSecrets(t *testing.T) {
	notes := captureNotifications(t)
	root := newReliabilityRepo(t, "JOB-SEC")
	ignoreLocalState(t, root)
	worktree := startTestJob(t, root, "JOB-SEC")
	writeTestFile(t, filepath.Join(worktree, "services", "example", "wip.go"), "package example\n")
	secret := "TOKEN=abc123supersecret"
	if err := jobTerminate(root, "JOB-SEC", "cancelled", jobOptions{actor: "Control", reason: "leaked " + secret}); err != nil {
		t.Fatal(err)
	}
	cancelled := notificationsFor(*notes, "cancelled")
	ready := notificationsFor(*notes, "recovery-ready")
	if len(cancelled) != 1 || len(ready) != 1 {
		t.Fatalf("cancel should notify cancellation and recovery readiness: %+v", *notes)
	}
	if !strings.Contains(cancelled[0].Reason, "withheld") || !strings.Contains(ready[0].RecoveryAction, "refs/leadfox/recovery/JOB-SEC/1") {
		t.Fatalf("unexpected notifications: %+v", *notes)
	}
	for _, note := range *notes {
		for _, field := range []string{note.Project, note.Job, note.Event, note.State, note.RecoveryAction} {
			if field == "" {
				t.Fatalf("notification missing a required field: %+v", note)
			}
		}
	}
	journal, err := os.ReadFile(journalPath(root, "JOB-SEC"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journal), "abc123supersecret") {
		t.Fatal("secret value reached the runtime journal")
	}
	report := buildJobStatus(root, mustReadJob(t, root, "JOB-SEC"))
	for _, text := range append(report.Warnings, report.Recovery, report.NextAction, report.Meaning) {
		if strings.Contains(text, "abc123supersecret") {
			t.Fatalf("secret value reached status output: %q", text)
		}
	}
}

func TestRuntimeSettingsValidation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(runtimeSettingsPath))
	if thresholds, err := loadRuntimeThresholds(root, jobPacket{}); err != nil || thresholds != defaultRuntimeThresholds {
		t.Fatalf("defaults not applied: %+v %v", thresholds, err)
	}
	for _, content := range []string{"heartbeat_stale_after: soon\n", "worker_lost_after: -5m\n", "heartbeat_stale_after: 2h\nworker_lost_after: 1h\n"} {
		writeTestFile(t, path, content)
		if _, err := loadRuntimeThresholds(root, jobPacket{}); err == nil {
			t.Fatalf("invalid settings accepted: %q", content)
		}
	}
	writeTestFile(t, path, "heartbeat_stale_after: 5m\nworker_lost_after: 20m\njob_timeout: none\n")
	thresholds, err := loadRuntimeThresholds(root, jobPacket{Timeout: "4h"})
	if err != nil || thresholds.StaleAfter != 5*time.Minute || thresholds.LostAfter != 20*time.Minute || thresholds.Timeout != 4*time.Hour {
		t.Fatalf("settings not applied: %+v %v", thresholds, err)
	}
	if _, err := loadRuntimeThresholds(root, jobPacket{ID: "J", Timeout: "forever"}); err == nil {
		t.Fatal("invalid packet timeout accepted")
	}
}

func TestJobCheckRefusesWithStagedChanges(t *testing.T) {
	root, _ := newRuntimeJob(t, "JOB-STAGED", time.Now().UTC())
	runtimeOK(t, root, "event", "JOB-STAGED", exitOptions(2))
	writeTestFile(t, filepath.Join(root, "notes.md"), "control work in progress\n")
	gitTest(t, root, "add", "notes.md")
	if err := jobCheck(root, []string{"JOB-STAGED"}, false); err == nil {
		t.Fatal("job check committed alongside unrelated staged changes")
	}
	if status := mustReadJob(t, root, "JOB-STAGED").Status; status != "running" {
		t.Fatalf("refused check changed status to %s", status)
	}
}

func TestRuntimeShowForClosedJobAndProgressCount(t *testing.T) {
	t0 := time.Now().UTC()
	root, _ := newRuntimeJob(t, "JOB-SHOW", t0)
	if err := jobProgress(root, "JOB-SHOW", jobOptions{next: "write tests", executionID: "exec-1"}); err != nil {
		t.Fatal(err)
	}
	runtimeOK(t, root, "event", "JOB-SHOW", jobOptions{executionID: "exec-1", eventType: "handoff"})
	events := mustJournal(t, root, "JOB-SHOW")
	packet := mustReadJob(t, root, "JOB-SHOW")
	later := t0.Add(13 * time.Hour)

	state, progress := runtimeShowState(packet, events, later, defaultRuntimeThresholds)
	if state.Condition != conditionHandedOff || progress != 1 {
		t.Fatalf("running job: condition %s, progress %d", state.Condition, progress)
	}
	for _, status := range []string{stateMerged, stateCancelled} {
		packet.Status = status
		state, _ := runtimeShowState(packet, events, later, defaultRuntimeThresholds)
		text := describeRuntime(state)
		if state.Condition != conditionJobEnded || !strings.Contains(text, "ended; the job is closed") || strings.Contains(text, "ago") {
			t.Fatalf("%s job shown as live runtime: %s", status, text)
		}
	}
}
