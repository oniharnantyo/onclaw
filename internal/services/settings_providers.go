package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/store"
)

// ProviderView is the read shape of one provider configuration, as bound to
// the frontend. It deliberately carries no API key: the raw secret never
// crosses the service boundary (design D2); callers see only whether a key
// is set and its non-secret hint.
type ProviderView struct {
	ID        string
	Type      string
	Name      string
	BaseURL   string
	Enabled   bool
	KeySet    bool
	KeyHint   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// VerifyResult is the outcome of one provider credential verification:
// whether the endpoint accepted the credential and, on failure, the
// endpoint's (truncated) reason. Verification failures land here, not in a
// Go error; the error is reserved for service and store failures.
type VerifyResult struct {
	OK     bool
	Detail string
}

// ProviderInput is the write shape of one provider configuration, as bound
// to the frontend. It carries only the fields a client may choose: the ID is
// addressed separately on update and the store assigns ID and timestamps, so
// time.Time fields never cross the bridge (an empty-string timestamp from a
// webview payload cannot unmarshal into time.Time).
type ProviderInput struct {
	Type    string
	Name    string
	BaseURL string
	APIKey  string
	Enabled bool
}

// keyHint derives the non-secret hint from a stored key: its last 4 runes,
// empty when no key is set.
func keyHint(key string) string {
	runes := []rune(key)
	if len(runes) == 0 {
		return ""
	}
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[len(runes)-4:])
}

// newProviderView projects a stored provider onto the read shape. The raw
// key is consumed here and must not leak into the result.
func newProviderView(provider store.Provider) ProviderView {
	return ProviderView{
		ID:        provider.ID,
		Type:      provider.Type,
		Name:      provider.Name,
		BaseURL:   provider.BaseURL,
		Enabled:   provider.Enabled,
		KeySet:    provider.APIKey != "",
		KeyHint:   keyHint(provider.APIKey),
		CreatedAt: provider.CreatedAt,
		UpdatedAt: provider.UpdatedAt,
	}
}

// checkProviderType rejects provider types the runtime has no connector for.
func checkProviderType(providerType string) error {
	if !agents.IsKnownProviderType(providerType) {
		return fmt.Errorf("unknown provider type %q (supported: %s)", providerType, strings.Join(agents.KnownProviderTypes, ", "))
	}
	return nil
}

// CreateProvider stores a new provider configuration; the store assigns its
// ID and timestamps. A duplicate provider name is an error; an unknown
// provider type is an error. A provider without an API key is accepted —
// compatible endpoints may legitimately need none — and verification flags
// it instead.
func (s *SettingsService) CreateProvider(ctx context.Context, input ProviderInput) (ProviderView, error) {
	if err := checkProviderType(input.Type); err != nil {
		return ProviderView{}, fmt.Errorf("create provider: %w", err)
	}
	provider := store.Provider{
		Type:    input.Type,
		Name:    input.Name,
		BaseURL: input.BaseURL,
		APIKey:  input.APIKey,
		Enabled: input.Enabled,
	}
	if err := s.store.Providers().Create(ctx, &provider); err != nil {
		return ProviderView{}, fmt.Errorf("create provider: %w", err)
	}
	if agents.IsCanonicalProviderType(provider.Type) {
		models := agents.CatalogProviderModels(provider.ID, provider.Type)
		if len(models) > 0 {
			if _, err := s.store.ProviderModels().UpsertMissing(ctx, models); err != nil {
				return ProviderView{}, fmt.Errorf("seed provider models: %w", err)
			}
		}
	}
	return newProviderView(provider), nil
}

