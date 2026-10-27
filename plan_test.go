package main

import "testing"

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

func TestBuildPlanForLTIRegistration(t *testing.T) {
	requirements := []requirement{{
		id:     "REQ-001-lti",
		title:  "LTI registration",
		status: "confirmed",
		body:   "The registration service receives LTI registration requests from the LMS and stores them in PostgreSQL.",
	}}

	result := buildPlan("lti-demo", requirements)

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

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
