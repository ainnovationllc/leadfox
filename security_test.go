package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSecurityScanUsesPinnedScannerAndRedactsFindings(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "fake-gitleaks")
	script := `#!/bin/sh
if [ "$1" = "version" ]; then
  echo "gitleaks 8.0.0"
  exit 0
fi
report=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--report-path" ]; then report="$arg"; fi
  previous="$arg"
done
printf '[{"Secret":"live-secret","Match":"live-secret","RuleID":"test"}]\n' > "$report"
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	config := `secret_scanning:
  enabled: true
  tool: gitleaks
  binary: ` + binary + `
  version: "8.0.0"
  mode: history
  report_path: control/evidence/secret-scan.json
  allow_network: false
`
	if err := os.WriteFile(filepath.Join(root, "control", "security.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := executeSecurityScan(root)
	if err == nil || report.Status != "failed" || report.ExitCode != 1 {
		t.Fatalf("finding was not reported as a scan failure: %+v, %v", report, err)
	}
	content, readErr := os.ReadFile(filepath.Join(root, "control", "evidence", "secret-scan.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) == "" || string(content) == "live-secret" {
		t.Fatalf("secret was not redacted: %s", content)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded[0]["Secret"] != "[REDACTED]" || decoded[0]["Match"] != "[REDACTED]" {
		t.Fatalf("redacted fields missing: %+v", decoded)
	}
}

func TestSecurityScanRejectsNetworkEnabledTruffleHog(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := `secret_scanning:
  enabled: true
  tool: trufflehog
  version: "3.0.0"
  mode: history
  allow_network: true
`
	if err := os.WriteFile(filepath.Join(root, "control", "security.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := executeSecurityScan(root); err == nil {
		t.Fatal("network-enabled TruffleHog was allowed in the local scanner")
	}
}
