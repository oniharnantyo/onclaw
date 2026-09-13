package handlers

// Gateway REST surface (integrate-telegram-gateway tasks 6.1/6.2):
// workspace-scoped Telegram gateway configuration, chat bindings, pairing
// tokens, identity links, and the public webhook ingress. Config, bindings,
// and per-member unpair are admin-gated via gateways.write (Owner/Admin);
// pairing-token mint/revoke and the self link/unpair are member-level (any
// membership, the workspace-context gate suffices) — the web pane mirrors
// exactly this split (web/src/lib/api.ts api.gateways.telegram).
//
// The bot token is a write-only secret (spec: "Token never leaks"): it is
// stored as a v1 secrets envelope AAD-bound to the workspace and every read
// carries only the last-4 token_hint. The wire shapes below match
// ApiGatewayConfig / ApiGatewayBinding / ApiGatewayLink / ApiPairingToken in
// web/src/lib/api.ts byte-for-byte; the pane is already built against them.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/gateways"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// webhookSecretInfoKey seeds the HMAC that derives a gateway's webhook
// secret. The derivation is deterministic so the same secret registers with
// Telegram (setWebhook — called by the composition's adapter factory) and
// validates the ingress header below, without any storage.
const webhookSecretInfoKey = "onclaw:gateway-webhook:"

