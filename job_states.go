package main

import (
	"fmt"
	"sort"
	"strings"
)

// Job lifecycle states. Names use the hyphenated style of the original
// states; the reliability specification's worker_lost and timed_out are
// written worker-lost and timed-out.
const (
	stateDraft            = "draft"
	stateReady            = "ready"
	stateRunning          = "running"
	statePaused           = "paused"
	stateBlocked          = "blocked"
	stateWorkerLost       = "worker-lost"
	stateTimedOut         = "timed-out"
	stateReview           = "review"
	stateReviewPending    = "review-pending"
	stateReviewFailed     = "review-failed"
	stateChangesRequested = "changes-requested"
	stateAccepted         = "accepted"
	stateMerged           = "merged"
	stateCancelled        = "cancelled"
)

type jobStateInfo struct {
	// Started states have a branch and normally a worktree.
	Started bool
	// Terminal states accept no further lifecycle operations except those
	// that explicitly allow them (recovery in a later phase).
	Terminal bool
	// Interrupted states mean execution stopped without a handoff.
	Interrupted bool
	// Meaning is shown in status output so that, for example, a pause is
	// never confused with a cancellation.
	Meaning    string
	NextAction string
}

var jobStates = map[string]jobStateInfo{
	stateDraft:            {Meaning: "packet is being prepared", NextAction: "complete the packet and mark it ready"},
	stateReady:            {Meaning: "approved for start; not started", NextAction: "start the approved job"},
	stateRunning:          {Started: true, Meaning: "work in progress", NextAction: "complete the work and return a handoff"},
	statePaused:           {Started: true, Meaning: "deliberately paused by Control; worktree kept", NextAction: "resume the job when work should continue"},
	stateBlocked:          {Started: true, Meaning: "waiting on an external blocker; worktree kept", NextAction: "resolve the blocker and resume or reissue the job"},
	stateWorkerLost:       {Started: true, Interrupted: true, Meaning: "the worker stopped without a handoff", NextAction: "inspect the branch and recorded progress, then resume or reissue from the preserved work"},
	stateTimedOut:         {Started: true, Interrupted: true, Meaning: "the job exceeded its time limit", NextAction: "inspect the branch and recorded progress, then resume or reissue from the preserved work"},
	stateReview:           {Started: true, Meaning: "under Control review", NextAction: "resolve review findings or record required attestations"},
	stateReviewPending:    {Started: true, Meaning: "review waiting for manual checks", NextAction: "resolve review findings or record required attestations"},
	stateReviewFailed:     {Started: true, Meaning: "review found failures", NextAction: "resolve review findings or record required attestations"},
	stateChangesRequested: {Started: true, Meaning: "changes requested after review", NextAction: "resolve review findings or record required attestations"},
	stateAccepted:         {Started: true, Meaning: "accepted; ready to merge", NextAction: "merge the accepted job"},
	stateMerged:           {Started: true, Terminal: true, Meaning: "completed and merged", NextAction: "no further lifecycle action"},
	stateCancelled:        {Terminal: true, Meaning: "cancelled; not completed", NextAction: "no further lifecycle action"},
}

func knownJobStatus(status string) bool {
	_, ok := jobStates[status]
	return ok
}

// jobOperationRule states which statuses a lifecycle operation may start
// from and, where fixed, the status it produces.
type jobOperationRule struct {
	From []string
	To   string
	Hint string
}

var (
	nonTerminalStates = []string{stateDraft, stateReady, stateRunning, statePaused, stateBlocked, stateWorkerLost, stateTimedOut, stateReview, stateReviewPending, stateReviewFailed, stateChangesRequested, stateAccepted}
	reviewStates      = []string{stateReview, stateReviewPending, stateReviewFailed}
)

// jobOperations is the single table of lifecycle preconditions. Operations
// whose result depends on evidence (review, attest, approve) leave To empty.
var jobOperations = map[string]jobOperationRule{
	"start":  {From: []string{stateReady}, To: stateRunning, Hint: "set it to ready after Control approval"},
	"pause":  {From: []string{stateRunning}, To: statePaused},
	"block":  {From: nonTerminalStates, To: stateBlocked},
	"cancel": {From: nonTerminalStates, To: stateCancelled},
	"resume": {From: []string{statePaused, stateBlocked, stateWorkerLost, stateTimedOut}, To: stateRunning},
	"sync":   {From: []string{stateRunning, statePaused, stateBlocked}},
	// Review may follow an interruption: Control can review recovered work
	// directly instead of resuming the worker.
	"review":   {From: []string{stateRunning, statePaused, stateBlocked, stateWorkerLost, stateTimedOut, stateReview, stateReviewPending, stateReviewFailed, stateChangesRequested, stateAccepted}},
	"approve":  {From: reviewStates},
	"attest":   {From: reviewStates},
	"evidence": {From: reviewStates},
	"merge":    {From: []string{stateAccepted}, To: stateMerged},
	"progress": {From: []string{stateRunning, statePaused, stateBlocked, stateWorkerLost, stateTimedOut, stateReview, stateReviewPending, stateReviewFailed, stateChangesRequested}},
}

// checkJobOperation returns an error when the operation is not allowed from
// the job's current status.
func checkJobOperation(id, operation, status string) error {
	rule, ok := jobOperations[operation]
	if !ok {
		return fmt.Errorf("unknown lifecycle operation %q", operation)
	}
	for _, from := range rule.From {
		if from == status {
			return nil
		}
	}
	if !knownJobStatus(status) {
		return fmt.Errorf("job %s has unrecognized status %q; %s is not allowed", id, status, operation)
	}
	allowed := append([]string(nil), rule.From...)
	sort.Strings(allowed)
	message := fmt.Sprintf("job %s is %q; %s requires one of: %s", id, status, operation, strings.Join(allowed, ", "))
	if rule.Hint != "" {
		message += "; " + rule.Hint
	}
	return fmt.Errorf("%s", message)
}

func jobStateMeaning(status string) string {
	if info, ok := jobStates[status]; ok {
		return info.Meaning
	}
	return "unrecognized status"
}

func isStartedJobStatus(status string) bool     { return jobStates[status].Started }
func isTerminalJobStatus(status string) bool    { return jobStates[status].Terminal }
func isInterruptedJobStatus(status string) bool { return jobStates[status].Interrupted }
