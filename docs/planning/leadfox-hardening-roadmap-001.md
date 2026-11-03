# Leadfox hardening and product roadmap

**Plan ID:** LEADFOX-ROADMAP-001  
**Version:** 1  
**Status:** draft  
**Audience:** Leadfox maintainers and Control  
**Scope:** general-purpose Leadfox distribution, project initialization,
planning, governance, agent coordination, and deployment support

## Current status

The local checkout was reviewed on 2026-11-02.

- Remote: `https://github.com/ainnovationllc/leadfox.git`
- Remote `main`: `2f33af2`
- Local `main`: `5413d0d`, two commits ahead of remote
- Working tree: contains uncommitted runtime, lifecycle, documentation,
  planning-template, and skill work
- `go test ./...`: passed
- `go vet ./...`: passed

The repository already has a useful prototype foundation:

- project initialization;
- architecture-plan generation;
- bounded job lifecycle with branches and worktrees;
- gates and repeat runs;
- evidence and assurance reporting;
- Gitleaks-based secret scanning;
- provider-neutral agent capabilities, including MCP declarations;
- project decision policy;
- project-defined risk controls;
- distribution builds and platform-aware version reporting.

The main gap is not another isolated command. It is turning these foundations
into a coherent, distributable product with a clear separation between:

1. installation;
2. developer/project-manager intake;
3. Control plan review;
4. deterministic Leadfox enforcement; and
5. external deployment operations.

## Product boundaries

Leadfox should remain project-agnostic. It supplies lifecycle mechanics,
validation, evidence, policy structure, and host capability declarations. The
consuming project supplies its own requirements, architecture, services,
environments, cloud choices, risk controls, and agent instructions.

Leadfox must not silently:

- approve plans or jobs;
- create credentials or secrets;
- contact cloud, Canvas, GitHub, registries, or other external services;
- copy `AGENTS.md` or `CLAUDE.md` into planning documents;
- turn an optional skill into a project requirement;
- claim that evidence constitutes a compliance certification.

## Planning validation boundary

The planning skill helps a human or agent populate the intake, but it is not
the authority that decides whether the intake is structurally acceptable.

The validator belongs to Leadfox and is made available by project
initialization. The intended flow is:

```text
planning skill -> control/project-intake.md -> Leadfox intake validator
```

The validator must be deterministic and independent of the agent that produced
the document. It should provide both human-readable diagnostics and a
machine-readable result. It validates structure, required fields, allowed
status values, document versioning, secret-content rules, and change-history
format. It does not approve the project, judge architecture quality, create
jobs, or contact external systems.

This is a first-slice architectural decision: a valid intake is a prerequisite
for Control review, not evidence that the project is approved or ready for
implementation.

## Priority 0 — release and installation foundation

These are required before calling Leadfox generally available.

### 0.1 Define the release artifact and npm bootstrap

Create a supported distribution model for:

- npm package name and ownership;
- platform and architecture matrix;
- versioned Leadfox binaries;
- exact checksums and, preferably, signed release metadata;
- unavailable-platform behavior;
- reproducible build metadata;
- release provenance.

Implement the planned user flows:

```text
npx leadfox install
npx leadfox@latest update
npx leadfox version
npx leadfox doctor
```

The bootstrap must install into a user-owned `~/.leadfox` directory, verify
the artifact before activation, preserve the previous version, and never
require administrator privileges by default.

### 0.2 Make PATH changes explicit and reversible

Implement shell-aware PATH handling:

- detect zsh, bash, and supported Windows shells;
- show the exact proposed change before writing it;
- request confirmation;
- avoid duplicate entries;
- preserve unrelated shell configuration;
- provide a manual setup path when the user declines;
- support rollback of the line Leadfox added.

### 0.3 Make installation and update failure-safe

Add tests for:

- wrong checksum;
- invalid signature, when signatures are enabled;
- interrupted download;
- malformed existing installation;
- conflicting binary or symlink;
- unwritable destination;
- failed activation;
- rollback to the prior verified version.

No partial artifact may become the active binary.

### 0.4 Establish release-quality CLI behavior

Before publishing, standardize:

- exit codes;
- machine-readable JSON output;
- stable error categories;
- version/source/platform reporting;
- `--help` output;
- diagnostic redaction;
- atomic file writes;
- installation and update logs without secrets.

## Priority 0 — planning and project initialization

### 0.5 Separate the two planning skills

Create and package two distinct skills:

#### `leadfox-project-planning`

General, optional skill for a human or planning agent. It helps create the
lightweight `control/project-intake.md` document using plain language.

#### `leadfox-control-plan-review`

Project-pinned internal skill used by Control after initialization. It reviews
the intake, reconciles it with the project model and policies, identifies gaps,
proposes decisions, and prepares the detailed architecture plan.

