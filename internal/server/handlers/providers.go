package handlers

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/services"
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
	// CatalogProvider echoes the stored catalog-mapping hint (empty = unset).
	CatalogProvider string `json:"catalog_provider,omitempty"`
	// SuggestedCatalogProvider is read-only: the host-based guess for
	// compatible gateway types (empty for canonically mapped types). The
	// stored CatalogProvider, once set, always wins over it.
	SuggestedCatalogProvider string `json:"suggested_catalog_provider,omitempty"`
}

func toProviderResponse(p *domain.ProviderConfig) ProviderResponse {
	suggested := ""
	if _, mapped := services.MapProviderType(p.Type); !mapped {
		suggested = providers.SuggestCatalogProvider(p.BaseURL)
	}
	return ProviderResponse{
		ID:                       p.ID,
		WorkspaceID:              p.WorkspaceID,
		Type:                     p.Type,
		Name:                     p.Name,
		BaseURL:                  p.BaseURL,
		KeySet:                   p.HasKey(),
		KeyHint:                  p.KeyHint,
		Enabled:                  p.Enabled,
		CreatedAt:                p.CreatedAt,
		UpdatedAt:                p.UpdatedAt,
		CatalogProvider:          p.CatalogProvider,
		SuggestedCatalogProvider: suggested,
	}
}

// validateCatalogHint rejects non-empty catalog-provider values that no known
// community-catalog provider carries. A catalog fetch failure accepts the
// value (fail open): a stale hint only ever resolves unknown downstream.
func validateCatalogHint(ctx context.Context, mc *services.ModelCatalog, hint string) error {
	trimmed := strings.TrimSpace(hint)
	if trimmed == "" || mc == nil {
		return nil
	}
	data, err := mc.FetchCatalog(ctx)
	if err != nil || data == nil {
		return nil
	}
	if _, ok := data.Providers[trimmed]; !ok {
		known := make([]string, 0, len(data.Providers))
		for id := range data.Providers {
			known = append(known, id)
		}
		sort.Strings(known)
		return fmt.Errorf("%w: unknown catalog_provider %q (not a community-catalog provider id)", domain.ErrInvalid, trimmed)
	}
	return nil
}

// providerHandlers handles workspace provider configuration endpoints.
type providerHandlers struct {
	providers     store.ProviderStore
	agents        store.AgentStore
	encryptionKey []byte
	registry      *providers.Registry
	modelCatalog  *services.ModelCatalog
}

// NewProviderHandlers creates a new providerHandlers instance with injected dependencies.
func NewProviderHandlers(providerConfigs store.ProviderStore, agentStore store.AgentStore, encryptionKey []byte, registry *providers.Registry, modelCatalog *services.ModelCatalog) *providerHandlers {
	if registry == nil {
		registry = providers.NewRegistry()
	}
	return &providerHandlers{
		providers:     providerConfigs,
		agents:        agentStore,
		encryptionKey: encryptionKey,
		registry:      registry,
		modelCatalog:  modelCatalog,
	}
}

