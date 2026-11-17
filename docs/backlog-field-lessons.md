# Backlog: lessons from field use

Generic improvements found while running Leadfox-managed projects. They are
backlog items: none is implemented, and none changes Leadfox behaviour by
being listed here. Each records the problem, the proposed capability, and
where it was observed.

## 1. Require real-tool validation where stand-ins cannot prove behaviour

**Problem.** A job tested a script's interaction with an external tool only
through a stand-in (a stubbed command). Every stubbed case passed, and the
review accepted them. The first real run failed at once, because the real
tool, in the configuration actually installed, did not support an option
the script relied on. The failure was safe (fail-closed), but it cost a
second job and an extra approval round.

**Generalization.** Stubs prove control flow, argument handling, and
refusals. They cannot prove a capability of a driver, exporter, provider,
CLI flag, platform service, or runtime.

**Proposed capability:**
- **Packets:** declare **external capability dependencies** (tool, the
  feature relied on, the environment where it runs).
- **Validation:** for each dependency, the packet requires at least one
  check against the real tool, offline where possible (for example a dead
  local address instead of a remote registry, or a minimal artifact instead
  of the full build).
- **Handoff:** a required section, "proved only by the real environment",
  listing what remains unproven and how the first real run fails closed.
- **Review:** checks that each declared dependency has real-tool evidence,
  or an explicit, developer-accepted residual risk with a recovery path. A
  stub-only result for a declared dependency is not accepted silently.
- **Evidence:** the real tool's identity and version (and its driver or
  mode) are recorded with the gate evidence.

**Observed:**
- a container image push mode (`push-by-digest`) stubbed in a gate passed;
  on a developer machine it failed, because the local build driver did not
  support it;
- the follow-up job added real-driver tests (an offline build plus a push to
  a closed local port) and a driver preflight.

## 2. Support dedicated, persistent operations checkouts for cloud work

**Problem.** Cloud operations (plans, applies, a bootstrap with local
state, backend-initialized working directories, saved plans) were run in
the project's normal development checkout. The checkout's offline gates
then failed:
- one tried to reach the real remote backend that the operational
  initialization had configured;
- another (correctly) rejected the state file left in the source tree.

The code was green in a clean worktree. The failure came only from
operational state mixed into the development checkout.

