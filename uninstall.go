package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type uninstallOptions struct {
	home     string
	dryRun   bool
	yes      bool
	keepPath bool
	showHelp bool
}

const uninstallHelpText = `Remove the user-local Leadfox installation.

Usage:
  leadfox uninstall --dry-run
  leadfox uninstall --yes

Options:
  --dry-run       Show managed files that would be removed
  --yes           Confirm removal; required for changes
  --home DIR      Test or alternate home directory
  --keep-path     Preserve the exact Leadfox PATH entry

Project directories and project-local skills are never removed.
`

type appInstallManifest struct {
	ActiveBinary string `json:"active_binary"`
	ActiveTarget string `json:"active_target"`
	BinarySHA256 string `json:"binary_sha256"`
}

func runUninstallCommand(args []string) error {
	options, err := parseUninstallOptions(args)
	if err != nil {
		return err
	}
	if options.showHelp {
		fmt.Print(uninstallHelpText)
		return nil
	}
	if !options.dryRun && !options.yes {
		return errors.New("uninstall changes user files; repeat with --yes after reviewing 'leadfox uninstall --dry-run'")
	}
	return uninstallLeadfox(options)
}

func parseUninstallOptions(args []string) (uninstallOptions, error) {
	var options uninstallOptions
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--help", "-h":
			options.showHelp = true
		case "--dry-run":
			options.dryRun = true
		case "--yes":
			options.yes = true
		case "--keep-path":
			options.keepPath = true
		case "--home":
			if index+1 >= len(args) {
				return options, errors.New("--home requires a directory")
			}
			index++
			options.home = args[index]
		default:
			return options, fmt.Errorf("unknown uninstall option %q", args[index])
		}
	}
	return options, nil
}

func uninstallHome(options uninstallOptions) (string, error) {
	if options.home != "" {
		return filepath.Abs(options.home)
	}
	return os.UserHomeDir()
}

func uninstallLeadfox(options uninstallOptions) error {
	home, err := uninstallHome(options)
	if err != nil {
		return err
	}
	root := filepath.Join(home, ".leadfox")
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Printf("No Leadfox installation found at %s\n", root)
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Leadfox installation: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to uninstall: %s is not a safe Leadfox directory", root)
	}

	planned := []string{}
	appManifest, appManifestOK := readAppManifest(root)
	if appManifestOK {
		if appManifest.ActiveBinary != "" {
			planned = append(planned, filepath.Join(root, appManifest.ActiveBinary))
		}
		if appManifest.ActiveTarget != "" {
			planned = append(planned, filepath.Join(root, appManifest.ActiveTarget))
		}
		planned = append(planned, filepath.Join(root, "manifest.json"), filepath.Join(root, "releases"))
	}

	skillManifest, skillManifestOK := loadSkillManifestForUninstall(filepath.Join(root, "skills", "manifest.json"))
	if skillManifestOK {
		for _, skill := range skillManifest.Skills {
			planned = append(planned, skill.Paths...)
		}
		planned = append(planned, filepath.Join(root, "skills", "manifest.json"))
	}
	if _, statErr := os.Stat(filepath.Join(root, "logs")); statErr == nil {
		planned = append(planned, filepath.Join(root, "logs"))
	}
	if !options.keepPath {
		planned = append(planned, pathConfigFiles(home)...)
	}

	for _, item := range planned {
		fmt.Printf("%s %s\n", map[bool]string{true: "would remove", false: "managed:"}[options.dryRun], item)
	}
	if options.dryRun {
		return nil
	}

	if appManifestOK {
		if err := removeAppFiles(root, appManifest); err != nil {
			return err
		}
	}
	if skillManifestOK {
		if err := removeSkillFiles(home, root, skillManifest); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(root, "skills", "manifest.json")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove skill manifest: %w", err)
		}
	}
	if appManifestOK {
		_ = os.Remove(filepath.Join(root, "manifest.json"))
	}
	if !options.keepPath {
		if err := removePathEntries(home); err != nil {
			return err
		}
	}
	removeEmptyDirectory(filepath.Join(root, "skills", planningSkillID))
	removeEmptyDirectory(filepath.Join(root, "skills"))
	removeEmptyDirectory(filepath.Join(root, "bin"))
	removeEmptyDirectory(filepath.Join(root, "releases"))
	removeEmptyDirectory(filepath.Join(root, "logs"))
	if entries, readErr := os.ReadDir(root); readErr == nil && len(entries) == 0 {
		if removeErr := os.Remove(root); removeErr != nil {
			return fmt.Errorf("remove empty Leadfox directory: %w", removeErr)
		}
	}
	fmt.Printf("Leadfox global installation removed where files were verified; project directories were not touched.\n")
	return nil
}

