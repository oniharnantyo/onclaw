package domain

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Recipe availability (design.md D2): available recipes offer a working
// connect flow; coming-soon recipes render as visible but disabled gallery
// cards awaiting a capability the product does not have yet (OAuth).
const (
	RecipeAvailable  = "available"
	RecipeComingSoon = "coming_soon"
)

// Recipe auth kinds. PAT recipes connect with a pasted access token; OAuth
// recipes (add-connection-oauth) connect through an authorization-code flow
// against an instance-registered provider app.
const (
	RecipeAuthPAT   = "pat"
	RecipeAuthOAuth = "oauth"
)

// Connection kinds a recipe can declare (add-connection-http design.md D6).
// Kind is a recipe-level fact, never a per-connection choice: mcp recipes
// materialize a workspace MCP server; http recipes contribute their declared
// verb tools without a server row. Empty normalizes to mcp (the pre-http
// default) at registration.
const (
	RecipeKindMCP  = "mcp"
	RecipeKindHTTP = "http"
)

// Recipe verb parameter types (add-connection-http tasks.md 1.1) — the
// minimal typed schema a declared verb's parameters carry. They map onto JSON
// schema types at tool generation.
const (
	RecipeParamString  = "string"
	RecipeParamNumber  = "number"
	RecipeParamBoolean = "boolean"
)

// Recipe verb parameter locations: the path template's {param} placeholders
// or the request's query string. The v1 verb schema declares no request body
// — a body-carrying verb is a recipe-schema extension, not user input.
const (
	RecipeParamInPath  = "path"
	RecipeParamInQuery = "query"
)

// DefaultRecipeRefreshMargin is the refresh margin an OAuth recipe uses when
// it declares none (add-connection-oauth design.md D3): a credential
// resolution within this duration of the stored access-token expiry refreshes
// the token first instead of yielding the possibly-stale one.
const DefaultRecipeRefreshMargin = 10 * time.Minute

// RecipeStep is one guided token-creation step shown in the connect dialog.
// Copy is real copy, not marketing filler.
type RecipeStep struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	URL    string `json:"url,omitempty"`
}

// RecipeScopes pairs one access level with its recommended token scopes —
// the guided copy the connect dialog shows for the chosen level. A slice, not
// a map, so the served JSON order is deterministic.
type RecipeScopes struct {
	AccessLevel string   `json:"access_level"`
	Scopes      []string `json:"scopes"`
}

// RecipeProbe declares the probe action (design.md D3): the connect flow
// runs it against the candidate connection before anything is stored, and
// on-demand status refresh re-runs it. Kind-shaped data — an mcp recipe names
// the MCP tool the probe calls; an http recipe names the request (method +
// path against the base URL) the probe issues (add-connection-http
// tasks.md 1.1). A recipe declares exactly one of the two shapes.
type RecipeProbe struct {
	// Tool is the MCP tool the probe calls (mcp kind).
	Tool string `json:"tool,omitempty"`
	// Method and Path are the request the probe issues against the recipe's
	// base URL (http kind). The path is literal — the probe takes no
	// parameters.
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
}

// RecipeVerbParam is one typed parameter of a declared verb tool
// (add-connection-http tasks.md 1.1): where it binds (path placeholder or
// query), its type, and whether the caller must supply it. Path parameters
// are always required — they are structurally part of the URL.
type RecipeVerbParam struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	In       string `json:"in"`
}

// RecipeVerb is one declared verb tool of an http-kind connection: a
// service-prefixed tool name over a pre-bound method and path template with
// typed parameters (add-connection-http design.md D1). The verb list is the
// connection's entire agent-facing surface — there is no free-form request
// tool, so covering a new operation means shipping this data, not model
// improvisation.
type RecipeVerb struct {
	// Tool is the tool name verbatim (e.g. "figma.get_comments") — the
	// recipe id prefix namespaces it across connections.
	Tool string `json:"name"`
	// Method is the HTTP method the engine sends (GET, POST, PUT, PATCH,
	// DELETE).
	Method string `json:"method"`
	// Path is the path template appended to the base URL; {param}
	// placeholders must be declared as path parameters.
	Path string `json:"path"`
	// Params declares the typed parameters; always an array (possibly empty).
	Params []RecipeVerbParam `json:"params"`
	// Description is the tool's agent-facing description. Real copy, no
	// credential material.
	Description string `json:"description"`
	// Tier classifies the verb for the connection tool gate
	// (add-integration-authority tasks.md 1.1): one of the RecipeToolTier*
	// constants. Empty is legal and means RecipeToolTierDefault (write) — the
	// fail-safe default — but built-ins declare their tiers explicitly.
	Tier string `json:"tier"`
}

