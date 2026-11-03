package main

import (
	"strings"
	"testing"
)

func TestJobReadinessRequiresReadyStatusAndBoundedScope(t *testing.T) {
	root := writeProjectFixture(t)
	packet := jobPacket{ID: "JOB-001", Status: "draft", Role: "service", Objective: "Implement a bounded change", AllowedPaths: []string{"services/example/**"}}
	report := inspectJobReadiness(root, packet)
	if report.Valid || !inspectionHasCode(report.Findings, "status") {
		t.Fatalf("expected draft job to be not ready: %+v", report)
	}
	packet.Status = "ready"
	if report := inspectJobReadiness(root, packet); !report.Valid {
		t.Fatalf("ready bounded job should pass: %+v", report)
	}
}

func TestJobReadinessRejectsOverlappingAndUnsafePaths(t *testing.T) {
	root := writeProjectFixture(t)
	report := inspectJobReadiness(root, jobPacket{ID: "JOB-002", Status: "ready", Role: "service", Objective: "change", AllowedPaths: []string{"services/**"}, ExcludedPaths: []string{"services/**", "../outside"}})
	if report.Valid || !inspectionHasCode(report.Findings, "scope-overlap") || !inspectionHasCode(report.Findings, "unsafe-path") {
		t.Fatalf("expected scope findings: %+v", report)
	}
}

func TestJobContextContainsBoundedInputsOnly(t *testing.T) {
	context := jobContextFromPacket(jobPacket{ID: "JOB-003", Status: "running", Role: "integration", Objective: "Run the bounded checks", AllowedPaths: []string{"tests/**"}, AcceptanceCriteria: []jobCriterion{{ID: "C1"}}})
	if context.Job != "JOB-003" || len(context.AllowedPaths) != 1 || len(context.AcceptanceIDs) != 1 || !context.StopBeforeReview {
		t.Fatalf("unexpected bounded context: %+v", context)
	}
}

func TestJobEvidenceValidationAllowsDraftButRequiresReviewedHandoff(t *testing.T) {
	root := writeProjectFixture(t)
	if report := inspectJobEvidence(root, jobPacket{ID: "JOB-004", Status: "draft"}); !report.Valid {
		t.Fatalf("draft evidence validation should not require review evidence: %+v", report)
	}
	packet := jobPacket{ID: "JOB-004", Status: "review", ReviewCommit: "abc123"}
	report := inspectJobEvidence(root, packet)
	if report.Valid || !inspectionHasCode(report.Findings, "handoff") {
		t.Fatalf("review without handoff should fail: %+v", report)
	}
}

func TestDurableArtifactValidationBlocksUnretainedRequiredArtifacts(t *testing.T) {
	packet := jobPacket{DurableArtifacts: []jobDurableArtifact{{ID: "archive", Required: true, SHA256: strings.Repeat("a", 64)}}}
	if err := validateDurableArtifacts(packet); err == nil {
		t.Fatal("expected unretained required artifact to block cleanup")
	}
	packet.DurableArtifacts[0].Retained = true
	packet.DurableArtifacts[0].RetainedSHA256 = strings.Repeat("a", 64)
	if err := validateDurableArtifacts(packet); err != nil {
		t.Fatal(err)
	}
}

func TestChangeImpactHelpersIdentifyApprovalSignals(t *testing.T) {
	body := "- Existing services affected: web\n- Proposed jobs: JOB-1\n"
	if labelValue(body, "Existing services affected") != "web" || labelValue(body, "Proposed jobs") != "JOB-1" {
		t.Fatal("expected change labels to be extracted")
	}
	if !containsInspectionString([]string{"security-sensitive"}, "security-sensitive") {
		t.Fatal("expected impact signal helper to find a signal")
	}
}

func inspectionHasCode(findings []inspectionFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