// GatewayWebhookSecret derives the ingress secret for one workspace's
// gateway: HMAC-SHA256 under the instance encryption key, base64url-encoded
// (43 characters of Telegram's allowed A-Z a-z 0-9 _- charset). It is never
// stored and never returned by any API.
func GatewayWebhookSecret(encKey []byte, workspaceID, platform string) string {
	mac := hmac.New(sha256.New, encKey)
	mac.Write([]byte(webhookSecretInfoKey + workspaceID + ":" + platform))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// telegramWebhookSecretHeader is the header Telegram sets on every webhook
// delivery to the secret_token configured via setWebhook.
const telegramWebhookSecretHeader = "X-Telegram-Bot-Api-Secret-Token"

// webhookMaxBodyBytes bounds one webhook update body. Bot API updates are
// far smaller; the cap exists so a hostile caller cannot stream forever.
const webhookMaxBodyBytes = 1 << 20

// GatewayBotVerifier validates a bot token against the platform API and
// resolves the bot username (the config PUT and /test paths). Implemented by
// the composition over the Telegram Bot API getMe.
type GatewayBotVerifier interface {
	VerifyBotToken(ctx context.Context, token string) (botUsername string, err error)
}

// gatewayHandlers carries the granular gateway stores plus the composition
// services the surface drives (pairing mint, lifecycle Sync, token probe).
type gatewayHandlers struct {
	gateways store.GatewayStore
	bindings store.GatewayBindings
	links    store.GatewayLinks
	agents   store.AgentStore
	users    store.UserStore
	pairing  *gateways.PairingService
	manager  *gateways.Manager
	service  *gateways.Service
	verifier GatewayBotVerifier
	encKey   []byte
}

// NewGatewayHandlers builds the gateway handlers. Every dependency is
// required (injected dependencies are never nil); the runtime is resolved
// before construction. The manager drives adapter lifecycle (Sync, webhook
// ingress) and the service carries the circuit breaker's admin resume.
func NewGatewayHandlers(
	gateways store.GatewayStore,
	bindings store.GatewayBindings,
	links store.GatewayLinks,
	agents store.AgentStore,
	users store.UserStore,
	pairing *gateways.PairingService,
	manager *gateways.Manager,
	service *gateways.Service,
	verifier GatewayBotVerifier,
	encKey []byte,
) *gatewayHandlers {
	return &gatewayHandlers{
		gateways: gateways,
		bindings: bindings,
		links:    links,
		agents:   agents,
		users:    users,
		pairing:  pairing,
		manager:  manager,
		service:  service,
		verifier: verifier,
		encKey:   encKey,
	}
}

// ---------------------------------------------------------------------------
// Read views (wire shapes from web/src/lib/api.ts)
// ---------------------------------------------------------------------------

// gatewayConfigView is the ApiGatewayConfig shape. The token never appears —
// only the last-4 hint; status_error is a response-time field (the PUT/test
// verification outcome), never stored.
type gatewayConfigView struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	Platform       string    `json:"platform"`
	Enabled        bool      `json:"enabled"`
	BotUsername    *string   `json:"bot_username"`
	TokenHint      string    `json:"token_hint,omitempty"`
	DefaultAgentID *string   `json:"default_agent_id,omitempty"`
	Transport      string    `json:"transport"`
	WebhookURL     string    `json:"webhook_url,omitempty"`
	StatusError    *string   `json:"status_error"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// gatewayConfigViewOf projects a stored config. tokenHint is precomputed by
// the caller (it needs a decrypt); statusError is response-time context.
func gatewayConfigViewOf(cfg *domain.GatewayConfig, tokenHint, statusError string) gatewayConfigView {
	view := gatewayConfigView{
		ID:             cfg.ID,
		WorkspaceID:    cfg.WorkspaceID,
		Platform:       cfg.Platform,
		Enabled:        cfg.Enabled,
		Transport:      cfg.Transport,
		WebhookURL:     cfg.WebhookURL,
		DefaultAgentID: cfg.DefaultAgentID,
		CreatedAt:      cfg.CreatedAt,
		UpdatedAt:      cfg.UpdatedAt,
	}
	if cfg.BotUsername != "" {
		view.BotUsername = &cfg.BotUsername
	}
	view.TokenHint = tokenHint
	if statusError != "" {
		view.StatusError = &statusError
	}
	return view
}

// gatewayBindingView is the ApiGatewayBinding shape. chat_title is accepted
// at create time and echoed only on the create response — the schema has no
// title column (design D2's minimal binding row), so listings carry null.
type gatewayBindingView struct {
	ID             string    `json:"id"`
	Platform       string    `json:"platform"`
	PlatformChatID string    `json:"platform_chat_id"`
	ChatTitle      *string   `json:"chat_title"`
	AgentID        string    `json:"agent_id"`
	CreatedAt      time.Time `json:"created_at"`
}

func gatewayBindingViewOf(b *domain.ChatBinding, chatTitle string) gatewayBindingView {
	view := gatewayBindingView{
		ID:             b.ID,
		Platform:       b.Platform,
		PlatformChatID: b.PlatformChatID,
		AgentID:        b.AgentID,
		CreatedAt:      b.CreatedAt,
	}
	if chatTitle != "" {
		view.ChatTitle = &chatTitle
	}
	return view
}

// gatewayLinkView is the ApiGatewayLink shape.
type gatewayLinkView struct {
	PlatformUserID string    `json:"platform_user_id"`
	Username       *string   `json:"username"`
	DisplayName    *string   `json:"display_name"`
	LinkedAt       time.Time `json:"linked_at"`
}

func (h *gatewayHandlers) linkView(ctx context.Context, l *domain.UserLink) gatewayLinkView {
	view := gatewayLinkView{
		PlatformUserID: l.PlatformUserID,
		LinkedAt:       l.CreatedAt,
	}
	if l.PlatformUsername != "" {
		username := l.PlatformUsername
		view.Username = &username
	}
	if u, err := h.users.ByID(ctx, l.UserID); err == nil && u != nil && u.Name != "" {
		name := u.Name
		view.DisplayName = &name
	}
	return view
}

// ---------------------------------------------------------------------------
// Payloads
// ---------------------------------------------------------------------------

// gatewayConfigPayload is the PUT body (GatewayConfigPayload): an omitted or
// empty token keeps the stored secret; default_agent_id null clears it.
type gatewayConfigPayload struct {
	Token          string  `json:"token"`
	DefaultAgentID *string `json:"default_agent_id"`
	Transport      string  `json:"transport"`
	WebhookURL     string  `json:"webhook_url"`
}

// gatewayBindingPayload is the binding-create body.
type gatewayBindingPayload struct {
	AgentID        string `json:"agent_id"`
	PlatformChatID string `json:"platform_chat_id"`
	ChatTitle      string `json:"chat_title"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// tokenHint decrypts the stored envelope and returns the plaintext's last
// four characters (the write-only hint). Undecryptable rows carry no hint
// rather than failing the read.
func (h *gatewayHandlers) tokenHint(ctx context.Context, workspaceID string, cfg *domain.GatewayConfig) string {
	plaintext, err := secrets.Decrypt(h.encKey, []byte(workspaceID), cfg.BotTokenCiphertext)
	if err != nil || len(plaintext) < 4 {
		return ""
	}
	return string(plaintext[len(plaintext)-4:])
}

// syncManager reconciles the lifecycle manager after a config mutation
// (design D11: gateways restart on config changes). The returned message, if
// any, is surfaced on the response's status_error — a saved-but-not-running
// gateway must be visible to the admin, not silently swallowed.
func (h *gatewayHandlers) syncManager(ctx context.Context, workspaceID string) string {
	if err := h.manager.Sync(ctx, workspaceID); err != nil {
		return err.Error()
	}
	return ""
}

// respondGateway writes 200 {gateway: view} after loading the fresh row —
// unconfigured workspaces answer {gateway: null}, never 404. statusError,
// when non-empty, rides the response as response-time context.
func (h *gatewayHandlers) respondGateway(c *gin.Context, statusError string) {
	ws := MustCurrentWorkspace(c)
	cfg, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, domain.GatewayPlatformTelegram)
	if err != nil {
		RespondError(c, err)
		return
	}
	if cfg == nil {
		c.JSON(http.StatusOK, gin.H{"gateway": nil})
		return
	}
	hint := h.tokenHint(c.Request.Context(), ws.ID, cfg)
	c.JSON(http.StatusOK, gin.H{"gateway": gatewayConfigViewOf(cfg, hint, statusError)})
}

