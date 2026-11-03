package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type adoptionPlan struct {
	Project     string   `json:"project"`
	Applied     bool     `json:"applied"`
	Directories []string `json:"directories,omitempty"`
	Files       []string `json:"files,omitempty"`
	Preserved   []string `json:"preserved,omitempty"`
	Manifest    string   `json:"manifest"`
}

func runProjectAdopt(args []string) error {
	root, format, yes, err := parseProjectAdoptOptions(args)
	if err != nil {
		return err
	}
	plan, err := planProjectAdoption(root)
	if err != nil {
		return err
	}
	if !yes {
		if format == "json" {
			encoded, marshalErr := json.MarshalIndent(plan, "", "  ")
			if marshalErr != nil {
				return marshalErr
			}
			fmt.Println(string(encoded))
		} else {
			printAdoptionPlan(plan)
			fmt.Println("Dry run only; repeat with --yes to create missing files.")
		}
		return nil
	}
	if err := applyProjectAdoption(&plan); err != nil {
		return err
	}
	if format == "json" {
		encoded, marshalErr := json.MarshalIndent(plan, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(encoded))
	} else {
		printAdoptionPlan(plan)
	}
	return nil
}

func parseProjectAdoptOptions(args []string) (string, string, bool, error) {
	if len(args) == 0 {
		return "", "", false, errors.New("a project path is required; use 'leadfox project adopt PATH'")
	}
	root := ""
	format := "markdown"
	yes := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--yes":
			yes = true
		case "--format":
			if index+1 >= len(args) {
				return "", "", false, errors.New("--format requires markdown or json")
			}
			index++
			format = args[index]
			if format != "markdown" && format != "json" {
				return "", "", false, fmt.Errorf("unsupported project report format %q", format)
			}
		default:
			if strings.HasPrefix(args[index], "-") {
				return "", "", false, fmt.Errorf("unknown project adoption option %q", args[index])
			}
			if root != "" {
				return "", "", false, errors.New("only one project path may be provided")
			}
			root = args[index]
		}
	}
	abs, err := filepath.Abs(root)
	return abs, format, yes, err
}

func planProjectAdoption(root string) (adoptionPlan, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return adoptionPlan{}, fmt.Errorf("inspect project: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return adoptionPlan{}, errors.New("project adoption requires an existing, real directory")
	}
	projectName := filepath.Base(root)
	files := projectFiles(projectName)
	directories := []string{
		"control/settings", "control/requirements", "control/jobs", "control/handoffs", "control/audit", "control/evidence",
		"skills/leadfox-control-plan-review", "services", "tests",
		"infrastructure/terraform", "contracts", "docs",
	}
	plan := adoptionPlan{Project: root, Manifest: "control/leadfox-adoption.yaml"}
	for _, directory := range directories {
		if _, err := os.Stat(filepath.Join(root, directory)); errors.Is(err, fs.ErrNotExist) {
			plan.Directories = append(plan.Directories, directory)
		} else if err != nil {
			return adoptionPlan{}, fmt.Errorf("inspect %s: %w", directory, err)
		} else {
			plan.Preserved = append(plan.Preserved, directory+"/")
		}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if _, err := os.Lstat(filepath.Join(root, path)); errors.Is(err, fs.ErrNotExist) {
			plan.Files = append(plan.Files, path)
		} else if err != nil {
			return adoptionPlan{}, fmt.Errorf("inspect %s: %w", path, err)
		} else {
			plan.Preserved = append(plan.Preserved, path)
		}
	}
	claude := filepath.Join(root, "CLAUDE.md")
	if _, err := os.Lstat(claude); errors.Is(err, fs.ErrNotExist) {
		plan.Files = append(plan.Files, "CLAUDE.md -> AGENTS.md")
	} else if err != nil {
		return adoptionPlan{}, fmt.Errorf("inspect CLAUDE.md: %w", err)
	} else {
		plan.Preserved = append(plan.Preserved, "CLAUDE.md")
	}
	return plan, nil
}

func applyProjectAdoption(plan *adoptionPlan) error {
	for _, directory := range plan.Directories {
		if err := os.MkdirAll(filepath.Join(plan.Project, directory), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", directory, err)
		}
	}
	files := projectFiles(filepath.Base(plan.Project))
	for _, path := range plan.Files {
		if path == "CLAUDE.md -> AGENTS.md" {
			if err := os.Symlink("AGENTS.md", filepath.Join(plan.Project, "CLAUDE.md")); err != nil {
				return fmt.Errorf("create CLAUDE.md: %w", err)
			}
			continue
		}
		content, ok := files[path]
		if !ok {
			continue
		}
		fullPath := filepath.Join(plan.Project, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return fmt.Errorf("create parent for %s: %w", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	manifestPath := filepath.Join(plan.Project, plan.Manifest)
	if _, err := os.Lstat(manifestPath); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
			return err
		}
		content := adoptionManifest(plan)
		if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write adoption record: %w", err)
		}
		plan.Files = append(plan.Files, plan.Manifest)
	} else if err != nil {
		return fmt.Errorf("inspect adoption record: %w", err)
	}
	plan.Applied = true
	return nil
}

func adoptionManifest(plan *adoptionPlan) string {
	var builder strings.Builder
	builder.WriteString("version: 1\nsource: leadfox project adopt\nadopted_at: ")
	builder.WriteString(time.Now().UTC().Format(time.RFC3339))
	builder.WriteString("\ncreated:\n")
	for _, path := range plan.Directories {
		fmt.Fprintf(&builder, "  - %s/\n", path)
	}
	for _, path := range plan.Files {
		fmt.Fprintf(&builder, "  - %s\n", path)
	}
	return builder.String()
}

func printAdoptionPlan(plan adoptionPlan) {
	if plan.Applied {
		fmt.Printf("Project adopted: %s\n", plan.Project)
	} else {
		fmt.Printf("Adoption plan: %s\n", plan.Project)
	}
	if len(plan.Directories) > 0 {
		fmt.Println("Directories to create:")
		for _, path := range plan.Directories {
			fmt.Printf("- %s/\n", path)
		}
	}
	if len(plan.Files) > 0 {
		fmt.Println("Files to create:")
		for _, path := range plan.Files {
			fmt.Printf("- %s\n", path)
		}
	}
	if len(plan.Preserved) > 0 {
		fmt.Println("Preserved existing paths:")
		for _, path := range plan.Preserved {
			fmt.Printf("- %s\n", path)
		}
	}
}
