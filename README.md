# Leadfox

Leadfox is a project-agnostic command-line assistant for coordinating software
development and deployment. It helps a Control Agent turn requirements into
bounded jobs, run repeatable gates, preserve evidence, and advance work only
after the appropriate review and approval.

Leadfox is useful when a project has multiple services, environments, agents,
or deployment checks and the team wants a traceable process without giving an
agent unrestricted authority.

## Quick start

Build the CLI:

```sh
make build
```

Create a project and inspect its policy:

```sh
./leadfox init demo
cd demo
../leadfox policy validate
../leadfox policy show
```

Then start the project’s planning agent. The generated `AGENTS.md` explains
the project structure and the agent-led planning workflow.

## Agent host requirements

Control runs Leadfox commands that write to Git: `job start` creates a branch
and a worktree, and review, approval, merge and other lifecycle steps commit
records. The agent host running Control must therefore be able to write to
the project's `.git` directory and to a worktree folder outside the project
checkout.

> **Warning: Codex is not currently recommended as a Control agent.** Codex's
> default `workspace-write` sandbox keeps `.git` read-only, so every
> lifecycle step (`job start`, `review`, `approve`, `merge`, and each commit)
> needs a developer approval or must be run by the developer. In practice
> this means monitoring every job, which defeats Leadfox's purpose. Running
> Codex with full access removes the problem but also removes its sandbox.
> Codex job agents meet the same restriction for their own commits. See
> `docs/user-guide.md`, "Agent host requirements", and backlog item 8 in
> `docs/backlog-field-lessons.md`.

## Core workflow

1. `leadfox init` creates the project skeleton and project-owned policies.
2. Control gathers requirements and runs `leadfox plan`.
3. Control reconciles the approved architecture into services, settings, pins,
   contracts, and gates.
4. Control creates a bounded job packet for a Service, Integration,
   Infrastructure, or Contract Agent.
5. The agent works in its assigned branch and worktree.
6. Control reviews scope, runs the required gates, and records evidence.
7. A developer approves the result before merge or external side effects.

See [docs/user-guide.md](docs/user-guide.md) for the complete workflow.

## Important boundaries

Leadfox records and validates process decisions; it does not make production
changes by itself. Project policies determine which actions Control may take
automatically, which require a recorded rationale, and which require explicit
developer approval.

Leadfox does not store agent credentials, MCP tokens, cloud secrets, or
deployment credentials. An agent host supplies optional capabilities such as
an MCP design server and remains responsible for authenticating to it.

Assurance reports are evidence-coverage reports. They are not SOC 2
certifications or auditor attestations.

## Main commands

```text
leadfox init <name>             Create a project skeleton
leadfox project check           Check initialized project structure
leadfox project status          Summarize project readiness and activity
leadfox project schema PATH     Inspect project schema compatibility
leadfox project migrate PATH    Create missing managed files safely
leadfox project adopt PATH     Add missing structure to an existing project
leadfox doctor                  Diagnose the current project
leadfox ops <operation>         Protect and inspect durable operations state
leadfox context export         Export compact Control context
leadfox decision <operation>   Record or inspect decisions and approvals
leadfox intake validate         Validate the living project intake
leadfox change validate PATH    Validate a proposed project change
leadfox change impact PATH      Summarize change impact and review signals
leadfox change jobs PATH        Preview or create draft jobs from a change workflow
leadfox change hold PATH        List or release change-plan hold points
leadfox skill status             Report project and global skill status
leadfox skill sync               Preview or synchronize a pinned built-in skill
leadfox plan                   Build an architecture plan from requirements
leadfox gate run <name>        Run a project gate
leadfox job <operation>        Create and review bounded agent jobs
leadfox job status JOB-ID      Show lifecycle, gates, evidence, blockers, and next action
leadfox job handoff draft ID   Draft a bounded handoff from packet and evidence metadata
leadfox job scope add ID       Record a scope amendment and invalidate stale review evidence
leadfox job readiness JOB-ID    Check whether a job is ready to start
leadfox job context JOB-ID      Export bounded job context
leadfox job validate JOB-ID    Validate handoff and evidence completeness
leadfox job pause|resume ID     Pause or resume a job; its worktree is kept
leadfox job progress JOB-ID     Record or show worker progress (not evidence)
leadfox job runtime <op> ID     Report execution-host events for a running job
leadfox job check --all         Record worker loss and timeouts from runtime events
leadfox job recover JOB-ID      Recreate a lost worktree from the job branch
leadfox capability <operation> Inspect agent-host capabilities
leadfox policy <operation>     Validate or inspect Control authority policy
leadfox assurance report       Report assurance evidence coverage
leadfox security scan          Run configured secret scanning
leadfox version                Show build information
```

Use `leadfox --help` and the command-specific `--help` options for current
flags and behavior.

## Development

```sh
make test
make vet
```

Leadfox is written in Go and intentionally keeps project-specific architecture,
service code, cloud choices, and agent instructions in the consuming project.