**Generalization.** Operational cloud state has a different lifecycle from
job worktrees and from the development checkout:
- it must persist (a bootstrap's local state cannot be regenerated);
- it must never be cleaned up as disposable;
- it must stay out of the tree the gates validate.

**Proposed capability:**
- **A first-class *operations checkout*:** a dedicated, named worktree or
  clone, recorded in project settings, created by Leadfox. It is
  **persistent**: excluded from job-merge cleanup and from worktree pruning.
  Leadfox refuses to remove it without an explicit command and
  confirmation.
- **Containment:** operation records name the checkout they ran in. Cloud
  plan and apply commands (or operation records) are refused, or at least
  warned about, when run from the development checkout or a job worktree.
- **Gate hygiene:** gates can report operational artifacts found in a
  development checkout (state files, backend-initialized directories, saved
  plans) as a distinct, actionable finding ("operational state in
  development checkout"), separate from code failures.
- **Integrity:** for local state kept in the operations checkout, Leadfox
  records its hash and mode, verifies them before each operation, and
  records them in the evidence after each operation.
- **Documentation:** the user guide explains when to create it, what lives
  there (local state, saved plans, backend `.terraform` directories,
  operation logs), and that cloud work never runs elsewhere.

**Observed:**
- a cloud capability pilot ran a state-bucket bootstrap and environment
  initialization in the development checkout; the offline infrastructure
  gate then failed there;
- the developer approved moving the operational state to a dedicated
  operations worktree, with its hash preserved, and keeping that worktree
  permanently.

## 3. Security-sensitive jobs prove both denial and success from a verified clean state

**Problem.** Privilege and access-control work is easy to "prove" wrongly:
- A list of forbidden actions that all fail looks like evidence. But if the
  login or setup step failed first, every negative check fails trivially,
  and the run reports success.
- A mutation (a deliberately weakened privilege) that "is caught" proves
  nothing if the baseline was already broken, or if the mutation hit a
  comment instead of the statement.

**Observed:** in one design check, a failed setup made nine "denied"
results and six "caught" mutations meaningless. Only added preconditions
exposed it.

**Generalization.** For any job whose purpose is to restrict or grant
access (database roles, IAM, network policy, secrets, permissions), the
evidence must show **both directions**, each from a **verified clean
state**.

**Proposed capability:** a packet section, *security evidence*, which
Leadfox validates and the review reproduces:
1. **Positive controls first.** The intended identity can authenticate, and
   its intended actions succeed (for example the real application
   migrations and data operations), in an isolated, disposable
   environment.
2. **Forbidden actions,** listed explicitly. Each must fail, and each check
   runs only after its positive control passed. A failed positive control
   marks every dependent negative result as invalid, not as passed.
3. **Weakened-privilege mutations,** listed explicitly. Each must make the
   verification fail. Each starts from a baseline that verification has
   confirmed clean, and is restored and re-verified afterwards. A dirty
   baseline marks the mutation result invalid.
4. **Mutations target real statements:** assignments or grants, not
   comments or documentation. The harness should match statements, not
   free text.
5. **Reproduced by review:** the reviewer reruns the security evidence on
   the reviewed commit and records the transcript (identities, booleans,
   denials; never secrets). Agent-reported results alone do not satisfy
   it.
6. **Later decisions** that only the real environment can settle (for
   example the provider's actual role attributes, or API behaviour) are
   listed explicitly. They stay open until checked there, with a stop on
   any difference.

**Observed (continued):** a least-privilege database design for a managed
PostgreSQL service. A throwaway container with the real migrations showed
that:
- the application's migrations and data operations succeed;
- nine forbidden actions fail after a positive login control;
- each weakened privilege is caught from a clean baseline.

The same check found three design facts that a stub could not:
- role attributes are not inherited without `SET ROLE`;
- a setup role lacks ADMIN OPTION on provider-created users;
- `ALTER ROLE … NOSUPERUSER` requires superuser.

## Related items already recorded by projects

- Retained job artifacts copied out of disposable worktrees before merge
  cleanup.
- Approval bound to an exact saved plan hash for apply operations.
- External evidence records for CI and cloud operations.
- Approval envelopes to reduce decision fatigue.

## 4. Validate changes to CI-run checks before the main branch

**Problem.** A check added to a CI job depended on repository history that the
CI checkout did not fetch. Local and review worktrees had full history, so all
offline validation passed; the first push to the main branch failed
deterministically.

**Proposed capability:**
- classify checks that execute in CI;
- require a temporary-branch validation before a change to such a check can
  be treated as main-ready;
- require both a normal green run and a controlled failure proving that the
  failure is reported and artifacts remain safe;
- record checkout depth and other CI assumptions as evidence;
- prevent a diagnostic rerun from satisfying the main-branch acceptance
  requirement.

The rule is provider-neutral: the CI adapter may use a branch, review
environment, or equivalent isolated validation target.

## 5. Treat flaky required tests as release blockers

**Problem.** A timing-dependent test intermittently failed in a required gate.
Rerunning it was useful diagnosis, but a green rerun did not establish that
the gate was reliable.

**Proposed capability:**
- distinguish a diagnostic rerun from a fresh acceptance run;
- allow a project risk control to require repeated runs, injected delays, or
  load conditions for timing-sensitive tests;
- record the suspected cause and the reproduction conditions;
- block promotion or deployment while a known flake remains in a required
  gate, unless the project explicitly records an accepted exception.

## 6. Enforce offline execution for job agents instead of per-command prefixes

**Problem.** A project required job agents to run offline, with no network
and no cloud tools. The rule was applied by prefixing each command with
proxy variables. A shell quoting mistake in one inline command split it, and
a fragment ran a real cloud tool (a database proxy on `PATH`) without the
prefix. It started, loaded the developer's credentials, and exited on a
malformed argument. No connection was possible, but whether the credential
setup contacted the provider could not be proven afterwards. Audit logs do
not record token refreshes, and the OS sandbox log did not show denials.

**Proposed capability:**
- run job agents' commands through an enforced execution wrapper, not a
  convention. The wrapper:
  - applies an OS-level network deny (local addresses and sockets only);
  - removes cloud SDK directories from `PATH`, exposing named tools only
    when a packet allows them;
  - denies execution of named high-risk binaries, which can still be read
    and hashed;
- prove the wrapper with negative controls before use: a refused outbound
  connection to a reserved test address, a denied exec of a dummy binary of
  the blocked name, and a positive control showing normal commands and exit
  codes pass through;
- record, per job, which wrapper ran and which tools it exposed, and treat
  a command run outside it as an incident;
- since denial logging may be unavailable, record that limit, and do not
  claim "no external access" from an empty log.

**Second case (same project, same day).** The briefing let agents run
"read-only inspection" outside the wrapper. An agent ran `terraform
version` as an inspection, and Terraform's default update check contacted
the vendor's checkpoint service. Only the anonymous product, version,
platform, and signature data was sent. Lessons:
- there is no safe "inspection" exception, because version and help
  commands of common tools phone home by default;
- route every agent shell command through the wrapper;
- set the vendors' opt-out variables (for example `CHECKPOINT_DISABLE=1`)
  at the user level as a second layer;
- detect contact from tool cache files (for example a rewritten
  `~/.terraform.d/checkpoint_cache`).

## 7. Onboard a new Control agent with a takeover briefing (nice to have)

**Priority.** Nice to have. Control can already be handed over by pointing a
new agent at `AGENTS.md`, `leadfox context export` and the control files;
this item makes the handover faster, more consistent, and checkable.

**Problem.** A project changed its Control agent mid-stream. Everything
Control needs is in the repository, but a new agent had to be told by hand
what to read, in what order, and what was in flight. `leadfox context
export` lists every decision (metadata only) and every job as a flat list;
on a mature project (170+ decisions, 58 jobs, five change plans) that is
long, puts merged history next to live work, and omits the things a new
Control most needs:
- which change plans are active, held, or complete, and their hold points;
- which jobs are not merged, and what each needs next;
- which owner decisions are still open or deferred;
- recent intake revisions (the running story of the project);
- standing owner conventions that are not decisions (for example naming
  rules, manual-only test systems, wording rules for plan documents);
- notes from the outgoing Control (unrecorded verbal decisions,
  half-finished work, questions waiting on the owner).

**Proposed capability:**
- **`leadfox control brief [PATH] [--format markdown|json]`** (or
  `context export --onboard`): a read-only takeover briefing containing:
  - project, structure, intake and policy validity, and pinned Leadfox and
    skill versions compared with the running binary;
  - a reading order derived from the project (instructions, decision policy,
    roles, intake revision log, project model and architecture plan, change
    plans with workflow files, decision ledger, latest handoffs, gates, risk
    controls, contracts);
  - live work only: active and held change plans with hold points; jobs not
    merged or cancelled, each with `job status` blockers and next action;
    open or deferred decisions; latest gate failures and missing evidence;
  - the last N intake revision-log entries;
  - the latest Control handoff, if any;
  - merged history summarized as counts, with a pointer to the full export.
- **Owner conventions:** an optional `control/conventions.yaml` (or an intake
  section) for standing rules that are not decisions, included verbatim in
  the briefing.
- **Control handoff record:** `leadfox control handoff draft` creates
  `control/handoffs/CONTROL-<date>.md` from a template (in-flight work,
  unrecorded owner decisions, open questions, known inconsistencies) for the
  outgoing Control or the developer to complete; it refuses to overwrite.
- **Understanding check:** the briefing ends with a fixed first task for the
  new Control: report current phase, active and held change plans, every
  non-merged job, open owner decisions, gate gaps, and inconsistencies
  between intake, decisions, change plans and jobs, then stop. Optionally
  `leadfox control ack --by NAME` records the developer's acceptance of that
  report in the decision ledger (action `agent_assignment`), so the start of
  a Control's tenure is auditable.
- **Boundary:** the briefing and handoff are guidance and records; they
  grant no authority. The decision policy still governs what the new Control
  may do.

**Observed:**
- sapient-lms, 2026-11-13: the owner asked how to start a new Control agent
  on the existing project. The project validated cleanly, but the takeover
  needed a hand-written reading list, a manual note about three draft jobs
  and a renumbered change plan, and a list of owner conventions held only in
  the intake revision log and in the owner's habits.

## 8. Work out of the box with sandboxed agent hosts such as Codex (high priority for adoption)

**Priority.** High for adoption. A developer trying Leadfox with Codex meets
several unexplained failures before the first job runs, and is likely to give
up. Each failure is correct behaviour by Leadfox or by the host's sandbox;
the problem is that nothing tells the developer what is wrong or how to set
the host up.

**Problem.** Codex's default `workspace-write` sandbox lets the agent write
inside the project but keeps `.git` read-only, blocks writes outside the
project except temporary directories, and blocks network access unless
enabled. Leadfox, run by Control inside that sandbox, needs exactly those
things:
- **Baseline commit.** A newly initialized project has `git init` but no
  commit, and every file is untracked. `job start` refuses ("Control
  worktree is not clean"); Codex cannot create the first commit because
  `.git` is read-only, and reports only that the commit "cannot be created
  in this environment".
- **Lifecycle commits and worktrees.** `job start` runs `git worktree add`
  and commits a start record; review, merge and evidence steps also commit.
  All write to `.git`.
- **Worktree location.** Leadfox correctly requires job worktrees outside
  the Control checkout. With no configured location, Codex picked
  `/private/tmp` because the sandbox allows it. macOS clears that directory
  on reboot and after a few days without access, so a long-running job can
  lose its worktree and leave stale `.git/worktrees/` entries.
- **Integration gates.** Gates that start Docker Compose stacks or reach
  local services need network or Docker-socket access the sandbox denies.
- A project run by a different host (sapient-lms, run by Claude) never hit
  these issues, because that host could write to `.git` and the project
  used a persistent sibling folder (`~/sapient-lms-jobs/<job>`) by
  convention. Nothing in that project records either rule, so switching it
  to Codex would reproduce every failure except the baseline commit.

**Proposed capability:**
- **Baseline at init.** `leadfox init` offers to create the baseline commit
  (default yes, with a reviewed file list and a generated `.gitignore` that
  includes `.DS_Store` and local-state folders), so a project never starts
  without one. `project check` reports "no commits" and "untracked control
  files" as explicit findings with the exact command to fix them.
- **Configured worktree root.** A project setting (for example
  `control/settings/worktrees.yaml: root: ../<project>-jobs`) that
  `job start` uses by default, resolved outside the checkout and refusing
  known-volatile locations (`/tmp`, `/private/tmp`, `/var/folders`) unless
  explicitly overridden. The job packet records the resolved path.
  *(Delivered in job reliability R0: `control/settings/worktrees.yaml`,
  volatile-path refusal with a recorded override; see the user guide,
  "Job reliability and recovery".)*
- **Host readiness check.** `leadfox doctor --host` (or `capability check
  host`) probes, without changing anything, whether the current process can:
  write to `.git` (create and remove a test ref in a scratch namespace),
  write to the worktree root, run `git worktree add` on a throwaway branch
  and remove it, reach the Docker socket, and open network connections when
  a gate needs them. It prints a pass/fail table and, for each failure, the
  host-specific remedy.
- **Host profiles in the documentation.** A short setup page per supported
  host. For Codex: run with `--sandbox workspace-write --ask-for-approval
  on-request`; add the worktree root with `--add-dir` or
  `sandbox_workspace_write.writable_roots`; approve git and `leadfox job`
  lifecycle commands per command, or let the developer run them; decide who
  runs Docker-based gates. Equivalent notes for Claude Code's sandbox.
- **Clear errors.** When a Leadfox git operation fails with a permission
  error, name the likely cause ("the agent host is not allowed to write to
  `.git`") and point to the host profile, instead of surfacing only Git's
  message.
- **Policy template.** The generated `control/decision-policy.yaml` and
  intake include explicit entries for local commits and lifecycle commits,
  the worktree root, and who runs network or Docker gates, so a project
  states these rules rather than inheriting them from whichever host ran it
  first.
- **Takeover link.** The Control takeover briefing (item 7) includes the
  host readiness result, so a new Control on a different host learns of
  these limits before its first job.
- **Paths.** Expand `~` in `worktree:` packet fields and `--worktree`, or
  refuse it with a clear message. Today `~/x` resolves inside the checkout
  and is rejected as "inside the Control checkout", which is confusing.
  *(Delivered in job reliability R0: `~` and `~/…` are expanded in packet
  paths, `--worktree` and the configured root.)*
- **Documentation example.** The user guide's `job start` example used
  `/tmp/project-job-001`, which steered a Codex Control to `/private/tmp`.
  (Corrected in the user guide; keep examples on a persistent folder.)
- **Interim warning (done, documentation only).** README and user guide now
  warn that Codex is not currently recommended as a Control agent, because
  the number of approvals forces the developer to monitor every job. Remove
  the warning when the host profile and readiness check make Codex usable
  without per-step approvals. A runtime warning (for example when
  `project check` or `job start` detects a read-only `.git`) is a follow-up.

**Observed:**
- paper-crane, 2026-11-13/14, Control on Codex: `job start` refused an
  untracked, commit-less checkout; Codex could not create the baseline
  commit; after the developer committed it by hand, Codex chose
  `/private/tmp` for the first job worktree; a packet path written with `~`
  was rejected; Control's own edits left the checkout unclean; with the path
  fixed, `job start` failed with `cannot lock ref 'refs/heads/job/job-001'`.
  The developer concluded that the approvals required make Codex unsuitable
  as Control for now.
- sapient-lms: the same project under Codex would need `.git` approvals,
  an added writable folder for `~/sapient-lms-jobs`, and a decision on who
  runs its Docker-based integration gates.

## 9. Clean up branches of merged and cancelled jobs (low priority)

`job merge` keeps the job branch on purpose: the review commit and the
history stay inspectable, and `--from-branch` reissues depend on it. Over a
project's life the branches accumulate (paper-crane had 32 `job/*`
branches after JOB-032), which clutters `git branch` and editor branch
pickers.

**Proposed:**
- `leadfox job prune [--merged] [--cancelled] [--older-than 30d] [--yes]`
  lists the branches it would delete and deletes them only with `--yes`.
- Merged branches are deleted only when the merge commit on the base branch
  contains the branch head (`git merge-base --is-ancestor`).
- Cancelled branches with partial work are skipped unless a recovery ref
  (`refs/leadfox/recovery/<job>/…`) preserves their head; the output names
  the ref that keeps the work reachable.
- The job record keeps the branch name and head commit, so `job status`
  still explains what was merged after the branch is gone.
- Superseded jobs follow the same rules as cancelled ones.

**Observed:** paper-crane JOB-032 trial, 2026-11-17.

*(Delivered after 0.3.0-rc.1 as `leadfox job prune`. Instead of skipping
cancelled branches without a recovery ref, prune first preserves their head
as a new recovery ref, so every merged or cancelled branch can be pruned
without making work unreachable.)*
