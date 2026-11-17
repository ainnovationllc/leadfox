package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newReliabilityRepo creates a Control checkout with one ready job whose
// scope is services/example/**.
func newReliabilityRepo(t *testing.T, id string) string {
	t.Helper()
	root := t.TempDir()
	gitTest(t, root, "init", "-b", "main")
	gitTest(t, root, "config", "user.email", "leadfox@example.test")
	gitTest(t, root, "config", "user.name", "Leadfox Test")
	for _, directory := range []string{filepath.Join(root, "control", "jobs"), filepath.Join(root, "services", "example")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeReadyPacket(t, root, id)
	writeTestFile(t, filepath.Join(root, "services", "example", "README.md"), "initial\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "initial")
	return root
}

func writeReadyPacket(t *testing.T, root, id string) {
	t.Helper()
	packet := "---\nid: " + id + "\nstatus: ready\nrole: service\nobjective: Reliability test job.\nbase_branch: main\nallowed_paths:\n  - services/example/**\n---\n# " + id + "\n"
	writeTestFile(t, jobPath(root, id), packet)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadJob(t *testing.T, root, id string) jobPacket {
	t.Helper()
	packet, _, err := readJob(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

// markReady moves a draft job to ready and commits it, as Control would.
func markReady(t *testing.T, root, id string) {
	t.Helper()
	packet, body, err := readJob(root, id)
	if err != nil {
		t.Fatal(err)
	}
	packet.Status = "ready"
	if err := writeJob(root, packet, body); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", jobPath(root, id))
	gitTest(t, root, "commit", "-m", "mark "+id+" ready")
}

func siblingPath(root, name string) string {
	return filepath.Join(filepath.Dir(root), name)
}

func startTestJob(t *testing.T, root, id string) string {
	t.Helper()
	worktree := siblingPath(root, "wt-"+strings.ToLower(id))
	if err := jobStart(root, id, jobOptions{worktree: worktree}); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	return worktree
}

func commitInWorktree(t *testing.T, worktree, relative, content, message string) string {
	t.Helper()
	writeTestFile(t, filepath.Join(worktree, filepath.FromSlash(relative)), content)
	gitTest(t, worktree, "add", relative)
	gitTest(t, worktree, "commit", "-m", message)
	return strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD"))
}

func refExists(t *testing.T, root, ref string) bool {
	t.Helper()
	_, err := gitOutput(root, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

func TestVolatileWorktreeRejectedUnlessOverridden(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-VOL")
	volatile := t.TempDir()
	useVolatileRoots(t, volatile)
	target := filepath.Join(volatile, "job")

	err := jobStart(root, "JOB-VOL", jobOptions{worktree: target})
	if err == nil || !strings.Contains(err.Error(), "volatile location") {
		t.Fatalf("volatile worktree was not rejected: %v", err)
	}

	// A symbolic link from a persistent folder into a volatile one is still
	// volatile.
	link := siblingPath(root, "persistent-link")
	if err := os.Symlink(volatile, link); err != nil {
		t.Fatal(err)
	}
	if err := jobStart(root, "JOB-VOL", jobOptions{worktree: filepath.Join(link, "job")}); err == nil || !strings.Contains(err.Error(), "volatile location") {
		t.Fatalf("volatile worktree behind a symlink was not rejected: %v", err)
	}

	if err := jobStart(root, "JOB-VOL", jobOptions{worktree: target, allowVolatile: true, reason: "short spike"}); err == nil || !strings.Contains(err.Error(), "not permitted") {
		t.Fatalf("override was accepted without project permission: %v", err)
	}

	writeTestFile(t, filepath.Join(root, filepath.FromSlash(worktreeSettingsPath)), "schema_version: 1\nroot: ../jobs\nallow_volatile: true\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "permit volatile override")
	if err := jobStart(root, "JOB-VOL", jobOptions{worktree: target, allowVolatile: true}); err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("override was accepted without a reason: %v", err)
	}
	if packet := mustReadJob(t, root, "JOB-VOL"); packet.Status != "ready" {
		t.Fatalf("refused starts changed the job: %+v", packet)
	}

	if err := jobStart(root, "JOB-VOL", jobOptions{worktree: target, allowVolatile: true, reason: "short spike", actor: "owner"}); err != nil {
		t.Fatalf("permitted override failed: %v", err)
	}
	packet := mustReadJob(t, root, "JOB-VOL")
	if packet.WorktreeOverride == nil || packet.WorktreeOverride.Reason != "short spike" || packet.WorktreeOverride.Actor != "owner" || packet.WorktreeOverride.Location != volatile {
		t.Fatalf("override was not recorded: %+v", packet.WorktreeOverride)
	}
	report := buildJobStatus(root, packet)
	if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "volatile location by recorded override") {
		t.Fatalf("status does not warn about the override: %+v", report.Warnings)
	}
}

func TestPersistentWorktreeRootIsDefault(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-ROOT")
	if err := jobStart(root, "JOB-ROOT", jobOptions{}); err == nil || !strings.Contains(err.Error(), worktreeSettingsPath) {
		t.Fatalf("start without a worktree or settings should name the settings file: %v", err)
	}
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(worktreeSettingsPath)), "schema_version: 1\nroot: ../project-jobs\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "configure worktree root")
	if err := jobStart(root, "JOB-ROOT", jobOptions{}); err != nil {
		t.Fatalf("start with configured root: %v", err)
	}
	packet := mustReadJob(t, root, "JOB-ROOT")
	expected := filepath.Join(filepath.Dir(root), "project-jobs", "JOB-ROOT")
	if packet.Worktree != expected {
		t.Fatalf("worktree %q, expected %q", packet.Worktree, expected)
	}
	if _, err := os.Stat(filepath.Join(expected, "services", "example", "README.md")); err != nil {
		t.Fatalf("worktree was not created at the configured root: %v", err)
	}
}

func TestWorktreeRootInsideCheckoutOrVolatileIsInvalid(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-CHECK")
	invalid, warnings := checkWorktreeSettings(root)
	if len(invalid) != 0 || len(warnings) != 1 {
		t.Fatalf("missing settings should warn only: %v %v", invalid, warnings)
	}
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(worktreeSettingsPath)), "root: jobs\n")
	if invalid, _ := checkWorktreeSettings(root); len(invalid) != 1 || !strings.Contains(invalid[0], "outside the Control checkout") {
		t.Fatalf("root inside the checkout was accepted: %v", invalid)
	}
	volatile := t.TempDir()
	useVolatileRoots(t, volatile)
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(worktreeSettingsPath)), "root: "+volatile+"/jobs\n")
	if invalid, _ := checkWorktreeSettings(root); len(invalid) != 1 || !strings.Contains(invalid[0], "volatile") {
		t.Fatalf("volatile root was accepted: %v", invalid)
	}
}

func TestHomeDirectoryExpansion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	expanded, err := expandHomePath("~/jobs/JOB-1")
	if err != nil || expanded != filepath.Join(home, "jobs", "JOB-1") {
		t.Fatalf("~/ was not expanded: %q %v", expanded, err)
	}
	if expanded, err := expandHomePath("~"); err != nil || expanded != home {
		t.Fatalf("~ was not expanded: %q %v", expanded, err)
	}
	if _, err := expandHomePath("~other/jobs"); err == nil {
		t.Fatal("~user form should be refused")
	}
	root := t.TempDir()
	resolved, err := resolveJobWorktreePath(root, "JOB-1", "~/project-jobs/JOB-1", "", worktreeSettings{})
	if err != nil || resolved != filepath.Join(home, "project-jobs", "JOB-1") {
		t.Fatalf("packet worktree with ~ resolved to %q: %v", resolved, err)
	}
	if err := ensureExternalWorktree(root, resolved); err != nil {
		t.Fatalf("expanded home path should be outside the checkout: %v", err)
	}
}

