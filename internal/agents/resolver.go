package agents

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

// CapabilityResolver resolves model capabilities through three fallback tiers
// (design D4):
// Tier 1: Stored row in provider_models (store-backed lookup)
// Tier 2: Static built-in catalog (by provider type)
// Tier 3: Conservative defaults (DefaultContextWindow, id-heuristic efforts, false inputs)
type CapabilityResolver struct {
	store store.Store
}

// NewCapabilityResolver returns a new CapabilityResolver backed by the given store.
// When store is nil, resolution skips tier 1 and proceeds to tiers 2 and 3.
func NewCapabilityResolver(s store.Store) *CapabilityResolver {
	return &CapabilityResolver{store: s}
}

// Resolve resolves the capabilities of (providerID, modelID).
//
// 1. Tier 1 (store-backed lookup): If store is present and providerID is non-empty,
// it queries store.ProviderModels().ByProvider(ctx, providerID).
// If a matching model row is found:
//   - If row.Enabled is false: returns the capabilities and ErrModelDisabled
//     (Enabled flag is false).
//   - If row.Enabled is true: capabilities come from the stored row
//     (ContextWindow, ImageInput, PDFInput, VideoInput, Efforts). A row with
//     an empty Efforts set resolves to [EffortHigh] (design D3: the row is
//     authoritative — no catalog or heuristic fallback for stored rows).
//     Returns capabilities and nil error.
//
// 2. Tier 2 (static catalog fallback): If no matching row is found in the store
// (or store lookup yields nothing), it looks up the provider's type in modelCatalog.
// If present, returns capabilities from the catalog and nil error.
//
// 3. Tier 3 (defaults fallback): If not found in static catalog, returns
// DefaultContextWindow, id-heuristic efforts, false for ImageInput and PDFInput,
// and nil error.
func (r *CapabilityResolver) Resolve(ctx context.Context, providerID, modelID string) (ModelCapabilities, error) {
	var providerType string
	if r != nil && r.store != nil && providerID != "" {
		if p, err := r.store.Providers().ByID(ctx, providerID); err == nil && p != nil {
			providerType = p.Type
		}
	}
	if providerType == "" && IsKnownProviderType(providerID) {
		providerType = providerID
	}

	// Tier 1: Store-backed lookup.
	if r != nil && r.store != nil && providerID != "" {
		rows, err := r.store.ProviderModels().ByProvider(ctx, providerID)
		if err == nil {
			for _, row := range rows {
				if row.ModelID == modelID {
					cw := row.ContextWindow
					if cw <= 0 {
						cw = DefaultContextWindow
					}
					// The row is authoritative (design D3): every field
					// resolves from it the way it already does for the
					// context window and the input flags — an undeclared
					// Efforts set degrades to high, never back to the
					// catalog or heuristic.
					efforts := row.Efforts
					if len(efforts) == 0 {
						efforts = []string{EffortHigh}
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

	// Tier 2: Static built-in catalog fallback.
	if providerType != "" {
		if models, ok := modelCatalog[providerType]; ok {
			if spec, ok := models[modelID]; ok {
				return ModelCapabilities{
					ContextWindow: spec.contextWindow,
					Efforts:       spec.efforts,
					ImageInput:    spec.imageInput,
					PDFInput:      spec.pdfInput,
					VideoInput:    spec.videoInput,
					Enabled:       true,
				}, nil
			}
		}
	}

	// Tier 3: Conservative defaults fallback.
	return ModelCapabilities{
		ContextWindow: DefaultContextWindow,
		Efforts:       heuristicEfforts(modelID),
		ImageInput:    false,
		PDFInput:      false,
		VideoInput:    false,
		Enabled:       true,
	}, nil
}
