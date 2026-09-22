package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
)

// ---------------------------------------------------------------------------
// OAuth surfaces (add-connection-oauth tasks 3.1/3.2): the PUBLIC callback
// the provider redirects the browser back to — no auth middleware; the signed
// single-use state is the authenticator (design.md D4) — and the instance
// admin OAuth app registry under /admin, guarded by
// domain.AdminIntegrationsWrite.
// ---------------------------------------------------------------------------

// oauthHandlers serves the public OAuth callback.
type oauthHandlers struct {
	service *services.ConnectionsService
	// publicBaseURL is the instance's externally reachable base URL the
	// callback redirects compose from; empty legitimately disables the
	// callback (config contract: validated at use, not at boot).
	publicBaseURL string
}

// NewOAuthHandlers creates a new oauthHandlers instance.
func NewOAuthHandlers(service *services.ConnectionsService, publicBaseURL string) *oauthHandlers {
	return &oauthHandlers{service: service, publicBaseURL: publicBaseURL}
}

// Callback completes the consent round trip (tasks.md 2.2): the service
// validates the state, exchanges the code, probe-gates the candidate, and
// activates or discards the token set; the browser is always bounced back to
// the settings pane's integrations gallery with the outcome in the query —
// `pane=integrations&oauth=<recipe_id>&status=connected|failed&detail=...`.
// Every failure — including a rejected state — is a redirect, never a JSON
// envelope and never a leaked validation detail.
func (h *oauthHandlers) Callback(c *gin.Context) {
	if h.publicBaseURL == "" {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "the OAuth callback is unavailable because the instance public base URL is not configured")
		return
	}

	// Provider-side denial (user declined, app misconfigured): the provider
	// redirects back with error/error_description instead of a code. The
	// signed state (when present and valid) names the recipe so the gallery
	// surfaces the failure inline under the right card; an absent or invalid
	// state keeps the recipe-less redirect.
	if errParam := c.Query("error"); strings.TrimSpace(errParam) != "" {
		detail := strings.TrimSpace(c.Query("error_description"))
		if detail == "" {
			detail = errParam
		}
		c.Redirect(http.StatusFound, h.redirectURL(h.service.RecipeIDFromState(c.Query("state")), services.OAuthCallbackFailed, "the provider declined the authorization: "+detail))
		return
	}

	result := h.service.HandleCallback(c.Request.Context(), c.Query("code"), c.Query("state"))
	c.Redirect(http.StatusFound, h.redirectURL(result.RecipeID, result.Status, result.Detail))
}

// redirectURL composes the settings-pane bounce for a callback outcome.
func (h *oauthHandlers) redirectURL(recipeID, status, detail string) string {
	query := url.Values{}
	query.Set("pane", "integrations")
	if recipeID != "" {
		query.Set("oauth", recipeID)
	}
	query.Set("status", status)
	if detail != "" {
		query.Set("detail", detail)
	}
	return strings.TrimRight(h.publicBaseURL, "/") + "/settings?" + query.Encode()
}

// ---------------------------------------------------------------------------
// Instance admin OAuth app registry (tasks.md 2.6/3.1)
// ---------------------------------------------------------------------------

// oauthAppsHandlers serves the instance's per-provider OAuth app registry.
type oauthAppsHandlers struct {
	service *services.OAuthAppsService
}

// NewOAuthAppsHandlers creates a new oauthAppsHandlers instance.
func NewOAuthAppsHandlers(service *services.OAuthAppsService) *oauthAppsHandlers {
	return &oauthAppsHandlers{service: service}
}

// oauthAppRequest is the PUT payload — exactly (client_id, client_secret):
// the secret is write-only and re-supplied on every save; an update that
// omits it keeps the stored credential.
type oauthAppRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// ListApps returns every registered app: provider, client id, last-4 secret
// hint, the derived redirect URI, and timestamps — never the secret.
func (h *oauthAppsHandlers) ListApps(c *gin.Context) {
	views, err := h.service.List(c.Request.Context())
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"apps": views})
}

// GetApp returns one provider's registered app; an unregistered provider is
// 404 not_found.
func (h *oauthAppsHandlers) GetApp(c *gin.Context) {
	view, err := h.service.Get(c.Request.Context(), c.Param("provider"))
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"app": view})
}

// PutApp create-or-replaces the provider's app. The client secret is sealed
// with the instance-scoped key derivation at the service; the response
// carries the hint and the derived redirect URI, never the secret.
func (h *oauthAppsHandlers) PutApp(c *gin.Context) {
	var req oauthAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}
	view, err := h.service.Upsert(c.Request.Context(), c.Param("provider"), req.ClientID, req.ClientSecret)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"app": view})
}
