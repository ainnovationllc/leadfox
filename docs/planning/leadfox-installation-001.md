# Leadfox installation and download plan

**Plan ID:** INSTALL-001  
**Version:** 1  
**Status:** draft  
**Audience:** developers and project managers installing Leadfox  
**Scope:** all user interaction before `leadfox init`  
**Implementation status:** Checkpoints 1–6 and the automated portion of
Checkpoint 7 complete locally; manual rehearsals, publication, and
signed-release validation remain deferred

## Checkpoint 0 contract

This section freezes the first vertical slice for implementation. Items marked
`proposed` are the current implementation baseline and remain subject to
developer review where they affect distribution or host integration.

### First-slice user flow

```text
npx leadfox install
leadfox version
leadfox init example-project
cd example-project
leadfox skill install leadfox-project-planning --scope project
start Claude or Codex from the project root
leadfox intake validate
```

The planning agent uses the installed skill to create or update:

```text
control/project-intake.md
```

The validator is a Leadfox capability, not part of the planning agent. The
agent may run it while iterating, but Control treats its result as the
deterministic structural check.

### Frozen first-slice contract

| Area | Contract |
|---|---|
| npm package | `leadfox` (proposed package name) |
| Initial platforms | macOS arm64/amd64 and Linux arm64/amd64, matching the current `make dist` targets |
| User install root | `~/.leadfox` |
| Stable executable | `~/.leadfox/bin/leadfox` |
| General planning skill | `leadfox-project-planning` |
| Project skill registration | `leadfox skill install leadfox-project-planning --scope project` |
| Intake path | `control/project-intake.md` |
| Intake validator | `leadfox intake validate` |
| Machine-readable validation | `leadfox intake validate --format json` |
| Validator success | exit `0`; intake is structurally ready for Control review |
| Validator finding | exit `1`; intake requires correction |
| Validator error | exit `2`; validator or input could not be processed |
| Project side effects | none during install; initialization only creates declared project files |
| External services | no cloud, Canvas, GitHub, registry, or MCP access during intake validation |

### Skill boundary

The planning skill:

- guides a human or agent through the intake fields;
- may suggest follow-up questions and optional skills;
- may invoke the validator during drafting;
- does not approve requirements or architecture;
- does not create jobs, credentials, infrastructure, or external effects.

The project validator:

- checks the intake schema and required fields;
- checks statuses, version metadata, and change history;
- rejects secret-like values and malformed content;
- emits human-readable and JSON diagnostics;
- does not judge architecture quality or approve the project.

### Checkpoint 0 acceptance evidence

Checkpoint 0 is complete when the repository contains reviewed documentation
for the command flow, supported platforms, installation root, skill boundary,
intake path, validator interface, exit codes, and no-side-effect boundary.

Implementation begins only after this contract is accepted. The package name,
release signing model, and host-specific skill discovery remain explicit
distribution decisions rather than hidden assumptions.

## Checkpoint 1 implementation result

**Status:** complete locally; publication and signed release validation remain
deferred.

Implemented in the repository:

- `package.json` with the proposed `leadfox` npm package and executable entry
  point;
- `npm/leadfox.js` for the `leadfox install` bootstrap command;
- `npm/install.js` for platform detection, release-manifest selection,
  HTTPS/local-fixture downloading, archive validation, SHA-256 verification,
  user-local installation, and installation metadata;
- `npm/leadfox.test.js` with platform, manifest, archive-install, and checksum
  failure tests.

Validated locally:

- `npm test`: 7 tests passed;
- `npm pack --dry-run`: package contents are limited to the bootstrap and
  package metadata, with the repository README included by npm;
- local HTTP fixture installation: passed;
- checksum mismatch stopped before installation: passed;
- unsupported platform and duplicate manifest artifact checks: passed;
- `go test ./...`: passed;
- `go vet ./...`: passed;
- whitespace validation: passed.

Not included yet:

