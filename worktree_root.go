package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// worktreeSettingsPath holds the project's persistent job-worktree policy.
// The file is optional: without it, job start requires an explicit worktree
// path, as it always has.
const worktreeSettingsPath = "control/settings/worktrees.yaml"

type worktreeSettings struct {
	SchemaVersion int    `yaml:"schema_version"`
	Root          string `yaml:"root"`
	AllowVolatile bool   `yaml:"allow_volatile"`
	present       bool
}

type jobWorktreeOverride struct {
	Path       string `yaml:"path"`
	Location   string `yaml:"volatile_location"`
	Reason     string `yaml:"reason"`
	Actor      string `yaml:"actor"`
	RecordedAt string `yaml:"recorded_at"`
}

func loadWorktreeSettings(root string) (worktreeSettings, error) {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(worktreeSettingsPath)))
	if errors.Is(err, os.ErrNotExist) {
		return worktreeSettings{}, nil
	}
	if err != nil {
		return worktreeSettings{}, fmt.Errorf("read %s: %w", worktreeSettingsPath, err)
	}
	var settings worktreeSettings
	if err := yaml.Unmarshal(content, &settings); err != nil {
		return worktreeSettings{}, fmt.Errorf("parse %s: %w", worktreeSettingsPath, err)
	}
	if settings.SchemaVersion != 0 && settings.SchemaVersion != 1 {
		return worktreeSettings{}, fmt.Errorf("%s schema_version %d is not supported", worktreeSettingsPath, settings.SchemaVersion)
	}
	if strings.TrimSpace(settings.Root) == "" {
		return worktreeSettings{}, fmt.Errorf("%s requires a root", worktreeSettingsPath)
	}
	settings.present = true
	return settings, nil
}

// resolveWorktreeRoot returns the absolute job-worktree root configured for
// the project. Relative roots are resolved from the Control checkout.
func resolveWorktreeRoot(root string, settings worktreeSettings) (string, error) {
	expanded, err := expandHomePath(settings.Root)
	if err != nil {
		return "", fmt.Errorf("%s root: %w", worktreeSettingsPath, err)
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(root, expanded)
	}
	return filepath.Clean(expanded), nil
}

// expandHomePath expands "~" and "~/..." to the current user's home
// directory. Other "~user" forms are refused rather than misread as a
// relative path inside the Control checkout.
func expandHomePath(value string) (string, error) {
	if value != "~" && !strings.HasPrefix(value, "~/") && !strings.HasPrefix(value, "~"+string(filepath.Separator)) {
		if strings.HasPrefix(value, "~") {
			return "", fmt.Errorf("unsupported home-directory form %q; use ~/ or an absolute path", value)
		}
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand %q: %w", value, err)
	}
	if value == "~" {
		return home, nil
	}
	return filepath.Join(home, value[2:]), nil
}

// resolveJobWorktreePath chooses and normalizes a job worktree path. Order of
// precedence: the packet's recorded worktree, the --worktree option, then the
// configured root joined with the job ID. Packet paths are relative to the
// Control checkout; option paths are relative to the current directory.
func resolveJobWorktreePath(root, id, packetValue, optionValue string, settings worktreeSettings) (string, error) {
	var value string
	relativeTo := root
	switch {
	case packetValue != "":
		value = packetValue
	case optionValue != "":
		value = optionValue
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		relativeTo = cwd
	case settings.present:
		base, err := resolveWorktreeRoot(root, settings)
		if err != nil {
			return "", err
		}
		return filepath.Join(base, id), nil
	default:
		return "", fmt.Errorf("a worktree path is required; pass --worktree DIR or configure a persistent root in %s", worktreeSettingsPath)
	}
	expanded, err := expandHomePath(value)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(relativeTo, expanded)
	}
	return filepath.Clean(expanded), nil
}

// volatileWorktreeRoots lists locations that hosts clear on reboot or after a
// period without access. Tests replace it so temporary test repositories can
// host job worktrees.
var volatileWorktreeRoots = defaultVolatileWorktreeRoots

func defaultVolatileWorktreeRoots() []string {
	roots := []string{"/tmp", "/private/tmp", "/var/tmp", "/private/var/tmp", "/var/folders", "/private/var/folders", "/dev/shm", os.TempDir()}
	if tmp := os.Getenv("TMPDIR"); tmp != "" {
		roots = append(roots, tmp)
	}
	return roots
}

