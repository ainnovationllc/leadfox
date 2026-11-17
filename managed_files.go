package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// updatableManagedFile is a project file that Leadfox writes and may later
// update. A copy whose content matches an earlier built-in version is known
// to be unmodified and is safe to replace; any other content is treated as
// a local edit and left alone.
type updatableManagedFile struct {
	Path    string
	Content func() string
	// Current is the SHA-256 of the built-in content. Previous lists the
	// SHA-256 of every earlier built-in version. When the built-in content
	// changes, move the old Current value into Previous and update Current;
	// TestManagedFileVersionsRecorded enforces this.
	Current  string
	Previous []string
}

var updatableManagedFiles = []updatableManagedFile{
	{
		Path:    agentRuntimeProtocolPath,
		Content: func() string { return agentRuntimeProtocolDoc },
		Current: "e573c10410424e756e9f651858650621d1ae4ea6b71589b01bffda93ab97aa7f",
		Previous: []string{
			"621213d16fedbdbe939ddf058100f1a12b24020b62d229e326b82b8236cfaf6d", // initial protocol (cec78c8)
		},
	},
}

const (
	managedMissing  = "missing"
	managedCurrent  = "current"
	managedOutdated = "outdated"
	managedModified = "modified"
)

func contentSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// state reports how a project's copy compares with the built-in versions.
func (file updatableManagedFile) state(root string) (string, error) {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
	if errors.Is(err, os.ErrNotExist) {
		return managedMissing, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", file.Path, err)
	}
	hash := contentSHA256(content)
	if hash == contentSHA256([]byte(file.Content())) {
		return managedCurrent, nil
	}
	for _, previous := range file.Previous {
		if hash == previous {
			return managedOutdated, nil
		}
	}
	return managedModified, nil
}

func findUpdatableManagedFile(path string) (updatableManagedFile, bool) {
	for _, file := range updatableManagedFiles {
		if file.Path == path {
			return file, true
		}
	}
	return updatableManagedFile{}, false
}

// outdatedManagedWarnings lists project check warnings for managed files
// that are unmodified copies of an earlier Leadfox version.
func outdatedManagedWarnings(root string) []string {
	var warnings []string
	for _, file := range updatableManagedFiles {
		if state, err := file.state(root); err == nil && state == managedOutdated {
			warnings = append(warnings, file.Path+" is an unmodified copy of an earlier Leadfox version; run leadfox project migrate --yes to update it")
		}
	}
	return warnings
}