- GitHub Release artifact generation;
- npm publication;
- signed release metadata;
- persistent PATH editing;
- update and rollback commands;
- project skill installation;
- `leadfox init` intake creation and `leadfox intake validate`.

## Checkpoint 2 implementation result

**Status:** complete locally; npm publication and signed release validation
remain deferred.

Implemented:

- `leadfox update --check`;
- `leadfox update --version VERSION`;
- `leadfox update --rollback`;
- `npx leadfox@latest update` through the same bootstrap entry point;
- `leadfox doctor` for installation, active-binary, checksum, version, and
  PATH diagnostics;
- `leadfox path add` and `leadfox path remove` with explicit confirmation,
  shell-aware configuration selection, idempotence, and a reversible marker;
- atomic active-version switching through a temporary symlink;
- retention of one previous verified version in installation metadata;
- end-to-end fixture coverage for update, rollback, doctor, PATH add/remove,
  idempotence, and checksum failure.

Validated locally:

- npm tests: 7 passed;
- update from a fixture release to a newer release: passed;
- rollback to the previous verified release: passed;
- doctor on the active installation: passed;
- PATH add/remove and repeated add: passed;
- Go tests and vet: passed;
- whitespace validation: passed.

Not included:

- npm publication or a real GitHub Release;
- signed release metadata;
- Windows PATH integration;
- project skill installation;
- `leadfox init` intake creation and validation.

## Checkpoint 4 implementation result

**Status:** initialization structure complete locally; deterministic intake
validation remains a follow-on implementation.

`leadfox init` now creates:

- `control/project-intake.md` with the lightweight living intake template;
- `control/skills.yaml` with the project-local Control skill checksum and an
  optional global planning-skill entry;
- `skills/leadfox-control-plan-review/SKILL.md` as a project-local, pinned
  Control review skill;
- existing Leadfox project structure and concise agent references updated to
  point to the intake and local Control skill.

The generated instructions do not embed the skill contents or planning
template contents. Initialization still refuses non-empty target directories
and supports `--dry-run` without writing files.

Validated locally:

- clean initialization creates the intake, manifest, project-local skill, and
  `AGENTS.md`/`CLAUDE.md`;
- the project skill checksum is recorded;
- dry-run creates no directory;
- non-empty directories are rejected without changing existing files;
- Go tests and vet passed.

The following remain separate:

- `leadfox intake validate`;
- automatic Claude/Codex launching;
- project-local skill migration for existing projects;
- the manual uninstall run test, deferred to the end of the milestone.

## Checkpoint 3A — permission checks and uninstall safety

**Status:** complete locally; manual uninstall against a real installation is
deferred to the final milestone checkpoint.

Before installation writes or downloads a release, it must verify that the
user can create or modify the intended `~/.leadfox` directory. Permission,
read-only filesystem, disk, malformed-installation, and unsafe-symlink failures
must stop with an explanation and no partial installation.

The uninstall interface will be explicit and user-driven:

```text
leadfox uninstall --dry-run
leadfox uninstall
leadfox uninstall --yes
```

It may remove only Leadfox-managed global files and an exact PATH entry that
Leadfox previously added. It must not remove or modify project-local skills,
project control files, `AGENTS.md`, `CLAUDE.md`, or project configuration.
Modified or unmanaged files are preserved and reported. Uninstall must never
follow unexpected symlinks or delete outside the verified installation root.

Agent safety boundary: no agent skill may execute, invoke, or simulate an
uninstall command. An agent may explain the command and display it for the
user, but the user must run it directly in their own shell. This applies even
when the agent has shell access.

## Checkpoint 3 implementation result

**Status:** complete locally; project-local Control skill registration remains
deferred to initialization.

Implemented in the Go CLI:

