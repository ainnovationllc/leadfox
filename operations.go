package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const operationsFileName = "control/operations.yaml"

type operationsFile struct {
	Version      int              `yaml:"version"`
	Checkout     string           `yaml:"checkout"`
	RegisteredAt string           `yaml:"registered_at"`
	Recovery     []recoveryRecord `yaml:"recovery,omitempty"`
}

type operationsReport struct {
	Project     string              `json:"project"`
	Checkout    string              `json:"checkout,omitempty"`
	CurrentPath string              `json:"current_path"`
	Status      string              `json:"status"`
	Findings    []diagnosticFinding `json:"findings,omitempty"`
	Operational []string            `json:"operational_paths,omitempty"`
}

type recoveryRecord struct {
	ID              string `yaml:"id" json:"id"`
	Status          string `yaml:"status" json:"status"`
	Reason          string `yaml:"reason" json:"reason"`
	NextStep        string `yaml:"next_step,omitempty" json:"next_step,omitempty"`
	StateHashBefore string `yaml:"state_hash_before,omitempty" json:"state_hash_before,omitempty"`
	StateHashAfter  string `yaml:"state_hash_after,omitempty" json:"state_hash_after,omitempty"`
	RecordedAt      string `yaml:"recorded_at" json:"recorded_at"`
}

func runOperationsCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(operationsHelpText)
		return nil
	}
	operation := args[0]
	switch operation {
	case "register":
		return registerOperations(args[1:])
	case "check":
		return checkOperationsCommand(args[1:])
	case "verify-plan":
		return verifyOperationsPlan(args[1:])
	case "retain":
		return retainOperationsArtifact(args[1:])
	case "recovery":
		return recordOperationsRecovery(args[1:])
	default:
		return fmt.Errorf("unknown operations operation %q; use 'leadfox ops --help' for usage", operation)
	}
}

func parseOperationsOptions(args []string) (root, format string, yes bool, values map[string]string, err error) {
	root, format, values = ".", "markdown", map[string]string{}
	for i := 0; i < len(args); i++ {
		key := args[i]
		if key == "--yes" {
			yes = true
			continue
		}
		if key == "--format" {
			if i+1 >= len(args) || (args[i+1] != "markdown" && args[i+1] != "json") {
				return "", "", false, nil, errors.New("--format requires markdown or json")
			}
			format, i = args[i+1], i+1
			continue
		}
		if key == "--project" {
			if i+1 >= len(args) {
				return "", "", false, nil, errors.New("--project requires a path")
			}
			root, i = args[i+1], i+1
			continue
		}
		if !strings.HasPrefix(key, "--") {
			if _, exists := values["positional"]; exists {
				return "", "", false, nil, errors.New("only one positional path is allowed")
			}
			values["positional"] = key
			continue
		}
		if i+1 >= len(args) {
			return "", "", false, nil, fmt.Errorf("%s requires a value", key)
		}
		values[strings.TrimPrefix(key, "--")], i = args[i+1], i+1
	}
	abs, absErr := filepath.Abs(root)
	if absErr != nil {
		return "", "", false, nil, absErr
	}
	return abs, format, yes, values, nil
}

func loadOperations(root string) (operationsFile, error) {
	content, err := os.ReadFile(filepath.Join(root, operationsFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return operationsFile{}, errors.New("no operations checkout is registered")
		}
		return operationsFile{}, fmt.Errorf("read %s: %w", operationsFileName, err)
	}
	var document operationsFile
	if err := yaml.Unmarshal(content, &document); err != nil {
		return document, fmt.Errorf("parse %s: %w", operationsFileName, err)
	}
	if document.Version != 1 || strings.TrimSpace(document.Checkout) == "" {
		return document, errors.New("operations registry must have version 1 and a checkout")
	}
	return document, nil
}

func registerOperations(args []string) error {
	root, _, yes, values, err := parseOperationsOptions(args)
	if err != nil {
		return err
	}
	checkout := values["positional"]
	if checkout == "" {
		return errors.New("operations register requires a checkout path")
	}
	checkout, err = filepath.Abs(checkout)
	if err != nil {
		return err
	}
	if !yes {
		fmt.Printf("Would register operations checkout: %s\nRepeat with --yes to write %s.\n", checkout, filepath.Join(root, operationsFileName))
		return nil
	}
	info, err := os.Stat(checkout)
	if err != nil {
		return fmt.Errorf("inspect operations checkout: %w", err)
	}
	if !info.IsDir() {
		return errors.New("operations checkout must be a directory")
	}
	projectAbs, _ := filepath.Abs(root)
	if isWithin(projectAbs, checkout) || isWithin(checkout, projectAbs) {
		return errors.New("operations checkout must be separate from the project checkout")
	}
	path := filepath.Join(root, operationsFileName)
	if _, err := os.Stat(path); err == nil {
		return errors.New("operations checkout is already registered; edit it through a reviewed project change")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	document := operationsFile{Version: 1, Checkout: checkout, RegisteredAt: time.Now().UTC().Format(time.RFC3339)}
	encoded, err := yaml.Marshal(document)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("write operations registry: %w", err)
	}
	fmt.Printf("Registered operations checkout: %s\n", checkout)
	return nil
}

