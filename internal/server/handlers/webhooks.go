package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// ---------------------------------------------------------------------------
// Connection webhook surface (add-connection-webhooks tasks 3.1): the
// management sub-resource under the workspace integrations group — every
// route guarded by domain.IntegrationsWrite, the credential-bearing trust
// tier (the HMAC secret's reveal-once makes it exactly the connect tier) —
// and the public ingest route, where the delivery's signature IS the
// authentication.
// ---------------------------------------------------------------------------

// webhookHandlers serves webhook management and public delivery ingress.
type webhookHandlers struct {
	service *webhooks.Service
	ingress *webhooks.Ingress
	// workspaces resolves the public route's workspace slug (the ingest
	// route carries no auth middleware, so resolution is by slug here).
	workspaces store.WorkspaceStore
	// publicBaseURL is the instance's externally reachable base the ingest
	// URL derives from (contract §4); empty keeps the path-only URL.
	publicBaseURL string
}

// NewWebhookHandlers creates a new webhookHandlers instance. Every
// dependency is required and resolved by the composition root (injected
// dependencies are never nil).
func NewWebhookHandlers(service *webhooks.Service, ingress *webhooks.Ingress, workspaces store.WorkspaceStore, publicBaseURL string) *webhookHandlers {
	return &webhookHandlers{
		service:       service,
		ingress:       ingress,
		workspaces:    workspaces,
		publicBaseURL: publicBaseURL,
	}
}

// respondWebhookView builds the contract §4 view from stored state with the
// request-scoped workspace slug and responds with the standard envelope.
func (h *webhookHandlers) respondWebhookView(c *gin.Context, state *domain.ConnectionWebhook) {
	ws := MustCurrentWorkspace(c)
	RespondOK(c, gin.H{"webhook": webhooks.BuildView(state, ws.Slug, h.publicBaseURL)})
}

// GetWebhook returns the connection's webhook state (no secret material —
// the hint only, contract §4: display-once).
func (h *webhookHandlers) GetWebhook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	state, err := h.service.Get(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	h.respondWebhookView(c, state)
}

// EnableWebhook enables ingestion: generates the secret (revealed exactly
// once in this response), stores the workspace-bound envelope, and applies
// the target binding and event selection.
func (h *webhookHandlers) EnableWebhook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req webhooks.EnableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	state, secret, err := h.service.Enable(c.Request.Context(), ws.ID, c.Param("id"), req)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{
		"secret":  secret,
		"webhook": webhooks.BuildView(state, ws.Slug, h.publicBaseURL),
	})
}

// DisableWebhook stops ingestion, preserving binding, selection, and the
// secret envelope.
func (h *webhookHandlers) DisableWebhook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	state, err := h.service.Disable(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	h.respondWebhookView(c, state)
}

// RotateWebhook mints a replacement secret — the prior one invalid the
// moment the envelope write lands — revealed exactly once here.
func (h *webhookHandlers) RotateWebhook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	state, secret, err := h.service.Rotate(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{
		"secret":  secret,
		"webhook": webhooks.BuildView(state, ws.Slug, h.publicBaseURL),
	})
}

// UpdateWebhookTarget rebinds the delivery target (agent + kind + id).
func (h *webhookHandlers) UpdateWebhookTarget(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req webhooks.TargetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	state, err := h.service.UpdateTarget(c.Request.Context(), ws.ID, c.Param("id"), req)
	if err != nil {
		RespondError(c, err)
		return
	}
	h.respondWebhookView(c, state)
}

// UpdateWebhookEvents replaces the selected event subset.
func (h *webhookHandlers) UpdateWebhookEvents(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req webhooks.EventsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	state, err := h.service.UpdateEvents(c.Request.Context(), ws.ID, c.Param("id"), req)
	if err != nil {
		RespondError(c, err)
		return
	}
	h.respondWebhookView(c, state)
}

// IngestDelivery is the PUBLIC delivery ingress (tasks 2.1–2.3): no auth
// middleware — the delivery's signature IS the authentication, verified
// constant-time inside the pipeline before any work. Every rejection the
// pipeline classifies as OutcomeUnknown — unknown workspace, unknown
// connection, non-webhook recipe, disabled ingestion, missing signature
// material, failed verification, malformed delivery — gets the same generic
// 404 with no enumeration help (design.md risks); replays and unselected
// events get the provider-friendly 2xx ack.
func (h *webhookHandlers) IngestDelivery(c *gin.Context) {
	ws, err := h.workspaces.BySlug(c.Request.Context(), c.Param("workspaceSlug"))
	if err != nil {
		h.ingestUnknown(c)
		return
	}

	body, ok := webhooks.ReadBody(c.Request.Body)
	if !ok {
		h.ingestUnknown(c)
		return
	}

	outcome, err := h.ingress.Deliver(c.Request.Context(), webhooks.DeliverInput{
		WorkspaceID:  ws.ID,
		ConnectionID: c.Param("connectionId"),
		Header:       c.Request.Header,
		Body:         body,
	})
	if err != nil {
		// Internal failures are logged inside the pipeline; the response
		// carries nothing but the generic envelope.
		AbortWithError(c, http.StatusInternalServerError, CodeInternal, "internal server error")
		return
	}
	switch outcome {
	case webhooks.OutcomeDelivered, webhooks.OutcomeAcked:
		c.JSON(http.StatusOK, gin.H{"ok": true})
	case webhooks.OutcomeUnknown:
		h.ingestUnknown(c)
	default:
		AbortWithError(c, http.StatusInternalServerError, CodeInternal, "internal server error")
	}
}

// ingestUnknown is the public route's one generic rejection: identical
// status, envelope, and message whether the workspace slug, connection id,
// enablement, or signature failed — no enumeration oracle.
func (h *webhookHandlers) ingestUnknown(c *gin.Context) {
	AbortWithError(c, http.StatusNotFound, CodeNotFound, "not found")
}
