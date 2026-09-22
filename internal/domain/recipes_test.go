package domain_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The v1 registry contents are part of the change contract (design.md D2):
// GitHub and GitLab available, Atlassian (one recipe covering Jira and
// Confluence), Slack, and Linear coming-soon. The registry is package-global,
// so the built-ins' relative order and availability are asserted rather than
// absolute list length — later registrations must not break these.
func TestRecipesBuiltinRegistryContents(t *testing.T) {
	recipes := domain.Recipes()

	byID := make(map[string]domain.Recipe, len(recipes))
	for _, r := range recipes {
		if _, dup := byID[r.ID]; dup {
			t.Errorf("recipe id %q registered twice", r.ID)
		}
		byID[r.ID] = r
	}

	wantOrder := []string{"github", "gitlab", "atlassian", "slack", "linear", "figma"}
	gotOrder := make([]string, 0, len(wantOrder))
	for _, r := range recipes {
		if slices.Contains(wantOrder, r.ID) {
			gotOrder = append(gotOrder, r.ID)
		}
	}
	if !slices.Equal(wantOrder, gotOrder) {
		t.Fatalf("expected built-in registration order %v, got %v", wantOrder, gotOrder)
	}

	available := map[string]bool{"github": true, "gitlab": true}
	comingSoon := map[string]bool{"atlassian": true, "slack": true, "linear": true}
	for id := range available {
		r := byID[id]
		if r.Availability != domain.RecipeAvailable {
			t.Errorf("expected %s to be %s, got %q", id, domain.RecipeAvailable, r.Availability)
		}
		if r.AuthKind != domain.RecipeAuthPAT {
			t.Errorf("expected %s to be %s auth, got %q", id, domain.RecipeAuthPAT, r.AuthKind)
		}
		if r.Endpoint == "" {
			t.Errorf("expected %s to declare a remote endpoint", id)
		}
		if r.TokenHeader == "" {
			t.Errorf("expected %s to declare the token header", id)
		}
		if len(r.Steps) == 0 {
			t.Errorf("expected %s to declare guided setup steps", id)
		}
		if !slices.Contains(r.AccessLevels, domain.ConnectionAccessReadOnly) ||
			!slices.Contains(r.AccessLevels, domain.ConnectionAccessReadWrite) {
			t.Errorf("expected %s to support both access levels, got %v", id, r.AccessLevels)
		}
	}
	for id := range comingSoon {
		r := byID[id]
		if r.Availability != domain.RecipeComingSoon {
			t.Errorf("expected %s to be %s, got %q", id, domain.RecipeComingSoon, r.Availability)
		}
		if r.Notes == "" {
			t.Errorf("expected %s to carry truthful coming-soon copy", id)
		}
	}

	// GitLab targets gitlab.com (design.md D2: self-managed excluded from v1).
	if gl := byID["gitlab"]; !strings.Contains(gl.Endpoint, "gitlab.com") {
		t.Errorf("expected gitlab endpoint to target gitlab.com, got %q", gl.Endpoint)
	}

	// Atlassian is one recipe covering both Jira and Confluence.
	if _, exists := byID["jira"]; exists {
		t.Error("expected no separate jira recipe — Atlassian covers both products")
	}
	if _, exists := byID["confluence"]; exists {
		t.Error("expected no separate confluence recipe — Atlassian covers both products")
	}

	// Scope guidance is declared per access level, including read-only.
	for _, id := range []string{"github", "gitlab"} {
		r := byID[id]
		var hasReadOnly bool
		for _, s := range r.Scopes {
			if s.AccessLevel == domain.ConnectionAccessReadOnly && len(s.Scopes) > 0 {
				hasReadOnly = true
			}
		}
		if !hasReadOnly {
			t.Errorf("expected %s to declare recommended read-only scopes", id)
		}
	}

	// Every declared recipe passes its own shape validation.
	for _, r := range recipes {
		if err := domain.ValidateRecipe(&r); err != nil {
			t.Errorf("expected built-in recipe %s to be valid, got %v", r.ID, err)
		}
	}

	// OAuth built-ins (add-connection-oauth tasks.md 1.1) declare the full
	// flow: both endpoints, the token row, scopes per access level, and the
	// app-registration guidance. Availability stays coming_soon in the
	// declaration — the gallery flips it at runtime when the instance app is
	// registered.
	oauthFields := map[string]bool{"atlassian": true, "slack": true, "linear": true}
	for id := range oauthFields {
		r := byID[id]
		if r.AuthKind != domain.RecipeAuthOAuth {
			t.Errorf("expected %s to be %s auth, got %q", id, domain.RecipeAuthOAuth, r.AuthKind)
		}
		if r.AuthorizeURL == "" || r.TokenURL == "" {
			t.Errorf("expected %s to declare the OAuth endpoints", id)
		}
		if r.TokenHeader == "" {
			t.Errorf("expected %s to declare the token header", id)
		}
		if r.AppRegistrationGuidance == "" {
			t.Errorf("expected %s to declare app-registration guidance", id)
		}
		if len(r.Scopes) == 0 {
			t.Errorf("expected %s to declare consent scopes", id)
		}
		for _, s := range r.Scopes {
			if len(s.Scopes) == 0 {
				t.Errorf("expected %s to declare non-empty scopes for %q", id, s.AccessLevel)
			}
		}
	}

	// Atlassian is the reference recipe: one connection covering Jira and
	// Confluence, refreshable (offline_access), with a read-only tier that
	// stays strictly narrower than read-and-write.
	atl := byID["atlassian"]
	var ro, rw []string
	for _, s := range atl.Scopes {
		switch s.AccessLevel {
		case domain.ConnectionAccessReadOnly:
			ro = s.Scopes
		case domain.ConnectionAccessReadWrite:
			rw = s.Scopes
		}
	}
	if len(ro) == 0 || len(rw) == 0 {
		t.Fatalf("expected atlassian to declare both scope tiers, got ro=%v rw=%v", ro, rw)
	}
	if !slices.Contains(ro, "offline_access") {
		t.Errorf("expected atlassian read-only scopes to include offline_access, got %v", ro)
	}
	for _, s := range ro {
		if !slices.Contains(rw, s) {
			t.Errorf("expected read-write tier to include read-only scope %q, got %v", s, rw)
		}
	}
}

