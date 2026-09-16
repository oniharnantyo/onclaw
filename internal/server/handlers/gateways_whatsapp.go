package handlers

// WhatsApp gateway REST surface (add-whatsapp-gateway and multi-bot-gateways):
// workspace-scoped configuration with an explicit lane, the
// public Meta webhook ingress, and the multi-device pairing state machine.
//
// Secrets are never echoed: the cloud lane's four labeled
// write-only fields are packed server-side into ONE encrypted JSON envelope
// (AAD-bound to the workspace) and every read carries only has_credentials.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
)

// whatsappWebhookMaxBodyBytes bounds one webhook payload.
const whatsappWebhookMaxBodyBytes = 1 << 20

// whatsappWebhookSignatureHeader is the HMAC header Meta sets on every webhook delivery.
const whatsappWebhookSignatureHeader = "X-Hub-Signature-256"

// WhatsAppWebhookAuth authenticates the public cloud webhook ingress against
// the gateway's decrypted credential envelope.
type WhatsAppWebhookAuth interface {
	VerifyWebhookSignature(ctx context.Context, gatewayID string, raw []byte, signatureHeader string) bool
	VerifyChallengeToken(ctx context.Context, gatewayID, verifyToken string) bool
}

// WhatsAppPairingSnapshot is the handlers-side view of the multi-device pairing/link state.
type WhatsAppPairingSnapshot struct {
	State         string
	Connection    string
	QRDataURL     string
	QR            string
	PairCode      string
	LinkedNumber  string
	PairExpiresAt time.Time
	Error         string
}

// WhatsAppRuntime is the platform seam the WhatsApp configuration surface drives.
type WhatsAppRuntime interface {
	CloudHealth(ctx context.Context, gatewayID string) (detail string, err error)
	ProbeCredentials(ctx context.Context, credentialJSON string) (display string, err error)
	PairingState(ctx context.Context, gatewayID string) (WhatsAppPairingSnapshot, bool)
	StartPairing(ctx context.Context, gatewayID, phone string) error
	RegeneratePairing(ctx context.Context, gatewayID string) error
	LogoutDevice(ctx context.Context, gatewayID string) error
}

type whatsappCloudEnvelope struct {
	AccessToken   string `json:"access_token"`
	PhoneNumberID string `json:"phone_number_id"`
	AppSecret     string `json:"app_secret"`
	VerifyToken   string `json:"verify_token"`
}

type ApiWhatsAppGatewayConfig struct {
	ID             string `json:"id"`
	Platform       string `json:"platform"`
	Lane           string `json:"lane"`
	Identity       string `json:"identity"`
	AgentID        string `json:"agent_id"`
	Enabled        bool   `json:"enabled"`
	BotUsername    string `json:"bot_username,omitempty"`
	Transport      string `json:"transport"`
	WebhookURL     string `json:"webhook_url,omitempty"`
	HasCredentials bool   `json:"has_credentials"`
	StatusError    string `json:"status_error,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

func (h *gatewayHandlers) whatsappGatewayViewOf(cfg *domain.GatewayConfig, statusError string) ApiWhatsAppGatewayConfig {
	view := ApiWhatsAppGatewayConfig{
		ID:             cfg.ID,
		Platform:       cfg.Platform,
		Lane:           cfg.Lane,
		Identity:       cfg.Identity,
		AgentID:        cfg.AgentID,
		Enabled:        cfg.Enabled,
		BotUsername:    cfg.BotUsername,
		Transport:      cfg.Transport,
		WebhookURL:     cfg.WebhookURL,
		HasCredentials: cfg.BotTokenCiphertext != "",
		StatusError:    statusError,
	}
	if !cfg.CreatedAt.IsZero() {
		view.CreatedAt = cfg.CreatedAt.Format(time.RFC3339)
	}
	if !cfg.UpdatedAt.IsZero() {
		view.UpdatedAt = cfg.UpdatedAt.Format(time.RFC3339)
	}
	return view
}

type whatsappPairingView struct {
	Status       string     `json:"status"`
	Connection   string     `json:"connection,omitempty"`
	QRDataURL    string     `json:"qr_data_url,omitempty"`
	QR           string     `json:"qr,omitempty"`
	PairCode     string     `json:"pair_code,omitempty"`
	LinkedNumber string     `json:"linked_number,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Error        string     `json:"error,omitempty"`
}

