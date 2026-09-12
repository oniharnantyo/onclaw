// Package teams implements the built-in team templates and their
// materialization into channels (OpenSpec change channel-teams, design D6).
//
// Templates are code-defined product surface over the v1 channel primitives:
// named role slots (a specialization note plus a role prompt hint), a
// conventions prefill, and one designated facilitator slot. Materializing a
// template creates the channel and its memberships, binding each slot either
// to a newly spawned agent (role-informed identity generation through the
// production creation path) or to an existing workspace agent.
package teams

import "strings"

// TeamSlot is one named role seat on a template. ID is the stable slot key
// the materialize payload binds against; Title is the human role name (and
// the spawned agent's name); Specialization becomes the roster note; RolePrompt
// seeds the spawned agent's identity documents; Facilitator marks the single
// slot that materializes as the channel's facilitator (design D2).
type TeamSlot struct {
	ID             string
	Title          string
	Specialization string
	RolePrompt     string
	Facilitator    bool
}

// TeamTemplate is a built-in team blueprint: the slots plus the channel
// conventions prefill the materialized channel starts with (design D5/D6).
type TeamTemplate struct {
	ID          string
	Name        string
	Description string
	Conventions string
	Slots       []TeamSlot
}

// softwareTeamConventions is the D5 conventions prefill: artifacts live in
// /project with PLAN.md as the tracker, claims before writes, discussion in
// the feed, human sign-off at gates, and the facilitator owns the close.
const softwareTeamConventions = `Artifacts live in /project: spec.md holds the agreed design, PLAN.md is the tracker — claim your area in PLAN.md before you write anything. Keep discussion in this feed; keep the artifacts factual and current. Tag a human with @mention whenever a decision or gate needs sign-off. The facilitator sequences the work and closes the session with a summary when the goal is met.`

// SoftwareTeam is the built-in "Software Team" template (design D6): a PM, an
// architect, a scrum-master facilitator, and three builders.
func softwareTeam() TeamTemplate {
	return TeamTemplate{
		ID:          "software-team",
		Name:        "Software Team",
		Description: "A product squad that turns a goal into a spec, builds it, and verifies it — PM, architect, scrum-master facilitator, frontend, backend, and tester.",
		Conventions: softwareTeamConventions,
		Slots: []TeamSlot{
			{
				ID:             "pm",
				Title:          "Product Manager",
				Specialization: "Owns scope: turns the goal into user stories with acceptance criteria and cuts what does not serve it.",
				RolePrompt:     "You scope product work. Restate the session goal as user stories with acceptance criteria, cut scope ruthlessly, and tag a human when a product decision needs sign-off. You do not write code.",
			},
			{
				ID:             "architect",
				Title:          "Architect",
				Specialization: "Owns the technical shape: system design, interfaces, data model, and trade-offs.",
				RolePrompt:     "You design before anyone builds. Write the design into /project/spec.md, keep interfaces stable across the team, and surface risky unknowns to the product manager early.",
			},
			{
				ID:             "scrum-master",
				Title:          "Scrum Master",
				Specialization: "Facilitates the session: sequencing, handoffs, unblocking, and the close-out summary.",
				RolePrompt:     "You are the session facilitator. Sequence the work, hand each task to the right specialist with @mentions, keep every hop on task, and when the goal is met call session.close with a summary of what shipped and what remains.",
				Facilitator:    true,
			},
			{
				ID:             "frontend",
				Title:          "Frontend Engineer",
				Specialization: "Implements the UI: components, states, accessibility, and responsive behavior.",
				RolePrompt:     "You implement the UI slices handed to you. Claim your area in /project/PLAN.md before writing, build to the spec's acceptance criteria, and note any deviation from the spec in the feed.",
			},
			{
				ID:             "backend",
				Title:          "Backend Engineer",
				Specialization: "Implements services and data: APIs, persistence, migrations, and tests.",
				RolePrompt:     "You implement the service side of the spec. Claim your area in /project/PLAN.md before writing, keep the API contract with the frontend exact, and raise schema risks to the architect.",
			},
			{
				ID:             "tester",
				Title:          "QA Engineer",
				Specialization: "Verifies the result: test plans, edge cases, and regression notes.",
				RolePrompt:     "You verify against the spec's acceptance criteria. Keep the test plan in /project, report defects in the feed with reproduction steps, and give a clear go/no-go at the release gate.",
			},
		},
	}
}

// BuiltIn returns the built-in templates in shipping order (design D11: the
// built-in set starts with Software Team). The returned slice is a fresh copy
// per call — callers may mutate it freely.
func BuiltIn() []TeamTemplate {
	return []TeamTemplate{softwareTeam()}
}

// Get resolves a built-in template by id.
func Get(id string) (TeamTemplate, bool) {
	for _, t := range BuiltIn() {
		if t.ID == strings.TrimSpace(id) {
			return t, true
		}
	}
	return TeamTemplate{}, false
}