- `leadfox skill list`;
- `leadfox skill install leadfox-project-planning --global --yes`;
- `leadfox skill verify leadfox-project-planning --global`;
- `leadfox skill remove leadfox-project-planning --global --yes`;
- embedded distribution of the planning skill;
- canonical global installation under `~/.leadfox/skills`;
- JSON skill manifest with version, source, scope, checksum, timestamp, and
  installed paths;
- optional Claude and Codex exposure with `--agent auto|claude|codex`;
- refusal to overwrite or remove modified skill files;
- auto mode that does not create undeclared host directories.

Validated locally:

- install, verify, modification detection, and safe removal tests passed;
- auto host detection test passed;
- CLI install/list/verify rehearsal passed with a temporary home directory;
- `go test ./...` passed;
- `go vet ./...` passed.

Not included:

- project-local `leadfox-control-plan-review` skill;
- `control/skills.yaml` registration;
- automatic Claude or Codex launch;
- global npm publication or signed release verification.

## Checkpoint 3A implementation result

**Status:** complete locally; no uninstall was run against the real user
installation.

Implemented:

- npm installation permission checks before release download;
- safe rejection of an existing `.leadfox` symlink or non-directory;
- `leadfox uninstall --dry-run`;
- `leadfox uninstall --yes`;
- preservation of modified or unmanaged global files;
- removal limited to verified Leadfox application and skill paths;
- optional removal of the exact Leadfox PATH entry;
- protection against manifest paths escaping the installation root;
- preservation of project directories and project-local skills;
- explicit agent boundary: planning skills may display uninstall commands but
  must never execute them.

Validated locally:

- unsafe installation path rejection;
- global skill install, verification, modification detection, and safe removal;
- uninstall preserving a project-local skill;
- Go tests and vet passed;
- npm tests passed;
- whitespace validation passed.

## Checkpoint 5 implementation result

**Status:** complete locally; the validator is implemented and tested. Manual
uninstall remains deferred to the final milestone checkpoint as planned.

Implemented:

- `leadfox intake validate [PATH]` for the project intake document;
- human-readable findings by default and `--format json` diagnostics for
  Control and automation;
- stable exit codes: `0` valid, `1` validation findings, and `2` processing
  errors;
- required intake sections and decision fields, allowed project stages,
  numbered first-flow and first-capability items, completed service,
  integration, environment, and change-history rows;
- rejection of secret-like values, while allowing names and references;
- validation of generated blank intake files, completed intake content,
  missing required content, and secret-like content.

Validated locally:

- `go test ./...` passed;
- `go vet ./...` passed;
- npm tests passed (7 tests);
- formatting and whitespace checks passed.

The validator is deterministic and independent of the planning agent. The
planning skill may guide completion and invoke the validator, but it cannot
approve the intake or replace Control's later review. Change-plan validation,
automatic agent launch, and the real uninstall rehearsal remain separate work.

## Checkpoint 6 implementation result

**Status:** complete locally; change-plan validation is implemented and tested.

Implemented:

- `leadfox change validate PATH` for proposed updates to an existing project;
- metadata checks for stable change identifiers, version, status, approval,
  and referenced baseline versions;
- safe, project-relative baseline references for the project model and
  architecture plan, including existence checks;
- required sections for affected baseline, proposed added/changed/removed
  deltas, compatibility and impact, validation and jobs, open decisions, and
  change history;
- rejection of incomplete required fields, invalid lifecycle metadata, unsafe
  baseline paths, missing baseline files, and secret-like values;
- Markdown and JSON diagnostics with the same exit contract as intake
  validation.

Validated locally:

- a complete change plan validates against an existing project model and
  architecture plan;
- a plan with missing baseline files is rejected;
- `go test ./...` passed;
- `go vet ./...` passed;
- npm tests passed (7 tests);
- formatting and whitespace checks passed.

The validator checks structural completeness and baseline references only. It
does not approve the change, interpret architecture quality, alter the living
architecture plan, create jobs, or perform implementation or external work.

## Checkpoint 7 implementation result

