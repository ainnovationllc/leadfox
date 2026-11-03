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

const capabilityFileName = "control/agent-capabilities.yaml"

type capabilityFile struct {
	Capabilities []agentCapability `yaml:"capabilities"`
}

// agentCapability describes a capability that an agent host may provide. It
// is intentionally provider-neutral: Leadfox records requirements and
// constraints, while the runtime owns the actual connection and credentials.
type agentCapability struct {
	ID             string   `yaml:"id"`
	Kind           string   `yaml:"kind"`
	Provider       string   `yaml:"provider"`
	Description    string   `yaml:"description,omitempty"`
	Tools          []string `yaml:"tools,omitempty"`
	Access         string   `yaml:"access,omitempty"`
	Authentication string   `yaml:"authentication,omitempty"`
	Network        bool     `yaml:"network,omitempty"`
	OutputPaths    []string `yaml:"output_paths,omitempty"`
	Fallback       string   `yaml:"fallback,omitempty"`
}

var capabilityIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func loadCapabilities(root string) (capabilityFile, error) {
	path := filepath.Join(root, capabilityFileName)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return capabilityFile{}, nil
		}
		return capabilityFile{}, fmt.Errorf("read %s: %w", capabilityFileName, err)
	}
	var document capabilityFile
	if err := yaml.Unmarshal(content, &document); err != nil {
		return capabilityFile{}, fmt.Errorf("parse %s: %w", capabilityFileName, err)
	}
	if err := validateCapabilityDefinitions(document.Capabilities); err != nil {
		return capabilityFile{}, err
	}
	return document, nil
}

func validateCapabilityDefinitions(capabilities []agentCapability) error {
	seen := make(map[string]bool, len(capabilities))
	for _, capability := range capabilities {
		if !capabilityIDPattern.MatchString(capability.ID) {
			return fmt.Errorf("invalid capability ID %q", capability.ID)
		}
		if seen[capability.ID] {
			return fmt.Errorf("duplicate capability ID %q", capability.ID)
		}
		seen[capability.ID] = true
		if strings.TrimSpace(capability.Kind) == "" || strings.TrimSpace(capability.Provider) == "" {
			return fmt.Errorf("capability %q requires kind and provider", capability.ID)
		}
		if capability.Access != "" && capability.Access != "read-only" && capability.Access != "read-write" {
			return fmt.Errorf("capability %q has unsupported access %q", capability.ID, capability.Access)
		}
		if capability.Authentication != "" && capability.Authentication != "none" && capability.Authentication != "user" && capability.Authentication != "host" {
			return fmt.Errorf("capability %q has unsupported authentication %q", capability.ID, capability.Authentication)
		}
		if capability.Fallback != "" && capability.Fallback != "stop" && capability.Fallback != "continue" {
			return fmt.Errorf("capability %q has unsupported fallback %q", capability.ID, capability.Fallback)
		}
		for _, path := range capability.OutputPaths {
			if path == "" || filepath.IsAbs(path) || strings.Contains(filepath.Clean(path), "..") {
				return fmt.Errorf("capability %q has unsafe output path %q", capability.ID, path)
			}
		}
	}
	return nil
}

func validateJobCapabilities(root string, required []string) error {
	if len(required) == 0 {
		return nil
	}
	document, err := loadCapabilities(root)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(document.Capabilities))
	for _, capability := range document.Capabilities {
		known[capability.ID] = true
	}
	seen := make(map[string]bool, len(required))
	for _, id := range required {
		if !capabilityIDPattern.MatchString(id) {
			return fmt.Errorf("invalid required capability ID %q", id)
		}
		if seen[id] {
			return fmt.Errorf("duplicate required capability %q", id)
		}
		seen[id] = true
		if !known[id] {
			return fmt.Errorf("required capability %q is not declared in %s", id, capabilityFileName)
		}
	}
	return nil
}

func runCapabilityCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(capabilityHelpText)
		return nil
	}
	root, err := gitRoot(".")
	if err != nil {
		return err
	}
	document, err := loadCapabilities(root)
	if err != nil {
		return err
	}
	switch args[0] {
	case "health":
		if len(args) > 1 {
			return errors.New("capability health takes no options; it reports all declared capabilities")
		}
		return capabilityHealth(root, document)
	case "list":
		if len(args) > 1 {
			return errors.New("capability list takes no options")
		}
		capabilities := append([]agentCapability(nil), document.Capabilities...)
		sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].ID < capabilities[j].ID })
		fmt.Println("ID\tKIND\tPROVIDER\tACCESS\tAUTHENTICATION")
		for _, capability := range capabilities {
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", capability.ID, capability.Kind, capability.Provider, capability.Access, capability.Authentication)
		}
		return nil
	case "validate":
		ids := args[1:]
		if len(ids) == 0 {
			ids = make([]string, 0, len(document.Capabilities))
			for _, capability := range document.Capabilities {
				ids = append(ids, capability.ID)
			}
		}
		if err := validateJobCapabilities(root, ids); err != nil {
			return err
		}
		fmt.Printf("Validated %d capability requirement(s) against %s\n", len(ids), capabilityFileName)
		return nil
	default:
		return fmt.Errorf("unknown capability command %q; use 'leadfox capability --help' for usage", args[0])
	}
}

const capabilityHelpText = `Manage provider-neutral agent capabilities.

Usage:
  leadfox capability list
  leadfox capability validate [CAPABILITY ...]
  leadfox capability health

Capabilities are declarations for agent hosts, including MCP servers. Leadfox
does not connect to providers or store credentials. Configure definitions in
control/agent-capabilities.yaml and reference IDs from job packets.
`
