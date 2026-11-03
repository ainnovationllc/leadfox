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
