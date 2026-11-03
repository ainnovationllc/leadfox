# Leadfox project planning templates

Leadfox uses two related planning documents for different audiences.

1. **Project intake** — a short document written by a developer or project
   manager. It gives Control enough information to understand the project’s
   purpose, users, main flow, services, integrations, and first environment.
2. **Control architecture plan** — the detailed document Control develops from
   the intake, requirements, and later decisions. It is the planning source
   used to decompose work into jobs and prepare an AInnov project.

The intake is intentionally incomplete. It is not expected to contain final
security, infrastructure, deployment, or compliance decisions. Unknowns are
useful information and should be recorded as unknown rather than guessed.

Both documents are living records. They should describe the current approved
understanding while preserving enough history to explain how that understanding
changed. A change to scope, requirements, users, services, integrations, data,
environments, or a significant decision must be recorded; it must not simply
overwrite the previous understanding without an explanation.

`AGENTS.md` and `CLAUDE.md` remain agent operating instructions. They are not
planning content and should not be copied into either template. The plan may
summarize a relevant constraint and cite the file that supplied it.

Skills are optional recommendations. A plan may identify a useful skill and
explain when it could help, but mentioning a skill does not make it required,
does not create an acceptance criterion, and does not block planning unless the
developer or an authoritative project policy explicitly adopts it.

## Recommended lifecycle

```text
Developer/project manager
        |
        v
Project intake
        |
        | Control clarifies gaps, without silently deciding for the owner
        v
Requirements and architecture plan
        |
        | Developer reviews proposed decisions and unresolved questions
        v
AInnov project initialization
        |
        v
Jobs, gates, implementation, review, and deployment
```

The planning skill should normally produce or update the intake first. Control
may then create the full architecture plan. Neither document approves work,
starts jobs, pushes code, or authorizes external operations.

## Stage 1: project intake

Suggested file: `control/project-intake.md`

The following is the complete intake template. Fields marked **required** are
the minimum needed for Control to understand the project. Fields marked
**optional** may be left blank or written as `unknown`.

```markdown
# Project intake

## Identity

- Project name: <!-- required; short stable name -->
- One-sentence description: <!-- required -->
- Project owner: <!-- required; person or team -->
- Intake author: <!-- required -->
- Date: <!-- required -->
- Current stage: <!-- required: idea | experiment | local development | staging | production -->
- Document version: <!-- required; start at 1 and increment for substantive changes -->
- Last updated: <!-- required -->
- Change summary: <!-- required for every update -->

## Problem and outcome

- Problem being solved: <!-- required -->
- Desired outcome: <!-- required -->
- How we will know the first version is useful: <!-- required -->
- Explicit non-goals for the first version: <!-- optional -->

## Users and primary flow

- Primary users: <!-- required -->
- Other actors or administrators: <!-- optional -->
- Main user flow, in numbered steps:
  1. <!-- required -->
  2. <!-- required -->
  3. <!-- required -->
- Most important failure or rejection cases: <!-- optional -->

## First-version capabilities

1. <!-- required: capability and user value -->
2. <!-- required -->
3. <!-- required -->

## Services and components

| Component or service | Purpose | Existing or new | Owner | Unknowns |
|---|---|---|---|---|
| <!-- e.g. web app --> | <!-- what it does --> | <!-- existing/new --> | <!-- team --> | <!-- open questions --> |

## Integrations and external dependencies

| Integration | Why it is needed | Direction | Environment needed | Owner | Known constraints |
|---|---|---|---|---|---|
| <!-- e.g. Canvas LMS --> | <!-- purpose --> | <!-- inbound/outbound/both --> | <!-- local/test/staging/prod --> | <!-- team --> | <!-- unknowns or limits --> |

## Data

- Data the system receives: <!-- required; high-level description only -->
- Data the system creates or stores: <!-- required -->
- Sensitive data involved: <!-- required; say none if none known -->
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
- Secrets or credentials required: <!-- required; list names/types, never values -->
- Important abuse or failure concerns: <!-- required -->
- Availability, performance, or cost concerns: <!-- optional -->
- Regulatory or contractual constraints: <!-- optional; do not claim compliance -->

## First planning questions

- Decisions the owner already made: <!-- optional; cite source if one exists -->
- Decisions Control should analyze: <!-- optional -->
- Questions that must be answered before implementation: <!-- required; may be none -->
- Preferred first job or milestone: <!-- optional -->

## Intake notes

- Assumptions explicitly allowed for initial planning: <!-- optional -->
- Information intentionally deferred: <!-- optional -->
- Optional skills that may help: <!-- optional; include purpose, not skill contents -->
- Additional references: <!-- optional; paths or links -->

## Change history

| Version | Date | Author | Change | Reason or source | Effect on scope, requirements, or decisions |
|---|---|---|---|---|---|
| 1 | <!-- date --> | <!-- author --> | Initial intake | <!-- source --> | Initial project outline |
```

