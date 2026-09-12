// Package services — catalog domain: model list resolution, community models.dev catalog caching, and effort/temperature metadata enrichment.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"golang.org/x/sync/singleflight"
)

// Default configuration constants.
const (
	DefaultCatalogURL = "https://models.dev/api.json"
	DefaultTTL        = 24 * time.Hour
	DefaultCacheDir   = ".onclaw/cache"
	CacheFileName     = "models.dev.json"
)

// MapProviderType maps an OnClaw provider type to the models.dev provider identifier.
// Returns the models.dev provider ID and true if mapped, or empty string and false if unmapped.
func MapProviderType(providerType string) (string, bool) {
	switch providerType {
	case providers.TypeOpenAI:
		return "openai", true
	case providers.TypeAnthropic:
		return "anthropic", true
	case providers.TypeGemini:
		return "google", true
	case providers.TypeOpenRouter:
		return "openrouter", true
	case providers.TypeOpenAICompatible, providers.TypeAnthropicCompatible:
		return "", false
	default:
		return "", false
	}
}

// ReasoningOption represents a reasoning option block in models.dev.
type ReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values,omitempty"`
}

// CatalogLimit represents limit metadata in models.dev.
type CatalogLimit struct {
	Context *int `json:"context,omitempty"`
	Output  *int `json:"output,omitempty"`
}

// CatalogModalities represents the input/output modality lists in models.dev.
type CatalogModalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

// CatalogModel represents a model entry parsed from models.dev.
type CatalogModel struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Temperature      *bool              `json:"temperature,omitempty"`
	ReasoningOptions []ReasoningOption  `json:"reasoning_options,omitempty"`
	Limit            *CatalogLimit      `json:"limit,omitempty"`
	Modalities       *CatalogModalities `json:"modalities,omitempty"`
	Attachment       *bool              `json:"attachment,omitempty"`
	Reasoning        *bool              `json:"reasoning,omitempty"`
	ToolCall         *bool              `json:"tool_call,omitempty"`
}

// ContextLimit returns the context limit from limit if present.
func (m *CatalogModel) ContextLimit() *int {
	if m == nil || m.Limit == nil {
		return nil
	}
	return m.Limit.Context
}

// EffortValues returns the effort values list from reasoning_options if present.
func (m *CatalogModel) EffortValues() []string {
	if m == nil {
		return nil
	}
	for _, opt := range m.ReasoningOptions {
		if strings.EqualFold(opt.Type, "effort") && len(opt.Values) > 0 {
			res := make([]string, len(opt.Values))
			copy(res, opt.Values)
			return res
		}
	}
	return nil
}

// UnmarshalJSON implements custom defensive unmarshaling for CatalogModel.
func (m *CatalogModel) UnmarshalJSON(data []byte) error {
	type rawModel struct {
		ID               string          `json:"id"`
		Name             string          `json:"name"`
		Temperature      *bool           `json:"temperature,omitempty"`
		ReasoningOptions json.RawMessage `json:"reasoning_options,omitempty"`
		Limit            *CatalogLimit   `json:"limit,omitempty"`
		Modalities       json.RawMessage `json:"modalities,omitempty"`
		Attachment       *bool           `json:"attachment,omitempty"`
		Reasoning        *bool           `json:"reasoning,omitempty"`
		ToolCall         *bool           `json:"tool_call,omitempty"`
	}

	var raw rawModel
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	m.ID = raw.ID
	m.Name = raw.Name
	m.Temperature = raw.Temperature
	m.Limit = raw.Limit
	m.Attachment = raw.Attachment
	m.Reasoning = raw.Reasoning
	m.ToolCall = raw.ToolCall

	if len(raw.ReasoningOptions) > 0 {
		trimmed := strings.TrimSpace(string(raw.ReasoningOptions))
		if strings.HasPrefix(trimmed, "[") {
			var opts []ReasoningOption
			if err := json.Unmarshal(raw.ReasoningOptions, &opts); err == nil {
				m.ReasoningOptions = opts
			}
		} else if strings.HasPrefix(trimmed, "{") {
			var opt ReasoningOption
			if err := json.Unmarshal(raw.ReasoningOptions, &opt); err == nil {
				m.ReasoningOptions = []ReasoningOption{opt}
			}
		}
	}

	// modalities is an object with input/output lists in models.dev; any other
	// shape degrades to absent so resolution falls back to attachment/unknown.
	if len(raw.Modalities) > 0 && strings.HasPrefix(strings.TrimSpace(string(raw.Modalities)), "{") {
		var mods CatalogModalities
		if err := json.Unmarshal(raw.Modalities, &mods); err == nil {
			m.Modalities = &mods
		}
	}
	return nil
}

