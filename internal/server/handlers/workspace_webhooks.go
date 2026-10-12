package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/webhookfilter"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// ---------------------------------------------------------------------------
// Standalone workspace webhook surface (add-workspace-webhooks tasks 5.1/5.2):
// the management sub-resource under the workspace integrations group — every
// route guarded by domain.IntegrationsWrite (the credential-bearing trust
// tier; the secret's reveal-once makes it exactly the connect tier, the
// connection-webhook contract) — and the public ingest route, where the
// delivery's signature IS the authentication.
//
// Setup copy for senders (the scheme's conventional signature headers):
//   - plain_token:       X-Gitlab-Token (the secret verbatim)
//   - hmac_sha256:       X-Hub-Signature-256 ("sha256=" + hex HMAC-SHA256 of
//                        the raw body; the bare hex also verifies)
//   - timestamped_hmac:  X-Webhook-Signature ("t=<unix>,v1=<hex>" over
//                        "{timestamp}.{body}", ±300s skew — keep the sender's
//                        clock NTP-synced)
// ---------------------------------------------------------------------------

// workspaceWebhookHandlers serves standalone webhook management and public
// endpoint delivery ingress.
type workspaceWebhookHandlers struct {
	service *webhooks.StandaloneService
	ingress *webhooks.StandaloneIngress
	// workspaces resolves the public route's workspace slug (the ingest
	// route carries no auth middleware, so resolution is by slug here).
	workspaces store.WorkspaceStore
	// publicBaseURL is the instance's externally reachable base the ingest
	// URL derives from (the connection webhook contract's derived-URL
	// posture); empty keeps the path-only URL.
	publicBaseURL string
}

// NewWorkspaceWebhookHandlers creates a new workspaceWebhookHandlers
// instance. Every dependency is required and resolved by the composition
// root (injected dependencies are never nil).
func NewWorkspaceWebhookHandlers(service *webhooks.StandaloneService, ingress *webhooks.StandaloneIngress, workspaces store.WorkspaceStore, publicBaseURL string) *workspaceWebhookHandlers {
	return &workspaceWebhookHandlers{
		service:       service,
		ingress:       ingress,
		workspaces:    workspaces,
		publicBaseURL: publicBaseURL,
	}
}

// respondWebhookView responds with the stored endpoint plus its derived
// ingest URL (computed, never stored — the connection view's BuildView
// posture). The plaintext secret never appears; the ciphertext never
// serializes.
func (h *workspaceWebhookHandlers) respondWebhookView(c *gin.Context, wh *domain.WorkspaceWebhook) {
	ws := MustCurrentWorkspace(c)
	RespondOK(c, gin.H{
		"webhook":    wh,
		"ingest_url": webhooks.StandaloneIngestURL(h.publicBaseURL, ws.Slug, wh.ID),
	})
}

// ListWebhooks returns the workspace's webhook endpoints: standalone
// endpoints plus connection-bound webhooks surfaced read-in (design.md
// D2/D9 — the read-in rows link to Integrations for management).
func (h *workspaceWebhookHandlers) ListWebhooks(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	rows, err := h.service.List(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	payloads := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		switch row.Source {
		case webhooks.StandaloneWebhookSourceStandalone:
			payloads = append(payloads, gin.H{
				"source":     row.Source,
				"webhook":    row.Webhook,
				"ingest_url": webhooks.StandaloneIngestURL(h.publicBaseURL, ws.Slug, row.Webhook.ID),
			})
		case webhooks.StandaloneWebhookSourceConnection:
			payloads = append(payloads, gin.H{
				"source":        row.Source,
				"connection_id": row.Connection.ConnectionID,
				"service":       row.Connection.Service,
				// The connection lane's own view (its ingest URL, target,
				// events, last error) — the exact enrichment the connections
				// handler serves.
				"webhook": webhooks.BuildView(row.Connection.State, ws.Slug, h.publicBaseURL),
			})
		}
	}
	RespondOK(c, gin.H{"webhooks": payloads})
}

