package domain

import (
	"fmt"
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

// RecipeProbe declares the probe action (design.md D3): the MCP tool call the
// connect flow runs against the candidate connection before anything is
// stored, and that on-demand status refresh re-runs.
type RecipeProbe struct {
	Tool string `json:"tool"`
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
	// Transport is one of the MCPTransport* constants — the materialized
	// server's transport.
	Transport string `json:"transport"`
	// Endpoint is the remote MCP endpoint URL for URL transports (the
	// materialized server's URL).
	Endpoint string `json:"endpoint,omitempty"`
	// Command is the local command for stdio recipes (the server binary must
	// be present in the deployment image — an operator provisioning concern,
	// design.md D2).
	Command string `json:"command,omitempty"`
	// TokenHeader is the header (URL transports) or env row (stdio) that
	// carries the token as the single secret row (design.md D4).
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
	// AccessLevels lists the supported access levels (ConnectionAccess*
	// constants); the first entry is the flow's default.
	AccessLevels []string       `json:"access_levels"`
	Steps        []RecipeStep   `json:"steps"`
	Scopes       []RecipeScopes `json:"scopes"`
	Probe        RecipeProbe    `json:"probe"`
	// Notes carries the card's truthful coming-soon copy or operator
	// provisioning notes; empty for available recipes that need none.
	Notes string `json:"notes,omitempty"`
}

// ValidateRecipe checks a recipe's declared shape: identity, auth kind,
// availability, transport, endpoint/command per transport, supported access
// levels, guided steps, scopes, and the probe declaration. Invalid recipes are
// a registration-time programming error, not a runtime condition.
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

	if strings.TrimSpace(r.Probe.Tool) == "" {
		return fmt.Errorf("%w: recipe %q probe tool cannot be empty", ErrInvalid, r.ID)
	}
	return nil
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
	// Normalize nil slices to empty so served JSON is always an array.
	if r.AccessLevels == nil {
		r.AccessLevels = []string{}
	}
	if r.Steps == nil {
		r.Steps = []RecipeStep{}
	}
	if r.Scopes == nil {
		r.Scopes = []RecipeScopes{}
	}
	recipeRegistry[r.ID] = r
	recipeOrder = append(recipeOrder, r.ID)
}

// Recipes returns all registered recipes in registration order. The returned
// slice is a copy; mutating it does not affect the registry.
func Recipes() []Recipe {
	recipeMu.RLock()
	defer recipeMu.RUnlock()
	out := make([]Recipe, 0, len(recipeOrder))
	for _, id := range recipeOrder {
		out = append(out, recipeRegistry[id])
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
	return &r
}
