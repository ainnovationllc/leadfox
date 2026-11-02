package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesIntakeAndProjectControlSkill(t *testing.T) {
	parent := t.TempDir()
	if err := runInit([]string{"example", "--parent", parent}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "example")
	for _, file := range []string{
		"control/project-intake.md",
		"control/skills.yaml",
		"skills/leadfox-control-plan-review/SKILL.md",
		"AGENTS.md",
		"CLAUDE.md",
	} {
		if _, err := os.Lstat(filepath.Join(root, file)); err != nil {
			t.Fatalf("expected initialized file %s: %v", file, err)
		}
	}
	intake, err := os.ReadFile(filepath.Join(root, "control/project-intake.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(intake), "## Change history") || !strings.Contains(string(intake), "Project name: example") {
		t.Fatal("generated intake is missing required planning structure")
	}
	manifest, err := os.ReadFile(filepath.Join(root, "control/skills.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), controlPlanReviewSkillSHA256()) {
		t.Fatal("project skill manifest does not contain the embedded skill checksum")
	}
	if info, err := os.Lstat(filepath.Join(root, "CLAUDE.md")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("CLAUDE.md is not the expected symlink")
	}
}

func TestInitDryRunDoesNotCreateFiles(t *testing.T) {
	parent := t.TempDir()
	if err := runInit([]string{"example", "--parent", parent, "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent, "example")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created project directory: %v", err)
	}
}

func TestInitRejectsNonEmptyDirectory(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "example")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runInit([]string{"example", "--parent", parent}); err == nil {
		t.Fatal("expected non-empty directory to be rejected")
	}
	if _, err := os.Stat(filepath.Join(root, "existing.txt")); err != nil {
		t.Fatalf("existing file was changed: %v", err)
	}
}