### Intake field definitions

| Field | Meaning | What Control should do with it |
|---|---|---|
| Project name | Stable human-readable project identifier | Use it consistently; do not rename it silently |
| Current stage | Where the project is now | Choose an appropriate planning depth; do not assume production intent |
| Problem and outcome | Why the project exists and what success means | Turn the outcome into candidate requirements and acceptance evidence |
| Primary users | People or systems using the project | Identify actors, permissions, and user-flow requirements |
| Main user flow | The minimum end-to-end behavior | Use it to identify services, interfaces, data movement, and failure cases |
| First-version capabilities | The initial scope | Separate must-have scope from later ideas |
| Component or service | A deployable or logically independent part | Propose ownership, boundaries, jobs, and tests |
| Integration | An outside system, API, or provider | Record direction, credentials, environment, owner, and validation needs |
| Data received/stored | Information crossing or retained by the system | Identify lifecycle, ownership, privacy, and deletion requirements |
| Sensitive data | Data requiring special handling | Define secret references and controls without asking for values |
| Environment | Local, experimental, staging, or production context | Keep state, credentials, network access, and cleanup boundaries separate |
| Security concerns | Threats, permissions, and protected assets | Convert them into risks, controls, and acceptance criteria; do not claim they are solved yet |
| First planning questions | Decisions still needing analysis or owner input | Preserve them as explicit questions with an owner and next action |
| Document version | Version of this planning record | Increment for substantive changes; do not use it as an implementation release number |
| Last updated | Date of the latest edit | Update whenever the document changes |
| Change summary | Short description of the current edit | Make the latest change understandable without comparing files |
| Change history | Chronological record of material edits | Preserve additions, changes, removals, reasons, and affected decisions |
| Optional skills | Skills that may help an agent perform later work | Treat as suggestions only unless explicitly adopted by the owner or project policy |

## Stage 2: Control architecture plan

Suggested file: `control/architecture-plan.md`

This is the detailed plan that Control creates after reviewing the intake. It
uses YAML front matter and the following stable sections. A section may be
`complete`, `partial`, `proposed`, `deferred`, `not_applicable`, or `blocked`.
The status describes the planning section, not whether the project has been
implemented.

```markdown
---
plan_id: PLAN-001
version: 1
status: draft
project: example-project
generated_by: leadfox-project-planner
approval: pending
created_at: YYYY-MM-DD
updated_at: YYYY-MM-DD
source_requirements: []
source_project_files: []
---

## Purpose and success
## Scope and non-goals
## Requirements and traceability
## Users and actors
## Services and responsibilities
## Interfaces and external dependencies
## Data and data lifecycle
## Environments
## Security, privacy, and secrets
## Testing and gates
## Operations and deployment
## Risks and mitigations
## Decisions and approvals
## Unresolved items
## Proposed job decomposition
## Ingestion readiness

## Change history
```

### Full-plan field definitions

| Section | What it must answer |
|---|---|
| Purpose and success | What is being built, for whom, and what observable result defines success? |
| Scope and non-goals | What is included now, and what is explicitly deferred? |
| Requirements and traceability | Which confirmed requirements exist, and where will each be delivered and tested? |
| Users and actors | Who uses, administers, operates, or reviews the system, and what are their boundaries? |
| Services and responsibilities | What components exist, who owns them, and what each one does? |
| Interfaces and external dependencies | What APIs, providers, protocols, and external systems are involved, and how will they be validated? |
| Data and data lifecycle | What data exists, who owns it, where it lives, how long it remains, and how it is deleted? |
| Environments | What local, experimental, staging, and production environments exist, with their state, secret, access, and destruction boundaries? |
| Security, privacy, and secrets | What assets and threats matter, what controls are required, and what evidence will prove them? |
| Testing and gates | Which tests and deterministic gates must pass before each milestone? |
| Operations and deployment | How are builds, releases, migrations, rollback, monitoring, support, and cleanup handled? |
| Risks and mitigations | What could go wrong, who owns each risk, what reduces it, and what evidence remains? |
| Decisions and approvals | Which decisions are confirmed, proposed, inherited, or awaiting owner approval? |
| Unresolved items | Which questions remain open, who must answer them, and what is the next action? |
| Proposed job decomposition | What bounded jobs could implement the plan, with paths, dependencies, gates, stop conditions, and acceptance criteria? |
| Ingestion readiness | Is the plan structurally ready for an initialized AInnov project, and what must happen next? |
| Change history | What changed between versions, why it changed, who made the change, and which requirements, decisions, jobs, or environments were affected? |

