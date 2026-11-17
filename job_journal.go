package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The runtime journal holds progress and, in a later phase, runtime events.
// It lives in the Control checkout under .local/, is never committed, and is
// never read by review, approval or merge: progress is not evidence.
const (
	journalDirectory     = ".local/leadfox/runtime"
	journalSchemaVersion = 1
	maxJournalTextLength = 1000
)

type runtimeEvent struct {
	Schema       int              `json:"schema"`
	EventID      string           `json:"event_id"`
	ExecutionID  string           `json:"execution_id,omitempty"`
	Seq          int64            `json:"seq,omitempty"`
	Job          string           `json:"job"`
	Type         string           `json:"type"`
	At           string           `json:"at"`
	Actor        string           `json:"actor,omitempty"`
	Branch       string           `json:"branch,omitempty"`
	Commit       string           `json:"commit,omitempty"`
	Worktree     string           `json:"worktree,omitempty"`
	Progress     *progressData    `json:"progress,omitempty"`
	Runtime      string           `json:"runtime,omitempty"`
	Note         string           `json:"note,omitempty"`
	ExitCode     *int             `json:"exit_code,omitempty"`
	OutputRef    string           `json:"output_ref,omitempty"`
	Notification *jobNotification `json:"notification,omitempty"`
}

type progressData struct {
	Remaining string   `json:"remaining,omitempty"`
	Next      string   `json:"next,omitempty"`
	Blocker   string   `json:"blocker,omitempty"`
	Gates     []string `json:"gates,omitempty"`
}

// controlRoot locates the Control checkout from any directory in it or in
// one of its linked job worktrees. A worktree's Git data lives in the
// Control checkout's .git directory, so the common directory's parent is the
// Control checkout.
func controlRoot(directory string) (string, error) {
	common, err := gitStdout(directory, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("current directory is not a Git worktree: %w", err)
	}
	if filepath.Base(common) != ".git" {
		return "", fmt.Errorf("cannot locate the Control checkout: Git directory %s is not a .git folder", common)
	}
	return filepath.Dir(common), nil
}

func journalPath(root, id string) string {
	return filepath.Join(root, filepath.FromSlash(journalDirectory), id+".jsonl")
}

// ensureJournalIgnored refuses to write a journal that Git would track, so
// runtime records cannot be committed by accident.
func ensureJournalIgnored(root, id string) error {
	relative := journalDirectory + "/" + id + ".jsonl"
	command := exec.Command("git", "check-ignore", "-q", "--no-index", relative)
	command.Dir = root
	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return fmt.Errorf("runtime journal %s is not ignored by Git; add .local/ to the project's .gitignore so runtime records are never committed", relative)
		}
		return fmt.Errorf("check runtime journal ignore rule: %w", err)
	}
	return nil
}

func newEventID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// appendJournalEvent writes one event as a single JSON line.
func appendJournalEvent(root string, event runtimeEvent) error {
	if err := ensureJournalIgnored(root, event.Job); err != nil {
		return err
	}
	path := journalPath(root, event.Job)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create runtime journal directory: %w", err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("cannot write runtime journal %s: the agent host may not allow writes to the Control checkout (see \"Agent host requirements\" in the user guide): %w", path, err)
		}
		return fmt.Errorf("open runtime journal: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append runtime journal: %w", err)
	}
	return file.Close()
}

