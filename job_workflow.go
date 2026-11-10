package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type jobStatusReport struct {
	Job           string               `json:"job"`
	Status        string               `json:"status"`
	Role          string               `json:"role"`
	Objective     string               `json:"objective"`
	BaseBranch    string               `json:"base_branch,omitempty"`
	BaseCommit    string               `json:"base_commit,omitempty"`
	Branch        string               `json:"branch,omitempty"`
	ReviewCommit  string               `json:"review_commit,omitempty"`
	Worktree      string               `json:"worktree,omitempty"`
	AllowedPaths  []string             `json:"allowed_paths,omitempty"`
	ExcludedPaths []string             `json:"excluded_paths,omitempty"`
	Gates         []jobStatusGate      `json:"gates,omitempty"`
	Criteria      []jobStatusCriterion `json:"criteria,omitempty"`
	Artifacts     []jobStatusArtifact  `json:"durable_artifacts,omitempty"`
	Amendments    int                  `json:"scope_amendments"`
	Blockers      []string             `json:"blockers,omitempty"`
	NextAction    string               `json:"next_action"`
	UpdatedAt     string               `json:"updated_at,omitempty"`
}

type jobStatusGate struct {
	Gate     string `json:"gate"`
	Status   string `json:"status"`
	Verified bool   `json:"verified"`
	Commit   string `json:"commit,omitempty"`
	Checks   int    `json:"checks,omitempty"`
	Passed   int    `json:"passed,omitempty"`
	Failed   int    `json:"failed,omitempty"`
	Pending  int    `json:"pending,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

type jobStatusCriterion struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Evidence int    `json:"evidence_count"`
}

type jobStatusArtifact struct {
	ID       string `json:"id"`
	Required bool   `json:"required"`
	Retained bool   `json:"retained"`
	SHA256   string `json:"sha256,omitempty"`
}

func jobStatus(root, id, format string) error {
	packet, _, err := readJob(root, id)
	if err != nil {
		return err
	}
	report := buildJobStatus(root, packet)
	if format == "json" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Printf("Job %s: %s\nRole: %s\nObjective: %s\n", report.Job, report.Status, report.Role, report.Objective)
	if report.BaseBranch != "" {
		fmt.Printf("Base: %s %s\n", report.BaseBranch, report.BaseCommit)
	}
	if report.Branch != "" {
		fmt.Printf("Branch: %s\n", report.Branch)
	}
	if report.ReviewCommit != "" {
		fmt.Printf("Review commit: %s\n", report.ReviewCommit)
	}
	if len(report.Gates) > 0 {
		fmt.Println("Gates:")
		for _, gate := range report.Gates {
			fmt.Printf("- %s: %s", gate.Gate, gate.Status)
			if gate.Checks > 0 {
				fmt.Printf(" (%d passed, %d failed, %d pending)", gate.Passed, gate.Failed, gate.Pending)
			}
			fmt.Println()
		}
	}
	if len(report.Criteria) > 0 {
		fmt.Println("Acceptance criteria:")
		for _, criterion := range report.Criteria {
			fmt.Printf("- %s: %s (%d evidence)\n", criterion.ID, criterion.Status, criterion.Evidence)
		}
	}
	if len(report.Blockers) > 0 {
		fmt.Println("Blockers:")
		for _, blocker := range report.Blockers {
			fmt.Println("-", blocker)
		}
	}
	fmt.Println("Next action:", report.NextAction)
	return nil
}

func buildJobStatus(root string, packet jobPacket) jobStatusReport {
	report := jobStatusReport{Job: packet.ID, Status: packet.Status, Role: packet.Role, Objective: packet.Objective, BaseBranch: packet.BaseBranch, BaseCommit: packet.BaseCommit, Branch: packet.Branch, ReviewCommit: packet.ReviewCommit, Worktree: packet.Worktree, AllowedPaths: packet.AllowedPaths, ExcludedPaths: packet.ExcludedPaths, Amendments: len(packet.ScopeAmendments), UpdatedAt: packet.UpdatedAt}
	for _, gate := range packet.Gates {
		item := jobStatusGate{Gate: gate, Status: "missing"}
		for _, evidence := range packet.GateEvidence {
			if evidence.Gate != gate {
				continue
			}
			item.Status, item.Verified, item.Commit, item.Evidence = evidence.Status, evidence.Verified, evidence.Commit, evidence.Evidence
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(evidence.Evidence)))
			if err == nil {
				var document gateEvidenceDocument
				if yaml.Unmarshal(content, &document) == nil {
					item.Checks = len(document.Result.Checks)
					for _, check := range document.Result.Checks {
						switch check.Status {
						case "passed":
							item.Passed++
						case "failed":
							item.Failed++
						default:
							item.Pending++
						}
					}
				}
			}
		}
		report.Gates = append(report.Gates, item)
	}
	for _, criterion := range packet.AcceptanceCriteria {
		report.Criteria = append(report.Criteria, jobStatusCriterion{ID: criterion.ID, Status: criterion.Status, Evidence: len(criterion.Evidence)})
	}
	for _, artifact := range packet.DurableArtifacts {
		report.Artifacts = append(report.Artifacts, jobStatusArtifact{ID: artifact.ID, Required: artifact.Required, Retained: artifact.Retained, SHA256: artifact.SHA256})
	}
	if packet.Status != "ready" && packet.Status != "running" && packet.Status != "review" && packet.Status != "review-pending" && packet.Status != "review-failed" && packet.Status != "accepted" && packet.Status != "merged" {
		report.Blockers = append(report.Blockers, fmt.Sprintf("job status is %q", packet.Status))
	}
	for _, gate := range report.Gates {
		if !gate.Verified || gate.Status != "passed" {
			report.Blockers = append(report.Blockers, "gate "+gate.Gate+" is not verified as passing")
		}
	}
	for _, criterion := range report.Criteria {
		if criterion.Status != "passed" && criterion.Status != "waived" {
			report.Blockers = append(report.Blockers, "acceptance criterion "+criterion.ID+" is "+criterion.Status)
		}
	}
	for _, artifact := range report.Artifacts {
		if artifact.Required && !artifact.Retained {
			report.Blockers = append(report.Blockers, "required artifact "+artifact.ID+" is not retained")
		}
	}
	report.NextAction = nextJobAction(packet, report.Blockers)
	return report
}

func nextJobAction(packet jobPacket, blockers []string) string {
	switch packet.Status {
	case "draft":
		return "complete the packet and mark it ready"
	case "ready":
		return "start the approved job"
	case "running":
		return "complete the work and return a handoff"
	case "review", "review-pending", "review-failed", "changes-requested":
		return "resolve review findings or record required attestations"
	case "accepted":
		return "merge the accepted job"
	case "merged":
		return "no further lifecycle action"
	case "blocked":
		return "resolve the blocker and resume or reissue the job"
	case "cancelled":
		return "no further lifecycle action"
	default:
		return "inspect the job and resolve its blockers"
	}
}

func jobHandoffDraft(root, id string) error {
	packet, _, err := readJob(root, id)
	if err != nil {
		return err
	}
	destination := root
	if packet.Worktree != "" {
		if _, statErr := os.Stat(packet.Worktree); statErr == nil {
			destination = packet.Worktree
		}
	}
	path := filepath.Join(destination, "control", "handoffs", id+".md")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("handoff already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	base := packet.BaseCommit
	if base == "" {
		base = "HEAD"
	}
	target := packet.ReviewCommit
	if target == "" {
		target = "HEAD"
	}
	changed := []string{}
	if output, diffErr := gitOutput(destination, "diff", "--name-only", base+"..."+target); diffErr == nil {
		changed = nonEmptyLines(output)
	}
	sort.Strings(changed)
	var b strings.Builder
	fmt.Fprintf(&b, "# Handoff: %s\n\n", id)
	fmt.Fprintf(&b, "## Job\n\n- Status: %s\n- Role: %s\n- Objective: %s\n", packet.Status, packet.Role, packet.Objective)
	fmt.Fprintf(&b, "- Base commit: `%s`\n- Review commit: `%s`\n\n", packet.BaseCommit, packet.ReviewCommit)
	b.WriteString("## Changed paths\n\n")
	if len(changed) == 0 {
		b.WriteString("- <!-- Leadfox found no committed path changes yet -->\n")
	} else {
		for _, path := range changed {
			fmt.Fprintf(&b, "- `%s`\n", path)
		}
	}
	b.WriteString("\n## Gate results\n\n")
	if len(packet.GateEvidence) == 0 {
		b.WriteString("- No gate evidence recorded yet.\n")
	} else {
		for _, evidence := range packet.GateEvidence {
			fmt.Fprintf(&b, "- `%s`: %s; evidence `%s`\n", evidence.Gate, evidence.Status, evidence.Evidence)
		}
	}
	b.WriteString("\n## Decisions and deviations\n\n- <!-- Link decisions, approvals, and accepted deviations. -->\n\n## Why\n\n<!-- Agent: explain what was done and why. -->\n\n## Checks and evidence\n\n<!-- Agent: list commands, results, and evidence references. -->\n\n## Assumptions and unresolved items\n\n<!-- Agent: list assumptions, open questions, and stop conditions. -->\n\n## Recommended next step\n\n<!-- Agent: recommend the next bounded action. -->\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Println("Created handoff draft:", path)
	return nil
}

func jobScopeAdd(root, id string, o jobOptions) error {
	packet, body, err := readJob(root, id)
	if err != nil {
		return err
	}
	if packet.Status == "merged" || packet.Status == "cancelled" {
		return fmt.Errorf("job %s is terminal and cannot be amended", id)
	}
	if strings.TrimSpace(o.reason) == "" || strings.TrimSpace(o.actor) == "" {
		return errors.New("--reason and --actor are required for a scope amendment")
	}
	if len(o.allow) == 0 && len(o.exclude) == 0 && len(o.gates) == 0 {
		return errors.New("scope amendment must add at least one path or gate")
	}
	for _, path := range append(append([]string{}, o.allow...), o.exclude...) {
		if filepath.IsAbs(path) || strings.Contains(filepath.Clean(path), "..") {
			return fmt.Errorf("scope path must be project-relative: %s", path)
		}
	}
	newAllow, newExclude, newGates := appendNew(packet.AllowedPaths, o.allow), appendNew(packet.ExcludedPaths, o.exclude), appendNew(packet.Gates, o.gates)
	if len(newAllow) == 0 && len(newExclude) == 0 && len(newGates) == 0 {
		return errors.New("scope amendment does not add anything new")
	}
	packet.AllowedPaths = append(packet.AllowedPaths, newAllow...)
	packet.ExcludedPaths = append(packet.ExcludedPaths, newExclude...)
	packet.Gates = append(packet.Gates, newGates...)
	now := time.Now().UTC().Format(time.RFC3339)
	packet.ScopeAmendments = append(packet.ScopeAmendments, jobScopeAmendment{AddedAllowed: newAllow, AddedExcluded: newExclude, AddedGates: newGates, Reason: o.reason, Actor: o.actor, RecordedAt: now})
	if packet.ReviewCommit != "" {
		packet.GateEvidence = nil
		packet.Attestations = nil
		packet.ReviewCommit = ""
		packet.Status = "changes-requested"
		appendTransition(&packet, "changes-requested", "scope amended: "+o.reason, o.actor, now)
	}
	packet.UpdatedAt = now
	if err := ensureNoStagedChanges(root); err != nil {
		return err
	}
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if err := commitLifecycleRecord(root, []string{jobPath(root, id)}, "Record scope amendment "+id); err != nil {
		return err
	}
	fmt.Printf("Amended scope for %s; review evidence must be regenerated\n", id)
	return nil
}

func appendNew(existing, additions []string) []string {
	var result []string
	for _, item := range additions {
		if !containsString(existing, item) && !containsString(result, item) {
			result = append(result, item)
		}
	}
	return result
}

func ensureNoStagedChanges(root string) error {
	output, err := gitOutput(root, "diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) != "" {
		return errors.New("Control worktree has staged changes; unstage them before a Leadfox lifecycle amendment")
	}
	return nil
}