// ---------------------------------------------------------------------------
// Config (gateways.write)
// ---------------------------------------------------------------------------

// GetConfig answers {gateway: null} for unconfigured workspaces, never 404.
func (h *gatewayHandlers) GetConfig(c *gin.Context) {
	h.respondGateway(c, "")
}

// PutConfig creates or updates the gateway configuration. A submitted token
// is verified best-effort against the platform API (a network-less or
// firewalled instance must still be able to save; a failed probe surfaces on
// status_error), encrypted with the workspace as AAD, and never echoed.
func (h *gatewayHandlers) PutConfig(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	var payload gatewayConfigPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	existing, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, domain.GatewayPlatformTelegram)
	if err != nil {
		RespondError(c, err)
		return
	}

	token := trimOrEmpty(payload.Token)
	if existing == nil && token == "" {
		respondGatewayValidationError(c, gatewayFieldError("token", "a bot token is required to connect the gateway"))
		return
	}

	// Transport: absent keeps the stored value; a first save defaults to
	// long polling (works behind NAT — design D12).
	transport := existingTransport(existing, payload.Transport)
	if transport != domain.GatewayTransportWebhook && transport != domain.GatewayTransportLongPolling {
		respondGatewayValidationError(c, gatewayFieldError("transport", "transport must be \"webhook\" or \"long_polling\""))
		return
	}

	// Default agent: null clears; a value must name a workspace agent.
	defaultAgentID := existingDefaultAgent(existing)
	if payload.DefaultAgentID != nil {
		trimmed := trimOrEmpty(*payload.DefaultAgentID)
		if trimmed == "" {
			defaultAgentID = nil
		} else if _, err := h.agents.ByID(c.Request.Context(), ws.ID, trimmed); err != nil {
			respondGatewayValidationError(c, gatewayFieldError("default_agent_id", "no agent with this id exists in the workspace"))
			return
		} else {
			defaultAgentID = &trimmed
		}
	}

	statusError := ""
	botUsername := ""
	if existing != nil {
		botUsername = existing.BotUsername
	}
	ciphertext := ""
	if existing != nil {
		ciphertext = existing.BotTokenCiphertext
	}
	if token != "" {
		username, verr := h.verifier.VerifyBotToken(c.Request.Context(), token)
		if verr != nil {
			statusError = verr.Error()
		} else {
			botUsername = username
		}
		envelope, encErr := secrets.Encrypt(h.encKey, []byte(ws.ID), []byte(token))
		if encErr != nil {
			RespondError(c, encErr)
			return
		}
		ciphertext = envelope
	}

	cfg := &domain.GatewayConfig{
		WorkspaceID:        ws.ID,
		Platform:           domain.GatewayPlatformTelegram,
		BotTokenCiphertext: ciphertext,
		BotUsername:        botUsername,
		Enabled:            existingEnabled(existing),
		Transport:          transport,
		WebhookURL:         payload.WebhookURL,
		DefaultAgentID:     defaultAgentID,
	}
	if err := domain.ValidateGatewayConfig(cfg); err != nil {
		RespondError(c, err)
		return
	}
	if err := h.gateways.UpsertGateway(c.Request.Context(), ws.ID, cfg); err != nil {
		RespondError(c, err)
		return
	}

	// Config changes restart the gateway (design D11). A sync failure (bad
	// token, unreachable platform) leaves the config saved but inert — the
	// response carries the reason on status_error.
	if syncErr := h.syncManager(c.Request.Context(), ws.ID); syncErr != "" && statusError == "" {
		statusError = syncErr
	}

	// respondGateway re-reads the row; the token hint comes from the stored
	// envelope, so a freshly submitted token is hinted identically.
	h.respondGateway(c, statusError)
}

