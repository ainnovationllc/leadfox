package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const riskControlsFileName = "control/risk-controls.yaml"

type riskControlsFile struct {
	Version      int                     `yaml:"version"`
	RiskControls []riskControlDefinition `yaml:"risk_controls"`
}

// Projects define the meaning and evidence for each control. Leadfox only
// validates declarations and job references; it does not embed domain rules.
type riskControlDefinition struct {
	ID               string   `yaml:"id"`
	Description      string   `yaml:"description"`
	RequiredEvidence []string `yaml:"required_evidence,omitempty"`
	StopConditions   []string `yaml:"stop_conditions,omitempty"`
}

var riskControlIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func loadRiskControls(root string) (riskControlsFile, error) {
	content, err := os.ReadFile(filepath.Join(root, riskControlsFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return riskControlsFile{}, errors.New("no control/risk-controls.yaml found; initialize or add project risk controls")
		}
		return riskControlsFile{}, fmt.Errorf("read %s: %w", riskControlsFileName, err)
	}
	var document riskControlsFile
	if err := yaml.Unmarshal(content, &document); err != nil {
		return riskControlsFile{}, fmt.Errorf("parse %s: %w", riskControlsFileName, err)
	}
	if err := validateRiskControls(document); err != nil {
		return riskControlsFile{}, err
	}
	return document, nil
}

func validateRiskControls(document riskControlsFile) error {
	if document.Version != 1 {
		return fmt.Errorf("risk controls version must be 1, got %d", document.Version)
	}
	seen := make(map[string]bool, len(document.RiskControls))
	for _, control := range document.RiskControls {
		if !riskControlIDPattern.MatchString(control.ID) {
			return fmt.Errorf("invalid risk control ID %q", control.ID)
		}
		if seen[control.ID] {
			return fmt.Errorf("duplicate risk control ID %q", control.ID)
		}
		seen[control.ID] = true
		if strings.TrimSpace(control.Description) == "" {
			return fmt.Errorf("risk control %q requires a description", control.ID)
		}
		for _, item := range append(append([]string{}, control.RequiredEvidence...), control.StopConditions...) {
			if strings.TrimSpace(item) == "" {
				return fmt.Errorf("risk control %q contains an empty item", control.ID)
			}
		}
	}
	return nil
}

func validateJobRisks(root string, required []string) error {
	if len(required) == 0 {
		return nil
	}
	document, err := loadRiskControls(root)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(document.RiskControls))
	for _, control := range document.RiskControls {
		known[control.ID] = true
	}
	seen := make(map[string]bool, len(required))
	for _, id := range required {
		if !riskControlIDPattern.MatchString(id) {
			return fmt.Errorf("invalid required risk control ID %q", id)
		}
		if seen[id] {
			return fmt.Errorf("duplicate required risk control %q", id)
		}
		seen[id] = true
		if !known[id] {
			return fmt.Errorf("required risk control %q is not declared in %s", id, riskControlsFileName)
		}
	}
	return nil
}

func runRiskCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(riskHelpText)
		return nil
	}
	root, err := gitRoot(".")
	if err != nil {
		return err
	}
	document, err := loadRiskControls(root)
	if err != nil {
		return err
	}
	switch args[0] {
	case "validate":
		if len(args) > 1 {
			return errors.New("risk validate takes no options")
		}
		fmt.Printf("Validated %s (version %d)\n", riskControlsFileName, document.Version)
		return nil
	case "list":
		if len(args) > 1 {
			return errors.New("risk list takes no options")
		}
		controls := append([]riskControlDefinition(nil), document.RiskControls...)
		sort.Slice(controls, func(i, j int) bool { return controls[i].ID < controls[j].ID })
		fmt.Println("ID\tDESCRIPTION")
		for _, control := range controls {
			fmt.Printf("%s\t%s\n", control.ID, control.Description)
		}
		return nil
	default:
		return fmt.Errorf("unknown risk command %q; use 'leadfox risk --help' for usage", args[0])
	}
}

const riskHelpText = `Manage project-defined risk controls.

Usage:
  leadfox risk validate
  leadfox risk list

Leadfox validates job references. The project defines the evidence and stop
conditions that Control must apply during review.
`
