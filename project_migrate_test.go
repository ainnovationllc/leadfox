package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectSchemaReportsMissingManagedFiles(t *testing.T) {
	root := writeProjectFixture(t)
	if err := os.Remove(filepath.Join(root, "control", "decisions.yaml")); err != nil {
		t.Fatal(err)
	}
	report, err := inspectProjectSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "migration-required" || len(report.MissingManaged) != 1 {
		t.Fatalf("unexpected schema report: %+v", report)
	}
}

func TestProjectMigrationIsIdempotentAndPreservesExistingFiles(t *testing.T) {
	root := writeProjectFixture(t)
	if err := os.Remove(filepath.Join(root, "control", "decisions.yaml")); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(root, "AGENTS.md")
	original, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	report, err := inspectProjectSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	created, err := applyProjectMigration(root, report)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 2 {
		t.Fatalf("expected decisions and migration record, got %v", created)
	}
	if migrated, _ := os.ReadFile(agents); string(migrated) != string(original) {
		t.Fatal("migration overwrote an existing project file")
	}
	repeat, err := inspectProjectSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	if repeat.Status != "current" || len(repeat.MissingManaged) != 0 {
		t.Fatalf("migration was not idempotent: %+v", repeat)
	}
}

func TestDoctorReportsIncompleteIntakeAsWarning(t *testing.T) {
	root := writeProjectFixture(t)
	report := buildDiagnosticReport(root)
	if report.Status != "warnings" {
		t.Fatalf("expected warning-only doctor result, got %+v", report)
	}
	found := false
	for _, finding := range report.Findings {
		if finding.Code == "intake.incomplete" && finding.Severity == "warning" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing intake warning: %+v", report.Findings)
	}
}