**Status:** automated portion complete locally; manual installation and
uninstallation rehearsals are intentionally deferred until after this
checkpoint and before any push or publication.

Completed:

- README and user-guide documentation now cover intake and change-plan
  validation, JSON diagnostics, and the exit-code contract;
- automated final checks cover Go tests, Go vet, npm tests, a local binary
  build, formatting, and whitespace validation;
- the documented planning flow remains side-effect free: validation does not
  launch agents, alter plans, create jobs, contact external systems, or run
  uninstall operations.

Deferred by request:

- a real `npx` installation on a clean machine;
- real update, rollback, PATH, and doctor rehearsal;
- the manual global uninstall run, including confirmation that project-local
  skills remain untouched;
- release publication, signing, and remote-release verification.

## Purpose

Define a safe, simple, repeatable way for a user to obtain Leadfox, verify the
installation, update it, and place it on the user's PATH before initializing a
project.

This plan does not define project initialization, planning, Control policy, or
any cloud operation. Those begin only after installation when the user runs
`leadfox init`.

## Goals

- Make the first installation approachable for a developer who has only Node.js
  and a shell available.
- Use `npx` as the low-friction bootstrap path where practical.
- Install Leadfox into a user-owned location, without requiring administrator
  privileges.
- Keep the installed version visible, reproducible, and updateable.
- Offer PATH setup explicitly and safely.
- Support macOS and Linux first, with a clear Windows decision before release.
- Fail safely when package, platform, checksum, permission, or PATH setup
  checks fail.
- Keep installation separate from project initialization and external systems.

## Non-goals

- Creating a project or writing `control/` files.
- Creating `AGENTS.md`, `CLAUDE.md`, or project skills.
- Installing or selecting Claude, Codex, Herdr, MCP servers, or model
  credentials.
- Contacting GCP, Canvas, GitHub, registries, or project integrations.
- Modifying a project repository.
- Managing shell configuration without explicit user consent.

## Proposed user experience

### First use

```text
npx leadfox install
```

The bootstrap command should:

1. Confirm the package name and requested version.
2. Detect operating system, CPU architecture, shell, and executable format.
3. Resolve the Leadfox release appropriate for that platform.
4. Verify the package and binary integrity before installation.
5. Install the binary under a user-owned Leadfox directory.
6. Report the exact installed version and path.
7. Offer to add the executable directory to PATH.
8. Verify that a new shell can invoke `leadfox version`.
9. Stop before project initialization and show the next command:
   `leadfox init <project-name>`.

The command should support a non-interactive form for automation, but the
interactive form is the default.

### Updating through npx

The npm bootstrap should also provide a convenient update path for an existing
installation:

```text
npx leadfox@latest update
```

This command should:

1. Discover the existing user-local installation.
2. Show the currently installed version and the proposed latest version.
3. Ask for confirmation unless the user supplies an explicit non-interactive
   approval flag.
4. Download and verify the new release before changing the active binary.
5. Preserve the previous verified version for rollback.
6. Activate the new version only after it passes an executable/version check.
7. Report the result and any required PATH action.

An exact-version update should also be supported:

```text
npx leadfox@1.2.3 update --version 1.2.3
```

The npm package version is the bootstrap version. The installed binary version
must still be checked and reported separately. `@latest` must never mean
"replace immediately": it selects a candidate, which is then checksum- or
signature-verified before activation.

### Recommended installation layout

```text
~/.leadfox/
├── bin/
│   └── leadfox
├── releases/
│   └── <version>/<os>-<arch>/leadfox
├── current                  # symlink or version pointer where supported
├── manifest.json             # installed version and verification metadata
└── logs/                     # optional redacted installer diagnostics
```

The executable in `bin/` is the stable PATH entry. Versioned releases permit
rollback and make updates recoverable. The installer must not store tokens,
cloud credentials, MCP credentials, or project secrets in this directory.

The exact layout may be simplified for the first implementation, but the
following properties are required: user ownership, version visibility,
integrity metadata, and a recoverable update path.