If skills are useful, include a short optional section such as:

```markdown
## Suggested skills

| Skill | Possible use | Required? | Adoption decision |
|---|---|---|---|
| `frontend-design` | Help explore an initial interface direction | No | Not adopted; suggestion only |
| `cloud-run-basics` | Provide reference guidance during deployment planning | No | Revisit during infrastructure planning |
```

Do not paste a skill's instructions into the project plan. The skill remains a
separate reusable resource.

The full plan should also include a short document-control block near the top
of the body:

```markdown
> **Document control**
> - Version: 3
> - Last updated: 2026-11-02
> - Change summary: Added the Canvas roster integration and deferred production deployment.
> - Current lifecycle status: draft
```

Its `## Change history` should use this format:

```markdown
## Change history

| Version | Date | Author | Change type | Summary | Affected items | Approval or source |
|---|---|---|---|---|---|---|
| 1 | 2026-11-01 | Project Manager | initial | Initial architecture outline | all sections | intake |
| 2 | 2026-11-02 | Control | added | Added Canvas roster integration | REQ-001.8, Canvas, JOB-P1-005 | proposed |
| 3 | 2026-11-04 | Developer | changed | Deferred production deployment | Production environment, ADR-005 | developer review |
```

## Living-document rules

- Increment the document version for substantive changes. Formatting-only
  corrections may be grouped without changing the version.
- Update `updated_at`, the document-control summary, and the change-history row
  in the same edit.
- Never delete a requirement, integration, service, decision, risk, or
  environment without recording that it was removed, when, why, and what
  replaces it, if anything.
- Preserve stable identifiers when the meaning is unchanged. If the meaning
  changes materially, create a new identifier and mark the old one `superseded`
  rather than silently reusing it.
- A removed feature is not the same as a deferred feature. Record whether it
  was removed, rejected, superseded, or postponed.
- Requirement and decision status should be explicit. Useful values include
  `proposed`, `confirmed`, `in_progress`, `implemented`, `deferred`,
  `superseded`, `rejected`, and `removed`.
- When a change affects jobs, update traceability and identify whether an
  existing job is invalidated, needs revision, or remains unaffected.
- When a change affects an approved decision, do not silently rewrite it.
  Record a new decision or revision that cites the earlier record and requires
  the appropriate approval.
- Keep the current plan readable. The change log records evolution; it should
  not require readers to reconstruct the current architecture from every past
  version.
- When practical, retain prior file versions through normal version control or
  project records. The change history is a human-readable index, not a
  replacement for the underlying review and approval evidence.

## Planning versus implementation status

These terms must not be conflated:

| Term | Meaning |
|---|---|
| `draft` | The plan is being prepared and may contain proposed or open material |
| `ready-for-review` | The plan is internally organized enough for developer review |
| `approved` | The developer explicitly approved the plan or cited decision record |
| `implemented` | The planned work is complete and supported by evidence |
| `proposed` | An agent or Control suggestion that still needs review |
| `deferred` | Intentionally postponed with an owner and next action |
| `unknown` | Not known; never fill it with an invented default |
| `not_applicable` | Considered and rejected as irrelevant, with a reason |
| `blocked` | Work cannot proceed until a named condition changes |

If the project has not been initialized, that is not a failure of the intake.
The plan should say:

```text
Planning status: draft
Project model: not initialized
Ingestion readiness: not ready
Next action: review the plan, initialize the AInnov project model, then reconcile the plan.
```

`control/project.yaml`, `AGENTS.md`, and `CLAUDE.md` are project-model and
governance files. The planning agent should read them when they exist, but it
should not create them merely to make an intake valid. Their absence means
that model consistency cannot yet be checked.

## Control handoff rules

When converting an intake into a full plan, Control should:

