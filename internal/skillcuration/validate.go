package skillcuration

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// Draft validation (add-skill-curation-from-traces 5.2, design D4). Cheap,
// no-LLM checks run on every parsed draft BEFORE it can become a candidate
// row: slug rules, cross-tier name collision with system names reserved,
// description presence, tool dependencies against the catalog, and size
// bounds. The proposer retries a rejected draft with the problems appended
// to the prompt, then drops it and logs — the cycle continues either way.

const (
	// MaxSkillContentBytes bounds a draft's SKILL.md content (32 KiB — an
	// order of magnitude above any sane procedure; a draft overflowing this
	// is a model malfunction, not a skill).
	MaxSkillContentBytes = 32 << 10
	// MaxSkillDescriptionChars bounds the description length in runes.
	MaxSkillDescriptionChars = 512
	// MinSkillDescriptionChars is the "present and non-trivial" floor: a
	// one-word description cannot tell an agent when to use the skill.
	MinSkillDescriptionChars = 10
)

// ReservedSystemSkillNames lists the system-tier skill names no curated
// draft may collide with (D4: "cross-tier collision with system names
// reserved"). It is the hardcoded mirror of the embedded system skills
// (internal/agents/systemskills: web-research, document-read); that leaf
// package is deliberately not imported here so this package stays decoupled
// from internal/agents, and validate_test.go pins this slice to
// systemskills.ListEmbedded() — drift fails `go test`. T6's approval
// handler re-validates collisions and shares THIS var, keeping both checks
// identical.
var ReservedSystemSkillNames = []string{"web-research", "document-read"}

// SkillContentReader reads one agent-tier skill's SKILL.md content from
// disk. found=false when the agent carries no such skill — absence is the
// normal state, never an error. The proposer uses it for an edit's
// superseded content and the agent-tier collision check; the composition
// root wires it to the agent skills directory reader (task 7) so this
// package never imports internal/agents. Tests stub it with a map.
type SkillContentReader func(ctx context.Context, workspaceID, agentID, skillName string) (content string, found bool, err error)

// ToolNamesFunc lists the workspace tool catalog's tool keys. Task 7 wires
// the real catalog (internal/agents ToolCatalog keys); tests inject fakes.
// Draft validation rejects any declared dependency outside the list (spec:
// "Draft naming a nonexistent tool is rejected").
type ToolNamesFunc func() []string

// SkillDraft is one parsed proposal: the strict output of the proposer's
// side-call after parsing, before validation and storage.
type SkillDraft struct {
	// Name is the skill slug (DNS-label shaped).
	Name string
	// Description is the one-line what-and-when.
	Description string
	// Content is the complete drafted SKILL.md.
	Content string
	// Tools are the declared dependencies.tools entries (catalog keys).
	Tools []string
	// CitedPatterns cite the wiki pattern slugs the draft builds on.
	CitedPatterns []string
	// CitedRuns cite the evidence-run session IDs the draft derives from.
	CitedRuns []string
	// Supersedes names the curated skill this draft edits; empty for a
	// first-time proposal.
	Supersedes string
}

// DraftRejection is the aggregate validation failure: every problem in one
// value so the proposer's retry can feed the full list back to the model.
// Stage failures (store or seam read errors) are returned as plain errors
// instead — they are the caller's fail-soft, not the draft's fault.
type DraftRejection struct {
	Problems []string
}

// Error renders the joined problems ("draft rejected: a; b; c").
func (e *DraftRejection) Error() string {
	return "draft rejected: " + strings.Join(e.Problems, "; ")
}

// DraftValidator applies the 5.2 checks to parsed drafts. It holds the
// collision surfaces that need I/O — the workspace skill registry (a
// workspace skill is a registry row + body on disk; the row name is the
// collision key) and the agent-tier skills-directory reader — plus the tool
// catalog source. The pure slug, description, and size rules are package
// constants and functions.
type DraftValidator struct {
	workspaceSkills store.WorkspaceSkillStore
	// agentSkillContent is the agent-tier seam: a name that reads back
	// found=true collides with an existing agent skill.
	agentSkillContent SkillContentReader
	tools             ToolNamesFunc
}

