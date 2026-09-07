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

// CatalogModel represents a model entry parsed from models.dev.
type CatalogModel struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Temperature      *bool             `json:"temperature,omitempty"`
	ReasoningOptions []ReasoningOption `json:"reasoning_options,omitempty"`
	Limit            *CatalogLimit     `json:"limit,omitempty"`
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
	}

	var raw rawModel
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	m.ID = raw.ID
	m.Name = raw.Name
	m.Temperature = raw.Temperature
	m.Limit = raw.Limit

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

// ResolveModels performs two-tier model resolution for the given provider credential.
// Tier 1: provider ListModels live API call
// Tier 2: models.dev cached catalog fallback
// Both fail: source "none" with empty models slice
func (s *ModelCatalog) ResolveModels(ctx context.Context, cred providers.Credential) (*domain.ModelsResult, error) {
	p, err := s.registry.Get(cred.Type)
	if err != nil {
		return nil, err
	}

	// Tier 1: Try live ListModels
	liveModels, liveErr := p.ListModels(ctx, cred)
	if liveErr == nil && len(liveModels) > 0 {
		// Enrich live models with catalog metadata if available
		catalogData, _ := s.FetchCatalog(ctx)
		catalogProviderID, hasMapping := MapProviderType(cred.Type)

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
			})
		}

		return &domain.ModelsResult{
			Source: domain.ModelSourceLive,
			Models: enriched,
		}, nil
	}

	// Tier 2: Try catalog fallback
	catalogProviderID, hasMapping := MapProviderType(cred.Type)
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

					catalogModels = append(catalogModels, domain.Model{
						ID:                  catModel.ID,
						Name:                name,
						Efforts:             efforts,
						SupportsTemperature: supportsTemp,
						ContextLimit:        contextLimit,
					})
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
// If the catalog knows the model and has effort values, those are returned.
// Otherwise, the static floor for the provider type is returned.
func (s *ModelCatalog) ResolveEfforts(ctx context.Context, providerType, modelID string) []string {
	p, err := s.registry.Get(providerType)
	if err != nil {
		return []string{}
	}

	catalogProviderID, hasMapping := MapProviderType(providerType)
	if hasMapping {
		catalogData, err := s.FetchCatalog(ctx)
		if err == nil && catalogData != nil {
			if prov, ok := catalogData.Providers[catalogProviderID]; ok {
				if catModel, ok := prov.Models[modelID]; ok {
					if catEfforts := catModel.EffortValues(); len(catEfforts) > 0 {
						return catEfforts
					}
				}
			}
		}
	}

	floor := p.ValidEfforts()
	if floor == nil {
		return []string{}
	}
	return floor
}

// ResolveContextLimit returns the context window limit for a given provider type and model ID.
// If the catalog knows the model and has limit.context, that value is returned.
// For compatible provider types, unmapped providers, or unknown models, nil is returned.
func (s *ModelCatalog) ResolveContextLimit(ctx context.Context, providerType, modelID string) *int {
	catalogProviderID, hasMapping := MapProviderType(providerType)
	if !hasMapping {
		return nil
	}

	catalogData, err := s.FetchCatalog(ctx)
	if err != nil || catalogData == nil {
		return nil
	}

	prov, ok := catalogData.Providers[catalogProviderID]
	if !ok {
		return nil
	}

	catModel, ok := prov.Models[modelID]
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