// Recipe is one declared service integration: recipe knowledge is server-side
// Go data (design.md D2) — endpoint, auth shape, guided steps, scopes, and
// probe are release-shippable facts, never user input (design.md D7). Adding
// a service is registering a recipe, not editing core code.
type Recipe struct {
	// ID is the recipe's stable identifier and the connect payload's recipe
	// reference (e.g. "github"). It is also the Connection.Service value.
	ID string `json:"id"`
	// Service is the display name (e.g. "GitHub").
	Service string `json:"service"`
	// Icon is the icon identifier the gallery renders.
	Icon string `json:"icon"`
	// AuthKind is one of the RecipeAuth* constants.
	AuthKind string `json:"auth_kind"`
	// Availability is one of the Recipe* availability constants.
	Availability string `json:"availability"`
	// Kind is the connection kind the recipe materializes (add-connection-http
	// design.md D6): mcp materializes a workspace MCP server, http contributes
	// its declared verb tools without a server row. Empty normalizes to mcp
	// at registration; the served value is always explicit.
	Kind string `json:"kind"`
	// Transport is one of the MCPTransport* constants — the materialized
	// server's transport (mcp kind only).
	Transport string `json:"transport"`
	// Endpoint is the remote MCP endpoint URL for URL transports (the
	// materialized server's URL) (mcp kind only).
	Endpoint string `json:"endpoint,omitempty"`
	// Command is the local command for stdio recipes (the server binary must
	// be present in the deployment image — an operator provisioning concern,
	// design.md D2) (mcp kind only).
	Command string `json:"command,omitempty"`
	// BaseURL is the pinned API origin http-kind requests are joined against
	// (add-connection-http design.md D2/D7): recipe data, never user input.
	BaseURL string `json:"base_url,omitempty"`
	// TokenHeader is the header (URL transports) or env row (stdio) that
	// carries the token as the single secret row (design.md D4); for http
	// recipes it is the auth header the engine attaches the credential under
	// at call time (e.g. "X-Figma-Token").
	TokenHeader string `json:"token_header,omitempty"`
	// TokenScheme is the prefix composed before the token in the secret row
	// value (e.g. "Bearer"); empty means the raw token is the row value.
	TokenScheme string `json:"token_scheme,omitempty"`
	// AuthorizeURL is the provider's OAuth authorization endpoint (auth kind
	// oauth): the URL the connect flow redirects the user to, with the
	// registered instance app's client id and the signed state attached.
	AuthorizeURL string `json:"authorize_url,omitempty"`
	// TokenURL is the provider's OAuth token endpoint (auth kind oauth): the
	// URL the authorization code (and, later, the refresh token) is exchanged
	// at.
	TokenURL string `json:"token_url,omitempty"`
	// RefreshMargin is how long before the stored access-token expiry a
	// credential resolution triggers a refresh (auth kind oauth, design.md
	// D3); zero selects DefaultRecipeRefreshMargin. Deliberately not served
	// JSON — it is lifecycle tuning, not gallery copy.
	RefreshMargin time.Duration `json:"-"`
	// AppRegistrationGuidance is the instance-admin copy (auth kind oauth)
	// explaining how to register the provider app whose client credentials
	// the whole instance authorizes through (design.md D2).
	AppRegistrationGuidance string `json:"app_registration_guidance,omitempty"`
	// OauthRedirectURI is DERIVED at serve time, not registry data (services.
	// EnrichRecipes): the instance's public base URL + the OAuth callback
	// path, shown in the gallery's setup flow so the operator can register it
	// at the provider before any app exists. Empty when the base URL is
	// unset. Never validated — it is projection copy.
	OauthRedirectURI string `json:"oauth_redirect_uri,omitempty"`
	// AccessLevels lists the supported access levels (ConnectionAccess*
	// constants); the first entry is the flow's default.
	AccessLevels []string       `json:"access_levels"`
	Steps        []RecipeStep   `json:"steps"`
	Scopes       []RecipeScopes `json:"scopes"`
	Probe        RecipeProbe    `json:"probe"`
	// Verbs is the http-kind connection's declared verb tools — the entire
	// agent-facing surface (add-connection-http design.md D1). Empty for mcp
	// recipes; always an array when served.
	Verbs []RecipeVerb `json:"verbs"`
	// ToolTiers is the mcp-kind connection's explicit tool-tier list
	// (add-integration-authority tasks.md 1.1): MCP tools are
	// runtime-discovered, so the recipe classifies the curated tool names the
	// remote server assigns. Kind-shaped data — http recipes carry their
	// tiers on the verbs and must not declare this list; anything an mcp
	// recipe's list does not name gates as RecipeToolTierDefault (write).
	// Treat the slice as read-only (the Verbs copy convention).
	ToolTiers []RecipeToolTierRule `json:"tool_tiers,omitempty"`
	// Webhooks is the recipe's webhook declaration (add-connection-webhooks
	// tasks.md 1.1): event catalog, signature scheme, per-event prompt
	// templates with whitelisted payload fields, and provider setup copy. Nil
	// means the service declares no webhook support; validated as part of
	// ValidateRecipe when non-nil. Reads hand out a deep copy — treat the
	// returned declaration as read-only.
	Webhooks *RecipeWebhook `json:"webhooks,omitempty"`
	// Notes carries the card's truthful coming-soon copy or operator
	// provisioning notes; empty for available recipes that need none.
	Notes string `json:"notes,omitempty"`
}