// CatalogProvider represents a provider entry parsed from models.dev.
type CatalogProvider struct {
	ID     string                  `json:"id"`
	Name   string                  `json:"name"`
	Models map[string]CatalogModel `json:"models"`
}

// UnmarshalJSON implements custom defensive unmarshaling for CatalogProvider.
func (p *CatalogProvider) UnmarshalJSON(data []byte) error {
	type rawProvider struct {
		ID     string          `json:"id"`
		Name   string          `json:"name"`
		Models json.RawMessage `json:"models"`
	}

	var raw rawProvider
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	p.ID = raw.ID
	p.Name = raw.Name
	p.Models = make(map[string]CatalogModel)

	if len(raw.Models) > 0 {
		trimmed := strings.TrimSpace(string(raw.Models))
		if strings.HasPrefix(trimmed, "{") {
			var modelsMap map[string]CatalogModel
			if err := json.Unmarshal(raw.Models, &modelsMap); err == nil {
				p.Models = modelsMap
				for k, m := range p.Models {
					if m.ID == "" {
						m.ID = k
						p.Models[k] = m
					}
				}
			}
		} else if strings.HasPrefix(trimmed, "[") {
			var modelsSlice []CatalogModel
			if err := json.Unmarshal(raw.Models, &modelsSlice); err == nil {
				for _, m := range modelsSlice {
					if m.ID != "" {
						p.Models[m.ID] = m
					}
				}
			}
		}
	}
	return nil
}

// CatalogData holds all providers and models from models.dev.
type CatalogData struct {
	Providers map[string]CatalogProvider
}

// parseCatalogJSON parses raw models.dev JSON defensively.
func parseCatalogJSON(data []byte) (*CatalogData, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return &CatalogData{Providers: make(map[string]CatalogProvider)}, nil
	}

	result := &CatalogData{Providers: make(map[string]CatalogProvider)}

	if strings.HasPrefix(trimmed, "{") {
		var rawMap map[string]CatalogProvider
		if err := json.Unmarshal(data, &rawMap); err != nil {
			return nil, fmt.Errorf("unmarshal catalog map: %w", err)
		}
		for k, p := range rawMap {
			if p.ID == "" {
				p.ID = k
			}
			result.Providers[p.ID] = p
		}
		return result, nil
	} else if strings.HasPrefix(trimmed, "[") {
		var rawSlice []CatalogProvider
		if err := json.Unmarshal(data, &rawSlice); err != nil {
			return nil, fmt.Errorf("unmarshal catalog slice: %w", err)
		}
		for _, p := range rawSlice {
			if p.ID != "" {
				result.Providers[p.ID] = p
			}
		}
		return result, nil
	}

	return nil, errors.New("invalid catalog JSON root format (must be object or array)")
}

// Options contains configuration for the model catalog service.
type ModelCatalogOptions struct {
	CacheDir   string
	CatalogURL string
	TTL        time.Duration
	Client     *http.Client
	Registry   *providers.Registry
}

// Service manages models.dev caching and two-tier model resolution.
type ModelCatalog struct {
	cacheDir   string
	cachePath  string
	catalogURL string
	ttl        time.Duration
	client     *http.Client
	registry   *providers.Registry
	sf         singleflight.Group

	mu        sync.RWMutex
	inMemory  *CatalogData
	lastFetch time.Time
}