func whatsappPairingViewOf(s WhatsAppPairingSnapshot) whatsappPairingView {
	view := whatsappPairingView{
		Status:       s.State,
		Connection:   s.Connection,
		QRDataURL:    s.QRDataURL,
		QR:           s.QR,
		PairCode:     s.PairCode,
		LinkedNumber: s.LinkedNumber,
		Error:        s.Error,
	}
	if !s.PairExpiresAt.IsZero() {
		expires := s.PairExpiresAt
		view.ExpiresAt = &expires
	}
	return view
}

type CreateWhatsAppGatewayPayload struct {
	Lane          string `json:"lane"`
	AgentID       string `json:"agent_id"`
	AccessToken   string `json:"access_token,omitempty"`
	PhoneNumberID string `json:"phone_number_id,omitempty"`
	AppSecret     string `json:"app_secret,omitempty"`
	VerifyToken   string `json:"verify_token,omitempty"`
	Transport     string `json:"transport,omitempty"`
	WebhookURL    string `json:"webhook_url,omitempty"`
}

type UpdateWhatsAppGatewayPayload struct {
	AgentID       *string `json:"agent_id,omitempty"`
	AccessToken   string  `json:"access_token,omitempty"`
	PhoneNumberID string  `json:"phone_number_id,omitempty"`
	AppSecret     string  `json:"app_secret,omitempty"`
	VerifyToken   string  `json:"verify_token,omitempty"`
	Transport     *string `json:"transport,omitempty"`
	WebhookURL    *string `json:"webhook_url,omitempty"`
}

func respondWhatsAppValidation(c *gin.Context, details ...ErrorDetail) {
	AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, "invalid whatsapp gateway configuration", details...)
}

func deriveWhatsAppWebhookURL(c *gin.Context, gatewayID string) string {
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + "/api/v1/webhooks/whatsapp/" + gatewayID
}

// ---------------------------------------------------------------------------
// WhatsApp Gateway Endpoints (gateways.write)
// ---------------------------------------------------------------------------

// ListWhatsAppGateways lists all WhatsApp gateway accounts for the workspace.
func (h *gatewayHandlers) ListWhatsAppGateways(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	list, err := h.gateways.ListGatewaysByPlatform(c.Request.Context(), ws.ID, domain.GatewayPlatformWhatsApp)
	if err != nil {
		RespondError(c, err)
		return
	}
	views := make([]ApiWhatsAppGatewayConfig, 0, len(list))
	for i := range list {
		views = append(views, h.whatsappGatewayViewOf(&list[i], ""))
	}
	c.JSON(http.StatusOK, views)
}

