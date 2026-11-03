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

## Phase 5 — assurance evidence (implemented)

- Add a project-controlled `control/assurance.yaml` model for frameworks,
  controls, owners and evidence references.
- Add `leadfox assurance report` in Markdown or JSON form.
- Check local evidence existence and optional SHA-256 integrity, while clearly
  distinguishing missing, present, verified, mismatched and external evidence.
- State explicitly that Leadfox reports evidence coverage and traceability; it
  does not certify SOC 2 or replace an independent auditor.

The model is framework-neutral. SOC 2 mappings may be declared by a consuming
project, but SOC 2 control language and compliance decisions are not embedded
in Leadfox.

## Phase 6 — secret scanning (implemented)

- Add project-controlled `control/security.yaml` scanner configuration.
- Add `leadfox security scan` with pinned scanner-version enforcement and
  history or working-tree modes.
- Support Gitleaks as the deterministic offline gate adapter.
- Permit TruffleHog only as a non-verifying local scan; active verification is
  an explicitly external evidence operation because it may call credential
  provider APIs.
- Redact scanner report fields that can contain secret material before reports
  are retained.
- Keep scanner artifacts and findings separate from compliance certification;
  findings still require remediation, rotation and auditor review.

## Phase 7 — agent capabilities (implemented)

- Add a provider-neutral `control/agent-capabilities.yaml` registry for
  capabilities supplied by an agent host, including MCP servers.
- Allow job packets to declare required capability IDs with `--capability`.
- Validate capability definitions and job requirements before a job starts or
  is reviewed.
- Provide `leadfox capability list` and `leadfox capability validate` for
  deterministic inspection.
- Record access, authentication, network, output-path and fallback constraints
  without storing provider credentials.

Leadfox records and validates capability requirements; the selected agent host
is responsible for connecting to the provider, authenticating, and enforcing
the host-side tool permissions.

## Phase 8 — project decision policy (implemented)

- Generate a project-owned `control/decision-policy.yaml` during `leadfox init`.
- Define automatic actions, Control decisions requiring a record, actions
  requiring explicit developer approval, and escalation triggers.
- Validate the policy with `leadfox policy validate` and inspect it with
  `leadfox policy show`.
- Instruct project Control Agents to consult the policy and avoid repeating
  questions already answered by an approved default.

Leadfox provides policy structure and validation. It does not infer authority
for external, destructive, production, credential, or compliance actions.

## Phase 9 — risk-aware governance (foundation implemented)

- Add a project-owned `control/risk-controls.yaml` registry rather than
  hard-coding cloud, database, security, or compliance assumptions in Leadfox.
- Allow job packets to declare risk-control IDs with repeated `--risk` flags.
- Validate risk-control definitions when referenced, and validate them again
  before a job starts or is reviewed.
- Provide `leadfox risk validate` and `leadfox risk list`.
- Carry project-defined evidence requirements and stop conditions into the
  generated job instructions without pretending that Leadfox can interpret
  domain-specific evidence.

The foundation is intentionally declarative. It supports projects that need
stronger review for external-tool behavior, security-sensitive changes,
persistent operations, CI/workflow changes, destructive actions, or other
risks, while leaving the actual controls and approval authority to the
project.

## Phase 10 — risk enforcement and durable operations (foundation implemented)

- Validate declared evidence types and distinguish positive controls,
  forbidden-action checks, clean-baseline mutations, real-tool evidence,
  diagnostic reruns, and accepted residual risk.
- Require real-tool evidence when a packet declares an external capability
  dependency; record tool version, driver/mode, and what remains unproven.
- Add persistent operations checkouts that are recorded, protected from job
  cleanup, and used for stateful external operations.
- Detect operational state in development worktrees and report it separately
  from code or gate failures.
- Retain declared artifacts outside disposable worktrees, hash them before
  cleanup, and refuse cleanup when retention is incomplete.
- Add CI-validation requirements for changes to CI-run checks, including
  temporary-branch green and deliberate-red validation before main.
- Add policy-driven approval envelopes so routine decisions are not repeatedly
  requested while external, destructive, security, cost, production, and
  uncertain actions still escalate.

Implemented in the current lifecycle upgrade: operations checkouts are
registered and protected, operational paths are diagnosed, saved plans can be
hash-verified, declared artifacts are retained outside disposable worktrees,
and cleanup or merge is refused when required retention is incomplete.

The following remain planned follow-up work: richer evidence-type semantics,
mandatory real-tool evidence for declared external capabilities, before/after
state and inventory records, CI-validation policy enforcement, and
policy-driven approval envelopes. These require additional compatibility and
project-policy design before they become defaults.

## Explicit boundary

Leadfox must not absorb project-specific assumptions such as Canvas, a
particular CI provider, macOS/Linux shell workarounds, or a project's agent
permission files. Those belong behind project or provider adapters.
