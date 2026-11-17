# Control prompt: Leadfox job reliability refactor

You are the Control Agent for the Leadfox repository. Implement the approved
job reliability and recovery plan in:

`docs/planning/leadfox-job-reliability-001.md`

The goal is to ensure that no Leadfox job can silently stop, remain falsely
marked as running, or lose recoverable work when an agent session or worker
ends.

## Operating rules

- Review the current implementation, tests, backlog, and documentation before
  editing.
- Work through R0, then R1, then R2, then R3. Do not skip checkpoints.
- Use bounded jobs for implementation. Keep each job independently reviewable.
- Do not begin Herdr integration during this refactor.
- Do not change project-specific files in consuming projects.
- Do not add credentials, provider tokens, or unrestricted worker output to
  Leadfox records.
- Do not merge, push, deploy, or make external changes without the required
  approval.

## First response required

Before creating implementation jobs, report:

1. the current lifecycle and runtime behavior;
2. the exact failure modes covered by existing tests;
3. the proposed R0–R3 job breakdown;
4. the default persistent worktree behavior;
5. the proposed interruption states and event model;
6. the notification and journal boundary;
7. files and APIs that will change;
8. unresolved owner decisions and stop conditions.

Stop for owner direction if any choice would change Leadfox authority,
credential handling, external side effects, or compatibility guarantees.

## Implementation requirements

- Preserve existing project and job records.
- Make interrupted work recoverable from a branch and commit.
- Reject volatile worktrees by default, or record an explicit override.
- Distinguish deliberate cancellation from timeout, worker loss, and pause.
- Record progress without treating progress as approval.
- Emit safe, actionable notifications for exceptional and terminal events.
- Keep manual/no-runtime use working.
- Add deterministic tests for the complete interruption matrix in the plan.
- Update the user guide and upgrade plan with the final command behavior.

## Completion report

For each checkpoint, return:

- implementation job and reviewed commit;
- changed paths and scope result;
- tests and gates run;
- interruption scenarios tested;
- notification examples with secret-safety result;
- migration/compatibility result;
- unresolved limitations;
- recommended next checkpoint.

Do not claim the refactor is complete until the acceptance criteria in the
plan pass and a fresh `leadfox job status` report shows no unexplained running
jobs.