// CreateWhatsAppGateway creates a new WhatsApp gateway account.
func (h *gatewayHandlers) CreateWhatsAppGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	var payload CreateWhatsAppGatewayPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	lane := trimOrEmpty(payload.Lane)
	if lane != domain.GatewayLaneCloudAPI && lane != domain.GatewayLaneMultiDevice {
		respondWhatsAppValidation(c, gatewayFieldError("lane", "lane must be \""+domain.GatewayLaneCloudAPI+"\" or \""+domain.GatewayLaneMultiDevice+"\""))
		return
	}

	agentID := trimOrEmpty(payload.AgentID)
	if agentID == "" {
		respondWhatsAppValidation(c, gatewayFieldError("agent_id", "agent_id is required"))
		return
	}
	if _, err := h.agents.ByID(c.Request.Context(), ws.ID, agentID); err != nil {
		respondWhatsAppValidation(c, gatewayFieldError("agent_id", "no agent with this id exists in the workspace"))
		return
	}

	gatewayID := uuid.NewString()

	cfg := &domain.GatewayConfig{
		ID:          gatewayID,
		WorkspaceID: ws.ID,
		Platform:    domain.GatewayPlatformWhatsApp,
		Lane:        lane,
		AgentID:     agentID,
		Enabled:     true,
	}

	statusError := ""
	if lane == domain.GatewayLaneCloudAPI {
		if transport := trimOrEmpty(payload.Transport); transport != "" && transport != domain.GatewayTransportWebhook {
			respondWhatsAppValidation(c, gatewayFieldError("transport", "the "+domain.GatewayLaneCloudAPI+" lane requires \""+domain.GatewayTransportWebhook+"\" transport — the Cloud API is webhook-only"))
			return
		}

		env := whatsappCloudEnvelope{
			AccessToken:   trimOrEmpty(payload.AccessToken),
			PhoneNumberID: trimOrEmpty(payload.PhoneNumberID),
			AppSecret:     trimOrEmpty(payload.AppSecret),
			VerifyToken:   trimOrEmpty(payload.VerifyToken),
		}
		var missing []ErrorDetail
		if env.AccessToken == "" {
			missing = append(missing, gatewayFieldError("access_token", "access_token is required to connect the cloud gateway"))
		}
		if env.PhoneNumberID == "" {
			missing = append(missing, gatewayFieldError("phone_number_id", "phone_number_id is required to connect the cloud gateway"))
		}
		if env.AppSecret == "" {
			missing = append(missing, gatewayFieldError("app_secret", "app_secret is required to connect the cloud gateway"))
		}
		if env.VerifyToken == "" {
			missing = append(missing, gatewayFieldError("verify_token", "verify_token is required to connect the cloud gateway"))
		}
		if len(missing) > 0 {
			respondWhatsAppValidation(c, missing...)
			return
		}

		envelopeJSON, err := json.Marshal(env)
		if err != nil {
			RespondError(c, err)
			return
		}

		if display, perr := h.waRuntime.ProbeCredentials(c.Request.Context(), string(envelopeJSON)); perr != nil {
			statusError = perr.Error()
		} else if display != "" {
			cfg.BotUsername = display
			cfg.Identity = display
		}
		if cfg.Identity == "" {
			cfg.Identity = env.PhoneNumberID
		}

		ciphertext, encErr := secrets.Encrypt(h.encKey, []byte(ws.ID), envelopeJSON)
		if encErr != nil {
			RespondError(c, encErr)
			return
		}
		cfg.BotTokenCiphertext = ciphertext
		cfg.Transport = domain.GatewayTransportWebhook
		if payload.WebhookURL != "" {
			cfg.WebhookURL = trimOrEmpty(payload.WebhookURL)
		} else {
			cfg.WebhookURL = deriveWhatsAppWebhookURL(c, gatewayID)
		}
	} else {
		// Multi-device lane
		cfg.BotTokenCiphertext = ""
		cfg.Transport = domain.GatewayTransportLongPolling
		cfg.Identity = "wa-md-" + gatewayID[:8]
	}

	if err := domain.ValidateGatewayConfig(cfg); err != nil {
		respondWhatsAppValidation(c, ErrorDetail{Message: err.Error()})
		return
	}

	if err := h.gateways.CreateGateway(c.Request.Context(), ws.ID, cfg); err != nil {
		RespondError(c, err)
		return
	}

	if syncErr := h.syncManager(c.Request.Context(), ws.ID); syncErr != "" && statusError == "" {
		statusError = syncErr
	}

	c.JSON(http.StatusCreated, h.whatsappGatewayViewOf(cfg, statusError))
}

// GetWhatsAppGateway returns one WhatsApp gateway account by id.
func (h *gatewayHandlers) GetWhatsAppGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	cfg, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, gatewayID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if cfg == nil || cfg.Platform != domain.GatewayPlatformWhatsApp {
		AbortNotFound(c, "gateway not found")
		return
	}
	c.JSON(http.StatusOK, h.whatsappGatewayViewOf(cfg, ""))
}

