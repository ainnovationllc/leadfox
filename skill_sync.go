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
)

type skillSyncReport struct {
	Project string `json:"project"`
	Skill   string `json:"skill"`
	Source  string `json:"source"`
	Version string `json:"version"`
	Status  string `json:"status"`
	Applied bool   `json:"applied"`
	Detail  string `json:"detail,omitempty"`
}

func syncPlanningSkill(options skillInstallOptions) error {
	project, err := os.Getwd()
	if err != nil {
		return err
	}
	report, record, err := planPlanningSkillSync(project, options)
	if err != nil {
		return err
	}
	if options.yes && report.Status == "ready" {
		if err := applyPlanningSkillSync(options, record); err != nil {
			return err
		}
		report.Applied = true
		report.Status = "synchronized"
	}
	if options.format == "json" {
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(encoded))
	} else {
		fmt.Printf("Skill: %s\nSource: %s\nVersion: %s\nStatus: %s\n", report.Skill, report.Source, report.Version, report.Status)
		if report.Detail != "" {
			fmt.Printf("%s\n", report.Detail)
		}
		if !options.yes && report.Status == "ready" {
			fmt.Println("Dry run only; repeat with --yes to synchronize the skill.")
		}
	}
	return nil
}

func planPlanningSkillSync(project string, options skillInstallOptions) (skillSyncReport, *projectSkillRecord, error) {
	records, err := readProjectSkillRegistry(filepath.Join(project, "control", "skills.yaml"))
	if err != nil {
		return skillSyncReport{}, nil, fmt.Errorf("read project skill registry: %w", err)
	}
	var record *projectSkillRecord
	for index := range records {
		if records[index].ID == planningSkillID && records[index].Scope == "global" {
			record = &records[index]
			break
		}
	}
	if record == nil {
		return skillSyncReport{}, nil, errors.New("project does not declare a global planning skill")
	}
	report := skillSyncReport{Project: project, Skill: record.ID, Source: record.Source, Version: record.Version, Status: "blocked"}
	if record.Source != planningSkillSource || record.Version != planningSkillVersion {
		report.Detail = "the pinned skill source or version is not available in this Leadfox build"
		return report, record, nil
	}
	if record.SHA256 != "" && record.SHA256 != skillDigest() {
		report.Detail = "the project pin does not match this Leadfox build"
		return report, record, nil
	}
	home, err := skillHome(options)
	if err != nil {
		return report, record, err
	}
	manifest, err := loadSkillManifest(home)
	if err != nil {
		return report, record, err
	}
	var installed *installedSkill
	for index := range manifest.Skills {
		if manifest.Skills[index].ID == planningSkillID {
			installed = &manifest.Skills[index]
			break
		}
	}
	if installed != nil {
		for _, path := range installed.Paths {
			content, readErr := os.ReadFile(path)
			if errors.Is(readErr, fs.ErrNotExist) {
				continue
			}
			if readErr != nil {
				return report, record, readErr
			}
			digest := sha256.Sum256(content)
			if hex.EncodeToString(digest[:]) != installed.SHA256 {
				report.Detail = fmt.Sprintf("refusing to overwrite modified skill: %s", path)
				return report, record, nil
			}
		}
		if installed.SHA256 == skillDigest() {
			report.Status = "current"
			report.Detail = "the installed skill already matches the project pin"
			return report, record, nil
		}
	}
	canonical := filepath.Join(skillRoot(home), planningSkillID, "SKILL.md")
	if content, readErr := os.ReadFile(canonical); readErr == nil {
		digest := sha256.Sum256(content)
		if installed == nil || hex.EncodeToString(digest[:]) != installed.SHA256 {
			report.Detail = "refusing to overwrite an unmanaged skill file; remove it explicitly after review"
			return report, record, nil
		}
	}
	report.Status = "ready"
	report.Detail = "the pinned built-in skill can be installed or updated without network access"
	return report, record, nil
}

func applyPlanningSkillSync(options skillInstallOptions, installed *projectSkillRecord) error {
	_ = installed
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
		for _, path := range record.Paths {
			if content, readErr := os.ReadFile(path); readErr == nil {
				digest := sha256.Sum256(content)
				if hex.EncodeToString(digest[:]) != record.SHA256 {
					return fmt.Errorf("refusing to overwrite modified skill: %s", path)
				}
			}
		}
		manifest.Skills = removeSkillRecord(manifest.Skills, planningSkillID)
		if err := saveSkillManifest(home, manifest); err != nil {
			return err
		}
		break
	}
	return installPlanningSkill(options)
}
