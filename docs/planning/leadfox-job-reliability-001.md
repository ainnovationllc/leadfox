# Leadfox job reliability and recovery plan

Status: proposed; planning only
Plan owner: Leadfox maintainers
Scope: general-purpose Leadfox job lifecycle and execution boundary
Priority: high; before Herdr integration

## Purpose

Ensure that a Leadfox job cannot disappear silently, remain falsely marked as
running, or lose recoverable work when an agent session ends, a worker exits,
or a host becomes unavailable.

Leadfox remains the authority for job scope, evidence, review, approval, and
promotion. An execution host such as a local process, Codex, Claude, or Herdr
may perform work, but it must report lifecycle facts back to Leadfox.

## Observed failure class

An agent session can end while a job is still being implemented. The current
CLI records the job as running until Control explicitly changes it, and the
current runtime boundary does not provide a durable worker, heartbeat,
watchdog, or notification channel. A temporary worktree can also disappear
before recovery.

The plan must address both classes of failure. A better prompt or project
convention is only an interim mitigation.

## Non-goals

- Do not make Herdr mandatory.
- Do not make Leadfox store model-provider credentials.
- Do not make Leadfox approve work because a worker exits successfully.
- Do not change project-specific job scope, gates, or approval policy.
- Do not automatically merge, push, deploy, or delete a preserved recovery
  branch.

## Implementation phases

### R0 — Recovery and persistent-worktree safety

- Document and implement a configured persistent job-worktree root.
- Reject volatile paths such as `/tmp`, `/private/tmp`, and equivalent host
  locations by default; permit an explicit override only with a recorded
  warning.
- Add a recovery workflow for cancelled or interrupted jobs that preserves
  the source branch, source commit, completed evidence, and remaining
  criteria.
- Make reissue output identify the recovery point and superseded job.
- Ensure cleanup never removes a preserved recovery branch.

Checkpoint R0:

- A job cannot start without a safe resolved worktree path.
- A simulated interrupted job leaves a usable branch and recovery record.
- A cancelled job cannot be mistaken for a completed job.
- Existing project fixtures and lifecycle records remain readable.

### R1 — Explicit execution states and progress

Add provider-neutral lifecycle states for execution interruptions, at minimum:

`ready`, `running`, `paused`, `blocked`, `worker_lost`, `timed_out`,
`review`, `accepted`, `merged`, and `cancelled`.

Add append-only progress records containing:

- job ID and runtime ID, when available;
- timestamp;
- latest branch and commit;
- worktree path;
- tests or gates run;
- remaining work;
- blocker or next action.

Checkpoint R1:

- Status output distinguishes worker loss, timeout, pause, blockage, and
  deliberate cancellation.
- Progress records are safe to inspect and contain no credentials or secret
  values.
- A job left running without progress is diagnosable from repository records.

### R2 — Runtime supervision and notifications

Extend the execution boundary without coupling core Leadfox to a provider:

- runtime registration with a stable execution ID;
- start, heartbeat, output-reference, handoff, exit, and disconnect events;
- stale-heartbeat detection with configurable thresholds;
- explicit `worker_lost` and `timed_out` transitions;
- local notification/status output for every terminal or exceptional event;
- optional notification adapters for Herdr or other hosts.

Notifications must identify the project, job, event, current state, latest
commit, recovery action, and whether owner attention is required. They must
not include secret values or unrestricted worker output.

Checkpoint R2:

- A simulated worker exit and stale heartbeat produce a visible event and a
  non-running terminal/interrupted state.
- A normal handoff produces a completion event.
- Duplicate, delayed, and out-of-order runtime events do not corrupt the job
  state.
- The no-runtime/manual workflow remains supported.

### R3 — Recovery command surface and migration

Provide deterministic commands or equivalent APIs for:

```text
leadfox job pause JOB-ID
leadfox job resume JOB-ID
leadfox job recover JOB-ID
leadfox job progress JOB-ID
leadfox job runtime JOB-ID
```

The exact names may change during implementation, but the capabilities must
exist. Existing cancelled/running records must remain readable and have a
clear migration or interpretation rule.

Checkpoint R3:

- Control can recover a partial implementation without manually reconstructing
  job metadata.
- Recovery does not imply approval, merge, push, deployment, or cleanup.
- The job status, branch, worktree, evidence, and next action are consistent
  after recovery.

## Required test matrix

The implementation must include deterministic tests for:

- worker exits before any commit;
- worker exits after partial commits;
- agent session ends while the job remains marked running;
- stale heartbeat;
- host restart or missing worktree;
- duplicate and out-of-order runtime events;
- cancellation during execution;
- recovery and reissue from a preserved branch;
- cleanup refusal for a preserved recovery branch;
- normal completion and handoff;
- manual/no-runtime operation;
- secret-free status, progress, and notification output.

## Acceptance criteria

- No started job can remain silently active after a known worker loss or
  timeout.
- Every interruption has a durable reason, timestamp, runtime context when
  available, and recovery instruction.
- Partial work remains recoverable from a recorded branch and commit.
- Volatile worktree use is rejected or explicitly recorded as an override.
- Notifications are emitted for start, stale progress, worker loss, timeout,
  handoff, blockage, cancellation, and recovery readiness.
- The existing scope, gate, evidence, approval, merge, and artifact-retention
  checks continue to pass.
- Herdr can later implement the runtime boundary without changing project
  metadata or Leadfox authority rules.

## Rollout order

Implement and validate R0 first, then R1, R2, and R3. Do not begin Herdr
integration until R0 and R1 are complete and the interruption test matrix
passes. R2 may initially use a local event log and terminal notification;
provider-specific delivery can follow.

## Required owner decisions

- Default persistent worktree root and whether explicit volatile-path
  overrides are allowed.
- Default heartbeat and stale-worker thresholds.
- Initial notification channel: local event log, terminal, or both.
- Whether interruption records are committed automatically or written to a
  separate runtime journal before Control commits them.
