package skillcuration

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/systemskills"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// Draft validation (5.2): cheap, no-LLM checks — slug rules, cross-tier
// collision with system names reserved, description presence, tool
// dependencies against the catalog, size bounds. Table-driven over a fake
// store and stub seams.
// ---------------------------------------------------------------------------

// mapContentReader is the agent-tier seam stub: skills live in a map.
func mapContentReader(skills map[string]string) SkillContentReader {
	return func(_ context.Context, _, _, name string) (string, bool, error) {
		content, ok := skills[name]
		return content, ok, nil
	}
}

// staticTools is the tool-catalog seam stub.
func staticTools(names ...string) ToolNamesFunc {
	return func() []string { return names }
}

// failingContentReader is the seam stub for reader I/O failures.
func failingContentReader(err error) SkillContentReader {
	return func(context.Context, string, string, string) (string, bool, error) { return "", false, err }
}

// validDraft is a draft that passes every check — table cases mutate it.
func validDraft() SkillDraft {
	return SkillDraft{
		Name:          "deploy-guard",
		Description:   "Guard deployments with a health check before promoting.",
		Content:       "# Deploy guard\n\nPurpose.\n\n1. Check health.",
		Tools:         []string{"grafana.query"},
		CitedPatterns: []string{"deploy-guard"},
		CitedRuns:     []string{fixtureSession},
	}
}

