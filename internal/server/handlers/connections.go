package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
)

// ---------------------------------------------------------------------------
// Workspace service connections (add-workspace-connections tasks 3.1/3.2):
// the recipes gallery and the connection lifecycle under
// /workspaces/:ws/integrations/. Reads — recipes, gallery, connections — ride
// workspace membership; connect, probe, and disconnect are guarded with
// domain.IntegrationsWrite (design.md D10, same trust tier as gateways.write).
// ---------------------------------------------------------------------------

// connectionsHandlers serves the recipe registry and the connection lifecycle.
type connectionsHandlers struct {
	service *services.ConnectionsService
	// invalidator drops the connection manager's cached entry for a
	// materialized server when its owning connection is disconnected, so the
	// teardown takes effect immediately (the MCP delete convention).
	invalidator MCPInvalidator
}

// NewConnectionsHandlers creates a new connectionsHandlers instance.
func NewConnectionsHandlers(service *services.ConnectionsService, invalidator MCPInvalidator) *connectionsHandlers {
	return &connectionsHandlers{service: service, invalidator: invalidator}
}

// connectRequest is the connect payload — exactly (recipe id, access level,
// token) per design.md D7: endpoints come from the recipe registry, never
// user input. An empty access level selects the recipe's read-only default.
type connectRequest struct {
	RecipeID    string `json:"recipe_id"`
	AccessLevel string `json:"access_level"`
	Token       string `json:"token"`
}

// respondConnectionError maps probe failures to 400 invalid_request with the
// upstream message verbatim (the connect dialog surfaces it so the user can
// correct the token); everything else rides the standard sentinel mapping —
// unknown recipe and bad access level chain to ErrInvalid (400), duplicate
// service to ErrConnectionExists (409), permission to 403.
func respondConnectionError(c *gin.Context, err error) {
	if errors.Is(err, services.ErrProbeFailed) {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, err.Error())
		return
	}
	RespondError(c, err)
}

// ListRecipes returns the recipe registry with the availability computed at
// serve time (add-connection-oauth tasks.md 2.5): an OAuth recipe is
// available iff its provider's instance app is registered, otherwise it stays
// a coming-soon card — the flip needs only the registration, no code change.
func (h *connectionsHandlers) ListRecipes(c *gin.Context) {
	RespondOK(c, gin.H{"recipes": h.service.EnrichRecipes(c.Request.Context())})
}

// ListConnections returns the workspace's connections as joined views.
func (h *connectionsHandlers) ListConnections(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	views, err := h.service.List(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"connections": views})
}

// GetConnection returns one connection as its joined view.
func (h *connectionsHandlers) GetConnection(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	view, err := h.service.Get(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"connection": view})
}

// Connect runs the connect flow (tasks.md 2.1/2.2): a PAT connect probes and
// persists synchronously, returning 201 with the joined view; an OAuth recipe
// dispatches to the authorize builder and returns 200 with
// `{"authorize_url"}` — the consent round trip completes at the public
// callback. A probe failure stores nothing and returns 400 with the upstream
// message; a missing app registration fails with an error naming it.
func (h *connectionsHandlers) Connect(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	var req connectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	result, err := h.service.Connect(c.Request.Context(), ws.ID, user.ID, req.RecipeID, req.AccessLevel, req.Token)
	if err != nil {
		respondConnectionError(c, err)
		return
	}
	if result.AuthorizeURL != "" {
		RespondOK(c, gin.H{"authorize_url": result.AuthorizeURL})
		return
	}
	RespondJSON(c, http.StatusCreated, gin.H{"connection": result.Connection})
}

// ReauthorizeConnection starts a fresh consent flow for an existing OAuth
// connection (add-connection-oauth tasks.md 2.4): the response is 200 with
// `{"authorize_url"}`, and the callback replaces the connection's token set
// in place — id, server, and attachments preserved.
func (h *connectionsHandlers) ReauthorizeConnection(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	authorizeURL, err := h.service.BeginReauthorize(c.Request.Context(), ws.ID, user.ID, c.Param("id"))
	if err != nil {
		respondConnectionError(c, err)
		return
	}
	RespondOK(c, gin.H{"authorize_url": authorizeURL})
}

// Disconnect removes the connection through the store's atomic cascade and
// invalidates the manager's cached entry for the materialized server.
func (h *connectionsHandlers) Disconnect(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	serverID, err := h.service.Disconnect(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	if serverID != "" {
		h.invalidator.Invalidate(ws.ID, serverID)
	}
	RespondNoContent(c)
}

// ProbeConnection re-runs the recipe probe against the linked server and
// returns the refreshed view — a failed probe is a persisted status, not a
// request error (the MCP registry's probe convention).
func (h *connectionsHandlers) ProbeConnection(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	view, err := h.service.Probe(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"connection": view})
}
