package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// recoveryRefPrefix is the namespace for preserved job work. Leadfox creates
// refs here and never deletes them; removing one is a manual owner decision.
const recoveryRefPrefix = "refs/leadfox/recovery/"

// jobRecoveryPoint records where a job's work can be recovered from after a
// worktree is removed or lost.
type jobRecoveryPoint struct {
	Ref                  string `yaml:"ref"`
	Commit               string `yaml:"commit"`
	Branch               string `yaml:"branch"`
	BranchHead           string `yaml:"branch_head"`
	Worktree             string `yaml:"worktree,omitempty"`
	Snapshot             bool   `yaml:"snapshot,omitempty"`
	DiscardedUncommitted bool   `yaml:"discarded_uncommitted,omitempty"`
	WorktreeMissing      bool   `yaml:"worktree_missing,omitempty"`
	Reason               string `yaml:"reason"`
	Actor                string `yaml:"actor"`
	RecordedAt           string `yaml:"recorded_at"`
}

// jobStartPoint records that a reissued job starts from preserved work
// rather than from its base branch.
type jobStartPoint struct {
	SourceJob string `yaml:"source_job"`
	Kind      string `yaml:"kind"`
	Ref       string `yaml:"ref"`
	Commit    string `yaml:"commit"`
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func recoveryRef(id string, number int) string {
	return fmt.Sprintf("%s%s/%d", recoveryRefPrefix, id, number)
}

// preserveJobWork records a recovery point for the job's branch before its
// worktree is removed. When the worktree has uncommitted tracked or untracked
// changes, they are captured in a snapshot commit whose parent is the branch
// head; the worktree, its index and the branch are not modified. Ignored files
// are not captured. The returned point has not yet been written to the packet.
func preserveJobWork(root string, packet jobPacket, reason, actor, now string, discard bool) (*jobRecoveryPoint, error) {
	if packet.Branch == "" {
		return nil, nil
	}
	branchHead, err := resolveCommit(root, "refs/heads/"+packet.Branch)
	if err != nil {
		return nil, fmt.Errorf("job branch %s is unavailable; refusing to remove its worktree: %w", packet.Branch, err)
	}
	point := &jobRecoveryPoint{
		Ref:        recoveryRef(packet.ID, len(packet.RecoveryPoints)+1),
		Commit:     branchHead,
		Branch:     packet.Branch,
		BranchHead: branchHead,
		Worktree:   packet.Worktree,
		Reason:     reason,
		Actor:      actor,
		RecordedAt: now,
	}
	if packet.Worktree == "" {
		point.WorktreeMissing = true
	} else if _, statErr := os.Stat(packet.Worktree); errors.Is(statErr, os.ErrNotExist) {
		point.WorktreeMissing = true
	} else if statErr != nil {
		return nil, statErr
	} else {
		clean, cleanErr := gitClean(packet.Worktree)
		if cleanErr != nil {
			return nil, fmt.Errorf("inspect job worktree before cleanup: %w", cleanErr)
		}
		if !clean {
			if discard {
				point.DiscardedUncommitted = true
			} else {
				snapshot, snapErr := snapshotWorktree(packet.Worktree, packet.ID)
				if snapErr != nil {
					return nil, fmt.Errorf("snapshot uncommitted work in %s (use --discard-uncommitted to cancel without it): %w", packet.Worktree, snapErr)
				}
				point.Commit, point.Snapshot = snapshot, true
			}
		}
	}
	if existing, err := gitOutput(root, "rev-parse", "--verify", "--quiet", point.Ref); err == nil && strings.TrimSpace(existing) != "" {
		return nil, fmt.Errorf("recovery ref %s already exists; refusing to overwrite it", point.Ref)
	}
	// The empty old value makes update-ref fail rather than overwrite a ref
	// created concurrently.
	if _, err := gitOutput(root, "update-ref", "-m", "leadfox recovery "+packet.ID, point.Ref, point.Commit, ""); err != nil {
		return nil, fmt.Errorf("record recovery ref: %w", err)
	}
	return point, nil
}

// snapshotWorktree builds a commit containing the worktree's current tracked
// and untracked (non-ignored) files, using a temporary index so that the
// worktree's own index and files are left untouched.
func snapshotWorktree(worktree, id string) (string, error) {
	head, err := gitStdout(worktree, nil, "rev-parse", "HEAD^{commit}")
	if err != nil {
		return "", err
	}
	indexFile, err := os.CreateTemp("", "leadfox-recovery-index-*")
	if err != nil {
		return "", err
	}
	indexPath := indexFile.Name()
	_ = indexFile.Close()
	_ = os.Remove(indexPath)
	defer os.Remove(indexPath)
	env := []string{"GIT_INDEX_FILE=" + indexPath}
	if _, err := gitStdout(worktree, env, "read-tree", head); err != nil {
		return "", err
	}
	if _, err := gitStdout(worktree, env, "add", "-A", "--", "."); err != nil {
		return "", err
	}
	tree, err := gitStdout(worktree, env, "write-tree")
	if err != nil {
		return "", err
	}
	identity := snapshotIdentity(worktree)
	message := fmt.Sprintf("Leadfox recovery snapshot %s\n\nUncommitted work preserved before worktree removal.", id)
	commit, err := gitStdout(worktree, identity, "commit-tree", tree, "-p", head, "-m", message)
	if err != nil {
		return "", err
	}
	return commit, nil
}

// snapshotIdentity supplies a committer identity only when the repository
// has none, so snapshots never fail for want of Git configuration.
func snapshotIdentity(directory string) []string {
	var env []string
	if name, err := gitStdout(directory, nil, "config", "user.name"); err != nil || name == "" {
		env = append(env, "GIT_AUTHOR_NAME=Leadfox", "GIT_COMMITTER_NAME=Leadfox")
	}
	if email, err := gitStdout(directory, nil, "config", "user.email"); err != nil || email == "" {
		env = append(env, "GIT_AUTHOR_EMAIL=leadfox@localhost", "GIT_COMMITTER_EMAIL=leadfox@localhost")
	}
	return env
}

// gitStdout runs git with extra environment values and returns trimmed
// standard output only, so warnings on standard error cannot corrupt
// object IDs.
func gitStdout(directory string, env []string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// latestRecoveryPoint returns the most recent recovery point, if any.
func latestRecoveryPoint(packet jobPacket) (jobRecoveryPoint, bool) {
	if len(packet.RecoveryPoints) == 0 {
		return jobRecoveryPoint{}, false
	}
	return packet.RecoveryPoints[len(packet.RecoveryPoints)-1], true
}

// resolveReissueStartPoint decides where a reissued job starts. Without
// --from-branch or --from-recovery the new job starts from its base branch,
// as before.
func resolveReissueStartPoint(root string, old jobPacket, o jobOptions) (*jobStartPoint, error) {
	if o.fromBranch && o.fromRecovery != "" {
		return nil, errors.New("use either --from-branch or --from-recovery, not both")
	}
	switch {
	case o.fromBranch:
		if old.Branch == "" {
			return nil, fmt.Errorf("job %s has no branch to continue from", old.ID)
		}
		ref := "refs/heads/" + old.Branch
		commit, err := resolveCommit(root, ref)
		if err != nil && old.PrunedBranch != nil {
			return nil, fmt.Errorf("job %s: %s", old.ID, prunedBranchHint(old))
		}
		if err != nil {
			return nil, fmt.Errorf("job %s branch %s is unavailable: %w", old.ID, old.Branch, err)
		}
		return &jobStartPoint{SourceJob: old.ID, Kind: "branch", Ref: ref, Commit: commit}, nil
	case o.fromRecovery != "":
		var point jobRecoveryPoint
		found := false
		if o.fromRecovery == "latest" {
			point, found = latestRecoveryPoint(old)
		} else {
			for _, candidate := range old.RecoveryPoints {
				if candidate.Ref == o.fromRecovery || filepath.Base(candidate.Ref) == o.fromRecovery {
					point, found = candidate, true
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("job %s has no recovery point %q", old.ID, o.fromRecovery)
		}
		commit, err := resolveCommit(root, point.Ref)
		if err != nil {
			return nil, fmt.Errorf("recovery ref %s is unavailable: %w", point.Ref, err)
		}
		if commit != point.Commit {
			return nil, fmt.Errorf("recovery ref %s no longer matches its recorded commit", point.Ref)
		}
		return &jobStartPoint{SourceJob: old.ID, Kind: "recovery", Ref: point.Ref, Commit: commit}, nil
	}
	return nil, nil
}

// recoveryDescription summarizes the preserved work of a cancelled job for
// status output. Legacy cancellations without recovery records are described
// from the branch, when it still exists.
func recoveryDescription(root string, packet jobPacket) string {
	if point, ok := latestRecoveryPoint(packet); ok {
		detail := "branch head"
		switch {
		case point.Snapshot:
			detail = "branch head plus uncommitted snapshot"
		case point.DiscardedUncommitted:
			detail = "branch head; uncommitted changes were discarded"
		case point.WorktreeMissing:
			detail = "branch head; worktree was already missing"
		}
		return fmt.Sprintf("work preserved at %s (%s, %s)", point.Ref, shortCommit(point.Commit), detail)
	}
	if packet.PrunedBranch != nil {
		return prunedBranchHint(packet)
	}
	if packet.Branch != "" {
		if commit, err := resolveCommit(root, "refs/heads/"+packet.Branch); err == nil {
			return fmt.Sprintf("cancelled before recovery records existed; branch %s (%s) may contain partial work", packet.Branch, shortCommit(commit))
		}
	}
	return ""
}

// jobRecover restores an interrupted or cancelled job to a resumable state
// without reconstructing its metadata by hand. It verifies the branch,
// prunes stale worktree entries, recreates a missing worktree from the
// branch (restoring the latest uncommitted snapshot when it applies),
// records a recovery point and leaves the job paused. It never approves,
// merges, pushes, or deletes anything.
func jobRecover(root, id string, o jobOptions) error {
	packet, body, err := readJob(root, id)
	if err != nil {
		return err
	}
	if err := checkJobOperation(id, "recover", packet.Status); err != nil {
		return err
	}
	if packet.SupersededBy != "" {
		return fmt.Errorf("job %s is superseded by %s; recover that job instead", id, packet.SupersededBy)
	}
	if strings.TrimSpace(o.actor) == "" {
		return errors.New("--actor is required for a lifecycle transition")
	}
	if strings.TrimSpace(o.reason) == "" {
		return errors.New("--reason is required")
	}
	if packet.Branch == "" {
		return fmt.Errorf("job %s never started and has no branch; set it ready and start it instead", id)
	}
	branchHead, err := resolveCommit(root, "refs/heads/"+packet.Branch)
	if err != nil && packet.PrunedBranch != nil {
		return fmt.Errorf("job %s cannot be recovered in place: %s", id, prunedBranchHint(packet))
	}
	if err != nil {
		return fmt.Errorf("job %s branch %s is unavailable; recovery needs the branch: %w", id, packet.Branch, err)
	}
	worktreePresent := false
	if packet.Worktree != "" {
		if _, statErr := os.Stat(packet.Worktree); statErr == nil {
			worktreePresent = true
		}
	}
	if worktreePresent && o.worktree != "" {
		return fmt.Errorf("job %s worktree %s still exists; --worktree applies only when it must be recreated", id, packet.Worktree)
	}
	if worktreePresent && (packet.Status == statePaused || packet.Status == stateBlocked) {
		return fmt.Errorf("job %s is %s and its worktree is present; nothing to recover, use leadfox job resume", id, packet.Status)
	}
	if !mustBeClean(root, "Control worktree") {
		return errors.New("Control worktree is not clean")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	restoredSnapshot := ""
	if !worktreePresent {
		if _, err := gitOutput(root, "worktree", "prune"); err != nil {
			return fmt.Errorf("prune stale worktree entries: %w", err)
		}
		settings, err := loadWorktreeSettings(root)
		if err != nil {
			return err
		}
		previous := packet.Worktree
		if previous == "" {
			if point, ok := latestRecoveryPoint(packet); ok {
				previous = point.Worktree
			}
		}
		worktree, err := resolveJobWorktreePath(root, id, "", firstNonEmpty(o.worktree, previous), settings)
		if err != nil {
			return err
		}
		if err := ensureExternalWorktree(root, worktree); err != nil {
			return err
		}
		override, err := checkJobWorktreeLocation(worktree, settings, o.allowVolatile, o.reason, o.actor, now)
		if err != nil {
			return err
		}
		if _, statErr := os.Stat(worktree); statErr == nil {
			return fmt.Errorf("recovery worktree path already exists: %s; choose another with --worktree", worktree)
		}
		if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil {
			return fmt.Errorf("create worktree parent: %w", err)
		}
		if _, err := gitOutput(root, "worktree", "add", worktree, packet.Branch); err != nil {
			return fmt.Errorf("recreate job worktree from %s: %w", packet.Branch, err)
		}
		if err := applyWorktreeEnvironment(root, worktree); err != nil {
			_, _ = gitOutput(root, "worktree", "remove", "--force", worktree)
			return err
		}
		if point, ok := latestRecoveryPoint(packet); ok && point.Snapshot && point.BranchHead == branchHead {
			// Restore the uncommitted work captured at cancellation as
			// unstaged changes, leaving the branch itself unchanged.
			if _, err := gitOutput(worktree, "checkout", point.Commit, "--", "."); err != nil {
				return fmt.Errorf("restore recovery snapshot %s: %w", point.Ref, err)
			}
			if _, err := gitOutput(worktree, "reset", "-q"); err != nil {
				return fmt.Errorf("unstage restored snapshot: %w", err)
			}
			restoredSnapshot = point.Ref
		}
		packet.Worktree = worktree
		packet.WorktreeOverride = override
	}
	point, err := preserveJobWork(root, packet, "recovered: "+o.reason, o.actor, now, false)
	if err != nil {
		return err
	}
	if point != nil {
		packet.RecoveryPoints = append(packet.RecoveryPoints, *point)
	}
	appendTransition(&packet, statePaused, "recovered: "+o.reason, o.actor, now)
	packet.Status, packet.UpdatedAt = statePaused, now
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if err := commitLifecycleRecord(root, []string{jobPath(root, id)}, "Record recover "+id); err != nil {
		return err
	}
	fmt.Printf("Recovered %s: branch %s at %s, worktree %s; status paused\n", id, packet.Branch, shortCommit(branchHead), packet.Worktree)
	if restoredSnapshot != "" {
		fmt.Printf("Restored uncommitted work from %s as unstaged changes\n", restoredSnapshot)
	}
	var remaining []string
	for _, criterion := range packet.AcceptanceCriteria {
		if criterion.Status != "passed" && criterion.Status != "waived" {
			remaining = append(remaining, criterion.ID+" ("+criterion.Status+")")
		}
	}
	if len(remaining) > 0 {
		fmt.Println("Remaining acceptance criteria:", strings.Join(remaining, ", "))
	}
	if len(packet.Gates) > 0 {
		fmt.Println("Required gates:", strings.Join(packet.Gates, ", "))
	}
	fmt.Printf("Next: register a new execution if a worker continues, then leadfox job resume %s --actor NAME\n", id)
	notifyJob(root, packet, "recovery-ready", o.reason, branchHead, "", false)
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
