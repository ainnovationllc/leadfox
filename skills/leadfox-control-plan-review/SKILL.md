---
name: leadfox-control-plan-review
description: Review a project intake for Control without silently approving or implementing it.
---

# Leadfox Control Plan Review

Use this project-local skill after a developer or planning agent has created
`control/project-intake.md`.

## Review sequence

1. Read the project instructions and project-owned control files.
2. Run `leadfox intake validate` and resolve structural findings.
3. Compare the intake with `control/project.yaml`, requirements, policies, and
   existing architecture records.
4. Distinguish inherited, confirmed, proposed, deferred, unknown, and
   not-applicable information.
5. Identify missing decisions, risks, environments, integrations, data
   boundaries, security controls, and operational concerns.
6. Propose updates to the architecture plan and bounded jobs for developer
   review.

## Authority boundary

This skill does not approve requirements, architecture, jobs, credentials,
external access, deployment, or production changes. It must not execute
uninstall commands. If uninstall is relevant, display the exact command and
require the user to run it directly in their own shell.

Skills are guidance. Leadfox's deterministic validator and the project's
decision policy are the authoritative checks.
