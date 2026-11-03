# Leadfox pre-Herdr hardening and continuity plan

Status: implementation baseline; release verification pending
Plan owner: Leadfox maintainers
Scope: general-purpose Leadfox core
Dependency: current Leadfox planning, project, policy, job, gate, evidence, capability, and decision features

## Purpose

Complete the core Leadfox control plane before introducing Herdr or another
agent-process host. Leadfox must be able to discover a project, preserve its
governance model, migrate it safely, validate bounded work, diagnose missing
prerequisites, and protect durable operational state without depending on how
an agent process is launched.

Herdr remains a later optional execution adapter. It may start and coordinate
Claude, Codex, or other agents, but it must not become the source of project
authority, approval, scope, evidence, or deployment policy.

The current implementation provides the core foundations described here:
project inspection and migration, diagnostics, decision and risk records,
capability health, change impact, persistent operations checkout protection,
durable artifact retention, and bounded Control context. The checkpoints below
remain the release acceptance checklist. Broader compatibility fixtures,
regression coverage, and policy enforcement are not claimed complete merely
because the foundations exist.

## Product boundary

Leadfox owns:

- project discovery and initialization;
- project model, intake, change, decision, and policy validation;
- job scope, lifecycle, readiness, context, evidence, and handoff validation;
- gates, security scans, assurance evidence, risk controls, and diagnostics;
- safe installation, update, migration, and recovery behavior.

An agent host owns:

- starting an agent process;
- supplying model credentials and host capabilities;
- terminal, MCP, or provider-specific session management;
- reporting process output back to Leadfox.

Leadfox never stores model-provider credentials and never treats an agent
process result as approval.

## Implementation sequence

The work should be delivered in the following order. Each phase must pass its
checkpoint before the next phase starts.

### Phase 0 — Baseline and compatibility contract

Document the current command surface and file schemas. Add a compatibility
matrix for projects created by each supported Leadfox version.

Deliverables:

- versioned schemas for `project.yaml`, `skills.yaml`, decision policy,
  decisions, risk controls, capabilities, jobs, and evidence;
- supported migration rules and unsupported-version behavior;
- a fixture project for the current release and at least one older fixture;
- no changes to existing project files during this phase.

Checkpoint P0:

- `go test ./...`, `go vet ./...`, and npm tests pass;
- the compatibility fixture is readable without network access;
- unsupported schema versions fail with an actionable message.

### Phase 1 — Installation, update, and recovery hardening

Complete the user-facing installation lifecycle before changing project
behavior.

Deliverables:

- verified release installation and update through the npm bootstrap;
- permission checks for `.leadfox` with an explanation and no partial install;
- atomic update with the previous installation retained until verification;
- rollback if verification fails;
- `doctor` checks for binary, manifest, checksum, PATH, and skill state;
- uninstall that requires an explicit user shell action and never removes
  project-local skills;
- clear behavior when a release manifest is unavailable.

Checkpoint P1:

- fresh install, update, failed update, rollback, doctor, and uninstall tests;
- no credentials, project files, or project-local skills are modified;
- interrupted installation leaves either the previous valid installation or a
  clearly recoverable state;
- release source and signing/checksum failures are visible to the user.

### Phase 2 — Project migration and adoption

Make initialization safe for existing and older projects.

Commands:

```text
leadfox project check PATH
leadfox project adopt PATH
leadfox project migrate PATH
```

Deliverables:

- migration from each supported project schema to the current schema;
- creation of missing managed files such as `control/decisions.yaml`;
- preservation of user-authored files, skills, symlinks, and comments where
  possible;
- dry-run output and a machine-readable migration report;
- a migration manifest recording created and changed managed paths;
- refusal to overwrite modified managed files without explicit user action.

Checkpoint P2:

- old, current, incomplete, and partially adopted fixtures are covered;
- migration is idempotent;
- no deletion or external access occurs;
- `project check` distinguishes invalid structure from incomplete planning.

### Phase 3 — Unified diagnostics

Provide one read-only diagnostic entry point for users and Control.

Commands:

```text
leadfox doctor
leadfox project doctor [PATH]
```

Diagnostics should cover:

- Leadfox installation and skill versions;
- project schema and required files;
- intake and change-plan validation;
- decision policy and decision ledger;
- capabilities and job references;
- job readiness and incomplete evidence;
- stale or missing skills;
- gate configuration and assurance references;
- operations checkout and stray state/plan/artifact files.

Checkpoint P3:

- diagnostics never execute gates, contact external systems, or modify files;
- findings have stable codes, severity, and remediation text;
- Markdown and JSON output are equivalent;
- exit codes distinguish healthy, findings, and processing errors.

### Phase 4 — Durable operations safety

Promote the lti-demo lessons into generic Leadfox controls.