// ListProviders lists all provider configurations for the current workspace.
func (h *providerHandlers) ListProviders(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	configs, err := h.providers.ListForWorkspace(c.Request.Context(), ws.ID)
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
	// CatalogProvider optionally maps a compatible gateway to a
	// community-catalog provider id; ignored by canonically mapped types.
	CatalogProvider *string `json:"catalog_provider,omitempty"`
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

	catalogProvider := ""
	if req.CatalogProvider != nil {
		catalogProvider = strings.TrimSpace(*req.CatalogProvider)
		if err := validateCatalogHint(c.Request.Context(), h.modelCatalog, catalogProvider); err != nil {
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
		WorkspaceID:     ws.ID,
		Type:            pType,
		Name:            pName,
		BaseURL:         baseURL,
		CatalogProvider: catalogProvider,
		KeyCiphertext:   keyCiphertext,
		KeyHint:         keyHint,
		Enabled:         enabled,
	}

	if err := h.providers.Create(c.Request.Context(), p); err != nil {
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
	// CatalogProvider replaces the stored hint when present (empty string
	// clears it back to host auto-detection).
	CatalogProvider *string `json:"catalog_provider,omitempty"`
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

	existing, err := h.providers.ByID(c.Request.Context(), ws.ID, id)
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

	if req.CatalogProvider != nil {
		trimmed := strings.TrimSpace(*req.CatalogProvider)
		if err := validateCatalogHint(c.Request.Context(), h.modelCatalog, trimmed); err != nil {
			RespondError(c, err)
			return
		}
		existing.CatalogProvider = trimmed
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

	if err := h.providers.Update(c.Request.Context(), existing); err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"provider": toProviderResponse(existing)})
}

// DeleteProvider deletes a provider configuration if it is not referenced by any agent.
func (h *providerHandlers) DeleteProvider(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	count, err := h.agents.CountByProvider(c.Request.Context(), ws.ID, id)
	if err != nil {
		RespondError(c, err)
		return
	}
	if count > 0 {
		RespondError(c, fmt.Errorf("%w: cannot delete provider in use by %d agent(s)", domain.ErrConflict, count))
		return
	}

	if err := h.providers.Delete(c.Request.Context(), ws.ID, id); err != nil {
		RespondError(c, err)
		return
	}

	RespondNoContent(c)
}

// GetProviderModels resolves models for a workspace provider config using stored decrypted credentials.
func (h *providerHandlers) GetProviderModels(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	existing, err := h.providers.ByID(c.Request.Context(), ws.ID, id)
	if err != nil {
		RespondError(c, err)
		return
	}

	var apiKey string
	if existing.HasKey() {
		plaintextKeyBytes, err := secrets.Decrypt(h.encryptionKey, []byte(ws.ID), existing.KeyCiphertext)
		if err == nil {
			apiKey = string(plaintextKeyBytes)
		}
	}

	cred := providers.Credential{
		Type:        existing.Type,
		BaseURL:     existing.BaseURL,
		APIKey:      apiKey,
		CatalogHint: existing.CatalogProvider,
	}

	if h.modelCatalog != nil {
		res, err := h.modelCatalog.ResolveModels(c.Request.Context(), cred)
		if err != nil {
			RespondError(c, err)
			return
		}
		RespondOK(c, res)
		return
	}

	// Fallback if model catalog is not initialized
	providerImpl, err := h.registry.Get(existing.Type)
	if err != nil {
		RespondError(c, err)
		return
	}

	models, err := providerImpl.ListModels(c.Request.Context(), cred)
	if err != nil {
		RespondOK(c, domain.ModelsResult{Source: domain.ModelSourceNone, Models: []domain.Model{}})
		return
	}

	resModels := make([]domain.Model, len(models))
	for i, m := range models {
		resModels[i] = domain.Model{
			ID:                  m.ID,
			Name:                m.Name,
			Efforts:             m.Efforts,
			SupportsTemperature: m.SupportsTemperature,
		}
	}
	RespondOK(c, domain.ModelsResult{Source: domain.ModelSourceLive, Models: resModels})
}

// ModelsPreviewRequest holds parameters for the unauthenticated/preview model resolution endpoint.
type ModelsPreviewRequest struct {
	Type    string `json:"type"`
	BaseURL string `json:"base_url,omitempty"`
	Key     string `json:"key,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	// CatalogProvider optionally maps a compatible gateway to a
	// community-catalog provider id for the preview resolution.
	CatalogProvider string `json:"catalog_provider,omitempty"`
}

// ModelsPreview resolves models using ephemeral credentials provided in the request body without storing them.
func (h *providerHandlers) ModelsPreview(c *gin.Context) {
	var req ModelsPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	pType := strings.TrimSpace(req.Type)
	if pType == "" {
		RespondError(c, fmt.Errorf("%w: provider type is required", domain.ErrInvalid))
		return
	}

	if _, err := h.registry.Get(pType); err != nil {
		RespondError(c, err)
		return
	}

	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(req.Key)
	}

	cred := providers.Credential{
		Type:        pType,
		BaseURL:     strings.TrimSpace(req.BaseURL),
		APIKey:      apiKey,
		CatalogHint: strings.TrimSpace(req.CatalogProvider),
	}

	if h.modelCatalog != nil {
		res, err := h.modelCatalog.ResolveModels(c.Request.Context(), cred)
		if err != nil {
			RespondError(c, err)
			return
		}
		RespondOK(c, res)
		return
	}

	providerImpl, err := h.registry.Get(pType)
	if err != nil {
		RespondError(c, err)
		return
	}

	models, err := providerImpl.ListModels(c.Request.Context(), cred)
	if err != nil {
		RespondOK(c, domain.ModelsResult{Source: domain.ModelSourceNone, Models: []domain.Model{}})
		return
	}

	resModels := make([]domain.Model, len(models))
	for i, m := range models {
		resModels[i] = domain.Model{
			ID:                  m.ID,
			Name:                m.Name,
			Efforts:             m.Efforts,
			SupportsTemperature: m.SupportsTemperature,
		}
	}
	RespondOK(c, domain.ModelsResult{Source: domain.ModelSourceLive, Models: resModels})
}

// VerifyProvider tests connectivity and authentication with the provider using the stored encrypted key.
func (h *providerHandlers) VerifyProvider(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	existing, err := h.providers.ByID(c.Request.Context(), ws.ID, id)
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

// VerifyDraftRequest carries the provider dialog's current (unsaved) form
// values for a connection probe. Key is write-only and never echoed back.
type VerifyDraftRequest struct {
	Type    string `json:"type"`
	BaseURL string `json:"base_url,omitempty"`
	Key     string `json:"key,omitempty"`
	// CatalogProvider optionally maps a compatible gateway to a
	// community-catalog provider id for the probe.
	CatalogProvider string `json:"catalog_provider,omitempty"`
	// ProviderID names an existing workspace config whose stored key is used
	// when Key is blank (the edit dialog's keep-stored-key case).
	ProviderID string `json:"provider_id,omitempty"`
}

// VerifyDraft tests unsaved provider form values without persisting anything
// (design D5, the dialog's "Verify connection"). The typed key is verified
// against the submitted type/base URL; a blank key falls back to the stored
// key of the named workspace provider config. Provider-side auth and
// connection failures report as 200 {ok: false, error} — never a 5xx.
func (h *providerHandlers) VerifyDraft(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req VerifyDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	pType := strings.TrimSpace(req.Type)
	if pType == "" {
		RespondError(c, fmt.Errorf("%w: provider type is required", domain.ErrInvalid))
		return
	}

	providerImpl, err := h.registry.Get(pType)
	if err != nil {
		RespondError(c, err)
		return
	}

	apiKey := strings.TrimSpace(req.Key)
	if apiKey == "" {
		id := strings.TrimSpace(req.ProviderID)
		if id == "" {
			RespondError(c, fmt.Errorf("%w: no credential to verify: provide a key or a saved provider id", domain.ErrInvalid))
			return
		}

		existing, err := h.providers.ByID(c.Request.Context(), ws.ID, id)
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
		apiKey = string(plaintextKeyBytes)
	}

	verifyErr := providerImpl.Verify(c.Request.Context(), providers.Credential{
		Type:        pType,
		BaseURL:     strings.TrimSpace(req.BaseURL),
		APIKey:      apiKey,
		CatalogHint: strings.TrimSpace(req.CatalogProvider),
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
