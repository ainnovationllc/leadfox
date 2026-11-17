# Leadfox job reliability: implementation plan

Status: approved; owner confirmed section 3 defaults on 2026-11-16
Source specification: `docs/planning/leadfox-job-reliability-001.md`
Control prompt: `docs/prompts/leadfox-job-reliability-control.md`
Priority: high; before Herdr integration and before other backlog work

This plan turns the reliability specification into ordered, independently
reviewable work units. It records the current behaviour, the design choices
that the specification leaves open, and the tests that prove each checkpoint.

## 1. Delivery method

The Leadfox repository is not itself a Leadfox-managed project (it has no
`control/` directory), so this work is delivered with normal Git commits
rather than Leadfox jobs. This replaces the Control prompt's "use bounded
jobs" rule for this refactor only; every other operating rule in the prompt
still applies.

- One branch per phase: `reliability/r0`, `reliability/r1`,
  `reliability/r2`, `reliability/r3`, each opened from an up-to-date `main`.
- One or more commits per work unit (REL-01 … REL-09). Each commit builds,
  passes `go test ./...`, `go vet ./...`, `gofmt -l .` (empty) and
  `npm test`, and changes only the paths listed for its work unit plus tests
  and documentation.
- A phase merges to `main` only after its checkpoint (section 6) passes and
  the owner has reviewed the phase. Merging, pushing, tagging and publishing
  require owner approval.
- The completion report required by the Control prompt is produced per
  checkpoint, with commit IDs in place of job IDs.

## 2. Current behaviour (baseline, `main` at `2384608`)

| Area | Current behaviour | Gap against the specification |
|---|---|---|
| States | `draft`, `ready`, `running`, `review`, `review-pending`, `review-failed`, `changes-requested`, `accepted`, `merged`, `blocked`, `cancelled` | No `paused`, `worker-lost`, `timed-out`; no table of allowed transitions — each command checks its own preconditions |
| Start | `job start` requires a worktree path outside the checkout (`ensureExternalWorktree`, `job_lifecycle.go:2455`) | No configured root; `/tmp` accepted; `~` not expanded and then rejected as "inside the checkout" |
| Cancel | `jobTerminate` (`job_lifecycle.go:2172`) records the cancel, then runs `git worktree remove --force` | **Uncommitted work in the job worktree is destroyed.** No recovery record |
| Reissue | `jobReissue` (`job_lifecycle.go:2272`) cancels the old job (same destructive cleanup) and creates a `draft` from the base branch | Partial work on the old branch is not carried forward; output does not name a recovery point |
| Block / resume | `block` keeps worktrees; `resume` only from `blocked` and only if the worktree exists | Cannot resume after worktree loss; no pause |
| Branches | No code deletes job branches | Correct, but not recorded or tested |
| Runtime | `AgentRuntime` / `ManualRuntime` (`agent_runtime.go`) are used only by tests | No heartbeat, progress, events, timeout or notification |
| Status | `job status` / `project status` report the packet's `status` field only | A dead worker leaves a job `running` indefinitely |
| Root discovery | `gitRoot(".")` returns `--show-toplevel` | Run from a job worktree, Leadfox resolves the worktree, not the Control checkout |

Tests that exist today for this area: `TestJobCanBeCancelledWithReason`,
`TestJobReissuePreservesCriteriaAndHistory`,
`TestLifecycleTransitionsPreserveHistory`, `TestManualRuntimeLifecycle`,
`TestManualRuntimeRejectsDuplicateJobs`. None covers worker loss, timeout,
missing worktrees, uncommitted work at cancel, or runtime events.

## 3. Design decisions

Decisions marked **(owner)** are the specification's required owner
decisions; the value shown is the proposed default. Work stops at the first
phase that depends on an unconfirmed owner decision.

### D1. Persistent worktree root (owner)

- New project setting `control/settings/worktrees.yaml`:

  ```yaml
  schema_version: 1
  root: ../<project>-jobs        # relative to the Control checkout, or absolute
  allow_volatile: false
  ```