// ValidateRecipe checks a recipe's declared shape: identity, auth kind,
// availability, kind (mcp or http, add-connection-http design.md D6), and
// per-kind surface — transport/endpoint/command and the MCP probe tool for
// mcp kind; base URL, auth header, probe call, and the declared verb tools
// for http kind — plus supported access levels, guided steps, scopes, and
// the tool-tier declarations (add-integration-authority tasks.md 1.1).
// Kind-shaped data never mixes: each kind rejects the other's fields
// (add-connection-oauth precedent). Invalid recipes are a registration-time
// programming error, not a runtime condition.
func ValidateRecipe(r *Recipe) error {
	if r == nil {
		return ErrInvalid
	}
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("%w: recipe id cannot be empty", ErrInvalid)
	}
	if strings.TrimSpace(r.Service) == "" {
		return fmt.Errorf("%w: recipe %q service name cannot be empty", ErrInvalid, r.ID)
	}
	switch r.AuthKind {
	case RecipeAuthPAT, RecipeAuthOAuth:
	default:
		return fmt.Errorf("%w: recipe %q auth kind %q must be %s or %s", ErrInvalid, r.ID, r.AuthKind, RecipeAuthPAT, RecipeAuthOAuth)
	}
	switch r.Availability {
	case RecipeAvailable, RecipeComingSoon:
	default:
		return fmt.Errorf("%w: recipe %q availability %q must be %s or %s", ErrInvalid, r.ID, r.Availability, RecipeAvailable, RecipeComingSoon)
	}

	// Kind-shaped data (add-connection-http design.md D6): each kind owns its
	// surface fields and must not declare the other kind's. An empty kind is
	// the pre-http mcp default (RegisterRecipe normalizes it to mcp).
	switch r.Kind {
	case "", RecipeKindMCP:
		// An mcp recipe materializes a server: transport-shaped data only.
		if r.BaseURL != "" {
			return fmt.Errorf("%w: recipe %q declares an http base url under %s kind", ErrInvalid, r.ID, RecipeKindMCP)
		}
		if r.Probe.Method != "" || r.Probe.Path != "" {
			return fmt.Errorf("%w: recipe %q declares an http probe call under %s kind", ErrInvalid, r.ID, RecipeKindMCP)
		}
		if len(r.Verbs) > 0 {
			return fmt.Errorf("%w: recipe %q declares verb tools under %s kind", ErrInvalid, r.ID, RecipeKindMCP)
		}
		switch r.Transport {
		case MCPTransportStreamableHTTP, MCPTransportSSE:
			if r.Endpoint == "" {
				return fmt.Errorf("%w: recipe %q endpoint is required for %s transport", ErrInvalid, r.ID, r.Transport)
			}
		case MCPTransportStdio:
			if r.Command == "" {
				return fmt.Errorf("%w: recipe %q command is required for %s transport", ErrInvalid, r.ID, r.Transport)
			}
		default:
			return fmt.Errorf("%w: recipe %q transport %q must be one of %s, %s, or %s", ErrInvalid, r.ID, r.Transport, MCPTransportStdio, MCPTransportStreamableHTTP, MCPTransportSSE)
		}
		if strings.TrimSpace(r.Probe.Tool) == "" {
			return fmt.Errorf("%w: recipe %q probe tool cannot be empty", ErrInvalid, r.ID)
		}
		// The tool-tier list is the mcp kind's tier surface
		// (add-integration-authority tasks.md 1.1): well-formed unique tool
		// names with catalog tiers.
		if err := validateRecipeToolTiers(r); err != nil {
			return err
		}
	case RecipeKindHTTP:
		if err := validateHTTPRecipe(r); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: recipe %q kind %q must be %s or %s", ErrInvalid, r.ID, r.Kind, RecipeKindMCP, RecipeKindHTTP)
	}

	if r.AuthKind == RecipeAuthPAT && r.TokenHeader == "" {
		return fmt.Errorf("%w: recipe %q token header is required for %s auth", ErrInvalid, r.ID, RecipeAuthPAT)
	}

	// OAuth recipes (add-connection-oauth): both flow endpoints and the token
	// row are required — the access token still materializes into the
	// materialized server's single secret row (change-1 design.md D4), so an
	// OAuth recipe declares its header/scheme exactly like a PAT recipe.
	if r.AuthKind == RecipeAuthOAuth {
		if r.AuthorizeURL == "" {
			return fmt.Errorf("%w: recipe %q authorize url is required for %s auth", ErrInvalid, r.ID, RecipeAuthOAuth)
		}
		if r.TokenURL == "" {
			return fmt.Errorf("%w: recipe %q token url is required for %s auth", ErrInvalid, r.ID, RecipeAuthOAuth)
		}
		if r.TokenHeader == "" {
			return fmt.Errorf("%w: recipe %q token header is required for %s auth", ErrInvalid, r.ID, RecipeAuthOAuth)
		}
	}
	// PAT recipes connect by pasted token only: OAuth flow endpoints are
	// auth-kind-shaped data, not optional extras.
	if r.AuthKind == RecipeAuthPAT && (r.AuthorizeURL != "" || r.TokenURL != "") {
		return fmt.Errorf("%w: recipe %q declares OAuth endpoints under %s auth", ErrInvalid, r.ID, RecipeAuthPAT)
	}

	// Webhook declarations (add-connection-webhooks tasks.md 1.1) are
	// kind-agnostic: mcp-kind recipes (GitHub, GitLab) declare them, and any
	// future recipe may. A non-nil declaration must be complete — scheme,
	// catalog, defaults, per-event templates, setup copy.
	if r.Webhooks != nil {
		if err := validateRecipeWebhook(r, r.Webhooks); err != nil {
			return err
		}
	}

	if len(r.AccessLevels) == 0 {
		return fmt.Errorf("%w: recipe %q must declare at least one access level", ErrInvalid, r.ID)
	}
	supported := make(map[string]bool, len(r.AccessLevels))
	for _, level := range r.AccessLevels {
		if !IsValidConnectionAccessLevel(level) {
			return fmt.Errorf("%w: recipe %q access level %q is not in the catalog", ErrInvalid, r.ID, level)
		}
		if supported[level] {
			return fmt.Errorf("%w: recipe %q access level %q is duplicated", ErrInvalid, r.ID, level)
		}
		supported[level] = true
	}

	if r.Availability == RecipeAvailable && len(r.Steps) == 0 {
		return fmt.Errorf("%w: available recipe %q must declare guided setup steps", ErrInvalid, r.ID)
	}

	seenScopeLevels := make(map[string]bool, len(r.Scopes))
	for _, s := range r.Scopes {
		if !supported[s.AccessLevel] {
			return fmt.Errorf("%w: recipe %q scopes name access level %q which it does not support", ErrInvalid, r.ID, s.AccessLevel)
		}
		if seenScopeLevels[s.AccessLevel] {
			return fmt.Errorf("%w: recipe %q scopes for access level %q are duplicated", ErrInvalid, r.ID, s.AccessLevel)
		}
		seenScopeLevels[s.AccessLevel] = true
	}

	return nil
}

