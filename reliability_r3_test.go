package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func allRefs(t *testing.T, root string) map[string]string {
	t.Helper()
	refs := map[string]string{}
	for _, line := range nonEmptyLines(gitTest(t, root, "for-each-ref", "--format=%(refname) %(objectname)")) {
		name, commit, _ := strings.Cut(line, " ")
		refs[name] = commit
	}
	return refs
}

func assertNoRefRemoved(t *testing.T, before, after map[string]string, step string) {
	t.Helper()
	for name := range before {
		if _, ok := after[name]; !ok {
			t.Fatalf("%s removed ref %s", step, name)
		}
	}
}

// loseWorker registers an execution, reports its exit, and records the loss.
func loseWorker(t *testing.T, root, id string) {
	t.Helper()
	runtimeOK(t, root, "register", id, jobOptions{runtime: "local", executionID: "exec-1"})
	runtimeOK(t, root, "event", id, exitOptions(1))
	if err := jobCheck(root, []string{id}, false); err != nil {
		t.Fatal(err)
	}
	if status := mustReadJob(t, root, id).Status; status != "worker-lost" {
		t.Fatalf("setup: expected worker-lost, got %s", status)
	}
}

func TestRecoverRecreatesMissingWorktree(t *testing.T) {
	notes := captureNotifications(t)
	root := newReliabilityRepo(t, "JOB-HOST")
	ignoreLocalState(t, root)
	worktree := startTestJob(t, root, "JOB-HOST")
	partial := commitInWorktree(t, worktree, "services/example/service.go", "package example\n", "partial work")
	loseWorker(t, root, "JOB-HOST")
	// Host restart: the worktree directory is gone and Git still lists it.
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}
	before := mustReadJob(t, root, "JOB-HOST")
	if err := jobResume(root, "JOB-HOST", jobOptions{actor: "Control"}); err == nil {
		t.Fatal("resume succeeded without a worktree")
	}
	if err := jobRecover(root, "JOB-HOST", jobOptions{actor: "Control", reason: "host restarted"}); err != nil {
		t.Fatalf("recover: %v", err)
	}
	packet := mustReadJob(t, root, "JOB-HOST")
	if packet.Status != "paused" || packet.Worktree != worktree {
		t.Fatalf("recovery did not restore the worktree record: %+v", packet)
	}
	if head := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD")); head != partial {
		t.Fatalf("recreated worktree is at %s, expected the branch head %s", head, partial)
	}
	if branch := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "--abbrev-ref", "HEAD")); branch != packet.Branch {
		t.Fatalf("recreated worktree is not on the job branch: %s", branch)
	}
	point := packet.RecoveryPoints[len(packet.RecoveryPoints)-1]
	if point.Commit != partial || !strings.HasPrefix(point.Reason, "recovered:") {
		t.Fatalf("recovery point not recorded: %+v", point)
	}
	if len(packet.GateEvidence) != len(before.GateEvidence) || len(packet.Interruptions) != 1 || packet.ApprovedBy != "" {
		t.Fatalf("recovery changed evidence or approval state: %+v", packet)
	}
	report := buildJobStatus(root, packet)
	if report.NextAction != "resume the job when work should continue" || len(report.Warnings) != 0 || report.Worktree != worktree {
		t.Fatalf("status inconsistent after recovery: next=%q warnings=%v", report.NextAction, report.Warnings)
	}
	if ready := notificationsFor(*notes, "recovery-ready"); len(ready) != 1 || !strings.Contains(ready[0].RecoveryAction, "leadfox job resume JOB-HOST") {
		t.Fatalf("recovery-ready notice missing or wrong: %+v", *notes)
	}
	if err := jobResume(root, "JOB-HOST", jobOptions{actor: "Control", reason: "continue"}); err != nil {
		t.Fatalf("resume after recovery: %v", err)
	}
	if err := jobCheck(root, []string{"JOB-HOST"}, false); err != nil {
		t.Fatal(err)
	}
	if status := mustReadJob(t, root, "JOB-HOST").Status; status != "running" {
		t.Fatalf("recovered job was re-marked %s by its lost execution", status)
	}
}

