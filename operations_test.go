package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperationsRegistrationRequiresSeparateCheckout(t *testing.T) {
	root := writeProjectFixture(t)
	checkout := t.TempDir()
	if err := registerOperations([]string{"--project", root, checkout, "--yes"}); err != nil {
		t.Fatal(err)
	}
	document, err := loadOperations(root)
	if err != nil {
		t.Fatal(err)
	}
	if document.Checkout != checkout {
		t.Fatalf("unexpected checkout: %+v", document)
	}
	if err := registerOperations([]string{"--project", root, root, "--yes"}); err == nil || !strings.Contains(err.Error(), "separate") {
		t.Fatal("expected same-checkout registration to fail")
	}
}

func TestOperationalPathDetectionDoesNotReadContents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "saved.tfplan"), []byte("secret-looking plan content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".terraform"), 0o700); err != nil {
		t.Fatal(err)
	}
	paths := findOperationalPaths(root)
	if len(paths) != 2 || paths[0] != ".terraform" || paths[1] != "saved.tfplan" {
		t.Fatalf("unexpected operational paths: %v", paths)
	}
}

func TestPlanHashVerificationAndArtifactRetention(t *testing.T) {
	root := writeProjectFixture(t)
	checkout := t.TempDir()
	if err := registerOperations([]string{"--project", root, checkout, "--yes"}); err != nil {
		t.Fatal(err)
	}
	plan := filepath.Join(checkout, "plan.tfplan")
	if err := os.WriteFile(plan, []byte("plan bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := hashFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyOperationsPlan([]string{plan, "--sha256", digest}); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "retained", "plan.tfplan")
	if err := retainOperationsArtifact([]string{"--project", root, plan, "--destination", destination, "--yes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}
	if err := retainOperationsArtifact([]string{"--project", root, plan, "--destination", filepath.Join(root, "retained.tfplan"), "--yes"}); err == nil {
		t.Fatal("expected retention inside project to fail")
	}
}
