package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const planHelpText = `Create a deterministic architecture plan from project requirements.

Usage:
  leadfox plan [options]

Options:
  -n, --dry-run     Show the plan without writing plan files
  -h, --help        Show this help message
`

type planOptions struct {
	dryRun   bool
	showHelp bool
}

type requirement struct {
	id     string
	title  string
	status string
	path   string
	body   string
}

type planResult struct {
	projectName      string
	requirements     []requirement
	services         []string
	dependencies     []string
	dataStores       []string
	entryPoints      []string
	unresolved       []string
	architectureText string
}

func runPlan(args []string) error {
	options, err := parsePlanOptions(args)
	if err != nil {
		return err
	}
	if options.showHelp {
		fmt.Print(planHelpText)
		return nil
	}

	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}
	if _, err := os.Stat(filepath.Join(root, "control", "project.yaml")); err != nil {
		if os.IsNotExist(err) {
			return errors.New("current directory is not a Leadfox project root; run this command from a directory containing control/project.yaml")
		}
		return fmt.Errorf("inspect project metadata: %w", err)
	}

	projectName, err := readProjectName(filepath.Join(root, "control", "project.yaml"))
	if err != nil {
		return err
	}
	requirements, err := readRequirements(filepath.Join(root, "control", "requirements"))
	if err != nil {
		return err
	}
	result := buildPlan(projectName, requirements)
	markdown := renderPlanMarkdown(result)

	if options.dryRun {
		fmt.Print(markdown)
		return nil
	}

	planDir := filepath.Join(root, "control")
	if err := os.WriteFile(filepath.Join(planDir, "architecture-plan.md"), []byte(markdown), 0o644); err != nil {
		return fmt.Errorf("write architecture plan: %w", err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "architecture-plan.yaml"), []byte(renderPlanYAML(result)), 0o644); err != nil {
		return fmt.Errorf("write architecture plan data: %w", err)
	}

	fmt.Printf("Created architecture plan for %s.\n", result.projectName)
	fmt.Printf("Review:\n  %s\n  %s\n", filepath.Join(planDir, "architecture-plan.md"), filepath.Join(planDir, "architecture-plan.yaml"))
	if len(result.unresolved) > 0 {
		fmt.Printf("Unresolved items: %d\n", len(result.unresolved))
	}
	return nil
}

func parsePlanOptions(args []string) (planOptions, error) {
	var options planOptions
	for _, arg := range args {
		switch arg {
		case "-h", "--help":
			options.showHelp = true
		case "-n", "--dry-run":
			options.dryRun = true
		default:
			return options, fmt.Errorf("unknown plan option %q", arg)
		}
	}
	return options, nil
}

func readProjectName(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read project metadata: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "name:") {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
			name = strings.Trim(name, "\"'")
			if name != "" {
				return name, nil
			}
		}
	}
	return "", errors.New("project metadata does not contain project.name")
}

func readRequirements(directory string) ([]requirement, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("no requirements directory found; create control/requirements and add confirmed requirement documents")
		}
		return nil, fmt.Errorf("read requirements directory: %w", err)
	}

	var requirements []requirement
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "README.md" || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read requirement %s: %w", entry.Name(), readErr)
		}
		requirements = append(requirements, parseRequirement(entry.Name(), string(content)))
	}
	if len(requirements) == 0 {
		return nil, errors.New("no requirement documents found; add a Markdown requirement under control/requirements")
	}
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].id < requirements[j].id })
	return requirements, nil
}

func parseRequirement(filename, content string) requirement {
	result := requirement{
		id:     strings.TrimSuffix(filename, filepath.Ext(filename)),
		title:  strings.TrimSuffix(filename, filepath.Ext(filename)),
		status: "active",
		path:   filepath.Join("control", "requirements", filename),
		body:   strings.TrimSpace(content),
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && result.title == strings.TrimSuffix(filename, filepath.Ext(filename)) {
			result.title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "status:") {
			result.status = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(trimmed), "status:"))
		}
	}
	return result
}