func TestRecipeByID(t *testing.T) {
	if r := domain.RecipeByID("github"); r == nil || r.Service != "GitHub" {
		t.Fatalf("expected the github recipe, got %+v", r)
	}
	if r := domain.RecipeByID("GITHUB"); r != nil {
		t.Fatalf("recipe ids are exact, got %+v", r)
	}
	if r := domain.RecipeByID("figma"); r == nil || r.Service != "Figma" {
		t.Fatalf("expected the figma recipe, got %+v", r)
	}
	if r := domain.RecipeByID("notarecipe"); r != nil {
		t.Fatalf("expected nil for an unregistered recipe id, got %+v", r)
	}
}

// Recipes and RecipeByID return copies: mutating them must not affect the
// registry.
func TestRecipesReturnsCopy(t *testing.T) {
	recipes := domain.Recipes()
	if len(recipes) == 0 {
		t.Fatal("expected registered recipes")
	}
	recipes[0].Service = "MUTATED"
	if domain.Recipes()[0].Service == "MUTATED" {
		t.Fatal("expected Recipes() to return copies, not registry internals")
	}

	byID := domain.RecipeByID(recipes[0].ID)
	byID.Service = "MUTATED"
	if domain.RecipeByID(recipes[0].ID).Service == "MUTATED" {
		t.Fatal("expected RecipeByID to return a copy, not registry internals")
	}
}

