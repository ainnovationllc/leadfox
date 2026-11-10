# Leadfox continuous integration plan

**Plan ID:** LEADFOX-CI-001
**Version:** 1
**Status:** implementation-ready; local rehearsal passed
**Audience:** Leadfox maintainers and Control
**Scope:** read-only GitHub Actions validation for the Leadfox repository
**Current head:** `4083e0b`
**Created:** 2026-11-10

## Implementation status

The initial workflow implementation is present locally in
`.github/workflows/ci.yml`. It uses read-only permissions, pinned official
actions, and runs Go tests, Go vet, npm tests, and release-candidate fixture
validation. Local actionlint and command rehearsal passed on the uncommitted
implementation. Temporary-branch validation, main validation, and any future
release remain pending.

## Purpose

Add a small, reproducible CI workflow before Leadfox publishes a public
release. CI must validate the same implementation that was tested locally,
including the cross-platform release-candidate packaging path. A red run is a
release blocker, but this first version does not create GitHub branch
protection or a required status check.

## Current state

- The repository has no `.github/workflows/` directory.
- Local verification currently passes:
  - `go test ./...`;
  - `go vet ./...`;
  - `npm test` (7 tests);
  - `make release-candidate VERSION=0.2.0-rc.1`;
  - local installer, `doctor`, update, rollback, and PATH lifecycle tests.
- `main` is currently at `4083e0b` and matches the remote.
- The release-candidate target creates four platform archives, checksums, and
  a local installer fixture.

## Goals

1. Run deterministic validation on every pull request targeting `main`.
2. Run the same validation on pushes to `main`.
3. Support an explicit `workflow_dispatch` run.
4. Use read-only repository permissions and no project or cloud credentials.
5. Validate the release-candidate packaging and manifest without publishing
   artifacts.
6. Make failures visible in one job with clear logs and no silent fallback.
7. Keep CI independent of Herdr, model providers, GCP, Docker registries,
   GitHub secrets, and external deployment services.

## Non-goals

- Publishing npm packages or GitHub releases.
- Uploading release artifacts as a public release.
- Deploying Leadfox or any project.
- Branch protection, rulesets, or required status checks.
- Secret scanning policy changes.
- Herdr or multi-agent orchestration.
- Network-dependent project tests.

## Proposed workflow

Create `.github/workflows/ci.yml` with the following triggers:

```yaml
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  workflow_dispatch:
```

The workflow has one `quality` job on a pinned Ubuntu runner. It uses only
official, explicitly pinned GitHub actions and declares:

```yaml
permissions:
  contents: read
```

No secrets, write permissions, service-account credentials, registry login, or
third-party actions are allowed.

### Job steps

1. Check out the repository.
2. Set up the pinned Go version from the repository's supported version.
3. Set up Node using the repository's supported minimum/runtime version.
4. Run `go test ./...`.
5. Run `go vet ./...`.
6. Run `npm test`.
7. Build the CLI.
8. Run `make release-candidate VERSION=ci`.
9. Validate that exactly four archives, one manifest, and one checksum file
   were created.
10. Validate archive contents and checksums again from the generated fixture.
11. Upload only the generated summaries or fixture metadata as a short-lived
    artifact if useful; never upload credentials, caches containing secrets,
    or an unreviewed release.

The job must fail on any non-zero command, missing artifact, checksum mismatch,
unexpected archive entry, or malformed manifest.

## Version and dependency policy

- Pin action references to reviewed immutable versions or commit SHAs.
- Pin the Go toolchain version used by CI.
- Use the existing `go.sum` and npm lock/package metadata as applicable.
- Do not add a dependency solely to implement CI.
- Do not use floating `latest` actions.
- The `ci` packaging version is a test label only and must never be published.

## Security and data handling

- CI receives repository contents read access only.
- The workflow must not print environment variables, tokens, or local paths
  containing user data.
- No cloud APIs, registries, GitHub release APIs, or external model services
  are contacted.
- Any artifact upload must contain only build summaries, checksums, and
  redacted validation metadata.
- A future security workflow may add pinned secret scanning, but it is outside
  this plan.

## Acceptance criteria

### CI-1: workflow structure

The workflow is syntactically valid, uses the declared triggers, has read-only
permissions, and uses only approved pinned actions.

### CI-2: language and installer tests

Go tests, Go vet, npm tests, and the CLI build pass on a clean runner.

### CI-3: release packaging

The CI run creates all four platform archives, each containing exactly one
regular executable named `leadfox`.

### CI-4: manifest integrity

The manifest contains exactly one artifact per supported platform, every
checksum matches, and the source commit is the checked-out commit.

### CI-5: failure behavior

A temporary validation branch or pull request proves that an intentionally
broken test or checksum causes a red job and does not publish artifacts.

### CI-6: trigger coverage

One pull-request or branch validation, one push to `main`, and one manual
`workflow_dispatch` run are green.

### CI-7: no unintended access

The workflow logs and job permissions show no credentials, write access,
registry login, cloud operation, or unapproved network dependency.

## Implementation checkpoints

### Checkpoint 0 — workflow design review

- Review the final action versions, runner image, Go version, Node version,
  artifact retention, and permission block.
- Confirm the workflow does not publish or deploy.
- Confirm the local release target is deterministic on a clean checkout.

### Checkpoint 1 — local workflow rehearsal

- Run every workflow command locally in a clean checkout.
- Test a correct package and a deliberately corrupted checksum.
- Verify the corrupted case stops before any artifact upload.

### Checkpoint 2 — implementation review

- Add only `.github/workflows/ci.yml` and any narrowly scoped CI documentation
  or test helper required by the packet.
- Run actionlint if available.
- Run all local gates and inspect the diff for credentials and unrelated files.

### Checkpoint 3 — temporary branch validation

- Push a temporary `ci/leadfox-validation` branch only after approval.
- Require one green run.
- Create a deliberate failing revision that changes a test or checksum.
- Confirm the job is red and no release is published.
- Delete the temporary branch normally.

### Checkpoint 4 — main validation

- Audit the commits and run the local suite before pushing `main`.
- Push `main` as a fast-forward only.
- Confirm the automatic push run is green.
- Run and confirm a manual `workflow_dispatch` run.

### Checkpoint 5 — release readiness decision

- Review both main runs and their logs.
- Decide separately whether to create a GitHub prerelease.
- Do not enable branch protection or required status checks without a new
  explicit decision.

## Failure and rollback

- A red CI run blocks release work until corrected.
- Workflow changes are reverted by a normal reviewed commit.
- No cloud or external resources require cleanup because this workflow does
  not provision or publish anything.
- Failed temporary-branch validation is cleaned up by deleting only that
  branch and retaining the run record for review.

## Evidence to retain

For the implementation and validation records, retain:

- workflow commit SHA;
- action and tool versions;
- trigger and run identifiers;
- job conclusion and permissions;
- packaging manifest and checksum summary;
- deliberate failure result;
- branch cleanup result;
- confirmation that no release or deployment occurred.

This evidence demonstrates validation coverage only. It is not a compliance
certification or attestation.

## Decisions still required

1. Approve the workflow design and CI triggers.
2. Approve the exact pinned action and tool versions.
3. Approve temporary-branch validation.
4. Approve any later GitHub prerelease or public release separately.