- When `--worktree` and the packet's `worktree:` are both absent,
  `job start` uses `<root>/<job-id>`. An explicit path still wins but is
  subject to the same checks.
- `~` and `~/…` are expanded in `--worktree`, packet `worktree:` and `root:`.
- Volatile locations are rejected: `/tmp`, `/private/tmp`, `/var/tmp`,
  `/private/var/tmp`, `/var/folders`, `/private/var/folders`, `/dev/shm`,
  `$TMPDIR`, and `os.TempDir()`, compared after resolving symlinks.
- Override: `job start --allow-volatile-worktree --reason TEXT`, permitted
  only when `allow_volatile: true`. The packet records
  `worktree_override: {reason, actor, recorded_at}` and status output shows a
  warning for the life of the job.
- Projects without the settings file keep today's behaviour except for the
  volatile-path rejection, which applies everywhere (see D9).

### D2. Non-destructive cancel and recovery refs

- Before any worktree is removed (cancel, reissue, merge cleanup), Leadfox
  snapshots uncommitted tracked and untracked changes without modifying the
  worktree: build a tree with a temporary index (`GIT_INDEX_FILE`,
  `git add -A`, `git write-tree`), create a commit on top of the branch head
  with `git commit-tree`, and store it at
  `refs/leadfox/recovery/<job-id>/<n>`. Ignored files are not captured.
- The packet gains `recovery_points: [{ref, commit, branch, branch_head,
  reason, recorded_at, actor, worktree, dirty}]`.
- Branches and `refs/leadfox/recovery/*` are never deleted by Leadfox. A test
  asserts that every cleanup path leaves them in place.
- `--discard-uncommitted` skips the snapshot and is recorded as such.

### D3. Execution states and transitions

New states use the existing hyphen style: `paused`, `worker-lost`,
`timed-out`. The specification's `worker_lost` / `timed_out` are written
`worker-lost` / `timed-out`; existing states keep their names.

A single transition table in a new `job_states.go` replaces the scattered
precondition checks. Allowed transitions added by this work:

| From | To | Command / cause |
|---|---|---|
| `running` | `paused` | `job pause` (deliberate, worktree kept) |
| `running` | `worker-lost` | `job check` / runtime `exit` or `disconnect` event / stale threshold |
| `running` | `timed-out` | `job check` when the job's wall-clock limit is exceeded |
| `paused`, `blocked`, `worker-lost`, `timed-out` | `running` | `job resume` (worktree must exist) |
| `worker-lost`, `timed-out`, `cancelled` | `paused` | `job recover` (rebuilds worktree, records recovery point) |
| any non-terminal | `cancelled` | `job cancel` (now non-destructive, D2) |

`merged` and `cancelled` remain terminal for lifecycle purposes;
`cancelled → paused` via `recover` is the single exception, and it requires
the job's branch to exist and not to be superseded.

### D4. Runtime journal location (owner)

- Progress, heartbeats and runtime events are appended to a journal that is
  **not committed**: `<control>/.local/leadfox/runtime/<job-id>.jsonl`.
  `.local/` is already ignored by projects created with `leadfox init`;
  `project migrate` adds the ignore rule to older projects, and Leadfox
  refuses to write a journal that Git would track.
- State **changes** derived from the journal (`worker-lost`, `timed-out`,
  `paused`, recovery) are written to the job packet and committed through
  `commitLifecycleRecord`, as all lifecycle changes are today.
- Progress records are not approval, are never read by review or approval
  code, and never change criteria status.

### D5. Locating the Control checkout from a job worktree

A new `controlRoot(path)` resolves `git rev-parse --git-common-dir` and uses
its parent, so `leadfox job progress` and `leadfox job heartbeat` run from a
job worktree write to the Control checkout's journal. A worker whose sandbox
cannot write to the Control checkout fails with a message that names the
journal path and points to the user guide's "Agent host requirements". (This
is a known limitation for sandboxed hosts; see backlog item 8.)