// EnableGateway turns the gateway on (an explicit admin action also clears a
// tripped circuit breaker — design D10's "resume only on explicit admin
// action") and reconciles the adapter.
func (h *gatewayHandlers) EnableGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	if err := h.gateways.SetGatewayEnabled(c.Request.Context(), ws.ID, domain.GatewayPlatformTelegram, true); err != nil {
		RespondError(c, err)
		return
	}
	cfg, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, domain.GatewayPlatformTelegram)
	if err != nil {
		RespondError(c, err)
		return
	}
	if cfg != nil {
		h.service.ResumeGateway(cfg.ID)
	}
	statusError := h.syncManager(c.Request.Context(), ws.ID)
	h.respondGateway(c, statusError)
}

// DisableGateway turns the gateway off. Disabling is inert (spec): the
// configuration, bindings, links, and sessions survive; the manager stops
// the adapter.
func (h *gatewayHandlers) DisableGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	if err := h.gateways.SetGatewayEnabled(c.Request.Context(), ws.ID, domain.GatewayPlatformTelegram, false); err != nil {
		RespondError(c, err)
		return
	}
	statusError := h.syncManager(c.Request.Context(), ws.ID)
	h.respondGateway(c, statusError)
}

// TestGateway verifies the STORED token against the platform API and
// persists nothing (the web pane's "Test" button; the wire shape is
// {ok, bot_username?, error?}).
func (h *gatewayHandlers) TestGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	cfg, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, domain.GatewayPlatformTelegram)
	if err != nil {
		RespondError(c, err)
		return
	}
	if cfg == nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "no bot token is configured for this gateway"})
		return
	}
	token, err := secrets.Decrypt(h.encKey, []byte(ws.ID), cfg.BotTokenCiphertext)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "the stored bot token could not be decrypted — re-enter it"})
		return
	}
	username, verr := h.verifier.VerifyBotToken(c.Request.Context(), string(token))
	if verr != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": verr.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "bot_username": username})
}

// ---------------------------------------------------------------------------
// Bindings (gateways.write)
// ---------------------------------------------------------------------------

// ListBindings returns the workspace's group bindings, oldest first.
func (h *gatewayHandlers) ListBindings(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	rows, err := h.bindings.ListChatBindings(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	views := make([]gatewayBindingView, 0, len(rows))
	for i := range rows {
		views = append(views, gatewayBindingViewOf(&rows[i], ""))
	}
	c.JSON(http.StatusOK, gin.H{"bindings": views})
}

// CreateBinding binds one platform chat to one agent. Duplicate chats are a
// 409 (one agent per group, globally unique per platform chat — design D2);
// field-level problems are 422s naming the offending field.
func (h *gatewayHandlers) CreateBinding(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	var payload gatewayBindingPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	var details []ErrorDetail
	agentID := trimOrEmpty(payload.AgentID)
	if agentID == "" {
		details = append(details, gatewayFieldError("agent_id", "agent_id is required"))
	}
	chatID := trimOrEmpty(payload.PlatformChatID)
	if chatID == "" {
		details = append(details, gatewayFieldError("platform_chat_id", "platform_chat_id is required"))
	}
	if len(details) > 0 {
		AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, "invalid gateway binding", details...)
		return
	}

	agent, err := h.agents.ByID(c.Request.Context(), ws.ID, agentID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, "invalid gateway binding",
				gatewayFieldError("agent_id", "no agent with this id exists in the workspace"))
			return
		}
		RespondError(c, err)
		return
	}

	binding := &domain.ChatBinding{
		WorkspaceID:    ws.ID,
		Platform:       domain.GatewayPlatformTelegram,
		PlatformChatID: chatID,
		AgentID:        agent.ID,
		CreatedBy:      &user.ID,
	}
	if err := h.bindings.CreateChatBinding(c.Request.Context(), ws.ID, binding); err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"binding": gatewayBindingViewOf(binding, trimOrEmpty(payload.ChatTitle))})
}

// DeleteBinding removes a binding by id.
func (h *gatewayHandlers) DeleteBinding(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	if err := h.bindings.DeleteChatBinding(c.Request.Context(), ws.ID, c.Param("id")); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// ---------------------------------------------------------------------------
// Pairing tokens (member-level)
// ---------------------------------------------------------------------------

// CreatePairingToken mints the calling member's one-time pairing token
// (single-use, one hour — design D6).
func (h *gatewayHandlers) CreatePairingToken(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	token, err := h.pairing.MintToken(c.Request.Context(), ws.ID, user.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"token": gin.H{
		"token":      token.Token,
		"expires_at": token.ExpiresAt,
	}})
}