// UpdateWhatsAppGateway updates a WhatsApp gateway account.
func (h *gatewayHandlers) UpdateWhatsAppGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	existing, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, gatewayID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if existing == nil || existing.Platform != domain.GatewayPlatformWhatsApp {
		AbortNotFound(c, "gateway not found")
		return
	}

	var payload UpdateWhatsAppGatewayPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	if payload.AgentID != nil {
		agentID := trimOrEmpty(*payload.AgentID)
		if agentID == "" {
			respondWhatsAppValidation(c, gatewayFieldError("agent_id", "agent_id is required"))
			return
		}
		if _, err := h.agents.ByID(c.Request.Context(), ws.ID, agentID); err != nil {
			respondWhatsAppValidation(c, gatewayFieldError("agent_id", "no agent with this id exists in the workspace"))
			return
		}
		existing.AgentID = agentID
	}

	statusError := ""
	if existing.Lane == domain.GatewayLaneCloudAPI {
		if payload.Transport != nil {
			transport := trimOrEmpty(*payload.Transport)
			if transport != "" && transport != domain.GatewayTransportWebhook {
				respondWhatsAppValidation(c, gatewayFieldError("transport", "the "+domain.GatewayLaneCloudAPI+" lane requires \""+domain.GatewayTransportWebhook+"\" transport — the Cloud API is webhook-only"))
				return
			}
		}

		env := h.decryptCloudEnvelope(c.Request.Context(), ws.ID, existing)
		merges := []struct {
			field string
			value string
			dest  *string
		}{
			{"access_token", payload.AccessToken, &env.AccessToken},
			{"phone_number_id", payload.PhoneNumberID, &env.PhoneNumberID},
			{"app_secret", payload.AppSecret, &env.AppSecret},
			{"verify_token", payload.VerifyToken, &env.VerifyToken},
		}
		var missing []ErrorDetail
		for _, m := range merges {
			if v := trimOrEmpty(m.value); v != "" {
				*m.dest = v
			}
			if *m.dest == "" {
				missing = append(missing, gatewayFieldError(m.field, m.field+" is required to connect the cloud gateway"))
			}
		}
		if len(missing) > 0 {
			respondWhatsAppValidation(c, missing...)
			return
		}

		envelopeJSON, err := json.Marshal(env)
		if err != nil {
			RespondError(c, err)
			return
		}

		if display, perr := h.waRuntime.ProbeCredentials(c.Request.Context(), string(envelopeJSON)); perr != nil {
			statusError = perr.Error()
		} else if display != "" {
			existing.BotUsername = display
			existing.Identity = display
		}

		ciphertext, encErr := secrets.Encrypt(h.encKey, []byte(ws.ID), envelopeJSON)
		if encErr != nil {
			RespondError(c, encErr)
			return
		}
		existing.BotTokenCiphertext = ciphertext
		if payload.WebhookURL != nil {
			existing.WebhookURL = trimOrEmpty(*payload.WebhookURL)
		}
	}

	if err := domain.ValidateGatewayConfig(existing); err != nil {
		respondWhatsAppValidation(c, ErrorDetail{Message: err.Error()})
		return
	}
	if err := h.gateways.UpdateGateway(c.Request.Context(), ws.ID, existing); err != nil {
		RespondError(c, err)
		return
	}

	if syncErr := h.syncManager(c.Request.Context(), ws.ID); syncErr != "" && statusError == "" {
		statusError = syncErr
	}
	c.JSON(http.StatusOK, h.whatsappGatewayViewOf(existing, statusError))
}

// EnableWhatsAppGateway enables a WhatsApp gateway account.
func (h *gatewayHandlers) EnableWhatsAppGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	ctx := c.Request.Context()
	cfg, err := h.gateways.GetGateway(ctx, ws.ID, gatewayID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if cfg == nil || cfg.Platform != domain.GatewayPlatformWhatsApp {
		AbortNotFound(c, "gateway not found")
		return
	}
	switch cfg.Lane {
	case domain.GatewayLaneCloudAPI:
		if cfg.BotTokenCiphertext == "" {
			respondWhatsAppValidation(c, gatewayFieldError("access_token", "save the cloud credentials before enabling the gateway"))
			return
		}
	case domain.GatewayLaneMultiDevice:
		snap, found := h.waRuntime.PairingState(ctx, gatewayID)
		if !found || (snap.State != "waiting" && snap.State != "connected") {
			respondWhatsAppValidation(c, gatewayFieldError("lane", "start pairing (or link a device) before enabling the multi-device gateway"))
			return
		}
	default:
		respondWhatsAppValidation(c, gatewayFieldError("lane", "the WhatsApp gateway lane is not configured"))
		return
	}

	if err := h.gateways.SetGatewayEnabled(ctx, ws.ID, gatewayID, true); err != nil {
		RespondError(c, err)
		return
	}
	h.service.ResumeGateway(gatewayID)
	statusError := h.syncManager(ctx, ws.ID)
	reloaded, _ := h.gateways.GetGateway(ctx, ws.ID, gatewayID)
	if reloaded == nil {
		reloaded = cfg
	}
	c.JSON(http.StatusOK, h.whatsappGatewayViewOf(reloaded, statusError))
}

