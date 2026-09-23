package domain_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// validHTTPRecipe is a minimal well-formed http-kind recipe (add-connection-http
// tasks.md 1.1): pinned base URL, auth header, literal probe call, and one
// declared verb.
func validHTTPRecipe() domain.Recipe {
	return domain.Recipe{
		ID:           "acme",
		Service:      "Acme",
		Icon:         "acme",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Kind:         domain.RecipeKindHTTP,
		BaseURL:      "https://api.acme.dev",
		TokenHeader:  "X-Acme-Token",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Create a token"}},
		Probe:        domain.RecipeProbe{Method: "GET", Path: "/v1/ping"},
		Verbs: []domain.RecipeVerb{{
			Tool:   "acme.list_widget_parts",
			Method: "GET",
			Path:   "/v1/widgets/{widget_id}/parts",
			Params: []domain.RecipeVerbParam{
				{Name: "widget_id", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath},
				{Name: "limit", Type: domain.RecipeParamNumber, Required: false, In: domain.RecipeParamInQuery},
			},
			Description: "List the parts of a widget.",
		}},
	}
}

// The http kind requires its full request surface — base URL, auth header,
// literal probe call, at least one verb — and accepts none of the mcp kind's
// materialization fields (add-connection-http tasks.md 1.1; the
// auth-kind-shaped-data mirror of the change-2 OAuth precedent).
func TestValidateRecipeHTTPKind(t *testing.T) {
	if err := domain.ValidateRecipe(func() *domain.Recipe { r := validHTTPRecipe(); return &r }()); err != nil {
		t.Fatalf("expected valid http recipe, got %v", err)
	}

	tests := []struct {
		name   string
		mutate func(r *domain.Recipe)
	}{
		{"missing base url", func(r *domain.Recipe) { r.BaseURL = "" }},
		{"relative base url", func(r *domain.Recipe) { r.BaseURL = "api.acme.dev/v1" }},
		{"non-http scheme", func(r *domain.Recipe) { r.BaseURL = "ftp://api.acme.dev" }},
		{"base url with query", func(r *domain.Recipe) { r.BaseURL = "https://api.acme.dev?x=1" }},
		{"base url with fragment", func(r *domain.Recipe) { r.BaseURL = "https://api.acme.dev#v1" }},
		{"missing auth header", func(r *domain.Recipe) { r.TokenHeader = "" }},
		{"missing probe method", func(r *domain.Recipe) { r.Probe.Method = "" }},
		{"missing probe path", func(r *domain.Recipe) { r.Probe.Path = "" }},
		{"probe path with placeholder", func(r *domain.Recipe) { r.Probe.Path = "/v1/{id}" }},
		{"mcp probe tool on http recipe", func(r *domain.Recipe) { r.Probe.Tool = "ping" }},
		{"no verbs", func(r *domain.Recipe) { r.Verbs = nil }},
		{"mcp transport on http recipe", func(r *domain.Recipe) { r.Transport = domain.MCPTransportStreamableHTTP }},
		{"mcp endpoint on http recipe", func(r *domain.Recipe) { r.Endpoint = "https://mcp.acme.dev/mcp" }},
		{"mcp command on http recipe", func(r *domain.Recipe) { r.Command = "acme-mcp" }},
		{"oauth auth on http recipe", func(r *domain.Recipe) {
			r.AuthKind = domain.RecipeAuthOAuth
			r.AuthorizeURL = "https://acme.dev/authorize"
			r.TokenURL = "https://acme.dev/token"
		}},
		{"no guided steps for available recipe", func(r *domain.Recipe) { r.Steps = nil }},
		{"duplicate verb tools", func(r *domain.Recipe) { r.Verbs = append(r.Verbs, r.Verbs[0]) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A fresh fixture per case: recipe structs share their verb
			// slices' backing arrays, so table mutations must not alias.
			r := validHTTPRecipe()
			tt.mutate(&r)
			if err := domain.ValidateRecipe(&r); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}

	// Non-GET methods are declarable — the verb list is curated recipe data,
	// and a write verb may join through a later recipe release.
	post := validHTTPRecipe()
	post.Verbs = append(post.Verbs, domain.RecipeVerb{
		Tool:        "acme.create_widget",
		Method:      "POST",
		Path:        "/v1/widgets",
		Params:      []domain.RecipeVerbParam{{Name: "team_id", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInQuery}},
		Description: "Create a widget.",
	})
	if err := domain.ValidateRecipe(&post); err != nil {
		t.Fatalf("expected http recipe with a POST verb to be valid, got %v", err)
	}

	// A base URL with a path prefix is legitimate (APIs mounted under /api).
	prefixed := validHTTPRecipe()
	prefixed.BaseURL = "https://api.acme.dev/api"
	if err := domain.ValidateRecipe(&prefixed); err != nil {
		t.Fatalf("expected base url with a path prefix to be valid, got %v", err)
	}
}

// Declared verbs are the whole agent-facing surface, so their shape is
// validated hard: service-prefixed tool names, canonical methods, rooted path
// templates whose every {placeholder} is a declared path parameter and whose
// every declared path parameter is used, and the minimal typed parameter
// schema (add-connection-http tasks.md 1.1).
func TestValidateRecipeVerbs(t *testing.T) {
	mutateVerb := func(f func(v *domain.RecipeVerb)) func(r *domain.Recipe) {
		return func(r *domain.Recipe) { f(&r.Verbs[0]) }
	}
	tests := []struct {
		name   string
		mutate func(r *domain.Recipe)
	}{
		{"unprefixed tool name", mutateVerb(func(v *domain.RecipeVerb) { v.Tool = "list_widget_parts" })},
		{"wrong service prefix", mutateVerb(func(v *domain.RecipeVerb) { v.Tool = "other.list_widget_parts" })},
		{"empty verb suffix", mutateVerb(func(v *domain.RecipeVerb) { v.Tool = "acme." })},
		{"whitespace in tool name", mutateVerb(func(v *domain.RecipeVerb) { v.Tool = "acme.list widget_parts" })},
		{"non-canonical method", mutateVerb(func(v *domain.RecipeVerb) { v.Method = "get" })},
		{"unknown method", mutateVerb(func(v *domain.RecipeVerb) { v.Method = "FETCH" })},
		{"path not rooted", mutateVerb(func(v *domain.RecipeVerb) { v.Path = "v1/widgets/{widget_id}/parts" })},
		{"path carries query", mutateVerb(func(v *domain.RecipeVerb) { v.Path = "/v1/widgets?x=1" })},
		{"path carries fragment", mutateVerb(func(v *domain.RecipeVerb) { v.Path = "/v1/widgets#x" })},
		{"unclosed placeholder", mutateVerb(func(v *domain.RecipeVerb) { v.Path = "/v1/widgets/{widget_id" })},
		{"stray closing brace", mutateVerb(func(v *domain.RecipeVerb) { v.Path = "/v1/widgets/}/parts" })},
		{"nested placeholder", mutateVerb(func(v *domain.RecipeVerb) { v.Path = "/v1/{widget{inner}}" })},
		{"empty placeholder", mutateVerb(func(v *domain.RecipeVerb) { v.Path = "/v1/widgets/{}" })},
		{"undeclared placeholder", mutateVerb(func(v *domain.RecipeVerb) { v.Path = "/v1/widgets/{widget_id}/parts/{part_id}" })},
		{"declared path parameter unused", mutateVerb(func(v *domain.RecipeVerb) {
			v.Params = append(v.Params, domain.RecipeVerbParam{Name: "orphan", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath})
		})},
		{"optional path parameter", mutateVerb(func(v *domain.RecipeVerb) { v.Params[0].Required = false })},
		{"duplicate parameter name", mutateVerb(func(v *domain.RecipeVerb) {
			v.Params = append(v.Params, domain.RecipeVerbParam{Name: "limit", Type: domain.RecipeParamNumber, Required: false, In: domain.RecipeParamInQuery})
		})},
		{"unknown parameter type", mutateVerb(func(v *domain.RecipeVerb) { v.Params[0].Type = "integer" })},
		{"unknown parameter location", mutateVerb(func(v *domain.RecipeVerb) { v.Params[0].In = "header" })},
		{"invalid parameter name", mutateVerb(func(v *domain.RecipeVerb) { v.Params[0].Name = "widget id" })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fresh fixture per case — verb slices alias their backing array.
			r := validHTTPRecipe()
			tt.mutate(&r)
			if err := domain.ValidateRecipe(&r); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}

	// Every parameter type and location in the catalog is declarable.
	allTypes := validHTTPRecipe()
	allTypes.Verbs[0].Params = []domain.RecipeVerbParam{
		{Name: "widget_id", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath},
		{Name: "limit", Type: domain.RecipeParamNumber, Required: false, In: domain.RecipeParamInQuery},
		{Name: "include_deleted", Type: domain.RecipeParamBoolean, Required: false, In: domain.RecipeParamInQuery},
	}
	if err := domain.ValidateRecipe(&allTypes); err != nil {
		t.Fatalf("expected all catalog param types to be valid, got %v", err)
	}
}

// The mirror prohibition (add-connection-oauth precedent): an mcp recipe
// declares server materialization data and must not declare the http kind's
// request surface.
func TestValidateRecipeMCPKindRejectsHTTPFields(t *testing.T) {
	valid := domain.Recipe{
		ID:           "mcpish",
		Service:      "MCPish",
		Icon:         "mcpish",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Kind:         domain.RecipeKindMCP,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     "https://mcp.mcpish.dev/mcp",
		TokenHeader:  "Authorization",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Create a token"}},
		Probe:        domain.RecipeProbe{Tool: "ping"},
	}
	if err := domain.ValidateRecipe(&valid); err != nil {
		t.Fatalf("expected valid mcp recipe, got %v", err)
	}

	tests := []struct {
		name   string
		mutate func(r *domain.Recipe)
	}{
		{"base url declared", func(r *domain.Recipe) { r.BaseURL = "https://api.mcpish.dev" }},
		{"http probe call declared", func(r *domain.Recipe) { r.Probe.Method, r.Probe.Path = "GET", "/v1/ping" }},
		{"verbs declared", func(r *domain.Recipe) {
			r.Verbs = []domain.RecipeVerb{{Tool: "mcpish.ping", Method: "GET", Path: "/v1/ping", Description: "Ping."}}
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
}

// The served wire shape (add-connection-http change contract): kind, base_url,
// and the verb declarations are on the wire; the mcp shape stays intact with
// an explicit kind and non-null verbs array.
func TestRecipeHTTPWireShape(t *testing.T) {
	httpRecipe := validHTTPRecipe()
	data, err := json.Marshal(&httpRecipe)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if wire["kind"] != domain.RecipeKindHTTP {
		t.Errorf("expected kind %q on the wire, got %s", domain.RecipeKindHTTP, data)
	}
	if wire["base_url"] != "https://api.acme.dev" {
		t.Errorf("expected base_url on the wire, got %s", data)
	}
	if wire["token_header"] != "X-Acme-Token" {
		t.Errorf("expected token_header (the http auth header) on the wire, got %s", data)
	}
	probe, ok := wire["probe"].(map[string]any)
	if !ok || probe["method"] != "GET" || probe["path"] != "/v1/ping" {
		t.Errorf("expected the probe's method and path on the wire, got %s", data)
	}
	if _, ok := probe["tool"]; ok {
		t.Errorf("expected no mcp probe tool on an http recipe, got %s", data)
	}
	verbs, ok := wire["verbs"].([]any)
	if !ok || len(verbs) != 1 {
		t.Fatalf("expected the declared verbs on the wire, got %s", data)
	}
	verb, ok := verbs[0].(map[string]any)
	if !ok || verb["name"] != "acme.list_widget_parts" || verb["method"] != "GET" ||
		verb["path"] != "/v1/widgets/{widget_id}/parts" || verb["description"] == "" {
		t.Errorf("expected the verb declaration on the wire, got %s", data)
	}
	params, ok := verb["params"].([]any)
	if !ok || len(params) != 2 {
		t.Fatalf("expected the verb's typed parameters on the wire, got %s", data)
	}
	param, _ := params[0].(map[string]any)
	if param["name"] != "widget_id" || param["type"] != domain.RecipeParamString ||
		param["in"] != domain.RecipeParamInPath || param["required"] != true {
		t.Errorf("expected the parameter schema (name, type, in, required) on the wire, got %s", data)
	}

	// The mcp wire shape gains an explicit kind and a non-null verbs array;
	// base_url stays off it. Recipes are served through the registry, so the
	// marshaled shape is the registered (normalized) one.
	mcpRecipe := domain.Recipe{
		ID:           "mcpwire",
		Service:      "MCPWire",
		Icon:         "mcpwire",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     "https://mcp.mcpwire.dev/mcp",
		TokenHeader:  "Authorization",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Create a token"}},
		Probe:        domain.RecipeProbe{Tool: "ping"},
	}
	domain.RegisterRecipe(mcpRecipe)
	served := domain.RecipeByID("mcpwire")
	if served == nil {
		t.Fatal("expected the registered mcp recipe to resolve")
	}
	data, err = json.Marshal(served)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	wire = nil
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if wire["kind"] != domain.RecipeKindMCP {
		t.Errorf("expected kind %q on the wire, got %s", domain.RecipeKindMCP, data)
	}
	if _, ok := wire["base_url"]; ok {
		t.Errorf("expected base_url to stay off the mcp wire shape, got %s", data)
	}
	if verbs, ok := wire["verbs"].([]any); !ok || len(verbs) != 0 {
		t.Errorf("expected verbs to serve as an empty array on mcp recipes, got %s", data)
	}
}

// The Figma reference recipe (add-connection-http design.md D5, tasks.md
// 1.2): http kind, pinned base URL and auth header, GET /v1/me probe, guided
// token steps, and the curated read verb list. Values are drafted pending the
// live-verification pinning (tasks.md 5.3).
func TestRecipesBuiltinFigmaRecipe(t *testing.T) {
	r := domain.RecipeByID("figma")
	if r == nil {
		t.Fatal("expected the figma recipe to be registered")
	}
	if r.Availability != domain.RecipeAvailable {
		t.Errorf("expected figma to be %s (connect is probe-gated), got %q", domain.RecipeAvailable, r.Availability)
	}
	if r.Kind != domain.RecipeKindHTTP {
		t.Errorf("expected figma to declare %s kind, got %q", domain.RecipeKindHTTP, r.Kind)
	}
	if r.AuthKind != domain.RecipeAuthPAT {
		t.Errorf("expected figma to be %s auth, got %q", domain.RecipeAuthPAT, r.AuthKind)
	}
	if r.BaseURL != "https://api.figma.com" {
		t.Errorf("expected the pinned figma base url, got %q", r.BaseURL)
	}
	if r.TokenHeader != "X-Figma-Token" {
		t.Errorf("expected the X-Figma-Token auth header, got %q", r.TokenHeader)
	}
	if r.TokenScheme != "" {
		t.Errorf("expected the raw token (no scheme prefix) for figma, got %q", r.TokenScheme)
	}
	if r.Transport != "" || r.Endpoint != "" || r.Command != "" {
		t.Errorf("expected no mcp materialization fields on figma, got transport=%q endpoint=%q command=%q", r.Transport, r.Endpoint, r.Command)
	}
	if r.Probe.Method != "GET" || r.Probe.Path != "/v1/me" {
		t.Errorf("expected the GET /v1/me probe call, got %+v", r.Probe)
	}
	if len(r.Steps) == 0 {
		t.Error("expected guided token-creation steps")
	}
	if len(r.AccessLevels) != 1 || r.AccessLevels[0] != domain.ConnectionAccessReadOnly {
		t.Errorf("expected read-only to be figma's only access level (the curated surface is all reads), got %v", r.AccessLevels)
	}
	if len(r.Scopes) == 0 {
		t.Error("expected access-level scope guidance")
	}

	// The curated verb list: bounded (design.md D5 — the workflows that
	// matter, not the full API), all GET, service-prefixed and unique, with
	// the key workflows covered.
	if len(r.Verbs) < 8 || len(r.Verbs) > 15 {
		t.Fatalf("expected a curated 8-15 verb list, got %d", len(r.Verbs))
	}
	seen := make(map[string]bool, len(r.Verbs))
	for _, v := range r.Verbs {
		if v.Method != "GET" {
			t.Errorf("expected verb %s to be a GET read, got %q", v.Tool, v.Method)
		}
		if !strings.HasPrefix(v.Tool, "figma.") {
			t.Errorf("expected verb tool %q to be service-prefixed", v.Tool)
		}
		if seen[v.Tool] {
			t.Errorf("verb tool %q declared twice", v.Tool)
		}
		seen[v.Tool] = true
		if strings.TrimSpace(v.Description) == "" {
			t.Errorf("expected verb %s to carry an agent-facing description", v.Tool)
		}
	}
	for _, want := range []string{
		"figma.get_me",
		"figma.get_file",
		"figma.get_file_nodes",
		"figma.get_file_comments",
		"figma.get_project_files",
		"figma.get_team_projects",
	} {
		if !seen[want] {
			t.Errorf("expected curated verb %s to be declared, got %v", want, seen)
		}
	}

	if err := domain.ValidateRecipe(r); err != nil {
		t.Errorf("expected the registered figma recipe to be valid, got %v", err)
	}
}

// Registration normalizes the declared shape: an empty kind is the mcp
// default, and the verb/parameter arrays serve as arrays, never null.
func TestRegisterRecipeNormalization(t *testing.T) {
	base := domain.Recipe{
		ID:           "normalized-service",
		Service:      "Normalized",
		Icon:         "normalized",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Transport:    domain.MCPTransportStdio,
		Command:      "normalized-mcp",
		TokenHeader:  "NORMALIZED_TOKEN",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "step"}},
		Probe:        domain.RecipeProbe{Tool: "ping"},
	}
	domain.RegisterRecipe(base)
	got := domain.RecipeByID("normalized-service")
	if got == nil {
		t.Fatal("expected the registered recipe to resolve")
	}
	if got.Kind != domain.RecipeKindMCP {
		t.Errorf("expected an empty kind to register as %q, got %q", domain.RecipeKindMCP, got.Kind)
	}
	if got.Verbs == nil {
		t.Error("expected verbs to normalize to an empty array")
	}

	httpBase := validHTTPRecipe()
	httpBase.ID = "normalized-http"
	// A verb with no declared parameters at all is the valid shape whose nil
	// Params must normalize to an empty array.
	httpBase.Verbs = []domain.RecipeVerb{{
		Tool:        "normalized-http.ping",
		Method:      "GET",
		Path:        "/v1/ping",
		Description: "Ping the API.",
	}}
	domain.RegisterRecipe(httpBase)
	got = domain.RecipeByID("normalized-http")
	if got == nil {
		t.Fatal("expected the registered http recipe to resolve")
	}
	if got.Verbs[0].Params == nil {
		t.Error("expected verb params to normalize to an empty array")
	}
	if !slices.Contains(got.AccessLevels, domain.ConnectionAccessReadOnly) {
		t.Errorf("expected access levels preserved, got %v", got.AccessLevels)
	}
}

// The GitLab REST recipe (add-recipe-base-url tasks.md 3.1): http kind, PAT
// auth, an origin parameter defaulting to gitlab.com, the /api/v4 path prefix
// on every declared call, the current-user probe, and the curated read/write
// verb split (design.md D4). This replaces the PAT-against-remote-MCP wiring
// that could never connect.
func TestRecipesBuiltinGitLabRecipe(t *testing.T) {
	r := domain.RecipeByID("gitlab")
	if r == nil {
		t.Fatal("expected the gitlab recipe to be registered")
	}
	if r.Availability != domain.RecipeAvailable {
		t.Errorf("expected gitlab to be %s, got %q", domain.RecipeAvailable, r.Availability)
	}
	if r.Kind != domain.RecipeKindHTTP {
		t.Errorf("expected gitlab to declare %s kind, got %q", domain.RecipeKindHTTP, r.Kind)
	}
	if r.AuthKind != domain.RecipeAuthPAT {
		t.Errorf("expected gitlab to be %s auth, got %q", domain.RecipeAuthPAT, r.AuthKind)
	}
	if r.BaseURL != "https://gitlab.com" {
		t.Errorf("expected the bare gitlab.com origin as base url, got %q", r.BaseURL)
	}
	if r.OriginParam == nil {
		t.Fatal("expected gitlab to declare an origin parameter")
	}
	if r.OriginParam.Default != "https://gitlab.com" {
		t.Errorf("expected the SaaS origin as the declared default, got %q", r.OriginParam.Default)
	}
	if r.OriginParam.Name == "" || r.OriginParam.Help == "" {
		t.Errorf("expected a labeled field with help copy, got %+v", r.OriginParam)
	}
	if r.TokenHeader != "PRIVATE-TOKEN" {
		t.Errorf("expected the PRIVATE-TOKEN auth header, got %q", r.TokenHeader)
	}
	if r.TokenScheme != "" {
		t.Errorf("expected the raw token (no scheme prefix) for GitLab PATs, got %q", r.TokenScheme)
	}
	if r.Transport != "" || r.Endpoint != "" || r.Command != "" {
		t.Errorf("expected no mcp materialization fields on gitlab, got transport=%q endpoint=%q command=%q", r.Transport, r.Endpoint, r.Command)
	}
	if r.Probe.Method != "GET" || r.Probe.Path != "/api/v4/user" {
		t.Errorf("expected the GET /api/v4/user (current-user) probe call, got %+v", r.Probe)
	}
	if len(r.Steps) == 0 {
		t.Error("expected guided token-creation steps")
	}
	if !slices.Contains(r.AccessLevels, domain.ConnectionAccessReadOnly) ||
		!slices.Contains(r.AccessLevels, domain.ConnectionAccessReadWrite) {
		t.Errorf("expected both access levels on gitlab, got %v", r.AccessLevels)
	}

	// The declared verb surface (design.md D4): the curated read-only verbs
	// and the read-write additions, every call under the /api/v4 prefix,
	// service-prefixed and unique.
	seen := make(map[string]bool, len(r.Verbs))
	reads, writes := 0, 0
	for _, v := range r.Verbs {
		if !strings.HasPrefix(v.Path, "/api/v4/") {
			t.Errorf("expected verb %s path %q to carry the /api/v4 prefix", v.Tool, v.Path)
		}
		if !strings.HasPrefix(v.Tool, "gitlab.") {
			t.Errorf("expected verb tool %q to be service-prefixed", v.Tool)
		}
		if seen[v.Tool] {
			t.Errorf("verb tool %q declared twice", v.Tool)
		}
		seen[v.Tool] = true
		if v.Tier == domain.RecipeToolTierRead {
			reads++
		} else if v.Tier == domain.RecipeToolTierWrite {
			writes++
		} else {
			t.Errorf("expected verb %s to declare an explicit tier, got %q", v.Tool, v.Tier)
		}
		if strings.TrimSpace(v.Description) == "" {
			t.Errorf("expected verb %s to carry an agent-facing description", v.Tool)
		}
	}
	if reads == 0 || writes == 0 {
		t.Fatalf("expected the curated read/write split, got %d reads / %d writes", reads, writes)
	}
	for _, want := range []string{
		// Read-only (design.md D4).
		"gitlab.get_current_user",
		"gitlab.list_projects",
		"gitlab.get_project",
		"gitlab.list_issues",
		"gitlab.get_issue",
		"gitlab.list_merge_requests",
		"gitlab.get_merge_request",
		"gitlab.list_branches",
		"gitlab.list_pipelines",
		// Read-write additions (design.md D4).
		"gitlab.create_issue",
		"gitlab.update_issue",
		"gitlab.create_issue_note",
		"gitlab.create_merge_request",
		"gitlab.update_merge_request",
		"gitlab.merge_merge_request",
		"gitlab.retry_pipeline",
		"gitlab.cancel_pipeline",
	} {
		if !seen[want] {
			t.Errorf("expected curated verb %s to be declared, got %v", want, seen)
		}
	}

	// The probe is the current-user declared call: a declared verb carries
	// exactly the probe's method + path.
	probeDeclared := false
	for _, v := range r.Verbs {
		if v.Method == r.Probe.Method && v.Path == r.Probe.Path {
			probeDeclared = true
		}
	}
	if !probeDeclared {
		t.Errorf("expected the probe call %s %s to be a declared verb", r.Probe.Method, r.Probe.Path)
	}

	// Guidance (tasks.md 3.3): the PAT-now copy covers self-managed origins,
	// and the OAuth MCP endpoint is tracked separately.
	if !strings.Contains(r.Steps[0].Detail, "self-managed") {
		t.Errorf("expected the guided steps to cover self-managed origins, got %q", r.Steps[0].Detail)
	}
	if !strings.Contains(r.Notes, "REST API") || !strings.Contains(r.Notes, "OAuth") {
		t.Errorf("expected the notes to state the PAT-over-REST posture and the OAuth MCP tracking, got %q", r.Notes)
	}

	if err := domain.ValidateRecipe(r); err != nil {
		t.Errorf("expected the registered gitlab recipe to be valid, got %v", err)
	}
}

// Webhook compatibility (add-recipe-base-url tasks.md 3.3): the GitLab
// re-scope leaves the webhook declaration byte-identical — the golden pins
// the exact pre-change wire shape (catalog, default selection, secret-token
// scheme, templates with whitelisted fields, setup copy).
func TestRecipesBuiltinGitLabWebhooksUnchanged(t *testing.T) {
	gl := domain.RecipeByID("gitlab")
	if gl == nil || gl.Webhooks == nil {
		t.Fatal("expected the gitlab recipe to declare webhook support")
	}
	data, err := json.Marshal(gl.Webhooks)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	const golden = `{"events":["push","tag_push","merge_request.open","merge_request.merge","merge_request.close","issue.open","issue.close","note"],"default_events":["push","merge_request.open","issue.open","note"],"signature_scheme":"secret_token","templates":[{"event":"push","fields":["project.path_with_namespace","user_name","ref"],"template":"Push to {project.path_with_namespace}: {user_name} pushed {ref}."},{"event":"tag_push","fields":["project.path_with_namespace","user_name","ref"],"template":"Tag {ref} pushed to {project.path_with_namespace} by {user_name}."},{"event":"merge_request.open","fields":["project.path_with_namespace","object_attributes.iid","object_attributes.title","object_attributes.url","user.name"],"template":"Merge request !{object_attributes.iid} opened in {project.path_with_namespace} by {user.name}: \"{object_attributes.title}\" — {object_attributes.url}"},{"event":"merge_request.merge","fields":["project.path_with_namespace","object_attributes.iid","object_attributes.title","object_attributes.url","user.name"],"template":"Merge request !{object_attributes.iid} merged in {project.path_with_namespace} by {user.name}: \"{object_attributes.title}\" — {object_attributes.url}"},{"event":"merge_request.close","fields":["project.path_with_namespace","object_attributes.iid","object_attributes.title","object_attributes.url","user.name"],"template":"Merge request !{object_attributes.iid} closed in {project.path_with_namespace} by {user.name}: \"{object_attributes.title}\" — {object_attributes.url}"},{"event":"issue.open","fields":["project.path_with_namespace","object_attributes.iid","object_attributes.title","object_attributes.url","user.name"],"template":"Issue #{object_attributes.iid} opened in {project.path_with_namespace} by {user.name}: \"{object_attributes.title}\" — {object_attributes.url}"},{"event":"issue.close","fields":["project.path_with_namespace","object_attributes.iid","object_attributes.title","object_attributes.url","user.name"],"template":"Issue #{object_attributes.iid} closed in {project.path_with_namespace} by {user.name}: \"{object_attributes.title}\" — {object_attributes.url}"},{"event":"note","fields":["project.path_with_namespace","object_attributes.note","object_attributes.url","user.name"],"template":"New comment in {project.path_with_namespace} by {user.name}:\n\"{object_attributes.note}\"\n{object_attributes.url}"}],"setup":{"signature_header":"X-Gitlab-Token","event_type_header":"X-Gitlab-Event","delivery_id_header":"X-Gitlab-Event-UUID","url_path_shape":"/api/ingest/webhooks/{workspace_slug}/{connection_id}","help":"In GitLab, open the project or group, go to Settings → Webhooks. Add the ingest URL below as the URL, set the Secret token to the generated secret shown once here, and select the events this connection subscribes to."}}`
	if string(data) != golden {
		t.Errorf("gitlab webhook declaration drifted from the pinned pre-change shape.\nwant: %s\ngot:  %s", golden, data)
	}
}