func TestCancelSnapshotsUncommittedWork(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-SNAP")
	worktree := startTestJob(t, root, "JOB-SNAP")
	partial := commitInWorktree(t, worktree, "services/example/service.go", "package example\n", "partial work")
	writeTestFile(t, filepath.Join(worktree, "services", "example", "README.md"), "edited but not committed\n")
	writeTestFile(t, filepath.Join(worktree, "services", "example", "new.go"), "package example // untracked\n")

	if err := jobTerminate(root, "JOB-SNAP", "cancelled", jobOptions{reason: "session ended", actor: "Control"}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	packet := mustReadJob(t, root, "JOB-SNAP")
	if packet.Status != "cancelled" || len(packet.RecoveryPoints) != 1 {
		t.Fatalf("cancel did not record a recovery point: %+v", packet)
	}
	point := packet.RecoveryPoints[0]
	if !point.Snapshot || point.BranchHead != partial || point.Commit == partial || point.Ref != "refs/leadfox/recovery/JOB-SNAP/1" {
		t.Fatalf("unexpected recovery point: %+v", point)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree should be removed after the snapshot: %v", err)
	}
	if got := gitTest(t, root, "show", point.Ref+":services/example/README.md"); got != "edited but not committed\n" {
		t.Fatalf("modified file not preserved: %q", got)
	}
	if got := gitTest(t, root, "show", point.Ref+":services/example/new.go"); got != "package example // untracked\n" {
		t.Fatalf("untracked file not preserved: %q", got)
	}
	if head := strings.TrimSpace(gitTest(t, root, "rev-parse", "refs/heads/"+packet.Branch)); head != partial {
		t.Fatalf("job branch moved or was deleted: %s", head)
	}
	report := buildJobStatus(root, packet)
	if !strings.Contains(report.Recovery, point.Ref) || !strings.Contains(report.Recovery, "uncommitted snapshot") {
		t.Fatalf("status does not describe the recovery point: %q", report.Recovery)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("removed worktree should not produce warnings: %v", report.Warnings)
	}
	if !strings.Contains(report.NextAction, "cancelled, not completed") {
		t.Fatalf("cancelled job could be mistaken for completed: %q", report.NextAction)
	}
}

func TestCancelVariantsRecordRecoveryContext(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-DISCARD")
	worktree := startTestJob(t, root, "JOB-DISCARD")
	writeTestFile(t, filepath.Join(worktree, "services", "example", "scratch.txt"), "throwaway\n")
	if err := jobTerminate(root, "JOB-DISCARD", "cancelled", jobOptions{reason: "wrong approach", actor: "Control", discardUncommitted: true}); err != nil {
		t.Fatalf("cancel with discard: %v", err)
	}
	point := mustReadJob(t, root, "JOB-DISCARD").RecoveryPoints[0]
	if !point.DiscardedUncommitted || point.Snapshot || point.Commit != point.BranchHead {
		t.Fatalf("discard was not recorded: %+v", point)
	}

	writeReadyPacket(t, root, "JOB-LOST")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "add JOB-LOST")
	lost := startTestJob(t, root, "JOB-LOST")
	commitInWorktree(t, lost, "services/example/lost.go", "package example\n", "committed before host restart")
	if err := os.RemoveAll(lost); err != nil {
		t.Fatal(err)
	}
	if err := jobTerminate(root, "JOB-LOST", "cancelled", jobOptions{reason: "host restarted", actor: "Control"}); err != nil {
		t.Fatalf("cancel with missing worktree: %v", err)
	}
	lostPoint := mustReadJob(t, root, "JOB-LOST").RecoveryPoints[0]
	if !lostPoint.WorktreeMissing || !refExists(t, root, lostPoint.Ref) {
		t.Fatalf("missing worktree was not recorded: %+v", lostPoint)
	}
	if got := gitTest(t, root, "show", lostPoint.Ref+":services/example/lost.go"); got != "package example\n" {
		t.Fatalf("committed work lost with the worktree: %q", got)
	}

	// A job that never started has no branch and no recovery point.
	writeReadyPacket(t, root, "JOB-NEVER")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "add JOB-NEVER")
	if err := jobTerminate(root, "JOB-NEVER", "cancelled", jobOptions{reason: "not needed", actor: "Control"}); err != nil {
		t.Fatal(err)
	}
	if points := mustReadJob(t, root, "JOB-NEVER").RecoveryPoints; len(points) != 0 {
		t.Fatalf("unstarted job should have no recovery point: %+v", points)
	}
}

