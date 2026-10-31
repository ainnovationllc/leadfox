package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecuteGateDistinguishesSkippedAndFailedChecks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		"gates:",
		"  - id: sample",
		"    checks:",
		"      - id: pass",
		"        run: test -f control/gates.yaml",
		"      - id: deferred",
		"        deferred: true",
		"        run: false",
		"      - id: skipped",
		"        requires_path: missing/file",
		"        run: false",
		"      - id: fail",
		"        run: exit 7",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "control", "gates.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := executeGate(root, gateOptions{gateName: "sample", timeout: time.Second})
	if err != nil {
		t.Fatalf("execute gate: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("expected failed gate, got %q", result.Status)
	}
	if result.Checks[0].Status != "passed" || result.Checks[1].Status != "deferred" || result.Checks[2].Status != "skipped" || result.Checks[3].Status != "failed" {
		t.Fatalf("unexpected check statuses: %+v", result.Checks)
	}
	if result.Checks[3].ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", result.Checks[3].ExitCode)
	}
}

func TestExecuteGateTimesOut(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "gates:\n  - id: sample\n    checks:\n      - id: slow\n        run: sleep 2\n"
	if err := os.WriteFile(filepath.Join(root, "control", "gates.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := executeGate(root, gateOptions{gateName: "sample", timeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("execute gate: %v", err)
	}
	if result.Status != "failed" || result.Checks[0].ExitCode != 124 {
		t.Fatalf("unexpected timeout result: %+v", result)
	}
}

func TestExecuteGateDoesNotCallManualChecksPassed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "gates:\n  - id: sample\n    checks:\n      - id: attest\n        manual: true\n"
	if err := os.WriteFile(filepath.Join(root, "control", "gates.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := executeGate(root, gateOptions{gateName: "sample", timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "manual" || result.Checks[0].Status != "manual" {
		t.Fatalf("manual check was reported as passed: %+v", result)
	}
}

func TestRedactGateOutput(t *testing.T) {
	output := redactGateOutput("Authorization: Bearer abc123\npassword=secret\nordinary output")
	if strings.Contains(output, "abc123") || strings.Contains(output, "secret") {
		t.Fatalf("sensitive output was not redacted: %q", output)
	}
	if !strings.Contains(output, "ordinary output") {
		t.Fatalf("ordinary output was removed: %q", output)
	}
}

func TestGateRepeatReportsConsistencyAndPlatform(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "gates:\n  - id: sample\n    checks:\n      - id: pass\n        run: test -f control/gates.yaml\n"
	if err := os.WriteFile(filepath.Join(root, "control", "gates.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	options, err := parseGateOptions([]string{"sample", "--repeat", "3", "--format", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if options.repeat != 3 {
		t.Fatalf("repeat option not parsed: %+v", options)
	}
	report := repeatGate(root, options)
	if report.Status != "passed" || report.Passed != 3 || report.Failed != 0 || report.Flaky || len(report.Runs) != 3 {
		t.Fatalf("unexpected repeat report: %+v", report)
	}
	if report.Runs[0].Platform.OS == "" || report.Runs[0].Platform.Arch == "" || !report.Runs[0].Applicable {
		t.Fatalf("platform applicability was not recorded: %+v", report.Runs[0])
	}
}
