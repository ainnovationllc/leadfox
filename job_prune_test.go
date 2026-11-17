package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// addReadyJob adds and commits another ready job packet.
func addReadyJob(t *testing.T, root, id string) {
	t.Helper()
	writeReadyPacket(t, root, id)
	gitTest(t, root, "add", jobPath(root, id))
	gitTest(t, root, "commit", "-m", "add "+id)
}

func branchHead(t *testing.T, root, branch string) string {
	t.Helper()
	return strings.TrimSpace(gitTest(t, root, "rev-parse", "refs/heads/"+branch))
}

func newPruneRepo(t *testing.T) (root string, heads map[string]string) {
	t.Helper()
	root = newReliabilityRepo(t, "JOB-M")
	for _, id := range []string{"JOB-C", "JOB-L", "JOB-K"} {
		addReadyJob(t, root, id)
	}
	heads = map[string]string{}

	// JOB-M: merged into main with --no-ff, worktree removed.
	worktree := startTestJob(t, root, "JOB-M")
	heads["JOB-M"] = commitInWorktree(t, worktree, "services/example/m.txt", "m\n", "merged work")
	gitTest(t, root, "worktree", "remove", worktree)
	gitTest(t, root, "merge", "--no-ff", "-m", "merge JOB-M", "job/job-m")
	packet, body, err := readJob(root, "JOB-M")
	if err != nil {
		t.Fatal(err)
	}
	packet.Status, packet.TerminalAt = stateMerged, time.Now().UTC().Format(time.RFC3339)
	if err := writeJob(root, packet, body); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "commit", "-am", "record merge JOB-M")

	// JOB-C, JOB-L, JOB-K: cancelled with committed work not in main.
	for _, id := range []string{"JOB-C", "JOB-L", "JOB-K"} {
		worktree := startTestJob(t, root, id)
		heads[id] = commitInWorktree(t, worktree, "services/example/"+strings.ToLower(id)+".txt", id+"\n", "partial work")
		if err := jobTerminate(root, id, stateCancelled, jobOptions{reason: "stopped", actor: "Control"}); err != nil {
			t.Fatal(err)
		}
	}
	// JOB-L imitates a job cancelled before recovery records existed: its
	// record lists no recovery point although ref 1 still exists.
	packet, body, err = readJob(root, "JOB-L")
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.RecoveryPoints) == 0 || !refExists(t, root, recoveryRef("JOB-L", 1)) {
		t.Fatalf("expected cancel to record recovery point 1: %+v", packet.RecoveryPoints)
	}
	gitTest(t, root, "update-ref", recoveryRef("JOB-L", 1), "main")
	packet.RecoveryPoints = nil
	if err := writeJob(root, packet, body); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "commit", "-am", "legacy JOB-L record")
	// JOB-K's branch is checked out again for inspection.
	gitTest(t, root, "worktree", "add", siblingPath(root, "inspect-k"), "job/job-k")
	return root, heads
}