func TestLegacyCancelledJobDescribesBranch(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-LEGACY")
	gitTest(t, root, "branch", "job/job-legacy")
	packet := jobPacket{ID: "JOB-LEGACY", Status: "cancelled", Branch: "job/job-legacy"}
	report := buildJobStatus(root, packet)
	if !strings.Contains(report.Recovery, "before recovery records existed") || !strings.Contains(report.Recovery, "job/job-legacy") {
		t.Fatalf("legacy cancellation not explained: %q", report.Recovery)
	}
}

func TestReissueFromBranchKeepsScopeBase(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-A")
	worktree := startTestJob(t, root, "JOB-A")
	partial := commitInWorktree(t, worktree, "services/example/service.go", "package example\n", "partial work")
	// The base branch moves on while the job is interrupted.
	writeTestFile(t, filepath.Join(root, "docs", "notes.md"), "base branch change\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "unrelated base change")

	if err := jobReissue(root, "JOB-A", "JOB-B", jobOptions{actor: "Control", reason: "worker lost", fromBranch: true}); err != nil {
		t.Fatalf("reissue from branch: %v", err)
	}
	newPacket := mustReadJob(t, root, "JOB-B")
	if newPacket.StartPoint == nil || newPacket.StartPoint.Kind != "branch" || newPacket.StartPoint.Commit != partial || newPacket.StartPoint.SourceJob != "JOB-A" {
		t.Fatalf("start point not recorded: %+v", newPacket.StartPoint)
	}
	if len(newPacket.RecoveryPoints) != 0 || newPacket.Worktree != "" {
		t.Fatalf("reissued job inherited runtime state: %+v", newPacket)
	}
	markReady(t, root, "JOB-B")
	newWorktree := startTestJob(t, root, "JOB-B")
	if _, err := os.Stat(filepath.Join(newWorktree, "services", "example", "service.go")); err != nil {
		t.Fatalf("preserved work missing from the new worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newWorktree, "docs", "notes.md")); err != nil {
		t.Fatalf("new worktree does not include the current base: %v", err)
	}
	started := mustReadJob(t, root, "JOB-B")
	if started.BranchStartCommit != partial || started.ScopeBaseCommit == "" {
		t.Fatalf("start provenance not recorded: %+v", started)
	}
	if err := jobScope(root, "JOB-B", jobOptions{}); err != nil {
		t.Fatalf("carried-over in-scope work should pass and base changes must not count: %v", err)
	}
	for _, ref := range []string{"refs/heads/job/job-a", "refs/leadfox/recovery/JOB-A/1", "refs/heads/job/job-b"} {
		if !refExists(t, root, ref) {
			t.Fatalf("cleanup removed %s", ref)
		}
	}
}

func TestReissueFromBranchScopeChecksCarriedCommits(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-OUT")
	worktree := startTestJob(t, root, "JOB-OUT")
	commitInWorktree(t, worktree, "outside.txt", "out of scope\n", "out-of-scope partial work")
	if err := jobReissue(root, "JOB-OUT", "JOB-OUT2", jobOptions{actor: "Control", reason: "retry", fromBranch: true}); err != nil {
		t.Fatal(err)
	}
	markReady(t, root, "JOB-OUT2")
	startTestJob(t, root, "JOB-OUT2")
	if err := jobScope(root, "JOB-OUT2", jobOptions{}); err == nil {
		t.Fatal("carried-over out-of-scope commit escaped the scope check")
	}
}

func TestReissueFromLatestRecoveryIncludesSnapshot(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-R1")
	worktree := startTestJob(t, root, "JOB-R1")
	writeTestFile(t, filepath.Join(worktree, "services", "example", "draft.go"), "package example // uncommitted\n")
	if err := jobReissue(root, "JOB-R1", "JOB-R2", jobOptions{actor: "Control", reason: "session ended", fromRecovery: "latest"}); err != nil {
		t.Fatalf("reissue from latest recovery point: %v", err)
	}
	old := mustReadJob(t, root, "JOB-R1")
	if old.Status != "cancelled" || old.SupersededBy != "JOB-R2" || len(old.RecoveryPoints) != 1 {
		t.Fatalf("old job not superseded with a recovery point: %+v", old)
	}
	newPacket := mustReadJob(t, root, "JOB-R2")
	if newPacket.StartPoint == nil || newPacket.StartPoint.Kind != "recovery" || newPacket.StartPoint.Ref != old.RecoveryPoints[0].Ref {
		t.Fatalf("start point does not reference the recovery point: %+v", newPacket.StartPoint)
	}
	markReady(t, root, "JOB-R2")
	newWorktree := startTestJob(t, root, "JOB-R2")
	content, err := os.ReadFile(filepath.Join(newWorktree, "services", "example", "draft.go"))
	if err != nil || string(content) != "package example // uncommitted\n" {
		t.Fatalf("snapshot content missing from reissued job: %q %v", content, err)
	}
	if err := jobReissue(root, "JOB-R1", "JOB-R3", jobOptions{actor: "Control", reason: "again"}); err == nil {
		t.Fatal("a superseded job was reissued twice")
	}
}

func TestReissueWithInvalidStartPointLeavesOldJobUntouched(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-IDLE")
	if err := jobReissue(root, "JOB-IDLE", "JOB-IDLE2", jobOptions{actor: "Control", reason: "retry", fromBranch: true}); err == nil {
		t.Fatal("reissue from a missing branch should fail")
	}
	if err := jobReissue(root, "JOB-IDLE", "JOB-IDLE2", jobOptions{actor: "Control", reason: "retry", fromRecovery: "latest"}); err == nil {
		t.Fatal("reissue from a recovery point that cannot exist should fail")
	}
	if packet := mustReadJob(t, root, "JOB-IDLE"); packet.Status != "ready" {
		t.Fatalf("failed reissue changed the old job: %+v", packet)
	}
	if _, err := os.Stat(jobPath(root, "JOB-IDLE2")); !os.IsNotExist(err) {
		t.Fatalf("failed reissue created the new job: %v", err)
	}
}

func TestCancelRefusesWhenSnapshotCannotBeTaken(t *testing.T) {
	root := newReliabilityRepo(t, "JOB-NOBRANCH")
	worktree := startTestJob(t, root, "JOB-NOBRANCH")
	writeTestFile(t, filepath.Join(worktree, "services", "example", "work.go"), "package example\n")
	// Simulate a corrupted record: the packet names a branch that is gone.
	packet, body, err := readJob(root, "JOB-NOBRANCH")
	if err != nil {
		t.Fatal(err)
	}
	packet.Branch = "job/does-not-exist"
	if err := writeJob(root, packet, body); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "commit", "-am", "corrupt branch record")
	if err := jobTerminate(root, "JOB-NOBRANCH", "cancelled", jobOptions{reason: "test", actor: "Control"}); err == nil {
		t.Fatal("cancel removed a worktree without a recovery point")
	}
	if _, err := os.Stat(filepath.Join(worktree, "services", "example", "work.go")); err != nil {
		t.Fatalf("refused cancel removed work: %v", err)
	}
	if status := mustReadJob(t, root, "JOB-NOBRANCH").Status; status != "running" {
		t.Fatalf("refused cancel changed status to %s", status)
	}
}
