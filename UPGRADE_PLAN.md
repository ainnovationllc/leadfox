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
- Record external CI run IDs, attempts, conclusions, and artifact references.
- Distinguish diagnostic reruns from evidence that satisfies approval.

## Phase 4 — distribution and execution

- Publish versioned Leadfox binaries or a reusable runner.
- Add platform identity and applicability to gate evidence.
- Add repeat-run support for soak and flake detection.

## Explicit boundary

Leadfox must not absorb project-specific assumptions such as Canvas, a
particular CI provider, macOS/Linux shell workarounds, or a project's agent
permission files. Those belong behind project or provider adapters.