## Installation modes

| Mode | Example | Intended use | Requirements |
|---|---|---|---|
| Bootstrap | `npx leadfox install` | First installation | Node.js/npm available; explicit confirmation before writes |
| Versioned bootstrap | `npx leadfox@1.2.3 install` | Reproducible setup | Exact package version recorded |
| Existing binary | `leadfox install --from <file>` | Air-gapped or internally distributed install | User supplies the artifact and verification data |
| Update | `leadfox self-update` | Update an existing install | Explicit version selection and rollback on failure |
| npx update | `npx leadfox@latest update` | Update without relying on PATH resolution | Existing installation discovered and verified before replacement |
| Inspect | `leadfox version`, `leadfox doctor` | Verify installation | Read-only; no project or network mutation |

Whether `leadfox self-update` is included in the first release is an open
decision. A documented `npx leadfox@<version> install --upgrade` fallback is
acceptable initially.

## Package and release model

The distribution package should contain a small platform-neutral bootstrap and
platform-specific Leadfox binaries or a verified mechanism to obtain them.

The release model must define:

- supported operating systems and architectures;
- package name and registry;
- version format;
- release artifact names;
- checksums and their source of truth;
- whether checksums are signed and how signatures are verified;
- how an unavailable platform is reported;
- how a user selects an exact version;
- how a user rolls back to a previous version.

### Release artifact naming

The four platform builds are distinct artifacts, not duplicate files. Use one
versioned archive per operating-system and architecture pair:

```text
leadfox_<version>_darwin_arm64.tar.gz
leadfox_<version>_darwin_amd64.tar.gz
leadfox_<version>_linux_arm64.tar.gz
leadfox_<version>_linux_amd64.tar.gz
```

Each archive contains one consistently named executable:

```text
leadfox
```

The GitHub Release also contains:

```text
checksums.txt
release-manifest.json
```

The manifest maps each archive to its version, operating system,
architecture, source commit, build metadata, download URL, and SHA-256. The
installer selects exactly one archive based on the detected platform and never
downloads the other three.

The installer must never treat a successful download as proof of authenticity
by itself. At minimum it must verify an exact pinned checksum for the selected
artifact. Signed release metadata is recommended for the generally available
release.

## PATH behavior

PATH setup must be opt-in and explain the exact change before making it.

Example prompt:

```text
Leadfox is installed at ~/.leadfox/bin/leadfox.
Add ~/.leadfox/bin to your PATH in ~/.zshrc? [y/N]
```

Rules:

- Detect the active shell instead of assuming `.zshrc`.
- Do not duplicate an existing PATH entry.
- Do not replace or rewrite unrelated shell configuration.
- Show the line before writing it.
- Keep a reversible record of the line Leadfox added.
- Explain that the current shell may need to be restarted or reloaded.
- If the user declines, print a one-time invocation path and manual setup
  instructions.
- In non-interactive mode, never modify PATH unless an explicit flag requests
  it.

Windows requires a separate design for PowerShell and the user PATH registry;
it should not be approximated by writing Unix shell files.

## Update behavior

Updates must be explicit. Leadfox should support both a native command and an
`npx` bootstrap path. The `npx` path is important when the existing binary is
not on PATH or is too old to understand the current update command.

Leadfox should support:

- `leadfox version` — installed version and installation path;
- `leadfox update --check` — report available versions without changing files;
- `leadfox update --version <version>` — install a selected version;
- `leadfox update --rollback` — restore the previous verified version;
- `npx leadfox@latest update` — discover and update the user-local install;
- `npx leadfox@<version> update --version <version>` — select an exact
  bootstrap and binary version;
- `leadfox doctor` — verify executable, manifest, checksum, permissions, and
  PATH guidance.

An update should download and verify the new version before changing the active
pointer. If activation fails, the previous version remains active. The update
must not modify project files or project-local Leadfox state.