// DisableWhatsAppGateway disables a WhatsApp gateway account.
func (h *gatewayHandlers) DisableWhatsAppGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	cfg, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, gatewayID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if cfg == nil || cfg.Platform != domain.GatewayPlatformWhatsApp {
		AbortNotFound(c, "gateway not found")
		return
	}
	if err := h.gateways.SetGatewayEnabled(c.Request.Context(), ws.ID, gatewayID, false); err != nil {
		RespondError(c, err)
		return
	}
	statusError := h.syncManager(c.Request.Context(), ws.ID)
	reloaded, _ := h.gateways.GetGateway(c.Request.Context(), ws.ID, gatewayID)
	if reloaded == nil {
		reloaded = cfg
	}
	c.JSON(http.StatusOK, h.whatsappGatewayViewOf(reloaded, statusError))
}

// DeleteWhatsAppGateway deletes a WhatsApp gateway account.
func (h *gatewayHandlers) DeleteWhatsAppGateway(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	if err := h.gateways.DeleteGateway(c.Request.Context(), ws.ID, gatewayID); err != nil {
		RespondError(c, err)
		return
	}
	_ = h.manager.Sync(c.Request.Context(), ws.ID)
	RespondNoContent(c)
}

// GetWhatsAppHealth checks health for a specific WhatsApp gateway account.
func (h *gatewayHandlers) GetWhatsAppHealth(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	ctx := c.Request.Context()
	cfg, err := h.gateways.GetGateway(ctx, ws.ID, gatewayID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if cfg == nil || cfg.Platform != domain.GatewayPlatformWhatsApp {
		c.JSON(http.StatusOK, gin.H{"status": "unconfigured"})
		return
	}
	dead, deadSuffix := h.deadOutboxSuffix(ctx, ws.ID, cfg.ID)
	switch cfg.Lane {
	case domain.GatewayLaneCloudAPI:
		detail, err := h.waRuntime.CloudHealth(ctx, cfg.ID)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"status": "error", "detail": err.Error(), "dead_outbox": dead})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "detail": detail + deadSuffix, "dead_outbox": dead})
	case domain.GatewayLaneMultiDevice:
		snap, found := h.waRuntime.PairingState(ctx, cfg.ID)
		if !found {
			c.JSON(http.StatusOK, gin.H{"status": "error", "detail": "the WhatsApp gateway is not running", "dead_outbox": dead})
			return
		}
		if snap.Connection == "connected" {
			detail := snap.LinkedNumber
			if detail == "" {
				detail = "device connected"
			}
			c.JSON(http.StatusOK, gin.H{"status": "ok", "detail": detail + deadSuffix, "dead_outbox": dead})
			return
		}
		detail := "device " + snap.Connection
		if snap.State == "logged_out" {
			detail = "no linked device"
		}
		c.JSON(http.StatusOK, gin.H{"status": "error", "detail": detail + deadSuffix, "dead_outbox": dead})
	default:
		c.JSON(http.StatusOK, gin.H{"status": "unconfigured"})
	}
}

func (h *gatewayHandlers) deadOutboxSuffix(ctx context.Context, workspaceID, gatewayID string) (int64, string) {
	n, err := h.outbox.CountDead(ctx, workspaceID, gatewayID)
	if err != nil {
		return 0, " · dead delivery count unavailable"
	}
	if n == 0 {
		return 0, ""
	}
	noun := "deliveries"
	if n == 1 {
		noun = "delivery"
	}
	return n, fmt.Sprintf(" · %d dead %s", n, noun)
}

func (h *gatewayHandlers) decryptCloudEnvelope(ctx context.Context, workspaceID string, existing *domain.GatewayConfig) whatsappCloudEnvelope {
	if existing == nil || existing.BotTokenCiphertext == "" {
		return whatsappCloudEnvelope{}
	}
	plaintext, err := secrets.Decrypt(h.encKey, []byte(workspaceID), existing.BotTokenCiphertext)
	if err != nil {
		return whatsappCloudEnvelope{}
	}
	var env whatsappCloudEnvelope
	if err := json.Unmarshal(plaintext, &env); err != nil {
		return whatsappCloudEnvelope{}
	}
	return env
}

func (h *gatewayHandlers) syncManager(ctx context.Context, workspaceID string) string {
	if err := h.manager.Sync(ctx, workspaceID); err != nil {
		return err.Error()
	}
	return ""
}

// -------------------------------------------------------------------------
// Multi-device pairing (gateways.write; md lane only)
// -------------------------------------------------------------------------