func TestJobPruneDeletesOnlyPreservedBranches(t *testing.T) {
	root, heads := newPruneRepo(t)

	// Dry run and an age filter change nothing.
	if err := jobPrune(root, jobOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := jobPrune(root, jobOptions{yes: true, olderThan: "30d"}); err != nil {
		t.Fatal(err)
	}
	for id := range heads {
		if !refExists(t, root, "refs/heads/job/"+strings.ToLower(id)) {
			t.Fatalf("%s branch deleted by a dry run or age filter", id)
		}
	}

	// --cancelled leaves merged jobs alone.
	if err := jobPrune(root, jobOptions{yes: true, pruneCancelled: true, actor: "Control"}); err != nil {
		t.Fatal(err)
	}
	if !refExists(t, root, "refs/heads/job/job-m") {
		t.Fatal("--cancelled deleted a merged job branch")
	}
	if subject := strings.TrimSpace(gitTest(t, root, "log", "-1", "--format=%s")); subject != "Record prune of 2 job branch(es)" {
		t.Fatalf("unexpected lifecycle commit: %s", subject)
	}

	// JOB-C: head was in its existing recovery ref.
	c := mustReadJob(t, root, "JOB-C")
	if refExists(t, root, "refs/heads/job/job-c") || c.PrunedBranch == nil || c.PrunedBranch.Head != heads["JOB-C"] || c.PrunedBranch.PreservedBy != recoveryRef("JOB-C", 1) || c.PrunedBranch.Actor != "Control" {
		t.Fatalf("JOB-C not pruned against its recovery ref: %+v", c.PrunedBranch)
	}

	// JOB-L: head was unpreserved; a new ref skips the existing number 1.
	l := mustReadJob(t, root, "JOB-L")
	newRef := recoveryRef("JOB-L", 2)
	if refExists(t, root, "refs/heads/job/job-l") || l.PrunedBranch == nil || l.PrunedBranch.PreservedBy != newRef {
		t.Fatalf("JOB-L not preserved before pruning: %+v", l.PrunedBranch)
	}
	if got := strings.TrimSpace(gitTest(t, root, "rev-parse", newRef)); got != heads["JOB-L"] {
		t.Fatalf("new recovery ref %s points at %s, want %s", newRef, got, heads["JOB-L"])
	}
	if got := strings.TrimSpace(gitTest(t, root, "rev-parse", recoveryRef("JOB-L", 1))); got == heads["JOB-L"] {
		t.Fatal("existing recovery ref 1 was overwritten")
	}
	if point, ok := latestRecoveryPoint(l); !ok || point.Ref != newRef || point.Commit != heads["JOB-L"] {
		t.Fatalf("recovery point not recorded: %+v", l.RecoveryPoints)
	}

	// JOB-K: checked out, so kept.
	if !refExists(t, root, "refs/heads/job/job-k") || mustReadJob(t, root, "JOB-K").PrunedBranch != nil {
		t.Fatal("a checked-out branch was pruned")
	}

	// Merged job: head contained in main.
	if err := jobPrune(root, jobOptions{yes: true, pruneMerged: true}); err != nil {
		t.Fatal(err)
	}
	m := mustReadJob(t, root, "JOB-M")
	if refExists(t, root, "refs/heads/job/job-m") || m.PrunedBranch == nil || m.PrunedBranch.PreservedBy != "main" {
		t.Fatalf("merged branch not pruned against main: %+v", m.PrunedBranch)
	}
	if status := strings.TrimSpace(gitTest(t, root, "status", "--porcelain")); status != "" {
		t.Fatalf("prune left uncommitted changes:\n%s", status)
	}

	// Status, reissue and recover explain the pruned branch.
	if report := buildJobStatus(root, m); !strings.Contains(report.Recovery, "branch job/job-m was pruned") {
		t.Fatalf("status does not report the pruned branch: %q", report.Recovery)
	}
	if _, err := resolveReissueStartPoint(root, l, jobOptions{fromBranch: true}); err == nil || !strings.Contains(err.Error(), "--from-recovery 2") {
		t.Fatalf("reissue --from-branch does not point to the recovery ref: %v", err)
	}
	if start, err := resolveReissueStartPoint(root, l, jobOptions{fromRecovery: "latest"}); err != nil || start.Commit != heads["JOB-L"] {
		t.Fatalf("reissue --from-recovery latest does not reach the pruned work: %+v %v", start, err)
	}
	if err := jobRecover(root, "JOB-L", jobOptions{actor: "Control", reason: "continue"}); err == nil || !strings.Contains(err.Error(), "was pruned") {
		t.Fatalf("recover does not explain the pruned branch: %v", err)
	}

	// A second run finds only the kept branch.
	if err := jobPrune(root, jobOptions{yes: true}); err != nil {
		t.Fatal(err)
	}
	if !refExists(t, root, "refs/heads/job/job-k") {
		t.Fatal("checked-out branch deleted on the second run")
	}
}

func TestJobPruneRefusesStagedChangesAndParsesAges(t *testing.T) {
	root, _ := newPruneRepo(t)
	writeTestFile(t, filepath.Join(root, "notes.txt"), "staged\n")
	gitTest(t, root, "add", "notes.txt")
	if err := jobPrune(root, jobOptions{yes: true}); err == nil || !strings.Contains(err.Error(), "staged changes") {
		t.Fatalf("prune ran with staged changes: %v", err)
	}
	if !refExists(t, root, "refs/heads/job/job-c") {
		t.Fatal("branch deleted despite staged changes")
	}

	for value, want := range map[string]time.Duration{"30d": 720 * time.Hour, "12h": 12 * time.Hour, "0d": 0} {
		if got, err := parseAge(value); err != nil || got != want {
			t.Fatalf("parseAge(%q) = %v, %v", value, got, err)
		}
	}
	for _, value := range []string{"-1d", "xd", "soon"} {
		if _, err := parseAge(value); err == nil {
			t.Fatalf("parseAge accepted %q", value)
		}
	}
	if err := validateJobOptions("prune", jobOptions{used: map[string]bool{"merged": true, "cancelled": true, "older-than": true, "actor": true, "yes": true}}); err != nil {
		t.Fatal(err)
	}
	if err := validateJobOptions("prune", jobOptions{used: map[string]bool{"reason": true}}); err == nil {
		t.Fatal("prune accepted --reason")
	}
}