// NewModelCatalog creates a new model catalog service.
func NewModelCatalog(opts ModelCatalogOptions) *ModelCatalog {
	cacheDir := strings.TrimSpace(opts.CacheDir)
	if cacheDir == "" {
		cacheDir = DefaultCacheDir
	}
	catalogURL := strings.TrimSpace(opts.CatalogURL)
	if catalogURL == "" {
		catalogURL = DefaultCatalogURL
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	reg := opts.Registry
	if reg == nil {
		reg = providers.NewRegistry()
	}

	return &ModelCatalog{
		cacheDir:   cacheDir,
		cachePath:  filepath.Join(cacheDir, CacheFileName),
		catalogURL: catalogURL,
		ttl:        ttl,
		client:     client,
		registry:   reg,
	}
}

// FetchCatalog returns the parsed models.dev catalog data, fetching from the network or disk cache.
func (s *ModelCatalog) FetchCatalog(ctx context.Context) (*CatalogData, error) {
	// 1. Check valid in-memory cache
	s.mu.RLock()
	if s.inMemory != nil && time.Since(s.lastFetch) < s.ttl {
		data := s.inMemory
		s.mu.RUnlock()
		return data, nil
	}
	s.mu.RUnlock()

	// 2. Check valid disk cache
	if info, err := os.Stat(s.cachePath); err == nil && time.Since(info.ModTime()) < s.ttl {
		if content, err := os.ReadFile(s.cachePath); err == nil {
			if data, err := parseCatalogJSON(content); err == nil {
				s.mu.Lock()
				s.inMemory = data
				s.lastFetch = info.ModTime()
				s.mu.Unlock()
				return data, nil
			}
		}
	}

	// 3. Expired or missing disk cache -> singleflight network fetch
	val, err, _ := s.sf.Do("fetch_catalog", func() (any, error) {
		// Double-check if another goroutine fetched while waiting
		s.mu.RLock()
		if s.inMemory != nil && time.Since(s.lastFetch) < s.ttl {
			data := s.inMemory
			s.mu.RUnlock()
			return data, nil
		}
		s.mu.RUnlock()

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, s.catalogURL, nil)
		if reqErr != nil {
			return s.fallbackToStale(reqErr)
		}
		req.Header.Set("User-Agent", "onclaw")

		resp, doErr := s.client.Do(req)
		if doErr != nil {
			return s.fallbackToStale(doErr)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return s.fallbackToStale(fmt.Errorf("models.dev returned HTTP %d (%s)", resp.StatusCode, http.StatusText(resp.StatusCode)))
		}

		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return s.fallbackToStale(readErr)
		}

		data, parseErr := parseCatalogJSON(body)
		if parseErr != nil {
			return s.fallbackToStale(parseErr)
		}

		// Atomic disk write
		if writeErr := atomicWriteFile(s.cachePath, body); writeErr != nil {
			slog.Warn("failed to write models catalog cache to disk", "path", s.cachePath, "error", writeErr)
		}

		s.mu.Lock()
		s.inMemory = data
		s.lastFetch = time.Now()
		s.mu.Unlock()

		return data, nil
	})

	if err != nil {
		return nil, err
	}
	return val.(*CatalogData), nil
}

func (s *ModelCatalog) fallbackToStale(originalErr error) (*CatalogData, error) {
	// Try reading stale file on disk
	if content, err := os.ReadFile(s.cachePath); err == nil {
		if data, err := parseCatalogJSON(content); err == nil {
			slog.Warn("serving stale models catalog from disk due to fetch failure", "error", originalErr)
			s.mu.Lock()
			s.inMemory = data
			s.mu.Unlock()
			return data, nil
		}
	}

	// Try in-memory stale cache
	s.mu.RLock()
	if s.inMemory != nil {
		data := s.inMemory
		s.mu.RUnlock()
		slog.Warn("serving stale in-memory models catalog due to fetch failure", "error", originalErr)
		return data, nil
	}
	s.mu.RUnlock()

	return nil, fmt.Errorf("failed to fetch models catalog: %w", originalErr)
}

// EffectiveCatalogHint returns the catalog provider id catalog resolution
// should use for the provider: empty for canonically mapped provider types
// (the type mapping wins, any stored hint is ignored), otherwise the stored
// hint when set, else the host-based suggestion for known gateway hosts.
func EffectiveCatalogHint(providerType, storedHint, baseURL string) string {
	if _, mapped := MapProviderType(providerType); mapped {
		return ""
	}
	if hint := strings.TrimSpace(storedHint); hint != "" {
		return hint
	}
	return providers.SuggestCatalogProvider(baseURL)
}

// catalogEntry returns the catalog model entry for the (provider, model) pair
// under the effective catalog mapping: the canonical MapProviderType id when
// the provider type maps, otherwise the caller-supplied hint. ok is false when
// the provider type is unmapped without a hint, the catalog is unavailable, or
// the provider/model entry is absent — callers degrade to unknown semantics.
func (s *ModelCatalog) catalogEntry(ctx context.Context, providerType, catalogHint, modelID string) (*CatalogModel, bool) {
	providerID, mapped := MapProviderType(providerType)
	if !mapped {
		hint := strings.TrimSpace(catalogHint)
		if hint == "" {
			return nil, false
		}
		providerID = hint
	}

	data, err := s.FetchCatalog(ctx)
	if err != nil || data == nil {
		return nil, false
	}
	prov, ok := data.Providers[providerID]
	if !ok {
		return nil, false
	}
	model, ok := prov.Models[modelID]
	if !ok {
		return nil, false
	}
	return &model, true
}