func TestRecoverCancelledJobRestoresSnapshot(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-UNDO")
	worktree := startTestJob(t, root, "JOB-UNDO")
	partial := commitInWorktree(t, worktree, "services/example/service.go", "package example\n", "partial work")
	writeTestFile(t, filepath.Join(worktree, "services", "example", "draft.go"), "package example // draft\n")
	if err := jobTerminate(root, "JOB-UNDO", "cancelled", jobOptions{actor: "Control", reason: "cancelled by mistake"}); err != nil {
		t.Fatal(err)
	}
	if err := jobRecover(root, "JOB-UNDO", jobOptions{actor: "Control", reason: "cancelled by mistake"}); err != nil {
		t.Fatalf("recover cancelled job: %v", err)
	}
	packet := mustReadJob(t, root, "JOB-UNDO")
	if packet.Status != "paused" || packet.Worktree != worktree {
		t.Fatalf("cancelled job not recovered at its recorded worktree: %+v", packet)
	}
	if head := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD")); head != partial {
		t.Fatalf("recovery moved the branch: %s", head)
	}
	content, err := os.ReadFile(filepath.Join(worktree, "services", "example", "draft.go"))
	if err != nil || string(content) != "package example // draft\n" {
		t.Fatalf("uncommitted snapshot not restored: %q %v", content, err)
	}
	if status := gitTest(t, worktree, "status", "--porcelain"); !strings.Contains(status, "?? services/example/draft.go") {
		t.Fatalf("restored work should be uncommitted and unstaged: %q", status)
	}
	var path []string
	for _, transition := range packet.Transitions {
		path = append(path, transition.To)
	}
	if strings.Join(path, ",") != "running,cancelled,paused" {
		t.Fatalf("transition history: %v", path)
	}
}

func TestRecoverRefusals(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-R")
	options := jobOptions{actor: "Control", reason: "try"}
	if err := jobRecover(root, "JOB-R", options); err == nil || !strings.Contains(err.Error(), "recover requires one of") {
		t.Fatalf("ready job recovered: %v", err)
	}
	if err := jobTerminate(root, "JOB-R", "cancelled", options); err != nil {
		t.Fatal(err)
	}
	if err := jobRecover(root, "JOB-R", options); err == nil || !strings.Contains(err.Error(), "never started") {
		t.Fatalf("job without a branch recovered: %v", err)
	}

	writeReadyPacket(t, root, "JOB-S")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "add JOB-S")
	worktree := startTestJob(t, root, "JOB-S")
	if err := jobRecover(root, "JOB-S", jobOptions{actor: "Control"}); err == nil {
		t.Fatal("recover without a reason accepted")
	}
	if err := jobPause(root, "JOB-S", options); err != nil {
		t.Fatal(err)
	}
	if err := jobRecover(root, "JOB-S", options); err == nil || !strings.Contains(err.Error(), "use leadfox job resume") {
		t.Fatalf("paused job with a worktree should resume, not recover: %v", err)
	}
	if err := jobReissue(root, "JOB-S", "JOB-S2", jobOptions{actor: "Control", reason: "replace"}); err != nil {
		t.Fatal(err)
	}
	if err := jobRecover(root, "JOB-S", options); err == nil || !strings.Contains(err.Error(), "superseded by JOB-S2") {
		t.Fatalf("superseded job recovered: %v", err)
	}
	_ = worktree

	if err := checkJobOperation("JOB-M", "recover", "merged"); err == nil {
		t.Fatal("merged job may not be recovered")
	}
}

func TestRecoveryImpliesNoApprovalOrSideEffects(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-SAFE")
	ignoreLocalState(t, root)
	worktree := startTestJob(t, root, "JOB-SAFE")
	commitInWorktree(t, worktree, "services/example/service.go", "package example\n", "work")
	loseWorker(t, root, "JOB-SAFE")
	before := allRefs(t, root)
	mainBefore := strings.TrimSpace(gitTest(t, root, "rev-parse", "main"))
	if err := jobRecover(root, "JOB-SAFE", jobOptions{actor: "Control", reason: "inspect"}); err != nil {
		t.Fatal(err)
	}
	after := allRefs(t, root)
	assertNoRefRemoved(t, before, after, "recover")
	for name, commit := range before {
		if name != "refs/heads/main" && after[name] != commit {
			t.Fatalf("recover moved %s", name)
		}
	}
	changed := nonEmptyLines(gitTest(t, root, "diff", "--name-only", mainBefore, "main"))
	if len(changed) != 1 || changed[0] != "control/jobs/JOB-SAFE.md" {
		t.Fatalf("recover changed more than the job record: %v", changed)
	}
	if remotes := strings.TrimSpace(gitTest(t, root, "remote")); remotes != "" {
		t.Fatalf("unexpected remote configuration: %s", remotes)
	}
	packet := mustReadJob(t, root, "JOB-SAFE")
	if packet.ApprovedBy != "" || packet.MergeCommit != "" || packet.Status != "paused" {
		t.Fatalf("recovery implied approval or merge: %+v", packet)
	}
}

