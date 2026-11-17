package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// jobPrunedBranch records a job branch deleted by job prune. The head commit
// stays reachable from PreservedBy (the base branch or a recovery ref), so
// the work can always be found again.
type jobPrunedBranch struct {
	Branch      string `yaml:"branch"`
	Head        string `yaml:"head"`
	PreservedBy string `yaml:"preserved_by"`
	Actor       string `yaml:"actor"`
	PrunedAt    string `yaml:"pruned_at"`
}

type pruneCandidate struct {
	packet      jobPacket
	body        string
	head        string
	preservedBy string // base branch or existing recovery ref containing head
	needsRef    bool   // a recovery ref must be created before deletion
	skip        string
}

// parseAge accepts Go durations and whole days such as 30d.
func parseAge(value string) (time.Duration, error) {
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil || days < 0 {
			return 0, fmt.Errorf("--older-than %q is not a valid age (for example 30d or 12h)", value)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	age, err := time.ParseDuration(value)
	if err != nil || age < 0 {
		return 0, fmt.Errorf("--older-than %q is not a valid age (for example 30d or 12h)", value)
	}
	return age, nil
}

// checkedOutBranches lists branches checked out in any worktree.
func checkedOutBranches(root string) (map[string]bool, error) {
	output, err := gitStdout(root, nil, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	branches := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "branch refs/heads/") {
			branches[strings.TrimPrefix(line, "branch refs/heads/")] = true
		}
	}
	return branches, nil
}

func isAncestor(root, commit, ref string) bool {
	_, err := gitStdout(root, nil, "merge-base", "--is-ancestor", commit, ref)
	return err == nil
}

// pruneTime is when the job became merged or cancelled.
func pruneTime(packet jobPacket) (time.Time, bool) {
	for _, value := range []string{packet.TerminalAt, packet.UpdatedAt} {
		if at, err := time.Parse(time.RFC3339, value); err == nil {
			return at, true
		}
	}
	return time.Time{}, false
}

func assessPruneCandidate(root string, packet jobPacket, checkedOut map[string]bool) pruneCandidate {
	candidate := pruneCandidate{packet: packet}
	head, err := resolveCommit(root, "refs/heads/"+packet.Branch)
	if err != nil {
		candidate.skip = "branch already deleted"
		return candidate
	}
	candidate.head = head
	if checkedOut[packet.Branch] {
		candidate.skip = "branch is checked out in a worktree"
		return candidate
	}
	base := packet.BaseBranch
	if base == "" {
		base = "main"
	}
	if _, err := resolveCommit(root, "refs/heads/"+base); err == nil && isAncestor(root, head, "refs/heads/"+base) {
		candidate.preservedBy = base
		return candidate
	}
	for index := len(packet.RecoveryPoints) - 1; index >= 0; index-- {
		ref := packet.RecoveryPoints[index].Ref
		if _, err := resolveCommit(root, ref); err == nil && isAncestor(root, head, ref) {
			candidate.preservedBy = ref
			return candidate
		}
	}
	candidate.needsRef = true
	// Use the next number with no existing ref, so a ref the record does
	// not list is never overwritten.
	for number := len(packet.RecoveryPoints) + 1; ; number++ {
		ref := recoveryRef(packet.ID, number)
		if _, err := resolveCommit(root, ref); err != nil {
			candidate.preservedBy = ref
			break
		}
	}
	return candidate
}

