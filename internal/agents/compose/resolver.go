package compose

import (
	"context"
	"errors"

	"github.com/oniharnantyo/buddies/internal/store"
)

// ErrModelDisabled indicates that a model exists in the provider's stored
// model list but has been marked disabled (design D4).
var ErrModelDisabled = errors.New("model is disabled")

// ModelCapabilities carries the resolved runtime capabilities for a model
// (design D4: context window, reasoning efforts, multimodal inputs — image,
// PDF, video — and enabled).
type ModelCapabilities struct {
	ContextWindow int
	Efforts       []string
	ImageInput    bool
	PDFInput      bool
	VideoInput    bool
	Enabled       bool
}

// ModelSpec describes a static model specification in a catalog.
type ModelSpec struct {
	ContextWindow int
	Efforts       []string
	ImageInput    bool
	PDFInput      bool
	VideoInput    bool
}

// ModelCatalogLookup resolves a model's static spec if defined.
type ModelCatalogLookup func(providerType, modelID string) (ModelSpec, bool)

// KnownProviderTypeFunc checks if a provider type is known.
type KnownProviderTypeFunc func(providerType string) bool

// CapabilityResolver resolves model capabilities through three fallback tiers
// (design D4):
// Tier 1: Stored row in provider_models (store-backed lookup)
// Tier 2: Static built-in catalog (by provider type)
// Tier 3: Conservative defaults (DefaultContextWindow, id-heuristic efforts, false inputs)
type CapabilityResolver struct {
	store           store.Store
	catalogLookup   ModelCatalogLookup
	isKnownType     KnownProviderTypeFunc
	defaultWindow   int
	effortHigh      string
	heuristicEffort func(modelID string) []string
}

// ResolverOption configures a CapabilityResolver.
type ResolverOption func(*CapabilityResolver)

// WithCatalogLookup configures the tier-2 catalog lookup.
func WithCatalogLookup(lookup ModelCatalogLookup) ResolverOption {
	return func(r *CapabilityResolver) { r.catalogLookup = lookup }
}

// WithKnownProviderType configures provider type validation.
func WithKnownProviderType(fn KnownProviderTypeFunc) ResolverOption {
	return func(r *CapabilityResolver) { r.isKnownType = fn }
}

// WithDefaultWindow configures the tier-3 default context window.
func WithDefaultWindow(w int) ResolverOption {
	return func(r *CapabilityResolver) { r.defaultWindow = w }
}

// WithHeuristicEffort configures tier-3 heuristic effort derivation.
func WithHeuristicEffort(fn func(string) []string) ResolverOption {
	return func(r *CapabilityResolver) { r.heuristicEffort = fn }
}

// NewCapabilityResolver returns a new CapabilityResolver backed by the given store.
func NewCapabilityResolver(s store.Store, opts ...ResolverOption) *CapabilityResolver {
	r := &CapabilityResolver{
		store:         s,
		defaultWindow: 200000,
		effortHigh:    "high",
		heuristicEffort: func(_ string) []string {
			return []string{"high"}
		},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Resolve resolves the capabilities of (providerID, modelID).
func (r *CapabilityResolver) Resolve(ctx context.Context, providerID, modelID string) (ModelCapabilities, error) {
	var providerType string
	if r != nil && r.store != nil && providerID != "" {
		if p, err := r.store.Providers().ByID(ctx, providerID); err == nil && p != nil {
			providerType = p.Type
		}
	}
	if providerType == "" && r != nil && r.isKnownType != nil && r.isKnownType(providerID) {
		providerType = providerID
	}

	defWindow := 200000
	if r != nil && r.defaultWindow > 0 {
		defWindow = r.defaultWindow
	}
	highEffort := "high"
	if r != nil && r.effortHigh != "" {
		highEffort = r.effortHigh
	}

	// Tier 1: Store-backed lookup.
	if r != nil && r.store != nil && providerID != "" {
		rows, err := r.store.ProviderModels().ByProvider(ctx, providerID)
		if err == nil {
			for _, row := range rows {
				if row.ModelID == modelID {
					cw := row.ContextWindow
					if cw <= 0 {
						cw = defWindow
					}
					efforts := row.Efforts
					if len(efforts) == 0 {
						efforts = []string{highEffort}
					}
					caps := ModelCapabilities{
						ContextWindow: cw,
						Efforts:       efforts,
						ImageInput:    row.ImageInput,
						PDFInput:      row.PDFInput,
						VideoInput:    row.VideoInput,
						Enabled:       row.Enabled,
					}
					if !row.Enabled {
						return caps, ErrModelDisabled
					}
					return caps, nil
				}
			}
		}
	}

	// Tier 2: Static catalog fallback.
	if providerType != "" && r != nil && r.catalogLookup != nil {
		if spec, ok := r.catalogLookup(providerType, modelID); ok {
			return ModelCapabilities{
				ContextWindow: spec.ContextWindow,
				Efforts:       spec.Efforts,
				ImageInput:    spec.ImageInput,
				PDFInput:      spec.PDFInput,
				VideoInput:    spec.VideoInput,
				Enabled:       true,
			}, nil
		}
	}

	// Tier 3: Conservative defaults fallback.
	efforts := []string{highEffort}
	if r != nil && r.heuristicEffort != nil {
		efforts = r.heuristicEffort(modelID)
	}

	return ModelCapabilities{
		ContextWindow: defWindow,
		Efforts:       efforts,
		ImageInput:    false,
		PDFInput:      false,
		VideoInput:    false,
		Enabled:       true,
	}, nil
}
