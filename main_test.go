package main

import (
	"os"
	"testing"
)

// TestMain disables volatile-location detection by default, because test
// repositories and their job worktrees live under the system temporary
// directory. Tests that exercise the policy install their own list with
// useVolatileRoots.
func TestMain(m *testing.M) {
	volatileWorktreeRoots = func() []string { return nil }
	os.Exit(m.Run())
}

func useVolatileRoots(t *testing.T, roots ...string) {
	t.Helper()
	previous := volatileWorktreeRoots
	volatileWorktreeRoots = func() []string { return roots }
	t.Cleanup(func() { volatileWorktreeRoots = previous })
}
