# Job runtime protocol

Leadfox can detect a stopped worker only if the worker reports. Follow these
rules so that every job marked running is explained by a live worker, a
handoff, or a deliberate decision. Progress, heartbeats and handoffs are
never approval: Control still reviews, and the developer still approves.

## Control: every session

1. **Start of session.** Run:

   ```sh
   leadfox job check --all
   leadfox job list
   ```

   `job check` records `worker-lost` or `timed-out` for jobs whose worker
   exited, disconnected, went silent past the threshold, or ran past its
   timeout. For every job with an ATTENTION entry or an interrupted state,
   run `leadfox job status JOB-ID` and choose one:
   - `leadfox job resume JOB-ID --actor NAME` when the worktree is present;
   - `leadfox job recover JOB-ID --actor NAME --reason TEXT` when the
     worktree is missing (it is rebuilt from the branch and the job is left
     paused);
   - `leadfox job reissue JOB-ID NEW-ID --actor NAME --reason TEXT
     --from-branch` (or `--from-recovery latest`) to continue the work in a
     new job.

   Record the decision when the decision policy requires it.
2. **Dispatch.** When starting a job agent, give it the job ID and the name
   of its host (for example claude, codex or local) and tell it to follow the
   job agent rules below. If the agent's host cannot write to this
   checkout's `.local/` folder, register the execution yourself, relay its
   progress with `leadfox job progress`, and supervise the job as a manual
   job.
3. **During work.** `leadfox job status JOB-ID` shows the runtime condition
   and latest progress. A `stale` worker needs attention before `job check`
   marks it lost.
4. **Handoff.** A handoff notice means the worker reports completion. Review
   it with `leadfox job review`; a handoff or a successful exit is not
   acceptance.
5. **End of session.** Do not leave a job running without a live worker.
   Pause jobs you will not supervise
   (`leadfox job pause JOB-ID --actor NAME --reason TEXT`) and block jobs
   waiting on an external dependency (`leadfox job block`).

## Job agent

Use the job ID from your packet. Run these commands from your job worktree;
they write only to the Control checkout's uncommitted runtime journal.

1. **Register once, at the start.**

   ```sh
   leadfox job runtime register JOB-ID --runtime HOST
   export LEADFOX_EXECUTION_ID=...   # the value the command prints
   ```

2. **Report progress at each milestone**: after each commit, before and
   after running gates, and at least every 10 minutes while working.

   ```sh
   leadfox job progress JOB-ID --remaining "what is left" --next "next step"
   ```

   Add `--gate ID` for gates you ran. With `LEADFOX_EXECUTION_ID` set,
   progress also counts as a heartbeat. During a long step with no
   milestone, send `leadfox job runtime heartbeat JOB-ID`.
3. **If blocked**, record `leadfox job progress JOB-ID --blocker "reason"`
   and stop; Control decides what happens next.
4. **When finished**, commit your work, write the handoff, then report it:

   ```sh
   leadfox job runtime event JOB-ID --type handoff
   ```

5. **If you must stop early**, commit what you have, record progress with
   the remaining work, then:

   ```sh
   leadfox job runtime event JOB-ID --type exit --exit-code 1
   ```

   Control's next `job check` marks the job worker-lost; your branch and
   uncommitted work are preserved.

Never put credentials, tokens or raw command output in progress text or
notes; Leadfox refuses text that looks like a credential. Reference logs with
`leadfox job runtime event JOB-ID --type output-ref --output-ref PATH`.

## Thresholds

`control/settings/runtime.yaml` sets when a silent worker is reported stale
(default 15m) and recorded lost (default 60m), and an optional job timeout.
A job with no registered runtime is never marked lost automatically, so
Control must supervise it directly.
