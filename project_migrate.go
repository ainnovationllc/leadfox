package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const currentProjectSchemaVersion = 1

type projectSchemaReport struct {
	Project        string   `json:"project"`
	Name           string   `json:"name,omitempty"`
	Current        int      `json:"current_version"`
	Supported      int      `json:"supported_version"`
	Status         string   `json:"status"`
	MissingManaged []string `json:"missing_managed,omitempty"`
	// OutdatedManaged are unmodified copies of an earlier built-in version;
	// migrate --yes replaces them.
	OutdatedManaged []string `json:"outdated_managed,omitempty"`
	// ModifiedManaged differ from every built-in version (local edits);
	// migrate never replaces them.
	ModifiedManaged []string `json:"modified_managed,omitempty"`
	UpdatedManaged  []string `json:"updated_managed,omitempty"`
	Action          string   `json:"action"`
}

type projectMigrationRecord struct {
	Version    int      `yaml:"version"`
	Source     string   `yaml:"source"`
	From       int      `yaml:"from_version"`
	To         int      `yaml:"to_version"`
	MigratedAt string   `yaml:"migrated_at"`
	Created    []string `yaml:"created,omitempty"`
	Updated    []string `yaml:"updated,omitempty"`
	UpdatedAt  string   `yaml:"updated_at,omitempty"`
}

func runProjectSchemaOperation(operation string, args []string) error {
	root, format, yes, printPath, err := parseProjectSchemaOptions(operation, args)
	if err != nil {
		return err
	}
	if printPath != "" {
		file, ok := findUpdatableManagedFile(printPath)
		if !ok {
			return fmt.Errorf("--print accepts a Leadfox-managed document: %s", agentRuntimeProtocolPath)
		}
		fmt.Print(file.Content())
		return nil
	}
	report, err := inspectProjectSchema(root)
	if err != nil {
		return err
	}
	if operation == "schema" {
		return printProjectSchema(report, format)
	}
	if report.Status == "unsupported" {
		return fmt.Errorf("project schema version %d is newer than Leadfox supports (version %d)", report.Current, report.Supported)
	}
	if !yes {
		report.Action = "dry-run"
		return printProjectSchema(report, format)
	}
	if len(report.MissingManaged) == 0 && len(report.OutdatedManaged) == 0 {
		report.Action = "no-op"
		return printProjectSchema(report, format)
	}
	created, updated, err := applyProjectMigration(root, report)
	if err != nil {
		return err
	}
	report.Action, report.Status = "migrated", "current"
	report.MissingManaged, report.OutdatedManaged, report.UpdatedManaged = created, nil, updated
	return printProjectSchema(report, format)
}

func parseProjectSchemaOptions(operation string, args []string) (string, string, bool, string, error) {
	root, format, yes, printPath := ".", "markdown", false, ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--yes":
			yes = true
		case "--format":
			if i+1 >= len(args) || (args[i+1] != "markdown" && args[i+1] != "json") {
				return "", "", false, "", errors.New("--format requires markdown or json")
			}
			format, i = args[i+1], i+1
		case "--print":
			if operation != "migrate" || i+1 >= len(args) {
				return "", "", false, "", errors.New("--print PATH applies to project migrate and needs a managed document path")
			}
			printPath, i = args[i+1], i+1
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", false, "", fmt.Errorf("unknown project %s option %q", operation, args[i])
			}
			if root != "." {
				return "", "", false, "", errors.New("only one project path may be provided")
			}
			root = args[i]
		}
	}
	if printPath != "" && yes {
		return "", "", false, "", errors.New("--print cannot be combined with --yes")
	}
	abs, err := filepath.Abs(root)
	return abs, format, yes, printPath, err
}

func inspectProjectSchema(root string) (projectSchemaReport, error) {
	report := projectSchemaReport{Project: root, Supported: currentProjectSchemaVersion, Action: "none"}
	content, err := os.ReadFile(filepath.Join(root, "control", "project.yaml"))
	if err != nil {
		return report, fmt.Errorf("read project metadata: %w", err)
	}
	var metadata projectMetadata
	if err := yaml.Unmarshal(content, &metadata); err != nil {
		return report, fmt.Errorf("parse project metadata: %w", err)
	}
	// The schema version is leadfox_schema, not project.version: the latter
	// belongs to the project (for example, its architecture plan revision).
	report.Name, report.Current = metadata.Project.Name, metadata.LeadfoxSchema
	if report.Current == 0 {
		report.Current = 1
	}
	switch {
	case report.Current > currentProjectSchemaVersion:
		report.Status = "unsupported"
	case report.Current < currentProjectSchemaVersion:
		report.Status = "migration-required"
	default:
		report.Status = "current"
	}
	for _, path := range []string{"control/decisions.yaml", agentRuntimeProtocolPath} {
		if _, statErr := os.Stat(filepath.Join(root, path)); errors.Is(statErr, os.ErrNotExist) {
			report.MissingManaged = append(report.MissingManaged, path)
		} else if statErr != nil {
			return report, fmt.Errorf("inspect %s: %w", path, statErr)
		}
	}
	for _, file := range updatableManagedFiles {
		state, stateErr := file.state(root)
		if stateErr != nil {
			return report, stateErr
		}
		switch state {
		case managedOutdated:
			report.OutdatedManaged = append(report.OutdatedManaged, file.Path)
		case managedModified:
			report.ModifiedManaged = append(report.ModifiedManaged, file.Path)
		}
	}
	if (len(report.MissingManaged) > 0 || len(report.OutdatedManaged) > 0) && report.Status == "current" {
		report.Status = "migration-required"
	}
	return report, nil
}