// SeedCanonicalModels sweeps all stored providers and seeds missing catalog
// models for every provider of a canonical type (anthropic, openai, gemini).
// Live-listed providers are left untouched. Existing rows and user toggles are
// preserved (design D3).
func (s *SettingsService) SeedCanonicalModels(ctx context.Context) error {
	providers, err := s.store.Providers().List(ctx)
	if err != nil {
		return fmt.Errorf("list providers for model seed: %w", err)
	}
	for _, p := range providers {
		if !agents.IsCanonicalProviderType(p.Type) {
			continue
		}
		models := agents.CatalogProviderModels(p.ID, p.Type)
		if len(models) == 0 {
			continue
		}
		if _, err := s.store.ProviderModels().UpsertMissing(ctx, models); err != nil {
			return fmt.Errorf("seed models for provider %s: %w", p.ID, err)
		}
	}
	return nil
}

// Provider returns the provider with the given ID; store.ErrNotFound when
// absent. The response is the secret-free ProviderView.
func (s *SettingsService) Provider(ctx context.Context, id string) (ProviderView, error) {
	provider, err := s.store.Providers().ByID(ctx, id)
	if err != nil {
		return ProviderView{}, fmt.Errorf("get provider: %w", err)
	}
	return newProviderView(*provider), nil
}

// Providers returns all providers, oldest first, projected onto the
// secret-free ProviderView. The result is never nil.
func (s *SettingsService) Providers(ctx context.Context) ([]ProviderView, error) {
	providers, err := s.store.Providers().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	views := make([]ProviderView, 0, len(providers))
	for _, provider := range providers {
		views = append(views, newProviderView(provider))
	}
	return views, nil
}

// UpdateProvider rewrites an existing provider configuration, addressed by
// id; store.ErrNotFound when absent. An unknown provider type is an error.
// A blank APIKey preserves the stored key — the edit form resubmits only
// what changed — while a non-blank key replaces it.
func (s *SettingsService) UpdateProvider(ctx context.Context, id string, input ProviderInput) (ProviderView, error) {
	stored, err := s.store.Providers().ByID(ctx, id)
	if err != nil {
		return ProviderView{}, fmt.Errorf("update provider: %w", err)
	}
	if err := checkProviderType(input.Type); err != nil {
		return ProviderView{}, fmt.Errorf("update provider: %w", err)
	}
	apiKey := input.APIKey
	if apiKey == "" {
		apiKey = stored.APIKey
	}
	provider := store.Provider{
		ID:      id,
		Type:    input.Type,
		Name:    input.Name,
		BaseURL: input.BaseURL,
		APIKey:  apiKey,
		Enabled: input.Enabled,
		// The store derives these; carrying the stored CreatedAt keeps the
		// returned view truthful on stores whose Update does not write it back.
		CreatedAt: stored.CreatedAt,
	}
	if err := s.store.Providers().Update(ctx, &provider); err != nil {
		return ProviderView{}, fmt.Errorf("update provider: %w", err)
	}
	return newProviderView(provider), nil
}

// DeleteProvider removes the provider; store.ErrNotFound when absent.
func (s *SettingsService) DeleteProvider(ctx context.Context, id string) error {
	if err := s.store.Providers().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	return nil
}

// VerifyProviderDraft tests unsaved form values against the provider
// endpoint without persisting anything. When providerID is set and the input
// carries a blank key — an edit that leaves the key field untouched — the
// stored key is verified instead (design D2). Verification failures come
// back in the result; the error is reserved for store failures and unknown
// provider types.
func (s *SettingsService) VerifyProviderDraft(ctx context.Context, input ProviderInput, providerID string) (VerifyResult, error) {
	apiKey := input.APIKey
	if apiKey == "" && providerID != "" {
		stored, err := s.store.Providers().ByID(ctx, providerID)
		if err != nil {
			return VerifyResult{}, fmt.Errorf("verify provider draft: %w", err)
		}
		apiKey = stored.APIKey
	}

	ok, detail, err := agents.VerifyCredentials(ctx, input.Type, input.BaseURL, apiKey)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("verify provider draft: %w", err)
	}
	return VerifyResult{OK: ok, Detail: detail}, nil
}

