package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const helpText = `Leadfox assists with the deployment of your project.

Usage:
  leadfox [command]

Commands:
  init <name>       Create a new Leadfox project
  intake <op>       Validate a project intake
  change <op>       Validate a proposed project change
  project <op>      Inspect an initialized Leadfox project
  doctor            Diagnose the current Leadfox project
  ops <operation>   Protect and inspect durable operations state
  context export    Export a compact, non-secret Control context
  decision <op>     Record and inspect project decisions
  plan              Create an architecture plan from requirements
  gate run <name>   Execute a project gate and report evidence
  job <operation>   Manage a bounded agent job lifecycle
  capability <op>   Inspect agent capabilities available to job hosts
  policy <op>       Validate or inspect the project decision policy
  risk <op>         Validate or inspect project-defined risk controls
  skill <op>        Install or verify Leadfox-provided skills
  assurance report  Report declared assurance evidence coverage
  security scan     Run the configured secret scanner
  uninstall         Remove the verified user-local Leadfox installation
  version [--json]  Show the Leadfox version and build information
  help              Show this help message

Options:
  -h, --help        Show this help message

Init options:
  -p, --parent DIR  Create the project under DIR
  -n, --dry-run     Show planned changes without creating files
`

const gateHelpText = `Execute a project gate from control/gates.yaml.

Usage:
  leadfox gate run <gate-name> [options]

Options:
  -c, --check ID       Run only one check from the gate
  -w, --worktree DIR   Execute from a project worktree
  -f, --format FORMAT  Output markdown (default) or json
  -t, --timeout D      Per-check timeout (default 10m)
  -r, --repeat N       Repeat the gate N times and report pass/fail consistency
  -h, --help           Show this help message
`

const initHelpText = `Create a new Leadfox project.

Usage:
  leadfox init <project-name> [options]

Options:
  -p, --parent DIR  Create the project under DIR
  -n, --dry-run     Show planned changes without creating files
  -h, --help        Show this help message
`

type initOptions struct {
	name     string
	parent   string
	dryRun   bool
	showHelp bool
}

