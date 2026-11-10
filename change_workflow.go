package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type changeWorkflow struct {
	Version    int               `yaml:"version"`
	HoldPoints []changeHoldPoint `yaml:"hold_points,omitempty"`
	Jobs       []changeJobSpec   `yaml:"jobs,omitempty"`
}

type changeHoldPoint struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Status      string `yaml:"status"`
	ReleasedBy  string `yaml:"released_by,omitempty"`
	ReleasedAt  string `yaml:"released_at,omitempty"`
	Reason      string `yaml:"reason,omitempty"`
}

type changeJobSpec struct {
	ID            string   `yaml:"id"`
	Role          string   `yaml:"role"`
	Objective     string   `yaml:"objective"`
	AllowedPaths  []string `yaml:"allowed_paths,omitempty"`
	ExcludedPaths []string `yaml:"excluded_paths,omitempty"`
	Gates         []string `yaml:"gates,omitempty"`
	Capabilities  []string `yaml:"capabilities,omitempty"`
	RiskControls  []string `yaml:"risk_controls,omitempty"`
	HoldPoint     string   `yaml:"hold_point,omitempty"`
}

func changeWorkflowPath(changePath string) string { return changePath + ".workflow.yaml" }

func loadChangeWorkflow(changePath string) (changeWorkflow, error) {
	content, err := os.ReadFile(changeWorkflowPath(changePath))
	if err != nil {
		return changeWorkflow{}, fmt.Errorf("read change workflow %s: %w", changeWorkflowPath(changePath), err)
	}
	var workflow changeWorkflow
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		return changeWorkflow{}, fmt.Errorf("parse change workflow: %w", err)
	}
	if workflow.Version == 0 {
		workflow.Version = 1
	}
	for index := range workflow.HoldPoints {
		if workflow.HoldPoints[index].Status == "" {
			workflow.HoldPoints[index].Status = "open"
		}
	}
	return workflow, nil
}

func runChangeWorkflowCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Println("Usage: leadfox change jobs PATH [--create --yes]")
		fmt.Println("       leadfox change hold list PATH")
		fmt.Println("       leadfox change hold release PATH HOLD-ID --by NAME --reason TEXT --yes")
		return nil
	}
	switch args[0] {
	case "jobs":
		path, create, yes, err := parseChangeWorkflowArgs(args[1:])
		if err != nil {
			return err
		}
		return changeJobs(path, create, yes)
	case "hold":
		if len(args) < 2 {
			return errors.New("change hold requires list or release")
		}
		switch args[1] {
		case "list":
			if len(args) != 3 {
				return errors.New("usage: leadfox change hold list PATH")
			}
			workflow, err := loadChangeWorkflow(args[2])
			if err != nil {
				return err
			}
			for _, hold := range workflow.HoldPoints {
				fmt.Printf("%s\t%s\t%s\n", hold.ID, hold.Status, hold.Description)
			}
			return nil
		case "release":
			return releaseChangeHold(args[2:])
		default:
			return fmt.Errorf("unknown change hold operation %q", args[1])
		}
	default:
		return fmt.Errorf("unknown change workflow operation %q", args[0])
	}
}

func parseChangeWorkflowArgs(args []string) (string, bool, bool, error) {
	path, create, yes := "", false, false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--create":
			create = true
		case "--yes":
			yes = true
		default:
			if strings.HasPrefix(args[index], "-") {
				return "", false, false, fmt.Errorf("unknown change workflow option %q", args[index])
			}
			if path != "" {
				return "", false, false, errors.New("only one change path may be supplied")
			}
			path = args[index]
		}
	}
	if path == "" {
		return "", false, false, errors.New("a change plan path is required")
	}
	if create && !yes {
		return "", false, false, errors.New("--create writes draft job packets; repeat with --yes after review")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, false, err
	}
	return abs, create, yes, nil
}

