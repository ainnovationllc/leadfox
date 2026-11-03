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

func TestProjectCheckValidatesInitializedStructure(t *testing.T) {
	parent := t.TempDir()
	if err := runInit([]string{"example", "--parent", parent}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "example")
	report, err := checkProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("initialized project failed structure check: %+v", report)
	}
	if report.IntakeValid {
		t.Fatal("uncompleted generated intake should be reported separately")
	}
	if len(report.Warnings) == 0 {
		t.Fatal("expected incomplete intake warning")
	}
}

func TestProjectCheckDetectsModifiedProjectSkill(t *testing.T) {
	parent := t.TempDir()
	if err := runInit([]string{"example", "--parent", parent}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "example")
	path := filepath.Join(root, "skills", "leadfox-control-plan-review", "SKILL.md")
	if err := os.WriteFile(path, []byte("modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := checkProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || len(report.Invalid) == 0 {
		t.Fatal("expected modified project skill to fail structure check")
	}
}

func TestSkillStatusReadOnlyReport(t *testing.T) {
	parent := t.TempDir()
	if err := runInit([]string{"example", "--parent", parent}); err != nil {
		t.Fatal(err)
	}
	if err := statusSkills(skillInstallOptions{home: t.TempDir(), format: "json"}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectAdoptionIsDryRunByDefaultAndPreservesExistingFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "existing")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(root, "README.md")
	if err := os.WriteFile(readme, []byte("existing project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := planProjectAdoption(root)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Applied || len(plan.Files) == 0 || len(plan.Directories) == 0 {
		t.Fatal("expected a non-mutating adoption plan")
	}
	if _, err := os.Stat(filepath.Join(root, "control")); !os.IsNotExist(err) {
		t.Fatal("dry-run created project structure")
	}
	if err := applyProjectAdoption(&plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "control", "leadfox-adoption.yaml")); err != nil {
		t.Fatal("adoption record was not created")
	}
	content, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "existing project\n" {
		t.Fatal("adoption overwrote an existing file")
	}
	report, err := checkProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("adopted project failed structure check: %+v", report)
	}
}

func TestSkillSyncIsPinnedExplicitAndOffline(t *testing.T) {
	parent := t.TempDir()
	if err := runInit([]string{"example", "--parent", parent}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "example")
	home := t.TempDir()
	options := skillInstallOptions{home: home, agent: "auto", format: "json"}
	report, record, err := planPlanningSkillSync(root, options)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "ready" || record == nil {
		t.Fatalf("expected a ready sync plan: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(home, ".leadfox")); !os.IsNotExist(err) {
		t.Fatal("sync planning created global files")
	}
	if err := applyPlanningSkillSync(options, record); err != nil {
		t.Fatal(err)
	}
	report, _, err = planPlanningSkillSync(root, options)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "current" {
		t.Fatalf("expected current skill after sync: %+v", report)
	}
}

func TestSkillSyncRefusesModifiedSkill(t *testing.T) {
	parent := t.TempDir()
	if err := runInit([]string{"example", "--parent", parent}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "example")
	home := t.TempDir()
	options := skillInstallOptions{home: home, agent: "auto"}
	report, record, err := planPlanningSkillSync(root, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPlanningSkillSync(options, record); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".leadfox", "skills", planningSkillID, "SKILL.md")
	if err := os.WriteFile(path, []byte("user modification\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, _, err = planPlanningSkillSync(root, options)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "blocked" || !strings.Contains(report.Detail, "modified") {
		t.Fatalf("expected modified skill to block sync: %+v", report)
	}
}