func main() {
	if len(os.Args) <= 1 {
		fmt.Println("I am here to assist you in the deployment of your project!")
		return
	}

	switch os.Args[1] {
	case "-h", "--help", "help":
		fmt.Print(helpText)
	case "version", "--version":
		if len(os.Args) > 2 && os.Args[2] == "--json" {
			encoded, err := leadfoxBuildInfoJSON()
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			fmt.Println(encoded)
		} else if len(os.Args) > 2 {
			fmt.Fprintf(os.Stderr, "Error: unknown version option %q\n", os.Args[2])
			os.Exit(1)
		} else {
			fmt.Println(leadfoxVersion())
		}
	case "init":
		if err := runInit(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "intake":
		if err := runIntakeCommand(os.Args[2:]); err != nil {
			if errors.Is(err, errIntakeInvalid) {
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "change":
		if err := runChangeCommand(os.Args[2:]); err != nil {
			if errors.Is(err, errChangeInvalid) {
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(2)
		}
	case "project":
		if err := runProjectCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "doctor":
		if err := runProjectDoctor(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "ops":
		if err := runOperationsCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "context":
		if err := runContextCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "decision":
		if err := runDecisionCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "plan":
		if err := runPlan(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "gate":
		if err := runGateCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "job":
		if err := runJobCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "capability":
		if err := runCapabilityCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "policy":
		if err := runPolicyCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "risk":
		if err := runRiskCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "skill":
		if err := runSkillCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "assurance":
		if err := runAssuranceCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "security":
		if err := runSecurityCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "uninstall":
		if err := runUninstallCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command %q\n\n%s", os.Args[1], helpText)
		os.Exit(1)
	}
}

func runInit(args []string) error {
	options, err := parseInitOptions(args)
	if err != nil {
		return err
	}
	if options.showHelp {
		fmt.Print(initHelpText)
		return nil
	}
	if options.name == "" {
		return errors.New("a project name is required; use 'leadfox init --help' for usage")
	}
	if filepath.Base(options.name) != options.name || options.name == "." || options.name == ".." {
		return fmt.Errorf("invalid project name %q; use a single directory name", options.name)
	}

	parent, err := filepath.Abs(options.parent)
	if err != nil {
		return fmt.Errorf("resolve parent directory: %w", err)
	}
	target := filepath.Join(parent, options.name)

	if info, statErr := os.Stat(target); statErr == nil {
		if !info.IsDir() {
			return fmt.Errorf("target exists and is not a directory: %s", target)
		}
		entries, readErr := os.ReadDir(target)
		if readErr != nil {
			return fmt.Errorf("inspect existing target: %w", readErr)
		}
		if len(entries) > 0 {
			return fmt.Errorf("target directory is not empty: %s", target)
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect target: %w", statErr)
	}

	files := projectFiles(options.name)
	directories := []string{
		"control/settings",
		"control/requirements",
		"control/jobs",
		"control/handoffs",
		"control/audit",
		"control/evidence",
		"skills/leadfox-control-plan-review",
		"services",
		"tests",
		"infrastructure/terraform",
		"contracts",
		"docs",
	}

	if options.dryRun {
		fmt.Printf("Would create project: %s\n", target)
		for _, directory := range directories {
			fmt.Printf("  directory %s\n", filepath.Join(target, directory))
		}
		for path := range files {
			fmt.Printf("  file %s\n", filepath.Join(target, path))
		}
		fmt.Printf("  symlink %s -> AGENTS.md\n", filepath.Join(target, "CLAUDE.md"))
		return nil
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create project directory: %w", err)
	}
	for _, directory := range directories {
		if err := os.MkdirAll(filepath.Join(target, directory), 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", directory, err)
		}
	}
	for path, content := range files {
		fullPath := filepath.Join(target, path)
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(target, "CLAUDE.md")); err != nil {
		return fmt.Errorf("create CLAUDE.md symlink: %w", err)
	}

	fmt.Printf("Created Leadfox project: %s\n", target)
	fmt.Printf("Next steps:\n  cd %s\n  start your preferred planning agent\n", target)
	return nil
}

func parseInitOptions(args []string) (initOptions, error) {
	options := initOptions{parent: "."}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "-h", "--help":
			options.showHelp = true
		case "-n", "--dry-run":
			options.dryRun = true
		case "-p", "--parent":
			if index+1 >= len(args) {
				return options, fmt.Errorf("%s requires a directory", arg)
			}
			index++
			options.parent = args[index]
		default:
			if len(arg) > 0 && arg[0] == '-' {
				return options, fmt.Errorf("unknown init option %q", arg)
			}
			if options.name != "" {
				return options, errors.New("only one project name may be provided")
			}
			options.name = arg
		}
	}
	return options, nil
}

func projectFiles(projectName string) map[string]string {
	return map[string]string{
		"README.md": fmt.Sprintf(`# %s

This project was initialized by Leadfox.

Next step: review the project structure and use a planning agent to gather
requirements and propose an architecture before implementation begins.
`, projectName),
		".gitignore": `# Local certificates, credentials, and runtime state.
.local/
`,
		"AGENTS.md": `# Agent instructions

This is a Leadfox-managed project.

## Repository structure

- control/: project metadata, pins, gates, and environment settings
- control/project-intake.md: developer or project-manager planning intake
- control/skills.yaml: project-pinned and optional host skills
- skills/: project-local Control skills
- control/requirements/: confirmed functional requirements
- services/: application services
- tests/: unit, integration, and end-to-end tests
- infrastructure/terraform/: infrastructure definitions
- contracts/: service and external-system contracts
- docs/: evolving architecture and process documentation

Read the relevant control files and documentation before making changes.
Agent capabilities are declared in control/agent-capabilities.yaml. A job may
require a capability such as an MCP design server, but Leadfox never stores
the provider credentials or grants access outside the agent host.

## Control decision policy

Read control/decision-policy.yaml before taking project actions. Control may
act automatically only for actions listed under automatic. Actions under
control_with_record require a recorded rationale. Actions under
developer_approval require explicit developer approval before execution.
Escalation triggers always require developer review. Do not repeatedly ask
about a decision already covered by the policy; escalate only when the action
falls outside it or a trigger applies.
For the planning workflow, read control/project-intake.md and
docs/agent-planning.md. The project-local Control plan-review skill is in
skills/leadfox-control-plan-review/SKILL.md. For role boundaries,
job packets, and handoffs, read docs/agent-roles.md.
When the project has no confirmed requirements, ask the developer whether to
begin the agent-led planning process. Do not assume the developer knows the
planning prompt.
Gather requirements and propose an architecture before implementing services.
Use the optional global leadfox-project-planning skill when available. Skills
guide agents but do not replace deterministic Leadfox validation or developer
approval.
After requirements are confirmed, run leadfox plan from the project root and
review its proposed architecture plan with the developer.
Use leadfox gate run <gate-name> to execute trusted project gates and attach
the resulting evidence to reviews. A gate result is evidence, not approval.
Before implementation, reconcile the approved project model: service names,
plan approval status, initial contracts, initial gates, environment settings,
and secret references. Show the resulting change summary before writing code.
Do not commit secrets or modify unrelated services.
`,
		"control/project.yaml": fmt.Sprintf(`project:
  name: %s
  version: 1
  # Optional trailers added to Leadfox-managed commits.
  # commit_trailers:
  #   - "Co-Authored-By: Example <example@example.test>"

services: []
external_dependencies: []
environments:
  - local
  - ephemeral
  - production
entry_points: []
data_stores: []
`, projectName),
		"control/decisions.yaml": `# Project decision and approval ledger.
# This records governance; it does not grant permission or execute actions.
version: 1
decisions: []
`,
		"control/project-intake.md": fmt.Sprintf(`# Project intake: %s

Document version: 1
Current stage: idea
Last updated: <!-- YYYY-MM-DD -->
Change summary: Initial project intake

## Identity

- Project name: %s
- Project owner: <!-- required -->
- Intake author: <!-- required -->

## Problem and outcome

- Problem being solved: <!-- required -->
- Desired outcome: <!-- required -->
- How we will know the first version is useful: <!-- required -->
- Explicit non-goals for the first version: <!-- optional -->

## Users and primary flow

- Primary users: <!-- required -->
- Other actors or administrators: <!-- optional -->
- Main user flow:
  1. <!-- required -->
  2. <!-- required -->
  3. <!-- required -->
- Most important failure or rejection cases: <!-- optional -->

## First-version capabilities

1. <!-- required -->
2. <!-- required -->
3. <!-- required -->

## Services and components

| Component or service | Purpose | Existing or new | Owner | Unknowns |
|---|---|---|---|---|
| <!-- required --> | <!-- required --> | <!-- existing/new --> | <!-- required --> | <!-- unknowns --> |

## Integrations and external dependencies

| Integration | Why it is needed | Direction | Environment needed | Owner | Known constraints |
|---|---|---|---|---|---|
| <!-- required; write none if none --> | <!-- required --> | <!-- inbound/outbound/both --> | <!-- local/test/staging/prod --> | <!-- required --> | <!-- unknowns --> |

## Data

- Data the system receives: <!-- required -->
- Data the system creates or stores: <!-- required -->
- Sensitive data involved: <!-- required; never enter values -->
- Data that must not be retained: <!-- optional -->
- Retention or deletion expectations: <!-- optional -->

## Environments

| Environment | Purpose | Must exist now? | External access | Data/secrets allowed | Destruction expectation |
|---|---|---|---|---|---|
| local | <!-- required --> | <!-- yes/no --> | <!-- none/mock/limited --> | <!-- placeholders/test data --> | <!-- disposable/retained --> |
| experiment or staging | <!-- optional --> | <!-- yes/no --> | <!-- description --> | <!-- description --> | <!-- description --> |
| production | <!-- optional --> | <!-- yes/no --> | <!-- description --> | <!-- description --> | <!-- description --> |

## Security and operational concerns

- What must be protected: <!-- required -->
- Who should be allowed to do what: <!-- optional -->
- Secrets or credentials required: <!-- names/types only -->
- Important abuse or failure concerns: <!-- required -->
- Availability, performance, or cost concerns: <!-- optional -->
- Regulatory or contractual constraints: <!-- optional; do not claim compliance -->

## First planning questions

- Decisions the owner already made: <!-- optional -->
- Decisions Control should analyze: <!-- optional -->
- Questions that must be answered before implementation: <!-- required -->
- Preferred first job or milestone: <!-- optional -->

## Intake notes

- Assumptions explicitly allowed for initial planning: <!-- optional -->
- Information intentionally deferred: <!-- optional -->
- Optional skills that may help: <!-- suggestions only -->
- Additional references: <!-- optional -->

## Change history

| Version | Date | Author | Change | Reason or source |
|---|---|---|---|---|
| 1 | <!-- date --> | <!-- author --> | Initial intake | Leadfox initialization |
`, projectName, projectName),
		"control/skills.yaml": fmt.Sprintf(`version: 1
skills:
  - id: leadfox-control-plan-review
    version: "1"
    source: leadfox-distribution
    scope: project
    required_for: control
    path: skills/leadfox-control-plan-review/SKILL.md
    sha256: %s
  - id: leadfox-project-planning
    version: "1"
    source: leadfox-distribution
    scope: global
    required_for: planning
    adoption: optional
    sha256: %s
    note: Install globally with leadfox skill install; project initialization does not overwrite host skills.
`, controlPlanReviewSkillSHA256(), skillDigest()),
		"skills/leadfox-control-plan-review/SKILL.md": string(controlPlanReviewSkill),
		"control/pins.yaml": `# Exact artifact and definition versions used by a candidate.
# Service-owned dependency locks remain with their service.
pins: []
`,
		"control/agent-capabilities.yaml": `# Provider-neutral capabilities available to agent hosts.
# Leadfox records requirements; it does not connect to providers or store credentials.
# Add a capability here, then reference its ID from a job with --capability.
capabilities: []
`,
		"control/decision-policy.yaml": `# Project-owned authority policy for the Control Agent.
# Action names are project-defined. Credentials, tokens and approvals do not belong here.
version: 1
decision_policy:
  automatic:
    - local_tests
    - formatting
    - worktree_cleanup
    - job_sequencing
    - documentation_updates
  control_with_record:
    - ephemeral_configuration
    - agent_assignment
    - model_selection
    - nonproduction_defaults
  developer_approval:
    - external_network_access
    - tool_installation
    - cloud_resources
    - iam_changes
    - secrets
    - remote_push
    - production_changes
  escalation_triggers:
    - public_exposure
    - cost_change
    - destructive_action
    - security_exception
    - compliance_claim
    - uncertain_requirement
`,
		"control/risk-controls.yaml": `# Project-owned risk controls for bounded jobs.
# Leadfox validates job references; the project defines evidence and stop conditions.
version: 1
risk_controls: []
`,
		"control/gates.yaml": `# Checks required at each stage of the project lifecycle.
gates: []
`,
		"control/assurance.yaml": `# Project assurance coverage. This maps framework controls to evidence.
# Evidence coverage is an audit aid; it is not a compliance certification.
frameworks: []
controls: []
`,
		"control/audit/README.md": `# Control audit records

Leadfox lifecycle records, approvals, gate evidence, attestations, decision
records, and durable-artifact hashes provide project history for review.
These records are evidence coverage only; they are not a SOC 2 certification,
attestation, or substitute for an independent audit.

Do not store credentials or secret values here. Store references, hashes,
actors, timestamps, commits, decisions, and outcomes.
`,
		"control/evidence/README.md": `# Control evidence

Store redacted, reviewable evidence here when a project policy requires it.
Evidence should identify its source, commit or operation, actor, timestamp,
result, and hash where applicable. Never store secret values.
`,
		"control/security.yaml": `# Project-configured secret scanning.
# Keep scanner binaries outside the repository and pin their versions here.
# TruffleHog verification is an external operation and is not allowed in the
# default local gate.
secret_scanning:
  enabled: false
  tool: gitleaks
  binary: gitleaks
  version: ""
  mode: history
  report_path: control/evidence/secret-scan.json
  allow_network: false
`,
		"control/settings/README.md": `# Settings

Environment-specific, non-secret settings belong here. Keep secrets as
references to an appropriate secret store rather than committing values.
`,
		"control/settings/technology.yaml": `# Project technology policy. Agents may propose deviations with a rationale.
technology:
  backend:
    preferred: go
    alternatives:
      - python
    selection_rule: use Python when it materially improves delivery and testing without unacceptable loss of performance, scalability, or operational simplicity
  frontend:
    preferred:
      - typescript
      - react
      - vite
      - tailwindcss
  scripting:
    preferred:
      - bash
`,
		"control/settings/environments.yaml": fmt.Sprintf(`environments:
  local:
    runtime: local
    tls:
      enabled: true
      provider: mkcert
      hosts:
        - localhost
        - %s.test
      certificate_directory: .local/tls
    secret_directory: .local/secrets
  ephemeral:
    runtime: isolated
    tls:
      enabled: true
      provider: environment-managed
  production:
    runtime: approved
    tls:
      enabled: true
      provider: managed-certificate
`, projectName),
		"control/requirements/README.md": `# Requirements

Add one Markdown document per requirement. Use a stable requirement ID in the
filename and confirm the requirement with the developer before planning.

The planning command reads active and confirmed documents from this directory.
`,
		"control/jobs/README.md": `# Agent jobs

Store approved job packets here. A job packet bounds delegated work; it does
not grant merge or deployment approval.

Each job should identify its role, requirement IDs, allowed paths, inputs,
expected outputs, checks, gates, stop conditions, handoff format, and any
required agent capability IDs.
`,
		"control/handoffs/README.md": `# Agent handoffs

Store handoff records here when a delegated job returns. Control reviews the
changed paths and evidence against the job packet before accepting the work.

An accepted handoff is required before dependent work, pin changes, promotion,
or merge approval.
`,
		"services/README.md": `# Services

Add one directory per application service. Each service should document its
runtime type, dependencies, interfaces, build method, and tests.
`,
		"tests/README.md": `# Tests

Place unit, integration, contract, and end-to-end tests here as the project
architecture is defined.
`,
		"infrastructure/terraform/README.md": `# Terraform

Infrastructure definitions will be added here when the project architecture
has been approved.
`,
		"contracts/README.md": `# Contracts

Document service, data, and external-system contracts here. Contracts define
interactions; they do not replace service implementations.
`,
		"docs/README.md": `# Documentation

Record approved architecture and evolving development, environment, gate, and
release processes here.
`,
		"docs/decision-policy.md": `# Control decision policy

The project-owned authority policy is in control/decision-policy.yaml. It
reduces approval fatigue by allowing Control to act within declared boundaries.

- automatic: Control may act without asking, then report normally;
- control_with_record: Control may decide, but records the action and rationale;
- developer_approval: Control must obtain explicit approval before execution;
- escalation_triggers: Control always pauses for developer review.

The policy does not grant permission to store credentials, access external
systems, make production changes, or claim compliance. When an action matches
an escalation trigger or falls outside the policy, Control must stop and ask.
Recorded defaults should not be asked again unless the project policy changes.
`,
		"docs/agent-roles.md": `# Agent roles and handoffs

Leadfox uses logical roles. A role may be performed by a separate agent, by a
human, or by the same agent performing another role. The role names describe
responsibilities and review boundaries; they do not require a particular
runtime or vendor.

## Control Agent

The Control Agent coordinates the project lifecycle. It gathers and confirms
requirements, maintains the project model, prepares architecture plans,
creates bounded job packets, assigns work, and coordinates reviews. It owns
pins, gates, approvals, and the decision to advance work. It should not accept
a service agent's claim of completion without inspecting the declared outputs
and running the applicable gates.

## Service Agent

The Service Agent implements one application service within its assigned
directory. It owns service code, service configuration interfaces, service
tests, migrations required by that service, and service-level contracts. It
must document assumptions, expose the declared entry points, run its local
checks, and return a handoff summary. It does not change unrelated services or
shared control decisions.

## Integration Agent

The Integration Agent assembles the runnable environment around services. It
owns local or ephemeral composition, test doubles for external systems,
service wiring, integration and end-to-end tests, and the integration gate. It
may execute service migrations against an isolated database, but it does not
silently redefine the service's schema or application behavior.

## Infrastructure Agent

The Infrastructure Agent maps the approved environment model to deployable
infrastructure, such as Terraform, Cloud Run, Cloud SQL, buckets, networking,
and secret references. It owns infrastructure plans and environment
provisioning checks. It does not invent application requirements or modify
service behavior to make infrastructure pass.

## Contract Agent (optional)

When useful, a Contract Agent maintains API, event, data, and external-system
contracts and their compatibility tests. This role can be combined with the
Service or Integration Agent for a small project.

## Job packet

Every delegated job should state:

- objective and requirement IDs;
- assigned role and responsible paths;
- allowed files or directories and explicitly excluded areas;
- input artifacts, pins, and settings references;
- required agent capability IDs and the fallback if a capability is unavailable;
- expected outputs and acceptance checks;
- required gate(s), stop conditions, and unresolved questions;
- handoff format and the agent's final status.

## Handoff and review

An agent returns the changed paths, tests and gates run, relevant output,
assumptions, unresolved items, and a recommended next step. Control reviews
the diff and artifacts, verifies that the work stayed within the job packet,
and runs or delegates the applicable gates. A successful handoff is not an
approval. Control explicitly accepts the result before assigning dependent
work or allowing a merge, pin update, or environment promotion.

For a small project, use these boundaries even when one agent performs all
roles. The separation preserves a reviewable process and makes later
parallelization possible.
`,
		"docs/agent-planning.md": `# Agent planning workflow

1. Read AGENTS.md, control/project-intake.md, and the current control files.
2. Determine the current project state.
3. If the intake is incomplete, use the optional leadfox-project-planning skill
   to guide the developer or planning agent through the missing fields.
4. Run leadfox intake validate and correct its structural findings.
5. If no confirmed requirements exist, ask the developer whether to begin the
   agent-led planning process.
6. If the developer agrees, gather and summarize the requirements.
7. Confirm the requirements with the developer.
8. Record confirmed requirements under control/requirements/.
9. Run leadfox plan from the project root.
10. Review the generated architecture plan with the developer.
11. Before implementation, reconcile the approved model:
   - resolve service-name mismatches;
   - mark the architecture plan approved;
   - add the initial contracts;
   - define the first gates;
   - confirm non-secret settings and secret references;
   - show the proposed change summary.
12. Do not implement services or provision infrastructure until the plan and
    reconciliation changes are explicitly approved.
11. Read docs/agent-roles.md and divide the approved build plan into bounded
    job packets. Start with the smallest independently reviewable step.
12. Assign service implementation, environment integration, and infrastructure
    provisioning to the appropriate logical role. One agent may perform more
    than one role, but the boundaries and handoffs still apply.
13. Review each handoff against its job packet and run the required gate before
    assigning dependent work. Do not treat an idle agent or a claimed
    completion as approval.
14. Use the project technology policy and environment settings when creating
    jobs. Local TLS may use mkcert; never commit generated certificates,
    private keys, or local secrets.

If requirements or a plan already exist, offer to resume the appropriate
stage instead of restarting the planning process.
`,
	}
}
