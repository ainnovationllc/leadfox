package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
`

type jobPacket struct {
	ID             string   `yaml:"id"`
	Status         string   `yaml:"status"`
	Role           string   `yaml:"role"`
	Objective      string   `yaml:"objective"`
	BaseBranch     string   `yaml:"base_branch,omitempty"`
	BaseCommit     string   `yaml:"base_commit,omitempty"`
	Branch         string   `yaml:"branch,omitempty"`
	Worktree       string   `yaml:"worktree,omitempty"`
	ReviewCommit   string   `yaml:"review_commit,omitempty"`
	ReviewWorktree string   `yaml:"review_worktree,omitempty"`
	AllowedPaths   []string `yaml:"allowed_paths,omitempty"`
	ExcludedPaths  []string `yaml:"excluded_paths,omitempty"`
	Gates          []string `yaml:"gates,omitempty"`
	CreatedAt      string   `yaml:"created_at,omitempty"`
	UpdatedAt      string   `yaml:"updated_at,omitempty"`
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
	changed, err := gitOutput(worktree, "diff", "--name-only", packet.BaseCommit+"...HEAD")
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
		return fmt.Errorf("review worktree already exists: %s", worktree)
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := gitOutput(root, "worktree", "add", "--detach", worktree, commit); err != nil {
		return fmt.Errorf("create review worktree: %w", err)
	}
	packet.Status, packet.ReviewCommit, packet.ReviewWorktree, packet.UpdatedAt = "review", strings.TrimSpace(commit), worktree, time.Now().UTC().Format(time.RFC3339)
	if err := writeJob(root, packet, body); err != nil {
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
	dir := filepath.Join(root, "control", "approvals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, id+".md")
	record := fmt.Sprintf("# Approval: %s\n\n- Verdict: %s\n- Reviewed commit: `%s`\n- Review worktree: `%s`\n- Recorded: %s\n\nAdd Control review evidence and rationale here.\n", id, o.verdict, packet.ReviewCommit, packet.ReviewWorktree, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(record), 0o644); err != nil {
		return err
	}
	packet.Status, packet.UpdatedAt = o.verdict, time.Now().UTC().Format(time.RFC3339)
	if err := writeJob(root, packet, body); err != nil {
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
	branchCommit, err := gitOutput(root, "rev-parse", packet.Branch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(branchCommit) != strings.TrimSpace(packet.ReviewCommit) {
		return errors.New("job branch no longer matches the reviewed commit")
	}
	if _, err := gitOutput(root, "merge", "--no-ff", packet.Branch, "-m", "Merge "+id); err != nil {
		return fmt.Errorf("merge job branch: %w", err)
	}
	packet.Status, packet.UpdatedAt = "merged", time.Now().UTC().Format(time.RFC3339)
	if err := writeJob(root, packet, body); err != nil {
		return err
	}
	if _, err := gitOutput(root, "add", jobPath(root, id), filepath.Join(root, "control", "approvals", id+".md")); err != nil {
		return fmt.Errorf("stage merge records: %w", err)
	}
	if _, err := gitOutput(root, "commit", "-m", "Record merge "+id); err != nil {
		return fmt.Errorf("record merge: %w", err)
	}
	for _, path := range []string{packet.Worktree, packet.ReviewWorktree} {
		if path != "" {
			if _, err := gitOutput(root, "worktree", "remove", path); err != nil {
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
