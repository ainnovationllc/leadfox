package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Runtime events are appended to the job's journal by an execution host or
// worker. They never change the job packet directly: Control applies their
// consequences with job check, which commits the resulting transition.

const runtimeSettingsPath = "control/settings/runtime.yaml"

// nowFunc is the clock for runtime supervision; tests replace it.
var nowFunc = func() time.Time { return time.Now().UTC() }

type runtimeSettings struct {
	SchemaVersion       int    `yaml:"schema_version"`
	HeartbeatStaleAfter string `yaml:"heartbeat_stale_after"`
	WorkerLostAfter     string `yaml:"worker_lost_after"`
	JobTimeout          string `yaml:"job_timeout"`
}

type runtimeThresholds struct {
	StaleAfter time.Duration
	LostAfter  time.Duration
	Timeout    time.Duration // zero means no timeout
}

var defaultRuntimeThresholds = runtimeThresholds{StaleAfter: 15 * time.Minute, LostAfter: 60 * time.Minute}

// loadRuntimeThresholds reads project thresholds, applying the defaults
// confirmed by the owner, and a per-job timeout from the packet.
func loadRuntimeThresholds(root string, packet jobPacket) (runtimeThresholds, error) {
	thresholds := defaultRuntimeThresholds
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(runtimeSettingsPath)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return thresholds, fmt.Errorf("read %s: %w", runtimeSettingsPath, err)
	}
	if err == nil {
		var settings runtimeSettings
		if err := yaml.Unmarshal(content, &settings); err != nil {
			return thresholds, fmt.Errorf("parse %s: %w", runtimeSettingsPath, err)
		}
		for _, item := range []struct {
			name   string
			value  string
			target *time.Duration
		}{
			{"heartbeat_stale_after", settings.HeartbeatStaleAfter, &thresholds.StaleAfter},
			{"worker_lost_after", settings.WorkerLostAfter, &thresholds.LostAfter},
			{"job_timeout", settings.JobTimeout, &thresholds.Timeout},
		} {
			if strings.TrimSpace(item.value) == "" || item.value == "none" {
				continue
			}
			parsed, parseErr := time.ParseDuration(item.value)
			if parseErr != nil || parsed <= 0 {
				return thresholds, fmt.Errorf("%s %s must be a positive duration such as 15m: %q", runtimeSettingsPath, item.name, item.value)
			}
			*item.target = parsed
		}
	}
	if packet.Timeout != "" {
		parsed, parseErr := time.ParseDuration(packet.Timeout)
		if parseErr != nil || parsed <= 0 {
			return thresholds, fmt.Errorf("job %s timeout must be a positive duration such as 4h: %q", packet.ID, packet.Timeout)
		}
		thresholds.Timeout = parsed
	}
	if thresholds.LostAfter < thresholds.StaleAfter {
		return thresholds, fmt.Errorf("%s worker_lost_after must not be shorter than heartbeat_stale_after", runtimeSettingsPath)
	}
	return thresholds, nil
}

// Runtime conditions derived from journal events.
const (
	conditionNoRuntime    = "no-runtime"
	conditionHealthy      = "healthy"
	conditionStale        = "stale"
	conditionPresumedLost = "presumed-lost"
	conditionExited       = "exited-without-handoff"
	conditionDisconnected = "disconnected"
	conditionHandedOff    = "handed-off"
	// conditionClosed marks an execution already closed by a recorded
	// interruption; its events are no longer evaluated.
	conditionClosed = "execution-closed"
)

var runtimeEventTypes = map[string]bool{"register": true, "start": true, "heartbeat": true, "progress": true, "output-ref": true, "handoff": true, "exit": true, "disconnect": true, "notification": true}

type runtimeState struct {
	ExecutionID   string   `json:"execution_id,omitempty"`
	Runtime       string   `json:"runtime,omitempty"`
	RegisteredAt  string   `json:"registered_at,omitempty"`
	LastEventAt   string   `json:"last_event_at,omitempty"`
	LastEventType string   `json:"last_event_type,omitempty"`
	LastSeq       int64    `json:"last_seq,omitempty"`
	LastCommit    string   `json:"last_commit,omitempty"`
	HandoffAt     string   `json:"handoff_at,omitempty"`
	ExitedAt      string   `json:"exited_at,omitempty"`
	ExitCode      *int     `json:"exit_code,omitempty"`
	Condition     string   `json:"condition"`
	SilentFor     string   `json:"silent_for,omitempty"`
	Superseded    []string `json:"superseded_executions,omitempty"`
	Late          int      `json:"late_events,omitempty"`
	Duplicates    int      `json:"duplicate_events,omitempty"`
	Ignored       int      `json:"ignored_events,omitempty"`
	lastEventTime time.Time
	lastEventID   string
}

