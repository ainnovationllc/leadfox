package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	planningSkillID      = "leadfox-project-planning"
	planningSkillVersion = "1"
	planningSkillSource  = "leadfox-distribution"
)

// The source path is retained while the repository transitions from the
// earlier planner name to the public project-planning skill ID.
//
//go:embed skills/leadfox-project-planner/SKILL.md
var planningSkill []byte

type skillInstallOptions struct {
	agent    string
	home     string
	yes      bool
	format   string
	showHelp bool
}

type installedSkill struct {
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Source      string   `json:"source"`
	SHA256      string   `json:"sha256"`
	Scope       string   `json:"scope"`
	InstalledAt string   `json:"installed_at"`
	Paths       []string `json:"paths"`
}

type skillManifest struct {
	Version int              `json:"version"`
	Skills  []installedSkill `json:"skills"`
}

const skillHelpText = `Manage Leadfox-provided skills.

Usage:
  leadfox skill list
  leadfox skill status [--format markdown|json]
  leadfox skill sync [--yes] [--format markdown|json]
  leadfox skill install leadfox-project-planning --global [--agent auto|claude|codex] --yes
  leadfox skill verify leadfox-project-planning --global
  leadfox skill remove leadfox-project-planning --global --yes

The canonical global copy is stored under ~/.leadfox/skills. Host exposure is
optional and never stores agent credentials.
`

func runSkillCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(skillHelpText)
		return nil
	}
	command := args[0]
	options, id, err := parseSkillOptions(args[1:])
	if err != nil {
		return err
	}
	if options.showHelp {
		fmt.Print(skillHelpText)
		return nil
	}
	if id == "" && command != "list" {
		if command != "status" && command != "sync" {
			return errors.New("a skill ID is required")
		}
	}
	if id != "" && id != planningSkillID {
		return fmt.Errorf("unknown Leadfox skill %q", id)
	}
	switch command {
	case "list":
		return listSkills(options)
	case "status":
		return statusSkills(options)
	case "sync":
		return syncPlanningSkill(options)
	case "install":
		if !options.yes {
			return errors.New("skill installation writes user files; repeat with --yes after reviewing the target paths")
		}
		return installPlanningSkill(options)
	case "verify":
		return verifyPlanningSkill(options)
	case "remove":
		if !options.yes {
			return errors.New("skill removal writes user files; repeat with --yes after reviewing the target paths")
		}
		return removePlanningSkill(options)
	default:
		return fmt.Errorf("unknown skill command %q; use 'leadfox skill --help'", command)
	}
}

func parseSkillOptions(args []string) (skillInstallOptions, string, error) {
	options := skillInstallOptions{agent: "auto", format: "markdown"}
	var id string
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--help", "-h":
			options.showHelp = true
		case "--global":
			// Global is the only supported scope in Checkpoint 3.
		case "--yes":
			options.yes = true
		case "--format":
			if index+1 >= len(args) {
				return options, id, errors.New("--format requires markdown or json")
			}
			index++
			options.format = args[index]
			if options.format != "markdown" && options.format != "json" {
				return options, id, fmt.Errorf("unsupported skill report format %q", options.format)
			}
		case "--agent":
			if index+1 >= len(args) {
				return options, id, errors.New("--agent requires auto, claude, or codex")
			}
			index++
			options.agent = args[index]
		case "--home":
			if index+1 >= len(args) {
				return options, id, errors.New("--home requires a directory")
			}
			index++
			options.home = args[index]
		default:
			if strings.HasPrefix(args[index], "-") {
				return options, id, fmt.Errorf("unknown skill option %q", args[index])
			}
			if id != "" {
				return options, id, errors.New("only one skill ID may be provided")
			}
			id = args[index]
		}
	}
	if options.agent != "auto" && options.agent != "claude" && options.agent != "codex" {
		return options, id, fmt.Errorf("unsupported skill agent %q", options.agent)
	}
	return options, id, nil
}

func skillHome(options skillInstallOptions) (string, error) {
	if options.home != "" {
		return filepath.Abs(options.home)
	}
	return os.UserHomeDir()
}

func skillRoot(home string) string { return filepath.Join(home, ".leadfox", "skills") }

func skillManifestPath(home string) string { return filepath.Join(skillRoot(home), "manifest.json") }

func ensureWritableDirectory(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot install skill: %s is not a safe directory", path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect skill directory: %w", err)
	}
	parent := path
	if errors.Is(err, fs.ErrNotExist) {
		parent = filepath.Dir(path)
		for {
			if _, statErr := os.Stat(parent); statErr == nil {
				break
			} else if !errors.Is(statErr, fs.ErrNotExist) {
				return fmt.Errorf("inspect skill directory parent: %w", statErr)
			}
			next := filepath.Dir(parent)
			if next == parent {
				return fmt.Errorf("cannot find a writable parent for %s", path)
			}
			parent = next
		}
	}
	probe, err := os.CreateTemp(parent, ".leadfox-write-test-*")
	if err != nil {
		return fmt.Errorf("cannot install skill: %s is not writable: %w", parent, err)
	}
	probeName := probe.Name()
	if closeErr := probe.Close(); closeErr != nil {
		_ = os.Remove(probeName)
		return fmt.Errorf("close permission check: %w", closeErr)
	}
	if removeErr := os.Remove(probeName); removeErr != nil {
		return fmt.Errorf("clean permission check: %w", removeErr)
	}
	return nil
}

func skillDigest() string {
	digest := sha256.Sum256(planningSkill)
	return hex.EncodeToString(digest[:])
}