func buildPlan(projectName string, requirements []requirement) planResult {
	result := planResult{projectName: projectName, requirements: requirements}
	var combined strings.Builder
	for _, item := range requirements {
		combined.WriteString(strings.ToLower(item.body))
		combined.WriteString("\n")
		if item.status != "active" && item.status != "confirmed" {
			result.unresolved = append(result.unresolved, fmt.Sprintf("%s has status %q and needs confirmation", item.id, item.status))
		}
	}
	text := combined.String()

	if containsAny(text, "lti", "registration", "http", "api") {
		result.services = append(result.services, "registration-service")
		result.entryPoints = append(result.entryPoints, "registration HTTP entry point")
	}
	if containsAny(text, "lms", "canvas", "external platform") {
		result.dependencies = append(result.dependencies, "external LMS platform")
	}
	if containsAny(text, "postgres", "postgresql", "database", "persistent", "store") {
		result.dataStores = append(result.dataStores, "PostgreSQL registration store")
	}
	if containsAny(text, "health", "readiness", "telemetry", "metrics") {
		result.entryPoints = append(result.entryPoints, "operational health and telemetry endpoints")
	}
	if len(result.services) == 0 {
		result.unresolved = append(result.unresolved, "no implementable application service was identified")
	}
	if len(result.dependencies) == 0 {
		result.unresolved = append(result.unresolved, "no external or runtime dependency was identified")
	}
	if len(result.dataStores) == 0 {
		result.unresolved = append(result.unresolved, "no data-store requirement was identified")
	}
	return result
}

func containsAny(value string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func renderPlanMarkdown(result planResult) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Architecture Plan: %s\n\n", result.projectName)
	builder.WriteString("Status: proposed\n\n")
	builder.WriteString("This plan is derived from the active requirement documents. It proposes project-model changes only; it does not implement services or provision infrastructure.\n\n")
	builder.WriteString("## Requirements\n\n")
	for _, item := range result.requirements {
		fmt.Fprintf(&builder, "- `%s` — %s (status: %s)\n", item.id, item.title, item.status)
	}
	builder.WriteString("\n## Proposed services\n\n")
	writeList(&builder, result.services)
	builder.WriteString("\n## Proposed dependencies\n\n")
	writeList(&builder, result.dependencies)
	builder.WriteString("\n## Proposed data stores\n\n")
	writeList(&builder, result.dataStores)
	builder.WriteString("\n## Proposed entry points\n\n")
	writeList(&builder, result.entryPoints)
	builder.WriteString("\n## Environment mapping\n\n")
	builder.WriteString("- Local: use local or emulated dependencies.\n- Ephemeral: use isolated, PR-specific dependencies and settings.\n- Production: use explicitly approved persistent dependencies and settings.\n")
	builder.WriteString("\n## Unresolved items\n\n")
	if len(result.unresolved) == 0 {
		builder.WriteString("- None identified by the initial planner.\n")
	} else {
		writeList(&builder, result.unresolved)
	}
	return builder.String()
}

func renderPlanYAML(result planResult) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "project: %s\nstatus: proposed\nrequirements:\n", result.projectName)
	for _, item := range result.requirements {
		fmt.Fprintf(&builder, "  - id: %s\n    status: %s\n    title: %q\n", item.id, item.status, item.title)
	}
	builder.WriteString("services:\n")
	writeYAMLList(&builder, result.services)
	builder.WriteString("dependencies:\n")
	writeYAMLList(&builder, result.dependencies)
	builder.WriteString("data_stores:\n")
	writeYAMLList(&builder, result.dataStores)
	builder.WriteString("entry_points:\n")
	writeYAMLList(&builder, result.entryPoints)
	builder.WriteString("unresolved:\n")
	writeYAMLList(&builder, result.unresolved)
	return builder.String()
}

func writeList(builder *strings.Builder, values []string) {
	if len(values) == 0 {
		builder.WriteString("- None identified.\n")
		return
	}
	for _, value := range values {
		fmt.Fprintf(builder, "- %s\n", value)
	}
}

func writeYAMLList(builder *strings.Builder, values []string) {
	if len(values) == 0 {
		builder.WriteString("  []\n")
		return
	}
	for _, value := range values {
		fmt.Fprintf(builder, "  - %q\n", value)
	}
}