Neither skill may silently approve decisions or perform external actions.

The intake validator is not part of either skill. It is a Leadfox/project
initialization capability and must be runnable even when no planning agent is
available.

### 0.6 Make intake the low-friction entry point

Add project initialization support for:

- a lightweight project intake;
- project owner, stage, purpose, users, first flow, capabilities, services,
  integrations, data, environments, security concerns, and open questions;
- unknown and deferred values without forced guessing;
- optional skill suggestions that do not become requirements automatically.

The intake should be useful before `control/project.yaml` exists.

### 0.7 Reconcile the current `leadfox plan` command

The current planner writes both `architecture-plan.md` and
`architecture-plan.yaml`. The product direction is one canonical,
human-readable architecture plan with structured metadata when required. This
needs an explicit compatibility decision and implementation:

- preserve existing approved plans;
- do not overwrite approvals;
- distinguish intake, draft architecture plan, approved plan, and implemented
  project state;
- validate source paths and requirement traceability;
- avoid invented requirement identifiers;
- report project-model consistency separately from planning completeness;
- never mark a draft invalid merely because project initialization is pending.

### 0.8 Make planning documents living records

Support and document:

- document version and update date;
- change summary;
- stable requirement, decision, service, and environment identifiers;
- additions, changes, removals, superseded decisions, and deferrals;
- affected jobs and traceability updates;
- human-readable change history.

Support a second planning mode for existing projects: a change plan. A change
plan references the current project model and architecture-plan version and
describes only the proposed delta. It must cover affected requirements,
services, integrations, data, environments, decisions, compatibility,
migration, rollback, security, cost, and deployment impact. The validator must
distinguish added, changed, removed, and superseded items.

## Priority 0 — deterministic Control safety

These capabilities turn lessons from lti-demo into reusable Leadfox behavior.

### 0.9 Complete risk-control enforcement

The current risk-control registry is a foundation. Add a validated evidence
model for controls such as:

- real-tool validation;
- clean-baseline security evidence;
- forbidden-action checks;
- mutation testing;
- persistent operations checkout;
- retained artifacts;
- CI validation before main;
- flaky-test confirmation;
- offline execution.

Leadfox should validate that required evidence is declared, attached, and
linked to the reviewed commit. It should not interpret domain-specific results
or grant approval automatically.

### 0.10 Enforce persistent operations checkouts

Add a first-class operations-checkout model:

- create or register a named persistent checkout;
- distinguish it from disposable job and review worktrees;
- refuse cleanup without explicit confirmation;
- route stateful cloud plans, applies, saved plans, and local state through it;
- record the checkout path in operation evidence;
- detect state or backend artifacts in development worktrees;
- hash and verify local state before and after operations.

### 0.11 Retain artifacts before cleanup

Before job cleanup, Leadfox should:

- identify declared durable artifacts;
- copy them outside disposable worktrees;
- record destination, mode, size, and hash;
- refuse cleanup when required retention is incomplete;
- distinguish temporary reports from milestone evidence.

### 0.12 Add enforced execution boundaries

Replace per-command conventions with an execution wrapper that can enforce:

- network denial for offline jobs;
- an allowlisted tool PATH;
- blocked high-risk binaries;
- wrapper identity and version evidence;
- positive and negative wrapper tests;
- a recorded incident when commands bypass the wrapper.

The product must not claim that an empty network log proves no network access.

## Priority 1 — Control and agent workflow

### 1.1 Make project discovery explicit

When Leadfox runs from a project directory, it should detect and report:

- whether this is an initialized Leadfox/AInnov project;
- project name and lifecycle stage;
- current plan and requirement status;
- services, environments, and integrations;
- open jobs, blockers, pending approvals, and dirty operational state.

Add a read-only `leadfox status` or `leadfox project status` command.

### 1.2 Pin and register project skills safely

During initialization, optionally register:

- the intake-planning skill;
- the Control plan-review skill;
- project-selected optional skills.

Record skill name, source, version, checksum, and adoption status. Do not copy
skill contents into `AGENTS.md` or `CLAUDE.md`, and never install globally
without explicit consent.

### 1.3 Add host-agent launch integration

Define a provider-neutral agent-launch interface for future Claude, Codex,
Herdr, and other hosts:

- launch request and model/reasoning profile;
- working directory and allowed paths;
- capability requirements;
- environment and secret boundary;
- cost or token budget when supported;
- cancellation and timeout;
- structured result and handoff;
- no implicit credential sharing.

Herdr should be an optional host adapter, not a Leadfox core dependency.

### 1.4 Strengthen job lifecycle invariants

Extend current lifecycle behavior with:

- explicit job schema versioning;
- typed role and status validation;
- immutable acceptance records;
- stronger evidence-to-criterion links;
- durable artifact retention hooks;
- operations-checkout exclusions;
- CI-validation requirements for CI-run checks;
- structured supersede/reissue history.

## Priority 1 — project model and validation

### 1.5 Add schema and semantic validation commands

Provide deterministic validation for:

- project model;
- intake;
- architecture plan;
- requirements and traceability;
- decision policy;
- risk controls;
- agent capabilities;
- gates;
- job packets;
- skill registrations.

The first command in this area should be an intake validator, for example:

```text
leadfox intake validate
```

It should validate `control/project-intake.md` without requiring an agent,
external service, or project approval. A successful result means the intake is
well-formed for Control review; it does not mean that the plan is approved.

Validation should identify whether a problem is a parse error, a missing
source, an unresolved decision, a policy violation, or a proposed item awaiting
approval.

The validation surface should include both:

```text
leadfox intake validate
leadfox change validate control/changes/CHANGE-001.md
```

The first validates a new-project intake. The second validates a proposed
change against an existing project baseline. A passing result means the
document is structurally suitable for Control review, not that the change is
approved or implemented.

### 1.6 Define initialization migrations

As the project structure evolves, initialization must be versioned and
non-destructive:

- detect existing projects;
- show a dry-run migration;
- preserve local modifications;
- create only missing files;
- record the Leadfox schema version;
- support rollback or documented recovery.

### 1.7 Improve generated project documentation

Generated files should explain:

- the two-stage planning model;
- current project status and next action;
- optional versus adopted skills;
- role boundaries;
- policy and approval categories;
- how to use the intake and living plan;
- how to run validation and gates.

Generated `AGENTS.md` and `CLAUDE.md` should remain concise operating
instructions, not copies of planning templates or skill contents.

## Priority 1 — testing, observability, and security

### 1.8 Build a test matrix for the installer and initializer

Cover supported operating systems, architectures, shells, existing projects,
empty directories, interrupted operations, permissions, upgrades, rollback,
and non-interactive use.

### 1.9 Add security regression coverage

Include tests for:

- path traversal;
- unsafe symlinks;
- arbitrary file overwrite;
- checksum/signature bypass;
- secret leakage in logs and reports;
- unsafe PATH edits;
- credentials inherited by child agents;
- unauthorized external operations;
- cleanup of persistent operations state.

### 1.10 Add structured local diagnostics

`leadfox doctor` should report version, project discovery, skill registration,
policy validity, capability declarations, executable permissions, and known
state hazards without contacting external services by default.

## Priority 2 — documentation and general availability

### 2.1 Publish the user manual

Provide:

- installation quick start;
- update and rollback guide;
- project intake walkthrough;
- Control workflow;
- job roles and handoffs;
- policies and approvals;
- capabilities and MCP;
- Herdr integration guidance;
- operations checkout guidance;
- troubleshooting and recovery.

### 2.2 Publish a maintainer guide

Document release builds, package publication, checksums/signatures, supported
platforms, compatibility policy, migration policy, and security response.

### 2.3 Establish release validation

Before a release is published:

- build from a clean checkout;
- run tests and vet;
- scan the repository and release artifacts;
- verify binary versions and checksums;
- install from npm on a clean machine;
- test update, failure, rollback, and PATH behavior;
- validate a sample project initialization;
- retain release evidence.

## Suggested implementation sequence

1. Protect the current work with a clean commit boundary and establish the
   release/package decisions.
2. Implement installer, update, verification, doctor, and PATH behavior.
3. Split and pin the intake and Control plan-review skills.
4. Reconcile `leadfox plan` with the two-stage living-document model.
5. Add project discovery, schema validation, and initialization migration
   support.
6. Complete risk-control enforcement, persistent operations checkouts, and
   artifact retention.
7. Add execution boundaries and CI-validation policy enforcement.
8. Add host-agent/Herdr adapters only after the core lifecycle is stable.
9. Publish the manual, maintainer guide, and first supported release.

## Review decisions needed before implementation

- Official npm package name and registry.
- First supported operating systems and architectures.
- Whether signed release metadata is required for the first release.
- Whether Windows is in the first release.
- Exact user-local installation directory and retention policy.
- Whether the architecture plan has one canonical format or a compatibility
  structured companion.
- Where project-pinned skills are stored and how they are verified.
- Whether `leadfox init` installs the intake skill automatically or asks first.
- Which operations are core Leadfox behavior versus optional host adapters.
- Compatibility and migration policy for initialized projects.

## Change history

| Version | Date | Author | Change |
|---|---|---|---|
| 1 | 2026-11-02 | Leadfox planning | Initial status review and prioritized hardening roadmap |