// deriveRuntimeState interprets journal events deterministically. Events
// apply in arrival order; a duplicate event ID is ignored; an event whose
// sequence number is not above the last applied one for its execution is
// counted as late and does not change state; events for an execution that
// was superseded by a later registration, or that was never registered,
// never change state.
func deriveRuntimeState(events []runtimeEvent, now time.Time, thresholds runtimeThresholds) runtimeState {
	state := runtimeState{Condition: conditionNoRuntime}
	seen := map[string]bool{}
	lastSeq := map[string]int64{}
	superseded := map[string]bool{}
	for _, event := range events {
		if event.EventID != "" {
			if seen[event.EventID] {
				state.Duplicates++
				continue
			}
			seen[event.EventID] = true
		}
		if event.Type == "notification" || event.Type == "progress" {
			continue
		}
		if event.ExecutionID == "" {
			state.Ignored++
			continue
		}
		if event.Type == "register" {
			if superseded[event.ExecutionID] || event.ExecutionID == state.ExecutionID {
				state.Late++
				continue
			}
			if state.ExecutionID != "" {
				superseded[state.ExecutionID] = true
				state.Superseded = append(state.Superseded, state.ExecutionID)
			}
			at, _ := time.Parse(time.RFC3339, event.At)
			state = runtimeState{Condition: conditionHealthy, ExecutionID: event.ExecutionID, Runtime: event.Runtime, RegisteredAt: event.At, LastEventAt: event.At, LastEventType: "register", LastSeq: event.Seq, LastCommit: event.Commit, Superseded: state.Superseded, Late: state.Late, Duplicates: state.Duplicates, Ignored: state.Ignored, lastEventTime: at, lastEventID: event.EventID}
			lastSeq[event.ExecutionID] = event.Seq
			continue
		}
		if event.ExecutionID != state.ExecutionID {
			if superseded[event.ExecutionID] {
				state.Late++
			} else {
				state.Ignored++
			}
			continue
		}
		if event.Seq <= lastSeq[event.ExecutionID] {
			state.Late++
			continue
		}
		lastSeq[event.ExecutionID] = event.Seq
		at, err := time.Parse(time.RFC3339, event.At)
		if err != nil {
			state.Ignored++
			continue
		}
		state.LastSeq, state.LastEventType, state.LastEventAt, state.lastEventID = event.Seq, event.Type, event.At, event.EventID
		if at.After(state.lastEventTime) {
			state.lastEventTime = at
		}
		if event.Commit != "" {
			state.LastCommit = event.Commit
		}
		switch event.Type {
		case "handoff":
			state.HandoffAt = event.At
		case "exit":
			state.ExitedAt = event.At
			state.ExitCode = event.ExitCode
		case "disconnect":
			if state.ExitedAt == "" {
				state.ExitedAt = event.At
			}
			state.Condition = conditionDisconnected
		}
	}
	if state.ExecutionID == "" {
		return state
	}
	silent := now.Sub(state.lastEventTime)
	if silent < 0 {
		silent = 0
	}
	state.SilentFor = silent.Round(time.Second).String()
	switch {
	case state.HandoffAt != "":
		state.Condition = conditionHandedOff
	case state.Condition == conditionDisconnected:
	case state.ExitedAt != "":
		state.Condition = conditionExited
	case silent >= thresholds.LostAfter:
		state.Condition = conditionPresumedLost
	case silent >= thresholds.StaleAfter:
		state.Condition = conditionStale
	default:
		state.Condition = conditionHealthy
	}
	return state
}

// jobTimedOut reports whether a running job has exceeded its timeout,
// measured from its most recent transition to running.
func jobExceededTimeout(packet jobPacket, now time.Time, thresholds runtimeThresholds) (bool, time.Duration) {
	if thresholds.Timeout <= 0 {
		return false, 0
	}
	start, ok := lastTransitionTo(packet, stateRunning)
	if !ok {
		return false, 0
	}
	started, err := time.Parse(time.RFC3339, start.RecordedAt)
	if err != nil {
		return false, 0
	}
	elapsed := now.Sub(started)
	return elapsed >= thresholds.Timeout, elapsed
}

var executionIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,96}$`)

// jobRuntimeCommand implements job runtime register|heartbeat|event|show.
func jobRuntimeCommand(root, action, id string, o jobOptions) error {
	packet, _, err := readJob(root, id)
	if err != nil {
		return err
	}
	if action == "show" {
		return showJobRuntime(root, packet, o.format)
	}
	if o.format != "" {
		return errors.New("--format applies only to job runtime show")
	}
	if err := checkJobOperation(id, "progress", packet.Status); err != nil {
		return fmt.Errorf("runtime events require a started job: %w", err)
	}
	events, _, err := readJournal(root, id)
	if err != nil {
		return err
	}
	event := runtimeEvent{Schema: journalSchemaVersion, Job: id, At: nowFunc().Format(time.RFC3339), Actor: o.actor, Branch: packet.Branch, Commit: currentJobCommit(root, packet), Worktree: packet.Worktree}
	if o.eventID != "" {
		if !executionIDPattern.MatchString(o.eventID) {
			return fmt.Errorf("--event-id %q is not a valid identifier", o.eventID)
		}
		for _, existing := range events {
			if existing.EventID == o.eventID {
				fmt.Printf("Duplicate event %s for %s ignored\n", o.eventID, id)
				return nil
			}
		}
		event.EventID = o.eventID
	} else if event.EventID, err = newEventID(); err != nil {
		return err
	}
	if err := validateJournalText("note", o.note); err != nil {
		return err
	}
	event.Note = o.note
	if err := validateJournalText("actor", o.actor); err != nil {
		return err
	}
	switch action {
	case "register":
		if strings.TrimSpace(o.runtime) == "" || !executionIDPattern.MatchString(o.runtime) {
			return errors.New("--runtime NAME is required (for example local, codex, claude, herdr)")
		}
		event.Type, event.Runtime = "register", o.runtime
		event.ExecutionID = o.executionID
		if event.ExecutionID == "" {
			event.ExecutionID = "exec-" + event.EventID[:12]
		}
		if !executionIDPattern.MatchString(event.ExecutionID) {
			return fmt.Errorf("--execution-id %q is not a valid identifier", event.ExecutionID)
		}
		for _, existing := range events {
			if existing.Type == "register" && existing.ExecutionID == event.ExecutionID {
				return fmt.Errorf("execution %s is already registered for %s", event.ExecutionID, id)
			}
		}
		event.Seq = o.seq
	case "heartbeat", "event":
		event.Type = "heartbeat"
		if action == "event" {
			event.Type = o.eventType
			if !runtimeEventTypes[event.Type] || event.Type == "register" || event.Type == "notification" || event.Type == "progress" || event.Type == "heartbeat" {
				return errors.New("--type must be start, output-ref, handoff, exit or disconnect")
			}
		}
		if o.executionID == "" {
			return errors.New("--execution-id is required; register the execution first")
		}
		registered := false
		for _, existing := range events {
			if existing.Type == "register" && existing.ExecutionID == o.executionID {
				registered = true
			}
		}
		if !registered {
			return fmt.Errorf("execution %s is not registered for %s", o.executionID, id)
		}
		event.ExecutionID = o.executionID
		event.Seq = o.seq
		if event.Seq == 0 {
			// Without a host-supplied sequence, number events in arrival
			// order for this execution.
			for _, existing := range events {
				if existing.ExecutionID == o.executionID && existing.Seq >= event.Seq {
					event.Seq = existing.Seq + 1
				}
			}
			if event.Seq == 0 {
				event.Seq = 1
			}
		}
		if event.Type == "exit" {
			if !o.used["exit-code"] {
				return errors.New("--exit-code is required for an exit event")
			}
			code := o.exitCode
			event.ExitCode = &code
		} else if o.used["exit-code"] {
			return errors.New("--exit-code applies only to exit events")
		}
		if event.Type == "output-ref" {
			if strings.TrimSpace(o.outputRef) == "" {
				return errors.New("--output-ref PATH|URL is required for an output-ref event")
			}
			if err := validateJournalText("output-ref", o.outputRef); err != nil {
				return err
			}
			if strings.ContainsAny(o.outputRef, "\n\r") {
				return errors.New("--output-ref must be a single path or URL, not output text")
			}
			event.OutputRef = o.outputRef
		} else if o.outputRef != "" {
			return errors.New("--output-ref applies only to output-ref events")
		}
	default:
		return fmt.Errorf("unknown runtime action %q; use register, heartbeat, event or show", action)
	}
	if err := appendJournalEvent(root, event); err != nil {
		return err
	}
	switch event.Type {
	case "register":
		fmt.Printf("Registered execution %s (%s) for %s\n", event.ExecutionID, event.Runtime, id)
	default:
		fmt.Printf("Recorded %s for %s execution %s (seq %d)\n", event.Type, id, event.ExecutionID, event.Seq)
	}
	if event.Type == "handoff" || event.Type == "exit" || event.Type == "disconnect" {
		fmt.Println("Control applies the result with: leadfox job check " + id)
	}
	return nil
}

func showJobRuntime(root string, packet jobPacket, format string) error {
	events, malformed, err := readJournal(root, packet.ID)
	if err != nil {
		return err
	}
	thresholds, err := loadRuntimeThresholds(root, packet)
	if err != nil {
		return err
	}
	state := deriveRuntimeState(events, nowFunc(), thresholds)
	if format == "json" {
		encoded, err := json.MarshalIndent(map[string]any{"job": packet.ID, "status": packet.Status, "runtime": state, "malformed_lines": malformed}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	if format != "" && format != "markdown" {
		return errors.New("--format requires markdown or json")
	}
	fmt.Printf("Job %s: %s\n", packet.ID, packet.Status)
	fmt.Printf("Runtime: %s\n", describeRuntime(state))
	for _, event := range events {
		if event.Type == "progress" {
			continue
		}
		line := fmt.Sprintf("- %s %s", event.At, event.Type)
		if event.ExecutionID != "" {
			line += fmt.Sprintf(" %s#%d", event.ExecutionID, event.Seq)
		}
		if event.Notification != nil {
			line += fmt.Sprintf(" %s (%s)", event.Notification.Event, event.Notification.State)
		}
		if event.ExitCode != nil {
			line += " exit=" + strconv.Itoa(*event.ExitCode)
		}
		if event.OutputRef != "" {
			line += " ref=" + event.OutputRef
		}
		fmt.Println(line)
	}
	if malformed > 0 {
		fmt.Printf("Warning: %d malformed journal line(s) skipped\n", malformed)
	}
	return nil
}

func describeRuntime(state runtimeState) string {
	if state.Condition == conditionClosed {
		return fmt.Sprintf("execution %s via %s was closed by a recorded interruption (last %s at %s); register a new execution when work resumes", state.ExecutionID, state.Runtime, state.LastEventType, state.LastEventAt)
	}
	if state.ExecutionID == "" {
		return "no runtime attached (manual workflow); worker loss is not detected automatically"
	}
	description := fmt.Sprintf("%s via %s: %s", state.ExecutionID, state.Runtime, state.Condition)
	if state.LastEventAt != "" {
		description += fmt.Sprintf("; last %s at %s (%s ago)", state.LastEventType, state.LastEventAt, state.SilentFor)
	}
	if state.Late > 0 || state.Duplicates > 0 || state.Ignored > 0 {
		description += fmt.Sprintf("; %d late, %d duplicate, %d ignored event(s)", state.Late, state.Duplicates, state.Ignored)
	}
	return description
}

const jobRuntimeHelpText = `Report execution-host events for a running job.

Usage:
  leadfox job runtime register <id> --runtime NAME [--execution-id ID]
  leadfox job runtime heartbeat <id> --execution-id ID [--seq N] [--event-id ID]
  leadfox job runtime event <id> --execution-id ID --type TYPE [options]
  leadfox job runtime show <id> [--format json]

Event types: start, output-ref (--output-ref PATH|URL), handoff,
exit (--exit-code N), disconnect.

Options:
      --seq N           Host sequence number; events not above the last
                        applied number are recorded as late and ignored
      --event-id ID     Host event ID; a repeated ID is ignored
      --note TEXT       Short note (credential-like text is refused)
      --actor NAME      Reporting identity

Events are appended to the Control checkout's runtime journal and may be
sent from the job worktree. They never change the job record directly:
Control runs leadfox job check to record worker-lost or timed-out.
Thresholds come from control/settings/runtime.yaml (defaults: stale after
15m, worker lost after 60m, no timeout); a packet may set timeout: 4h.
`
