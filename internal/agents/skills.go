package agents

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The skills seam (spec agent-skills, design D6): one turn's skill capability
// is a two-tier backend — the global skills library plus the agent's
// workspace skills directory, agent tier winning on a name conflict, both
// read fresh per turn — carrying the eligibility gate (global enable × the
// agent's skill allowlist, empty = all). The backend feeds the Eino skill
// middleware, which advertises metadata only and loads bodies on demand
// through its skill tool; $name tokens in the user's input are preprocessed
// into explicit invocations ahead of composition (resolve → compose →
// execute, design D1).
//
// Like the workspace-rooted tools, the capability degrades to a null object:
// a runtime without a skills directory, or an agent without a slug, runs
// turns with no skills at all.

// storeSkillEligibility adapts the store's skill registry into the backend's
// SkillEligibility seam: a registry row switched off is a globally disabled
// skill name; rows are absent for never-indexed skills (workspace-tier-only
// names), which therefore count as enabled — the registry only knows the
// global library. The adapter lives inside internal/agents so the backend
// package stays free of store imports (the websearch_source.go pattern).
type storeSkillEligibility struct {
	registry store.SkillRegistry
}

// DisabledSkillNames implements backend.SkillEligibility.
func (s storeSkillEligibility) DisabledSkillNames(ctx context.Context) ([]string, error) {
	rows, err := s.registry.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("read skill registry: %w", err)
	}
	var disabled []string
	for _, row := range rows {
		if !row.Enabled {
			disabled = append(disabled, row.Name)
		}
	}
	return disabled, nil
}

// resolveSkills builds the turn's skill backend: the global library plus the
// agent's workspace skills directory (created on demand so agents can author
// skills into it through the jailed file tools), gated by the store-backed
// eligibility and the agent's skill allowlist. A nil backend means the
// capability is off and the turn composes without it. A real IO failure
// while materializing the skills directory fails the turn, matching the
// workspace materialization contract (gate.go).
func (m *manager) resolveSkills(ctx context.Context, agent store.Agent) (*backend.SkillBackend, error) {
	if m.skillsDir == "" || m.workspaces == nil || agent.Slug == "" {
		return nil, nil
	}
	agentSkillsDir, err := m.workspaces.SkillsDir(agent.Slug)
	if err != nil {
		return nil, fmt.Errorf("resolve skills for agent %q: %w", agent.Slug, err)
	}
	return backend.NewSkillBackend(m.skillsDir, agentSkillsDir, storeSkillEligibility{registry: m.store.Skills()}, agent.Skills), nil
}

// skillMentionPattern matches $name tokens in user input. Skill names are
// DNS-label shaped (lowercase letters, digits, hyphens); anything else —
// shell variables like $HOME, dollar amounts — does not match.
var skillMentionPattern = regexp.MustCompile(`\$([a-z0-9][a-z0-9-]*)`)

// injectSkillInvocations preprocesses user input for explicit skill
// invocation: every $name token matching an eligible skill turns into a
// blocking instruction injected ahead of the user's message, mirroring the
// skill middleware's own "invoke before responding" language. Execution
// still flows through the skill middleware's tool. Non-matching tokens pass
// through untouched; a lookup failure degrades to the original input.
//
// The injected text is runner plumbing, never the user's words: it rides the
// model input only — the persisted user message and the session title keep
// the raw input.
func injectSkillInvocations(ctx context.Context, b *backend.SkillBackend, input string) string {
	if input == "" || b == nil || !strings.Contains(input, "$") {
		return input
	}

	names, err := b.AvailableSkillNames(ctx)
	if err != nil {
		slog.WarnContext(ctx, "skill invocation preprocessing skipped: available skills lookup failed", "error", err)
		return input
	}
	available := make(map[string]struct{}, len(names))
	for _, name := range names {
		available[name] = struct{}{}
	}

	var invocations []string
	injected := make(map[string]struct{})
	for _, match := range skillMentionPattern.FindAllStringSubmatch(input, -1) {
		name := match[1]
		if _, done := injected[name]; done {
			continue
		}
		if _, ok := available[name]; !ok {
			continue // unknown or ineligible: plain text
		}
		injected[name] = struct{}{}
		invocations = append(invocations, fmt.Sprintf(
			"The user explicitly invoked the skill %q ($%s). Before generating any other response about this task, invoke the skill tool with name %q to load the skill's full instructions, then follow them. Do not answer until the skill is loaded.",
			name, name, name,
		))
	}
	if len(invocations) == 0 {
		return input
	}

	return strings.Join(invocations, "\n") + "\n\n" + input
}
