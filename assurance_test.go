package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssuranceReportDistinguishesCoverageAndHashFailures(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control", "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	evidencePath := filepath.Join(root, "control", "evidence", "gate.yaml")
	if err := os.WriteFile(evidencePath, []byte("verified evidence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := `frameworks:
  - id: soc2
    name: SOC 2
controls:
  - id: CC6.1
    framework: soc2
    objective: Access is authorized.
    owner: Control
    evidence:
      - ref: control/evidence/gate.yaml
        type: gate
        required: true
  - id: CC7.2
    framework: soc2
    objective: Events are monitored.
    evidence:
      - ref: control/evidence/missing.yaml
        type: external
        required: true
`
	if err := os.WriteFile(filepath.Join(root, "control", "assurance.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := buildAssuranceReport(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "incomplete" || report.Complete != 1 || report.Incomplete != 1 {
		t.Fatalf("unexpected assurance summary: %+v", report)
	}
	if report.Controls[0].Coverage != "complete" || report.Controls[0].Evidence[0].Status != "present" {
		t.Fatalf("present evidence was not reported correctly: %+v", report.Controls[0])
	}
	if report.Controls[1].Coverage != "incomplete" || !strings.Contains(strings.Join(report.Controls[1].Missing, ","), "missing.yaml") {
		t.Fatalf("missing evidence was not reported correctly: %+v", report.Controls[1])
	}
}

func TestAssuranceEvidenceHashVerification(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "control", "evidence.txt"), []byte("actual\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := inspectAssuranceEvidence(root, assuranceEvidence{Ref: "control/evidence.txt", Required: true, SHA256: strings.Repeat("0", 64)})
	if result.Status != "hash-mismatch" || result.SHA256 == "" {
		t.Fatalf("hash mismatch was not detected: %+v", result)
	}
}

func TestAssuranceEvidenceCannotEscapeWorktree(t *testing.T) {
	root := t.TempDir()
	result := inspectAssuranceEvidence(root, assuranceEvidence{Ref: "../outside.txt", Required: true})
	if result.Status != "invalid-reference" {
		t.Fatalf("path escape was not rejected: %+v", result)
	}
}
