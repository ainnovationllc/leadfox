# Leadfox User Guide

This guide is for a developer using Leadfox to coordinate a project from
scratch development through tested deployment. It assumes the developer wants
Control to handle routine coordination while retaining approval over external,
security-sensitive, costly, destructive, and production actions.

## 1. What Leadfox provides

Leadfox provides a repeatable control layer around a software project:

- project initialization and structure;
- requirements-to-architecture planning;
- bounded jobs for specialized agents;
- isolated Git branches and worktrees;
- deterministic gates and retained evidence;
- review, approval, merge, cancellation, and reissue lifecycle;
- project decision policies that reduce repetitive prompts;
- optional agent capabilities, including MCP-based tools;
- security scanning and framework-neutral assurance evidence.

Leadfox is not an application framework, CI provider, cloud provider, secret
manager, or compliance certification service.

## 2. Installation and verification

From the Leadfox repository:

```sh
make build
./leadfox version
./leadfox version --json
```

For repeatable distribution builds, use `make dist`. Pin the binary version in
the consuming project when its lifecycle records depend on a particular
behavior.

## 3. Initialize a project

```sh
leadfox init my-project
cd my-project
```

Useful options:

```sh
leadfox init my-project --parent /path/to/projects
leadfox init my-project --dry-run
leadfox init --help
```

Initialization creates the project directories and starter files, including:

- `control/` for project metadata, requirements, pins, settings, gates, jobs,
  assurance, security, capabilities, and decision policy;
- `services/`, `tests/`, `contracts/`, and `infrastructure/terraform/`;
- `AGENTS.md` and a `CLAUDE.md` symlink;
- `docs/agent-planning.md`, `docs/agent-roles.md`, and
  `docs/decision-policy.md`.

Initialization does not create services, provision infrastructure, contact
external systems, or create a Git remote.

## 3.1 Validate planning documents

After the planning agent or project manager completes the intake, validate it
before Control begins architecture planning:

```sh
leadfox intake validate
leadfox intake validate --format json
```

For an existing project, validate a proposed living-document update against
its referenced baseline:

```sh
leadfox change validate control/changes/CHANGE-001-add-service.md
leadfox change validate control/changes/CHANGE-001-add-service.md --format json
```

Validation is deterministic. Exit `0` means the document is structurally
complete, exit `1` reports findings that need correction, and exit `2` means
the input could not be processed. A passing result is not approval and does
not create jobs, modify the architecture plan, contact external systems, or
perform the proposed change.

## 4. Planning with Control

Start the preferred planning agent from the project root. The generated
`AGENTS.md` tells Control to ask whether planning should begin when no
requirements exist.

The planning sequence is:

1. gather functional and non-functional requirements;
2. confirm them with the developer;
3. write one stable requirement document per requirement;
4. run `leadfox plan`;
5. review and approve the architecture plan;
6. reconcile the approved model into services, dependencies, entry points,
   data stores, settings, pins, contracts, and gates;
7. divide the approved build into bounded jobs.

Do not treat a generated plan or an agent handoff as approval.

## 5. Project controls

The main project control files are:

| File | Purpose |
| --- | --- |
| `control/project.yaml` | project identity, services, environments, entry points, and data stores |
| `control/requirements/` | confirmed functional and non-functional requirements |
| `control/pins.yaml` | exact versions, digests, and artifact references |
| `control/settings/` | non-secret environment settings and secret references |
| `control/gates.yaml` | checks that must pass at lifecycle stages |
| `control/agent-capabilities.yaml` | tools or MCP capabilities an agent host may provide |
| `control/decision-policy.yaml` | authority boundaries for Control |
| `control/risk-controls.yaml` | project-defined risks and required review inputs |
| `control/assurance.yaml` | evidence coverage mappings, not certification |
| `control/security.yaml` | project secret-scanning configuration |
| `control/jobs/` | bounded job packets and their lifecycle records |
| `control/handoffs/` | agent handoff records |

Pins identify what is being used. Settings describe how an environment is
configured. Gates define what must be checked. None of these files contains
secret values.

## 6. Decision policy and approval fatigue

Inspect the project policy with:

```sh
leadfox policy validate
leadfox policy show
```

The policy has four categories:

- `automatic`: Control may perform routine actions without asking again;
- `control_with_record`: Control may decide, but records the rationale;
- `developer_approval`: Control must ask before execution;
- `escalation_triggers`: conditions that always require developer review.

The default policy treats cloud resources, IAM, secrets, production changes,
remote pushes, external downloads, and compliance claims as approval-required
or escalation actions. A project may edit the policy, but should keep explicit
approval for actions with external, costly, destructive, public, security, or
production effects.

Control should not repeatedly ask about an approved policy default. It should
ask only when an action falls outside the policy or an escalation trigger
applies.

## 7. Risk controls

Projects may declare additional review requirements without changing Leadfox's
core behavior. Define them in `control/risk-controls.yaml`:

```yaml
version: 1
risk_controls:
  - id: external-tool
    description: "The job depends on a capability of an external tool."
    required_evidence:
      - real-tool-check
      - tool-version-and-mode
    stop_conditions:
      - real-capability-unproven
```