// VerifyProvider tests a saved provider's stored credential against its
// endpoint without modifying it. Verification failures come back in the
// result; the error is reserved for store failures and unknown provider
// types.
func (s *SettingsService) VerifyProvider(ctx context.Context, id string) (VerifyResult, error) {
	provider, err := s.store.Providers().ByID(ctx, id)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("verify provider: %w", err)
	}

	ok, detail, err := agents.VerifyCredentials(ctx, provider.Type, provider.BaseURL, provider.APIKey)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("verify provider: %w", err)
	}
	return VerifyResult{OK: ok, Detail: detail}, nil
}

// ModelCapability is the read shape of one selectable model: its id plus the
// capability data the agent config modal and the composer pickers
// render (design D2: the same DTO feeds both surfaces).
type ModelCapability struct {
	ID string
	// ContextWindow is the model's context limit in tokens; the default
	// window for models outside the static catalog.
	ContextWindow int
	// Efforts are the model's reasoning-effort options in declared order:
	// for a stored row, the row's declared values in declared order, or
	// exactly ["high"] when the row declares none (design D3).
	Efforts []string
	// ImageInput, PDFInput, and VideoInput report the model's multimodal
	// input support.
	ImageInput bool
	PDFInput   bool
	VideoInput bool
}

// ProviderModelInput carries the write shape for a provider model row.
// It exposes no secrets.
type ProviderModelInput struct {
	ModelID       string
	ContextWindow int
	ImageInput    bool
	PDFInput      bool
	VideoInput    bool
	// Efforts are the declared reasoning efforts in order; free-form
	// values; empty means undeclared (design D7).
	Efforts []string
	Enabled bool
}