func hostSkillPaths(home, agent string) []string {
	paths := []string{}
	for _, candidate := range []string{"claude", "codex"} {
		if agent != "auto" && agent != candidate {
			continue
		}
		root := filepath.Join(home, "."+candidate)
		if agent == "auto" {
			if _, err := os.Stat(root); err != nil {
				continue
			}
		}
		paths = append(paths, filepath.Join(root, "skills", planningSkillID, "SKILL.md"))
	}
	return paths
}

func loadSkillManifest(home string) (skillManifest, error) {
	content, err := os.ReadFile(skillManifestPath(home))
	if errors.Is(err, fs.ErrNotExist) {
		return skillManifest{Version: 1}, nil
	}
	if err != nil {
		return skillManifest{}, fmt.Errorf("read skill manifest: %w", err)
	}
	var manifest skillManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return skillManifest{}, fmt.Errorf("parse skill manifest: %w", err)
	}
	if manifest.Version == 0 {
		manifest.Version = 1
	}
	return manifest, nil
}

func saveSkillManifest(home string, manifest skillManifest) error {
	root := skillRoot(home)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}
	sort.Slice(manifest.Skills, func(i, j int) bool { return manifest.Skills[i].ID < manifest.Skills[j].ID })
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode skill manifest: %w", err)
	}
	temporary := skillManifestPath(home) + fmt.Sprintf(".tmp-%d", os.Getpid())
	if err := os.WriteFile(temporary, append(content, '\n'), 0o600); err != nil {
		return fmt.Errorf("write skill manifest: %w", err)
	}
	if err := os.Rename(temporary, skillManifestPath(home)); err != nil {
		return fmt.Errorf("activate skill manifest: %w", err)
	}
	return nil
}

func writeSkillFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) != string(planningSkill) {
			return fmt.Errorf("refusing to overwrite existing skill file: %s", path)
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, planningSkill, 0o644)
}

func installPlanningSkill(options skillInstallOptions) error {
	home, err := skillHome(options)
	if err != nil {
		return err
	}
	if err := ensureWritableDirectory(skillRoot(home)); err != nil {
		return err
	}
	canonical := filepath.Join(skillRoot(home), planningSkillID, "SKILL.md")
	paths := append([]string{canonical}, hostSkillPaths(home, options.agent)...)
	for _, target := range paths {
		if err := writeSkillFile(target); err != nil {
			return err
		}
	}
	manifest, err := loadSkillManifest(home)
	if err != nil {
		return err
	}
	manifest.Skills = removeSkillRecord(manifest.Skills, planningSkillID)
	manifest.Skills = append(manifest.Skills, installedSkill{ID: planningSkillID, Version: planningSkillVersion, Source: planningSkillSource, SHA256: skillDigest(), Scope: "global", InstalledAt: time.Now().UTC().Format(time.RFC3339), Paths: paths})
	if err := saveSkillManifest(home, manifest); err != nil {
		return err
	}
	fmt.Printf("Installed %s globally at %s\n", planningSkillID, canonical)
	return nil
}

func verifyPlanningSkill(options skillInstallOptions) error {
	home, err := skillHome(options)
	if err != nil {
		return err
	}
	manifest, err := loadSkillManifest(home)
	if err != nil {
		return err
	}
	var record *installedSkill
	for index := range manifest.Skills {
		if manifest.Skills[index].ID == planningSkillID {
			record = &manifest.Skills[index]
		}
	}
	if record == nil {
		return errors.New("planning skill is not installed")
	}
	if record.SHA256 != skillDigest() {
		return errors.New("installed planning skill metadata does not match this Leadfox build")
	}
	for _, target := range record.Paths {
		content, readErr := os.ReadFile(target)
		if readErr != nil {
			return fmt.Errorf("read installed skill %s: %w", target, readErr)
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != record.SHA256 {
			return fmt.Errorf("skill checksum mismatch: %s", target)
		}
	}
	fmt.Printf("Verified %s (%s)\n", planningSkillID, record.SHA256)
	return nil
}

func removePlanningSkill(options skillInstallOptions) error {
	home, err := skillHome(options)
	if err != nil {
		return err
	}
	manifest, err := loadSkillManifest(home)
	if err != nil {
		return err
	}
	for _, record := range manifest.Skills {
		if record.ID != planningSkillID {
			continue
		}
		for _, target := range record.Paths {
			content, readErr := os.ReadFile(target)
			if errors.Is(readErr, fs.ErrNotExist) {
				continue
			}
			if readErr != nil {
				return readErr
			}
			digest := sha256.Sum256(content)
			if hex.EncodeToString(digest[:]) != record.SHA256 {
				return fmt.Errorf("refusing to remove modified skill file: %s", target)
			}
			if err := os.Remove(target); err != nil {
				return fmt.Errorf("remove skill file %s: %w", target, err)
			}
		}
	}
	manifest.Skills = removeSkillRecord(manifest.Skills, planningSkillID)
	if err := saveSkillManifest(home, manifest); err != nil {
		return err
	}
	fmt.Printf("Removed %s\n", planningSkillID)
	return nil
}

func removeSkillRecord(records []installedSkill, id string) []installedSkill {
	filtered := records[:0]
	for _, record := range records {
		if record.ID != id {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func listSkills(options skillInstallOptions) error {
	home, err := skillHome(options)
	if err != nil {
		return err
	}
	manifest, err := loadSkillManifest(home)
	if err != nil {
		return err
	}
	if len(manifest.Skills) == 0 {
		fmt.Println("No Leadfox skills installed.")
		return nil
	}
	for _, record := range manifest.Skills {
		fmt.Printf("%s\t%s\t%s\n", record.ID, record.Version, record.Scope)
	}
	return nil
}
