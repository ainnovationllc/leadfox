package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobTemplateDefaults(t *testing.T) {
	role, objective, err := jobTemplateDefaults("verifier")
	if err != nil || role != "verifier" || objective == "" {
		t.Fatalf("unexpected verifier template: role=%q objective=%q err=%v", role, objective, err)
	}
	if _, _, err := jobTemplateDefaults("unknown"); err == nil {
		t.Fatal("unknown template should be rejected")
	}
}

func TestJobLikeCopiesDesignButNotApprovalState(t *testing.T) {
	root := writeProjectFixture(t)
	source := jobPacket{
		ID: "JOB-SOURCE", Status: "accepted", Role: "service", Objective: "source",
		AllowedPaths: []string{"services/**"}, Gates: []string{"local-dev"},
		AcceptanceCriteria: []jobCriterion{{ID: "C1", Statement: "pass", Status: "passed", Evidence: []string{"evidence"}}},
		DurableArtifacts:   []jobDurableArtifact{{ID: "archive", Required: true, Retained: true, RetainedSHA256: strings.Repeat("a", 64)}},
	}
	if err := writeJob(root, source, "# JOB-SOURCE\n"); err != nil {
		t.Fatal(err)
	}
	o := jobOptions{like: "JOB-SOURCE", used: map[string]bool{"like": true}}
	if err := jobNew(root, "JOB-COPY", o); err != nil {
		t.Fatal(err)
	}
	copy, _, err := readJob(root, "JOB-COPY")
	if err != nil {
		t.Fatal(err)
	}
	if copy.Status != "draft" || copy.Role != source.Role || len(copy.AllowedPaths) != 1 {
		t.Fatalf("design metadata was not copied safely: %+v", copy)
	}
	if copy.AcceptanceCriteria[0].Status != "pending" || len(copy.AcceptanceCriteria[0].Evidence) != 0 {
		t.Fatalf("approval evidence leaked into copy: %+v", copy.AcceptanceCriteria)
	}
	if copy.DurableArtifacts[0].Retained {
		t.Fatalf("artifact retention leaked into copy: %+v", copy.DurableArtifacts)
	}
}

func TestJobStatusReportsBlockersWithoutSecrets(t *testing.T) {
	packet := jobPacket{ID: "JOB-STATUS", Status: "draft", Role: "service", Objective: "bounded work", Gates: []string{"local-dev"}, AcceptanceCriteria: []jobCriterion{{ID: "C1", Status: "pending"}}}
	report := buildJobStatus(t.TempDir(), packet)
	if len(report.Blockers) != 3 || report.NextAction != "complete the packet and mark it ready" {
		t.Fatalf("unexpected status report: %+v", report)
	}
	if strings.Contains(strings.Join(report.Blockers, " "), "secret") {
		t.Fatal("status report should not contain secret material")
	}
}

func TestHandoffDraftCreatesBoundedTemplate(t *testing.T) {
	root := writeProjectFixture(t)
	packet := jobPacket{ID: "JOB-HANDOFF", Status: "running", Role: "service", Objective: "bounded change", BaseCommit: "HEAD"}
	if err := writeJob(root, packet, "# JOB-HANDOFF\n"); err != nil {
		t.Fatal(err)
	}
	if err := jobHandoffDraft(root, packet.ID); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "control", "handoffs", packet.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"## Changed paths", "## Gate results", "## Why", "## Recommended next step"} {
		if !strings.Contains(string(content), expected) {
			t.Fatalf("handoff draft missing %q", expected)
		}
	}
}

func TestAppendNewPreservesExistingAndDeduplicates(t *testing.T) {
	result := appendNew([]string{"services/**"}, []string{"services/**", "tests/**", "tests/**"})
	if len(result) != 1 || result[0] != "tests/**" {
		t.Fatalf("unexpected additions: %v", result)
	}
}
