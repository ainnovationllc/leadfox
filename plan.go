package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const planHelpText = `Create a deterministic architecture plan from project requirements.

Usage:
  leadfox plan [options]

Options:
  -n, --dry-run     Show the plan without writing plan files
  -h, --help        Show this help message

The planner preserves approved project-model entries and reports new ideas as proposals.
`

type planOptions struct {
	dryRun   bool
	showHelp bool
}

type requirement struct {
	id          string
	title       string
	status      string
	statusNote  string
	statusError string
	path        string
	body        string
}

type approvedModel struct {
	projectName  string
	services     []string
	dependencies []string
	dataStores   []string
	entryPoints  []string
}

type requirementTrace struct {
	requirementID string
	services      []string
	dependencies  []string
	dataStores    []string
	entryPoints   []string
}

type planResult struct {
	projectName          string
	requirements         []requirement
	services             []string
	dependencies         []string
	dataStores           []string
	entryPoints          []string
	proposedServices     []string
	proposedDependencies []string
	proposedDataStores   []string
	proposedEntryPoints  []string
	proposals            []string
	traces               []requirementTrace
	unresolved           []string
	deferred             []string
	conflicts            []string
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

	projectPath := filepath.Join(root, "control", "project.yaml")
	model, err := readApprovedModel(projectPath)
	if err != nil {
		return err
	}
	requirements, err := readRequirements(filepath.Join(root, "control", "requirements"))
	if err != nil {
		return err
	}
	result := buildPlanFromModel(model, requirements)
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
	if len(result.conflicts) > 0 {
		fmt.Printf("Planner conflicts: %d\n", len(result.conflicts))
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
	name, _, err := readProjectModel(path)
	return name, err
}

func readProjectModel(path string) (string, []string, error) {
	model, err := readApprovedModel(path)
	if err != nil {
		return "", nil, err
	}
	return model.projectName, model.dataStores, nil
}

func readApprovedModel(path string) (approvedModel, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return approvedModel{}, fmt.Errorf("read project metadata: %w", err)
	}
	var document map[string]interface{}
	if err := yaml.Unmarshal(content, &document); err != nil {
		return approvedModel{}, fmt.Errorf("parse project metadata: %w", err)
	}
	project, _ := document["project"].(map[string]interface{})
	model := approvedModel{
		projectName:  nestedString(project, "name"),
		services:     yamlStringList(document["services"]),
		dependencies: yamlStringList(document["external_dependencies"]),
		dataStores:   yamlStringList(document["data_stores"]),
		entryPoints:  yamlStringList(document["entry_points"]),
	}
	if model.projectName == "" {
		return approvedModel{}, errors.New("project metadata does not contain project.name")
	}
	return model, nil
}

func nestedString(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func yamlStringList(value interface{}) []string {
	var result []string
	items, ok := value.([]interface{})
	if !ok {
		if single, ok := value.(string); ok && strings.TrimSpace(single) != "" {
			return []string{strings.TrimSpace(single)}
		}
		return nil
	}
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				result = appendUnique(result, strings.TrimSpace(typed))
			}
		case map[string]interface{}:
			for _, key := range []string{"name", "id", "path", "route", "endpoint"} {
				if text := nestedString(typed, key); text != "" {
					result = appendUnique(result, text)
					break
				}
			}
		}
	}
	return result
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
		requirement := parseRequirement(entry.Name(), string(content))
		if requirement.statusError != "" {
			return nil, fmt.Errorf("invalid status in %s: %s; expected one of draft, active, confirmed, deferred, rejected", entry.Name(), requirement.statusError)
		}
		requirements = append(requirements, requirement)
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
			value := strings.TrimSpace(trimmed[len("status:"):])
			fields := strings.Fields(value)
			if len(fields) == 0 {
				result.statusError = "status is empty"
				continue
			}
			result.status = strings.ToLower(strings.Trim(fields[0], "`\"'"))
			if !knownRequirementStatus(result.status) {
				result.statusError = fmt.Sprintf("unknown status %q", result.status)
				continue
			}
			result.statusNote = strings.TrimSpace(strings.TrimLeft(value[len(fields[0]):], " -—:()"))
		}
	}
	return result
}