// validateHTTPRecipe checks the http-kind declaration (add-connection-http
// tasks.md 1.1): the pinned API origin, the credential header, the probe
// call, and the declared verb surface. OAuth-for-http is a declared non-goal
// (add-connection-http design.md) until its follow-up lands, so http recipes
// connect by pasted token only — an http recipe under oauth auth is a
// registration-time error, never a half-wired connect flow.
func validateHTTPRecipe(r *Recipe) error {
	if r.Transport != "" {
		return fmt.Errorf("%w: recipe %q declares an mcp transport under %s kind", ErrInvalid, r.ID, RecipeKindHTTP)
	}
	if r.Endpoint != "" {
		return fmt.Errorf("%w: recipe %q declares an mcp endpoint under %s kind", ErrInvalid, r.ID, RecipeKindHTTP)
	}
	if r.Command != "" {
		return fmt.Errorf("%w: recipe %q declares an mcp command under %s kind", ErrInvalid, r.ID, RecipeKindHTTP)
	}
	if r.AuthKind != RecipeAuthPAT {
		return fmt.Errorf("%w: recipe %q must declare %s auth under %s kind — http recipes connect by pasted token", ErrInvalid, r.ID, RecipeAuthPAT, RecipeKindHTTP)
	}
	if r.TokenHeader == "" {
		return fmt.Errorf("%w: recipe %q auth header is required for %s kind", ErrInvalid, r.ID, RecipeKindHTTP)
	}
	if r.BaseURL == "" {
		return fmt.Errorf("%w: recipe %q base url is required for %s kind", ErrInvalid, r.ID, RecipeKindHTTP)
	}
	if u, err := url.Parse(r.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%w: recipe %q base url %q must be an absolute http(s) URL without query or fragment", ErrInvalid, r.ID, r.BaseURL)
	}
	if r.Probe.Tool != "" {
		return fmt.Errorf("%w: recipe %q declares an mcp probe tool under %s kind", ErrInvalid, r.ID, RecipeKindHTTP)
	}
	if len(r.ToolTiers) > 0 {
		return fmt.Errorf("%w: recipe %q declares an mcp tool-tier list under %s kind — verbs carry their own tiers", ErrInvalid, r.ID, RecipeKindHTTP)
	}
	if err := validateHTTPCall(r.ID, "probe call", r.Probe.Method, r.Probe.Path); err != nil {
		return err
	}
	if strings.Contains(r.Probe.Path, "{") {
		return fmt.Errorf("%w: recipe %q probe path %q must be literal — the probe takes no parameters", ErrInvalid, r.ID, r.Probe.Path)
	}
	if len(r.Verbs) == 0 {
		return fmt.Errorf("%w: %s recipe %q must declare at least one verb tool", ErrInvalid, RecipeKindHTTP, r.ID)
	}
	seen := make(map[string]bool, len(r.Verbs))
	for i := range r.Verbs {
		v := &r.Verbs[i]
		if err := validateRecipeVerb(r, v); err != nil {
			return err
		}
		if seen[v.Tool] {
			return fmt.Errorf("%w: recipe %q verb tool %q is duplicated", ErrInvalid, r.ID, v.Tool)
		}
		seen[v.Tool] = true
	}
	return nil
}