// RevokePairingToken cancels a minted-but-unused token.
func (h *gatewayHandlers) RevokePairingToken(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	if err := h.links.RevokePairingToken(c.Request.Context(), ws.ID, c.Param("token")); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// ---------------------------------------------------------------------------
// Identity links
// ---------------------------------------------------------------------------

// GetMyLink answers {link: null} when the calling member has no Telegram
// identity paired, never 404.
func (h *gatewayHandlers) GetMyLink(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	link := h.telegramLinkForMember(c, ws.ID, user.ID)
	if link == nil {
		c.JSON(http.StatusOK, gin.H{"link": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"link": h.linkView(c.Request.Context(), link)})
}

// UnpairMyLink removes the calling member's Telegram identity link.
func (h *gatewayHandlers) UnpairMyLink(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	link := h.telegramLinkForMember(c, ws.ID, user.ID)
	if link == nil {
		RespondError(c, domain.ErrNotFound)
		return
	}
	if err := h.links.DeleteUserLink(c.Request.Context(), ws.ID, link.Platform, link.PlatformUserID); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// UnpairMemberLink is the admin per-member unpair (spec: "per-member
// unpairing" under the admin API).
func (h *gatewayHandlers) UnpairMemberLink(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	memberID := c.Param("uid")
	link := h.telegramLinkForMember(c, ws.ID, memberID)
	if link == nil {
		RespondError(c, domain.ErrNotFound)
		return
	}
	if err := h.links.DeleteUserLink(c.Request.Context(), ws.ID, link.Platform, link.PlatformUserID); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// telegramLinkForMember resolves the member's Telegram link, or nil.
func (h *gatewayHandlers) telegramLinkForMember(c *gin.Context, workspaceID, userID string) *domain.UserLink {
	links, err := h.links.ListUserLinksForMember(c.Request.Context(), workspaceID, userID)
	if err != nil {
		return nil
	}
	for i := range links {
		if links[i].Platform == domain.GatewayPlatformTelegram {
			return &links[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Public webhook ingress (task 6.2)
// ---------------------------------------------------------------------------

// WebhookUpdate is the PUBLIC Telegram update ingress. Authentication is the
// derived per-workspace secret carried in X-Telegram-Bot-Api-Secret-Token —
// a request without the configured secret is rejected unauthenticated and
// never processed (spec: "Webhook secret enforced"). :ws is the workspace ID
// (stable, unlike slugs — webhook URLs must not rot).
func (h *gatewayHandlers) WebhookUpdate(c *gin.Context) {
	workspaceID := c.Param("ws")
	expected := GatewayWebhookSecret(h.encKey, workspaceID, domain.GatewayPlatformTelegram)
	if subtle.ConstantTimeCompare([]byte(c.GetHeader(telegramWebhookSecretHeader)), []byte(expected)) != 1 {
		AbortUnauthenticated(c, "webhook secret mismatch")
		return
	}

	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, webhookMaxBodyBytes))
	if err != nil {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "unreadable webhook body")
		return
	}

	// The lifecycle manager routes the raw payload to the workspace's running
	// webhook receiver; unknown/stopped gateways are 404, malformed payloads
	// are dropped inside the receiver (zero uncaught parse errors).
	if err := h.manager.HandleWebhook(c.Request.Context(), workspaceID, domain.GatewayPlatformTelegram, raw); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			AbortNotFound(c, "no running gateway for this workspace")
			return
		}
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---------------------------------------------------------------------------
// Small shared helpers
// ---------------------------------------------------------------------------

func trimOrEmpty(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func last4(s string) string {
	if len(s) < 4 {
		return s
	}
	return s[len(s)-4:]
}

func existingEnabled(cfg *domain.GatewayConfig) bool {
	return cfg != nil && cfg.Enabled
}

func existingTransport(cfg *domain.GatewayConfig, requested string) string {
	if requested != "" {
		return requested
	}
	if cfg != nil && cfg.Transport != "" {
		return cfg.Transport
	}
	return domain.GatewayTransportLongPolling
}

func existingDefaultAgent(cfg *domain.GatewayConfig) *string {
	if cfg == nil {
		return nil
	}
	return cfg.DefaultAgentID
}

func gatewayFieldError(field, message string) ErrorDetail {
	return ErrorDetail{Field: field, Message: message}
}

func respondGatewayValidationError(c *gin.Context, detail ErrorDetail) {
	AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, "invalid gateway configuration", detail)
}
