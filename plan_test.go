package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseRequirement(t *testing.T) {
	requirement := parseRequirement("REQ-001-lti.md", "# Dynamic registration\n\nStatus: confirmed\n\nThe LMS sends registration requests.")

	if requirement.id != "REQ-001-lti" {
		t.Fatalf("unexpected requirement ID: %q", requirement.id)
	}
	if requirement.title != "Dynamic registration" {
		t.Fatalf("unexpected requirement title: %q", requirement.title)
	}
	if requirement.status != "confirmed" {
		t.Fatalf("unexpected requirement status: %q", requirement.status)
	}
}

func TestParseRequirementPreservesStatusNote(t *testing.T) {
	requirement := parseRequirement("REQ-003.md", "# Future state\n\nStatus: deferred — revisit after persistence is approved\n")

	if requirement.status != "deferred" {
		t.Fatalf("unexpected requirement status: %q", requirement.status)
	}
	if requirement.statusNote != "revisit after persistence is approved" {
		t.Fatalf("unexpected status note: %q", requirement.statusNote)
	}
	if requirement.statusError != "" {
		t.Fatalf("unexpected status error: %q", requirement.statusError)
	}
}

func TestReadRequirementsRejectsUnknownStatus(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "REQ-001.md"), []byte("# Requirement\n\nStatus: maybe\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := readRequirements(directory)
	if err == nil || !strings.Contains(err.Error(), `unknown status "maybe"`) {
		t.Fatalf("expected clear unknown-status error, got %v", err)
	}
}

func TestBuildPlanForLTIRegistration(t *testing.T) {
	requirements := []requirement{{
		id:     "REQ-001-lti",
		title:  "LTI registration",
		status: "confirmed",
		body:   "The registration service receives LTI registration requests from the LMS and stores them in PostgreSQL.",
	}}

	result := buildPlan("lti-demo", requirements, []string{"PostgreSQL registration store"})

	if !contains(result.services, "registration-service") {
		t.Fatal("expected registration service")
	}
	if !contains(result.dependencies, "external LMS platform") {
		t.Fatal("expected external LMS dependency")
	}
	if !contains(result.dataStores, "PostgreSQL registration store") {
		t.Fatal("expected PostgreSQL data store")
	}
	if len(result.unresolved) != 0 {
		t.Fatalf("unexpected unresolved items: %v", result.unresolved)
	}
}

func TestBuildPlanExcludesDeferredRequirements(t *testing.T) {
	requirements := []requirement{{
		id:     "FR-001-registration",
		title:  "Registration",
		status: "active",
		body:   "The service receives LTI registration requests from the LMS.",
	}, {
		id:     "FR-003-login-and-token-state",
		title:  "Login and token state",
		status: "deferred",
		body:   "Persist login and token state in PostgreSQL.",
	}}

	result := buildPlan("lti-demo", requirements, nil)

	if len(result.deferred) != 1 || result.deferred[0] != "FR-003-login-and-token-state" {
		t.Fatalf("expected deferred requirement, got %v", result.deferred)
	}
	if len(result.unresolved) != 0 {
		t.Fatalf("deferred requirement was reported as unresolved: %v", result.unresolved)
	}
	if len(result.dataStores) != 0 {
		t.Fatalf("deferred requirement introduced a data store: %v", result.dataStores)
	}
}

func TestBuildPlanReportsDataStoreConflict(t *testing.T) {
	requirements := []requirement{{
		id:     "FR-001-registration",
		title:  "Registration",
		status: "active",
		body:   "The service stores registration information.",
	}}

	result := buildPlan("lti-demo", requirements, nil)

	if len(result.dataStores) != 0 {
		t.Fatalf("unexpected proposed data store: %v", result.dataStores)
	}
	if len(result.conflicts) != 1 {
		t.Fatalf("expected one data-store conflict, got %v", result.conflicts)
	}
}

func TestBuildPlanPreservesApprovedModel(t *testing.T) {
	model := approvedModel{
		projectName:  "lti-demo",
		services:     []string{"maxwell"},
		dependencies: []string{"Canvas test LMS"},
		dataStores:   []string{"PostgreSQL registration store"},
		entryPoints:  []string{"POST /lti/register"},
	}
	requirements := []requirement{{
		id:     "REQ-001-lti",
		status: "confirmed",
		body:   "The service receives LTI registration requests from the LMS and stores them in PostgreSQL.",
	}}

	result := buildPlanFromModel(model, requirements)
	if !contains(result.services, "maxwell") || contains(result.services, "registration-service") {
		t.Fatalf("approved service model was not preserved: %v", result.services)
	}
	if !contains(result.dependencies, "Canvas test LMS") || contains(result.dependencies, "external LMS platform") {
		t.Fatalf("approved dependency model was not preserved: %v", result.dependencies)
	}
	if !contains(result.entryPoints, "POST /lti/register") || contains(result.entryPoints, "registration HTTP entry point") {
		t.Fatalf("approved entry-point model was not preserved: %v", result.entryPoints)
	}
	if len(result.proposals) != 0 {
		t.Fatalf("unexpected proposals for complete approved model: %v", result.proposals)
	}
	if len(result.traces) != 1 || result.traces[0].requirementID != "REQ-001-lti" {
		t.Fatalf("expected requirement traceability, got %+v", result.traces)
	}
}

func TestRenderPlanIncludesStructuredAdditionsAndTraceability(t *testing.T) {
	result := buildPlan("demo", []requirement{{
		id:     "REQ-001",
		status: "active",
		body:   "The API receives registration requests from an LMS.",
	}}, nil)
	markdown := renderPlanMarkdown(result)
	if !strings.Contains(markdown, "## Proposed additions") || !strings.Contains(markdown, "## Requirement traceability") {
		t.Fatalf("planner output lacks structured sections:\n%s", markdown)
	}
	if !strings.Contains(renderPlanYAML(result), "proposed_additions:") {
		t.Fatal("planner YAML lacks proposed additions")
	}
	var document map[string]interface{}
	if err := yaml.Unmarshal([]byte(renderPlanYAML(result)), &document); err != nil {
		t.Fatalf("planner YAML is invalid: %v", err)
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