// validateHTTPCall checks one http-kind request declaration (the probe call
// or a verb's method + path template): a canonical HTTP method and a rooted,
// placeholder-free structural path. Placeholder well-formedness is checked
// here; placeholder-to-parameter matching happens per verb.
func validateHTTPCall(recipeID, what, method, path string) error {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return fmt.Errorf("%w: recipe %q %s method %q must be one of GET, POST, PUT, PATCH, or DELETE", ErrInvalid, recipeID, what, method)
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("%w: recipe %q %s path %q must be rooted", ErrInvalid, recipeID, what, path)
	}
	if strings.ContainsAny(path, "?#") {
		return fmt.Errorf("%w: recipe %q %s path %q must not carry a query or fragment — parameters are declared, not embedded", ErrInvalid, recipeID, what, path)
	}
	if _, ok := pathTemplateParams(path); !ok {
		return fmt.Errorf("%w: recipe %q %s path %q has malformed {param} placeholders", ErrInvalid, recipeID, what, path)
	}
	return nil
}

// validateRecipeVerb checks one declared verb against its recipe: the
// service-prefixed tool name, the request shape, and the parameter schema —
// every path placeholder declared, every declared path parameter used.
func validateRecipeVerb(r *Recipe, v *RecipeVerb) error {
	prefix := r.ID + "."
	if !strings.HasPrefix(v.Tool, prefix) || strings.TrimSpace(strings.TrimPrefix(v.Tool, prefix)) == "" {
		return fmt.Errorf("%w: recipe %q verb tool %q must be service-prefixed (%s.<verb>)", ErrInvalid, r.ID, v.Tool, r.ID)
	}
	if strings.ContainsAny(v.Tool, " \t\r\n") {
		return fmt.Errorf("%w: recipe %q verb tool %q must not contain whitespace", ErrInvalid, r.ID, v.Tool)
	}
	if err := validateVerbTier(r, v); err != nil {
		return err
	}
	if err := validateHTTPCall(r.ID, "verb "+v.Tool+" path", v.Method, v.Path); err != nil {
		return err
	}

	placeholders, ok := pathTemplateParams(v.Path)
	if !ok {
		return fmt.Errorf("%w: recipe %q verb %q path %q has malformed {param} placeholders", ErrInvalid, r.ID, v.Tool, v.Path)
	}
	pathParams := make(map[string]bool)
	seenParams := make(map[string]bool, len(v.Params))
	for _, p := range v.Params {
		if !isValidParamName(p.Name) {
			return fmt.Errorf("%w: recipe %q verb %q parameter %q is not a valid name", ErrInvalid, r.ID, v.Tool, p.Name)
		}
		if seenParams[p.Name] {
			return fmt.Errorf("%w: recipe %q verb %q parameter %q is duplicated", ErrInvalid, r.ID, v.Tool, p.Name)
		}
		seenParams[p.Name] = true
		switch p.In {
		case RecipeParamInPath:
			if !p.Required {
				return fmt.Errorf("%w: recipe %q verb %q path parameter %q must be required", ErrInvalid, r.ID, v.Tool, p.Name)
			}
			pathParams[p.Name] = true
		case RecipeParamInQuery:
		default:
			return fmt.Errorf("%w: recipe %q verb %q parameter %q location %q must be %s or %s", ErrInvalid, r.ID, v.Tool, p.Name, p.In, RecipeParamInPath, RecipeParamInQuery)
		}
		switch p.Type {
		case RecipeParamString, RecipeParamNumber, RecipeParamBoolean:
		default:
			return fmt.Errorf("%w: recipe %q verb %q parameter %q type %q must be %s, %s, or %s", ErrInvalid, r.ID, v.Tool, p.Name, p.Type, RecipeParamString, RecipeParamNumber, RecipeParamBoolean)
		}
	}
	for _, name := range placeholders {
		if !pathParams[name] {
			return fmt.Errorf("%w: recipe %q verb %q path %q placeholder {%s} is not declared as a path parameter", ErrInvalid, r.ID, v.Tool, v.Path, name)
		}
		delete(pathParams, name)
	}
	for name := range pathParams {
		return fmt.Errorf("%w: recipe %q verb %q path parameter %q does not appear in path %q", ErrInvalid, r.ID, v.Tool, name, v.Path)
	}
	return nil
}