### D6. Stale-worker detection without a daemon (owner)

Leadfox is a short-lived CLI, so staleness is evaluated when Leadfox runs:

- `job status`, `job list`, `project status` and `doctor` compute
  `last_heartbeat` age from the journal and report `stale` or
  `presumed-lost` **without writing anything**.
- `leadfox job check [JOB-ID | --all]` applies the thresholds and records the
  `worker-lost` / `timed-out` transitions, with a reason, the last heartbeat
  and the last known commit. Control runs it at the start of each session; a
  host or cron may run it on a schedule.
- Defaults in `control/settings/runtime.yaml` (per project):
  `heartbeat_stale_after: 15m`, `worker_lost_after: 60m`,
  `job_timeout: none`. A job packet may set `timeout:`.
- Jobs with no runtime registration and no heartbeat (manual workflow) are
  never marked `worker-lost` automatically; status shows "no runtime
  attached; last activity <commit time>".

### D7. Runtime event model

Each journal line is one JSON object:

```json
{"schema":1,"event_id":"<uuid>","execution_id":"<id>","seq":12,
 "job":"JOB-001","type":"heartbeat","at":"2026-11-16T20:00:00Z",
 "commit":"<sha>","data":{...}}
```

- Types: `register`, `start`, `heartbeat`, `progress`, `output-ref`,
  `handoff`, `exit`, `disconnect`, `notification`.
- `register` returns a stable `execution_id`; a job has at most one active
  execution. Re-registering supersedes the previous execution and records it.
- Duplicate `event_id`s are ignored. Events with `seq` at or below the last
  applied `seq` for that execution are recorded as `late` but do not change
  state. Events for a superseded execution never change state.
- `output-ref` stores a path or URL only, never output text. Free-text `data`
  fields are checked against the patterns used by `redactGateOutput`
  (`gate_runner.go:434`: bearer tokens, `password=`, `token=`, `secret=`),
  moved into a shared helper; a value that matches is rejected, not stored
  or redacted. Other `data` fields have fixed types (commit IDs, paths, gate
  IDs) and are validated against them.
- `ManualRuntime` gains the same events so tests drive them deterministically
  with an injected clock.

### D8. Notifications (owner)

Initial channels: the journal (`type: notification`) and a line on standard
error when the triggering command runs. Each notification contains: project,
job, event, current state, latest commit, recovery action, and
`attention_required: true|false`. No worker output, no environment values.
Emitted for: start, stale heartbeat, worker loss, timeout, handoff, block,
cancel, and recovery-ready. An adapter interface (`notifier`) is defined but
only the local implementation ships.

### D9. Compatibility

- Existing packets are read unchanged; all new fields are optional.
- A `running` packet with no journal is interpreted as "manual; no runtime
  attached" — never as lost.
- A `cancelled` packet without `recovery_points` is reported as
  "cancelled before recovery records existed; branch `<b>` may contain
  partial work" when the branch still exists.
- Volatile-path rejection applies only to new `job start` calls; existing
  running jobs in volatile locations get a status warning, not an error.
- The new settings files are created by `project migrate`, never required.

## 4. Work units

### Phase R0 — recovery and persistent-worktree safety

**REL-01 Worktree root and path safety** — D1.
Files: new `worktree_root.go`; `job_lifecycle.go` (`jobStart`,
`parseJobOptions`, `validateJobOptions`, help text); `project_migrate.go`
(settings file); `project_check.go` (report missing root / volatile jobs).
Tests: default root used; `~` expanded; each volatile location rejected,
including via symlink; override refused when not allowed; override recorded
when allowed; explicit path inside checkout still rejected.

