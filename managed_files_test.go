package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestManagedFileVersionsRecorded fails when a managed document changes
// without its hash history being updated; without that history, existing
// projects could not tell an unedited old copy from a local edit.
func TestManagedFileVersionsRecorded(t *testing.T) {
	for _, file := range updatableManagedFiles {
		if got := contentSHA256([]byte(file.Content())); got != file.Current {
			t.Errorf("%s changed: move %s into Previous and set Current to %s in managed_files.go", file.Path, file.Current, got)
		}
		seen := map[string]bool{file.Current: true}
		for _, previous := range file.Previous {
			if seen[previous] {
				t.Errorf("%s lists hash %s twice (or as both current and previous)", file.Path, previous)
			}
			seen[previous] = true
		}
	}
}

// withPreviousProtocol registers content as an earlier built-in version of
// the runtime protocol for the duration of a test.
func withPreviousProtocol(t *testing.T, content string) {
	t.Helper()
	saved := updatableManagedFiles
	files := append([]updatableManagedFile(nil), saved...)
	for index := range files {
		if files[index].Path == agentRuntimeProtocolPath {
			files[index].Previous = append(append([]string(nil), files[index].Previous...), contentSHA256([]byte(content)))
		}
	}
	updatableManagedFiles = files
	t.Cleanup(func() { updatableManagedFiles = saved })
}

func readMigrationRecord(t *testing.T, root string) projectMigrationRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "control", "leadfox-migration.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var record projectMigrationRecord
	if err := yaml.Unmarshal(content, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestMigrateUpdatesUnmodifiedOutdatedProtocol(t *testing.T) {
	root := writeProjectFixture(t)
	protocol := filepath.Join(root, filepath.FromSlash(agentRuntimeProtocolPath))
	oldVersion := "# Job runtime protocol\n\nAn earlier Leadfox version.\n"
	withPreviousProtocol(t, oldVersion)
	writeTestFile(t, protocol, oldVersion)

	report, err := inspectProjectSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "migration-required" || !containsProjectString(report.OutdatedManaged, agentRuntimeProtocolPath) || len(report.ModifiedManaged) != 0 {
		t.Fatalf("outdated protocol not detected: %+v", report)
	}
	check, err := checkProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(check.Warnings, "\n"), agentRuntimeProtocolPath+" is an unmodified copy of an earlier Leadfox version") {
		t.Fatalf("project check does not warn about the outdated protocol: %v", check.Warnings)
	}

	created, updated, err := applyProjectMigration(root, report)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0] != agentRuntimeProtocolPath || containsProjectString(created, agentRuntimeProtocolPath) {
		t.Fatalf("protocol not reported as updated: created %v updated %v", created, updated)
	}
	if content, _ := os.ReadFile(protocol); string(content) != agentRuntimeProtocolDoc {
		t.Fatal("outdated protocol was not replaced with the built-in version")
	}
	record := readMigrationRecord(t, root)
	if !containsProjectString(record.Updated, agentRuntimeProtocolPath) || record.UpdatedAt == "" {
		t.Fatalf("migration record does not note the update: %+v", record)
	}

	// A second migration of the same project changes nothing.
	again, err := inspectProjectSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != "current" || len(again.OutdatedManaged) != 0 {
		t.Fatalf("project not current after the update: %+v", again)
	}
	if check, _ := checkProject(root); strings.Contains(strings.Join(check.Warnings, "\n"), "earlier Leadfox version") {
		t.Fatalf("warning persists after the update: %v", check.Warnings)
	}

	// A record written before updates existed keeps its earlier fields.
	writeTestFile(t, protocol, oldVersion)
	report, _ = inspectProjectSchema(root)
	if _, _, err := applyProjectMigration(root, report); err != nil {
		t.Fatal(err)
	}
	if after := readMigrationRecord(t, root); after.MigratedAt != record.MigratedAt || len(after.Updated) != 1 {
		t.Fatalf("existing migration record rewritten incorrectly: %+v", after)
	}
}

func TestMigrateNeverOverwritesEditedProtocol(t *testing.T) {
	root := writeProjectFixture(t)
	protocol := filepath.Join(root, filepath.FromSlash(agentRuntimeProtocolPath))
	edited := agentRuntimeProtocolDoc + "\nProject rule: run gates inside the dev container.\n"
	writeTestFile(t, protocol, edited)

	report, err := inspectProjectSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "current" || !containsProjectString(report.ModifiedManaged, agentRuntimeProtocolPath) || len(report.OutdatedManaged) != 0 {
		t.Fatalf("edited protocol not reported as modified: %+v", report)
	}
	if _, updated, err := applyProjectMigration(root, report); err != nil || len(updated) != 0 {
		t.Fatalf("edited protocol was updated: %v %v", updated, err)
	}
	if content, _ := os.ReadFile(protocol); string(content) != edited {
		t.Fatal("migration overwrote a locally edited protocol")
	}
	if check, _ := checkProject(root); strings.Contains(strings.Join(check.Warnings, "\n"), "earlier Leadfox version") {
		t.Fatalf("project check warns about an edited copy: %v", check.Warnings)
	}

	// An outdated copy edited between inspection and apply is left alone.
	oldVersion := "# Job runtime protocol\n\nAn earlier Leadfox version.\n"
	withPreviousProtocol(t, oldVersion)
	writeTestFile(t, protocol, oldVersion)
	report, _ = inspectProjectSchema(root)
	writeTestFile(t, protocol, oldVersion+"edited after inspection\n")
	if _, updated, err := applyProjectMigration(root, report); err != nil || len(updated) != 0 {
		t.Fatalf("file edited after inspection was updated: %v %v", updated, err)
	}
}

func TestMigratePrintShowsBuiltInDocument(t *testing.T) {
	root := writeProjectFixture(t)
	if err := runProjectSchemaOperation("migrate", []string{root, "--print", agentRuntimeProtocolPath}); err != nil {
		t.Fatal(err)
	}
	if err := runProjectSchemaOperation("migrate", []string{root, "--print", "control/decisions.yaml"}); err == nil {
		t.Fatal("--print accepted a file that is not a managed document")
	}
	if err := runProjectSchemaOperation("migrate", []string{root, "--print", agentRuntimeProtocolPath, "--yes"}); err == nil {
		t.Fatal("--print combined with --yes")
	}
	if err := runProjectSchemaOperation("schema", []string{root, "--print", agentRuntimeProtocolPath}); err == nil {
		t.Fatal("--print accepted outside project migrate")
	}
}