Validate or inspect the registry with:

```sh
leadfox risk validate
leadfox risk list
```

Attach a control to a job when creating its packet:

```sh
leadfox job new JOB-001 --risk external-tool --allow scripts/**
```

Leadfox checks that every referenced ID exists when the packet is created,
started, and reviewed. The consuming project remains responsible for deciding
whether the evidence is sufficient and for recording any approved residual
risk. This keeps database, cloud, SOC 2, and other domain rules out of the
Leadfox core.

## 8. Agent capabilities and MCP

Declare a host capability in `control/agent-capabilities.yaml`, for example an
MCP-backed design tool:

```yaml
capabilities:
  - id: design-mcp
    kind: mcp
    provider: host-configured
    tools:
      - read_design
      - export_assets
    access: read-only
    authentication: user
    network: true
    output_paths:
      - services/web/src
    fallback: stop
```

Inspect capabilities with:

```sh
leadfox capability list
leadfox capability validate design-mcp
```

Require one in a job packet with:

```sh
leadfox job new JOB-UI-001 \
  --role service \
  --objective "Implement the approved frontend design" \
  --capability design-mcp \
  --allow services/web/**
```

Leadfox validates the declaration and carries the requirement through the job
runtime. The agent host supplies the MCP connection and credentials. Leadfox
does not store tokens or silently grant access to an unavailable capability.

## 9. Create and run a bounded job

Create a packet:

```sh
leadfox job new JOB-001 \
  --role service \
  --objective "Implement the registration endpoint" \
  --allow services/registration/** \
  --exclude infrastructure/** \
  --gate local-dev
```

Control then completes the packet, adds acceptance criteria and settings or
pin references, and marks it ready. The lifecycle is:

```text
draft -> ready -> running -> review-pending/review -> accepted -> merged
```

Start work in an isolated worktree:

```sh
leadfox job start JOB-001 --worktree /tmp/project-job-001
```

The agent receives only the packet as its work brief. It must stay inside the
allowed paths, obey stop conditions, run its checks, and return a handoff.

Useful lifecycle operations include:

```text
leadfox job scope JOB-001
leadfox job review JOB-001 --commit <sha> --worktree /tmp/project-review-001
leadfox job attest JOB-001 --check local-dev/scope --result passed
leadfox job evidence JOB-001 --criterion C1 --provider github-actions --run-id 123 --commit <sha> --conclusion success
leadfox job approve JOB-001 --verdict accepted --approved-by developer
leadfox job merge JOB-001
leadfox job block JOB-001 --reason "waiting for external decision"
leadfox job cancel JOB-001 --reason "superseded by JOB-002"
leadfox job reissue JOB-001 JOB-002 --reason "new base required"
```

Review checks scope, runs declared gates, records evidence, and validates the
reviewed commit. Approval is a separate decision. Merge is not deployment.

## 9. Gates and evidence

Run a gate from the project root:

```sh
leadfox gate run local-dev
leadfox gate run integration --format json
leadfox gate run local-dev --repeat 3
```

Gate results are evidence, not approval. A gate should define automated checks,
manual checks, applicability, and any required paths. A manual check must be
attested; it must not be silently treated as passed.

For every important job, retain:

- the reviewed commit;
- the gate result and hash;
- setup evidence;
- external CI run and artifact references;
- manual attestations and their actor;
- the Control review and approval record.

## 10. Security and assurance

Run the configured project scanner with:

```sh
leadfox security scan
leadfox security scan --format json
```

Keep scanner versions and checksums in project pins. Findings should be fixed
at the source. Exact fingerprints may be used only for reviewed, unchangeable
historical false positives; broad history exclusions are unsafe.

Report evidence coverage with:

```sh
leadfox assurance report
leadfox assurance report --format json
```

The assurance report identifies missing, present, verified, mismatched, and
external evidence. It does not certify SOC 2 or replace an auditor.

## 11. Git, environments, and deployment

Jobs use branches and worktrees so agent changes remain isolated from the
Control checkout and from one another. Control reviews and merges approved
work. Job branches and worktrees are not automatically production releases.

Environment settings, Terraform, container composition, external test
systems, and cloud resources should be represented in the project controls so
they can be reproduced and gated. External actions—such as creating a remote,
pushing, applying Terraform, creating secrets, or changing IAM—require the
authority specified by the decision policy.

## 12. Troubleshooting

- If a job starts with the wrong base, cancel and reissue it rather than
  rewriting history.
- If a long-running job needs new `main` work, synchronize it and review scope
  from the recorded merge base.
- If a gate fails, inspect the evidence and handoff before rerunning. A green
  rerun does not erase a prior failure.
- If a required capability is unavailable, stop the job and report it; do not
  silently substitute an unapproved provider.
- If assurance hashes mismatch, re-review the changed evidence or update the
  mapping only through a normal Control change.

## 13. Current boundaries

Leadfox currently does not provide a universal agent launcher, MCP connection
manager, cloud credential manager, GitHub App, branch-protection system, or
SOC 2 certification. Those concerns remain with the agent host, project
configuration, cloud platform, Git provider, and independent auditors.
