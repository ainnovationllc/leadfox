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

## Related items already recorded by projects

- Retained job artifacts copied out of disposable worktrees before merge
  cleanup.
- Approval bound to an exact saved plan hash for apply operations.
- External evidence records for CI and cloud operations.
- Approval envelopes to reduce decision fatigue.