func checkOperationsCommand(args []string) error {
	root, format, _, values, err := parseOperationsOptions(args)
	if err != nil {
		return err
	}
	current := values["cwd"]
	if current == "" {
		current, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return err
	}
	document, err := loadOperations(root)
	report := operationsReport{Project: root, CurrentPath: current, Status: "healthy"}
	if err != nil {
		report.Status = "findings"
		report.Findings = append(report.Findings, diagnosticFinding{Code: "operations.unregistered", Severity: "error", Message: err.Error(), Remediation: "Register a separate persistent operations checkout after review."})
	} else {
		report.Checkout = document.Checkout
		if !isWithin(document.Checkout, current) {
			report.Status = "findings"
			report.Findings = append(report.Findings, diagnosticFinding{Code: "operations.wrong-checkout", Severity: "error", Message: "current path is outside the registered operations checkout", Remediation: "Run cloud plans and applies from the registered operations checkout."})
		}
	}
	report.Operational = findOperationalPaths(root)
	if len(report.Operational) > 0 {
		report.Status = "findings"
		report.Findings = append(report.Findings, diagnosticFinding{Code: "operations.stray-state", Severity: "error", Message: "operational state or credential-shaped files are present in the project checkout", Remediation: "Move them to the registered operations checkout before cleanup or external operations."})
	}
	if format == "json" {
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(encoded))
	} else {
		fmt.Printf("Operations: %s\nProject: %s\nCurrent path: %s\nCheckout: %s\n", report.Status, report.Project, report.CurrentPath, report.Checkout)
		for _, finding := range report.Findings {
			fmt.Printf("- [%s] %s\n  Remedy: %s\n", finding.Code, finding.Message, finding.Remediation)
		}
		if len(report.Operational) > 0 {
			fmt.Println("Operational paths:")
			for _, path := range report.Operational {
				fmt.Printf("- %s\n", path)
			}
		}
	}
	if report.Status == "findings" {
		return errors.New("operations check found issues")
	}
	return nil
}

func findOperationalPaths(root string) []string {
	paths := []string{}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil || relative == "." {
			return nil
		}
		if entry.IsDir() {
			if relative == ".git" || relative == "node_modules" || relative == "dist" {
				return filepath.SkipDir
			}
			if relative == ".terraform" {
				paths = append(paths, relative)
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".tfstate") || strings.Contains(lower, ".tfstate.") || strings.HasSuffix(lower, ".tfplan") || lower == ".env" || strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") {
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	})
	sort.Strings(paths)
	return paths
}

func verifyOperationsPlan(args []string) error {
	_, _, _, values, err := parseOperationsOptions(args)
	if err != nil {
		return err
	}
	path := values["positional"]
	expected := strings.ToLower(values["sha256"])
	if path == "" || expected == "" {
		return errors.New("ops verify-plan requires PATH and --sha256 HASH")
	}
	if !isHexHash(expected) {
		return errors.New("--sha256 must be 64 hexadecimal characters")
	}
	actual, err := hashFile(path)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("plan hash mismatch: expected %s, got %s", expected, actual)
	}
	fmt.Printf("Verified plan SHA-256: %s\n", actual)
	return nil
}

func retainOperationsArtifact(args []string) error {
	root, _, yes, values, err := parseOperationsOptions(args)
	if err != nil {
		return err
	}
	source := values["positional"]
	destination := values["destination"]
	if source == "" || destination == "" {
		return errors.New("ops retain requires SOURCE and --destination PATH")
	}
	source, _ = filepath.Abs(source)
	destination, _ = filepath.Abs(destination)
	if isWithin(root, destination) {
		return errors.New("retained artifact destination must be outside the project checkout")
	}
	if _, err := os.Stat(source); err != nil {
		return err
	}
	if _, err := os.Stat(destination); err == nil {
		return errors.New("retained artifact destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !yes {
		fmt.Printf("Would retain %s at %s\nRepeat with --yes to copy it.\n", source, destination)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	digest, err := hashFile(destination)
	if err != nil {
		return err
	}
	fmt.Printf("Retained artifact: %s\nSHA-256: %s\n", destination, digest)
	return nil
}

func recordOperationsRecovery(args []string) error {
	root, _, yes, values, err := parseOperationsOptions(args)
	if err != nil {
		return err
	}
	id, status, reason := values["positional"], values["status"], values["reason"]
	if id == "" || status == "" || reason == "" {
		return errors.New("ops recovery requires ID, --status STATUS, and --reason TEXT")
	}
	if !map[string]bool{"failed": true, "blocked": true, "ready": true}[status] {
		return errors.New("recovery status must be failed, blocked, or ready")
	}
	if !yes {
		fmt.Printf("Would record recovery %s (%s)\nRepeat with --yes to write it.\n", id, status)
		return nil
	}
	document, loadErr := loadOperations(root)
	if loadErr != nil {
		return loadErr
	}
	record := recoveryRecord{ID: id, Status: status, Reason: reason, NextStep: values["next-step"], StateHashBefore: values["state-hash-before"], StateHashAfter: values["state-hash-after"], RecordedAt: time.Now().UTC().Format(time.RFC3339)}
	document.Recovery = append(document.Recovery, record)
	encoded, err := yaml.Marshal(document)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, operationsFileName), encoded, 0o644); err != nil {
		return err
	}
	fmt.Printf("Recorded recovery: %s\n", id)
	return nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
func isHexHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}
func isWithin(base, candidate string) bool {
	base, _ = filepath.Abs(base)
	candidate, _ = filepath.Abs(candidate)
	relative, err := filepath.Rel(base, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

const operationsHelpText = `Protect and inspect durable operations state.

Usage:
  leadfox ops register PATH --project PROJECT [--yes]
  leadfox ops check --project PROJECT [--cwd PATH] [--format json]
  leadfox ops verify-plan PATH --sha256 HASH
  leadfox ops retain SOURCE --destination PATH --project PROJECT [--yes]
  leadfox ops recovery record ID --status STATUS --reason TEXT --project PROJECT [--yes]

These commands do not contact cloud services. Registration, retention, and
recovery writes require --yes. Operational state and plan contents are never
printed.
`