// CreateWebhook creates an endpoint: generated secrets ride the response
// exactly once (spec: Create with generated secret); BYO secrets are stored
// and never re-revealed. An invalid filter expression rejects 400 with the
// parser's message and persists nothing.
func (h *workspaceWebhookHandlers) CreateWebhook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req webhooks.CreateStandaloneWebhookInput
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	created, err := h.service.Create(c.Request.Context(), ws.ID, req)
	if err != nil {
		RespondError(c, err)
		return
	}
	body := gin.H{
		"webhook":    created.Webhook,
		"ingest_url": webhooks.StandaloneIngestURL(h.publicBaseURL, ws.Slug, created.Webhook.ID),
	}
	if created.Secret != "" {
		// Reveal-once: the generated plaintext appears here and never again.
		body["secret"] = created.Secret
	}
	RespondCreated(c, body)
}

// GetWebhook returns one endpoint (no secret material — the hint only).
func (h *workspaceWebhookHandlers) GetWebhook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	wh, err := h.service.Get(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	h.respondWebhookView(c, wh)
}

// UpdateWebhook applies the full-state edit. A filter parse error rejects
// 400 with the parser's message and the prior expression stays in force.
func (h *workspaceWebhookHandlers) UpdateWebhook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req webhooks.UpdateStandaloneWebhookInput
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	wh, err := h.service.Update(c.Request.Context(), ws.ID, c.Param("id"), req)
	if err != nil {
		RespondError(c, err)
		return
	}
	h.respondWebhookView(c, wh)
}

// DeleteWebhook removes the endpoint; ingestion stops with it.
func (h *workspaceWebhookHandlers) DeleteWebhook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	if err := h.service.Delete(c.Request.Context(), ws.ID, c.Param("id")); err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"ok": true})
}

// RotateWebhook mints a replacement secret — the prior one invalid the
// moment the envelope write lands — revealed exactly once here.
func (h *workspaceWebhookHandlers) RotateWebhook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	revealed, err := h.service.Rotate(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{
		"secret":     revealed.Secret,
		"webhook":    revealed.Webhook,
		"ingest_url": webhooks.StandaloneIngestURL(h.publicBaseURL, ws.Slug, revealed.Webhook.ID),
	})
}

// EnableWebhook resumes ingestion; DisableWebhook pauses it (spec: Paused
// endpoint ignores deliveries without verification side effects).
func (h *workspaceWebhookHandlers) EnableWebhook(c *gin.Context) {
	h.setEnabled(c, true)
}

// DisableWebhook pauses the endpoint.
func (h *workspaceWebhookHandlers) DisableWebhook(c *gin.Context) {
	h.setEnabled(c, false)
}

func (h *workspaceWebhookHandlers) setEnabled(c *gin.Context, enabled bool) {
	ws := MustCurrentWorkspace(c)

	wh, err := h.service.SetEnabled(c.Request.Context(), ws.ID, c.Param("id"), enabled)
	if err != nil {
		RespondError(c, err)
		return
	}
	h.respondWebhookView(c, wh)
}

// dryRunRequest is the dry-run payload (spec: "a supplied sample payload");
// arbitrary JSON under the payload key.
type dryRunRequest struct {
	Payload json.RawMessage `json:"payload"`
}

// DryRunWebhook evaluates the sample payload against the endpoint's saved
// expression and reports match, ignored, or error — no side effects, no
// store writes, no enqueueing (spec: Dry-run reports the outcome).
func (h *workspaceWebhookHandlers) DryRunWebhook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req dryRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}
	if len(req.Payload) == 0 {
		RespondError(c, fmt.Errorf("%w: a dry run requires a sample payload", domain.ErrInvalid))
		return
	}

	verdict, err := h.service.DryRun(c.Request.Context(), ws.ID, c.Param("id"), req.Payload)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"verdict": renderVerdict(verdict)})
}

// renderVerdict maps the dry-run verdict onto its wire form: the outcome as
// its catalog string, the message carried for errors.
func renderVerdict(v webhookfilter.Verdict) gin.H {
	outcome := "match"
	switch v.Outcome {
	case webhookfilter.VerdictIgnored:
		outcome = "ignored"
	case webhookfilter.VerdictError:
		outcome = "error"
	}
	body := gin.H{"outcome": outcome}
	if v.Message != "" {
		body["message"] = v.Message
	}
	return body
}

// WebhookDeliveryLog returns the endpoint's bounded delivery history
// (design.md D8 — "did the sender's event reach the agent, and if not, why
// not"), newest first.
func (h *workspaceWebhookHandlers) WebhookDeliveryLog(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	entries, err := h.service.DeliveryLog(c.Request.Context(), ws.ID, c.Param("id"), domain.WorkspaceWebhookDeliveryLogKeep)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"entries": entries})
}

