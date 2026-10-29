package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const jobHelpText = `Manage a bounded Leadfox agent job.

Usage:
  leadfox job new <id> [options]
  leadfox job start <id> [options]
  leadfox job scope <id> [options]
  leadfox job review <id> [options]
  leadfox job approve <id> --verdict accepted|changes-requested
  leadfox job merge <id>

Options:
  -b, --base BRANCH       Base branch (default: main)
  -w, --worktree DIR      Agent or review worktree path
      --branch BRANCH     Job branch name
      --commit SHA        Commit to review
      --role ROLE         Job role for job new
      --objective TEXT    Job objective for job new
      --allow GLOB        Allowed path (repeatable)
      --exclude GLOB      Excluded path (repeatable)
      --gate ID           Required gate (repeatable)
      --verdict VALUE     accepted or changes-requested
  -h, --help              Show this help message

Review creates a clean detached worktree, resolves commits to full IDs,
checks scope, runs required gates, and records evidence before approval.
`

type jobPacket struct {
	ID             string            `yaml:"id"`
	Status         string            `yaml:"status"`
	Role           string            `yaml:"role"`
	Objective      string            `yaml:"objective"`
	BaseBranch     string            `yaml:"base_branch,omitempty"`
	BaseCommit     string            `yaml:"base_commit,omitempty"`
	Branch         string            `yaml:"branch,omitempty"`
	Worktree       string            `yaml:"worktree,omitempty"`
	ReviewCommit   string            `yaml:"review_commit,omitempty"`
	ReviewWorktree string            `yaml:"review_worktree,omitempty"`
	AllowedPaths   []string          `yaml:"allowed_paths,omitempty"`
	ExcludedPaths  []string          `yaml:"excluded_paths,omitempty"`
	Gates          []string          `yaml:"gates,omitempty"`
	CreatedAt      string            `yaml:"created_at,omitempty"`
	UpdatedAt      string            `yaml:"updated_at,omitempty"`
	GateEvidence   []jobGateEvidence `yaml:"gate_evidence,omitempty"`
	ReviewRounds   []jobReviewRound  `yaml:"review_rounds,omitempty"`
	ApprovedBy     string            `yaml:"approved_by,omitempty"`
	ApprovedAt     string            `yaml:"approved_at,omitempty"`
	MergeCommit    string            `yaml:"merge_commit,omitempty"`
}

type jobGateEvidence struct {
	Gate       string `yaml:"gate"`
	Status     string `yaml:"status"`
	Verified   bool   `yaml:"verified"`
	Commit     string `yaml:"commit"`
	Evidence   string `yaml:"evidence"`
	RecordedAt string `yaml:"recorded_at"`
}

type jobReviewRound struct {
	Number     int               `yaml:"number"`
	Commit     string            `yaml:"commit"`
	Status     string            `yaml:"status"`
	RecordedAt string            `yaml:"recorded_at"`
	Evidence   []jobGateEvidence `yaml:"evidence,omitempty"`
}

type gateEvidenceDocument struct {
	Job        string     `yaml:"job"`
	Round      int        `yaml:"round"`
	Commit     string     `yaml:"commit"`
	RecordedAt string     `yaml:"recorded_at"`
	Result     gateResult `yaml:"result"`
}

type jobOptions struct {
	base, worktree, branch, commit, role, objective, verdict string
	allow, exclude, gates                                    []string
	showHelp                                                 bool
}

func runJobCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(jobHelpText)
		return nil
	}
	operation := args[0]
	options, id, err := parseJobOptions(args[1:])
	if err != nil {
		return err
	}
	if options.showHelp {
		fmt.Print(jobHelpText)
		return nil
	}
	if id == "" {
		return errors.New("a job ID is required; use 'leadfox job --help' for usage")
	}
	root, err := gitRoot(".")
	if err != nil {
		return err
	}
	switch operation {
	case "new":
		return jobNew(root, id, options)
	case "start":
		return jobStart(root, id, options)
	case "scope":
		return jobScope(root, id, options)
	case "review":
		return jobReview(root, id, options)
	case "approve":
		return jobApprove(root, id, options)
	case "merge":
		return jobMerge(root, id)
	default:
		return fmt.Errorf("unknown job operation %q; use 'leadfox job --help' for usage", operation)
	}
}

