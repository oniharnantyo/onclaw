package domain_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// validTieredMCPRecipe is a minimal well-formed mcp-kind recipe carrying a
// tool-tier list (add-integration-authority tasks.md 1.1).
func validTieredMCPRecipe() domain.Recipe {
	return domain.Recipe{
		ID:           "tiered",
		Service:      "Tiered",
		Icon:         "tiered",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     "https://mcp.tiered.dev/mcp",
		TokenHeader:  "Authorization",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Create a token"}},
		Probe:        domain.RecipeProbe{Tool: "ping"},
		ToolTiers: []domain.RecipeToolTierRule{
			{Tool: "ping", Tier: domain.RecipeToolTierRead},
			{Tool: "create_widget", Tier: domain.RecipeToolTierWrite},
		},
	}
}

// Verb tiers (add-integration-authority tasks.md 1.1): catalog values only,
// empty legal as the fail-safe write default, and the mcp tool-tier list is
// kind-shaped data an http recipe must not declare.
func TestValidateRecipeVerbTiers(t *testing.T) {
	// A tierless verb is the legal fail-safe shape: it registers and gates as
	// write (the compatibility direction — pre-tier declarations stay valid).
	tierless := validHTTPRecipe()
	if err := domain.ValidateRecipe(&tierless); err != nil {
		t.Fatalf("expected a tierless verb to stay valid, got %v", err)
	}

	for _, tier := range []string{domain.RecipeToolTierRead, domain.RecipeToolTierWrite} {
		r := validHTTPRecipe()
		r.Verbs[0].Tier = tier
		if err := domain.ValidateRecipe(&r); err != nil {
			t.Fatalf("expected tier %q to be valid, got %v", tier, err)
		}
	}

	tests := []struct {
		name   string
		mutate func(r *domain.Recipe)
	}{
		{"unknown tier", func(r *domain.Recipe) { r.Verbs[0].Tier = "admin" }},
		{"case-sensitive tier", func(r *domain.Recipe) { r.Verbs[0].Tier = "Read" }},
		{"mcp tool-tier list on http recipe", func(r *domain.Recipe) {
			r.ToolTiers = []domain.RecipeToolTierRule{{Tool: "anything", Tier: domain.RecipeToolTierRead}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validHTTPRecipe()
			tt.mutate(&r)
			if err := domain.ValidateRecipe(&r); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}

// The mcp-kind tool-tier list (add-integration-authority tasks.md 1.1):
// well-formed unique tool names with catalog tiers — the names must survive
// MCP name sanitization unchanged, so anything else is rejected at
// registration instead of silently gating as write forever.
func TestValidateRecipeToolTierRules(t *testing.T) {
	if err := domain.ValidateRecipe(func() *domain.Recipe { r := validTieredMCPRecipe(); return &r }()); err != nil {
		t.Fatalf("expected valid tiered mcp recipe, got %v", err)
	}

	tests := []struct {
		name   string
		mutate func(r *domain.Recipe)
	}{
		{"duplicate tool", func(r *domain.Recipe) {
			r.ToolTiers = append(r.ToolTiers, domain.RecipeToolTierRule{Tool: "ping", Tier: domain.RecipeToolTierWrite})
		}},
		{"empty tool name", func(r *domain.Recipe) {
			r.ToolTiers = append(r.ToolTiers, domain.RecipeToolTierRule{Tool: "", Tier: domain.RecipeToolTierRead})
		}},
		{"tool name with whitespace", func(r *domain.Recipe) {
			r.ToolTiers = append(r.ToolTiers, domain.RecipeToolTierRule{Tool: "create widget", Tier: domain.RecipeToolTierWrite})
		}},
		{"tool name with a dot", func(r *domain.Recipe) {
			r.ToolTiers = append(r.ToolTiers, domain.RecipeToolTierRule{Tool: "widgets.create", Tier: domain.RecipeToolTierWrite})
		}},
		{"applied runtime name declared", func(r *domain.Recipe) {
			r.ToolTiers = append(r.ToolTiers, domain.RecipeToolTierRule{Tool: "mcp__tiered__ping", Tier: domain.RecipeToolTierRead})
		}},
		{"unknown tier", func(r *domain.Recipe) {
			r.ToolTiers = append(r.ToolTiers, domain.RecipeToolTierRule{Tool: "list_widgets", Tier: "readonly"})
		}},
		{"empty tier", func(r *domain.Recipe) {
			r.ToolTiers = append(r.ToolTiers, domain.RecipeToolTierRule{Tool: "list_widgets"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validTieredMCPRecipe()
			tt.mutate(&r)
			if err := domain.ValidateRecipe(&r); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}

// EffectiveToolTier (add-integration-authority spec: "Verb tier
// declarations"): declared tiers win, everything else — nil recipe, unknown
// tool, undeclared name, altered runtime name — fails safe to write.
func TestEffectiveToolTier(t *testing.T) {
	if got := domain.EffectiveToolTier(nil, "anything"); got != domain.RecipeToolTierWrite {
		t.Errorf("expected nil recipe to default to %s, got %s", domain.RecipeToolTierWrite, got)
	}
	if got := domain.EffectiveToolTier(&domain.Recipe{}, ""); got != domain.RecipeToolTierWrite {
		t.Errorf("expected an empty tool name to default to %s, got %s", domain.RecipeToolTierWrite, got)
	}

	// http kind: the declared verb tiers, verbatim names, write for the rest.
	http := validHTTPRecipe()
	http.Verbs[0].Tier = domain.RecipeToolTierRead
	if got := domain.EffectiveToolTier(&http, "acme.list_widget_parts"); got != domain.RecipeToolTierRead {
		t.Errorf("expected the declared read verb to resolve read, got %s", got)
	}
	if got := domain.EffectiveToolTier(&http, "acme.unknown_verb"); got != domain.RecipeToolTierWrite {
		t.Errorf("expected an unknown http tool to default to %s, got %s", domain.RecipeToolTierWrite, got)
	}
	if got := domain.EffectiveToolTier(&http, "mcp__acme__list_widget_parts"); got != domain.RecipeToolTierWrite {
		t.Errorf("expected an applied-style name to match no http verb (write), got %s", got)
	}
	tierless := validHTTPRecipe()
	if got := domain.EffectiveToolTier(&tierless, "acme.list_widget_parts"); got != domain.RecipeToolTierWrite {
		t.Errorf("expected a tierless verb to gate as %s, got %s", domain.RecipeToolTierWrite, got)
	}

	// mcp kind: raw and applied runtime names both resolve.
	r := validTieredMCPRecipe()
	if got := domain.EffectiveToolTier(&r, "ping"); got != domain.RecipeToolTierRead {
		t.Errorf("expected the raw read tool to resolve read, got %s", got)
	}
	if got := domain.EffectiveToolTier(&r, "create_widget"); got != domain.RecipeToolTierWrite {
		t.Errorf("expected the raw write tool to resolve write, got %s", got)
	}
	if got := domain.EffectiveToolTier(&r, "mcp__tiered__ping"); got != domain.RecipeToolTierRead {
		t.Errorf("expected the applied name to resolve the tool's tier, got %s", got)
	}
	if got := domain.EffectiveToolTier(&r, "mcp__other__ping"); got != domain.RecipeToolTierWrite {
		t.Errorf("expected another server's applied name to default to %s, got %s", domain.RecipeToolTierWrite, got)
	}
	if got := domain.EffectiveToolTier(&r, "ping_2"); got != domain.RecipeToolTierWrite {
		t.Errorf("expected a collision-suffixed runtime name to default to %s, got %s", domain.RecipeToolTierWrite, got)
	}
	if got := domain.EffectiveToolTier(&r, "undclared_tool"); got != domain.RecipeToolTierWrite {
		t.Errorf("expected an undeclared tool to default to %s, got %s", domain.RecipeToolTierWrite, got)
	}
}

// ToolTierCounts sums the declared tiers; undeclared tools gate as write but
// are not counted (the projection is declaration-derived).
func TestToolTierCounts(t *testing.T) {
	if got := domain.ToolTierCounts(nil); got != (domain.RecipeTierCounts{}) {
		t.Errorf("expected zero counts for a nil recipe, got %+v", got)
	}

	http := validHTTPRecipe()
	if got := domain.ToolTierCounts(&http); got.Read != 0 || got.Write != 1 {
		t.Errorf("expected the tierless verb to count write, got %+v", got)
	}
	http.Verbs = append(http.Verbs, domain.RecipeVerb{Tool: "acme.create_widget", Method: "POST", Path: "/v1/widgets", Tier: domain.RecipeToolTierWrite, Description: "Create a widget."})
	http.Verbs[0].Tier = domain.RecipeToolTierRead
	if got := domain.ToolTierCounts(&http); got.Read != 1 || got.Write != 1 {
		t.Errorf("expected a 1/1 declared split, got %+v", got)
	}

	mcp := validTieredMCPRecipe()
	if got := domain.ToolTierCounts(&mcp); got.Read != 1 || got.Write != 1 {
		t.Errorf("expected the tier list's 1/1 split, got %+v", got)
	}
	empty := validTieredMCPRecipe()
	empty.ToolTiers = nil
	if got := domain.ToolTierCounts(&empty); got != (domain.RecipeTierCounts{}) {
		t.Errorf("expected an empty tier list to count zero, got %+v", got)
	}
}

// Built-in tiering (add-integration-authority tasks.md 1.2): figma all-read
// on its verbs, github and gitlab carrying both tiers over their MCP tool
// lists, the coming-soon mcp recipes declaring nothing (all write), and the
// default-write spot check for an unknown tool.
func TestRecipesBuiltinTiers(t *testing.T) {
	figma := domain.RecipeByID("figma")
	if figma == nil {
		t.Fatal("expected the figma recipe to be registered")
	}
	if len(figma.ToolTiers) != 0 {
		t.Errorf("expected no mcp tool-tier list on the http recipe, got %d rules", len(figma.ToolTiers))
	}
	for _, v := range figma.Verbs {
		if v.Tier != domain.RecipeToolTierRead {
			t.Errorf("expected figma verb %s to be %s, got %q", v.Tool, domain.RecipeToolTierRead, v.Tier)
		}
	}
	if got := domain.ToolTierCounts(figma); got.Read != len(figma.Verbs) || got.Write != 0 {
		t.Errorf("expected an all-read %d/0 split for figma, got %+v", len(figma.Verbs), got)
	}
	if got := domain.EffectiveToolTier(figma, "figma.get_file"); got != domain.RecipeToolTierRead {
		t.Errorf("expected figma.get_file to resolve read, got %s", got)
	}
	if got := domain.EffectiveToolTier(figma, "figma.post_comment"); got != domain.RecipeToolTierWrite {
		t.Errorf("expected an undeclared figma tool to default to %s, got %s", domain.RecipeToolTierWrite, got)
	}

	github := domain.RecipeByID("github")
	if github == nil {
		t.Fatal("expected the github recipe to be registered")
	}
	hasRead, hasWrite := false, false
	seenTools := make(map[string]bool, len(github.ToolTiers))
	for _, rule := range github.ToolTiers {
		if !domain.IsValidRecipeToolTier(rule.Tier) {
			t.Errorf("github tool %q carries out-of-catalog tier %q", rule.Tool, rule.Tier)
		}
		if seenTools[rule.Tool] {
			t.Errorf("github tool %q declared twice", rule.Tool)
		}
		seenTools[rule.Tool] = true
		hasRead = hasRead || rule.Tier == domain.RecipeToolTierRead
		hasWrite = hasWrite || rule.Tier == domain.RecipeToolTierWrite
	}
	if !hasRead || !hasWrite {
		t.Errorf("expected github's tier list to carry both tiers, got read=%v write=%v", hasRead, hasWrite)
	}
	// The applied runtime names resolve through the sanitized server segment
	// (the materialized server's display name is the recipe's Service).
	if got := domain.EffectiveToolTier(github, "mcp__github__get_file_contents"); got != domain.RecipeToolTierRead {
		t.Errorf("expected the applied read name to resolve read, got %s", got)
	}
	if got := domain.EffectiveToolTier(github, "mcp__github__merge_pull_request"); got != domain.RecipeToolTierWrite {
		t.Errorf("expected the applied write name to resolve write, got %s", got)
	}
	if got := domain.EffectiveToolTier(github, "some_future_github_tool"); got != domain.RecipeToolTierWrite {
		t.Errorf("expected an unknown github tool to default to %s, got %s", domain.RecipeToolTierWrite, got)
	}

	gitlab := domain.RecipeByID("gitlab")
	if gitlab == nil {
		t.Fatal("expected the gitlab recipe to be registered")
	}
	got := domain.ToolTierCounts(gitlab)
	if got.Read == 0 || got.Write == 0 {
		t.Errorf("expected gitlab's tier list to carry both tiers, got %+v", got)
	}

	for _, id := range []string{"atlassian", "slack", "linear"} {
		r := domain.RecipeByID(id)
		if r == nil {
			t.Fatalf("expected the %s recipe to be registered", id)
		}
		if len(r.ToolTiers) != 0 {
			t.Errorf("expected %s to declare no tier list yet, got %d rules", id, len(r.ToolTiers))
		}
		if got := domain.EffectiveToolTier(r, "any_tool_at_all"); got != domain.RecipeToolTierWrite {
			t.Errorf("expected %s tools to default to %s, got %s", id, domain.RecipeToolTierWrite, got)
		}
	}
}

// The tier declarations ride the served recipe JSON: verb tier always
// present on http verbs, the mcp tier list under tool_tiers.
func TestRecipeTiersWireShape(t *testing.T) {
	data, err := json.Marshal(domain.RecipeByID("figma"))
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if _, exists := wire["tool_tiers"]; exists {
		t.Errorf("expected no tool_tiers key on the http recipe, got %s", data)
	}
	verbs, ok := wire["verbs"].([]any)
	if !ok || len(verbs) == 0 {
		t.Fatalf("expected the figma verbs on the wire, got %s", data)
	}
	verb, _ := verbs[0].(map[string]any)
	if verb["tier"] != domain.RecipeToolTierRead {
		t.Errorf("expected the verb's tier on the wire, got %s", data)
	}

	mcp := validTieredMCPRecipe()
	domain.RegisterRecipe(mcp)
	served := domain.RecipeByID("tiered")
	if served == nil {
		t.Fatal("expected the registered tiered recipe to resolve")
	}
	if err := domain.ValidateRecipe(served); err != nil {
		t.Fatalf("expected the registered tiered recipe to be valid, got %v", err)
	}
	data, err = json.Marshal(served)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	wire = nil
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	rules, ok := wire["tool_tiers"].([]any)
	if !ok || len(rules) != 2 {
		t.Fatalf("expected the tool_tiers list on the wire, got %s", data)
	}
	rule, _ := rules[0].(map[string]any)
	if rule["tool"] != "ping" || rule["tier"] != domain.RecipeToolTierRead {
		t.Errorf("expected the tool/tier rule shape on the wire, got %s", data)
	}
}