// jobPrune deletes branches of merged and cancelled jobs whose work is
// reachable elsewhere. Without --yes it only reports what it would do.
func jobPrune(root string, o jobOptions) error {
	includeMerged, includeCancelled := o.pruneMerged, o.pruneCancelled
	if !includeMerged && !includeCancelled {
		includeMerged, includeCancelled = true, true
	}
	var minimumAge time.Duration
	if o.olderThan != "" {
		age, err := parseAge(o.olderThan)
		if err != nil {
			return err
		}
		minimumAge = age
	}
	actor := o.actor
	if actor == "" {
		actor = lifecycleActor()
	}
	entries, err := os.ReadDir(filepath.Join(root, "control", "jobs"))
	if err != nil {
		return fmt.Errorf("read job directory: %w", err)
	}
	checkedOut, err := checkedOutBranches(root)
	if err != nil {
		return err
	}
	now := nowFunc()
	var candidates []pruneCandidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || entry.Name() == "README.md" {
			continue
		}
		packet, body, readErr := readJob(root, strings.TrimSuffix(entry.Name(), ".md"))
		if readErr != nil || packet.Branch == "" || packet.PrunedBranch != nil {
			continue
		}
		if !(packet.Status == stateMerged && includeMerged) && !(packet.Status == stateCancelled && includeCancelled) {
			continue
		}
		if minimumAge > 0 {
			if at, ok := pruneTime(packet); !ok || now.Sub(at) < minimumAge {
				continue
			}
		}
		candidate := assessPruneCandidate(root, packet, checkedOut)
		candidate.body = body
		if candidate.skip == "branch already deleted" {
			continue
		}
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].packet.ID < candidates[j].packet.ID })
	if len(candidates) == 0 {
		fmt.Println("No job branches to prune.")
		return nil
	}

	fmt.Println("JOB\tSTATUS\tBRANCH\tHEAD\tRESULT")
	if !o.yes {
		prunable := 0
		for _, candidate := range candidates {
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", candidate.packet.ID, candidate.packet.Status, candidate.packet.Branch, shortCommit(candidate.head), pruneDescription(candidate, false))
			if candidate.skip == "" {
				prunable++
			}
		}
		fmt.Printf("Dry run: %d branch(es) would be deleted. Repeat with --yes to delete them.\n", prunable)
		return nil
	}

	staged, err := gitStdout(root, nil, "diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	if staged != "" {
		return errors.New("the Control checkout has staged changes; commit or unstage them before job prune records deletions")
	}
	recordedAt := now.UTC().Format(time.RFC3339)
	var changed []string
	failures := 0
	for _, candidate := range candidates {
		packet := candidate.packet
		if candidate.skip != "" {
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", packet.ID, packet.Status, packet.Branch, shortCommit(candidate.head), pruneDescription(candidate, true))
			continue
		}
		if candidate.needsRef {
			// The empty old value makes update-ref refuse to overwrite.
			if _, err := gitOutput(root, "update-ref", "-m", "leadfox prune "+packet.ID, candidate.preservedBy, candidate.head, ""); err != nil {
				failures++
				fmt.Printf("%s\t%s\t%s\t%s\terror: preserve head: %v\n", packet.ID, packet.Status, packet.Branch, shortCommit(candidate.head), err)
				continue
			}
			packet.RecoveryPoints = append(packet.RecoveryPoints, jobRecoveryPoint{
				Ref: candidate.preservedBy, Commit: candidate.head, Branch: packet.Branch, BranchHead: candidate.head,
				Reason: "preserved before job prune deleted the branch", Actor: actor, RecordedAt: recordedAt,
			})
		}
		// The expected old value makes the deletion fail if the branch moved.
		if _, err := gitOutput(root, "update-ref", "-m", "leadfox prune "+packet.ID, "-d", "refs/heads/"+packet.Branch, candidate.head); err != nil {
			failures++
			fmt.Printf("%s\t%s\t%s\t%s\terror: delete branch: %v\n", packet.ID, packet.Status, packet.Branch, shortCommit(candidate.head), err)
			if candidate.needsRef {
				// Keep the record of the recovery ref that now exists.
				if writeErr := writeJob(root, packet, candidate.body); writeErr == nil {
					changed = append(changed, jobPath(root, packet.ID))
				}
			}
			continue
		}
		packet.PrunedBranch = &jobPrunedBranch{Branch: packet.Branch, Head: candidate.head, PreservedBy: candidate.preservedBy, Actor: actor, PrunedAt: recordedAt}
		if err := writeJob(root, packet, candidate.body); err != nil {
			failures++
			fmt.Printf("%s\t%s\t%s\t%s\terror: branch deleted but record not written: %v\n", packet.ID, packet.Status, packet.Branch, shortCommit(candidate.head), err)
			continue
		}
		changed = append(changed, jobPath(root, packet.ID))
		candidate.packet = packet
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", packet.ID, packet.Status, packet.Branch, shortCommit(candidate.head), pruneDescription(candidate, true))
	}
	if len(changed) > 0 {
		if err := commitLifecycleRecord(root, changed, fmt.Sprintf("Record prune of %d job branch(es)", len(changed))); err != nil {
			return err
		}
	}
	if failures > 0 {
		return fmt.Errorf("job prune could not prune %d branch(es)", failures)
	}
	return nil
}

func pruneDescription(candidate pruneCandidate, applied bool) string {
	switch {
	case candidate.skip != "":
		return "kept: " + candidate.skip
	case candidate.needsRef && applied:
		return "deleted; head preserved at new recovery ref " + candidate.preservedBy
	case candidate.needsRef:
		return "would delete; head would be preserved at new recovery ref " + candidate.preservedBy
	case applied:
		return "deleted; head contained in " + candidate.preservedBy
	}
	return "would delete; head contained in " + candidate.preservedBy
}

// prunedBranchHint explains how to continue a job whose branch was pruned.
func prunedBranchHint(packet jobPacket) string {
	pruned := packet.PrunedBranch
	if strings.HasPrefix(pruned.PreservedBy, recoveryRefPrefix) {
		return fmt.Sprintf("branch %s was pruned on %s; its head %s is preserved at %s (continue with --from-recovery %s)", pruned.Branch, pruned.PrunedAt, shortCommit(pruned.Head), pruned.PreservedBy, filepath.Base(pruned.PreservedBy))
	}
	return fmt.Sprintf("branch %s was pruned on %s; its head %s is contained in %s", pruned.Branch, pruned.PrunedAt, shortCommit(pruned.Head), pruned.PreservedBy)
}

const jobPruneHelpText = `Delete branches of merged and cancelled jobs whose work is kept elsewhere.

Usage:
  leadfox job prune [--merged] [--cancelled] [--older-than AGE] [--actor NAME] [--yes]

Without --yes, job prune only lists what it would do. A branch is deleted
only when its head commit is contained in the job's base branch or in one of
the job's recovery refs; otherwise Leadfox first preserves the head as a new
recovery ref (refs/leadfox/recovery/<job>/<n>). Branches checked out in a
worktree are kept. Each job record keeps the branch name, head commit and
what preserves it, and the change is committed as one lifecycle record.

Options:
      --merged          Only merged jobs (default: merged and cancelled)
      --cancelled       Only cancelled jobs, including superseded ones
      --older-than AGE  Only jobs closed at least AGE ago (for example 30d, 12h)
      --actor NAME      Identity recorded with the deletion
      --yes             Delete the listed branches
`
