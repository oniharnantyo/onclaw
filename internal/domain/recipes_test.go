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
		// The URL surface is kind-shaped: an mcp endpoint or an http base url.
		if r.Endpoint == "" && r.BaseURL == "" {
			t.Errorf("expected %s to declare a remote endpoint or base url", id)
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

	// GitLab targets gitlab.com as its declared default origin
	// (add-recipe-base-url tasks.md 3.1: the REST re-scope keeps the SaaS
	// origin as the parameter default).
	if gl := byID["gitlab"]; gl.OriginParam == nil || gl.OriginParam.Default != "https://gitlab.com" {
		t.Errorf("expected gitlab's origin parameter to default to gitlab.com, got %+v", gl.OriginParam)
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

// ParseOrigin (add-recipe-base-url tasks.md 1.2): origin-only values —
// scheme, host, optional port — with one deterministic normalized form,
// because the resolved origin keys the (service, origin) connection
// uniqueness comparison.
func TestParseOrigin(t *testing.T) {
	valid := []struct {
		name string
		raw  string
		want string
	}{
		{"https origin", "https://gitlab.com", "https://gitlab.com"},
		{"http origin with port", "http://gitlab.internal:8080", "http://gitlab.internal:8080"},
		{"trailing slash stripped", "https://gitlab.com/", "https://gitlab.com"},
		{"scheme lowercased", "HTTPS://gitlab.com", "https://gitlab.com"},
		{"host lowercased", "https://GitLab.Example.Com", "https://gitlab.example.com"},
		{"port preserved across normalization", "https://gitlab.example.com:8443/", "https://gitlab.example.com:8443"},
		{"ipv6 host", "http://[::1]:3000", "http://[::1]:3000"},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ParseOrigin(tt.raw)
			if err != nil {
				t.Fatalf("expected %q to parse, got %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}

	rejected := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"missing scheme", "gitlab.example.com"},
		{"wrong scheme", "ftp://gitlab.example.com"},
		{"non-http scheme", "mailto:user@gitlab.com"},
		{"missing host", "https://"},
		{"opaque form", "http:opaque"},
		{"path", "https://gitlab.com/api/v4"},
		{"deeper path", "https://gitlab.com/api/v4/mcp"},
		{"escaped path", "https://gitlab.com/%2F"},
		{"double slash path", "https://gitlab.com//"},
		{"query", "https://gitlab.com?x=1"},
		{"fragment", "https://gitlab.com#frag"},
		{"userinfo", "https://user@gitlab.com"},
		{"userinfo with password", "https://user:pass@gitlab.com"},
		{"space in host", "https://git lab.com"},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ParseOrigin(tt.raw)
			if err == nil {
				t.Fatalf("expected %q to be rejected, got %q", tt.raw, got)
			}
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}

	// Different spellings of one origin normalize to the same value — the
	// uniqueness comparison's precondition.
	for _, spellings := range [][]string{
		{"https://gitlab.com", "https://gitlab.com/", "HTTPS://GITLAB.COM/", "https://GITLAB.com"},
		{"http://git.example.com:8443", "http://Git.Example.COM:8443/"},
	} {
		first, err := domain.ParseOrigin(spellings[0])
		if err != nil {
			t.Fatalf("expected %q to parse, got %v", spellings[0], err)
		}
		for _, s := range spellings[1:] {
			got, err := domain.ParseOrigin(s)
			if err != nil {
				t.Fatalf("expected %q to parse, got %v", s, err)
			}
			if got != first {
				t.Fatalf("expected %q to normalize to %q, got %q", s, first, got)
			}
		}
	}
}

// The origin parameter's declared shape (add-recipe-base-url tasks.md 1.1):
// PAT-kind recipes only, a parseable default origin, a URL surface to resolve
// against (no stdio), and — the resolved origin replacing BaseURL wholesale
// on http kind — a bare-origin base url there. The OAuth prohibition is the
// change's declared non-goal.
func TestValidateRecipeOriginParam(t *testing.T) {
	mcp := domain.Recipe{
		ID:           "gitea",
		Service:      "Gitea",
		Icon:         "gitea",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     "https://gitea.com/mcp/",
		TokenHeader:  "Authorization",
		TokenScheme:  "Bearer",
		OriginParam: &domain.RecipeOriginParam{
			Name:    "base_url",
			Default: "https://gitea.com",
			Help:    "The Gitea deployment origin — gitea.com or your self-managed instance.",
		},
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Create a token"}},
		Probe:        domain.RecipeProbe{Tool: "ping"},
	}
	if err := domain.ValidateRecipe(&mcp); err != nil {
		t.Fatalf("expected valid parametrized mcp recipe, got %v", err)
	}

	// The parameter is legal on http kind too — the GitLab REST re-scope
	// needs exactly that combination.
	http := validHTTPRecipe()
	http.OriginParam = &domain.RecipeOriginParam{Name: "base_url", Default: "https://api.acme.dev", Help: "The Acme deployment origin."}
	if err := domain.ValidateRecipe(&http); err != nil {
		t.Fatalf("expected valid parametrized http recipe, got %v", err)
	}

	tests := []struct {
		name   string
		mutate func(r *domain.Recipe)
	}{
		{"oauth auth kind", func(r *domain.Recipe) {
			r.AuthKind = domain.RecipeAuthOAuth
			r.AuthorizeURL = "https://gitea.com/login/oauth/authorize"
			r.TokenURL = "https://gitea.com/login/oauth/token"
		}},
		{"stdio transport", func(r *domain.Recipe) {
			r.Transport = domain.MCPTransportStdio
			r.Endpoint = ""
			r.Command = "gitea-mcp"
		}},
		{"empty name", func(r *domain.Recipe) { r.OriginParam.Name = " " }},
		{"empty help", func(r *domain.Recipe) { r.OriginParam.Help = "" }},
		{"default missing scheme", func(r *domain.Recipe) { r.OriginParam.Default = "gitea.com" }},
		{"default with path", func(r *domain.Recipe) { r.OriginParam.Default = "https://gitea.com/mcp" }},
		{"default with userinfo", func(r *domain.Recipe) { r.OriginParam.Default = "https://user@gitea.com" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fresh fixture per case: the recipe copy shares its OriginParam
			// pointer with mcp, so table mutations must not alias.
			r := mcp
			origin := *r.OriginParam
			r.OriginParam = &origin
			tt.mutate(&r)
			if err := domain.ValidateRecipe(&r); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}

	// On http kind a path-prefixed base url under an origin parameter would
	// lose its path at resolution — a registration-time error.
	prefixed := validHTTPRecipe()
	prefixed.BaseURL = "https://api.acme.dev/api"
	prefixed.OriginParam = &domain.RecipeOriginParam{Name: "base_url", Default: "https://api.acme.dev", Help: "The Acme deployment origin."}
	if err := domain.ValidateRecipe(&prefixed); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a path-prefixed base url under an origin parameter, got %v", err)
	}

	// The origin parameter is served — the connect dialog renders the field
	// from it — and stays off the wire when undeclared.
	data, err := json.Marshal(&mcp)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	param, ok := wire["origin_param"].(map[string]any)
	if !ok || param["name"] != "base_url" || param["default"] != "https://gitea.com" ||
		param["help"] != mcp.OriginParam.Help {
		t.Errorf("expected the origin parameter on the wire, got %s", data)
	}
	plain := mcp
	plain.OriginParam = nil
	data, err = json.Marshal(&plain)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	wire = nil
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if _, ok := wire["origin_param"]; ok {
		t.Errorf("expected origin_param to stay off the wire when undeclared, got %s", data)
	}
}

// Endpoint resolution (add-recipe-base-url tasks.md 1.3): the recipe's own
// path constants stay authoritative — the resolved origin substitutes the
// pinned origin only — and recipes without an origin parameter resolve
// byte-identically to their declared constants.
func TestResolveRecipeBase(t *testing.T) {
	parametrizedMCP := &domain.Recipe{
		ID:          "gitea",
		AuthKind:    domain.RecipeAuthPAT,
		Kind:        domain.RecipeKindMCP,
		Transport:   domain.MCPTransportStreamableHTTP,
		Endpoint:    "https://gitea.com/mcp/",
		OriginParam: &domain.RecipeOriginParam{Name: "base_url", Default: "https://gitea.com", Help: "The Gitea deployment origin."},
	}

	// Custom origin: the endpoint's path constant rides along verbatim,
	// trailing slash included.
	got, err := domain.ResolveRecipeBase(parametrizedMCP, "https://git.acme.corp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://git.acme.corp/mcp/" {
		t.Fatalf("expected the custom origin with the declared endpoint path, got %q", got)
	}

	// The connect-time origin normalizes like the default does.
	got, err = domain.ResolveRecipeBase(parametrizedMCP, "https://git.acme.corp/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://git.acme.corp/mcp/" {
		t.Fatalf("expected the normalized origin with the declared endpoint path, got %q", got)
	}

	// An empty origin field falls back to the declared SaaS default.
	got, err = domain.ResolveRecipeBase(parametrizedMCP, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://gitea.com/mcp/" {
		t.Fatalf("expected the default-origin endpoint, got %q", got)
	}

	// A non-origin connect-time value is rejected.
	if _, err := domain.ResolveRecipeBase(parametrizedMCP, "git.acme.corp"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}

	// Http kind resolves to the base the declared probe and verb paths join
	// against — the rooted path constants are appended unchanged.
	parametrizedHTTP := validHTTPRecipe()
	parametrizedHTTP.OriginParam = &domain.RecipeOriginParam{Name: "base_url", Default: "https://api.acme.dev", Help: "The Acme deployment origin."}
	base, err := domain.ResolveRecipeBase(&parametrizedHTTP, "https://acme.eu.pvt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != "https://acme.eu.pvt" {
		t.Fatalf("expected the resolved origin as the verb base, got %q", base)
	}
	if full := base + parametrizedHTTP.Verbs[0].Path; full != "https://acme.eu.pvt/v1/widgets/{widget_id}/parts" {
		t.Fatalf("expected the declared verb path joined against the base, got %q", full)
	}
	if full := base + parametrizedHTTP.Probe.Path; full != "https://acme.eu.pvt/v1/ping" {
		t.Fatalf("expected the declared probe path joined against the base, got %q", full)
	}
	if base, err = domain.ResolveRecipeBase(&parametrizedHTTP, ""); err != nil || base != "https://api.acme.dev" {
		t.Fatalf("expected the default origin as the verb base, got %q err %v", base, err)
	}

	// Non-parametrized recipes resolve byte-identically to their declared
	// constants — exactly today's materialization, no normalization.
	fixedMCP := &domain.Recipe{ID: "fixed", Transport: domain.MCPTransportStreamableHTTP, Endpoint: "https://MCP.Example.COM/mcp"}
	got, err = domain.ResolveRecipeBase(fixedMCP, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != fixedMCP.Endpoint {
		t.Fatalf("expected byte-identical endpoint passthrough, got %q", got)
	}
	fixedHTTP := validHTTPRecipe()
	fixedHTTP.BaseURL = "https://api.acme.dev/api/prefix"
	got, err = domain.ResolveRecipeBase(&fixedHTTP, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != fixedHTTP.BaseURL {
		t.Fatalf("expected byte-identical base-url passthrough, got %q", got)
	}

	// Stdio recipes have no URL surface to resolve.
	stdio := &domain.Recipe{ID: "stdioish", Kind: domain.RecipeKindMCP, Transport: domain.MCPTransportStdio, Command: "stdio-mcp"}
	if _, err := domain.ResolveRecipeBase(stdio, ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}

	if _, err := domain.ResolveRecipeBase(nil, "https://x.example"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a nil recipe, got %v", err)
	}
}

// The registry's returns-copies guarantee extends to the origin-parameter
// declaration pointer.
func TestRecipesReturnsOriginParamCopy(t *testing.T) {
	fixture := domain.Recipe{
		ID:           "origin-copy-probe",
		Service:      "Origin Copy Probe",
		Icon:         "probe",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     "https://mcp.origin-copy-probe.dev/mcp",
		TokenHeader:  "PROBE_TOKEN",
		OriginParam:  &domain.RecipeOriginParam{Name: "base_url", Default: "https://origin-copy-probe.dev", Help: "The probe deployment origin."},
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "step"}},
		Probe:        domain.RecipeProbe{Tool: "ping"},
	}
	domain.RegisterRecipe(fixture)
	fixture.OriginParam.Default = "https://caller-mutated.example.com"
	if got := domain.RecipeByID("origin-copy-probe"); got == nil || got.OriginParam.Default != "https://origin-copy-probe.dev" {
		t.Fatalf("expected the registered declaration to be detached from the caller's object, got %+v", got)
	}
	served := domain.RecipeByID("origin-copy-probe")
	served.OriginParam.Default = "https://served-mutated.example.com"
	if got := domain.RecipeByID("origin-copy-probe"); got.OriginParam.Default != "https://origin-copy-probe.dev" {
		t.Fatal("expected RecipeByID to return a copy of the origin-parameter declaration, not registry internals")
	}
}

// The built-in github recipe declares the base-URL parameter
// (add-recipe-base-url tasks.md 4.1/4.2): the SaaS endpoint is the default,
// a GitHub Enterprise Cloud data-residency origin joins the fixed /mcp/ path
// (the materialized server URL derivation), and the guidance names the
// ghe.com pattern and GHES's missing remote MCP.
func TestRecipesBuiltinGitHubOriginParam(t *testing.T) {
	github := domain.RecipeByID("github")
	if github == nil {
		t.Fatal("expected the github recipe to be registered")
	}
	if github.OriginParam == nil {
		t.Fatal("expected github to declare an origin parameter")
	}
	if github.OriginParam.Default != "https://api.githubcopilot.com" {
		t.Errorf("expected the SaaS endpoint as the declared default, got %q", github.OriginParam.Default)
	}
	if github.OriginParam.Name == "" || github.OriginParam.Help == "" {
		t.Errorf("expected a labeled field with help copy, got %+v", github.OriginParam)
	}
	// The ghe.com data-residency pattern and the GHES unsupported-remote
	// status are the guidance's facts.
	for _, want := range []string{"copilot-api.<subdomain>.ghe.com", "Enterprise Server"} {
		if !strings.Contains(github.OriginParam.Help, want) {
			t.Errorf("expected the guidance to mention %q, got %q", want, github.OriginParam.Help)
		}
	}

	// Materialized server URL derivation (tasks.md 4.2): a ghe.com-style
	// origin joins the endpoint's fixed /mcp/ path; the empty submission
	// falls back to the SaaS endpoint.
	base, err := domain.ResolveRecipeBase(github, "https://copilot-api.acme.ghe.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != "https://copilot-api.acme.ghe.com/mcp/" {
		t.Errorf("expected the ghe.com origin joined with the /mcp/ path, got %q", base)
	}
	if base, err = domain.ResolveRecipeBase(github, ""); err != nil || base != github.Endpoint {
		t.Errorf("expected the default-origin endpoint %q, got %q (%v)", github.Endpoint, base, err)
	}
}
