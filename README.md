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
leadfox intake validate         Validate the living project intake
leadfox change validate PATH    Validate a proposed project change
leadfox plan                   Build an architecture plan from requirements
leadfox gate run <name>        Run a project gate
leadfox job <operation>        Create and review bounded agent jobs
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
