package main

import (
	"os"
	"path/filepath"
	"strings"
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
	created, _, err := applyProjectMigration(root, report)
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

func TestSchemaVersionIsIndependentOfProjectModelVersion(t *testing.T) {
	root := writeProjectFixture(t)
	path := filepath.Join(root, "control", "project.yaml")
	// A project created before leadfox_schema existed, whose Control used
	// project.version for its own architecture revision.
	writeTestFile(t, path, "project:\n  name: example\n  version: 3\n")
	report, err := inspectProjectSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Current != 1 || report.Status != "current" {
		t.Fatalf("project.version was treated as the schema version: %+v", report)
	}
	status, err := buildProjectStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	if status.ModelVersion != 3 {
		t.Fatalf("project model version not reported: %+v", status)
	}

	writeTestFile(t, path, "leadfox_schema: 2\nproject:\n  name: example\n  version: 1\n")
	if report, err := inspectProjectSchema(root); err != nil || report.Status != "unsupported" {
		t.Fatalf("a newer leadfox_schema should be unsupported: %+v %v", report, err)
	}
}

func TestInitDeclaresLeadfoxSchema(t *testing.T) {
	root := writeProjectFixture(t)
	report, err := inspectProjectSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "control", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Current != 1 || report.Status != "current" || !strings.Contains(string(content), "leadfox_schema: 1") || !strings.Contains(string(content), "Leadfox does not interpret it") {
		t.Fatalf("init template does not separate the schema and model versions: %+v\n%s", report, content)
	}
}
