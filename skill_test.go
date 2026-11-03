package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanningSkillInstallVerifyAndRemove(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	options := skillInstallOptions{home: home, agent: "auto", yes: true}
	if err := installPlanningSkill(options); err != nil {
		t.Fatal(err)
	}
	if err := verifyPlanningSkill(options); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(home, ".leadfox", "skills", planningSkillID, "SKILL.md")
	hostCopy := filepath.Join(home, ".claude", "skills", planningSkillID, "SKILL.md")
	for _, file := range []string{canonical, hostCopy} {
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("expected installed skill %s: %v", file, err)
		}
	}
	if err := os.WriteFile(hostCopy, []byte(strings.Replace(string(planningSkill), "# Leadfox Project Planning", "# Modified", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyPlanningSkill(options); err == nil {
		t.Fatal("expected modified host skill to fail verification")
	}
	if err := removePlanningSkill(options); err == nil {
		t.Fatal("expected modified host skill to block removal")
	}
	if err := os.WriteFile(hostCopy, planningSkill, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removePlanningSkill(options); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Fatalf("canonical skill still exists: %v", err)
	}
}

func TestPlanningSkillTargetsIntakeAsFirstOutput(t *testing.T) {
	text := string(planningSkill)
	if !strings.Contains(text, "control/project-intake.md") {
		t.Fatal("planning skill must target control/project-intake.md")
	}
	if !strings.Contains(text, "leadfox intake validate control/project-intake.md") {
		t.Fatal("planning skill must require intake validation")
	}
	if strings.Contains(text, "produce one canonical human-readable plan document:\n`control/architecture-plan.md`") {
		t.Fatal("planning skill must not make architecture-plan.md the first output")
	}
}

func TestSkillAutoDoesNotCreateUndeclaredHosts(t *testing.T) {
	home := t.TempDir()
	if err := installPlanningSkill(skillInstallOptions{home: home, agent: "auto", yes: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("auto mode created an undeclared Claude directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("auto mode created an undeclared Codex directory: %v", err)
	}
}

func TestUninstallPreservesProjectLocalSkill(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := installPlanningSkill(skillInstallOptions{home: home, agent: "auto", yes: true}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, ".leadfox")
	target := filepath.Join(root, "releases", "0.1.0", "linux_amd64", "leadfox")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("binary"))
	appManifest := appInstallManifest{ActiveBinary: "bin/leadfox", ActiveTarget: "releases/0.1.0/linux_amd64/leadfox", BinarySHA256: hex.EncodeToString(digest[:])}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "bin", "leadfox")); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(appManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	projectSkill := filepath.Join(home, "project", "skills", "custom", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(projectSkill), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectSkill, []byte("project skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := uninstallLeadfox(uninstallOptions{home: home, yes: true, keepPath: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(projectSkill); err != nil {
		t.Fatalf("project-local skill was removed: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("global Leadfox root remains: %v", err)
	}
}

func TestWritableDirectoryRejectsUnsafePath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureWritableDirectory(file); err == nil {
		t.Fatal("expected unsafe skill directory to be rejected")
	}
}