func parseJobOptions(args []string) (jobOptions, string, error) {
	o := jobOptions{base: "main"}
	id := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			o.showHelp = true
		case "-b", "--base":
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.base = v
			i = n
		case "-w", "--worktree":
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.worktree = v
			i = n
		case "--branch":
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.branch = v
			i = n
		case "--commit":
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.commit = v
			i = n
		case "--role":
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.role = v
			i = n
		case "--objective":
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.objective = v
			i = n
		case "--verdict":
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.verdict = v
			i = n
		case "--allow":
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.allow = append(o.allow, v)
			i = n
		case "--exclude":
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.exclude = append(o.exclude, v)
			i = n
		case "--gate":
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.gates = append(o.gates, v)
			i = n
		default:
			if strings.HasPrefix(arg, "-") {
				return o, id, fmt.Errorf("unknown job option %q", arg)
			}
			if id != "" {
				return o, id, errors.New("only one job ID may be provided")
			}
			id = arg
		}
	}
	return o, id, nil
}

func jobPath(root, id string) string { return filepath.Join(root, "control", "jobs", id+".md") }

func readJob(root, id string) (jobPacket, string, error) {
	content, err := os.ReadFile(jobPath(root, id))
	if err != nil {
		return jobPacket{}, "", fmt.Errorf("read job packet: %w", err)
	}
	front, body, err := splitFrontMatter(string(content))
	if err != nil {
		return jobPacket{}, "", err
	}
	var packet jobPacket
	if err := yaml.Unmarshal([]byte(front), &packet); err != nil {
		return jobPacket{}, "", fmt.Errorf("parse job metadata: %w", err)
	}
	if packet.ID == "" {
		packet.ID = id
	}
	return packet, body, nil
}

func writeJob(root string, packet jobPacket, body string) error {
	front, err := yaml.Marshal(packet)
	if err != nil {
		return fmt.Errorf("encode job metadata: %w", err)
	}
	content := "---\n" + string(front) + "---\n" + strings.TrimLeft(body, "\n")
	if err := os.WriteFile(jobPath(root, packet.ID), []byte(content), 0o644); err != nil {
		return fmt.Errorf("write job packet: %w", err)
	}
	return nil
}

func splitFrontMatter(content string) (string, string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return "", "", errors.New("job packet must begin with YAML front matter")
	}
	rest := strings.TrimPrefix(content, "---\n")
	marker := strings.Index(rest, "\n---\n")
	if marker < 0 {
		return "", "", errors.New("job packet front matter is not closed")
	}
	return rest[:marker], rest[marker+len("\n---\n"):], nil
}

func jobNew(root, id string, o jobOptions) error {
	if !regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(id) {
		return fmt.Errorf("invalid job ID %q", id)
	}
	path := jobPath(root, id)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("job packet already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	if o.role == "" {
		o.role = "service"
	}
	if o.objective == "" {
		o.objective = "Describe the bounded work for this job."
	}
	now := time.Now().UTC().Format(time.RFC3339)
	packet := jobPacket{ID: id, Status: "draft", Role: o.role, Objective: o.objective, BaseBranch: o.base, AllowedPaths: o.allow, ExcludedPaths: o.exclude, Gates: o.gates, CreatedAt: now, UpdatedAt: now}
	body := "# " + id + "\n\n## Objective\n\n" + o.objective + "\n\n## Agent instructions\n\nRead the approved project documentation before starting. Do not modify files outside the allowed paths. Return a handoff listing changed paths, checks run, results, assumptions, unresolved items, and the recommended next step.\n\n## Acceptance\n\nSet the packet status to ready after Control has completed the job design.\n"
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	fmt.Printf("Created draft job packet: %s\n", path)
	return nil
}