func (h *gatewayHandlers) requireMDPairing(c *gin.Context) *domain.GatewayConfig {
	ws := MustCurrentWorkspace(c)
	gatewayID := c.Param("gatewayId")
	cfg, err := h.gateways.GetGateway(c.Request.Context(), ws.ID, gatewayID)
	if err != nil {
		RespondError(c, err)
		return nil
	}
	if cfg == nil || cfg.Platform != domain.GatewayPlatformWhatsApp || cfg.Lane != domain.GatewayLaneMultiDevice {
		respondWhatsAppValidation(c, gatewayFieldError("lane", "pairing is only available on the "+domain.GatewayLaneMultiDevice+" lane"))
		return nil
	}
	return cfg
}

func (h *gatewayHandlers) StartWhatsAppPairing(c *gin.Context) {
	cfg := h.requireMDPairing(c)
	if cfg == nil {
		return
	}
	var payload struct {
		Phone string `json:"phone"`
	}
	_ = c.ShouldBindJSON(&payload)
	if err := h.waRuntime.StartPairing(c.Request.Context(), cfg.ID, trimOrEmpty(payload.Phone)); err != nil {
		AbortWithError(c, http.StatusConflict, CodeConflict, err.Error())
		return
	}
	h.respondWhatsAppPairing(c, cfg.ID)
}

func (h *gatewayHandlers) GetWhatsAppPairingStatus(c *gin.Context) {
	cfg := h.requireMDPairing(c)
	if cfg == nil {
		return
	}
	h.respondWhatsAppPairing(c, cfg.ID)
}

func (h *gatewayHandlers) RegenerateWhatsAppPairing(c *gin.Context) {
	cfg := h.requireMDPairing(c)
	if cfg == nil {
		return
	}
	if err := h.waRuntime.RegeneratePairing(c.Request.Context(), cfg.ID); err != nil {
		AbortWithError(c, http.StatusConflict, CodeConflict, err.Error())
		return
	}
	h.respondWhatsAppPairing(c, cfg.ID)
}

func (h *gatewayHandlers) LogoutWhatsAppDevice(c *gin.Context) {
	cfg := h.requireMDPairing(c)
	if cfg == nil {
		return
	}
	if err := h.waRuntime.LogoutDevice(c.Request.Context(), cfg.ID); err != nil {
		AbortWithError(c, http.StatusConflict, CodeConflict, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "logged_out"})
}

func (h *gatewayHandlers) respondWhatsAppPairing(c *gin.Context, gatewayID string) {
	snap, found := h.waRuntime.PairingState(c.Request.Context(), gatewayID)
	if !found {
		c.JSON(http.StatusOK, gin.H{"status": "not_started"})
		return
	}
	c.JSON(http.StatusOK, whatsappPairingViewOf(snap))
}

// -------------------------------------------------------------------------
// Member pairing identity
// -------------------------------------------------------------------------

func (h *gatewayHandlers) GetMyWhatsAppLink(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	link := h.linkForMember(c, ws.ID, user.ID, domain.GatewayPlatformWhatsApp)
	if link == nil {
		c.JSON(http.StatusOK, gin.H{"link": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"link": h.linkView(c.Request.Context(), link)})
}

func (h *gatewayHandlers) UnpairMyWhatsAppLink(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	link := h.linkForMember(c, ws.ID, user.ID, domain.GatewayPlatformWhatsApp)
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

// -------------------------------------------------------------------------
// Public webhook ingress
// -------------------------------------------------------------------------

func (h *gatewayHandlers) WhatsAppWebhookVerify(c *gin.Context) {
	gatewayID := c.Param("gatewayId")
	if c.Query("hub.mode") != "subscribe" ||
		!h.waAuth.VerifyChallengeToken(c.Request.Context(), gatewayID, c.Query("hub.verify_token")) {
		c.String(http.StatusForbidden, "forbidden")
		return
	}
	c.String(http.StatusOK, c.Query("hub.challenge"))
}

func (h *gatewayHandlers) WhatsAppWebhookUpdate(c *gin.Context) {
	gatewayID := c.Param("gatewayId")

	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, whatsappWebhookMaxBodyBytes))
	if err != nil {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "unreadable webhook body")
		return
	}

	if !h.waAuth.VerifyWebhookSignature(c.Request.Context(), gatewayID, raw, c.GetHeader(whatsappWebhookSignatureHeader)) {
		AbortUnauthenticated(c, "webhook signature mismatch")
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