func changeJobs(path string, create, yes bool) error {
	if report, err := validateChangePlan(path); err != nil {
		return err
	} else if !report.Valid {
		return errChangeInvalid
	}
	workflow, err := loadChangeWorkflow(path)
	if err != nil {
		return err
	}
	root := projectRootForChange(path)
	for _, spec := range workflow.Jobs {
		fmt.Printf("%s\t%s\t%s\n", spec.ID, spec.Role, spec.Objective)
		if !create {
			continue
		}
		if _, err := os.Stat(jobPath(root, spec.ID)); err == nil {
			return fmt.Errorf("job packet already exists: %s", spec.ID)
		} else if !os.IsNotExist(err) {
			return err
		}
		o := jobOptions{role: spec.Role, objective: spec.Objective, allow: spec.AllowedPaths, exclude: spec.ExcludedPaths, gates: spec.Gates, capabilities: spec.Capabilities, risks: spec.RiskControls, used: map[string]bool{"role": true, "objective": true, "allow": true, "exclude": true, "gate": true, "capability": true, "risk": true}}
		if err := jobNew(root, spec.ID, o); err != nil {
			return err
		}
		packet, body, err := readJob(root, spec.ID)
		if err != nil {
			return err
		}
		packet.ChangePlan, packet.HoldPoint = filepath.ToSlash(relativeOrSelf(root, path)), spec.HoldPoint
		if err := writeJob(root, packet, body); err != nil {
			return err
		}
		fmt.Printf("Created draft job packet: %s\n", jobPath(root, spec.ID))
	}
	return nil
}

func releaseChangeHold(args []string) error {
	if len(args) < 1 {
		return errors.New("change hold release requires PATH and HOLD-ID")
	}
	path, holdID := args[0], ""
	by, reason := "", ""
	yes := false
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--by":
			if index+1 >= len(args) {
				return errors.New("--by requires a name")
			}
			by = args[index+1]
			index++
		case "--reason":
			if index+1 >= len(args) {
				return errors.New("--reason requires text")
			}
			reason = args[index+1]
			index++
		case "--yes":
			yes = true
		default:
			if strings.HasPrefix(args[index], "-") {
				return fmt.Errorf("unknown option %q", args[index])
			}
			if holdID != "" {
				return errors.New("only one hold ID may be supplied")
			}
			holdID = args[index]
		}
	}
	if holdID == "" || by == "" || reason == "" {
		return errors.New("hold release requires HOLD-ID, --by, and --reason")
	}
	if !yes {
		return errors.New("hold release changes project policy; repeat with --yes after review")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	workflow, err := loadChangeWorkflow(abs)
	if err != nil {
		return err
	}
	found := false
	for index := range workflow.HoldPoints {
		if workflow.HoldPoints[index].ID != holdID {
			continue
		}
		workflow.HoldPoints[index].Status = "released"
		workflow.HoldPoints[index].ReleasedBy = by
		workflow.HoldPoints[index].ReleasedAt = time.Now().UTC().Format(time.RFC3339)
		workflow.HoldPoints[index].Reason = reason
		found = true
	}
	if !found {
		return fmt.Errorf("hold point %q was not found", holdID)
	}
	content, err := yaml.Marshal(workflow)
	if err != nil {
		return err
	}
	if err := os.WriteFile(changeWorkflowPath(abs), content, 0o644); err != nil {
		return err
	}
	fmt.Printf("Released hold %s by %s\n", holdID, by)
	return nil
}

func ensureJobHoldReleased(root string, packet jobPacket) error {
	if packet.ChangePlan == "" || packet.HoldPoint == "" {
		return nil
	}
	workflow, err := loadChangeWorkflow(filepath.Join(root, filepath.FromSlash(packet.ChangePlan)))
	if err != nil {
		return err
	}
	for _, hold := range workflow.HoldPoints {
		if hold.ID == packet.HoldPoint && hold.Status == "released" {
			return nil
		}
	}
	return fmt.Errorf("job %s is held at %s; release the change hold before starting it", packet.ID, packet.HoldPoint)
}

func relativeOrSelf(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return relative
}
