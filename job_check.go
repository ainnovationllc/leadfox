package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// jobInterruption is the durable record of a detected interruption.
type jobInterruption struct {
	State          string `yaml:"state"`
	Reason         string `yaml:"reason"`
	ExecutionID    string `yaml:"execution_id,omitempty"`
	Runtime        string `yaml:"runtime,omitempty"`
	LastEventAt    string `yaml:"last_event_at,omitempty"`
	LastCommit     string `yaml:"last_commit,omitempty"`
	RecoveryAction string `yaml:"recovery_action"`
	RecordedAt     string `yaml:"recorded_at"`
}

// jobAssessment is the read-only evaluation of a job's runtime health.
type jobAssessment struct {
	Runtime    runtimeState
	Closed     bool
	TimedOut   bool
	Elapsed    time.Duration
	Thresholds runtimeThresholds
	// Transition is the state job check would record, if any.
	Transition string
	Reason     string
}

// assessJob evaluates a running job without changing anything. Executions
// already closed by a recorded interruption are not evaluated again, so a
// resumed job is not immediately re-marked lost by its old execution.
func assessJob(root string, packet jobPacket, now time.Time) (jobAssessment, error) {
	assessment := jobAssessment{Runtime: runtimeState{Condition: conditionNoRuntime}}
	thresholds, err := loadRuntimeThresholds(root, packet)
	if err != nil {
		return assessment, err
	}
	assessment.Thresholds = thresholds
	events, _, err := readJournal(root, packet.ID)
	if err != nil {
		return assessment, err
	}
	state := deriveRuntimeState(events, now, thresholds)
	for _, interruption := range packet.Interruptions {
		if state.ExecutionID != "" && interruption.ExecutionID == state.ExecutionID {
			assessment.Closed = true
		}
	}
	if assessment.Closed {
		state = runtimeState{Condition: conditionClosed, ExecutionID: state.ExecutionID, Runtime: state.Runtime, LastEventAt: state.LastEventAt, LastEventType: state.LastEventType, LastCommit: state.LastCommit}
	}
	assessment.Runtime = state
	if packet.Status != stateRunning {
		return assessment, nil
	}
	assessment.TimedOut, assessment.Elapsed = jobExceededTimeout(packet, now, thresholds)
	last := state.LastCommit
	if last == "" {
		last = currentJobCommit(root, packet)
	}
	commitText := ""
	if last != "" {
		commitText = "; last commit " + shortCommit(last)
	}
	switch state.Condition {
	case conditionExited:
		code := "unknown"
		if state.ExitCode != nil {
			code = fmt.Sprint(*state.ExitCode)
		}
		assessment.Transition = stateWorkerLost
		assessment.Reason = fmt.Sprintf("worker %s exited (code %s) at %s without a handoff%s", state.ExecutionID, code, state.ExitedAt, commitText)
	case conditionDisconnected:
		assessment.Transition = stateWorkerLost
		assessment.Reason = fmt.Sprintf("runtime reported %s disconnected at %s%s", state.ExecutionID, state.ExitedAt, commitText)
	case conditionPresumedLost:
		assessment.Transition = stateWorkerLost
		assessment.Reason = fmt.Sprintf("no runtime event from %s for %s (worker_lost_after %s); last %s at %s%s", state.ExecutionID, state.SilentFor, thresholds.LostAfter, state.LastEventType, state.LastEventAt, commitText)
	}
	if assessment.Transition == "" && assessment.TimedOut {
		assessment.Transition = stateTimedOut
		assessment.Reason = fmt.Sprintf("running for %s, exceeding the %s timeout%s", assessment.Elapsed.Round(time.Second), thresholds.Timeout, commitText)
	}
	return assessment, nil
}

// attentionText summarizes what job status should warn about for a job that
// job check would change or report.
func (a jobAssessment) attentionText() string {
	switch {
	case a.Transition != "":
		return fmt.Sprintf("%s; run leadfox job check to record %s", a.Reason, a.Transition)
	case a.Runtime.Condition == conditionStale:
		return fmt.Sprintf("worker %s has been silent for %s (stale after %s)", a.Runtime.ExecutionID, a.Runtime.SilentFor, a.Thresholds.StaleAfter)
	case a.Runtime.Condition == conditionHandedOff:
		return fmt.Sprintf("worker %s handed off at %s; review the job", a.Runtime.ExecutionID, a.Runtime.HandoffAt)
	}
	return ""
}

