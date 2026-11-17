package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// jobNotification is a safe, structured notice about an exceptional or
// terminal job event. It carries identifiers, states, a commit ID and fixed
// recovery text only; free-text reasons are included only when they pass the
// credential check, and worker output is never included.
type jobNotification struct {
	Project           string `json:"project"`
	Job               string `json:"job"`
	Event             string `json:"event"`
	State             string `json:"state"`
	Commit            string `json:"commit,omitempty"`
	Reason            string `json:"reason,omitempty"`
	RecoveryAction    string `json:"recovery_action"`
	AttentionRequired bool   `json:"attention_required"`
	// Key identifies the condition being reported so that repeated job
	// checks do not repeat the same notice.
	Key string `json:"key,omitempty"`
}

// notifier delivers notifications. Only the local implementation ships;
// host adapters (for example Herdr) can implement the same interface later.
type notifier interface {
	Notify(root string, notification jobNotification) error
}

type localNotifier struct{}

// Notify writes the notice to standard error and, when the project's
// runtime journal is usable, records it there. A journal that cannot be
// written (for example in a project whose .gitignore lacks .local/) does not
// block the lifecycle operation that raised the notice.
func (localNotifier) Notify(root string, notification jobNotification) error {
	attention := "no owner action needed"
	if notification.AttentionRequired {
		attention = "owner attention required"
	}
	line := fmt.Sprintf("leadfox notice: [%s] %s %s — state %s", notification.Project, notification.Job, notification.Event, notification.State)
	if notification.Commit != "" {
		line += ", commit " + shortCommit(notification.Commit)
	}
	if notification.Reason != "" {
		line += ", reason: " + notification.Reason
	}
	line += "; " + notification.RecoveryAction + " (" + attention + ")"
	fmt.Fprintln(os.Stderr, line)
	eventID, err := newEventID()
	if err != nil {
		return err
	}
	event := runtimeEvent{Schema: journalSchemaVersion, EventID: eventID, Job: notification.Job, Type: "notification", At: nowFunc().Format(time.RFC3339), Commit: notification.Commit, Notification: &notification}
	if err := appendJournalEvent(root, event); err != nil {
		fmt.Fprintf(os.Stderr, "leadfox notice: not recorded in the runtime journal: %v\n", err)
	}
	return nil
}

var activeNotifier notifier = localNotifier{}

// safeReason returns reason text suitable for a notification.
func safeReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	if secretLikeText(reason) {
		return "[withheld: matched a credential pattern]"
	}
	if len(reason) > 200 {
		reason = reason[:200] + "…"
	}
	return strings.ReplaceAll(reason, "\n", " ")
}

func projectDisplayName(root string) string {
	if content, err := os.ReadFile(filepath.Join(root, "control", "project.yaml")); err == nil {
		var metadata projectMetadata
		if yaml.Unmarshal(content, &metadata) == nil && strings.TrimSpace(metadata.Project.Name) != "" {
			return metadata.Project.Name
		}
	}
	return filepath.Base(root)
}

// recoveryActionFor gives fixed, state-specific recovery guidance.
func recoveryActionFor(packet jobPacket, event string) string {
	switch event {
	case "start":
		return "monitor progress with leadfox job status " + packet.ID
	case "stale-progress":
		return "check on the worker; leadfox job check will mark it worker-lost if silence continues"
	case "worker-lost", "timed-out":
		return fmt.Sprintf("inspect branch %s and progress, then leadfox job resume %s or leadfox job reissue %s NEW-ID --from-branch", packet.Branch, packet.ID, packet.ID)
	case "handoff":
		return "review the handoff with leadfox job review " + packet.ID
	case "blocked":
		return "resolve the blocker, then leadfox job resume " + packet.ID
	case "paused":
		return "leadfox job resume " + packet.ID + " when work should continue"
	case "cancelled":
		return "no action needed unless the work should continue"
	case "recovery-ready":
		if packet.Status == statePaused {
			return fmt.Sprintf("worktree %s restored; continue with leadfox job resume %s", packet.Worktree, packet.ID)
		}
		if point, ok := latestRecoveryPoint(packet); ok {
			return fmt.Sprintf("work preserved at %s; continue with leadfox job reissue %s NEW-ID --from-recovery latest", point.Ref, packet.ID)
		}
		return fmt.Sprintf("continue with leadfox job reissue %s NEW-ID --from-branch", packet.ID)
	}
	return "inspect the job with leadfox job status " + packet.ID
}

func notifyJob(root string, packet jobPacket, event, reason, commit, key string, attention bool) {
	if commit == "" {
		commit = currentJobCommit(root, packet)
	}
	notification := jobNotification{
		Project:           projectDisplayName(root),
		Job:               packet.ID,
		Event:             event,
		State:             packet.Status,
		Commit:            commit,
		Reason:            safeReason(reason),
		RecoveryAction:    recoveryActionFor(packet, event),
		AttentionRequired: attention,
		Key:               key,
	}
	if err := activeNotifier.Notify(root, notification); err != nil {
		fmt.Fprintf(os.Stderr, "leadfox notice delivery failed: %v\n", err)
	}
}

// notifiedKeys returns the keys of notifications already recorded for a job.
func notifiedKeys(events []runtimeEvent) map[string]bool {
	keys := map[string]bool{}
	for _, event := range events {
		if event.Type == "notification" && event.Notification != nil && event.Notification.Key != "" {
			keys[event.Notification.Key] = true
		}
	}
	return keys
}