// pathTemplateParams extracts a path template's {placeholder} names in order
// of first appearance. ok is false when a brace is unbalanced or nested, or a
// placeholder is empty or malformed — the structural check validation runs
// before parameter matching.
func pathTemplateParams(path string) (names []string, ok bool) {
	depth, start := 0, 0
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case '{':
			if depth > 0 {
				return nil, false
			}
			depth, start = 1, i+1
		case '}':
			if depth == 0 {
				return nil, false
			}
			name := path[start:i]
			if !isValidParamName(name) {
				return nil, false
			}
			names = append(names, name)
			depth = 0
		}
	}
	if depth != 0 {
		return nil, false
	}
	return names, true
}

// isValidParamName reports whether name is a well-formed parameter or
// placeholder identifier: non-empty letters, digits, underscores, or hyphens.
func isValidParamName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// recipeRegistry is the in-Go recipe registry (design.md D2). Built-ins ship
// as ordinary registrations through RegisterRecipe; a future plugin-supplied
// recipe uses the same door. Order is insertion order — the gallery's order.
var (
	recipeMu       sync.RWMutex
	recipeOrder    []string
	recipeRegistry = make(map[string]Recipe)
)

// RegisterRecipe registers an integration recipe. It panics on a duplicate id
// or an invalid recipe — registration is a startup-time programming error,
// mirroring the store driver registry.
func RegisterRecipe(r Recipe) {
	if err := ValidateRecipe(&r); err != nil {
		panic(fmt.Sprintf("domain: invalid recipe %q: %v", r.ID, err))
	}
	recipeMu.Lock()
	defer recipeMu.Unlock()
	if _, exists := recipeRegistry[r.ID]; exists {
		panic(fmt.Sprintf("domain: recipe %q already registered", r.ID))
	}
	// Normalize nil slices to empty so served JSON is always an array, and
	// the kind to its explicit value so the served shape always carries it.
	if r.Kind == "" {
		r.Kind = RecipeKindMCP
	}
	if r.AccessLevels == nil {
		r.AccessLevels = []string{}
	}
	if r.Steps == nil {
		r.Steps = []RecipeStep{}
	}
	if r.Scopes == nil {
		r.Scopes = []RecipeScopes{}
	}
	if r.Verbs == nil {
		r.Verbs = []RecipeVerb{}
	}
	for i := range r.Verbs {
		if r.Verbs[i].Params == nil {
			r.Verbs[i].Params = []RecipeVerbParam{}
		}
	}
	// The declaration is stored detached from the caller's object so a later
	// mutation of the caller's RecipeWebhook cannot reach the registry.
	r.Webhooks = copyRecipeWebhooks(r.Webhooks)
	recipeRegistry[r.ID] = r
	recipeOrder = append(recipeOrder, r.ID)
}