// ProviderModelView is the read shape of one provider model row.
// It deliberately carries no API key or secrets (design D2).
type ProviderModelView struct {
	ID            int64
	ProviderID    string
	ModelID       string
	ContextWindow int
	ImageInput    bool
	PDFInput      bool
	VideoInput    bool
	// Efforts is the row's declared reasoning-effort set in declared
	// order; empty means undeclared (design D7).
	Efforts   []string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

func newProviderModelView(m *store.ProviderModel) *ProviderModelView {
	if m == nil {
		return nil
	}
	return &ProviderModelView{
		ID:            m.ID,
		ProviderID:    m.ProviderID,
		ModelID:       m.ModelID,
		ContextWindow: m.ContextWindow,
		ImageInput:    m.ImageInput,
		PDFInput:      m.PDFInput,
		VideoInput:    m.VideoInput,
		// Copy so a view never aliases a store row's slice; nil stays nil.
		Efforts:   append([]string(nil), m.Efforts...),
		Enabled:   m.Enabled,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

// normalizeEfforts trims each declared effort, drops empties, and dedupes
// preserving the first occurrence, keeping declared order (design D2).
// Surviving values are stored verbatim — no case-folding — because they ride
// the provider's native request fields. Returns nil when nothing survives.
func normalizeEfforts(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, e := range in {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ProviderModels returns the provider's enabled stored models with their
// capability data (design D6). No live network fetch is performed; the store
// is the single source of truth. Efforts resolve stored-first (design D3): a
// row's declared set when non-empty, otherwise exactly ["high"] — the one
// tier every connector family understands — with no catalog or heuristic
// fallback for stored rows.
func (s *SettingsService) ProviderModels(ctx context.Context, id string) ([]ModelCapability, error) {
	if _, err := s.store.Providers().ByID(ctx, id); err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	rows, err := s.store.ProviderModels().ByProvider(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list provider models: %w", err)
	}
	out := make([]ModelCapability, 0, len(rows))
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		cw := row.ContextWindow
		if cw <= 0 {
			cw = agents.DefaultContextWindow
		}
		efforts := row.Efforts
		if len(efforts) == 0 {
			efforts = []string{agents.EffortHigh}
		}
		out = append(out, ModelCapability{
			ID:            row.ModelID,
			ContextWindow: cw,
			Efforts:       efforts,
			ImageInput:    row.ImageInput,
			PDFInput:      row.PDFInput,
			VideoInput:    row.VideoInput,
		})
	}
	return out, nil
}

// CreateModel adds a new model to a provider's model list. The ID and timestamps
// are store-assigned. A duplicate (provider_id, model_id) returns an error
// wrapping store.ErrDuplicate.
func (s *SettingsService) CreateModel(ctx context.Context, providerID string, input ProviderModelInput) (*ProviderModelView, error) {
	if strings.TrimSpace(providerID) == "" {
		return nil, fmt.Errorf("provider id is required")
	}
	if _, err := s.store.Providers().ByID(ctx, providerID); err != nil {
		return nil, fmt.Errorf("get provider %q: %w", providerID, err)
	}
	modelID := strings.TrimSpace(input.ModelID)
	if modelID == "" {
		return nil, fmt.Errorf("model id is required")
	}
	cw := input.ContextWindow
	if cw <= 0 {
		cw = agents.DefaultContextWindow
	}
	m := &store.ProviderModel{
		ProviderID:    providerID,
		ModelID:       modelID,
		ContextWindow: cw,
		ImageInput:    input.ImageInput,
		PDFInput:      input.PDFInput,
		VideoInput:    input.VideoInput,
		Efforts:       normalizeEfforts(input.Efforts),
		Enabled:       input.Enabled,
	}
	if err := s.store.ProviderModels().Create(ctx, m); err != nil {
		return nil, fmt.Errorf("create model %q for provider %q: %w", modelID, providerID, err)
	}
	return newProviderModelView(m), nil
}

// UpdateModel rewrites an existing model row addressed by id; store.ErrNotFound
// when absent. A duplicate (provider_id, model_id) returns store.ErrDuplicate.
func (s *SettingsService) UpdateModel(ctx context.Context, id int64, input ProviderModelInput) (*ProviderModelView, error) {
	stored, err := s.store.ProviderModels().ByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get model %d: %w", id, err)
	}
	modelID := strings.TrimSpace(input.ModelID)
	if modelID == "" {
		return nil, fmt.Errorf("model id is required")
	}
	cw := input.ContextWindow
	if cw <= 0 {
		cw = agents.DefaultContextWindow
	}
	stored.ModelID = modelID
	stored.ContextWindow = cw
	stored.ImageInput = input.ImageInput
	stored.PDFInput = input.PDFInput
	stored.VideoInput = input.VideoInput
	stored.Efforts = normalizeEfforts(input.Efforts)
	stored.Enabled = input.Enabled
	if err := s.store.ProviderModels().Update(ctx, stored); err != nil {
		return nil, fmt.Errorf("update model %d: %w", id, err)
	}
	return newProviderModelView(stored), nil
}

// DeleteModel removes a model row by ID; store.ErrNotFound when absent.
func (s *SettingsService) DeleteModel(ctx context.Context, id int64) error {
	if err := s.store.ProviderModels().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete model %d: %w", id, err)
	}
	return nil
}

// ToggleModel updates the enabled state of a model row by ID.
func (s *SettingsService) ToggleModel(ctx context.Context, id int64, enabled bool) (*ProviderModelView, error) {
	stored, err := s.store.ProviderModels().ByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get model %d: %w", id, err)
	}
	stored.Enabled = enabled
	if err := s.store.ProviderModels().Update(ctx, stored); err != nil {
		return nil, fmt.Errorf("toggle model %d: %w", id, err)
	}
	return newProviderModelView(stored), nil
}

// ListProviderModels returns all stored models (both enabled and disabled) for
// a provider, ordered by ID ascending.
func (s *SettingsService) ListProviderModels(ctx context.Context, providerID string) ([]*ProviderModelView, error) {
	if _, err := s.store.Providers().ByID(ctx, providerID); err != nil {
		return nil, fmt.Errorf("get provider %q: %w", providerID, err)
	}
	rows, err := s.store.ProviderModels().ByProvider(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("list provider models for %q: %w", providerID, err)
	}
	out := make([]*ProviderModelView, 0, len(rows))
	for _, r := range rows {
		out = append(out, newProviderModelView(r))
	}
	return out, nil
}