// SupportsInput resolves whether the model behind (providerType, catalogHint,
// modelID) accepts the given non-text input kind. Resolution is
// provider-scoped: the same model id on different gateways may expose
// different modalities. Any missing evidence — unmapped provider without
// hint, catalog fetch failure, absent provider or model entry — resolves to
// InputUnknown, never an error; an entry whose input list lacks the kind
// resolves to InputUnsupported.
func (s *ModelCatalog) SupportsInput(ctx context.Context, providerType, catalogHint, modelID string, kind domain.InputKind) domain.InputSupport {
	switch kind {
	case domain.InputKindImage, domain.InputKindPDF:
	default:
		return domain.InputUnknown
	}

	model, ok := s.catalogEntry(ctx, providerType, catalogHint, modelID)
	if !ok {
		return domain.InputUnknown
	}

	// An entry with an input list is affirmative evidence: the kind present →
	// supported, absent → unsupported (the tri-state point).
	if model.Modalities != nil && len(model.Modalities.Input) > 0 {
		for _, mod := range model.Modalities.Input {
			if strings.EqualFold(strings.TrimSpace(mod), string(kind)) {
				return domain.InputSupported
			}
		}
		return domain.InputUnsupported
	}

	// Without a modalities list only the pdf kind has a fallback signal
	// (attachment==true); image has none and stays unknown (fail-open).
	if kind == domain.InputKindPDF && model.Attachment != nil && *model.Attachment {
		return domain.InputSupported
	}
	return domain.InputUnknown
}

// projectModelCapabilities copies the catalog entry's input modality and
// capability flags onto the resolved model. The bools are set only on
// affirmative catalog evidence; the pointer fields are set only when the
// catalog affirmatively says true, so absent entries stay omitted.
func projectModelCapabilities(m *domain.Model, cat *CatalogModel) {
	if cat.Modalities != nil {
		for _, mod := range cat.Modalities.Input {
			switch strings.ToLower(strings.TrimSpace(mod)) {
			case "image":
				m.ImageInput = true
			case "pdf":
				m.PDFInput = true
			}
		}
	}
	if cat.Reasoning != nil && *cat.Reasoning {
		m.Reasoning = cat.Reasoning
	}
	if cat.ToolCall != nil && *cat.ToolCall {
		m.ToolCall = cat.ToolCall
	}
}

// catalogMapping resolves the catalog provider id for the credential: the
// canonical provider-type mapping wins; compatible gateways fall back to the
// credential's catalog hint, then the host-based suggestion for known gateway
// hosts. hasMapping is false when none applies.
func catalogMapping(cred providers.Credential) (string, bool) {
	providerID, mapped := MapProviderType(cred.Type)
	if mapped {
		return providerID, true
	}
	if hint := strings.TrimSpace(cred.CatalogHint); hint != "" {
		return hint, true
	}
	if suggested := providers.SuggestCatalogProvider(cred.BaseURL); suggested != "" {
		return suggested, true
	}
	return "", false
}

