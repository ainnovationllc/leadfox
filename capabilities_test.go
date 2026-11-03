package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCapabilityConfig(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, capabilityFileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadCapabilitiesAndValidateRequirements(t *testing.T) {
	root := writeCapabilityConfig(t, `capabilities:
  - id: design-mcp
    kind: mcp
    provider: host-configured
    tools: [read_design, export_assets]
    access: read-only
    authentication: user
    network: true
    output_paths: [services/web/src]
    fallback: stop
`)

	document, err := loadCapabilities(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Capabilities) != 1 || document.Capabilities[0].ID != "design-mcp" {
		t.Fatalf("unexpected capabilities: %#v", document.Capabilities)
	}
	if err := validateJobCapabilities(root, []string{"design-mcp"}); err != nil {
		t.Fatal(err)
	}
}

func TestCapabilityValidationRejectsUnknownAndUnsafeDefinitions(t *testing.T) {
	root := writeCapabilityConfig(t, `capabilities:
  - id: design-mcp
    kind: mcp
    provider: host-configured
`)
	if err := validateJobCapabilities(root, []string{"missing"}); err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("expected unknown capability error, got %v", err)
	}

	unsafeRoot := writeCapabilityConfig(t, `capabilities:
  - id: design-mcp
    kind: mcp
    provider: host-configured
    output_paths: [../outside]
`)
	if _, err := loadCapabilities(unsafeRoot); err == nil || !strings.Contains(err.Error(), "unsafe output path") {
		t.Fatalf("expected unsafe path error, got %v", err)
	}
}

func TestCapabilityDefinitionsRejectDuplicateAndInvalidAccess(t *testing.T) {
	duplicate := []agentCapability{{ID: "design-mcp", Kind: "mcp", Provider: "host"}, {ID: "design-mcp", Kind: "mcp", Provider: "host"}}
	if err := validateCapabilityDefinitions(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	invalidAccess := []agentCapability{{ID: "design-mcp", Kind: "mcp", Provider: "host", Access: "admin"}}
	if err := validateCapabilityDefinitions(invalidAccess); err == nil || !strings.Contains(err.Error(), "unsupported access") {
		t.Fatalf("expected access error, got %v", err)
	}
}