**REL-02 Non-destructive cleanup and recovery records** — D2.
Files: new `job_recovery.go`; `job_lifecycle.go` (`jobTerminate`,
`jobMerge`, `jobReissue` cleanup paths).
Tests: cancel with uncommitted tracked and untracked changes creates a
recovery ref containing them; clean worktree records a recovery point
without a snapshot; `--discard-uncommitted` recorded; branch and recovery
refs survive cancel, reissue and merge cleanup; cancelled status output
names the recovery point and is distinct from `merged`.

**REL-03 Reissue from preserved work.**
`job reissue OLD NEW --from-branch` starts NEW from OLD's branch head (or a
named recovery point with `--from-recovery REF`). NEW keeps OLD's original
`scope_base_commit`, so the carried-over commits are scope-checked. Output
names the superseded job and the recovery point.
Files: `job_lifecycle.go` (`jobReissue`, `jobStart`), `job_recovery.go`.
Tests: reissue from a branch with partial commits; scope check covers the
carried-over commits; reissue without the flag keeps today's behaviour.

### Phase R1 — explicit states and progress

**REL-04 State model** — D3, D9.
Files: new `job_states.go`; every status check in `job_lifecycle.go`,
`job_workflow.go` (`nextJobAction`, status blockers), `status_context.go`.
Tests: table-driven test of every allowed and refused transition; legacy
packets (fixtures for each existing state) load and report correctly.

**REL-05 Progress records** — D4, D5.
`leadfox job progress JOB-ID [--remaining TEXT] [--next TEXT]
[--blocker TEXT] [--gate ID]...` appends a progress record with job,
execution ID if any, timestamp, branch, commit, worktree, gates run,
remaining work and next action. `leadfox job progress JOB-ID --show` lists
them.
Files: new `job_journal.go`, `job_lifecycle.go` (dispatch), `job_workflow.go`
(status shows latest progress).
Tests: record from Control checkout and from a job worktree; refused when the
journal would be tracked; secret-like values rejected; status shows the latest
record; review and approval ignore progress.

### Phase R2 — runtime supervision and notifications

**REL-06 Runtime events** — D7.
`leadfox job runtime register|heartbeat|event|show JOB-ID`; `ManualRuntime`
emits the same events.
Files: `job_journal.go`, `agent_runtime.go`, new `job_runtime.go`.
Tests: duplicate, delayed and out-of-order events; superseded execution;
`exit` before any commit; `exit` after partial commits; `handoff` produces a
completion event.

**REL-07 Detection and notifications** — D6, D8.
`leadfox job check [JOB-ID | --all]`; read-only staleness in `job status`,
`job list`, `project status`, `doctor`.
Files: `job_runtime.go`, new `notify.go`, `job_workflow.go`,
`status_context.go`, `doctor.go`.
Tests (injected clock): stale heartbeat → reported, then `job check` →
`worker-lost`; timeout → `timed-out`; manual job never auto-lost; every
notification type rendered and scanned for secrets.

### Phase R3 — recovery commands and migration

**REL-08 Pause, resume, recover.**
`job pause`, extended `job resume` (from `paused`, `blocked`, `worker-lost`,
`timed-out`), and `job recover JOB-ID [--worktree DIR]`, which checks the
branch, runs `git worktree prune`, recreates a missing worktree from the
branch (applying `applyWorktreeEnvironment`), records a recovery point,
lists remaining criteria and sets the job `paused`. Recovery never approves,
merges, pushes or deletes anything.
Files: `job_recovery.go`, `job_lifecycle.go`.
Tests: host restart / missing worktree recovered; cancelled job recovered
from its branch; recover refused for merged or superseded jobs; status,
branch, worktree, evidence and next action consistent after recovery.

**REL-09 Matrix completion and documentation.**
Fill any remaining rows of section 5, update `docs/user-guide.md` (job
lifecycle, recovery, agent host requirements), `UPGRADE_PLAN.md` (new phase),
`README.md` command list, and mark backlog item 8's worktree-root and `~`
items as delivered.

## 5. Interruption test matrix

All tests use temporary Git repositories, as existing lifecycle tests do,
and an injected clock; none sleeps or uses the network.

