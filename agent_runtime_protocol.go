package main

import _ "embed"

// agentRuntimeProtocolPath is the managed project document that tells
// Control and job agents how to keep running jobs explained.
const agentRuntimeProtocolPath = "docs/agent-runtime.md"

// agentRuntimeProtocolDoc is written by leadfox init and project migrate.
//
//go:embed templates/agent-runtime.md
var agentRuntimeProtocolDoc string

// jobRuntimeReportingSection is appended to new job packets so that the
// job agent's brief carries the reporting commands with its job ID.
func jobRuntimeReportingSection(id string) string {
	return "## Runtime reporting\n\nFollow " + agentRuntimeProtocolPath + ". From your job worktree:\n\n" +
		"1. Start: `leadfox job runtime register " + id + " --runtime HOST`, then set `LEADFOX_EXECUTION_ID` to the printed value.\n" +
		"2. At each milestone (each commit, before and after gates, at least every 10 minutes): `leadfox job progress " + id + " --remaining TEXT --next TEXT`.\n" +
		"3. Blocked: `leadfox job progress " + id + " --blocker TEXT`, then stop.\n" +
		"4. Finished: commit, write the handoff, then `leadfox job runtime event " + id + " --type handoff`.\n" +
		"5. Stopping early: commit, record remaining work, then `leadfox job runtime event " + id + " --type exit --exit-code 1`.\n\n" +
		"Progress and handoff are not approval. Never include credentials or raw output.\n"
}

// jobReportingCommands lists the job-specific reporting commands for job
// context exports.
func jobReportingCommands(id string) []string {
	return []string{
		"leadfox job runtime register " + id + " --runtime HOST",
		"leadfox job progress " + id + " --remaining TEXT --next TEXT",
		"leadfox job runtime heartbeat " + id,
		"leadfox job runtime event " + id + " --type handoff",
		"leadfox job runtime event " + id + " --type exit --exit-code N",
	}
}