func TestCleanupNeverDeletesBranchesOrRecoveryRefs(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-KEEP")
	worktree := startTestJob(t, root, "JOB-KEEP")
	commitInWorktree(t, worktree, "services/example/a.go", "package example\n", "a")
	writeTestFile(t, filepath.Join(worktree, "services", "example", "b.go"), "package example\n")

	before := allRefs(t, root)
	if err := jobTerminate(root, "JOB-KEEP", "cancelled", jobOptions{actor: "Control", reason: "stop"}); err != nil {
		t.Fatal(err)
	}
	afterCancel := allRefs(t, root)
	assertNoRefRemoved(t, before, afterCancel, "cancel")
	if _, ok := afterCancel["refs/leadfox/recovery/JOB-KEEP/1"]; !ok {
		t.Fatal("cancel did not create a recovery ref")
	}
	if err := jobRecover(root, "JOB-KEEP", jobOptions{actor: "Control", reason: "continue"}); err != nil {
		t.Fatal(err)
	}
	afterRecover := allRefs(t, root)
	assertNoRefRemoved(t, afterCancel, afterRecover, "recover")
	if err := jobReissue(root, "JOB-KEEP", "JOB-KEEP2", jobOptions{actor: "Control", reason: "fresh job", fromRecovery: "latest"}); err != nil {
		t.Fatal(err)
	}
	afterReissue := allRefs(t, root)
	assertNoRefRemoved(t, afterRecover, afterReissue, "reissue")
	var recovery []string
	for name := range afterReissue {
		if strings.HasPrefix(name, recoveryRefPrefix) {
			recovery = append(recovery, name)
		}
	}
	sort.Strings(recovery)
	if len(recovery) != 3 {
		t.Fatalf("expected recovery refs from cancel, recover and reissue: %v", recovery)
	}
}

// TestNoUnexplainedRunningJobsAfterCheck is the plan's R3 acceptance
// fixture: after job check, every job still marked running is explained by
// a healthy runtime, a handoff, or the manual workflow.
func TestNoUnexplainedRunningJobsAfterCheck(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-HEALTHY")
	ignoreLocalState(t, root)
	for _, id := range []string{"JOB-MANUAL", "JOB-LOST", "JOB-HANDOFF", "JOB-BLOCKED", "JOB-CANCELLED"} {
		writeReadyPacket(t, root, id)
	}
	writeTestFile(t, jobPath(root, "JOB-MERGED"), "---\nid: JOB-MERGED\nstatus: merged\nrole: service\nobjective: done\nbranch: job/job-merged\n---\n# JOB-MERGED\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "fixture jobs")
	for _, id := range []string{"JOB-HEALTHY", "JOB-MANUAL", "JOB-LOST", "JOB-HANDOFF", "JOB-BLOCKED", "JOB-CANCELLED"} {
		startTestJob(t, root, id)
	}
	t0 := time.Now().UTC()
	setClock(t, t0)
	for _, id := range []string{"JOB-HEALTHY", "JOB-LOST", "JOB-HANDOFF"} {
		runtimeOK(t, root, "register", id, jobOptions{runtime: "local", executionID: "exec-" + strings.ToLower(id)})
	}
	runtimeOK(t, root, "event", "JOB-HANDOFF", jobOptions{executionID: "exec-job-handoff", eventType: "handoff"})
	if err := jobTerminate(root, "JOB-BLOCKED", "blocked", jobOptions{actor: "Control", reason: "waiting on API key issuance"}); err != nil {
		t.Fatal(err)
	}
	if err := jobTerminate(root, "JOB-CANCELLED", "cancelled", jobOptions{actor: "Control", reason: "descoped"}); err != nil {
		t.Fatal(err)
	}
	// Ninety minutes later the healthy worker is still sending heartbeats;
	// the lost one went silent.
	setClock(t, t0.Add(90*time.Minute))
	runtimeOK(t, root, "heartbeat", "JOB-HEALTHY", jobOptions{executionID: "exec-job-healthy"})
	if err := jobCheck(root, nil, true); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{"JOB-HEALTHY": "running", "JOB-MANUAL": "running", "JOB-LOST": "worker-lost", "JOB-HANDOFF": "running", "JOB-BLOCKED": "blocked", "JOB-CANCELLED": "cancelled", "JOB-MERGED": "merged"}
	for id, status := range expected {
		packet := mustReadJob(t, root, id)
		if packet.Status != status {
			t.Fatalf("%s: status %s, expected %s", id, packet.Status, status)
		}
		report := buildJobStatus(root, packet)
		if packet.Status != "running" {
			continue
		}
		explained := report.Runtime != nil && (report.Runtime.Condition == conditionHealthy || report.Runtime.Condition == conditionHandedOff || report.Runtime.Condition == conditionNoRuntime)
		if !explained {
			t.Fatalf("%s is running without an explanation: %+v", id, report.Runtime)
		}
		for _, warning := range report.Warnings {
			if strings.Contains(warning, "job check") {
				t.Fatalf("%s still needs job check after job check: %s", id, warning)
			}
		}
	}
}
