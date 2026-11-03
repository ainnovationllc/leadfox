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

const decisionPolicyFileName = "control/decision-policy.yaml"

type decisionPolicyFile struct {
	Version        int            `yaml:"version"`
	DecisionPolicy decisionPolicy `yaml:"decision_policy"`
}

// decisionPolicy describes the authority granted to Control for one project.
// The action names are project-defined; Leadfox validates their shape and
// separation but does not silently grant authority for external side effects.
type decisionPolicy struct {
	Automatic          []string `yaml:"automatic,omitempty" json:"automatic,omitempty"`
	ControlWithRecord  []string `yaml:"control_with_record,omitempty" json:"control_with_record,omitempty"`
	DeveloperApproval  []string `yaml:"developer_approval,omitempty" json:"developer_approval,omitempty"`
	EscalationTriggers []string `yaml:"escalation_triggers,omitempty" json:"escalation_triggers,omitempty"`
}

var decisionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func loadDecisionPolicy(root string) (decisionPolicyFile, error) {
	path := filepath.Join(root, decisionPolicyFileName)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return decisionPolicyFile{}, errors.New("no control/decision-policy.yaml found; initialize or add a project decision policy")
		}
		return decisionPolicyFile{}, fmt.Errorf("read %s: %w", decisionPolicyFileName, err)
	}
	var document decisionPolicyFile
	if err := yaml.Unmarshal(content, &document); err != nil {
		return decisionPolicyFile{}, fmt.Errorf("parse %s: %w", decisionPolicyFileName, err)
	}
	if err := validateDecisionPolicy(document); err != nil {
		return decisionPolicyFile{}, err
	}
	return document, nil
}

func validateDecisionPolicy(document decisionPolicyFile) error {
	if document.Version != 1 {
		return fmt.Errorf("decision policy version must be 1, got %d", document.Version)
	}
	groups := map[string][]string{
		"automatic":           document.DecisionPolicy.Automatic,
		"control_with_record": document.DecisionPolicy.ControlWithRecord,
		"developer_approval":  document.DecisionPolicy.DeveloperApproval,
		"escalation_triggers": document.DecisionPolicy.EscalationTriggers,
	}
	seen := make(map[string]string)
	for group, actions := range groups {
		for _, action := range actions {
			action = strings.TrimSpace(action)
			if !decisionIDPattern.MatchString(action) {
				return fmt.Errorf("invalid decision action %q in %s", action, group)
			}
			if previous, exists := seen[action]; exists {
				return fmt.Errorf("decision action %q appears in both %s and %s", action, previous, group)
			}
			seen[action] = group
		}
	}
	return nil
}

func runPolicyCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(policyHelpText)
		return nil
	}
	root, err := gitRoot(".")
	if err != nil {
		return err
	}
	document, err := loadDecisionPolicy(root)
	if err != nil {
		return err
	}
	switch args[0] {
	case "validate":
		if len(args) > 1 {
			return errors.New("policy validate takes no options")
		}
		fmt.Printf("Validated %s (version %d)\n", decisionPolicyFileName, document.Version)
		return nil
	case "show":
		if len(args) > 1 {
			return errors.New("policy show takes no options")
		}
		groups := []struct {
			name    string
			actions []string
		}{
			{"automatic", document.DecisionPolicy.Automatic},
			{"control_with_record", document.DecisionPolicy.ControlWithRecord},
			{"developer_approval", document.DecisionPolicy.DeveloperApproval},
			{"escalation_triggers", document.DecisionPolicy.EscalationTriggers},
		}
		for _, group := range groups {
			actions := append([]string(nil), group.actions...)
			sort.Strings(actions)
			fmt.Printf("%s:\n", group.name)
			for _, action := range actions {
				fmt.Printf("  - %s\n", action)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown policy command %q; use 'leadfox policy --help' for usage", args[0])
	}
}

const policyHelpText = `Validate or inspect the project decision policy.

Usage:
  leadfox policy validate
  leadfox policy show

The policy is project-owned and lives in control/decision-policy.yaml. It
defines what Control may decide automatically, what it must record, what needs
developer approval, and which conditions require escalation.
`