1. Preserve the owner's stated goals and explicitly confirmed decisions.
2. Label every new suggestion as `proposed` until reviewed.
3. Convert unknowns into questions with an owner and next action.
4. Trace requirements to actual source files or clearly labeled derived items.
5. Separate local, experimental, staging, and production assumptions.
6. Never include secret values, credentials, private keys, or tokens.
7. Avoid compliance claims; describe controls and evidence instead.
8. Avoid creating jobs, infrastructure, credentials, or external side effects
   during planning.
9. Treat the intake and architecture plan as living documents: preserve stable
   identifiers, record material changes, and maintain traceability across
   versions.
10. Keep agent instructions and skill contents separate from the plan; cite
    relevant constraints and list skills only as optional recommendations unless
    explicitly adopted.

The intake is therefore the developer’s low-friction entry point. The full
architecture plan remains the control-grade document needed before detailed
job planning, implementation, and deployment.

## Planning updates to an existing project

An existing project needs a second planning mode: a **change plan**. This is
used when a developer proposes adding a service, changing an integration,
altering a data model, introducing an environment, or removing functionality.

An update should not regenerate a new complete architecture plan from scratch.
That would make it difficult to see what changed and could accidentally erase
approved project history. Instead:

1. The current project model and architecture plan are the baseline.
2. The change plan describes only the proposed delta and its impact.
3. Leadfox validates the change plan against that baseline.
4. Control reviews it and proposes bounded jobs.
5. After approval and implementation, the current architecture plan is updated
   and the change record remains linked in the living-document history.

Suggested file: `control/changes/CHANGE-001-add-reporting-service.md`

```markdown
---
change_id: CHANGE-001
version: 1
status: draft
project: example-project
change_type: add-service
generated_by: leadfox-project-planner
approval: pending
base_project_model: control/project.yaml
base_architecture_plan: control/architecture-plan.md
base_plan_version: 3
source_requirements: []
---

## Change summary

- What is changing: <!-- required -->
- Why it is changing: <!-- required -->
- Requested by: <!-- required -->
- Desired milestone: <!-- optional -->

## Affected baseline

- Existing services affected: <!-- required; may be none -->
- Existing integrations affected: <!-- required; may be none -->
- Existing data stores affected: <!-- required; may be none -->
- Existing environments affected: <!-- required -->
- Existing requirements affected: <!-- required -->
- Existing decisions or jobs affected: <!-- required; may be none -->

## Proposed delta

### Added

- Services:
- Requirements:
- Interfaces or integrations:
- Data entities or schema changes:
- Environments or configuration:
- Gates, controls, or capabilities:

### Changed

- Services:
- Requirements:
- Interfaces or integrations:
- Data entities or schema changes:
- Environments or configuration:
- Gates, controls, or capabilities:

### Removed or superseded

- Item:
- Reason:
- Replacement, if any:

## Compatibility and impact

- Existing behavior preserved: <!-- required -->
- API or contract compatibility: <!-- required -->
- Data migration required: <!-- required; yes/no/unknown -->
- Backfill or rollback strategy: <!-- required -->
- Secret and credential impact: <!-- required; references only, never values -->
- Security and privacy impact: <!-- required -->
- Cost, performance, and availability impact: <!-- required -->
- Deployment order and dependencies: <!-- required -->

## Proposed validation and jobs

- Required tests and gates:
- Real-tool or external validation needed:
- Proposed jobs:
- Stop conditions:

## Open decisions

| Decision | Owner | Status | Next action |
|---|---|---|---|
| <!-- decision --> | <!-- owner --> | proposed/deferred/unknown | <!-- action --> |

## Change history

| Version | Date | Author | Change | Reason or source |
|---|---|---|---|---|
| 1 | <!-- date --> | <!-- author --> | Initial change proposal | <!-- source --> |
```

### Update validation rules

The validator should require that a change plan:

- identifies an existing project model and architecture-plan version;
- verifies that the referenced baseline files exist and are internally valid;
- identifies every affected requirement, service, integration, data store,
  environment, decision, and job;
- distinguishes added, changed, removed, and superseded items;
- explains compatibility, migration, rollback, security, cost, and deployment
  impact;
- does not silently change an approved decision;
- does not claim that implementation is complete merely because the change is
  proposed;
- uses stable identifiers and records the eventual result in project history.

The same deterministic validator can support both modes:

```text
leadfox intake validate
leadfox change validate control/changes/CHANGE-001-add-reporting-service.md
```

The first validates a new-project intake. The second validates a proposed
delta against the current project baseline. Both commands are useful to a
planning agent while remaining mandatory review inputs for Control.
