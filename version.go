package main

import (
	"fmt"
	"runtime/debug"
)

const leadfoxModule = "leadfox"

func leadfoxVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "leadfox (development build)"
	}
	version := info.Main.Version
	if version == "" || version == "(devel)" {
		version = "development"
	}
	result := fmt.Sprintf("leadfox %s", version)
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			result += " (commit " + setting.Value + ")"
		}
	}
	return result
}
