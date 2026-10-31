# Leadfox Upgrade Plan

This roadmap keeps Leadfox project-agnostic. Project-specific harnesses,
platform adapters, and agent instructions remain in the consuming project.

## Phase 1 — lifecycle correctness (implemented)

- Reject command options that a job subcommand does not support.
- Make `job scope --commit` evaluate the requested commit and identify it in
  the result.
- Add regression tests that prove an ignored option or wrong scope input fails.

Implemented in commit `21c49b9`.

## Phase 2 — long-running jobs (implemented)

- Add an explicit synchronization operation for bringing a job up to date with
  its base branch.
- Record the synchronization source, merge base, and resulting job commit.
- Evaluate scope from the recorded merge base after synchronization.
- Add a supersede/reissue operation that preserves criteria, decisions,
  deviations, and source-job history without rewriting commits.
- Keep cancel-and-reissue as the safe fallback until synchronization is used.

Implemented in the current lifecycle upgrade. Synchronization records the
merge base for provenance and evaluates scope against the synchronized base
tip; reissue preserves criteria and links the superseded job without rewriting
history.

## Phase 3 — structured evidence

- Represent acceptance criteria and their evidence references in job metadata.
- Record external CI run IDs, attempts, conclusions, and artifact references with
  `leadfox job evidence`.
- Distinguish diagnostic reruns from evidence that satisfies approval.
- Require all declared criteria to be passed or waived before an accepted
  approval; jobs without criteria retain their previous behavior.

Implemented in the current lifecycle upgrade. External evidence is tied to the
reviewed commit, duplicate run attempts are rejected, and diagnostic evidence
is retained without satisfying a criterion.

## Phase 4 — distribution and execution

- Publish reproducible versioned binaries or a reusable runner.
- Add platform identity and applicability to gate evidence.
- Add repeat-run support for soak and flake detection.

Implemented in the current lifecycle upgrade:

- `make build` embeds a version and `make dist` builds Darwin and Linux
  binaries for arm64 and amd64.
- `leadfox version --json` reports the version, source commit, Go version and
  target platform.
- Gate results and evidence identify operating system, architecture, optional
  runner identity, and applicability.
- `leadfox gate run GATE --repeat N` produces an aggregate report with each
  run, pass/fail counts, and a `flaky` indicator. A nonzero result is returned
  if any repetition fails.

## Explicit boundary

Leadfox must not absorb project-specific assumptions such as Canvas, a
particular CI provider, macOS/Linux shell workarounds, or a project's agent
permission files. Those belong behind project or provider adapters.