// readJournal returns the job's journal events in file order. Malformed lines
// are skipped and counted rather than failing the read.
func readJournal(root, id string) ([]runtimeEvent, int, error) {
	file, err := os.Open(journalPath(root, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	var events []runtimeEvent
	malformed := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event runtimeEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil || event.Job != id || event.Type == "" {
			malformed++
			continue
		}
		events = append(events, event)
	}
	return events, malformed, scanner.Err()
}

var (
	secretTokenPattern = regexp.MustCompile(`(?i)(bearer\s+\S+|-----begin [a-z ]*private key-----|\b(ghp|gho|ghs|ghu|github_pat)_[A-Za-z0-9_]{20,}|\bAKIA[0-9A-Z]{16}\b|\bxox[abprs]-[A-Za-z0-9-]{10,}|\bsk-[A-Za-z0-9_-]{20,})`)
	secretAssignment   = regexp.MustCompile(`(?i)\b(password|passwd|token|secret|api[_-]?key|access[_-]?key|client[_-]?secret|private[_-]?key)\s*[=:]\s*\S+`)
	journalIDPattern   = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,128}$`)
)

// secretLikeText reports whether free text appears to contain a credential.
// Journal and notification text that matches is rejected, not redacted, so
// the record never holds the value.
func secretLikeText(value string) bool {
	return secretTokenPattern.MatchString(value) || secretAssignment.MatchString(value)
}

func validateJournalText(field, value string) error {
	if len(value) > maxJournalTextLength {
		return fmt.Errorf("--%s is longer than %d characters; record a summary, not raw output", field, maxJournalTextLength)
	}
	if secretLikeText(value) {
		return fmt.Errorf("--%s appears to contain a credential; progress records must not include secret values", field)
	}
	return nil
}

// jobProgress appends a progress record for a started job. It may be run
// from the Control checkout or from the job's worktree.
func jobProgress(root, id string, o jobOptions) error {
	packet, _, err := readJob(root, id)
	if err != nil {
		return err
	}
	if o.show {
		return showJobProgress(root, packet, o.format)
	}
	if o.format != "" {
		return errors.New("--format applies only with --show")
	}
	if err := checkJobOperation(id, "progress", packet.Status); err != nil {
		return err
	}
	if o.remaining == "" && o.next == "" && o.blocker == "" && len(o.gates) == 0 {
		return errors.New("record at least one of --remaining, --next, --blocker or --gate")
	}
	for field, value := range map[string]string{"remaining": o.remaining, "next": o.next, "blocker": o.blocker, "actor": o.actor} {
		if err := validateJournalText(field, value); err != nil {
			return err
		}
	}
	for _, gate := range o.gates {
		if !journalIDPattern.MatchString(gate) {
			return fmt.Errorf("--gate %q is not a valid gate identifier", gate)
		}
	}
	eventID, err := newEventID()
	if err != nil {
		return err
	}
	actor := o.actor
	if actor == "" {
		actor = lifecycleActor()
	}
	event := runtimeEvent{
		Schema:   journalSchemaVersion,
		EventID:  eventID,
		Job:      id,
		Type:     "progress",
		At:       time.Now().UTC().Format(time.RFC3339),
		Actor:    actor,
		Branch:   packet.Branch,
		Commit:   currentJobCommit(root, packet),
		Worktree: packet.Worktree,
		Progress: &progressData{Remaining: o.remaining, Next: o.next, Blocker: o.blocker, Gates: o.gates},
	}
	if err := appendJournalEvent(root, event); err != nil {
		return err
	}
	fmt.Printf("Recorded progress for %s at %s", id, event.At)
	if event.Commit != "" {
		fmt.Printf(" (commit %s)", shortCommit(event.Commit))
	}
	fmt.Println()
	return nil
}

// currentJobCommit returns the job worktree's HEAD, or its branch head when
// the worktree is unavailable.
func currentJobCommit(root string, packet jobPacket) string {
	if packet.Worktree != "" {
		if commit, err := gitStdout(packet.Worktree, nil, "rev-parse", "HEAD^{commit}"); err == nil {
			return commit
		}
	}
	if packet.Branch != "" {
		if commit, err := resolveCommit(root, "refs/heads/"+packet.Branch); err == nil {
			return commit
		}
	}
	return ""
}

func showJobProgress(root string, packet jobPacket, format string) error {
	events, malformed, err := readJournal(root, packet.ID)
	if err != nil {
		return err
	}
	progress := []runtimeEvent{}
	for _, event := range events {
		if event.Type == "progress" {
			progress = append(progress, event)
		}
	}
	if format == "json" {
		encoded, err := json.MarshalIndent(map[string]any{"job": packet.ID, "progress": progress, "malformed_lines": malformed}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	if format != "" && format != "markdown" {
		return errors.New("--format requires markdown or json")
	}
	if len(progress) == 0 {
		fmt.Printf("No progress recorded for %s\n", packet.ID)
	}
	for _, event := range progress {
		fmt.Printf("- %s by %s at %s\n", event.At, event.Actor, shortCommit(event.Commit))
		printProgressFields(event.Progress, "  ")
	}
	if malformed > 0 {
		fmt.Printf("Warning: %d malformed journal line(s) skipped\n", malformed)
	}
	return nil
}

func printProgressFields(progress *progressData, indent string) {
	if progress == nil {
		return
	}
	if progress.Remaining != "" {
		fmt.Printf("%sRemaining: %s\n", indent, progress.Remaining)
	}
	if progress.Next != "" {
		fmt.Printf("%sNext: %s\n", indent, progress.Next)
	}
	if progress.Blocker != "" {
		fmt.Printf("%sBlocker: %s\n", indent, progress.Blocker)
	}
	if len(progress.Gates) > 0 {
		fmt.Printf("%sGates run: %s\n", indent, strings.Join(progress.Gates, ", "))
	}
}

type jobStatusActivity struct {
	StartedAt       string        `json:"started_at,omitempty"`
	LastProgressAt  string        `json:"last_progress_at,omitempty"`
	LastProgressBy  string        `json:"last_progress_by,omitempty"`
	LastProgress    *progressData `json:"last_progress,omitempty"`
	ProgressRecords int           `json:"progress_records"`
	BranchHead      string        `json:"branch_head,omitempty"`
	LastCommitAt    string        `json:"last_commit_at,omitempty"`
	MalformedLines  int           `json:"malformed_journal_lines,omitempty"`
	Summary         string        `json:"summary"`
}

// buildJobActivity explains what is known about a started job's recent work
// from repository and journal records, so a job left running with no worker
// is diagnosable without the worker.
func buildJobActivity(root string, packet jobPacket) *jobStatusActivity {
	if !isStartedJobStatus(packet.Status) || isTerminalJobStatus(packet.Status) {
		return nil
	}
	activity := &jobStatusActivity{}
	if start, ok := lastTransitionTo(packet, stateRunning); ok {
		activity.StartedAt = start.RecordedAt
	}
	if packet.Branch != "" {
		if output, err := gitStdout(root, nil, "log", "-1", "--format=%H %ct", "refs/heads/"+packet.Branch); err == nil {
			if head, at, found := strings.Cut(output, " "); found {
				if seconds, parseErr := strconv.ParseInt(at, 10, 64); parseErr == nil {
					activity.BranchHead, activity.LastCommitAt = head, time.Unix(seconds, 0).UTC().Format(time.RFC3339)
				}
			}
		}
	}
	events, malformed, err := readJournal(root, packet.ID)
	activity.MalformedLines = malformed
	if err == nil {
		for _, event := range events {
			if event.Type != "progress" {
				continue
			}
			activity.ProgressRecords++
			activity.LastProgressAt, activity.LastProgressBy, activity.LastProgress = event.At, event.Actor, event.Progress
		}
	}
	switch {
	case activity.ProgressRecords == 0 && activity.StartedAt != "":
		activity.Summary = fmt.Sprintf("no progress recorded since start at %s", activity.StartedAt)
	case activity.ProgressRecords == 0:
		activity.Summary = "no progress recorded"
	default:
		activity.Summary = fmt.Sprintf("last progress at %s by %s (%d record(s))", activity.LastProgressAt, activity.LastProgressBy, activity.ProgressRecords)
	}
	if activity.LastCommitAt != "" {
		activity.Summary += fmt.Sprintf("; last branch commit %s at %s", shortCommit(activity.BranchHead), activity.LastCommitAt)
	}
	return activity
}
