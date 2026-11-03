---
name: leadfox-project-planning
description: Guide a developer or planning agent through the lightweight Leadfox project intake and produce a validator-passing control/project-intake.md without approving or implementing the project.
---

# Leadfox Project Planning

Use this skill for the first planning conversation with a developer or
project manager. The output of this skill is the living intake document:

```text
control/project-intake.md
```

This is intentionally not an architecture plan, job packet, approval record,
or deployment design. Control uses the completed intake as the starting point
for later architecture planning and project review.

## Required outcome

Finish with a structurally valid intake by running:

```sh
leadfox intake validate control/project-intake.md
```

Keep revising the intake until the command exits `0`. If it exits `1`, fix the
reported findings. If it exits `2`, report the processing error and stop.
Never claim that an intake is valid without running the validator when Leadfox
is available.

The validator passing means only that the intake is structurally complete. It
does not approve the project, confirm architecture, authorize implementation,
or authorize external effects.

## Output boundary

For a new or early-stage project, create or update only:

```text
control/project-intake.md
```

Do not create `control/architecture-plan.md`, `control/project.yaml`,
requirements, jobs, Terraform, services, credentials, or secrets merely to
make the intake pass. `leadfox init` is responsible for creating the project
structure when the developer requests initialization.

If the project has already been initialized, use its existing template and
preserve its history. If it has not been initialized, explain that the
developer should run `leadfox init <name>` before creating a project-owned
intake. Do not present an external draft as an accepted project intake.

If an existing intake is present:

- preserve its stable document version and change history;
- update it as a living document rather than replacing it wholesale;
- record material additions, changes, removals, and deferrals in `Change
  history`;
- do not silently overwrite developer-confirmed information.

## Authority and safety

- Read applicable `AGENTS.md`, `CLAUDE.md`, project policy, and existing
  control files as instructions and project context; do not copy their
  contents into the intake.
- Treat the developer’s specification as the source of requirements.
- Never invent users, services, integrations, data, security properties, or
  production decisions.
- If information is unknown, write an explicit question, assumption, or
  deferral with an owner or next action.
- Suggestions are not decisions. Label them as suggestions or optional items.
- Never enter passwords, tokens, private keys, API keys, connection strings,
  or other secret values. Record only names, types, references, and handling
  expectations.
- Do not approve requirements or architecture, mark a project production
  ready, create jobs, start agents, merge, push, deploy, contact external
  systems, or create credentials.
- Never execute or simulate Leadfox uninstall. If relevant, show the command
  and require the user to run it directly.

## Initial project check

Before asking questions, inspect the project root and report briefly:

- whether `control/project-intake.md` exists;
- whether the project was initialized by Leadfox;
- the current lifecycle stage;
- whether requirements, services, environments, integrations, or an existing
  architecture plan already exist;
- any obvious open questions or inherited decisions.

If the developer supplied a detailed specification, do not force a long
interview. Extract what is explicit, then ask only for missing information
needed by the intake template.

Ask simple questions in small groups:

1. What are you building and who will use it?
2. What is the most important user flow?
3. What are the three most important first-version capabilities?
4. What services, data, environments, and outside systems are involved?
5. What must be protected, and what could go wrong?
6. What questions must Control answer before implementation?

Accept “unknown,” “not applicable,” and “defer this” when appropriate. Record
those answers explicitly instead of guessing.

## Intake document contract

Use the template created by `leadfox init`. Do not replace its headings with
architecture-plan headings or YAML front matter. Keep the template's field
names and meaning. Equivalent Markdown presentation is acceptable: metadata
and fields may be ordinary lines or Markdown bullets, and the change-history
table may use a different column order if its headers identify the version,
date, author, and change columns. The document must contain these top-level
sections:

```text
## Identity
## Problem and outcome
## Users and primary flow
## First-version capabilities
## Services and components
## Integrations and external dependencies
## Data
## Environments
## Security and operational concerns
## First planning questions
## Intake notes
## Change history
```

Complete these required fields:

```text
Document version
Current stage
Last updated
Change summary
Project name
Project owner
Intake author
Problem being solved
Desired outcome
How we will know the first version is useful
Primary users
What must be protected
Important abuse or failure concerns
Questions that must be answered before implementation
```

The document must also contain:

- three completed numbered items under `Main user flow`;
- three completed numbered first-version capabilities;
- at least one completed service/component row;
- at least one completed environment row, including `local`;
- a completed integration row or an explicit `none` entry;
- an initial completed `Change history` row.

Use a positive numeric document version such as `1` or `1.0.0`. Do not omit
the version, date, author, or change information from the history row.

Keep the intake understandable to a project manager. Do not require YAML
knowledge, infrastructure choices, job IDs, exact cloud resources, or
implementation details that the developer has not supplied.

## Status and certainty language

Use plain labels in the intake where useful:

- `confirmed`: explicitly supplied or confirmed by the developer;
- `proposed`: a suggestion requiring review;
- `deferred`: intentionally postponed with owner and next action;
- `unknown`: not yet known;
- `not applicable`: considered and rejected with a short reason.

Do not add an overall `PASS`, `FAIL`, `NOT READY`, or approval status to the
intake. Those are Control review outcomes, not planning-agent decisions.
`Current stage` describes the project, for example `idea`, `experiment`,
`local development`, `staging`, or `production`.

## Final response to the developer

Before finishing, report:

- the file created or updated;
- the information captured from the specification;
- explicit assumptions and deferred questions;
- the validator command and result;
- what Control still needs to review.

If validation fails, show the findings and continue editing when the agent has
enough information. Treat formatting-only findings as fixable presentation
issues, not as missing project substance. If information is genuinely
missing, stop with focused questions rather than inventing content.

## What happens next

After the intake passes, Control may use its project-local plan-review skill to
compare the intake with `control/project.yaml`, requirements, policies, and
existing project state. Control may then prepare a separate architecture plan
and proposed jobs for developer review.

The planning skill must not create that architecture plan as a substitute for
the intake, and must not describe a draft intake as approved.