func jobStart(root, id string, o jobOptions) error {
	packet, body, err := readJob(root, id)
	if err != nil {
		return err
	}
	if packet.Status != "ready" {
		return fmt.Errorf("job %s is %q; set it to ready after Control approval", id, packet.Status)
	}
	clean, err := gitClean(root)
	if err != nil {
		return err
	}
	if !clean {
		return errors.New("Control worktree is not clean")
	}
	base := packet.BaseBranch
	if base == "" {
		base = o.base
	}
	branch := packet.Branch
	if branch == "" {
		branch = o.branch
	}
	if branch == "" {
		branch = "job/" + strings.ToLower(id)
	}
	worktree := packet.Worktree
	if worktree == "" {
		worktree = o.worktree
	}
	if worktree == "" {
		return errors.New("a worktree path is required")
	}
	worktree, err = filepath.Abs(worktree)
	if err != nil {
		return err
	}
	if err := ensureExternalWorktree(root, worktree); err != nil {
		return err
	}
	if _, err := os.Stat(worktree); err == nil {
		return fmt.Errorf("job worktree already exists: %s", worktree)
	} else if !os.IsNotExist(err) {
		return err
	}
	baseCommit, err := gitOutput(root, "rev-parse", base)
	if err != nil {
		return fmt.Errorf("resolve base: %w", err)
	}
	if _, err := gitOutput(root, "worktree", "add", "-b", branch, worktree, base); err != nil {
		return fmt.Errorf("create job worktree: %w", err)
	}
	packet.Status, packet.BaseBranch, packet.BaseCommit, packet.Branch, packet.Worktree, packet.UpdatedAt = "running", base, strings.TrimSpace(baseCommit), branch, worktree, time.Now().UTC().Format(time.RFC3339)
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if err := commitLifecycleRecord(root, []string{jobPath(root, id)}, "Record start "+id); err != nil {
		return err
	}
	fmt.Printf("Started %s on %s at %s\n", id, branch, worktree)
	return nil
}

func jobScope(root, id string, o jobOptions) error {
	packet, _, err := readJob(root, id)
	if err != nil {
		return err
	}
	if packet.BaseCommit == "" {
		return errors.New("job has no recorded base commit; start the job first")
	}
	worktree := o.worktree
	if worktree == "" {
		worktree = packet.Worktree
	}
	if worktree == "" {
		return errors.New("a job worktree is required")
	}
	return checkJobScope(root, packet, worktree, "HEAD")
}

