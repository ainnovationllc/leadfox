package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDecisionPolicy(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, decisionPolicyFileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDecisionPolicyLoadsAndSeparatesAuthority(t *testing.T) {
	root := writeDecisionPolicy(t, `version: 1
decision_policy:
  automatic: [local_tests]
  control_with_record: [agent_assignment]
  developer_approval: [cloud_resources]
  escalation_triggers: [public_exposure]
`)
	document, err := loadDecisionPolicy(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.DecisionPolicy.Automatic) != 1 || len(document.DecisionPolicy.DeveloperApproval) != 1 {
		t.Fatalf("unexpected policy: %#v", document)
	}
}

func TestDecisionPolicyRejectsDuplicateActions(t *testing.T) {
	document := decisionPolicyFile{
		Version: 1,
		DecisionPolicy: decisionPolicy{
			Automatic:         []string{"cloud_resources"},
			DeveloperApproval: []string{"cloud_resources"},
		},
	}
	if err := validateDecisionPolicy(document); err == nil || !strings.Contains(err.Error(), "appears in both") {
		t.Fatalf("expected duplicate action error, got %v", err)
	}
}

func TestDecisionPolicyRejectsInvalidVersionAndAction(t *testing.T) {
	if err := validateDecisionPolicy(decisionPolicyFile{Version: 2}); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("expected version error, got %v", err)
	}
	invalid := decisionPolicyFile{Version: 1, DecisionPolicy: decisionPolicy{Automatic: []string{"Needs Approval"}}}
	if err := validateDecisionPolicy(invalid); err == nil || !strings.Contains(err.Error(), "invalid decision action") {
		t.Fatalf("expected action error, got %v", err)
	}
}
