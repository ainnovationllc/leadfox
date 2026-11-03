package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedIntakeRequiresCompletion(t *testing.T) {
	parent := t.TempDir()
	if err := runInit([]string{"example", "--parent", parent}); err != nil {
		t.Fatal(err)
	}
	report, err := validateIntakeFile(filepath.Join(parent, "example", "control", "project-intake.md"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || len(report.Findings) == 0 {
		t.Fatal("expected generated intake template to require completion")
	}
}

func TestCompletedIntakeValidates(t *testing.T) {
	parent := t.TempDir()
	if err := runInit([]string{"example", "--parent", parent}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(parent, "example", "control", "project-intake.md")
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	replacements := map[string]string{
		"Last updated: <!-- YYYY-MM-DD -->":                                 "Last updated: 2026-11-02",
		"- Project owner: <!-- required -->":                                "- Project owner: Example Team",
		"- Intake author: <!-- required -->":                                "- Intake author: Example Manager",
		"- Problem being solved: <!-- required -->":                         "- Problem being solved: Coordinate project onboarding",
		"- Desired outcome: <!-- required -->":                              "- Desired outcome: Produce a reviewable intake",
		"- How we will know the first version is useful: <!-- required -->": "- How we will know the first version is useful: Control can review it",
		"- Primary users: <!-- required -->":                                "- Primary users: Project developers",
		"  1. <!-- required -->":                                            "  1. User submits project context",
		"  2. <!-- required -->":                                            "  2. Agent creates the intake",
		"  3. <!-- required -->":                                            "  3. Leadfox validates the intake",
		"1. <!-- required -->":                                              "1. Capture project purpose",
		"2. <!-- required -->":                                              "2. Capture project flow",
		"3. <!-- required -->":                                              "3. Validate the document",
		"| <!-- required --> | <!-- required --> | <!-- existing/new --> | <!-- required --> | <!-- unknowns --> |":                                                                 "| Web app | Project interface | new | Example Team | none |",
		"| <!-- required; write none if none --> | <!-- required --> | <!-- inbound/outbound/both --> | <!-- local/test/staging/prod --> | <!-- required --> | <!-- unknowns --> |": "| none | No external integration | none | local | Example Team | none |",
		"| local | <!-- required --> | <!-- yes/no --> | <!-- none/mock/limited --> | <!-- placeholders/test data --> | <!-- disposable/retained --> |":                             "| local | Development and validation | yes | none | test data | disposable |",
		"- What must be protected: <!-- required -->":                                       "- What must be protected: project credentials",
		"- Important abuse or failure concerns: <!-- required -->":                          "- Important abuse or failure concerns: accidental secret disclosure",
		"- Questions that must be answered before implementation: <!-- required -->":        "- Questions that must be answered before implementation: What is the first service?",
		"| 1 | <!-- date --> | <!-- author --> | Initial intake | Leadfox initialization |": "| 1 | 2026-11-02 | Example Manager | Initial intake | Project kickoff |",
	}
	for old, replacement := range replacements {
		text = strings.ReplaceAll(text, old, replacement)
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := validateIntakeFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("completed intake did not validate: %+v", report.Findings)
	}
}

func TestIntakeRejectsSecretLikeContent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "project-intake.md")
	if err := os.WriteFile(file, []byte("# Project intake\n\n- Project name: example\n- token: ghp_abcdefghijklmnopqrstuvwxyz123456\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := validateIntakeFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		if finding.Code == "secret-like-content" {
			return
		}
	}
	t.Fatal("expected secret-like content finding")
}

func TestIntakeAcceptsEquivalentMarkdownFormatting(t *testing.T) {
	file := filepath.Join(t.TempDir(), "project-intake.md")
	text := `# Project Intake

## Identity
- Document version: 1.0.0
- Current stage: local development
- Last updated: 2026-11-02
- Change summary: Initial intake
- Project name: example
- Project owner: Example Team
- Intake author: Example Manager

## Problem and outcome
- Problem being solved: Coordinate onboarding
- Desired outcome: A useful first version
- How we will know the first version is useful: A user completes the flow

## Users and primary flow
- Primary users: Project developers
- Main user flow:
  1. User starts
  2. User submits information
  3. System confirms completion

## First-version capabilities
1. Capture information
2. Validate information
3. Report completion

## Services and components
| Component | Purpose | Status |
|---|---|---|
| Web app | User flow | proposed |

## Integrations and external dependencies
| System | Purpose | Status |
|---|---|---|
| none | No external system | none |

## Data
- Data: Project information

## Environments
| Environment | Purpose | Status |
|---|---|---|
| local | Development | confirmed |

## Security and operational concerns
- What must be protected: Project credentials
- Important abuse or failure concerns: Accidental disclosure

## First planning questions
- Questions that must be answered before implementation: What is the first milestone?

## Intake notes
- Notes: Keep the first version local

## Change history
| Date | Version | Author | Change Description |
|---|---|---|---|
| 2026-11-02 | 1.0.0 | Example Manager | Initial intake |
`
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := validateIntakeFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("equivalent Markdown formatting rejected: %+v", report.Findings)
	}
}

