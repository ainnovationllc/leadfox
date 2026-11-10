package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJobHoldBlocksUntilReleased(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control", "changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "control", "changes", "CHANGE-001.md")
	if err := os.WriteFile(path+".workflow.yaml", []byte("version: 1\nhold_points:\n  - id: owner-review\n    status: open\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	packet := jobPacket{ID: "JOB-001", ChangePlan: "control/changes/CHANGE-001.md", HoldPoint: "owner-review"}
	if err := ensureJobHoldReleased(root, packet); err == nil {
		t.Fatal("open hold should block the job")
	}
	if err := os.WriteFile(path+".workflow.yaml", []byte("version: 1\nhold_points:\n  - id: owner-review\n    status: released\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureJobHoldReleased(root, packet); err != nil {
		t.Fatal(err)
	}
}

func TestRepeatedGateEvidenceChecksEveryRun(t *testing.T) {
	packet := jobPacket{Attestations: []jobAttestation{{Gate: "local-dev", Check: "scope", Result: "passed"}}}
	result := gateResult{
		Gate:   "local-dev",
		Status: "passed",
		Checks: []checkResult{{ID: "scope", Status: "manual"}},
		Runs: []gateResult{
			{Gate: "local-dev", Status: "passed", Checks: []checkResult{{ID: "scope", Status: "manual"}}},
			{Gate: "local-dev", Status: "failed", Checks: []checkResult{{ID: "tests", Status: "failed"}}},
		},
	}
	if gateResultVerified(result, "local-dev", packet) {
		t.Fatal("repeated gate evidence hid a failed later run")
	}
	if got := gateResultReason(result, "local-dev", packet); got != "tests failed" {
		t.Fatalf("unexpected repeated gate failure reason: %q", got)
	}
}

func TestWorktreeEnvironmentCreatesDeclaredLinkAndProtectsMarker(t *testing.T) {
	root, worktree := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".local"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "links:\n  - source: .local\n    target: .local\nresources:\n  - id: db\n    marker: .leadfox/resources/db.running\n"
	if err := os.WriteFile(filepath.Join(root, worktreeConfigPath), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyWorktreeEnvironment(root, worktree); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(worktree, ".local")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("declared link was not created: info=%v err=%v", info, err)
	}
	marker := filepath.Join(worktree, ".leadfox", "resources")
	if err := os.MkdirAll(marker, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marker, "db.running"), []byte("active\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureNoActiveWorktreeResources(root, worktree); err == nil {
		t.Fatal("active resource marker should block cleanup")
	}
}
