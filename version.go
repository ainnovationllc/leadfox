package main

import (
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
)

const leadfoxModule = "leadfox"

var buildVersion string

type buildInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

func leadfoxVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok && buildVersion == "" {
		return "leadfox (development build)"
	}
	version := buildVersion
	if version == "" && ok {
		version = info.Main.Version
	}
	if version == "" || version == "(devel)" {
		version = "development"
	}
	result := fmt.Sprintf("leadfox %s", version)
	if ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				result += " (commit " + setting.Value + ")"
			}
		}
	}
	return result
}

func leadfoxBuildInfo() buildInfo {
	version := "development"
	commit := ""
	if buildVersion != "" {
		version = buildVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if version == "development" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				commit = setting.Value
			}
		}
	}
	return buildInfo{Name: leadfoxModule, Version: version, Commit: commit, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
}

func leadfoxBuildInfoJSON() (string, error) {
	encoded, err := json.MarshalIndent(leadfoxBuildInfo(), "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode build information: %w", err)
	}
	return string(encoded), nil
}
