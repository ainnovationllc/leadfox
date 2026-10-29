package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobLifecycleCreatesReviewsApprovesAndMerges(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-b", "main")
	gitTest(t, root, "config", "user.email", "leadfox@example.test")
	gitTest(t, root, "config", "user.name", "Leadfox Test")
	if err := os.MkdirAll(filepath.Join(root, "control", "jobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	packet := `---
id: JOB-001
status: ready
role: service
objective: Build the service skeleton.
base_branch: main
allowed_paths:
  - services/example/**
  - control/handoffs/JOB-001.md
gates:
  - unit
review_setup:
  - touch review-ready
---
# JOB-001

Build the service skeleton.
`
	if err := os.WriteFile(jobPath(root, "JOB-001"), []byte(packet), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "services", "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "control", "gates.yaml"), []byte("gates:\n  - id: unit\n    checks:\n      - id: scope\n        manual: true\n      - id: packet\n        run: test -f review-ready && test -f control/project.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "control", "project.yaml"), []byte("project:\n  name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "services", "example", "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "initial")

	jobWorktree := filepath.Join(root, "..", "job-worktree")
	if err := jobStart(root, "JOB-001", jobOptions{worktree: jobWorktree}); err != nil {
		t.Fatalf("start job: %v", err)
	}
	jobCopy, _, err := readJob(jobWorktree, "JOB-001")
	if err != nil {
		t.Fatal(err)
	}
	if jobCopy.Status != "running" || jobCopy.Branch == "" || jobCopy.Worktree == "" || jobCopy.BaseCommit == "" {
		t.Fatalf("job worktree received stale packet: %+v", jobCopy)
	}
	changed := filepath.Join(jobWorktree, "services", "example", "service.go")
	if err := os.WriteFile(changed, []byte("package example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(jobWorktree, "control", "handoffs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobWorktree, "control", "handoffs", "JOB-001.md"), []byte("# JOB-001 handoff\n\nComplete.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, jobWorktree, "add", ".")
	gitTest(t, jobWorktree, "commit", "-m", "JOB-001: implement service")
	if err := jobScope(root, "JOB-001", jobOptions{}); err != nil {
		t.Fatalf("scope check: %v", err)
	}

	reviewWorktree := filepath.Join(root, "..", "job-review")
	fullBranchCommit := strings.TrimSpace(gitTest(t, jobWorktree, "rev-parse", "HEAD"))
	shortBranchCommit := fullBranchCommit[:8]
	if err := jobReview(root, "JOB-001", jobOptions{commit: shortBranchCommit, worktree: reviewWorktree}); err != nil {
		t.Fatalf("review job: %v", err)
	}
	packetAfterReview, _, err := readJob(root, "JOB-001")
	if err != nil {
		t.Fatal(err)
	}
	if packetAfterReview.ReviewCommit != fullBranchCommit {
		t.Fatalf("review commit was not normalized: %q", packetAfterReview.ReviewCommit)
	}
	if len(packetAfterReview.GateEvidence) != 1 || !packetAfterReview.GateEvidence[0].Verified {
		t.Fatalf("gate evidence was not recorded: %+v", packetAfterReview.GateEvidence)
	}
	evidencePath := filepath.Join(root, packetAfterReview.GateEvidence[0].Evidence)
	evidenceBytes, err := os.ReadFile(evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidencePath, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := jobApprove(root, "JOB-001", jobOptions{verdict: "accepted", approvedBy: "developer"}); err == nil {
		t.Fatal("approval accepted tampered evidence")
	}
	if err := os.WriteFile(evidencePath, evidenceBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := jobApprove(root, "JOB-001", jobOptions{verdict: "changes-requested", approvedBy: "developer"}); err != nil {
		t.Fatalf("request changes: %v", err)
	}
	secondReviewWorktree := filepath.Join(root, "..", "job-review-second")
	if err := jobReview(root, "JOB-001", jobOptions{commit: fullBranchCommit, worktree: secondReviewWorktree}); err != nil {
		t.Fatalf("review second round: %v", err)
	}
	packetAfterSecondReview, _, err := readJob(root, "JOB-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(packetAfterSecondReview.ReviewRounds) != 2 {
		t.Fatalf("review rounds were overwritten: %+v", packetAfterSecondReview.ReviewRounds)
	}
	if err := jobApprove(root, "JOB-001", jobOptions{verdict: "accepted", approvedBy: "developer"}); err != nil {
		t.Fatalf("approve job: %v", err)
	}
	approval, err := os.ReadFile(filepath.Join(root, "control", "approvals", "JOB-001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(approval), "Add Control review evidence") || !strings.Contains(string(approval), "Approved by:") {
		t.Fatalf("approval record is incomplete: %s", approval)
	}
	if err := jobMerge(root, "JOB-001"); err != nil {
		t.Fatalf("merge job: %v", err)
	}
	if _, err := os.Stat(jobWorktree); !os.IsNotExist(err) {
		t.Fatalf("job worktree was not removed: %v", err)
	}
	if _, err := os.Stat(reviewWorktree); !os.IsNotExist(err) {
		t.Fatalf("review worktree was not removed: %v", err)
	}
	if _, err := os.Stat(secondReviewWorktree); !os.IsNotExist(err) {
		t.Fatalf("second review worktree was not removed: %v", err)
	}
	merged := gitTest(t, root, "log", "--oneline", "-3")
	if !strings.Contains(merged, "Merge JOB-001") || !strings.Contains(merged, "Record merge JOB-001") {
		t.Fatalf("merge records missing: %s", merged)
	}
	mergedPacket, _, err := readJob(root, "JOB-001")
	if err != nil {
		t.Fatal(err)
	}
	if mergedPacket.MergeCommit == "" {
		t.Fatal("merge commit was not recorded")
	}
	evidence, err := os.ReadFile(filepath.Join(root, packetAfterSecondReview.GateEvidence[0].Evidence))
	if err != nil {
		t.Fatalf("gate evidence file missing: %v", err)
	}
	if !strings.Contains(string(evidence), fullBranchCommit) {
		t.Fatalf("gate evidence does not identify reviewed commit: %s", evidence)
	}
}

func TestJobCanBeCancelledWithReason(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-b", "main")
	gitTest(t, root, "config", "user.email", "leadfox@example.test")
	gitTest(t, root, "config", "user.name", "Leadfox Test")
	if err := os.MkdirAll(filepath.Join(root, "control", "jobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	packet := `---
id: JOB-CANCEL
status: running
role: service
objective: test cancellation
---
# JOB-CANCEL
`
	if err := os.WriteFile(jobPath(root, "JOB-CANCEL"), []byte(packet), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "initial")
	if err := jobTerminate(root, "JOB-CANCEL", "cancelled", jobOptions{reason: "review prerequisite unavailable", approvedBy: "Control"}); err != nil {
		t.Fatal(err)
	}
	updated, _, err := readJob(root, "JOB-CANCEL")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "cancelled" || updated.TerminalReason != "review prerequisite unavailable" {
		t.Fatalf("unexpected cancellation record: %+v", updated)
	}
}

func gitTest(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