// jobCheck applies runtime findings: it records worker-lost and timed-out
// transitions with an interruption record, and emits notifications for
// stale workers and handoffs once per condition.
func jobCheck(root string, ids []string, all bool) error {
	if all {
		entries, err := os.ReadDir(filepath.Join(root, "control", "jobs"))
		if err != nil {
			return fmt.Errorf("read job directory: %w", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") && entry.Name() != "README.md" {
				ids = append(ids, strings.TrimSuffix(entry.Name(), ".md"))
			}
		}
	}
	if len(ids) == 0 {
		return errors.New("a job ID or --all is required")
	}
	sort.Strings(ids)
	now := nowFunc()
	failures := 0
	fmt.Println("JOB\tSTATUS\tRUNTIME\tRESULT")
	for _, id := range ids {
		result, status, condition, err := checkOneJob(root, id, now)
		if err != nil {
			failures++
			result = "error: " + err.Error()
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", id, status, condition, result)
	}
	if failures > 0 {
		return fmt.Errorf("job check could not evaluate %d job(s)", failures)
	}
	return nil
}

func checkOneJob(root, id string, now time.Time) (result, status, condition string, err error) {
	packet, body, err := readJob(root, id)
	if err != nil {
		return "", "unreadable", "-", err
	}
	status = packet.Status
	if packet.Status != stateRunning {
		return "not running; nothing to check", status, "-", nil
	}
	assessment, err := assessJob(root, packet, now)
	if err != nil {
		return "", status, "-", err
	}
	condition = assessment.Runtime.Condition
	events, _, _ := readJournal(root, id)
	notified := notifiedKeys(events)
	if assessment.Transition != "" {
		staged, stagedErr := gitStdout(root, nil, "diff", "--cached", "--name-only")
		if stagedErr != nil {
			return "", status, condition, stagedErr
		}
		if staged != "" {
			return "", status, condition, errors.New("the Control checkout has staged changes; commit or unstage them before job check records a transition")
		}
		operation := "lose"
		if assessment.Transition == stateTimedOut {
			operation = "time-out"
		}
		if err := checkJobOperation(id, operation, packet.Status); err != nil {
			return "", status, condition, err
		}
		recordedAt := now.Format(time.RFC3339)
		packetAfter := packet
		packetAfter.Status = assessment.Transition
		recovery := recoveryActionFor(packetAfter, assessment.Transition)
		appendTransition(&packet, assessment.Transition, assessment.Reason, "leadfox job check", recordedAt)
		packet.Status, packet.UpdatedAt = assessment.Transition, recordedAt
		packet.Interruptions = append(packet.Interruptions, jobInterruption{
			State:          assessment.Transition,
			Reason:         assessment.Reason,
			ExecutionID:    assessment.Runtime.ExecutionID,
			Runtime:        assessment.Runtime.Runtime,
			LastEventAt:    assessment.Runtime.LastEventAt,
			LastCommit:     assessment.Runtime.LastCommit,
			RecoveryAction: recovery,
			RecordedAt:     recordedAt,
		})
		if err := writeJob(root, packet, body); err != nil {
			return "", status, condition, err
		}
		if err := commitLifecycleRecord(root, []string{jobPath(root, id)}, "Record "+assessment.Transition+" "+id); err != nil {
			return "", status, condition, err
		}
		notifyJob(root, packet, assessment.Transition, assessment.Reason, assessment.Runtime.LastCommit, assessment.Transition+":"+assessment.Runtime.ExecutionID+":"+recordedAt, true)
		return "recorded " + assessment.Transition + ": " + assessment.Reason, assessment.Transition, condition, nil
	}
	switch condition {
	case conditionStale:
		key := "stale:" + assessment.Runtime.ExecutionID + ":" + assessment.Runtime.lastEventID
		if !notified[key] {
			notifyJob(root, packet, "stale-progress", "silent for "+assessment.Runtime.SilentFor, assessment.Runtime.LastCommit, key, true)
		}
		return "stale; worker silent for " + assessment.Runtime.SilentFor, status, condition, nil
	case conditionHandedOff:
		key := "handoff:" + assessment.Runtime.ExecutionID
		if !notified[key] {
			notifyJob(root, packet, "handoff", "", assessment.Runtime.LastCommit, key, false)
		}
		return "handed off; ready for review", status, condition, nil
	case conditionNoRuntime:
		return "no runtime attached; manual workflow", status, condition, nil
	case conditionClosed:
		return "previous execution closed by a recorded interruption; no runtime attached", status, condition, nil
	}
	return "healthy", status, condition, nil
}