// ListWorkspaceWebhookRuns serves the workspace-wide webhook run listing
// (add-webhook-channel-visibility 4.4): the submitted deliveries of every
// endpoint, projected as runs with their live status — the run-history
// screen's feed, beside the scheduler runs. Bounded at the screen's page
// size; the route rides integrations.write (the webhooks pane's trust
// tier — the feed exposes endpoint names and event shapes).
func (h *workspaceWebhookHandlers) ListWorkspaceWebhookRuns(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	runs, err := h.service.WorkspaceRuns(c.Request.Context(), ws.ID, domain.WorkspaceWebhookRunsLimit)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"runs": runs})
}

// IngestStandaloneDelivery is the PUBLIC delivery ingress for standalone
// endpoints (tasks 4.1–4.3, design.md D5): no auth middleware — the
// delivery's signature IS the authentication, verified constant-time inside
// the pipeline before any work. The response mapping is the spec's exact
// contract:
//
//   - Submitted / duplicate / generic ack  → 200 {"ok": true}
//   - Filtered                             → 200 {"status":"ignored","reason":"filter"}
//   - Rejected (unknown slug or endpoint, paused, missing or invalid
//     signature material, malformed, over-cap, replayed signature)
//     → the SAME generic unenumerable 404 — no distinguishing detail, the
//     connection ingest's exact no-enumeration parity
//   - Rejected with ErrRateLimited         → 429 + Retry-After (the ONE
//     enumerable rejection)
//   - Failed                               → 500 internal
func (h *workspaceWebhookHandlers) IngestStandaloneDelivery(c *gin.Context) {
	ws, err := h.workspaces.BySlug(c.Request.Context(), c.Param("workspaceSlug"))
	if err != nil {
		h.ingestUnknown(c)
		return
	}

	body, ok := webhooks.ReadStandaloneBody(c.Request.Body)
	if !ok {
		// Over-cap (or unreadable) is a generic rejection — no cap-size
		// oracle (design.md D5's unenumerable posture).
		h.ingestUnknown(c)
		return
	}

	outcome, err := h.ingress.Deliver(c.Request.Context(), webhooks.StandaloneDeliverInput{
		WorkspaceID: ws.ID,
		WebhookID:   c.Param("webhookId"),
		Header:      c.Request.Header,
		Body:        body,
	})
	// The outcome governs the response; the classified error rides it (the
	// standalone pipeline returns a sentinel with every rejected verdict for
	// tests, logs, and this one permitted split). Internal failures are
	// logged inside the pipeline; the response carries nothing but the
	// generic envelope.
	switch outcome {
	case webhooks.StandaloneOutcomeSubmitted,
		webhooks.StandaloneOutcomeAcked,
		webhooks.StandaloneOutcomeDuplicate:
		c.JSON(http.StatusOK, gin.H{"ok": true})
	case webhooks.StandaloneOutcomeFiltered:
		// The explicit ignored ack (spec: "acknowledged 200 with an explicit
		// ignored reason") — distinct from the plain delivery ack.
		c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "filter"})
	case webhooks.StandaloneOutcomeRejected:
		if errors.Is(err, webhooks.ErrRateLimited) {
			var rle *webhooks.RateLimitError
			if errors.As(err, &rle) {
				c.Header("Retry-After", strconv.Itoa(webhooks.RetryAfterSeconds(rle.RetryAfter)))
			}
			AbortWithError(c, http.StatusTooManyRequests, CodeInvalidRequest, "rate limited")
			return
		}
		h.ingestUnknown(c)
	default:
		// StandaloneOutcomeFailed (or an unclassified verdict): generic 500.
		AbortWithError(c, http.StatusInternalServerError, CodeInternal, "internal server error")
	}
}

// ingestUnknown is the public route's one generic rejection: identical
// status, envelope, and message whether the workspace slug, endpoint id,
// enablement, signature material, size, or signature failed — no enumeration
// oracle (spec: Invalid signature is unenumerable).
func (h *workspaceWebhookHandlers) ingestUnknown(c *gin.Context) {
	AbortWithError(c, http.StatusNotFound, CodeNotFound, "not found")
}