// copyRecipeWebhooks deep-copies a recipe's webhook declaration — the slices
// it carries have no shared backing arrays with the source — so neither
// registration nor a read can hand out a mutable path into the registry (the
// Recipes-returns-copies guarantee, extended to the declaration pointer).
func copyRecipeWebhooks(w *RecipeWebhook) *RecipeWebhook {
	if w == nil {
		return nil
	}
	cp := *w
	cp.Events = append([]string(nil), w.Events...)
	cp.DefaultEvents = append([]string(nil), w.DefaultEvents...)
	cp.Templates = make([]RecipeWebhookTemplate, len(w.Templates))
	for i := range w.Templates {
		cp.Templates[i] = w.Templates[i]
		cp.Templates[i].Fields = append([]string(nil), w.Templates[i].Fields...)
	}
	return &cp
}

// Recipes returns all registered recipes in registration order. The returned
// slice is a copy; mutating it (or its webhook declarations) does not affect
// the registry.
func Recipes() []Recipe {
	recipeMu.RLock()
	defer recipeMu.RUnlock()
	out := make([]Recipe, 0, len(recipeOrder))
	for _, id := range recipeOrder {
		r := recipeRegistry[id]
		r.Webhooks = copyRecipeWebhooks(r.Webhooks)
		out = append(out, r)
	}
	return out
}

// RecipeByID returns the registered recipe with the given id, or nil when the
// id is not registered (the caller maps that to ErrUnknownRecipe).
func RecipeByID(id string) *Recipe {
	recipeMu.RLock()
	defer recipeMu.RUnlock()
	r, exists := recipeRegistry[id]
	if !exists {
		return nil
	}
	r.Webhooks = copyRecipeWebhooks(r.Webhooks)
	return &r
}