// volatileWorktreeLocation reports the volatile location containing path, if
// any. Both sides are compared after resolving symbolic links so /tmp and
// /private/tmp, or /var and /private/var, are treated alike.
func volatileWorktreeLocation(path string) (string, bool) {
	resolved := resolveExistingPrefix(path)
	for _, candidate := range volatileWorktreeRoots() {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		base := resolveExistingPrefix(filepath.Clean(candidate))
		if base == string(filepath.Separator) {
			continue
		}
		if isWithin(base, resolved) || isWithin(filepath.Clean(candidate), filepath.Clean(path)) {
			return filepath.Clean(candidate), true
		}
	}
	return "", false
}

// resolveExistingPrefix resolves symbolic links in the longest existing
// prefix of path and re-appends the components that do not exist yet.
func resolveExistingPrefix(path string) string {
	path = filepath.Clean(path)
	missing := []string{}
	current := path
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// checkJobWorktreeLocation enforces the volatile-location policy for a new
// job worktree. It returns an override record when the project permits the
// override and the caller supplied a reason.
func checkJobWorktreeLocation(worktree string, settings worktreeSettings, allowVolatile bool, reason, actor, now string) (*jobWorktreeOverride, error) {
	location, volatile := volatileWorktreeLocation(worktree)
	if !volatile {
		if allowVolatile {
			return nil, errors.New("--allow-volatile-worktree was given but the worktree is not in a volatile location")
		}
		return nil, nil
	}
	if !allowVolatile {
		return nil, fmt.Errorf("job worktree %s is in volatile location %s, which the host may clear; use a persistent folder (configure %s)", worktree, location, worktreeSettingsPath)
	}
	if !settings.AllowVolatile {
		return nil, fmt.Errorf("volatile worktree override is not permitted; set allow_volatile: true in %s to allow it", worktreeSettingsPath)
	}
	if strings.TrimSpace(reason) == "" {
		return nil, errors.New("--reason is required with --allow-volatile-worktree")
	}
	return &jobWorktreeOverride{Path: worktree, Location: location, Reason: reason, Actor: actor, RecordedAt: now}, nil
}

// worktreeWarnings explains location risks for a job that already has a
// worktree, including jobs started before volatile paths were rejected.
func worktreeWarnings(packet jobPacket) []string {
	var warnings []string
	if packet.WorktreeOverride != nil && packet.Worktree != "" {
		warnings = append(warnings, fmt.Sprintf("worktree %s uses a volatile location by recorded override (%s, by %s)", packet.WorktreeOverride.Path, packet.WorktreeOverride.Reason, packet.WorktreeOverride.Actor))
	} else if packet.Worktree != "" {
		if location, volatile := volatileWorktreeLocation(packet.Worktree); volatile {
			warnings = append(warnings, fmt.Sprintf("worktree %s is in volatile location %s and may be cleared by the host", packet.Worktree, location))
		}
	}
	if packet.Worktree != "" && isActiveJobStatus(packet.Status) {
		if _, err := os.Stat(packet.Worktree); errors.Is(err, os.ErrNotExist) {
			warnings = append(warnings, fmt.Sprintf("worktree %s is missing; work committed to branch %s is preserved", packet.Worktree, packet.Branch))
		}
	}
	return warnings
}

func isActiveJobStatus(status string) bool {
	switch status {
	case "running", "blocked", "review", "review-pending", "review-failed", "changes-requested", "accepted":
		return true
	}
	return false
}

// checkWorktreeSettings validates the optional project worktree policy for
// project check.
func checkWorktreeSettings(root string) (invalid []string, warnings []string) {
	settings, err := loadWorktreeSettings(root)
	if err != nil {
		return []string{err.Error()}, nil
	}
	if !settings.present {
		return nil, []string{worktreeSettingsPath + " is missing; job start requires an explicit --worktree path"}
	}
	resolved, err := resolveWorktreeRoot(root, settings)
	if err != nil {
		return []string{err.Error()}, nil
	}
	if err := ensureExternalWorktree(root, resolved); err != nil {
		return []string{fmt.Sprintf("%s root must be outside the Control checkout: %s", worktreeSettingsPath, resolved)}, nil
	}
	if location, volatile := volatileWorktreeLocation(resolved); volatile {
		return []string{fmt.Sprintf("%s root %s is in volatile location %s", worktreeSettingsPath, resolved, location)}, nil
	}
	return nil, nil
}
