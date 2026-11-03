package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// AgentRole is a logical responsibility, not a particular model or vendor.
type AgentRole string

const (
	RoleControl        AgentRole = "control"
	RoleService        AgentRole = "service"
	RoleIntegration    AgentRole = "integration"
	RoleInfrastructure AgentRole = "infrastructure"
	RoleContract       AgentRole = "contract"
)

type AgentState string

const (
	AgentPending AgentState = "pending"
	AgentWorking AgentState = "working"
	AgentBlocked AgentState = "blocked"
	AgentDone    AgentState = "done"
	AgentFailed  AgentState = "failed"
)

// JobPacket is the provider-neutral unit of delegated work.
type JobPacket struct {
	ID              string
	Role            AgentRole
	Objective       string
	RequirementIDs  []string
	AllowedPaths    []string
	ExcludedPaths   []string
	InputArtifacts  []string
	Pins            []string
	Settings        []string
	Capabilities    []string
	ExpectedOutputs []string
	RequiredGates   []string
	StopConditions  []string
	HandoffFormat   string
}

type AgentHandle struct {
	ID    string
	JobID string
}

type AgentSnapshot struct {
	Handle AgentHandle
	State  AgentState
	Output string
}

type Handoff struct {
	JobID               string
	State               AgentState
	ChangedPaths        []string
	Checks              []string
	Output              string
	Assumptions         []string
	Unresolved          []string
	RecommendedNextStep string
}

// AgentRuntime is the boundary between Leadfox Control and an execution
// runtime such as a local shell, Herdr, or another future implementation.
type AgentRuntime interface {
	Start(context.Context, JobPacket) (AgentHandle, error)
	Prompt(context.Context, AgentHandle, string) error
	Wait(context.Context, AgentHandle) (AgentSnapshot, error)
	Read(context.Context, AgentHandle) (AgentSnapshot, error)
	Handoff(context.Context, AgentHandle, Handoff) error
}

// ManualRuntime is a deterministic local adapter for development and tests.
// It records jobs and prompts but does not launch a process. A future Herdr
// adapter can implement the same interface without changing project metadata.
type ManualRuntime struct {
	mu   sync.Mutex
	jobs map[string]*manualJob
}

type manualJob struct {
	packet   JobPacket
	snapshot AgentSnapshot
	handoff  *Handoff
}

func NewManualRuntime() *ManualRuntime {
	return &ManualRuntime{jobs: make(map[string]*manualJob)}
}

func (r *ManualRuntime) Start(_ context.Context, packet JobPacket) (AgentHandle, error) {
	if packet.ID == "" {
		return AgentHandle{}, errors.New("job packet ID is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.jobs[packet.ID]; exists {
		return AgentHandle{}, fmt.Errorf("job already exists: %s", packet.ID)
	}
	handle := AgentHandle{ID: "manual-" + packet.ID, JobID: packet.ID}
	r.jobs[packet.ID] = &manualJob{
		packet:   packet,
		snapshot: AgentSnapshot{Handle: handle, State: AgentPending},
	}
	return handle, nil
}

func (r *ManualRuntime) Prompt(_ context.Context, handle AgentHandle, prompt string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[handle.JobID]
	if !ok {
		return fmt.Errorf("unknown agent job: %s", handle.JobID)
	}
	job.snapshot.State = AgentWorking
	if prompt != "" {
		job.snapshot.Output += prompt + "\n"
	}
	return nil
}

func (r *ManualRuntime) Wait(ctx context.Context, handle AgentHandle) (AgentSnapshot, error) {
	select {
	case <-ctx.Done():
		return AgentSnapshot{}, ctx.Err()
	default:
	}
	return r.Read(ctx, handle)
}

func (r *ManualRuntime) Read(_ context.Context, handle AgentHandle) (AgentSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[handle.JobID]
	if !ok {
		return AgentSnapshot{}, fmt.Errorf("unknown agent job: %s", handle.JobID)
	}
	return job.snapshot, nil
}

func (r *ManualRuntime) Handoff(_ context.Context, handle AgentHandle, handoff Handoff) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[handle.JobID]
	if !ok {
		return fmt.Errorf("unknown agent job: %s", handle.JobID)
	}
	if handoff.JobID == "" {
		handoff.JobID = handle.JobID
	}
	if handoff.State == "" {
		handoff.State = AgentDone
	}
	job.handoff = &handoff
	job.snapshot.State = handoff.State
	job.snapshot.Output = handoff.Output
	return nil
}

// HandoffTimestamp is kept here as a small shared convention for persisted
// handoff records. Callers should serialize it rather than relying on local
// wall-clock formatting.
func HandoffTimestamp() time.Time { return time.Now().UTC() }