## Permissions and safety

- No administrator or `sudo` access by default.
- Installation directories are created with user ownership and restrictive
  permissions appropriate to executables and metadata.
- Existing files are never overwritten silently.
- A conflicting binary, symlink, or unexpected file causes a stop and clear
  recovery instructions.
- Installer logs never include environment variables, credentials, tokens, or
  downloaded file contents.
- Network access is limited to the package/release sources required for the
  selected installation mode.
- The installer records the source URL, version, platform, checksum, and time,
  but not authentication material.

## Failure and recovery cases

The installer must give a specific error and leave the existing installation
unchanged when:

- Node.js or npm is unavailable;
- the requested package or platform is unavailable;
- the checksum does not match;
- a release signature is invalid, when signatures are enabled;
- the destination is not writable;
- a PATH file cannot be safely identified;
- an existing installation is malformed;
- the download is interrupted;
- the binary cannot execute on the detected platform.

After a failed update, the prior active version must remain usable. A partial
download must be removed or quarantined and must never become the active
binary.

## First-release acceptance criteria

1. A clean machine with supported Node.js can run the documented `npx` command.
2. The installer identifies the platform and installs a working Leadfox binary
   without administrator privileges.
3. The selected artifact's checksum is verified before activation.
4. The installer reports version, path, source, and verification result.
5. PATH modification is opt-in, shell-aware, idempotent, and reversible.
6. Declining PATH setup still leaves a usable documented invocation.
7. A failed or interrupted install leaves no active partial binary.
8. An update verifies the new binary before activation.
9. A failed update leaves the previous version active.
10. `npx leadfox@latest update` can update an installation that is not currently
    on PATH.
11. The npx update path is confirmation-based by default and supports an
    explicit non-interactive mode.
12. `leadfox version` and `leadfox doctor` are read-only and useful before
    `leadfox init`.
13. Installation and update do not create a project, contact external project services,
    create credentials, or write project files.
14. Installation documentation clearly distinguishes installation from
    project initialization.

## Proposed implementation jobs

These are planning proposals, not started jobs.

| Job | Role | Objective | Main outputs | Required validation |
|---|---|---|---|---|
| JOB-INSTALL-001 | Service | Define the package/bootstrap command and platform detection | npm package entry point, version selection, platform resolver | Unit tests and clean-machine rehearsal |
| JOB-INSTALL-002 | Service | Implement verified user-local installation and manifest | installer, `~/.leadfox` layout, checksum verification | Wrong checksum, interrupted download, permission, and symlink tests |
| JOB-INSTALL-003 | Service | Implement PATH guidance and safe shell integration | opt-in PATH updater, doctor diagnostics, rollback of PATH line | zsh, bash, PowerShell design decision; idempotence tests |
| JOB-INSTALL-004 | Service | Implement update and rollback behavior | versioned release activation and rollback | Failed update leaves prior version active |
| JOB-INSTALL-005 | Documentation | Publish installation and troubleshooting guide | README and user guide updates | Fresh-machine walkthrough |

## Open decisions

- Will the first distribution use npm-hosted platform archives, npm-hosted
  binaries, or a separate release host?
- Will releases use signed metadata in addition to checksums?
- Which operating systems and architectures are supported at first release?
- Is Windows included in the first release?
- Is a self-update command required for version one?
- Should the installer retain one previous version automatically?
- What is the official package name?
- Is `~/.leadfox` the permanent user data directory, or only the initial
  installation directory?

## Out of scope until after installation

- `leadfox init` project scaffolding;
- installation of the intake-planning skill;
- installation or registration of the Control-plan-review skill;
- project-local AInnov files;
- agent host configuration, Claude, Codex, Herdr, or MCP setup;
- cloud authentication and deployment.

## Change history

| Version | Date | Author | Change |
|---|---|---|---|
| 1 | 2026-11-02 | Project owner / Leadfox planning | Initial pre-initialization installation plan |
