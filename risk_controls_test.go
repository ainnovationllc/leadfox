package main

import "testing"

func TestValidateRiskControls(t *testing.T) {
	document := riskControlsFile{Version: 1, RiskControls: []riskControlDefinition{{
		ID:               "security-sensitive",
		Description:      "Requires positive and negative security evidence.",
		RequiredEvidence: []string{"positive-control", "negative-controls"},
		StopConditions:   []string{"clean baseline missing"},
	}}}
	if err := validateRiskControls(document); err != nil {
		t.Fatalf("valid risk controls rejected: %v", err)
	}
	for _, broken := range []riskControlsFile{
		{Version: 2, RiskControls: document.RiskControls},
		{Version: 1, RiskControls: []riskControlDefinition{{ID: "bad id", Description: "x"}}},
		{Version: 1, RiskControls: []riskControlDefinition{{ID: "security-sensitive", Description: "x"}, {ID: "security-sensitive", Description: "y"}}},
		{Version: 1, RiskControls: []riskControlDefinition{{ID: "empty-description"}}},
	} {
		if err := validateRiskControls(broken); err == nil {
			t.Errorf("invalid risk controls unexpectedly accepted: %#v", broken)
		}
	}
}

func TestValidateJobRisks(t *testing.T) {
	root := t.TempDir()
	if err := validateJobRisks(root, nil); err != nil {
		t.Fatalf("empty risk requirements should be allowed: %v", err)
	}
	if err := validateJobRisks(root, []string{"security-sensitive"}); err == nil {
		t.Fatal("missing risk registry unexpectedly accepted")
	}
}