func applyProjectMigration(root string, report projectSchemaReport) (created, updated []string, err error) {
	files := projectFiles(report.Name)
	created = []string{}
	for _, relative := range report.MissingManaged {
		content, ok := files[relative]
		if !ok {
			return nil, nil, fmt.Errorf("no managed migration content for %s", relative)
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, nil, fmt.Errorf("write %s: %w", relative, err)
		}
		created = append(created, relative)
	}
	for _, relative := range report.OutdatedManaged {
		file, ok := findUpdatableManagedFile(relative)
		if !ok {
			return nil, nil, fmt.Errorf("no managed update content for %s", relative)
		}
		// Re-check immediately before writing so a file edited since the
		// inspection is never overwritten.
		if state, stateErr := file.state(root); stateErr != nil {
			return nil, nil, stateErr
		} else if state != managedOutdated {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(relative)), []byte(file.Content()), 0o644); err != nil {
			return nil, nil, fmt.Errorf("update %s: %w", relative, err)
		}
		updated = append(updated, relative)
	}
	recordPath := filepath.Join(root, "control", "leadfox-migration.yaml")
	now := time.Now().UTC().Format(time.RFC3339)
	existing, readErr := os.ReadFile(recordPath)
	switch {
	case errors.Is(readErr, os.ErrNotExist):
		record := projectMigrationRecord{Version: 1, Source: "leadfox project migrate", From: report.Current, To: report.Supported, MigratedAt: now, Created: created}
		if len(updated) > 0 {
			record.Updated, record.UpdatedAt = updated, now
		}
		if err := writeMigrationRecord(recordPath, record); err != nil {
			return nil, nil, err
		}
		created = append(created, "control/leadfox-migration.yaml")
	case readErr != nil:
		return nil, nil, readErr
	case len(updated) > 0:
		var record projectMigrationRecord
		if err := yaml.Unmarshal(existing, &record); err != nil {
			return nil, nil, fmt.Errorf("parse migration record: %w", err)
		}
		for _, path := range updated {
			if !containsProjectString(record.Updated, path) {
				record.Updated = append(record.Updated, path)
			}
		}
		record.UpdatedAt = now
		if err := writeMigrationRecord(recordPath, record); err != nil {
			return nil, nil, err
		}
	}
	return created, updated, nil
}

func writeMigrationRecord(path string, record projectMigrationRecord) error {
	encoded, err := yaml.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("write migration record: %w", err)
	}
	return nil
}

func printProjectSchema(report projectSchemaReport, format string) error {
	if format == "json" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Printf("Project: %s\nSchema: %d (supported %d)\nStatus: %s\nAction: %s\n", report.Name, report.Current, report.Supported, report.Status, report.Action)
	if len(report.MissingManaged) > 0 {
		fmt.Println("Missing managed files:")
		for _, path := range report.MissingManaged {
			fmt.Printf("- %s\n", path)
		}
	}
	if len(report.OutdatedManaged) > 0 {
		fmt.Println("Outdated managed files (unmodified copies of an earlier Leadfox version; --yes updates them):")
		for _, path := range report.OutdatedManaged {
			fmt.Printf("- %s\n", path)
		}
	}
	if len(report.UpdatedManaged) > 0 {
		fmt.Println("Updated managed files:")
		for _, path := range report.UpdatedManaged {
			fmt.Printf("- %s\n", path)
		}
	}
	if len(report.ModifiedManaged) > 0 {
		fmt.Println("Locally modified managed files (never updated automatically; compare with leadfox project migrate --print PATH):")
		for _, path := range report.ModifiedManaged {
			fmt.Printf("- %s\n", path)
		}
	}
	if report.Status == "migration-required" && report.Action == "dry-run" {
		fmt.Println("Dry run only; repeat with --yes to add missing and update outdated managed files.")
	}
	return nil
}
