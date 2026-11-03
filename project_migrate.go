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
	Action         string   `json:"action"`
}

type projectMigrationRecord struct {
	Version    int      `yaml:"version"`
	Source     string   `yaml:"source"`
	From       int      `yaml:"from_version"`
	To         int      `yaml:"to_version"`
	MigratedAt string   `yaml:"migrated_at"`
	Created    []string `yaml:"created,omitempty"`
}

func runProjectSchemaOperation(operation string, args []string) error {
	root, format, yes, err := parseProjectSchemaOptions(operation, args)
	if err != nil {
		return err
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
	if len(report.MissingManaged) == 0 {
		report.Action = "no-op"
		return printProjectSchema(report, format)
	}
	created, err := applyProjectMigration(root, report)
	if err != nil {
		return err
	}
	report.Action, report.Status = "migrated", "current"
	report.MissingManaged = created
	return printProjectSchema(report, format)
}

func parseProjectSchemaOptions(operation string, args []string) (string, string, bool, error) {
	root, format, yes := ".", "markdown", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--yes":
			yes = true
		case "--format":
			if i+1 >= len(args) || (args[i+1] != "markdown" && args[i+1] != "json") {
				return "", "", false, errors.New("--format requires markdown or json")
			}
			format, i = args[i+1], i+1
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", false, fmt.Errorf("unknown project %s option %q", operation, args[i])
			}
			if root != "." {
				return "", "", false, errors.New("only one project path may be provided")
			}
			root = args[i]
		}
	}
	abs, err := filepath.Abs(root)
	return abs, format, yes, err
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
	report.Name, report.Current = metadata.Project.Name, metadata.Project.Version
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
	for _, path := range []string{"control/decisions.yaml"} {
		if _, statErr := os.Stat(filepath.Join(root, path)); errors.Is(statErr, os.ErrNotExist) {
			report.MissingManaged = append(report.MissingManaged, path)
		} else if statErr != nil {
			return report, fmt.Errorf("inspect %s: %w", path, statErr)
		}
	}
	if len(report.MissingManaged) > 0 && report.Status == "current" {
		report.Status = "migration-required"
	}
	return report, nil
}

func applyProjectMigration(root string, report projectSchemaReport) ([]string, error) {
	files := projectFiles(report.Name)
	created := []string{}
	for _, relative := range report.MissingManaged {
		content, ok := files[relative]
		if !ok {
			return nil, fmt.Errorf("no managed migration content for %s", relative)
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", relative, err)
		}
		created = append(created, relative)
	}
	recordPath := filepath.Join(root, "control", "leadfox-migration.yaml")
	if _, err := os.Stat(recordPath); errors.Is(err, os.ErrNotExist) {
		record := projectMigrationRecord{Version: 1, Source: "leadfox project migrate", From: report.Current, To: report.Supported, MigratedAt: time.Now().UTC().Format(time.RFC3339), Created: created}
		encoded, marshalErr := yaml.Marshal(record)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if err := os.WriteFile(recordPath, encoded, 0o644); err != nil {
			return nil, fmt.Errorf("write migration record: %w", err)
		}
		created = append(created, "control/leadfox-migration.yaml")
	} else if err != nil {
		return nil, err
	}
	return created, nil
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
	if report.Status == "migration-required" && report.Action == "dry-run" {
		fmt.Println("Dry run only; repeat with --yes to migrate missing managed files.")
	}
	return nil
}