| Scenario (specification) | Test | Unit |
|---|---|---|
| Worker exits before any commit | `TestWorkerExitBeforeCommitIsRecoverable` | REL-06/08 |
| Worker exits after partial commits | `TestWorkerExitAfterPartialCommitsPreservesBranch` | REL-06/08 |
| Session ends, job still `running` | `TestRunningJobWithoutHeartbeatIsReportedStale` | REL-07 |
| Stale heartbeat | `TestStaleHeartbeatMarksWorkerLost` | REL-07 |
| Host restart / missing worktree | `TestRecoverRecreatesMissingWorktree` | REL-08 |
| Duplicate and out-of-order events | `TestRuntimeEventsIdempotentAndOrdered` | REL-06 |
| Cancellation during execution | `TestCancelSnapshotsUncommittedWork` | REL-02 |
| Recovery and reissue from preserved branch | `TestReissueFromBranchKeepsScopeBase` | REL-03 |
| Cleanup refuses to remove recovery branch | `TestCleanupNeverDeletesBranchesOrRecoveryRefs` | REL-02 |
| Normal completion and handoff | `TestHandoffEmitsCompletionEvent` | REL-06 |
| Manual / no-runtime operation | `TestManualJobNeverAutoMarkedLost` + existing lifecycle tests | REL-07 |
| Secret-free status, progress, notifications | `TestReliabilityOutputsContainNoSecrets` | REL-05/07 |
| Volatile worktree rejected or recorded | `TestVolatileWorktreeRejectedUnlessOverridden` | REL-01 |
| Legacy records readable | `TestLegacyPacketsLoadUnderNewStateModel` | REL-04 |

## 6. Checkpoints

**R0** — REL-01..03 merged. A job cannot start without a safe resolved
worktree; a simulated interrupted job leaves a usable branch and recovery
record; cancelled is distinct from completed; all existing tests pass
unchanged.

**R1** — REL-04..05 merged. Status distinguishes pause, blockage and
cancellation (worker loss and timeout become reachable in R2); progress
records are secret-free; a job left running without progress is diagnosable
from repository and journal records.

**R2** — REL-06..07 merged. Simulated exit and stale heartbeat produce a
visible event and a non-running state; handoff produces a completion event;
duplicate, delayed and out-of-order events do not corrupt state; manual
workflow unchanged.

**R3** — REL-08..09 merged. Control recovers partial work without
reconstructing metadata; recovery implies no approval or side effect; the
full matrix passes; on a fixture project with running, blocked, cancelled and
merged jobs, `leadfox job check --all` followed by `leadfox job status` for
each job shows no unexplained running jobs.

Herdr integration may begin after R1 per the specification, but this plan
recommends waiting for R2 so that Herdr implements the finished event model.

## 7. Stop conditions

Stop and ask the owner before:

- changing who may approve, merge, push or deploy, or letting any runtime
  event cause approval or merge;
- storing any credential, token, environment value or raw worker output;
- making a change that makes an existing packet, evidence file or project
  unreadable, or that requires a migration step for existing projects;
- deleting branches, refs or worktrees in a way not described in D2;
- adding a background process, network listener or third-party dependency;
- any owner decision in section 3 is rejected or changed after its phase
  has started.

## 8. Owner confirmations required

| Decision | Proposed default | Needed before |
|---|---|---|
| D1 worktree root | `../<project>-jobs`; volatile paths rejected; override only if `allow_volatile: true` | R0 |
| D4 journal | Uncommitted `.local/leadfox/runtime/`; Control commits state changes | R1 |
| D6 thresholds | stale 15m, worker-lost 60m, no default timeout; configurable per project | R2 |
| D8 notifications | Journal + standard error; adapters later | R2 |
| D3 naming | `worker-lost` / `timed-out` (hyphenated, matching existing states) | R1 |
| Branch policy | One branch per phase, merged after checkpoint review | R0 |
