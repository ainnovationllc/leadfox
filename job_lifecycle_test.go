package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAttestationReplacementAndFailureRules(t *testing.T) {
	automatic := jobAttestation{Gate: "local-dev", Check: "scope", Result: "passed", Actor: "Leadfox"}
	explicit := jobAttestation{Gate: "local-dev", Check: "scope", Result: "passed", Actor: "Control", Note: "confirmed"}
	attestations := appendAttestation([]jobAttestation{automatic}, explicit)
	if len(attestations) != 1 || attestations[0].Actor != "Control" || attestations[0].Note != "confirmed" {
		t.Fatalf("explicit attestation did not replace automatic placeholder: %+v", attestations)
	}
	attestations = append(attestations, jobAttestation{Gate: "local-dev", Check: "scope", Result: "failed", Actor: "Control"})
	if hasAttestation(attestations, "local-dev", "scope") {
		t.Fatal("failed attestation did not invalidate a prior passing attestation")
	}
	if !hasFailedAttestation(attestations, "local-dev", "scope") {
		t.Fatal("failed attestation was not detected")
	}
}

func TestJobOptionsRejectUnsupportedFlags(t *testing.T) {
	options, _, err := parseJobOptions([]string{"JOB-001", "--commit", "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateJobOptions("start", options); err == nil || !strings.Contains(err.Error(), "job start does not accept --commit") {
		t.Fatalf("unsupported option was not rejected: %v", err)
	}
	options, _, err = parseJobOptions([]string{"JOB-001", "--commit", "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateJobOptions("scope", options); err != nil {
		t.Fatalf("supported scope option was rejected: %v", err)
	}
}

func TestJobScopeUsesExplicitCommit(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-b", "main")
	gitTest(t, root, "config", "user.email", "leadfox@example.test")
	gitTest(t, root, "config", "user.name", "Leadfox Test")
	if err := os.MkdirAll(filepath.Join(root, "control", "jobs", "JOB-SCOPE"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "services", "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	packetPath := jobPath(root, "JOB-SCOPE")
	packet := "---\nid: JOB-SCOPE\nstatus: running\nallowed_paths:\n  - services/example/**\n---\n# JOB-SCOPE\n"
	if err := os.WriteFile(packetPath, []byte(packet), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "services", "example", "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "initial")
	base := strings.TrimSpace(gitTest(t, root, "rev-parse", "HEAD"))
	updatedPacket := packet[:len(packet)-len("---\n# JOB-SCOPE\n")] + "base_commit: " + base + "\nworktree: " + root + "\n---\n# JOB-SCOPE\n"
	if err := os.WriteFile(packetPath, []byte(updatedPacket), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", packetPath)
	gitTest(t, root, "commit", "-m", "record base")
	if err := os.WriteFile(filepath.Join(root, "services", "example", "service.go"), []byte("package example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "allowed change")
	allowedCommit := strings.TrimSpace(gitTest(t, root, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(root, "outside.txt"), []byte("out of scope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "out of scope change")
	if err := jobScope(root, "JOB-SCOPE", jobOptions{commit: allowedCommit}); err != nil {
		t.Fatalf("explicit commit was not used for scope: %v", err)
	}
	if err := jobScope(root, "JOB-SCOPE", jobOptions{}); err == nil {
		t.Fatal("scope ignored the branch head's out-of-scope change")
	}
}

func TestJobReissuePreservesCriteriaAndHistory(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-b", "main")
	gitTest(t, root, "config", "user.email", "leadfox@example.test")
	gitTest(t, root, "config", "user.name", "Leadfox Test")
	if err := os.MkdirAll(filepath.Join(root, "control", "jobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	packet := `---
id: JOB-OLD
status: ready
role: integration
objective: Preserve this bounded objective.
base_branch: main
allowed_paths:
  - services/example/**
gates:
  - local-dev
---
# JOB-OLD

Carry the acceptance criteria forward.
`
	if err := os.WriteFile(jobPath(root, "JOB-OLD"), []byte(packet), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "initial")
	if err := jobReissue(root, "JOB-OLD", "JOB-NEW", jobOptions{actor: "Control", reason: "new base required"}); err != nil {
		t.Fatalf("reissue job: %v", err)
	}
	old, _, err := readJob(root, "JOB-OLD")
	if err != nil {
		t.Fatal(err)
	}
	newJob, newBody, err := readJob(root, "JOB-NEW")
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != "cancelled" || old.SupersededBy != "JOB-NEW" {
		t.Fatalf("old job was not superseded: %+v", old)
	}
	if newJob.Status != "draft" || newJob.Supersedes != "JOB-OLD" || newJob.Objective != "Preserve this bounded objective." || len(newJob.Gates) != 1 {
		t.Fatalf("new job did not preserve criteria: %+v", newJob)
	}
	if !strings.Contains(newBody, "Supersedes `JOB-OLD`") {
		t.Fatalf("reissue provenance missing from body: %s", newBody)
	}
}

func TestJobSyncUsesSynchronizedBaseForScope(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-b", "main")
	gitTest(t, root, "config", "user.email", "leadfox@example.test")
	gitTest(t, root, "config", "user.name", "Leadfox Test")
	if err := os.MkdirAll(filepath.Join(root, "control", "jobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	packet := `---
id: JOB-SYNC
status: ready
role: service
objective: Test synchronization.
base_branch: main
allowed_paths:
  - services/example/**
---
# JOB-SYNC
`
	if err := os.WriteFile(jobPath(root, "JOB-SYNC"), []byte(packet), 0o644); err != nil {
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
	jobWorktree := filepath.Join(root, "..", "job-sync-worktree")
	if err := jobStart(root, "JOB-SYNC", jobOptions{worktree: jobWorktree}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "base.md"), []byte("base update\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "advance base")
	baseHead := strings.TrimSpace(gitTest(t, root, "rev-parse", "HEAD"))
	if err := jobSync(root, "JOB-SYNC", jobOptions{actor: "Control", reason: "bring job current"}); err != nil {
		t.Fatalf("sync job: %v", err)
	}
	updated, _, err := readJob(root, "JOB-SYNC")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ScopeBaseCommit != baseHead || len(updated.Syncs) != 1 || updated.Syncs[0].BaseHead != baseHead {
		t.Fatalf("synchronization metadata is wrong: %+v", updated)
	}
	if err := jobScope(root, "JOB-SYNC", jobOptions{}); err != nil {
		t.Fatalf("base changes were incorrectly included in scope: %v", err)
	}
}

func TestScopeAttestationDoesNotApproveOtherManualChecks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "gates:\n  - id: integration\n    checks:\n      - id: scope\n        manual: true\n      - id: review-note\n        manual: true\n"
	if err := os.WriteFile(filepath.Join(root, "control", "gates.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	attestations := appendScopeAttestations(root, jobPacket{Gates: []string{"integration"}})
	if len(attestations) != 1 || attestations[0].Check != "scope" {
		t.Fatalf("unexpected automatic attestations: %+v", attestations)
	}
}

func TestEvidenceOutputLinesPreserveLeadingBlankLines(t *testing.T) {
	document := gateEvidenceDocument{
		SchemaVersion: evidenceSchemaVersion,
		Job:           "JOB-OUTPUT",
		Round:         1,
		Commit:        "abc",
		Result: gateResult{Checks: []checkResult{{
			ID:          "probe",
			Output:      "Cg p",
			OutputLines: []string{"", "", "  probe: readable", ""},
		}}},
	}
	encoded, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var decoded gateEvidenceDocument
	if err := yaml.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("evidence YAML did not round-trip: %v\n%s", err, encoded)
	}
	if len(decoded.Result.Checks[0].OutputLines) != 4 || decoded.Result.Checks[0].OutputLines[2] != "  probe: readable" {
		t.Fatalf("output lines were not readable after round-trip: %#v", decoded.Result.Checks[0].OutputLines)
	}
}

func TestReviewEvidenceStateDistinguishesPendingAndFailedManualChecks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control", "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	evidencePath := filepath.Join("control", "evidence", "JOB-STATE.yaml")
	document := gateEvidenceDocument{
		SchemaVersion: evidenceSchemaVersion,
		Job:           "JOB-STATE",
		Round:         1,
		Result: gateResult{Gate: "local-dev", Status: "manual", Checks: []checkResult{
			{ID: "review-note", Status: "manual"},
		}},
		Attestations: []jobAttestation{{Gate: "local-dev", Check: "review-note", Result: "passed"}},
	}
	encoded, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(evidencePath)), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	state := reviewEvidenceState(root, []jobGateEvidence{{Gate: "local-dev", Evidence: evidencePath}})
	if state.Pending || state.Failed {
		t.Fatalf("passed manual check incorrectly remained unresolved: %+v", state)
	}
	document.Attestations = nil
	encoded, err = yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(evidencePath)), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	state = reviewEvidenceState(root, []jobGateEvidence{{Gate: "local-dev", Evidence: evidencePath}})
	if !state.Pending || state.Failed {
		t.Fatalf("unattested manual check was not pending: %+v", state)
	}
	document.Attestations = []jobAttestation{{Gate: "local-dev", Check: "review-note", Result: "failed"}}
	document.Result.Status = "failed"
	encoded, err = yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(evidencePath)), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	state = reviewEvidenceState(root, []jobGateEvidence{{Gate: "local-dev", Evidence: evidencePath}})
	if state.Pending || !state.Failed {
		t.Fatalf("failed manual check was not failed: %+v", state)
	}
}

func TestLifecycleTransitionsPreserveHistory(t *testing.T) {
	packet := jobPacket{Status: "running"}
	appendTransition(&packet, "blocked", "waiting for review", "Control", "2026-10-29T00:00:00Z")
	packet.Status = "blocked"
	appendTransition(&packet, "running", "dependency available", "Control", "2026-10-29T00:01:00Z")
	packet.Status = "running"
	appendTransition(&packet, "cancelled", "superseded", "Control", "2026-10-29T00:02:00Z")
	if len(packet.Transitions) != 3 {
		t.Fatalf("transition history was not preserved: %+v", packet.Transitions)
	}
	if packet.Transitions[0].From != "running" || packet.Transitions[0].To != "blocked" || packet.Transitions[1].From != "blocked" || packet.Transitions[2].To != "cancelled" {
		t.Fatalf("unexpected transition history: %+v", packet.Transitions)
	}
}

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
	if err := os.WriteFile(filepath.Join(root, "control", "project.yaml"), []byte("project:\n  name: test\n  commit_trailers:\n    - \"Co-Authored-By: Test Agent <agent@example.test>\"\n"), 0o644); err != nil {
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
	if len(jobCopy.Transitions) != 1 || jobCopy.Transitions[0].From != "ready" || jobCopy.Transitions[0].To != "running" {
		t.Fatalf("job start transition was not recorded: %+v", jobCopy.Transitions)
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
	if len(packetAfterReview.Transitions) < 2 || packetAfterReview.Transitions[1].From != "running" || packetAfterReview.Transitions[1].To != "review" {
		t.Fatalf("review transition was not recorded: %+v", packetAfterReview.Transitions)
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
	mergeBody := gitTest(t, root, "show", "-s", "--format=%B", mergedPacket.MergeCommit)
	if !strings.Contains(mergeBody, "Leadfox-Project: test") || !strings.Contains(mergeBody, "Co-Authored-By: Test Agent <agent@example.test>") {
		t.Fatalf("configured lifecycle trailer missing: %s", mergeBody)
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
