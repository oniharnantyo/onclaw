package handlers

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ProviderResponse represents the safe, external representation of a provider configuration.
// KeyCiphertext is NEVER exposed across the HTTP boundary.
type ProviderResponse struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
	Type        string    `json:"type"`
	Name        string    `json:"name"`
	BaseURL     string    `json:"base_url,omitempty"`
	KeySet      bool      `json:"key_set"`
	KeyHint     string    `json:"key_hint,omitempty"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toProviderResponse(p *domain.ProviderConfig) ProviderResponse {
	return ProviderResponse{
		ID:          p.ID,
		WorkspaceID: p.WorkspaceID,
		Type:        p.Type,
		Name:        p.Name,
		BaseURL:     p.BaseURL,
		KeySet:      p.HasKey(),
		KeyHint:     p.KeyHint,
		Enabled:     p.Enabled,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// providerHandlers handles workspace provider configuration endpoints.
type providerHandlers struct {
	store         store.Store
	encryptionKey []byte
	registry      *providers.Registry
}

// NewProviderHandlers creates a new providerHandlers instance with injected dependencies.
func NewProviderHandlers(st store.Store, encryptionKey []byte, registry *providers.Registry) *providerHandlers {
	if registry == nil {
		registry = providers.NewRegistry()
	}
	return &providerHandlers{
		store:         st,
		encryptionKey: encryptionKey,
		registry:      registry,
	}
}

// ListProviders lists all provider configurations for the current workspace.
func (h *providerHandlers) ListProviders(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	configs, err := h.store.Providers().ListForWorkspace(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	items := make([]ProviderResponse, 0, len(configs))
	for i := range configs {
		items = append(items, toProviderResponse(&configs[i]))
	}

	RespondOK(c, gin.H{"providers": items})
}

// CreateProviderRequest holds parameters for creating a provider configuration.
type CreateProviderRequest struct {
	Type    string  `json:"type"`
	Name    string  `json:"name"`
	BaseURL *string `json:"base_url,omitempty"`
	Key     *string `json:"key,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

// CreateProvider creates a new provider configuration in the current workspace.
func (h *providerHandlers) CreateProvider(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req CreateProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	pType := strings.TrimSpace(req.Type)
	providerImpl, err := h.registry.Get(pType)
	if err != nil {
		RespondError(c, err)
		return
	}

	pName := strings.TrimSpace(req.Name)
	if pName == "" {
		RespondError(c, fmt.Errorf("%w: name is required", domain.ErrInvalid))
		return
	}

	var baseURL string
	if req.BaseURL != nil {
		baseURL = strings.TrimSpace(*req.BaseURL)
	}

	if providerImpl.RequiresBaseURL() && baseURL == "" {
		RespondError(c, fmt.Errorf("%w: base_url is required for provider type %q", domain.ErrInvalid, pType))
		return
	}

	if baseURL != "" {
		if err := validateBaseURL(baseURL); err != nil {
			RespondError(c, err)
			return
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	var keyCiphertext, keyHint string
	if req.Key != nil && strings.TrimSpace(*req.Key) != "" {
		trimmedKey := strings.TrimSpace(*req.Key)
		envelope, err := secrets.Encrypt(h.encryptionKey, []byte(ws.ID), []byte(trimmedKey))
		if err != nil {
			RespondError(c, err)
			return
		}
		keyCiphertext = envelope
		keyHint = domain.GenerateKeyHint(trimmedKey)
	}

	p := &domain.ProviderConfig{
		WorkspaceID:   ws.ID,
		Type:          pType,
		Name:          pName,
		BaseURL:       baseURL,
		KeyCiphertext: keyCiphertext,
		KeyHint:       keyHint,
		Enabled:       enabled,
	}

	if err := h.store.Providers().Create(c.Request.Context(), p); err != nil {
		RespondError(c, err)
		return
	}

	RespondCreated(c, gin.H{"provider": toProviderResponse(p)})
}

// PatchProviderRequest holds parameters for updating a provider configuration.
type PatchProviderRequest struct {
	Name    *string `json:"name,omitempty"`
	BaseURL *string `json:"base_url,omitempty"`
	Key     *string `json:"key,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

// PatchProvider updates an existing provider configuration.
func (h *providerHandlers) PatchProvider(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	var req PatchProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	existing, err := h.store.Providers().ByID(c.Request.Context(), ws.ID, id)
	if err != nil {
		RespondError(c, err)
		return
	}

	providerImpl, _ := h.registry.Get(existing.Type)

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: name cannot be empty", domain.ErrInvalid))
			return
		}
		existing.Name = trimmed
	}

	if req.BaseURL != nil {
		trimmed := strings.TrimSpace(*req.BaseURL)
		if providerImpl != nil && providerImpl.RequiresBaseURL() && trimmed == "" {
			RespondError(c, fmt.Errorf("%w: base_url is required for provider type %q", domain.ErrInvalid, existing.Type))
			return
		}
		if trimmed != "" {
			if err := validateBaseURL(trimmed); err != nil {
				RespondError(c, err)
				return
			}
		}
		existing.BaseURL = trimmed
	}

	if req.Key != nil {
		trimmed := strings.TrimSpace(*req.Key)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: key cannot be empty; unsetting keys is not supported", domain.ErrInvalid))
			return
		}
		envelope, err := secrets.Encrypt(h.encryptionKey, []byte(ws.ID), []byte(trimmed))
		if err != nil {
			RespondError(c, err)
			return
		}
		existing.KeyCiphertext = envelope
		existing.KeyHint = domain.GenerateKeyHint(trimmed)
	}

	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := h.store.Providers().Update(c.Request.Context(), existing); err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"provider": toProviderResponse(existing)})
}

// DeleteProvider deletes a provider configuration unconditionally.
func (h *providerHandlers) DeleteProvider(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	if err := h.store.Providers().Delete(c.Request.Context(), ws.ID, id); err != nil {
		RespondError(c, err)
		return
	}

	RespondNoContent(c)
}

// VerifyProvider tests connectivity and authentication with the provider using the stored encrypted key.
func (h *providerHandlers) VerifyProvider(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	existing, err := h.store.Providers().ByID(c.Request.Context(), ws.ID, id)
	if err != nil {
		RespondError(c, err)
		return
	}

	if !existing.HasKey() {
		RespondError(c, fmt.Errorf("%w: provider has no API key configured", domain.ErrInvalid))
		return
	}

	plaintextKeyBytes, err := secrets.Decrypt(h.encryptionKey, []byte(ws.ID), existing.KeyCiphertext)
	if err != nil {
		RespondError(c, domain.ErrUndecryptable)
		return
	}

	providerImpl, err := h.registry.Get(existing.Type)
	if err != nil {
		RespondError(c, err)
		return
	}

	verifyErr := providerImpl.Verify(c.Request.Context(), providers.Credential{
		Type:    existing.Type,
		BaseURL: existing.BaseURL,
		APIKey:  string(plaintextKeyBytes),
	})

	if verifyErr != nil {
		RespondOK(c, gin.H{"ok": false, "error": verifyErr.Error()})
		return
	}

	RespondOK(c, gin.H{"ok": true})
}

func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: invalid base_url %q", domain.ErrInvalid, raw)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: base_url scheme must be http or https", domain.ErrInvalid)
	}
	return nil
}