// ResolveModels performs two-tier model resolution for the given provider credential.
// Tier 1: provider ListModels live API call
// Tier 2: models.dev cached catalog fallback
// Both fail: source "none" with empty models slice
func (s *ModelCatalog) ResolveModels(ctx context.Context, cred providers.Credential) (*domain.ModelsResult, error) {
	p, err := s.registry.Get(cred.Type)
	if err != nil {
		return nil, err
	}

	catalogProviderID, hasMapping := catalogMapping(cred)

	// Tier 1: Try live ListModels
	liveModels, liveErr := p.ListModels(ctx, cred)
	if liveErr == nil && len(liveModels) > 0 {
		// Enrich live models with catalog metadata if available
		catalogData, _ := s.FetchCatalog(ctx)

		enriched := make([]domain.Model, 0, len(liveModels))
		for _, m := range liveModels {
			efforts := m.Efforts
			if efforts == nil {
				efforts = p.ValidEfforts()
			}
			supportsTemp := m.SupportsTemperature
			var contextLimit *int

			if catalogData != nil && hasMapping {
				if prov, ok := catalogData.Providers[catalogProviderID]; ok {
					if catModel, ok := prov.Models[m.ID]; ok {
						if catEfforts := catModel.EffortValues(); len(catEfforts) > 0 {
							efforts = catEfforts
						}
						if catModel.Temperature != nil {
							supportsTemp = *catModel.Temperature
						}
						if catModel.Limit != nil && catModel.Limit.Context != nil {
							contextLimit = catModel.Limit.Context
						}
						projectModelCapabilities(&m, &catModel)
					}
				}
			}

			if efforts == nil {
				efforts = []string{}
			}
			name := m.Name
			if name == "" {
				name = m.ID
			}

			enriched = append(enriched, domain.Model{
				ID:                  m.ID,
				Name:                name,
				Efforts:             efforts,
				SupportsTemperature: supportsTemp,
				ContextLimit:        contextLimit,
				ImageInput:          m.ImageInput,
				PDFInput:            m.PDFInput,
				Reasoning:           m.Reasoning,
				ToolCall:            m.ToolCall,
			})
		}

		return &domain.ModelsResult{
			Source: domain.ModelSourceLive,
			Models: enriched,
		}, nil
	}

	// Tier 2: Try catalog fallback
	if hasMapping {
		catalogData, catErr := s.FetchCatalog(ctx)
		if catErr == nil && catalogData != nil {
			if prov, ok := catalogData.Providers[catalogProviderID]; ok && len(prov.Models) > 0 {
				catalogModels := make([]domain.Model, 0, len(prov.Models))
				for _, catModel := range prov.Models {
					if catModel.ID == "" {
						continue
					}
					name := catModel.Name
					if name == "" {
						name = catModel.ID
					}
					efforts := catModel.EffortValues()
					if len(efforts) == 0 {
						efforts = p.ValidEfforts()
					}
					if efforts == nil {
						efforts = []string{}
					}

					supportsTemp := p.SupportsTemperature()
					if catModel.Temperature != nil {
						supportsTemp = *catModel.Temperature
					}

					var contextLimit *int
					if catModel.Limit != nil && catModel.Limit.Context != nil {
						contextLimit = catModel.Limit.Context
					}

					res := domain.Model{
						ID:                  catModel.ID,
						Name:                name,
						Efforts:             efforts,
						SupportsTemperature: supportsTemp,
						ContextLimit:        contextLimit,
					}
					projectModelCapabilities(&res, &catModel)

					catalogModels = append(catalogModels, res)
				}

				// Sort catalog models deterministically by ID
				sort.Slice(catalogModels, func(i, j int) bool {
					return catalogModels[i].ID < catalogModels[j].ID
				})

				if len(catalogModels) > 0 {
					return &domain.ModelsResult{
						Source: domain.ModelSourceCatalog,
						Models: catalogModels,
					}, nil
				}
			}
		}
	}

	// Both failed (or compatible type with no live models, or unknown gateway)
	return &domain.ModelsResult{
		Source: domain.ModelSourceNone,
		Models: []domain.Model{},
	}, nil
}

// ResolveEfforts returns the allowed effort values for a given provider type and model ID.
// catalogHint supplies the community-catalog provider id for compatible gateway
// types that MapProviderType leaves unmapped. If the catalog knows the model
// and has effort values, those are returned; otherwise, the static floor for
// the provider type is returned.
func (s *ModelCatalog) ResolveEfforts(ctx context.Context, providerType, modelID, catalogHint string) []string {
	p, err := s.registry.Get(providerType)
	if err != nil {
		return []string{}
	}

	if catModel, ok := s.catalogEntry(ctx, providerType, catalogHint, modelID); ok {
		if catEfforts := catModel.EffortValues(); len(catEfforts) > 0 {
			return catEfforts
		}
	}

	floor := p.ValidEfforts()
	if floor == nil {
		return []string{}
	}
	return floor
}

// ResolveContextLimit returns the context window limit for a given provider
// type and model ID. catalogHint supplies the community-catalog provider id
// for compatible gateway types that MapProviderType leaves unmapped. For
// unmapped providers without a hint, catalog fetch failures, or unknown
// models, nil is returned.
func (s *ModelCatalog) ResolveContextLimit(ctx context.Context, providerType, modelID, catalogHint string) *int {
	catModel, ok := s.catalogEntry(ctx, providerType, catalogHint, modelID)
	if !ok {
		return nil
	}
	return catModel.ContextLimit()
}

// atomicWriteFile writes data to a temporary file in the destination directory and renames it atomically.
func atomicWriteFile(filePath string, data []byte) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".models.dev-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpName, filePath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename file: %w", err)
	}

	return nil
}
