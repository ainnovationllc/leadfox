package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProjectFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range projectFiles("example") {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestProjectStatusSummarizesProjectWithoutExecutingAnything(t *testing.T) {
	root := writeProjectFixture(t)
	status, err := buildProjectStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	if status.Name != "example" || status.Structure != "valid" || status.Intake != "incomplete" {
		t.Fatalf("unexpected project status: %+v", status)
	}
	if status.Policy != "valid" || status.DecisionCount != 0 {
		t.Fatalf("unexpected governance status: %+v", status)
	}
}

func TestDecisionLedgerRequiresDecidersForFinalStatuses(t *testing.T) {
	err := validateDecisionLedger(decisionFile{Version: 1, Decisions: []projectDecision{{
		ID: "cloud-access", Title: "Cloud access", Action: "cloud_resources", Status: "approved",
	}}})
	if err == nil {
		t.Fatal("expected approved decision without decider to be rejected")
	}
	valid := decisionFile{Version: 1, Decisions: []projectDecision{{
		ID: "cloud-access", Title: "Cloud access", Action: "cloud_resources", Status: "approved", DecidedBy: "owner",
	}}}
	if err := validateDecisionLedger(valid); err != nil {
		t.Fatal(err)
	}
}

func TestDecisionCommandsRecordAndApprove(t *testing.T) {
	root := writeProjectFixture(t)
	if err := runDecisionCommand([]string{"add", "cloud-access", "--project", root, "--title", "Cloud access", "--action", "cloud_resources", "--rationale", "Needed for the approved pilot"}); err != nil {
		t.Fatal(err)
	}
	if err := runDecisionCommand([]string{"approve", "cloud-access", "--project", root, "--by", "owner", "--reason", "Approved for the pilot only"}); err != nil {
		t.Fatal(err)
	}
	ledger, err := loadDecisionLedger(root)
	if err != nil {
		t.Fatal(err)
	}
	decision, found := findDecision(ledger.Decisions, "cloud-access")
	if !found || decision.Status != "approved" || decision.DecidedBy != "owner" {
		t.Fatalf("unexpected recorded decision: %+v", decision)
	}
}

func TestContextExportIncludesOnlyDecisionMetadata(t *testing.T) {
	root := writeProjectFixture(t)
	if err := runDecisionCommand([]string{"add", "secret-review", "--project", root, "--title", "Review secret handling", "--action", "secrets", "--rationale", "Do not include secret values"}); err != nil {
		t.Fatal(err)
	}
	ledger, err := loadDecisionLedger(root)
	if err != nil {
		t.Fatal(err)
	}
	context := projectContextReport{GeneratedBy: "test", Decisions: []contextDecision{{ID: ledger.Decisions[0].ID, Action: ledger.Decisions[0].Action, Status: ledger.Decisions[0].Status}}}
	encoded, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || strings.Contains(string(encoded), "secret-value") {
		t.Fatalf("unexpected context content: %s", encoded)
	}
}
