package handlers

// Gateway REST surface (integrate-telegram-gateway and multi-bot-gateways):
// workspace-scoped Telegram gateway accounts, chat bindings, pairing
// tokens, identity links, and the public webhook ingress.
//
// The bot token is a write-only secret: it is stored as a v1 secrets envelope
// AAD-bound to the workspace and every read carries only the last-4 token_hint.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/gateways"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// webhookSecretInfoKey seeds the HMAC that derives a gateway's webhook
// secret per gateway account ID.
const webhookSecretInfoKey = "onclaw:gateway-webhook:"

// GatewayWebhookSecret derives the ingress secret for one gateway account:
// HMAC-SHA256 under the instance encryption key, base64url-encoded.
func GatewayWebhookSecret(encKey []byte, gatewayID string) string {
	mac := hmac.New(sha256.New, encKey)
	mac.Write([]byte(webhookSecretInfoKey + gatewayID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// telegramWebhookSecretHeader is the header Telegram sets on every webhook delivery.
const telegramWebhookSecretHeader = "X-Telegram-Bot-Api-Secret-Token"

// webhookMaxBodyBytes bounds one webhook update body.
const webhookMaxBodyBytes = 1 << 20

// GatewayBotVerifier validates a bot token against the platform API and
// resolves the bot username.
type GatewayBotVerifier interface {
	VerifyBotToken(ctx context.Context, token string) (botUsername string, err error)
}

// gatewayHandlers carries the granular gateway stores plus the composition
// services the surface drives.
type gatewayHandlers struct {
	gateways  store.GatewayStore
	bindings  store.GatewayBindings
	links     store.GatewayLinks
	outbox    store.GatewayOutbox
	agents    store.AgentStore
	users     store.UserStore
	pairing   *gateways.PairingService
	manager   *gateways.Manager
	service   *gateways.Service
	verifier  GatewayBotVerifier
	encKey    []byte
	waRuntime WhatsAppRuntime
	waAuth    WhatsAppWebhookAuth
}

// NewGatewayHandlers builds the gateway handlers.
func NewGatewayHandlers(
	gateways store.GatewayStore,
	bindings store.GatewayBindings,
	links store.GatewayLinks,
	outbox store.GatewayOutbox,
	agents store.AgentStore,
	users store.UserStore,
	pairing *gateways.PairingService,
	manager *gateways.Manager,
	service *gateways.Service,
	verifier GatewayBotVerifier,
	encKey []byte,
	waRuntime WhatsAppRuntime,
	waAuth WhatsAppWebhookAuth,
) *gatewayHandlers {
	return &gatewayHandlers{
		gateways:  gateways,
		bindings:  bindings,
		links:     links,
		outbox:    outbox,
		agents:    agents,
		users:     users,
		pairing:   pairing,
		manager:   manager,
		service:   service,
		verifier:  verifier,
		encKey:    encKey,
		waRuntime: waRuntime,
		waAuth:    waAuth,
	}
}

// ---------------------------------------------------------------------------
// Wire shapes
// ---------------------------------------------------------------------------

type ApiGatewayConfig struct {
	ID          string `json:"id"`
	Platform    string `json:"platform"`
	Identity    string `json:"identity"`
	AgentID     string `json:"agent_id"`
	BotUsername string `json:"bot_username,omitempty"`
	TokenHint   string `json:"token_hint,omitempty"`
	Enabled     bool   `json:"enabled"`
	Transport   string `json:"transport"`
	WebhookURL  string `json:"webhook_url,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type CreateGatewayPayload struct {
	Token      string `json:"token"`
	AgentID    string `json:"agent_id"`
	Transport  string `json:"transport"`
	WebhookURL string `json:"webhook_url"`
}

type UpdateGatewayPayload struct {
	Token      string  `json:"token,omitempty"`
	AgentID    *string `json:"agent_id,omitempty"`
	Transport  *string `json:"transport,omitempty"`
	WebhookURL *string `json:"webhook_url,omitempty"`
}

type ApiGatewayBinding struct {
	ID             string  `json:"id"`
	GatewayID      string  `json:"gateway_id"`
	Platform       string  `json:"platform"`
	PlatformChatID string  `json:"platform_chat_id"`
	AgentID        string  `json:"agent_id"`
	CreatedBy      *string `json:"created_by,omitempty"`
	CreatedAt      string  `json:"created_at"`
}

type CreateGatewayBindingPayload struct {
	GatewayID      string `json:"gateway_id"`
	PlatformChatID string `json:"platform_chat_id"`
	AgentID        string `json:"agent_id"`
}

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
// Helpers
// ---------------------------------------------------------------------------

func (h *gatewayHandlers) tokenHint(ctx context.Context, workspaceID string, cfg *domain.GatewayConfig) string {
	if cfg == nil || cfg.BotTokenCiphertext == "" {
		return ""
	}
	plaintext, err := secrets.Decrypt(h.encKey, []byte(workspaceID), cfg.BotTokenCiphertext)
	if err != nil || len(plaintext) < 4 {
		return ""
	}
	return string(plaintext[len(plaintext)-4:])
}

func (h *gatewayHandlers) apiGatewayConfigOf(ctx context.Context, wsID string, cfg *domain.GatewayConfig) ApiGatewayConfig {
	hint := h.tokenHint(ctx, wsID, cfg)
	view := ApiGatewayConfig{
		ID:          cfg.ID,
		Platform:    cfg.Platform,
		Identity:    cfg.Identity,
		AgentID:     cfg.AgentID,
		BotUsername: cfg.BotUsername,
		TokenHint:   hint,
		Enabled:     cfg.Enabled,
		Transport:   cfg.Transport,
		WebhookURL:  cfg.WebhookURL,
	}
	if !cfg.CreatedAt.IsZero() {
		view.CreatedAt = cfg.CreatedAt.Format(time.RFC3339)
	}
	if !cfg.UpdatedAt.IsZero() {
		view.UpdatedAt = cfg.UpdatedAt.Format(time.RFC3339)
	}
	return view
}

// ---------------------------------------------------------------------------
// Telegram Endpoints (gateways.write)
// ---------------------------------------------------------------------------

// ListTelegramGateways lists all Telegram gateway accounts for the workspace.
func (h *gatewayHandlers) ListTelegramGateways(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	list, err := h.gateways.ListGatewaysByPlatform(c.Request.Context(), ws.ID, domain.GatewayPlatformTelegram)
	if err != nil {
		RespondError(c, err)
		return
	}
	views := make([]ApiGatewayConfig, 0, len(list))
	for i := range list {
		views = append(views, h.apiGatewayConfigOf(c.Request.Context(), ws.ID, &list[i]))
	}
	c.JSON(http.StatusOK, views)
}

// CreateTelegramGateway creates a new Telegram gateway account.
func (h *gatewayHandlers) CreateTelegramGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	var payload CreateGatewayPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	token := trimOrEmpty(payload.Token)
	if token == "" {
		respondGatewayValidationError(c, gatewayFieldError("token", "a bot token is required to connect the gateway"))
		return
	}

	agentID := trimOrEmpty(payload.AgentID)
	if agentID == "" {
		respondGatewayValidationError(c, gatewayFieldError("agent_id", "agent_id is required"))
		return
	}
	if _, err := h.agents.ByID(c.Request.Context(), ws.ID, agentID); err != nil {
		respondGatewayValidationError(c, gatewayFieldError("agent_id", "no agent with this id exists in the workspace"))
		return
	}

	transport := trimOrEmpty(payload.Transport)
	if transport == "" {
		transport = domain.GatewayTransportLongPolling
	}
	if transport != domain.GatewayTransportWebhook && transport != domain.GatewayTransportLongPolling {
		respondGatewayValidationError(c, gatewayFieldError("transport", "transport must be \"webhook\" or \"long_polling\""))
		return
	}

	webhookURL := trimOrEmpty(payload.WebhookURL)
	if transport == domain.GatewayTransportWebhook {
		if webhookURL == "" || !strings.HasPrefix(webhookURL, "https://") {
			respondGatewayValidationError(c, gatewayFieldError("webhook_url", "webhook transport requires an https:// webhook_url"))
			return
		}
	} else {
		webhookURL = ""
	}

	botUsername := ""
	username, verr := h.verifier.VerifyBotToken(c.Request.Context(), token)
	if verr == nil && username != "" {
		botUsername = username
	}

	envelope, encErr := secrets.Encrypt(h.encKey, []byte(ws.ID), []byte(token))
	if encErr != nil {
		RespondError(c, encErr)
		return
	}

	identity := botUsername
	if identity == "" {
		identity = "tg-" + last4(token)
	}

	cfg := &domain.GatewayConfig{
		ID:                 uuid.NewString(),
		WorkspaceID:        ws.ID,
		Platform:           domain.GatewayPlatformTelegram,
		Identity:           identity,
		AgentID:            agentID,
		BotTokenCiphertext: envelope,
		BotUsername:        botUsername,
		Enabled:            true,
		Transport:          transport,
		WebhookURL:         webhookURL,
	}

	if err := domain.ValidateGatewayConfig(cfg); err != nil {
		RespondError(c, err)
		return
	}

	if err := h.gateways.CreateGateway(c.Request.Context(), ws.ID, cfg); err != nil {
		RespondError(c, err)
		return
	}

	_ = h.manager.Sync(c.Request.Context(), ws.ID)

	c.JSON(http.StatusCreated, h.apiGatewayConfigOf(c.Request.Context(), ws.ID, cfg))
}

// GetTelegramGateway returns one Telegram gateway account by id.
func (h *gatewayHandlers) GetTelegramGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	cfg, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, gatewayID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if cfg == nil || cfg.Platform != domain.GatewayPlatformTelegram {
		AbortNotFound(c, "gateway not found")
		return
	}
	c.JSON(http.StatusOK, h.apiGatewayConfigOf(c.Request.Context(), ws.ID, cfg))
}

// UpdateTelegramGateway updates one Telegram gateway account.
func (h *gatewayHandlers) UpdateTelegramGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	existing, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, gatewayID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if existing == nil || existing.Platform != domain.GatewayPlatformTelegram {
		AbortNotFound(c, "gateway not found")
		return
	}

	var payload UpdateGatewayPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	token := trimOrEmpty(payload.Token)
	if token != "" {
		username, verr := h.verifier.VerifyBotToken(c.Request.Context(), token)
		if verr == nil && username != "" {
			existing.BotUsername = username
			existing.Identity = username
		}
		envelope, encErr := secrets.Encrypt(h.encKey, []byte(ws.ID), []byte(token))
		if encErr != nil {
			RespondError(c, encErr)
			return
		}
		existing.BotTokenCiphertext = envelope
	}

	if payload.AgentID != nil {
		agentID := trimOrEmpty(*payload.AgentID)
		if agentID == "" {
			respondGatewayValidationError(c, gatewayFieldError("agent_id", "agent_id is required"))
			return
		}
		if _, err := h.agents.ByID(c.Request.Context(), ws.ID, agentID); err != nil {
			respondGatewayValidationError(c, gatewayFieldError("agent_id", "no agent with this id exists in the workspace"))
			return
		}
		existing.AgentID = agentID
	}

	if payload.Transport != nil {
		transport := trimOrEmpty(*payload.Transport)
		if transport != domain.GatewayTransportWebhook && transport != domain.GatewayTransportLongPolling {
			respondGatewayValidationError(c, gatewayFieldError("transport", "transport must be \"webhook\" or \"long_polling\""))
			return
		}
		existing.Transport = transport
	}

	if payload.WebhookURL != nil {
		existing.WebhookURL = trimOrEmpty(*payload.WebhookURL)
	}

	if existing.Transport == domain.GatewayTransportLongPolling {
		existing.WebhookURL = ""
	} else if existing.Transport == domain.GatewayTransportWebhook {
		if existing.WebhookURL == "" || !strings.HasPrefix(existing.WebhookURL, "https://") {
			respondGatewayValidationError(c, gatewayFieldError("webhook_url", "webhook transport requires an https:// webhook_url"))
			return
		}
	}

	if err := domain.ValidateGatewayConfig(existing); err != nil {
		RespondError(c, err)
		return
	}

	if err := h.gateways.UpdateGateway(c.Request.Context(), ws.ID, existing); err != nil {
		RespondError(c, err)
		return
	}

	_ = h.manager.Sync(c.Request.Context(), ws.ID)

	c.JSON(http.StatusOK, h.apiGatewayConfigOf(c.Request.Context(), ws.ID, existing))
}

// EnableTelegramGateway enables a Telegram gateway account.
func (h *gatewayHandlers) EnableTelegramGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	cfg, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, gatewayID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if cfg == nil || cfg.Platform != domain.GatewayPlatformTelegram {
		AbortNotFound(c, "gateway not found")
		return
	}

	if err := h.gateways.SetGatewayEnabled(c.Request.Context(), ws.ID, gatewayID, true); err != nil {
		RespondError(c, err)
		return
	}
	h.service.ResumeGateway(gatewayID)
	_ = h.manager.Sync(c.Request.Context(), ws.ID)
	c.JSON(http.StatusOK, gin.H{})
}

// DisableTelegramGateway disables a Telegram gateway account.
func (h *gatewayHandlers) DisableTelegramGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	cfg, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, gatewayID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if cfg == nil || cfg.Platform != domain.GatewayPlatformTelegram {
		AbortNotFound(c, "gateway not found")
		return
	}

	if err := h.gateways.SetGatewayEnabled(c.Request.Context(), ws.ID, gatewayID, false); err != nil {
		RespondError(c, err)
		return
	}
	_ = h.manager.Sync(c.Request.Context(), ws.ID)
	c.JSON(http.StatusOK, gin.H{})
}

// TestTelegramGateway tests the stored bot token against the platform API.
func (h *gatewayHandlers) TestTelegramGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	cfg, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, gatewayID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if cfg == nil || cfg.Platform != domain.GatewayPlatformTelegram {
		AbortNotFound(c, "gateway not found")
		return
	}

	if cfg.BotTokenCiphertext == "" {
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

// DeleteTelegramGateway deletes one Telegram gateway account.
func (h *gatewayHandlers) DeleteTelegramGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	if err := h.gateways.DeleteGateway(c.Request.Context(), ws.ID, gatewayID); err != nil {
		RespondError(c, err)
		return
	}
	_ = h.manager.Sync(c.Request.Context(), ws.ID)
	RespondNoContent(c)
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
	views := make([]ApiGatewayBinding, 0, len(rows))
	for i := range rows {
		b := &rows[i]
		var createdBy *string
		if b.CreatedBy != nil {
			cb := *b.CreatedBy
			createdBy = &cb
		}
		views = append(views, ApiGatewayBinding{
			ID:             b.ID,
			GatewayID:      b.GatewayID,
			Platform:       b.Platform,
			PlatformChatID: b.PlatformChatID,
			AgentID:        b.AgentID,
			CreatedBy:      createdBy,
			CreatedAt:      b.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, views)
}

// CreateBinding binds one platform chat to one agent under an owning gateway account.
func (h *gatewayHandlers) CreateBinding(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	var payload CreateGatewayBindingPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	var details []ErrorDetail
	gatewayID := trimOrEmpty(payload.GatewayID)
	if gatewayID == "" {
		details = append(details, gatewayFieldError("gateway_id", "gateway_id is required"))
	}
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

	gw, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, gatewayID)
	if err != nil || gw == nil {
		AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, "invalid gateway binding",
			gatewayFieldError("gateway_id", "no gateway with this id exists in the workspace"))
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
		GatewayID:      gatewayID,
		Platform:       gw.Platform,
		PlatformChatID: chatID,
		AgentID:        agent.ID,
		CreatedBy:      &user.ID,
	}
	if err := h.bindings.CreateChatBinding(c.Request.Context(), ws.ID, binding); err != nil {
		RespondError(c, err)
		return
	}

	var createdBy *string
	if binding.CreatedBy != nil {
		cb := *binding.CreatedBy
		createdBy = &cb
	}
	c.JSON(http.StatusCreated, ApiGatewayBinding{
		ID:             binding.ID,
		GatewayID:      binding.GatewayID,
		Platform:       binding.Platform,
		PlatformChatID: binding.PlatformChatID,
		AgentID:        binding.AgentID,
		CreatedBy:      createdBy,
		CreatedAt:      binding.CreatedAt.Format(time.RFC3339),
	})
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

// CreatePairingToken mints the calling member's one-time pairing token.
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

// GetMyLink answers the calling member's Telegram identity link.
func (h *gatewayHandlers) GetMyLink(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	link := h.linkForMember(c, ws.ID, user.ID, domain.GatewayPlatformTelegram)
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
	link := h.linkForMember(c, ws.ID, user.ID, domain.GatewayPlatformTelegram)
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

// UnpairMemberLink is the admin per-member unpair.
func (h *gatewayHandlers) UnpairMemberLink(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	memberID := c.Param("uid")
	link := h.linkForMember(c, ws.ID, memberID, domain.GatewayPlatformTelegram)
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

func (h *gatewayHandlers) linkForMember(c *gin.Context, workspaceID, userID, platform string) *domain.UserLink {
	links, err := h.links.ListUserLinksForMember(c.Request.Context(), workspaceID, userID)
	if err != nil {
		return nil
	}
	for i := range links {
		if links[i].Platform == platform {
			return &links[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Public webhook ingress
// ---------------------------------------------------------------------------

// WebhookUpdate is the PUBLIC Telegram update ingress per gateway account ID.
func (h *gatewayHandlers) WebhookUpdate(c *gin.Context) {
	gatewayID := c.Param("gatewayId")
	expected := GatewayWebhookSecret(h.encKey, gatewayID)
	if subtle.ConstantTimeCompare([]byte(c.GetHeader(telegramWebhookSecretHeader)), []byte(expected)) != 1 {
		AbortUnauthenticated(c, "webhook secret mismatch")
		return
	}

	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, webhookMaxBodyBytes))
	if err != nil {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "unreadable webhook body")
		return
	}

	if err := h.manager.HandleWebhook(c.Request.Context(), gatewayID, raw); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			AbortNotFound(c, "no running gateway for this account")
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
	return strings.TrimSpace(s)
}

func last4(s string) string {
	if len(s) < 4 {
		return s
	}
	return s[len(s)-4:]
}

func gatewayFieldError(field, message string) ErrorDetail {
	return ErrorDetail{Field: field, Message: message}
}

func respondGatewayValidationError(c *gin.Context, detail ErrorDetail) {
	AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, "invalid gateway configuration", detail)
}