// NewDraftValidator constructs the validator from its granular
// dependencies: the workspace skill registry, the agent-tier content
// reader, and the tool catalog source.
func NewDraftValidator(workspaceSkills store.WorkspaceSkillStore, agentSkillContent SkillContentReader, tools ToolNamesFunc) *DraftValidator {
	return &DraftValidator{
		workspaceSkills:   workspaceSkills,
		agentSkillContent: agentSkillContent,
		tools:             tools,
	}
}

// ToolNames returns the configured tool catalog source — the proposer reads
// it to render the catalog into its prompt from the same list validation
// checks against (one source, never two diverging copies).
func (v *DraftValidator) ToolNames() []string { return v.tools() }

// AgentSkillContent reads one agent-tier skill through the injected reader.
func (v *DraftValidator) AgentSkillContent(ctx context.Context, workspaceID, agentID, skillName string) (string, bool, error) {
	return v.agentSkillContent(ctx, workspaceID, agentID, skillName)
}

// ValidateDraft checks one parsed draft. exempt is the superseded skill's
// name for an edit — the one name allowed to collide, because it names the
// skill being replaced; pass "" for first-time proposals.
//
// Draft failures return a *DraftRejection (errors.As-able) carrying every
// problem; store or seam READ failures return plain errors (stage errors —
// the caller fails the stage, it does not blame the draft).
func (v *DraftValidator) ValidateDraft(ctx context.Context, workspaceID, agentID string, draft SkillDraft, exempt string) error {
	var problems []string

	name := strings.TrimSpace(draft.Name)

	// Slug rules — DNS-label shaped like wiki page slugs (and like the
	// on-disk skills the name will one day materialize next to).
	if err := ValidatePageSlug(name); err != nil {
		problems = append(problems, fmt.Sprintf("name: %v", err))
	}
	// System tier is reserved outright.
	if slices.Contains(ReservedSystemSkillNames, name) {
		problems = append(problems, fmt.Sprintf("name %q is reserved for a system skill", name))
	}

	// Workspace tier: the registry rows.
	rows, err := v.workspaceSkills.List(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("skillcuration: list workspace skills: %w", err)
	}
	for _, row := range rows {
		if row.Name == name && name != exempt {
			problems = append(problems, fmt.Sprintf("name %q collides with a workspace skill", name))
			break
		}
	}

	// Agent tier: the skills directory on disk, through the injected
	// reader.
	if _, found, err := v.agentSkillContent(ctx, workspaceID, agentID, name); err != nil {
		return fmt.Errorf("skillcuration: read agent skill %q: %w", name, err)
	} else if found && name != exempt {
		problems = append(problems, fmt.Sprintf("name %q collides with an existing agent skill", name))
	}

	// Description: present and non-trivial.
	desc := strings.TrimSpace(draft.Description)
	runes := utf8.RuneCountInString(desc)
	switch {
	case desc == "":
		problems = append(problems, "description is empty")
	case runes < MinSkillDescriptionChars:
		problems = append(problems, fmt.Sprintf("description is trivial (%d chars; at least %d required)", runes, MinSkillDescriptionChars))
	case runes > MaxSkillDescriptionChars:
		problems = append(problems, fmt.Sprintf("description exceeds %d chars (%d)", MaxSkillDescriptionChars, runes))
	}

	// dependencies.tools ⊆ tool catalog.
	catalog := make(map[string]struct{})
	for _, tool := range v.tools() {
		catalog[strings.TrimSpace(tool)] = struct{}{}
	}
	for _, tool := range draft.Tools {
		tool = strings.TrimSpace(tool)
		switch {
		case tool == "":
			problems = append(problems, "declares an empty tool dependency")
		default:
			if _, known := catalog[tool]; !known {
				problems = append(problems, fmt.Sprintf("declares unknown tool %q", tool))
			}
		}
	}

	// Size bounds.
	if strings.TrimSpace(draft.Content) == "" {
		problems = append(problems, "content is empty")
	}
	if len(draft.Content) > MaxSkillContentBytes {
		problems = append(problems, fmt.Sprintf("content exceeds %d bytes (%d)", MaxSkillContentBytes, len(draft.Content)))
	}

	if len(problems) == 0 {
		return nil
	}
	return &DraftRejection{Problems: problems}
}