Deliverables:

- registration of a persistent operations checkout;
- refusal or explicit escalation when cloud Terraform runs elsewhere;
- detection of state, plan, credentials, provider binaries, and retained
  artifacts in disposable worktrees;
- saved-plan hash verification before apply;
- before/after state hash and inventory recording;
- artifact retention outside a disposable worktree before cleanup;
- cleanup refusal when declared durable artifacts were not retained;
- recovery records for failed or partial operations.

Checkpoint P4:

- offline tests cover stray state, wrong checkout, missing artifact retention,
  mismatched plan hashes, and partial cleanup;
- no cleanup operation can delete a registered operations checkout;
- state and plan contents are never printed in diagnostics;
- a failed operation produces a resumable, reviewable record.

### Phase 5 — Job and evidence completion

Finish the job-control layer around the new inspection commands.

Deliverables:

- job readiness validation before start;
- bounded context export for agent hosts;
- handoff schema and validation;
- required gate, acceptance-criterion, attestation, and external-evidence
  completeness checks;
- evidence redaction and reference/hash validation;
- clear distinction between diagnostic evidence and acceptance evidence;
- review and approval records linked to the exact reviewed commit.

Checkpoint P5:

- a job cannot be accepted with missing required evidence;
- a job cannot start with unsafe scope or unavailable declared controls;
- context export contains no credentials, secret values, or unrestricted
  project dumps;
- intentionally incomplete fixtures fail with stable diagnostics.

### Phase 6 — Capability health and change impact

Make capabilities and living project changes useful to Control without
pretending Leadfox can test provider credentials itself.

Deliverables:

- capability declaration/reference health;
- optional host-supplied capability attestations with provider, version,
  access mode, and expiry;
- no credential capture by Leadfox;
- change-impact reports covering services, integrations, data, environments,
  requirements, decisions, jobs, security, cost, migration, and deployment;
- policy-sensitive signals that require review, not automatic approval;
- baseline and supersession checks for living change documents.

Checkpoint P6:

- missing, stale, duplicated, and unreferenced capabilities are diagnosed;
- host availability remains explicitly separate from project declaration;
- change-impact output is deterministic and machine-readable;
- impact signals cannot silently approve or execute a change.

### Phase 7 — Release-quality documentation and regression matrix

Make the pre-Herdr core usable by developers, project managers, Control, and
future agent hosts.

Deliverables:

- quick start for installation, initialization, intake, status, context, and
  job workflows;
- detailed user and maintainer manuals;
- command/schema reference generated or checked against the CLI;
- migration guide and recovery runbook;
- fixture matrix covering fresh, old, incomplete, migrated, and interrupted
  projects;
- security regression tests for secret exposure, unsafe paths, state handling,
  and output redaction.

Checkpoint P7:

- a new user can install Leadfox, initialize a project, use the planning skill,
  validate intake, inspect status, and export Control context;
- a Control agent can diagnose a blocked or incomplete project without asking
  for information already present in project records;
- the full test matrix passes offline;
- release artifacts are reproducible and verifiable.

## Herdr entry criteria

Herdr should not be integrated until P0–P7 are complete, unless a separate
experiment is explicitly isolated from the supported Leadfox workflow.

The first Herdr adapter should be narrow:

- Leadfox creates or selects a ready job;
- Leadfox exports bounded job context;
- Herdr starts one or more agents using host-provided integrations;
- Herdr returns process status and output references;
- Leadfox validates scope, gates, handoff, evidence, and approval;
- Leadfox, not Herdr, decides whether the job may advance.

The adapter must support a dry-run mode, a no-Herdr fallback, explicit model
selection, cancellation, timeout, and a complete execution record. Herdr must
not receive project credentials unless the host explicitly supplies them under
the project capability policy.

## Non-goals

This plan does not:

- make Leadfox an autonomous deployment system;
- add cloud-provider credentials or provider-specific control logic;
- require Herdr for normal Leadfox operation;
- make heuristic impact signals equivalent to approval;
- replace project-specific gates, risk controls, or developer decisions;
- claim compliance certification.

## Acceptance record

Each phase should have a local review record containing the changed paths,
tests, compatibility fixtures, known deviations, and the decision to proceed.
No phase authorizes a remote push, external network access, cloud operation,
or Herdr execution by itself.

## Change history

| Version | Date | Author | Change | Reason |
|---|---|---|---|---|
| 1 | 2026-11-03 | Leadfox maintainers | Initial pre-Herdr hardening plan | Consolidate retained functionality and lti-demo lessons before Herdr |
| 2 | 2026-11-03 | Leadfox maintainers | Recorded implemented foundations and remaining release verification | Align the plan with the current code without claiming full P0–P7 completion |