// newValidateWorld seeds the fake store with the fixture workspace and one
// workspace-tier skill.
func newValidateWorld(t *testing.T) store.Store {
	t.Helper()
	ctx := context.Background()
	st := storefake.New()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{ID: fixtureWorkspace, Slug: "qual", Name: "Qual"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := st.WorkspaceSkills().Create(ctx, &domain.WorkspaceSkill{
		WorkspaceID: fixtureWorkspace,
		Name:        "ws-existing",
		Description: "An existing workspace skill.",
		Source:      domain.SkillSourceAuthored,
	}); err != nil {
		t.Fatalf("create workspace skill: %v", err)
	}
	return st
}

func TestValidateDraftAcceptsValidDraft(t *testing.T) {
	st := newValidateWorld(t)
	v := NewDraftValidator(st.WorkspaceSkills(), mapContentReader(map[string]string{}), staticTools("grafana.query", "files.write"))
	if err := v.ValidateDraft(context.Background(), fixtureWorkspace, fixtureAgent, validDraft(), ""); err != nil {
		t.Fatalf("valid draft rejected: %v", err)
	}
}

func TestValidateDraftRejections(t *testing.T) {
	st := newValidateWorld(t)
	agentSkills := map[string]string{"agent-existing": "# Agent existing\n"}
	v := NewDraftValidator(st.WorkspaceSkills(), mapContentReader(agentSkills), staticTools("grafana.query", "files.write"))
	ctx := context.Background()

	oversize := strings.Repeat("x", MaxSkillContentBytes+1)

	cases := []struct {
		name    string
		mutate  func(d *SkillDraft)
		exempt  string
		wantSub []string // substrings the rejection must mention
	}{
		{
			name:    "uppercase slug",
			mutate:  func(d *SkillDraft) { d.Name = "Deploy-Guard" },
			wantSub: []string{"name:"},
		},
		{
			name:    "underscore slug",
			mutate:  func(d *SkillDraft) { d.Name = "deploy_guard" },
			wantSub: []string{"name:"},
		},
		{
			name:    "leading hyphen",
			mutate:  func(d *SkillDraft) { d.Name = "-deploy" },
			wantSub: []string{"name:"},
		},
		{
			name:    "empty name",
			mutate:  func(d *SkillDraft) { d.Name = "" },
			wantSub: []string{"name:"},
		},
		{
			name:    "reserved system name web-research",
			mutate:  func(d *SkillDraft) { d.Name = "web-research" },
			wantSub: []string{`reserved for a system skill`},
		},
		{
			name:    "reserved system name document-read",
			mutate:  func(d *SkillDraft) { d.Name = "document-read" },
			wantSub: []string{`reserved for a system skill`},
		},
		{
			name:    "workspace-tier collision",
			mutate:  func(d *SkillDraft) { d.Name = "ws-existing" },
			wantSub: []string{"collides with a workspace skill"},
		},
		{
			name:    "agent-tier collision",
			mutate:  func(d *SkillDraft) { d.Name = "agent-existing" },
			wantSub: []string{"collides with an existing agent skill"},
		},
		{
			name:    "empty description",
			mutate:  func(d *SkillDraft) { d.Description = "   " },
			wantSub: []string{"description is empty"},
		},
		{
			name:    "trivial description",
			mutate:  func(d *SkillDraft) { d.Description = "short" },
			wantSub: []string{"description is trivial"},
		},
		{
			name: "oversize description",
			mutate: func(d *SkillDraft) {
				d.Description = strings.Repeat("d", MaxSkillDescriptionChars+1)
			},
			wantSub: []string{"description exceeds"},
		},
		{
			name:    "unknown tool",
			mutate:  func(d *SkillDraft) { d.Tools = []string{"grafana.query", "k8s.reincarnate"} },
			wantSub: []string{`unknown tool "k8s.reincarnate"`},
		},
		{
			name:    "empty tool entry",
			mutate:  func(d *SkillDraft) { d.Tools = []string{" "} },
			wantSub: []string{"empty tool dependency"},
		},
		{
			name:    "empty content",
			mutate:  func(d *SkillDraft) { d.Content = "  " },
			wantSub: []string{"content is empty"},
		},
		{
			name:    "oversize content",
			mutate:  func(d *SkillDraft) { d.Content = oversize },
			wantSub: []string{"content exceeds"},
		},
		{
			name:    "multiple problems aggregate",
			mutate:  func(d *SkillDraft) { d.Name = "Deploy Guard"; d.Tools = []string{"nope"} },
			wantSub: []string{"name:", `unknown tool "nope"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draft := validDraft()
			tc.mutate(&draft)
			err := v.ValidateDraft(ctx, fixtureWorkspace, fixtureAgent, draft, tc.exempt)
			if err == nil {
				t.Fatalf("expected rejection, got nil")
			}
			var rejection *DraftRejection
			if !errors.As(err, &rejection) {
				t.Fatalf("expected *DraftRejection, got %T: %v", err, err)
			}
			for _, sub := range tc.wantSub {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("rejection %q missing %q", err.Error(), sub)
				}
			}
		})
	}
}

func TestValidateDraftExemptNamesTheSupersededSkill(t *testing.T) {
	st := newValidateWorld(t)
	agentSkills := map[string]string{"deploy-guard": "# Deploy guard\n"}
	v := NewDraftValidator(st.WorkspaceSkills(), mapContentReader(agentSkills), staticTools("grafana.query"))
	ctx := context.Background()

	// An edit re-uses the superseded skill's name: the collision checks
	// must exempt exactly that name, at every tier.
	if err := v.ValidateDraft(ctx, fixtureWorkspace, fixtureAgent, validDraft(), "deploy-guard"); err != nil {
		t.Fatalf("edit with exempt name rejected: %v", err)
	}

	// The exemption is narrow: any other collision still rejects.
	other := validDraft()
	other.Name = "ws-existing"
	if err := v.ValidateDraft(ctx, fixtureWorkspace, fixtureAgent, other, "deploy-guard"); err == nil {
		t.Fatal("non-exempt collision accepted despite exemption")
	}
}

func TestValidateDraftStageErrorsAreNotRejections(t *testing.T) {
	st := newValidateWorld(t)
	boom := errors.New("disk exploded")

	// A seam READ failure must surface as a plain error (stage failure),
	// never as a *DraftRejection blaming the draft.
	v := NewDraftValidator(st.WorkspaceSkills(), failingContentReader(boom), staticTools("grafana.query"))
	_, _, err := v.AgentSkillContent(context.Background(), fixtureWorkspace, fixtureAgent, "any")
	if !errors.Is(err, boom) {
		t.Fatalf("reader seam error lost: %v", err)
	}
}

func TestReservedSystemSkillNamesMatchEmbedded(t *testing.T) {
	// The reserved list is a hardcoded mirror of the embedded system skills
	// (internal/agents/systemskills) — this parity check is what keeps the
	// two from drifting apart.
	embedded, err := systemskills.ListEmbedded()
	if err != nil {
		t.Fatalf("list embedded system skills: %v", err)
	}
	slices.Sort(embedded)

	got := slices.Clone(ReservedSystemSkillNames)
	slices.Sort(got)

	if !slices.Equal(got, embedded) {
		t.Errorf("ReservedSystemSkillNames = %v, embedded system skills = %v — update the reserved list", got, embedded)
	}
}

func TestDraftValidatorToolNamesSharedSource(t *testing.T) {
	// The prompt's catalog and the validation's catalog must be the same
	// list — one source, never two diverging copies.
	tools := staticTools("grafana.query", "files.write")
	v := NewDraftValidator(newValidateWorld(t).WorkspaceSkills(), mapContentReader(nil), tools)
	got := v.ToolNames()
	if len(got) != 2 || got[0] != "grafana.query" || got[1] != "files.write" {
		t.Errorf("ToolNames() = %v, want the configured catalog", got)
	}
}
