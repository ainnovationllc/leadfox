package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const worktreeConfigPath = "control/worktree.yaml"

type worktreeConfig struct {
	Links     []worktreeLink     `yaml:"links,omitempty"`
	Resources []worktreeResource `yaml:"resources,omitempty"`
}

type worktreeLink struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

type worktreeResource struct {
	ID     string `yaml:"id"`
	Marker string `yaml:"marker"`
}

func loadWorktreeConfig(root string) (worktreeConfig, error) {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(worktreeConfigPath)))
	if errors.Is(err, os.ErrNotExist) {
		return worktreeConfig{}, nil
	}
	if err != nil {
		return worktreeConfig{}, fmt.Errorf("read %s: %w", worktreeConfigPath, err)
	}
	var config worktreeConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return worktreeConfig{}, fmt.Errorf("parse %s: %w", worktreeConfigPath, err)
	}
	for _, link := range config.Links {
		if err := validateRelativeWorktreePath(link.Source, "link source"); err != nil {
			return worktreeConfig{}, err
		}
		if err := validateRelativeWorktreePath(link.Target, "link target"); err != nil {
			return worktreeConfig{}, err
		}
	}
	for _, resource := range config.Resources {
		if strings.TrimSpace(resource.ID) == "" || strings.TrimSpace(resource.Marker) == "" {
			return worktreeConfig{}, errors.New("worktree resources require id and marker")
		}
		if err := validateRelativeWorktreePath(resource.Marker, "resource marker"); err != nil {
			return worktreeConfig{}, err
		}
	}
	return config, nil
}

func validateRelativeWorktreePath(value, label string) error {
	if strings.TrimSpace(value) == "" || filepath.IsAbs(value) || filepath.Clean(value) == "." || strings.HasPrefix(filepath.Clean(value), ".."+string(filepath.Separator)) || filepath.Clean(value) == ".." {
		return fmt.Errorf("%s must be a non-empty project-relative path: %q", label, value)
	}
	return nil
}

func applyWorktreeEnvironment(root, worktree string) error {
	config, err := loadWorktreeConfig(root)
	if err != nil {
		return err
	}
	for _, link := range config.Links {
		source := filepath.Join(root, filepath.FromSlash(link.Source))
		target := filepath.Join(worktree, filepath.FromSlash(link.Target))
		if _, err := os.Stat(source); err != nil {
			return fmt.Errorf("worktree link source %s is unavailable: %w", link.Source, err)
		}
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("worktree link target already exists: %s", link.Target)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		relative, err := filepath.Rel(filepath.Dir(target), source)
		if err != nil {
			return err
		}
		if err := os.Symlink(relative, target); err != nil {
			return fmt.Errorf("create worktree link %s: %w", link.Target, err)
		}
	}
	return nil
}

func ensureNoActiveWorktreeResources(root, worktree string) error {
	config, err := loadWorktreeConfig(root)
	if err != nil {
		return err
	}
	for _, resource := range config.Resources {
		marker := filepath.Join(worktree, filepath.FromSlash(resource.Marker))
		if _, err := os.Stat(marker); err == nil {
			return fmt.Errorf("protected runtime resource %q is still active (marker %s); stop it before cleanup", resource.ID, resource.Marker)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