func TestChangePlanValidatesAgainstBaseline(t *testing.T) {
	root := t.TempDir()
	changes := filepath.Join(root, "control", "changes")
	if err := os.MkdirAll(changes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "control", "project.yaml"), []byte("name: example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "control", "architecture-plan.md"), []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"---", "change_id: CHANGE-001-add-reporting", "version: 1", "status: draft", "project: example", "change_type: add-service", "generated_by: planner", "approval: pending", "base_project_model: control/project.yaml", "base_architecture_plan: control/architecture-plan.md", "base_plan_version: 3", "---", "",
		"## Change summary", "- What is changing: Add reporting", "- Why it is changing: Users need reports", "- Requested by: Product", "",
		"## Affected baseline", "- Existing services affected: Web", "- Existing integrations affected: None", "- Existing data stores affected: None", "- Existing environments affected: local", "- Existing requirements affected: REQ-001", "- Existing decisions or jobs affected: None", "",
		"## Proposed delta", "### Added", "- Services: reporting", "### Changed", "- Services: none", "### Removed or superseded", "- Item: none", "",
		"## Compatibility and impact", "- Existing behavior preserved: yes", "- API or contract compatibility: additive", "- Data migration required: no", "- Backfill or rollback strategy: revert", "- Secret and credential impact: references only", "- Security and privacy impact: private", "- Cost, performance, and availability impact: low", "- Deployment order and dependencies: after API", "",
		"## Proposed validation and jobs", "- Required tests and gates: unit", "- Real-tool or external validation needed: provider", "- Proposed jobs: service job", "- Stop conditions: secrets", "",
		"## Open decisions", "| Decision | Owner | Status | Next action |", "|---|---|---|---|", "| Format | Product | proposed | Decide |", "",
		"## Change history", "| Version | Date | Author | Change | Reason or source |", "|---|---|---|---|---|", "| 1 | 2026-11-02 | Product | Initial | Request |",
	}
	file := filepath.Join(changes, "CHANGE-001.md")
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := validateChangePlan(file)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("valid change plan rejected: %+v", report.Findings)
	}
}

func TestChangePlanRejectsMissingBaseline(t *testing.T) {
	root := t.TempDir()
	changes := filepath.Join(root, "control", "changes")
	if err := os.MkdirAll(changes, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(changes, "CHANGE-001.md")
	text := "---\nchange_id: CHANGE-001\nversion: 1\nstatus: draft\nproject: example\nchange_type: add-service\ngenerated_by: planner\napproval: pending\nbase_project_model: control/project.yaml\nbase_architecture_plan: control/architecture-plan.md\nbase_plan_version: 1\n---\n"
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := validateChangePlan(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		if finding.Code == "missing-baseline" {
			return
		}
	}
	t.Fatal("expected missing baseline finding")
}
