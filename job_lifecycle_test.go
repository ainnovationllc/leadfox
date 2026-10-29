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
	if err := os.WriteFile(filepath.Join(root, "services", "example", "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "initial")

	jobWorktree := filepath.Join(root, "..", "job-worktree")
	if err := jobStart(root, "JOB-001", jobOptions{worktree: jobWorktree}); err != nil {
		t.Fatalf("start job: %v", err)
	}
	changed := filepath.Join(jobWorktree, "services", "example", "service.go")
	if err := os.WriteFile(changed, []byte("package example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, jobWorktree, "add", ".")
	gitTest(t, jobWorktree, "commit", "-m", "JOB-001: implement service")
	if err := jobScope(root, "JOB-001", jobOptions{}); err != nil {
		t.Fatalf("scope check: %v", err)
	}

	reviewWorktree := filepath.Join(root, "..", "job-review")
	branchCommit := strings.TrimSpace(gitTest(t, jobWorktree, "rev-parse", "HEAD"))
	if err := jobReview(root, "JOB-001", jobOptions{commit: branchCommit, worktree: reviewWorktree}); err != nil {
		t.Fatalf("review job: %v", err)
	}
	if err := jobApprove(root, "JOB-001", jobOptions{verdict: "accepted"}); err != nil {
		t.Fatalf("approve job: %v", err)
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
	merged := gitTest(t, root, "log", "--oneline", "-3")
	if !strings.Contains(merged, "Merge JOB-001") || !strings.Contains(merged, "Record merge JOB-001") {
		t.Fatalf("merge records missing: %s", merged)
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