func TestValidateRecipe(t *testing.T) {
	valid := domain.Recipe{
		ID:           "figma",
		Service:      "Figma",
		Icon:         "figma",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     "https://mcp.figma.com/mcp",
		TokenHeader:  "Authorization",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Create a token"}},
		Probe:        domain.RecipeProbe{Tool: "list_files"},
	}
	if err := domain.ValidateRecipe(&valid); err != nil {
		t.Fatalf("expected valid recipe, got %v", err)
	}
	if err := domain.ValidateRecipe(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil recipe, got %v", err)
	}

	tests := []struct {
		name   string
		mutate func(r *domain.Recipe)
	}{
		{"empty id", func(r *domain.Recipe) { r.ID = "" }},
		{"empty service", func(r *domain.Recipe) { r.Service = "" }},
		{"unknown auth kind", func(r *domain.Recipe) { r.AuthKind = "magic" }},
		{"unknown availability", func(r *domain.Recipe) { r.Availability = "maybe" }},
		{"unknown transport", func(r *domain.Recipe) { r.Transport = "carrier-pigeon" }},
		{"missing endpoint for url transport", func(r *domain.Recipe) { r.Endpoint = "" }},
		{"missing token header for pat auth", func(r *domain.Recipe) { r.TokenHeader = "" }},
		{"no access levels", func(r *domain.Recipe) { r.AccessLevels = nil }},
		{"unknown access level", func(r *domain.Recipe) {
			r.AccessLevels = []string{"sudo"}
		}},
		{"no steps for available recipe", func(r *domain.Recipe) { r.Steps = nil }},
		{"empty probe tool", func(r *domain.Recipe) { r.Probe.Tool = " " }},
		{"scopes naming an unsupported level", func(r *domain.Recipe) {
			r.Scopes = []domain.RecipeScopes{{AccessLevel: domain.ConnectionAccessReadWrite, Scopes: []string{"x"}}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := valid
			tt.mutate(&r)
			if err := domain.ValidateRecipe(&r); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}

	// A coming-soon recipe may ship without guided steps (there is no connect
	// flow to guide yet).
	comingSoon := valid
	comingSoon.Availability = domain.RecipeComingSoon
	comingSoon.Steps = nil
	if err := domain.ValidateRecipe(&comingSoon); err != nil {
		t.Fatalf("expected coming-soon recipe without steps to be valid, got %v", err)
	}

	// A stdio recipe carries a command instead of an endpoint (design.md D2:
	// stdio recipes are representable).
	stdio := valid
	stdio.Transport = domain.MCPTransportStdio
	stdio.Endpoint = ""
	stdio.Command = "github-mcp-server"
	if err := domain.ValidateRecipe(&stdio); err != nil {
		t.Fatalf("expected stdio recipe with command to be valid, got %v", err)
	}
	if err := domain.ValidateRecipe(func() *domain.Recipe { r := stdio; r.Command = ""; return &r }()); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for stdio recipe without command, got %v", err)
	}
}

// OAuth recipe shapes (add-connection-oauth tasks.md 1.1): both flow
// endpoints and the token row are required, and the OAuth fields are
// auth-kind-shaped data a PAT recipe must not declare.
func TestValidateRecipeOAuth(t *testing.T) {
	valid := domain.Recipe{
		ID:                      "asana",
		Service:                 "Asana",
		Icon:                    "asana",
		AuthKind:                domain.RecipeAuthOAuth,
		Availability:            domain.RecipeComingSoon,
		Transport:               domain.MCPTransportStreamableHTTP,
		Endpoint:                "https://mcp.asana.com/mcp",
		TokenHeader:             "Authorization",
		TokenScheme:             "Bearer",
		AuthorizeURL:            "https://app.asana.com/-/oauth_authorization_intermediate",
		TokenURL:                "https://app.asana.com/-/oauth_token",
		AccessLevels:            []string{domain.ConnectionAccessReadOnly},
		Scopes:                  []domain.RecipeScopes{{AccessLevel: domain.ConnectionAccessReadOnly, Scopes: []string{"default"}}},
		AppRegistrationGuidance: "Create an Asana service app.",
		Probe:                   domain.RecipeProbe{Tool: "list_workspaces"},
	}
	if err := domain.ValidateRecipe(&valid); err != nil {
		t.Fatalf("expected valid oauth recipe, got %v", err)
	}
	if valid.RefreshMargin != 0 {
		t.Fatal("expected zero RefreshMargin to be the declared (default-margin) shape")
	}
	if domain.DefaultRecipeRefreshMargin <= 0 {
		t.Fatal("expected a positive default refresh margin")
	}

	tests := []struct {
		name   string
		mutate func(r *domain.Recipe)
	}{
		{"missing authorize url", func(r *domain.Recipe) { r.AuthorizeURL = "" }},
		{"missing token url", func(r *domain.Recipe) { r.TokenURL = "" }},
		{"missing token header", func(r *domain.Recipe) { r.TokenHeader = "" }},
		{"pat recipe declaring oauth endpoints", func(r *domain.Recipe) {
			r.AuthKind = domain.RecipeAuthPAT
			r.Steps = []domain.RecipeStep{{Title: "Create a token"}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := valid
			tt.mutate(&r)
			if err := domain.ValidateRecipe(&r); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}

	// The OAuth-only JSON contract: authorize_url, token_url, and
	// app_registration_guidance are served; the refresh margin is lifecycle
	// tuning and is deliberately absent from the wire shape.
	data, err := json.Marshal(&valid)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	for _, key := range []string{"authorize_url", "token_url", "app_registration_guidance"} {
		if _, ok := wire[key]; !ok {
			t.Errorf("expected %q in the served recipe JSON, got %s", key, data)
		}
	}
	if _, ok := wire["refresh_margin"]; ok {
		t.Errorf("expected refresh_margin to stay off the wire, got %s", data)
	}
}

func TestRegisterRecipe(t *testing.T) {
	base := domain.Recipe{
		ID:           "registry-probe-service",
		Service:      "Registry Probe",
		Icon:         "probe",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Transport:    domain.MCPTransportStdio,
		Command:      "probe-mcp-server",
		TokenHeader:  "PROBE_TOKEN",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "step"}},
		Probe:        domain.RecipeProbe{Tool: "ping"},
	}

	// RegisterRecipe panics on an invalid recipe (a startup-time programming
	// error, mirroring the store driver registry). Probed via a deferred
	// recover so the suite continues cleanly.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic on invalid recipe registration")
			}
		}()
		invalid := base
		invalid.Command = "" // stdio recipe without a command
		domain.RegisterRecipe(invalid)
	}()

	// Duplicate registration panics; a successful registration is verified
	// first so the duplicate is meaningful.
	domain.RegisterRecipe(base)
	if got := domain.RecipeByID("registry-probe-service"); got == nil || got.Command != "probe-mcp-server" {
		t.Fatalf("expected the registered stdio recipe to resolve, got %+v", got)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic on duplicate recipe registration")
			}
		}()
		domain.RegisterRecipe(base)
	}()
}
