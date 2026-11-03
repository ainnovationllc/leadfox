package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func jobRetain(root, id string, options jobOptions) error {
	packet, body, err := readJob(root, id)
	if err != nil {
		return err
	}
	if options.artifactID == "" || options.actor == "" {
		return errors.New("job retain requires --artifact-id and --actor")
	}
	index := -1
	for i := range packet.DurableArtifacts {
		if packet.DurableArtifacts[i].ID == options.artifactID {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("durable artifact %q is not declared in job %s", options.artifactID, id)
	}
	artifact := &packet.DurableArtifacts[index]
	if artifact.Retained {
		return fmt.Errorf("durable artifact %q is already retained", artifact.ID)
	}
	if !isHexHash(strings.ToLower(artifact.SHA256)) {
		return fmt.Errorf("durable artifact %q has no valid expected SHA-256", artifact.ID)
	}
	base := root
	if packet.Worktree != "" {
		base = packet.Worktree
	}
	source := artifact.Source
	if !filepath.IsAbs(source) {
		source = filepath.Join(base, source)
	}
	destination := artifact.Destination
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(root, destination)
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	for _, worktree := range uniqueStrings(append([]string{packet.Worktree, packet.ReviewWorktree}, packet.ReviewWorktrees...)) {
		if worktree != "" && isWithin(worktree, destination) {
			return errors.New("durable artifact destination must be outside disposable job worktrees")
		}
	}
	actual, err := hashFile(source)
	if err != nil {
		return fmt.Errorf("hash durable artifact source: %w", err)
	}
	if !strings.EqualFold(actual, artifact.SHA256) {
		return fmt.Errorf("durable artifact %q hash mismatch: expected %s, got %s", artifact.ID, artifact.SHA256, actual)
	}
	if err := copyRetainedArtifact(source, destination); err != nil {
		return err
	}
	retainedHash, err := hashFile(destination)
	if err != nil {
		return err
	}
	if !strings.EqualFold(retainedHash, artifact.SHA256) {
		return errors.New("retained artifact hash changed during copy")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	artifact.Retained, artifact.RetainedSHA256, artifact.RetainedBy, artifact.RetainedAt = true, retainedHash, options.actor, now
	commit, _ := resolveCommit(root, "HEAD")
	packet.AuditRecords = append(packet.AuditRecords, jobAuditRecord{Event: "durable-artifact-retained", Actor: options.actor, RecordedAt: now, Commit: commit, Details: artifact.ID + " -> " + destination})
	packet.UpdatedAt = now
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	paths := []string{jobPath(root, id)}
	if isWithin(root, destination) {
		relative, relErr := filepath.Rel(root, destination)
		if relErr == nil {
			paths = append(paths, relative)
		}
	}
	if err := commitLifecycleRecord(root, paths, "Record durable artifact "+id); err != nil {
		return err
	}
	fmt.Printf("Retained durable artifact %s for %s\nSHA-256: %s\n", artifact.ID, id, retainedHash)
	return nil
}

func copyRetainedArtifact(source, destination string) error {
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("retained artifact destination already exists: %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create retention directory: %w", err)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".leadfox-retain-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, input); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return fmt.Errorf("activate retained artifact: %w", err)
	}
	return nil
}

func validateDurableArtifacts(packet jobPacket) error {
	for _, artifact := range packet.DurableArtifacts {
		if !artifact.Required {
			continue
		}
		if !artifact.Retained {
			return fmt.Errorf("required durable artifact %q has not been retained", artifact.ID)
		}
		if !isHexHash(strings.ToLower(artifact.SHA256)) || !strings.EqualFold(artifact.SHA256, artifact.RetainedSHA256) {
			return fmt.Errorf("required durable artifact %q has no matching retained hash", artifact.ID)
		}
	}
	return nil
}

func ensureCleanupPathAllowed(root, path string) error {
	if path == "" {
		return nil
	}
	operations, err := loadOperations(root)
	if err != nil {
		return nil
	}
	if isWithin(operations.Checkout, path) || isWithin(path, operations.Checkout) {
		return errors.New("refusing cleanup of the registered operations checkout")
	}
	return nil
}