func readAppManifest(root string) (appInstallManifest, bool) {
	content, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return appInstallManifest{}, false
	}
	var manifest appInstallManifest
	if json.Unmarshal(content, &manifest) != nil || manifest.ActiveBinary == "" || manifest.ActiveTarget == "" {
		return appInstallManifest{}, false
	}
	return manifest, true
}

func loadSkillManifestForUninstall(file string) (skillManifest, bool) {
	content, err := os.ReadFile(file)
	if err != nil {
		return skillManifest{}, false
	}
	var manifest skillManifest
	if json.Unmarshal(content, &manifest) != nil {
		return skillManifest{}, false
	}
	return manifest, true
}

func removeAppFiles(root string, manifest appInstallManifest) error {
	target := filepath.Join(root, manifest.ActiveTarget)
	if !withinRoot(root, target) {
		return errors.New("refusing to remove an application target outside .leadfox")
	}
	if manifest.BinarySHA256 != "" {
		content, err := os.ReadFile(target)
		if err != nil {
			return fmt.Errorf("read installed binary: %w", err)
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != manifest.BinarySHA256 {
			return fmt.Errorf("preserving modified Leadfox binary: %s", target)
		}
	}
	if err := removeKnownPath(filepath.Join(root, manifest.ActiveBinary)); err != nil {
		return err
	}
	if err := removeKnownPath(target); err != nil {
		return err
	}
	return removeManagedTree(filepath.Join(root, "releases"))
}

func removeSkillFiles(home, root string, manifest skillManifest) error {
	for _, skill := range manifest.Skills {
		for _, file := range skill.Paths {
			if !allowedSkillPath(home, root, file) {
				return fmt.Errorf("refusing to remove unexpected skill path: %s", file)
			}
			content, err := os.ReadFile(file)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			digest := sha256.Sum256(content)
			if hex.EncodeToString(digest[:]) != skill.SHA256 {
				fmt.Printf("preserved modified skill: %s\n", file)
				continue
			}
			if err := removeKnownPath(file); err != nil {
				return err
			}
		}
	}
	return nil
}

func allowedSkillPath(home, root, file string) bool {
	if withinRoot(root, file) {
		return true
	}
	for _, agent := range []string{"claude", "codex"} {
		allowed := filepath.Join(home, "."+agent, "skills", planningSkillID, "SKILL.md")
		if filepath.Clean(file) == allowed {
			return true
		}
	}
	return false
}

func withinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func removeKnownPath(file string) error {
	info, err := os.Lstat(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("refusing to remove unexpected directory: %s", file)
	}
	return os.Remove(file)
}

func removeManagedTree(root string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("preserving unexpected release path: %s", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		entryPath := filepath.Join(root, entry.Name())
		if entry.IsDir() {
			if err := removeManagedTree(entryPath); err != nil {
				return err
			}
		} else if err := removeKnownPath(entryPath); err != nil {
			return err
		}
	}
	return os.Remove(root)
}

func removeEmptyDirectory(path string) {
	entries, err := os.ReadDir(path)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(path)
	}
}

func pathConfigFiles(home string) []string {
	return []string{filepath.Join(home, ".zshrc"), filepath.Join(home, ".bashrc")}
}

func removePathEntries(home string) error {
	for _, file := range pathConfigFiles(home) {
		content, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		lines := strings.Split(string(content), "\n")
		filtered := make([]string, 0, len(lines))
		for index := 0; index < len(lines); index++ {
			if lines[index] == "# Leadfox PATH" && index+1 < len(lines) && lines[index+1] == `export PATH="$HOME/.leadfox/bin:$PATH"` {
				index++
				continue
			}
			filtered = append(filtered, lines[index])
		}
		next := strings.Join(filtered, "\n")
		if next != string(content) {
			if err := os.WriteFile(file, []byte(next), 0o600); err != nil {
				return fmt.Errorf("remove Leadfox PATH entry from %s: %w", file, err)
			}
		}
	}
	return nil
}
