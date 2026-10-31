package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
  leadfox job attest <id> --check GATE/CHECK --result passed [--note TEXT]
  leadfox job cancel <id> --reason TEXT
  leadfox job block <id> --reason TEXT
  leadfox job resume <id> [--reason TEXT]
  leadfox job list
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
      --setup COMMAND     Review setup command (repeatable)
      --verdict VALUE     accepted or changes-requested
      --approved-by NAME  Explicit approver identity
      --actor NAME       Identity performing a lifecycle transition
      --reason TEXT       Reason for cancellation, blocking, or re-review
      --note TEXT         Attestation or approval note
  -h, --help              Show this help message

Review creates a clean detached worktree, resolves commits to full IDs,
checks scope, runs required gates, and records evidence before approval.
`

const evidenceSchemaVersion = 2

type jobPacket struct {
	ID                 string              `yaml:"id"`
	Status             string              `yaml:"status"`
	Role               string              `yaml:"role"`
	Objective          string              `yaml:"objective"`
	BaseBranch         string              `yaml:"base_branch,omitempty"`
	BaseCommit         string              `yaml:"base_commit,omitempty"`
	Branch             string              `yaml:"branch,omitempty"`
	BranchStartCommit  string              `yaml:"branch_start_commit,omitempty"`
	Worktree           string              `yaml:"worktree,omitempty"`
	ReviewCommit       string              `yaml:"review_commit,omitempty"`
	ReviewWorktree     string              `yaml:"review_worktree,omitempty"`
	ReviewWorktrees    []string            `yaml:"review_worktrees,omitempty"`
	AllowedPaths       []string            `yaml:"allowed_paths,omitempty"`
	ExcludedPaths      []string            `yaml:"excluded_paths,omitempty"`
	Gates              []string            `yaml:"gates,omitempty"`
	CreatedAt          string              `yaml:"created_at,omitempty"`
	UpdatedAt          string              `yaml:"updated_at,omitempty"`
	GateEvidence       []jobGateEvidence   `yaml:"gate_evidence,omitempty"`
	ReviewRounds       []jobReviewRound    `yaml:"review_rounds,omitempty"`
	ReviewSetup        []string            `yaml:"review_setup,omitempty"`
	Attestations       []jobAttestation    `yaml:"attestations,omitempty"`
	AttestationHistory []jobAttestation    `yaml:"attestation_history,omitempty"`
	Transitions        []jobTransition     `yaml:"transitions,omitempty"`
	RemovedWorktrees   []jobWorktreeRecord `yaml:"removed_worktrees,omitempty"`
	ApprovedBy         string              `yaml:"approved_by,omitempty"`
	ApprovedAt         string              `yaml:"approved_at,omitempty"`
	MergeCommit        string              `yaml:"merge_commit,omitempty"`
	TerminalReason     string              `yaml:"terminal_reason,omitempty"`
	TerminalBy         string              `yaml:"terminal_by,omitempty"`
	TerminalAt         string              `yaml:"terminal_at,omitempty"`
	ResumeReason       string              `yaml:"resume_reason,omitempty"`
	ResumeBy           string              `yaml:"resume_by,omitempty"`
	ResumeAt           string              `yaml:"resume_at,omitempty"`
}

type jobTransition struct {
	From       string `yaml:"from"`
	To         string `yaml:"to"`
	Reason     string `yaml:"reason,omitempty"`
	Actor      string `yaml:"actor"`
	RecordedAt string `yaml:"recorded_at"`
}

type jobGateEvidence struct {
	Gate          string `yaml:"gate"`
	Status        string `yaml:"status"`
	Verified      bool   `yaml:"verified"`
	Commit        string `yaml:"commit"`
	Evidence      string `yaml:"evidence"`
	Hash          string `yaml:"hash"`
	RecordedAt    string `yaml:"recorded_at"`
	SetupEvidence string `yaml:"setup_evidence,omitempty"`
	SetupHash     string `yaml:"setup_hash,omitempty"`
}

type jobAttestation struct {
	Gate       string `yaml:"gate"`
	Check      string `yaml:"check"`
	Result     string `yaml:"result"`
	Note       string `yaml:"note,omitempty"`
	Actor      string `yaml:"actor"`
	RecordedAt string `yaml:"recorded_at"`
	ReplacedAt string `yaml:"replaced_at,omitempty"`
	ReplacedBy string `yaml:"replaced_by,omitempty"`
}

type evidenceState struct {
	Pending bool
	Failed  bool
}

type jobWorktreeRecord struct {
	Path      string `yaml:"path"`
	RemovedAt string `yaml:"removed_at"`
}

type jobReviewRound struct {
	Number     int               `yaml:"number"`
	Commit     string            `yaml:"commit"`
	Status     string            `yaml:"status"`
	Reason     string            `yaml:"reason,omitempty"`
	RecordedAt string            `yaml:"recorded_at"`
	Evidence   []jobGateEvidence `yaml:"evidence,omitempty"`
	ApprovedBy string            `yaml:"approved_by,omitempty"`
	ApprovedAt string            `yaml:"approved_at,omitempty"`
}

type gateEvidenceDocument struct {
	SchemaVersion int              `yaml:"schema_version"`
	Job           string           `yaml:"job"`
	Round         int              `yaml:"round"`
	Commit        string           `yaml:"commit"`
	RecordedAt    string           `yaml:"recorded_at"`
	Leadfox       string           `yaml:"leadfox"`
	SetupEvidence string           `yaml:"setup_evidence,omitempty"`
	Attestations  []jobAttestation `yaml:"attestations,omitempty"`
	Result        gateResult       `yaml:"result"`
}

type setupResult struct {
	Command        string    `yaml:"command"`
	Status         string    `yaml:"status"`
	StartedAt      time.Time `yaml:"started_at"`
	FinishedAt     time.Time `yaml:"finished_at"`
	DurationMS     int64     `yaml:"duration_ms"`
	ExitCode       int       `yaml:"exit_code"`
	Output         string    `yaml:"output,omitempty"`
	OutputText     string    `yaml:"output_text,omitempty"`
	OutputLines    []string  `yaml:"output_lines,omitempty"`
	OutputEncoding string    `yaml:"output_encoding,omitempty"`
	Reason         string    `yaml:"reason,omitempty"`
}

type setupEvidenceDocument struct {
	SchemaVersion int           `yaml:"schema_version"`
	Job           string        `yaml:"job"`
	Round         int           `yaml:"round"`
	Commit        string        `yaml:"commit"`
	RecordedAt    string        `yaml:"recorded_at"`
	Leadfox       string        `yaml:"leadfox"`
	Setup         []setupResult `yaml:"setup"`
}

type jobOptions struct {
	base, worktree, branch, commit, role, objective, verdict string
	approvedBy, actor, reason, note, result, check           string
	allow, exclude, gates, setup                             []string
	showHelp                                                 bool
	used                                                     map[string]bool
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
	if err := validateJobOptions(operation, options); err != nil {
		return err
	}
	root, err := gitRoot(".")
	if err != nil {
		return err
	}
	if id == "" {
		if operation == "list" {
			return jobList(root)
		}
		return errors.New("a job ID is required; use 'leadfox job --help' for usage")
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
	case "attest":
		return jobAttest(root, id, options)
	case "cancel":
		return jobTerminate(root, id, "cancelled", options)
	case "block":
		return jobTerminate(root, id, "blocked", options)
	case "resume":
		return jobResume(root, id, options)
	case "list":
		return jobList(root)
	case "merge":
		return jobMerge(root, id)
	default:
		return fmt.Errorf("unknown job operation %q; use 'leadfox job --help' for usage", operation)
	}
}

func jobList(root string) error {
	directory := filepath.Join(root, "control", "jobs")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read job directory: %w", err)
	}
	fmt.Println("JOB\tSTATUS\tBRANCH\tUPDATED")
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || entry.Name() == "README.md" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".md")
		packet, _, readErr := readJob(root, id)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", entry.Name(), readErr)
			continue
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", packet.ID, packet.Status, packet.Branch, packet.UpdatedAt)
	}
	return nil
}

func parseJobOptions(args []string) (jobOptions, string, error) {
	o := jobOptions{base: "main", used: make(map[string]bool)}
	id := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			o.showHelp = true
		case "-b", "--base":
			o.used["base"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.base = v
			i = n
		case "-w", "--worktree":
			o.used["worktree"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.worktree = v
			i = n
		case "--branch":
			o.used["branch"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.branch = v
			i = n
		case "--commit":
			o.used["commit"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.commit = v
			i = n
		case "--role":
			o.used["role"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.role = v
			i = n
		case "--objective":
			o.used["objective"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.objective = v
			i = n
		case "--verdict":
			o.used["verdict"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.verdict = v
			i = n
		case "--approved-by":
			o.used["approved-by"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.approvedBy, i = v, n
		case "--actor":
			o.used["actor"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.actor, i = v, n
		case "--reason":
			o.used["reason"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.reason, i = v, n
		case "--note":
			o.used["note"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.note, i = v, n
		case "--result":
			o.used["result"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.result, i = v, n
		case "--check":
			o.used["check"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.check, i = v, n
		case "--allow":
			o.used["allow"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.allow = append(o.allow, v)
			i = n
		case "--exclude":
			o.used["exclude"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.exclude = append(o.exclude, v)
			i = n
		case "--gate":
			o.used["gate"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.gates = append(o.gates, v)
			i = n
		case "--setup":
			o.used["setup"] = true
			v, n, e := requiredOptionValue(args, i, arg)
			if e != nil {
				return o, id, e
			}
			o.setup = append(o.setup, v)
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

func validateJobOptions(operation string, o jobOptions) error {
	allowed := map[string]map[string]bool{
		"new":     {"base": true, "worktree": true, "branch": true, "role": true, "objective": true, "allow": true, "exclude": true, "gate": true, "setup": true},
		"start":   {"base": true, "worktree": true, "branch": true},
		"scope":   {"worktree": true, "commit": true},
		"review":  {"worktree": true, "commit": true, "reason": true},
		"approve": {"verdict": true, "approved-by": true, "note": true},
		"attest":  {"check": true, "result": true, "approved-by": true, "note": true},
		"cancel":  {"reason": true, "actor": true, "approved-by": true},
		"block":   {"reason": true, "actor": true, "approved-by": true},
		"resume":  {"reason": true, "actor": true},
		"list":    {},
		"merge":   {},
	}
	operationAllowed, found := allowed[operation]
	if !found {
		return fmt.Errorf("unknown job operation %q; use 'leadfox job --help' for usage", operation)
	}
	for option := range o.used {
		if !operationAllowed[option] {
			return fmt.Errorf("job %s does not accept --%s", operation, option)
		}
	}
	return nil
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
	packet := jobPacket{ID: id, Status: "draft", Role: o.role, Objective: o.objective, BaseBranch: o.base, AllowedPaths: o.allow, ExcludedPaths: o.exclude, Gates: o.gates, ReviewSetup: o.setup, CreatedAt: now, UpdatedAt: now}
	body := "# " + id + "\n\n## Objective\n\n" + o.objective + "\n\n## Agent instructions\n\nRead the approved project documentation before starting. Do not modify files outside the allowed paths. Return a handoff listing changed paths, checks run, results, assumptions, unresolved items, and the recommended next step.\n\n## Git provenance\n\n`base_commit` and `branch_start_commit` identify the commit from which the job branch was created. The shared lifecycle start record is committed immediately afterward and is reachable from the job branch; its SHA is intentionally not stored inside that same commit because a commit cannot contain its own ID.\n\n## Acceptance\n\nSet the packet status to ready after Control has completed the job design.\n"
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
	baseCommit = strings.TrimSpace(baseCommit)
	if _, err := gitOutput(root, "worktree", "add", "-b", branch, worktree, baseCommit); err != nil {
		return fmt.Errorf("create job worktree: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	appendTransition(&packet, "running", "job started", "Control", now)
	packet.Status, packet.BaseBranch, packet.BaseCommit, packet.Branch, packet.BranchStartCommit, packet.Worktree, packet.UpdatedAt = "running", base, baseCommit, branch, baseCommit, worktree, now
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if err := commitLifecycleRecord(root, []string{jobPath(root, id)}, "Record start "+id); err != nil {
		return err
	}
	startCommit, err := resolveCommit(root, "HEAD")
	if err != nil {
		return fmt.Errorf("resolve start record: %w", err)
	}
	// The branch was created at BaseCommit. Fast-forward it to the shared
	// lifecycle commit so the job packet is clean and the start record is not
	// duplicated when the branch is merged.
	if _, err := gitOutput(worktree, "merge", "--ff-only", startCommit); err != nil {
		return fmt.Errorf("advance job worktree to start record: %w", err)
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
	target := "HEAD"
	if o.commit != "" {
		target, err = resolveCommit(root, o.commit)
		if err != nil {
			return fmt.Errorf("resolve scope commit: %w", err)
		}
	}
	return checkJobScope(root, packet, worktree, target)
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
	fmt.Printf("Scope target: %s\n", strings.TrimSpace(commit))
	var violations []string
	for _, path := range paths {
		if filepath.ToSlash(path) == filepath.ToSlash(filepath.Join("control", "jobs", packet.ID+".md")) {
			continue
		}
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
	visiblePaths := make([]string, 0, len(paths))
	for _, path := range paths {
		if filepath.ToSlash(path) != filepath.ToSlash(filepath.Join("control", "jobs", packet.ID+".md")) {
			visiblePaths = append(visiblePaths, path)
		}
	}
	fmt.Printf("Scope: PASSED (%d changed paths)\n", len(visiblePaths))
	for _, path := range visiblePaths {
		fmt.Println(" ", path)
	}
	return nil
}

func runReviewSetup(worktree string, commands []string, timeout time.Duration) ([]setupResult, error) {
	results := make([]setupResult, 0, len(commands))
	for _, commandText := range commands {
		started := time.Now().UTC()
		result := setupResult{Command: commandText, StartedAt: started}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		command := exec.CommandContext(ctx, "bash", "-c", commandText)
		command.Dir = worktree
		output, err := command.CombinedOutput()
		redactedOutput := redactGateOutput(string(output))
		result.Output = base64.StdEncoding.EncodeToString([]byte(redactedOutput))
		result.OutputLines = strings.Split(redactedOutput, "\n")
		result.OutputEncoding = "base64"
		result.FinishedAt = time.Now().UTC()
		result.DurationMS = result.FinishedAt.Sub(started).Milliseconds()
		if ctx.Err() == context.DeadlineExceeded {
			result.Status = "failed"
			result.ExitCode = 124
			result.Reason = fmt.Sprintf("timed out after %s", timeout)
			cancel()
			results = append(results, result)
			return results, fmt.Errorf("review setup timed out: %s", commandText)
		}
		cancel()
		if err != nil {
			result.Status = "failed"
			result.ExitCode = 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitErr.ExitCode()
			}
			result.Reason = err.Error()
			results = append(results, result)
			return results, fmt.Errorf("review setup failed: %s", commandText)
		}
		result.Status = "passed"
		results = append(results, result)
	}
	return results, nil
}

func runJobGates(root string, packet jobPacket, worktree, commit string, setupErr error, setupResults []setupResult) ([]jobGateEvidence, error) {
	if len(packet.Gates) == 0 {
		return nil, nil
	}
	round := len(packet.ReviewRounds) + 1
	evidenceDir := filepath.Join(root, "control", "evidence")
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		return nil, fmt.Errorf("create evidence directory: %w", err)
	}
	results := make([]jobGateEvidence, 0, len(packet.Gates))
	setupPath, setupHash, err := writeSetupEvidence(root, packet, round, commit, setupResults)
	if err != nil {
		return nil, err
	}
	var firstErr error
	for _, gateName := range packet.Gates {
		var result gateResult
		var err error
		if setupErr != nil {
			now := time.Now().UTC()
			result = gateResult{Gate: gateName, Worktree: worktree, StartedAt: now, FinishedAt: now, Status: "failed", Checks: []checkResult{{ID: "review-setup", Status: "failed", ExitCode: 1, Reason: setupErr.Error()}}}
		} else {
			result, err = executeGate(worktree, gateOptions{gateName: gateName, worktree: worktree, timeout: 10 * time.Minute})
			if err != nil {
				now := time.Now().UTC()
				result = gateResult{Gate: gateName, Worktree: worktree, StartedAt: now, FinishedAt: now, Status: "failed", Checks: []checkResult{{ID: "gate-runner", Status: "failed", ExitCode: 1, Reason: err.Error()}}}
			}
		}
		verified := gateResultVerified(result, gateName, packet)
		path := filepath.Join(evidenceDir, fmt.Sprintf("%s-round-%d-%s.yaml", packet.ID, round, sanitizeFilename(gateName)))
		evidenceResult := result
		for index := range evidenceResult.Checks {
			if evidenceResult.Checks[index].Status == "manual" {
				evidenceResult.Checks[index].Attestation = gateName + "/" + evidenceResult.Checks[index].ID
			}
			if evidenceResult.Checks[index].Output != "" {
				rawOutput := evidenceResult.Checks[index].Output
				evidenceResult.Checks[index].Output = base64.StdEncoding.EncodeToString([]byte(rawOutput))
				evidenceResult.Checks[index].OutputLines = strings.Split(rawOutput, "\n")
				evidenceResult.Checks[index].OutputEncoding = "base64"
			}
		}
		document := gateEvidenceDocument{SchemaVersion: evidenceSchemaVersion, Job: packet.ID, Round: round, Commit: commit, RecordedAt: time.Now().UTC().Format(time.RFC3339), Leadfox: leadfoxVersion(), SetupEvidence: setupPath, Attestations: packet.Attestations, Result: evidenceResult}
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
		hash := sha256.Sum256(encoded)
		results = append(results, jobGateEvidence{Gate: gateName, Status: result.Status, Verified: verified, Commit: commit, Evidence: filepath.ToSlash(relative), Hash: hex.EncodeToString(hash[:]), RecordedAt: document.RecordedAt, SetupEvidence: setupPath, SetupHash: setupHash})
		if !verified {
			if firstErr == nil {
				firstErr = fmt.Errorf("required gate %q did not verify: %s", gateName, gateResultReason(result, gateName, packet))
			}
		}
	}
	return results, firstErr
}

func writeSetupEvidence(root string, packet jobPacket, round int, commit string, setup []setupResult) (string, string, error) {
	if len(setup) == 0 {
		return "", "", nil
	}
	document := setupEvidenceDocument{SchemaVersion: evidenceSchemaVersion, Job: packet.ID, Round: round, Commit: commit, RecordedAt: time.Now().UTC().Format(time.RFC3339), Leadfox: leadfoxVersion(), Setup: setup}
	encoded, err := yaml.Marshal(document)
	if err != nil {
		return "", "", fmt.Errorf("encode setup evidence: %w", err)
	}
	path := filepath.Join(root, "control", "evidence", fmt.Sprintf("%s-round-%d-setup.yaml", packet.ID, round))
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return "", "", fmt.Errorf("write setup evidence: %w", err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256(encoded)
	return filepath.ToSlash(relative), hex.EncodeToString(hash[:]), nil
}

func gateResultVerified(result gateResult, gateName string, packet jobPacket) bool {
	if result.Status == "failed" || result.Status == "deferred" {
		return false
	}
	for _, item := range result.Checks {
		if item.Status == "failed" || item.Status == "deferred" {
			return false
		}
		if item.Status == "manual" {
			if !hasAttestation(packet.Attestations, gateName, item.ID) {
				return false
			}
		}
	}
	return result.Status == "passed" || result.Status == "manual"
}

func gateResultReason(result gateResult, gateName string, packet jobPacket) string {
	for _, item := range result.Checks {
		if item.Status == "failed" {
			return item.ID + " failed"
		}
		if item.Status == "manual" {
			if !hasAttestation(packet.Attestations, gateName, item.ID) {
				return gateName + "/" + item.ID + " requires explicit attestation"
			}
		}
	}
	return "manual or deferred checks require explicit attestation"
}

func hasAttestation(attestations []jobAttestation, gate, check string) bool {
	passed, failed := false, false
	for _, attestation := range attestations {
		if attestation.Gate == gate && attestation.Check == check {
			passed = passed || attestation.Result == "passed"
			failed = failed || attestation.Result == "failed"
		}
	}
	return passed && !failed
}

func hasFailedAttestation(attestations []jobAttestation, gate, check string) bool {
	for _, attestation := range attestations {
		if attestation.Gate == gate && attestation.Check == check && attestation.Result == "failed" {
			return true
		}
	}
	return false
}

func hasAnyAttestation(attestations []jobAttestation, gate, check string) bool {
	for _, attestation := range attestations {
		if attestation.Gate == gate && attestation.Check == check {
			return true
		}
	}
	return false
}

func currentAttestation(attestations []jobAttestation, gate, check string) (jobAttestation, bool) {
	for _, attestation := range attestations {
		if attestation.Gate == gate && attestation.Check == check {
			return attestation, true
		}
	}
	return jobAttestation{}, false
}

func appendAttestation(attestations []jobAttestation, value jobAttestation) []jobAttestation {
	for index, existing := range attestations {
		if existing.Gate == value.Gate && existing.Check == value.Check {
			// Keep one current decision per manual check. This replaces the
			// automatic scope placeholder with an explicit decision and avoids
			// contradictory passed/failed entries accumulating.
			attestations[index] = value
			return attestations
		}
	}
	return append(attestations, value)
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func isManualCheck(file gateFile, gateName, checkName string) bool {
	for _, gate := range file.Gates {
		if gate.ID != gateName {
			continue
		}
		for _, check := range gate.Checks {
			if check.ID == checkName {
				return check.Manual || gate.Manual
			}
		}
	}
	return false
}

func gitShowFile(root, commit, path string) (string, error) {
	return gitOutput(root, "show", commit+":"+filepath.ToSlash(path))
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
	operation := strings.TrimPrefix(message, "Record ")
	commitBody := lifecycleCommitBody(root, operation)
	if _, err := gitOutput(root, "commit", "-m", message, "-m", commitBody); err != nil {
		return fmt.Errorf("commit lifecycle record: %w", err)
	}
	return nil
}

func lifecycleCommitBody(root, operation string) string {
	project := filepath.Base(root)
	if content, err := os.ReadFile(filepath.Join(root, "control", "project.yaml")); err == nil {
		var metadata struct {
			Name    string `yaml:"name"`
			Project struct {
				Name           string   `yaml:"name"`
				CommitTrailers []string `yaml:"commit_trailers,omitempty"`
			} `yaml:"project"`
		}
		if yaml.Unmarshal(content, &metadata) == nil && strings.TrimSpace(metadata.Name) != "" {
			project = strings.TrimSpace(metadata.Name)
		} else if strings.TrimSpace(metadata.Project.Name) != "" {
			project = strings.TrimSpace(metadata.Project.Name)
		}
		trailers := metadata.Project.CommitTrailers
		var trailerText strings.Builder
		for _, trailer := range trailers {
			trailer = strings.TrimSpace(trailer)
			if trailer == "" || strings.ContainsAny(trailer, "\r\n") {
				continue
			}
			trailerText.WriteString("\n")
			trailerText.WriteString(trailer)
		}
		return "Recorded by Leadfox lifecycle management.\n\nLeadfox-Managed: true\nLeadfox-Project: " + project + "\nLeadfox-Operation: " + operation + trailerText.String()
	}
	return "Recorded by Leadfox lifecycle management.\n\nLeadfox-Managed: true\nLeadfox-Project: " + project + "\nLeadfox-Operation: " + operation
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
	if packet.Status == "accepted" && o.reason == "" {
		return errors.New("--reason is required to re-review an accepted job")
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
	if packet.Status == "review" || packet.Status == "review-failed" || packet.Status == "changes-requested" {
		for _, previous := range uniqueStrings(append(packet.ReviewWorktrees, packet.ReviewWorktree)) {
			if _, statErr := os.Stat(previous); statErr == nil {
				if _, removeErr := gitOutput(root, "worktree", "remove", "--force", previous); removeErr != nil {
					return fmt.Errorf("remove previous review worktree: %w", removeErr)
				}
			} else if !os.IsNotExist(statErr) {
				return statErr
			}
		}
	}
	if _, err := os.Stat(worktree); err == nil {
		return fmt.Errorf("review worktree already exists: %s", worktree)
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
	packet.Attestations = appendScopeAttestations(worktree, packet)
	setupResults, setupErr := runReviewSetup(worktree, packet.ReviewSetup, 10*time.Minute)
	evidence, gateErr := runJobGates(root, packet, worktree, commit, setupErr, setupResults)
	reviewErr := setupErr
	if reviewErr == nil {
		reviewErr = gateErr
	}
	roundStatus := "review"
	packetStatus := "review"
	if reviewErr != nil {
		state := reviewEvidenceState(root, evidence)
		if setupErr == nil && state.Pending && !state.Failed {
			roundStatus = "pending"
			packetStatus = "review-pending"
			reviewErr = nil
		} else {
			roundStatus = "failed"
			packetStatus = "review-failed"
		}
	}
	round := jobReviewRound{Number: len(packet.ReviewRounds) + 1, Commit: commit, Status: roundStatus, Reason: o.reason, RecordedAt: time.Now().UTC().Format(time.RFC3339), Evidence: evidence}
	now := time.Now().UTC().Format(time.RFC3339)
	appendTransition(&packet, packetStatus, "review completed", "Leadfox", now)
	packet.Status, packet.ReviewCommit, packet.ReviewWorktree, packet.UpdatedAt = packetStatus, commit, worktree, now
	packet.ReviewWorktrees = appendUnique(packet.ReviewWorktrees, worktree)
	packet.GateEvidence = evidence
	packet.ReviewRounds = append(packet.ReviewRounds, round)
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	paths := []string{jobPath(root, id)}
	for _, item := range evidence {
		paths = append(paths, item.Evidence)
		if item.SetupEvidence != "" {
			paths = append(paths, item.SetupEvidence)
		}
	}
	if err := commitLifecycleRecord(root, paths, fmt.Sprintf("Record review %s round %d", id, round.Number)); err != nil {
		return err
	}
	if reviewErr != nil {
		_, _ = gitOutput(root, "worktree", "remove", "--force", worktree)
		return reviewErr
	}
	if packetStatus == "review-pending" {
		fmt.Printf("Review pending manual attestations for %s\n", id)
		return nil
	}
	fmt.Printf("Created review worktree for %s at %s\n", id, worktree)
	return nil
}

func reviewEvidenceState(root string, evidence []jobGateEvidence) evidenceState {
	state := evidenceState{}
	for _, item := range evidence {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(item.Evidence)))
		if err != nil {
			state.Failed = true
			continue
		}
		var document gateEvidenceDocument
		if yaml.Unmarshal(content, &document) != nil {
			state.Failed = true
			continue
		}
		if document.Result.Status == "failed" || document.Result.Status == "deferred" {
			state.Failed = true
		}
		for _, check := range document.Result.Checks {
			if check.Status == "failed" || check.Status == "deferred" {
				state.Failed = true
			}
		}
		if len(document.Result.Checks) > 0 {
			checks, err := pendingManualChecks(root, item)
			if err == nil && len(checks) > 0 {
				state.Pending = true
			}
		}
	}
	return state
}

func appendScopeAttestations(worktree string, packet jobPacket) []jobAttestation {
	content, err := os.ReadFile(filepath.Join(worktree, "control", "gates.yaml"))
	if err != nil {
		return packet.Attestations
	}
	var file gateFile
	if yaml.Unmarshal(content, &file) != nil {
		return packet.Attestations
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, gateName := range packet.Gates {
		for _, gate := range file.Gates {
			if gate.ID != gateName {
				continue
			}
			for _, item := range gate.Checks {
				// Scope is the only manual result Leadfox can establish itself.
				// Every other manual check requires a real attestation.
				if item.ID == "scope" && item.Manual && !hasAnyAttestation(packet.Attestations, gateName, item.ID) {
					packet.Attestations = appendAttestation(packet.Attestations, jobAttestation{Gate: gateName, Check: item.ID, Result: "passed", Note: "Leadfox scope check passed for the reviewed commit.", Actor: "Leadfox", RecordedAt: now})
				}
			}
		}
	}
	return packet.Attestations
}

func jobApprove(root, id string, o jobOptions) error {
	packet, body, err := readJob(root, id)
	if err != nil {
		return err
	}
	if packet.Status != "review" && packet.Status != "review-pending" && packet.Status != "review-failed" {
		return fmt.Errorf("job %s is %q; create a review worktree first", id, packet.Status)
	}
	if o.verdict != "accepted" && o.verdict != "changes-requested" {
		return errors.New("--verdict must be accepted or changes-requested")
	}
	if strings.TrimSpace(o.approvedBy) == "" {
		return errors.New("--approved-by is required so the approval identity is explicit")
	}
	if len(packet.Gates) > 0 {
		if len(packet.GateEvidence) != len(packet.Gates) {
			return errors.New("required gate evidence is missing; review the job before approval")
		}
		var pending []string
		var failed []string
		var unverified []string
		for _, evidence := range packet.GateEvidence {
			if evidence.Verified {
				continue
			}
			checks, pendingErr := pendingManualChecks(root, evidence)
			if pendingErr == nil {
				pending = append(pending, checks...)
			}
			failedChecks, failedErr := failedManualChecks(root, evidence)
			if failedErr == nil {
				failed = append(failed, failedChecks...)
			}
			if len(checks) == 0 && len(failedChecks) == 0 {
				unverified = append(unverified, evidence.Gate)
			}
		}
		if len(failed) > 0 {
			return fmt.Errorf("failed manual check(s): %s; pending manual check(s): %s", strings.Join(failed, ", "), strings.Join(pending, ", "))
		}
		if len(pending) > 0 {
			return fmt.Errorf("pending manual check(s): %s", strings.Join(pending, ", "))
		}
		if len(unverified) > 0 {
			return fmt.Errorf("required gate(s) not verified: %s", strings.Join(unverified, ", "))
		}
		if err := verifyGateEvidence(root, packet); err != nil {
			return err
		}
	}
	handoffPath := filepath.ToSlash(filepath.Join("control", "handoffs", id+".md"))
	if _, err := gitOutput(root, "cat-file", "-e", packet.ReviewCommit+":"+handoffPath); err != nil {
		return fmt.Errorf("reviewed commit does not contain required handoff %s", handoffPath)
	}
	dir := filepath.Join(root, "control", "approvals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, id+".md")
	now := time.Now().UTC().Format(time.RFC3339)
	actor := o.approvedBy
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
	fmt.Fprintf(&record, "\n## Round %d\n\n- Verdict: %s\n- Approved by: %s\n- Reviewed commit: `%s`\n", len(packet.ReviewRounds), o.verdict, actor, packet.ReviewCommit)
	if packet.ReviewWorktree != "" {
		if _, statErr := os.Stat(packet.ReviewWorktree); statErr == nil {
			fmt.Fprintf(&record, "- Review worktree: `%s`\n", packet.ReviewWorktree)
		}
	}
	fmt.Fprintf(&record, "- Recorded: %s\n", now)
	if len(packet.GateEvidence) == 0 {
		record.WriteString("- Required gates: none declared\n")
	} else {
		record.WriteString("- Required gates:\n")
		for _, evidence := range packet.GateEvidence {
			fmt.Fprintf(&record, "  - `%s`: %s; evidence: `%s`\n", evidence.Gate, evidence.Status, evidence.Evidence)
		}
	}
	if o.note != "" {
		fmt.Fprintf(&record, "- Note: %s\n", o.note)
	}
	record.WriteString("\nControl review evidence is recorded in the referenced gate evidence files.\n")
	fmt.Fprintf(&record, "Handoff: `%s`\n", handoffPath)
	if err := os.WriteFile(path, []byte(record.String()), 0o644); err != nil {
		return err
	}
	packet.Status, packet.ApprovedBy, packet.ApprovedAt, packet.UpdatedAt = o.verdict, actor, now, now
	if len(packet.ReviewRounds) > 0 {
		last := &packet.ReviewRounds[len(packet.ReviewRounds)-1]
		last.Status, last.ApprovedBy, last.ApprovedAt = o.verdict, actor, now
	}
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if err := commitLifecycleRecord(root, []string{jobPath(root, id), path}, fmt.Sprintf("Record approval %s round %d", id, len(packet.ReviewRounds))); err != nil {
		return err
	}
	fmt.Printf("Recorded %s verdict for %s\n", o.verdict, id)
	return nil
}

func pendingManualChecks(root string, evidence jobGateEvidence) ([]string, error) {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(evidence.Evidence)))
	if err != nil {
		return nil, err
	}
	var document gateEvidenceDocument
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, err
	}
	var pending []string
	for _, check := range document.Result.Checks {
		if check.Status == "manual" && !hasAttestation(document.Attestations, evidence.Gate, check.ID) && !hasFailedAttestation(document.Attestations, evidence.Gate, check.ID) {
			pending = append(pending, evidence.Gate+"/"+check.ID)
		}
	}
	return pending, nil
}

func failedManualChecks(root string, evidence jobGateEvidence) ([]string, error) {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(evidence.Evidence)))
	if err != nil {
		return nil, err
	}
	var document gateEvidenceDocument
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, err
	}
	var failed []string
	for _, check := range document.Result.Checks {
		if check.Status == "manual" && hasFailedAttestation(document.Attestations, evidence.Gate, check.ID) {
			failed = append(failed, evidence.Gate+"/"+check.ID)
		}
	}
	return failed, nil
}

func verifyGateEvidence(root string, packet jobPacket) error {
	latestRound := len(packet.ReviewRounds)
	setupVerified := map[string]bool{}
	for _, evidence := range packet.GateEvidence {
		if evidence.SetupEvidence != "" && !setupVerified[evidence.SetupEvidence] {
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(evidence.SetupEvidence)))
			if err != nil {
				return fmt.Errorf("required setup evidence is unavailable: %w", err)
			}
			hash := sha256.Sum256(content)
			if hex.EncodeToString(hash[:]) != evidence.SetupHash {
				return fmt.Errorf("required setup evidence has changed since review")
			}
			var setup setupEvidenceDocument
			if err := yaml.Unmarshal(content, &setup); err != nil {
				return fmt.Errorf("parse setup evidence: %w", err)
			}
			if setup.SchemaVersion != evidenceSchemaVersion {
				return fmt.Errorf("setup evidence uses schema version %d; expected %d; regenerate the review evidence with 'leadfox job review'", setup.SchemaVersion, evidenceSchemaVersion)
			}
			if setup.Job != packet.ID || setup.Round != latestRound || setup.Commit != packet.ReviewCommit {
				return errors.New("setup evidence does not match the reviewed job, round, or commit")
			}
			setupVerified[evidence.SetupEvidence] = true
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(evidence.Evidence)))
		if err != nil {
			return fmt.Errorf("required gate evidence %q is unavailable: %w", evidence.Gate, err)
		}
		hash := sha256.Sum256(content)
		if hex.EncodeToString(hash[:]) != evidence.Hash {
			return fmt.Errorf("required gate evidence %q has changed since review", evidence.Gate)
		}
		var document gateEvidenceDocument
		if err := yaml.Unmarshal(content, &document); err != nil {
			return fmt.Errorf("parse evidence for gate %q: %w", evidence.Gate, err)
		}
		if document.SchemaVersion != evidenceSchemaVersion {
			return fmt.Errorf("evidence for gate %q uses schema version %d; expected %d; regenerate the review evidence with 'leadfox job review'", evidence.Gate, document.SchemaVersion, evidenceSchemaVersion)
		}
		if document.Job != packet.ID || document.Round != latestRound || document.Commit != packet.ReviewCommit || document.Result.Gate != evidence.Gate {
			return fmt.Errorf("required gate evidence %q does not match the reviewed job, round, commit, or gate", evidence.Gate)
		}
		if document.Result.Status != evidence.Status || document.Result.Status == "failed" || document.Result.Status == "deferred" {
			return fmt.Errorf("required gate evidence %q is not passing", evidence.Gate)
		}
		for _, check := range document.Result.Checks {
			if check.Status == "failed" || check.Status == "deferred" {
				return fmt.Errorf("required gate evidence %q contains a %s check", evidence.Gate, check.Status)
			}
			if check.Status == "manual" && !hasAttestation(document.Attestations, evidence.Gate, check.ID) {
				return fmt.Errorf("required gate evidence %q contains unattested manual check %q", evidence.Gate, check.ID)
			}
		}
	}
	return nil
}

func jobAttest(root, id string, o jobOptions) error {
	packet, body, err := readJob(root, id)
	if err != nil {
		return err
	}
	if packet.Status != "review" && packet.Status != "review-pending" && packet.Status != "review-failed" {
		return fmt.Errorf("job %s is %q; attestations require a review result", id, packet.Status)
	}
	if o.check == "" {
		return errors.New("--check is required, for example --check local-dev/scope")
	}
	parts := strings.SplitN(o.check, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("--check must name an existing manual check as GATE/CHECK")
	}
	gateName, checkName := parts[0], parts[1]
	if !containsString(packet.Gates, gateName) {
		return fmt.Errorf("cannot attest check %q: gate is not required by this job", o.check)
	}
	if packet.ReviewCommit == "" {
		return errors.New("job has no reviewed commit")
	}
	gateFileContent, err := gitShowFile(root, packet.ReviewCommit, "control/gates.yaml")
	if err != nil {
		return fmt.Errorf("read reviewed gate definitions: %w", err)
	}
	var gateDefinitions gateFile
	if err := yaml.Unmarshal([]byte(gateFileContent), &gateDefinitions); err != nil {
		return fmt.Errorf("parse reviewed gate definitions: %w", err)
	}
	if !isManualCheck(gateDefinitions, gateName, checkName) {
		return fmt.Errorf("cannot attest check %q: it is not a declared manual check", o.check)
	}
	if o.result != "passed" && o.result != "failed" {
		return errors.New("--result must be passed or failed")
	}
	if o.approvedBy == "" {
		return errors.New("--approved-by is required for a manual attestation")
	}
	if o.result == "passed" && hasFailedAttestation(packet.Attestations, gateName, checkName) {
		return fmt.Errorf("check %s/%s has a failed attestation; create a new review round before attesting it as passed", gateName, checkName)
	}
	attestation := jobAttestation{Gate: gateName, Check: checkName, Result: o.result, Note: o.note, Actor: o.approvedBy, RecordedAt: time.Now().UTC().Format(time.RFC3339)}
	if previous, found := currentAttestation(packet.Attestations, gateName, checkName); found {
		previous.ReplacedAt = attestation.RecordedAt
		previous.ReplacedBy = attestation.Actor
		packet.AttestationHistory = append(packet.AttestationHistory, previous)
	}
	packet.Attestations = appendAttestation(packet.Attestations, attestation)
	packet.UpdatedAt = attestation.RecordedAt
	for index, evidence := range packet.GateEvidence {
		if evidence.Gate != gateName {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(evidence.Evidence)))
		if readErr != nil {
			return fmt.Errorf("read gate evidence %q: %w", evidence.Gate, readErr)
		}
		var document gateEvidenceDocument
		if err := yaml.Unmarshal(content, &document); err != nil {
			return fmt.Errorf("parse gate evidence %q: %w", evidence.Gate, err)
		}
		if document.SchemaVersion != evidenceSchemaVersion {
			return fmt.Errorf("gate evidence %q uses schema version %d; regenerate the review evidence with 'leadfox job review' before attesting", evidence.Gate, document.SchemaVersion)
		}
		document.Attestations = packet.Attestations
		gateVerified := gateResultVerified(document.Result, evidence.Gate, packet)
		if o.result == "failed" {
			document.Result.Status = "failed"
		} else if gateVerified {
			document.Result.Status = "passed"
		}
		updated, err := yaml.Marshal(document)
		if err != nil {
			return fmt.Errorf("encode gate evidence %q: %w", evidence.Gate, err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(evidence.Evidence)), updated, 0o644); err != nil {
			return fmt.Errorf("update gate evidence %q: %w", evidence.Gate, err)
		}
		hash := sha256.Sum256(updated)
		packet.GateEvidence[index].Hash = hex.EncodeToString(hash[:])
		packet.GateEvidence[index].Status = document.Result.Status
		packet.GateEvidence[index].Verified = gateVerified && o.result != "failed"
	}
	allVerified := len(packet.Gates) == 0
	if len(packet.Gates) > 0 {
		allVerified = len(packet.GateEvidence) == len(packet.Gates)
		for _, evidence := range packet.GateEvidence {
			allVerified = allVerified && evidence.Verified
		}
	}
	state := reviewEvidenceState(root, packet.GateEvidence)
	previousStatus := packet.Status
	nextStatus := "review-failed"
	switch {
	case allVerified:
		nextStatus = "review"
	case state.Failed:
		nextStatus = "review-failed"
	case state.Pending:
		nextStatus = "review-pending"
	}
	if previousStatus != nextStatus {
		appendTransition(&packet, nextStatus, "attestation "+o.check, o.approvedBy, packet.UpdatedAt)
	}
	packet.Status = nextStatus
	if len(packet.ReviewRounds) > 0 {
		last := &packet.ReviewRounds[len(packet.ReviewRounds)-1]
		last.Evidence = append([]jobGateEvidence(nil), packet.GateEvidence...)
		last.RecordedAt = packet.UpdatedAt
		switch packet.Status {
		case "review":
			last.Status = "review"
		case "review-pending":
			last.Status = "pending"
		default:
			last.Status = "failed"
		}
	}
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	paths := []string{jobPath(root, id)}
	for _, evidence := range packet.GateEvidence {
		paths = append(paths, evidence.Evidence)
	}
	if err := commitLifecycleRecord(root, paths, "Record attestation "+id); err != nil {
		return err
	}
	fmt.Printf("Recorded %s attestation for %s\n", o.result, o.check)
	return nil
}

func jobTerminate(root, id, status string, o jobOptions) error {
	packet, body, err := readJob(root, id)
	if err != nil {
		return err
	}
	if status != "cancelled" && status != "blocked" {
		return errors.New("invalid terminal job state")
	}
	if o.reason == "" {
		return errors.New("--reason is required")
	}
	if packet.Status == "merged" || packet.Status == "cancelled" {
		return fmt.Errorf("job %s is already terminal: %s", id, packet.Status)
	}
	actor := o.actor
	if actor == "" {
		actor = o.approvedBy
	}
	if actor == "" {
		return errors.New("--actor is required for a lifecycle transition")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	appendTransition(&packet, status, o.reason, actor, now)
	packet.Status, packet.TerminalReason, packet.TerminalBy, packet.TerminalAt, packet.UpdatedAt = status, o.reason, actor, now, now
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if status == "blocked" {
		if err := commitLifecycleRecord(root, []string{jobPath(root, id)}, "Record block "+id); err != nil {
			return err
		}
		fmt.Printf("Marked %s as blocked; its worktrees remain available for resume\n", id)
		return nil
	}
	if err := commitLifecycleRecord(root, []string{jobPath(root, id)}, "Record cancel "+id); err != nil {
		return err
	}
	for _, path := range uniqueStrings(append([]string{packet.Worktree, packet.ReviewWorktree}, packet.ReviewWorktrees...)) {
		if path == "" {
			continue
		}
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			continue
		}
		if _, removeErr := gitOutput(root, "worktree", "remove", "--force", path); removeErr != nil {
			return fmt.Errorf("recorded %s but cleanup failed: %w", status, removeErr)
		}
		packet.RemovedWorktrees = append(packet.RemovedWorktrees, jobWorktreeRecord{Path: path, RemovedAt: now})
	}
	packet.Worktree, packet.ReviewWorktree = "", ""
	packet.ReviewWorktrees = nil
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if err := commitLifecycleRecord(root, []string{jobPath(root, id)}, "Record cancel cleanup "+id); err != nil {
		return err
	}
	fmt.Printf("Marked %s as %s and cleaned up worktrees\n", id, status)
	return nil
}

func jobResume(root, id string, o jobOptions) error {
	packet, body, err := readJob(root, id)
	if err != nil {
		return err
	}
	if packet.Status != "blocked" {
		return fmt.Errorf("job %s is %q; only blocked jobs may resume", id, packet.Status)
	}
	if packet.Worktree == "" {
		return errors.New("blocked job has no worktree; start a new job instead")
	}
	if _, err := os.Stat(packet.Worktree); err != nil {
		return fmt.Errorf("blocked job worktree is unavailable: %w", err)
	}
	actor := o.actor
	if actor == "" {
		return errors.New("--actor is required for a lifecycle transition")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	appendTransition(&packet, "running", o.reason, actor, now)
	packet.Status = "running"
	packet.ResumeReason, packet.ResumeBy, packet.ResumeAt = o.reason, actor, now
	packet.UpdatedAt = now
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if err := commitLifecycleRecord(root, []string{jobPath(root, id)}, "Record resume "+id); err != nil {
		return err
	}
	fmt.Printf("Resumed %s by %s\n", id, actor)
	return nil
}

func appendTransition(packet *jobPacket, to, reason, actor, recordedAt string) {
	packet.Transitions = append(packet.Transitions, jobTransition{From: packet.Status, To: to, Reason: reason, Actor: actor, RecordedAt: recordedAt})
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
	if err := verifyGateEvidence(root, packet); err != nil {
		return fmt.Errorf("refusing merge: %w", err)
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
	if _, err := gitOutput(root, "merge", "--no-ff", packet.Branch, "-m", "Merge "+id, "-m", lifecycleCommitBody(root, "merge "+id)); err != nil {
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
	removedAt := time.Now().UTC().Format(time.RFC3339)
	for _, path := range uniqueStrings(append([]string{packet.Worktree, packet.ReviewWorktree}, packet.ReviewWorktrees...)) {
		if path != "" {
			if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
				continue
			}
			if _, err := gitOutput(root, "worktree", "remove", "--force", path); err != nil {
				return fmt.Errorf("merge succeeded but cleanup failed: %w", err)
			}
			packet.RemovedWorktrees = append(packet.RemovedWorktrees, jobWorktreeRecord{Path: path, RemovedAt: removedAt})
		}
	}
	packet.Worktree, packet.ReviewWorktree = "", ""
	packet.ReviewWorktrees = nil
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if err := commitLifecycleRecord(root, []string{jobPath(root, id)}, "Record cleanup "+id); err != nil {
		return err
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
		if strings.HasPrefix(path, filepath.ToSlash(filepath.Join("control", "evidence", id+"-"))) {
			continue
		}
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
