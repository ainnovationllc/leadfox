package main

import (
	"context"
	"testing"
)

func TestManualRuntimeLifecycle(t *testing.T) {
	runtime := NewManualRuntime()
	ctx := context.Background()
	packet := JobPacket{ID: "job-001", Role: RoleService, Objective: "Implement the service"}
	handle, err := runtime.Start(ctx, packet)
	if err != nil {
		t.Fatalf("start job: %v", err)
	}
	if err := runtime.Prompt(ctx, handle, "Begin work"); err != nil {
		t.Fatalf("prompt agent: %v", err)
	}
	snapshot, err := runtime.Wait(ctx, handle)
	if err != nil {
		t.Fatalf("wait for agent: %v", err)
	}
	if snapshot.State != AgentWorking {
		t.Fatalf("expected working state, got %q", snapshot.State)
	}
	if err := runtime.Handoff(ctx, handle, Handoff{
		State:        AgentDone,
		ChangedPaths: []string{"services/example"},
		Checks:       []string{"go test ./..."},
		Output:       "Implementation complete",
	}); err != nil {
		t.Fatalf("record handoff: %v", err)
	}
	snapshot, err = runtime.Read(ctx, handle)
	if err != nil {
		t.Fatalf("read handoff state: %v", err)
	}
	if snapshot.State != AgentDone || snapshot.Output != "Implementation complete" {
		t.Fatalf("unexpected final snapshot: %+v", snapshot)
	}
}

func TestManualRuntimeRejectsDuplicateJobs(t *testing.T) {
	runtime := NewManualRuntime()
	packet := JobPacket{ID: "job-001"}
	if _, err := runtime.Start(context.Background(), packet); err != nil {
		t.Fatalf("start initial job: %v", err)
	}
	if _, err := runtime.Start(context.Background(), packet); err == nil {
		t.Fatal("expected duplicate job to be rejected")
	}
}