func knownRequirementStatus(status string) bool {
	switch status {
	case "draft", "active", "confirmed", "deferred", "rejected":
		return true
	default:
		return false
	}
}

func buildPlan(projectName string, requirements []requirement, approvedDataStores []string) planResult {
	return buildPlanFromModel(approvedModel{projectName: projectName, dataStores: approvedDataStores}, requirements)
}

func buildPlanFromModel(model approvedModel, requirements []requirement) planResult {
	result := planResult{
		projectName:  model.projectName,
		requirements: requirements,
		services:     append([]string(nil), model.services...),
		dependencies: append([]string(nil), model.dependencies...),
		dataStores:   append([]string(nil), model.dataStores...),
		entryPoints:  append([]string(nil), model.entryPoints...),
	}
	var combined strings.Builder
	for _, item := range requirements {
		status := strings.ToLower(item.status)
		if status == "deferred" {
			result.deferred = append(result.deferred, item.id)
			continue
		}
		if status != "active" && status != "confirmed" {
			result.unresolved = append(result.unresolved, fmt.Sprintf("%s has status %q and needs confirmation", item.id, item.status))
			continue
		}
		combined.WriteString(strings.ToLower(item.body))
		combined.WriteString("\n")
	}
	text := combined.String()

	if containsAny(text, "lti", "registration", "http", "api") {
		if len(result.services) == 0 {
			result.services = appendUnique(result.services, "registration-service")
			result.proposedServices = appendUnique(result.proposedServices, "registration-service")
			result.proposals = append(result.proposals, "A registration service was inferred; confirm or rename it in control/project.yaml.")
		}
		if len(result.entryPoints) == 0 {
			result.entryPoints = appendUnique(result.entryPoints, "registration HTTP entry point")
			result.proposedEntryPoints = appendUnique(result.proposedEntryPoints, "registration HTTP entry point")
			result.proposals = append(result.proposals, "A registration HTTP entry point was inferred; confirm its route and method in control/project.yaml.")
		}
	}
	if containsAny(text, "lms", "canvas", "external platform") {
		if len(result.dependencies) == 0 {
			result.dependencies = appendUnique(result.dependencies, "external LMS platform")
			result.proposedDependencies = appendUnique(result.proposedDependencies, "external LMS platform")
			result.proposals = append(result.proposals, "An external LMS dependency was inferred; identify it explicitly in control/project.yaml.")
		}
	}
	if containsAny(text, "postgres", "postgresql", "database", "persistent", "store") {
		if len(result.dataStores) == 0 {
			result.conflicts = append(result.conflicts, "active requirements imply a data store, but the approved project model declares data_stores: []")
			result.proposals = append(result.proposals, "A data store is implied but was not invented; declare its approved type and lifecycle in control/project.yaml.")
		}
	}
	if containsAny(text, "health", "readiness", "telemetry", "metrics") {
		if len(model.entryPoints) == 0 {
			result.entryPoints = appendUnique(result.entryPoints, "operational health and telemetry endpoints")
			result.proposedEntryPoints = appendUnique(result.proposedEntryPoints, "operational health and telemetry endpoints")
		}
	}
	if len(result.services) == 0 {
		result.unresolved = append(result.unresolved, "no implementable application service was identified")
	}
	if len(result.dependencies) == 0 {
		result.unresolved = append(result.unresolved, "no external or runtime dependency was identified")
	}
	for _, item := range requirements {
		if item.status != "active" && item.status != "confirmed" {
			continue
		}
		trace := requirementTrace{requirementID: item.id, services: append([]string(nil), result.services...), dependencies: append([]string(nil), result.dependencies...), entryPoints: append([]string(nil), result.entryPoints...)}
		if containsAny(strings.ToLower(item.body), "postgres", "postgresql", "database", "persistent", "store") {
			trace.dataStores = append([]string(nil), result.dataStores...)
		}
		result.traces = append(result.traces, trace)
	}
	return result
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
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
		if item.statusNote != "" {
			fmt.Fprintf(&builder, "- `%s` — %s (status: %s; note: %s)\n", item.id, item.title, item.status, item.statusNote)
		} else {
			fmt.Fprintf(&builder, "- `%s` — %s (status: %s)\n", item.id, item.title, item.status)
		}
	}
	builder.WriteString("\n## Project model used by planner\n\n")
	builder.WriteString("### Services\n\n")
	writeList(&builder, result.services)
	builder.WriteString("\n### Dependencies\n\n")
	writeList(&builder, result.dependencies)
	builder.WriteString("\n### Data stores\n\n")
	writeList(&builder, result.dataStores)
	builder.WriteString("\n### Entry points\n\n")
	writeList(&builder, result.entryPoints)
	builder.WriteString("\n## Proposed additions\n\n")
	if len(result.proposals) == 0 && len(result.proposedServices) == 0 && len(result.proposedDependencies) == 0 && len(result.proposedDataStores) == 0 && len(result.proposedEntryPoints) == 0 {
		builder.WriteString("- None identified.\n")
	} else {
		for _, proposal := range result.proposals {
			fmt.Fprintf(&builder, "- %s\n", proposal)
		}
		writeProposedList(&builder, "services", result.proposedServices)
		writeProposedList(&builder, "dependencies", result.proposedDependencies)
		writeProposedList(&builder, "data stores", result.proposedDataStores)
		writeProposedList(&builder, "entry points", result.proposedEntryPoints)
	}
	builder.WriteString("\n## Requirement traceability\n\n")
	if len(result.traces) == 0 {
		builder.WriteString("- None identified.\n")
	} else {
		for _, trace := range result.traces {
			fmt.Fprintf(&builder, "- `%s`: services=%s; dependencies=%s; data stores=%s; entry points=%s\n", trace.requirementID, formatTraceValues(trace.services), formatTraceValues(trace.dependencies), formatTraceValues(trace.dataStores), formatTraceValues(trace.entryPoints))
		}
	}
	builder.WriteString("\n## Deferred requirements\n\n")
	writeList(&builder, result.deferred)
	builder.WriteString("\n## Planner conflicts\n\n")
	writeList(&builder, result.conflicts)
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
	builder.WriteString("proposed_additions:\n  services:\n")
	writeYAMLListIndented(&builder, result.proposedServices)
	builder.WriteString("  dependencies:\n")
	writeYAMLListIndented(&builder, result.proposedDependencies)
	builder.WriteString("  data_stores:\n")
	writeYAMLListIndented(&builder, result.proposedDataStores)
	builder.WriteString("  entry_points:\n")
	writeYAMLListIndented(&builder, result.proposedEntryPoints)
	builder.WriteString("requirement_traceability:\n")
	for _, trace := range result.traces {
		fmt.Fprintf(&builder, "  - requirement: %q\n    services:\n", trace.requirementID)
		writeYAMLListIndented(&builder, trace.services)
		builder.WriteString("    dependencies:\n")
		writeYAMLListIndented(&builder, trace.dependencies)
		builder.WriteString("    data_stores:\n")
		writeYAMLListIndented(&builder, trace.dataStores)
		builder.WriteString("    entry_points:\n")
		writeYAMLListIndented(&builder, trace.entryPoints)
	}
	builder.WriteString("deferred_requirements:\n")
	writeYAMLList(&builder, result.deferred)
	builder.WriteString("conflicts:\n")
	writeYAMLList(&builder, result.conflicts)
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

func writeProposedList(builder *strings.Builder, label string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(builder, "- Proposed %s: %s\n", label, formatTraceValues(values))
}

func formatTraceValues(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
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

func writeYAMLListIndented(builder *strings.Builder, values []string) {
	if len(values) == 0 {
		builder.WriteString("      []\n")
		return
	}
	for _, value := range values {
		fmt.Fprintf(builder, "      - %q\n", value)
	}
}