func checkJobScope(root string, packet jobPacket, worktree, commit string) error {
	baseCommit := packet.BaseCommit
	if baseCommit == "" {
		baseCommit = packet.BaseBranch
	}
	changed, err := gitOutput(worktree, "diff", "--name-only", baseCommit+"..."+commit)
	if err != nil {
		return fmt.Errorf("inspect job scope: %w", err)
	}
	paths := nonEmptyLines(changed)
	var violations []string
	for _, path := range paths {
		if matchesAny(path, packet.ExcludedPaths) || !matchesAny(path, packet.AllowedPaths) {
			violations = append(violations, path)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		fmt.Println("Scope: FAILED")
		for _, path := range violations {
			fmt.Println("  violation", path)
		}
		return errors.New("job changed paths outside its allowlist")
	}
	fmt.Printf("Scope: PASSED (%d changed paths)\n", len(paths))
	for _, path := range paths {
		fmt.Println(" ", path)
	}
	return nil
}

func runJobGates(root string, packet jobPacket, worktree, commit string) ([]jobGateEvidence, error) {
	if len(packet.Gates) == 0 {
		return nil, nil
	}
	round := len(packet.ReviewRounds) + 1
	evidenceDir := filepath.Join(root, "control", "evidence")
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		return nil, fmt.Errorf("create evidence directory: %w", err)
	}
	results := make([]jobGateEvidence, 0, len(packet.Gates))
	for _, gateName := range packet.Gates {
		result, err := executeGate(worktree, gateOptions{gateName: gateName, worktree: worktree, timeout: 10 * time.Minute})
		if err != nil {
			return nil, fmt.Errorf("execute required gate %q: %w", gateName, err)
		}
		verified := gateResultVerified(result, gateName, root, packet, worktree, commit)
		path := filepath.Join(evidenceDir, fmt.Sprintf("%s-round-%d-%s.yaml", packet.ID, round, sanitizeFilename(gateName)))
		document := gateEvidenceDocument{Job: packet.ID, Round: round, Commit: commit, RecordedAt: time.Now().UTC().Format(time.RFC3339), Result: result}
		encoded, err := yaml.Marshal(document)
		if err != nil {
			return nil, fmt.Errorf("encode gate evidence: %w", err)
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			return nil, fmt.Errorf("write gate evidence: %w", err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		results = append(results, jobGateEvidence{Gate: gateName, Status: result.Status, Verified: verified, Commit: commit, Evidence: filepath.ToSlash(relative), RecordedAt: document.RecordedAt})
		if !verified {
			return nil, fmt.Errorf("required gate %q did not verify: %s", gateName, gateResultReason(result))
		}
	}
	return results, nil
}

func gateResultVerified(result gateResult, gateName, root string, packet jobPacket, worktree, commit string) bool {
	if result.Status == "failed" || result.Status == "deferred" {
		return false
	}
	for _, item := range result.Checks {
		if item.Status == "failed" || item.Status == "deferred" {
			return false
		}
		if item.Status == "manual" {
			// A job's scope is independently checked immediately before gates.
			// That check is the attestation for the conventional manual scope check.
			if item.ID != "scope" || checkJobScope(root, packet, worktree, commit) != nil {
				return false
			}
		}
	}
	return result.Status == "passed" || result.Status == "manual"
}

func gateResultReason(result gateResult) string {
	if result.Status == "failed" {
		for _, item := range result.Checks {
			if item.Status == "failed" {
				return item.ID + " failed"
			}
		}
	}
	return "manual or deferred checks require explicit attestation"
}

func resolveCommit(root, value string) (string, error) {
	commit, err := gitOutput(root, "rev-parse", "--verify", value+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(commit), nil
}

func commitLifecycleRecord(root string, paths []string, message string) error {
	paths = uniqueStrings(paths)
	args := append([]string{"add", "--"}, paths...)
	if _, err := gitOutput(root, args...); err != nil {
		return fmt.Errorf("stage lifecycle record: %w", err)
	}
	status, err := gitOutput(root, append([]string{"status", "--porcelain", "--"}, paths...)...)
	if err != nil {
		return fmt.Errorf("inspect lifecycle record: %w", err)
	}
	if strings.TrimSpace(status) == "" {
		return nil
	}
	if _, err := gitOutput(root, "commit", "-m", message, "-m", "Recorded by Leadfox lifecycle management."); err != nil {
		return fmt.Errorf("commit lifecycle record: %w", err)
	}
	return nil
}

func lifecycleActor() string {
	if actor := strings.TrimSpace(os.Getenv("LEADFOX_ACTOR")); actor != "" {
		return actor
	}
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}
	return "unknown"
}

func sanitizeFilename(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		seen := false
		for _, existing := range result {
			if existing == value {
				seen = true
				break
			}
		}
		if !seen {
			result = append(result, value)
		}
	}
	return result
}

func jobReview(root, id string, o jobOptions) error {
	packet, body, err := readJob(root, id)
	if err != nil {
		return err
	}
	commit := o.commit
	if commit == "" {
		commit = packet.ReviewCommit
	}
	if commit == "" {
		return errors.New("a commit is required for review")
	}
	commit, err = resolveCommit(root, commit)
	if err != nil {
		return fmt.Errorf("resolve review commit: %w", err)
	}
	worktree := o.worktree
	if worktree == "" {
		worktree = packet.ReviewWorktree
	}
	if worktree == "" {
		return errors.New("a review worktree path is required")
	}
	worktree, err = filepath.Abs(worktree)
	if err != nil {
		return err
	}
	if err := ensureExternalWorktree(root, worktree); err != nil {
		return err
	}
	if _, err := os.Stat(worktree); err == nil {
		if packet.ReviewWorktree == worktree && (packet.Status == "review" || packet.Status == "changes-requested") {
			if _, removeErr := gitOutput(root, "worktree", "remove", "--force", worktree); removeErr != nil {
				return fmt.Errorf("remove previous review worktree: %w", removeErr)
			}
		} else {
			return fmt.Errorf("review worktree already exists: %s", worktree)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := gitOutput(root, "worktree", "add", "--detach", worktree, commit); err != nil {
		return fmt.Errorf("create review worktree: %w", err)
	}
	if err := checkJobScope(root, packet, worktree, commit); err != nil {
		_, _ = gitOutput(root, "worktree", "remove", "--force", worktree)
		return err
	}
	evidence, err := runJobGates(root, packet, worktree, commit)
	if err != nil {
		_, _ = gitOutput(root, "worktree", "remove", "--force", worktree)
		return err
	}
	round := jobReviewRound{Number: len(packet.ReviewRounds) + 1, Commit: commit, Status: "review", RecordedAt: time.Now().UTC().Format(time.RFC3339), Evidence: evidence}
	packet.Status, packet.ReviewCommit, packet.ReviewWorktree, packet.UpdatedAt = "review", commit, worktree, time.Now().UTC().Format(time.RFC3339)
	packet.GateEvidence = evidence
	packet.ReviewRounds = append(packet.ReviewRounds, round)
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	paths := []string{jobPath(root, id)}
	for _, item := range evidence {
		paths = append(paths, item.Evidence)
	}
	if err := commitLifecycleRecord(root, paths, fmt.Sprintf("Record review %s round %d", id, round.Number)); err != nil {
		return err
	}
	fmt.Printf("Created review worktree for %s at %s\n", id, worktree)
	return nil
}

func jobApprove(root, id string, o jobOptions) error {
	packet, body, err := readJob(root, id)
	if err != nil {
		return err
	}
	if packet.Status != "review" {
		return fmt.Errorf("job %s is %q; create a review worktree first", id, packet.Status)
	}
	if o.verdict != "accepted" && o.verdict != "changes-requested" {
		return errors.New("--verdict must be accepted or changes-requested")
	}
	if len(packet.Gates) > 0 {
		if len(packet.GateEvidence) != len(packet.Gates) {
			return errors.New("required gate evidence is missing; review the job before approval")
		}
		for _, evidence := range packet.GateEvidence {
			if !evidence.Verified {
				return fmt.Errorf("required gate %q is not verified", evidence.Gate)
			}
		}
	}
	dir := filepath.Join(root, "control", "approvals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, id+".md")
	now := time.Now().UTC().Format(time.RFC3339)
	actor := lifecycleActor()
	var record strings.Builder
	if existing, readErr := os.ReadFile(path); readErr == nil {
		record.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			record.WriteString("\n")
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	} else {
		fmt.Fprintf(&record, "# Approval: %s\n", id)
	}
	fmt.Fprintf(&record, "\n## Round %d\n\n- Verdict: %s\n- Approved by: %s\n- Reviewed commit: `%s`\n- Review worktree: `%s`\n- Recorded: %s\n", len(packet.ReviewRounds), o.verdict, actor, packet.ReviewCommit, packet.ReviewWorktree, now)
	if len(packet.GateEvidence) == 0 {
		record.WriteString("- Required gates: none declared\n")
	} else {
		record.WriteString("- Required gates:\n")
		for _, evidence := range packet.GateEvidence {
			fmt.Fprintf(&record, "  - `%s`: %s; evidence: `%s`\n", evidence.Gate, evidence.Status, evidence.Evidence)
		}
	}
	record.WriteString("\nControl review evidence is recorded in the referenced gate evidence files and the review handoff.\n")
	if err := os.WriteFile(path, []byte(record.String()), 0o644); err != nil {
		return err
	}
	packet.Status, packet.ApprovedBy, packet.ApprovedAt, packet.UpdatedAt = o.verdict, actor, now, now
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if err := commitLifecycleRecord(root, []string{jobPath(root, id), path}, fmt.Sprintf("Record approval %s round %d", id, len(packet.ReviewRounds))); err != nil {
		return err
	}
	fmt.Printf("Recorded %s verdict for %s\n", o.verdict, id)
	return nil
}

func jobMerge(root, id string) error {
	packet, body, err := readJob(root, id)
	if err != nil {
		return err
	}
	if packet.Status != "accepted" {
		return fmt.Errorf("job %s is %q; only accepted jobs may merge", id, packet.Status)
	}
	if packet.ReviewCommit == "" || packet.Branch == "" {
		return errors.New("job is missing reviewed commit or source branch")
	}
	if err := gitCleanForLifecycle(root, id); err != nil {
		return err
	}
	branchCommit, err := gitOutput(root, "rev-parse", packet.Branch+"^{commit}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(branchCommit) != packet.ReviewCommit {
		return errors.New("job branch no longer matches the reviewed commit")
	}
	if _, err := gitOutput(root, "merge", "--no-ff", packet.Branch, "-m", "Merge "+id); err != nil {
		return fmt.Errorf("merge job branch: %w", err)
	}
	mergeCommit, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve merge commit: %w", err)
	}
	packet.Status, packet.MergeCommit, packet.UpdatedAt = "merged", strings.TrimSpace(mergeCommit), time.Now().UTC().Format(time.RFC3339)
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if err := commitLifecycleRecord(root, []string{jobPath(root, id)}, "Record merge "+id); err != nil {
		return err
	}
	for _, path := range uniqueStrings([]string{packet.Worktree, packet.ReviewWorktree}) {
		if path != "" {
			if _, err := gitOutput(root, "worktree", "remove", "--force", path); err != nil {
				return fmt.Errorf("merge succeeded but cleanup failed: %w", err)
			}
		}
	}
	fmt.Printf("Merged and cleaned up %s\n", id)
	return nil
}

func gitRoot(path string) (string, error) {
	output, err := gitOutput(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("current directory is not a Git worktree: %w", err)
	}
	return strings.TrimSpace(output), nil
}

func ensureExternalWorktree(root, worktree string) error {
	relative, err := filepath.Rel(root, worktree)
	if err != nil {
		return fmt.Errorf("compare worktree path: %w", err)
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return fmt.Errorf("worktree must be outside the Control checkout: %s", worktree)
	}
	return nil
}
func gitClean(root string) (bool, error) {
	output, err := gitOutput(root, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) == "", nil
}

func gitCleanForLifecycle(root, id string) error {
	output, err := gitOutput(root, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect Git status: %w", err)
	}
	allowed := map[string]bool{
		filepath.ToSlash(filepath.Join("control", "jobs", id+".md")):      true,
		filepath.ToSlash(filepath.Join("control", "approvals", id+".md")): true,
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) < 4 {
			return errors.New("unexpected Git status output")
		}
		path := filepath.ToSlash(strings.TrimSpace(line[3:]))
		if !allowed[path] {
			return fmt.Errorf("Control worktree has unrelated changes: %s", path)
		}
	}
	return nil
}
func gitOutput(directory string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
func nonEmptyLines(value string) []string {
	var result []string
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, strings.TrimSpace(line))
		}
	}
	return result
}
func matchesAny(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if globMatch(path, strings.TrimPrefix(filepath.ToSlash(pattern), "./")) {
			return true
		}
	}
	return false
}
func globMatch(path, pattern string) bool {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
			b.WriteByte('\\')
			b.WriteByte(pattern[i])
		default:
			b.WriteByte(pattern[i])
		}
	}
	b.WriteString("$")
	matched, err := regexp.MatchString(b.String(), filepath.ToSlash(path))
	return err == nil && matched
}
