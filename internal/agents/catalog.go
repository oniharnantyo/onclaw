package agents

import (
	"sort"
	"strings"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Provider types the runtime has connectors for. The three canonical types
// resolve to first-party endpoints; openrouter and the two "-compatible"
// types target OpenAI- or Anthropic-shaped third-party endpoints.
const (
	ProviderAnthropic           = "anthropic"
	ProviderOpenAI              = "openai"
	ProviderGemini              = "gemini"
	ProviderOpenRouter          = "openrouter"
	ProviderOpenAICompatible    = "openai-compatible"
	ProviderAnthropicCompatible = "anthropic-compatible"
)

// KnownProviderTypes lists every provider type id the runtime accepts, for
// error messages and validation. The only source of membership.
var KnownProviderTypes = []string{
	ProviderAnthropic,
	ProviderOpenAI,
	ProviderGemini,
	ProviderOpenRouter,
	ProviderOpenAICompatible,
	ProviderAnthropicCompatible,
}

// IsKnownProviderType reports whether the runtime has a connector for the
// provider type.
func IsKnownProviderType(providerType string) bool {
	for _, known := range KnownProviderTypes {
		if known == providerType {
			return true
		}
	}
	return false
}

// IsCanonicalProviderType reports whether the provider type is one of the three
// canonical types backed by the built-in catalog (anthropic, openai, gemini).
func IsCanonicalProviderType(providerType string) bool {
	switch providerType {
	case ProviderAnthropic, ProviderOpenAI, ProviderGemini:
		return true
	default:
		return false
	}
}

// RequiresAPIKey reports whether the provider type cannot serve requests
// without a credential: the hosted types (the three canonical ones plus
// openrouter) reject an empty key up front with a clear error. The compatible
// types target arbitrary endpoints — local runtimes like Ollama or LM Studio
// legitimately need none — so an empty key is accepted and a missing
// credential surfaces from the endpoint itself. Unknown types are treated as
// requiring one (they are rejected as unsupported regardless).
func RequiresAPIKey(providerType string) bool {
	switch providerType {
	case ProviderOpenAICompatible, ProviderAnthropicCompatible:
		return false
	default:
		return true
	}
}

// DefaultBaseURL returns the provider type's canonical API base, applied when
// a provider or verification call passes a blank base URL. The compatible
// types have no default — the user must supply a base, so an empty string is
// returned and callers surface a clear failure instead of guessing.
func DefaultBaseURL(providerType string) string {
	switch providerType {
	case ProviderAnthropic:
		return "https://api.anthropic.com"
	case ProviderOpenAI:
		return "https://api.openai.com/v1"
	case ProviderGemini:
		return "https://generativelanguage.googleapis.com"
	case ProviderOpenRouter:
		return "https://openrouter.ai/api/v1"
	default:
		return ""
	}
}

// DefaultContextWindow is the fallback context window (tokens) for models
// outside the built-in catalog — the donor's conservative default.
const DefaultContextWindow = 200000

// Reasoning-effort option ids, the normalized picker vocabulary (design D2).
// Each maps to the provider's native reasoning parameter through its
// connector; the ids double as the values the session row and the agent's
// effort default store.
const (
	EffortMinimal = "minimal"
	EffortLow     = "low"
	EffortMedium  = "medium"
	EffortHigh    = "high"
)

// baseEfforts is the common-denominator reasoning set: minimal is offered
// only where natively supported, so heuristic-resolved models (whose exact
// native vocabulary is unknown) never offer it.
var baseEfforts = []string{EffortLow, EffortMedium, EffortHigh}

// gpt5Efforts is the gpt-5 family set: the one family whose minimal tier is
// natively supported across its members.
var gpt5Efforts = []string{EffortMinimal, EffortLow, EffortMedium, EffortHigh}

// modelSpec is one catalog model's static capabilities (design D2: the same
// table feeds context accounting, the effort pickers, and the attachment
// model-degradation checks).
type modelSpec struct {
	contextWindow int
	// efforts are the reasoning-effort options the model supports, in
	// ascending effort order. An empty set means the model takes no
	// reasoning-effort parameter (surfaces hide the picker, turns reject
	// any effort).
	efforts []string
	// imageInput, pdfInput and videoInput report the model's multimodal
	// input support.
	imageInput bool
	pdfInput   bool
	videoInput bool
}

// modelCatalog is the built-in model catalog: static capabilities per model
// id, keyed by provider type. It backs context accounting (4.4) and the
// settings/chat surfaces' model listings. Models absent from the catalog
// resolve to DefaultContextWindow and an empty capability set.
var modelCatalog = map[string]map[string]modelSpec{
	ProviderAnthropic: {
		"claude-opus-4-1":   {contextWindow: 200000, efforts: baseEfforts, imageInput: true, pdfInput: true},
		"claude-sonnet-4-5": {contextWindow: 200000, efforts: baseEfforts, imageInput: true, pdfInput: true},
		"claude-haiku-4-5":  {contextWindow: 200000, efforts: baseEfforts, imageInput: true, pdfInput: true},
	},
	ProviderOpenAI: {
		"gpt-5":       {contextWindow: 400000, efforts: gpt5Efforts, imageInput: true},
		"gpt-4o":      {contextWindow: 128000, imageInput: true},
		"gpt-4o-mini": {contextWindow: 128000, imageInput: true},
	},
	ProviderGemini: {
		"gemini-2.5-pro":   {contextWindow: 1048576, efforts: baseEfforts, imageInput: true, pdfInput: true},
		"gemini-2.5-flash": {contextWindow: 1048576, efforts: baseEfforts, imageInput: true, pdfInput: true},
	},
}

// ContextWindow returns the model's context window in tokens: the catalog
// value when the model is listed, DefaultContextWindow otherwise. Unknown
// provider types resolve to the default as well — an unknown model must not
// break accounting, only make it conservative.
func ContextWindow(providerType, model string) int {
	if models, ok := modelCatalog[providerType]; ok {
		if spec, ok := models[model]; ok {
			return spec.contextWindow
		}
	}
	return DefaultContextWindow
}

// Efforts returns the reasoning-effort options the model supports, in
// ascending effort order (design D2). Canonical providers resolve from the
// static catalog — an unlisted model on a canonical provider has none. The
// live-listed types (openrouter, the two compatible types) resolve from the
// openrouter-family id heuristic instead, since their endpoints carry no
// capability metadata. An unknown id yields an empty set: surfaces hide the
// picker, and turns reject any effort.
//
// The returned slice is shared — callers read it, never mutate it.
func Efforts(providerType, model string) []string {
	if models, ok := modelCatalog[providerType]; ok {
		return models[model].efforts
	}
	switch providerType {
	case ProviderOpenRouter, ProviderOpenAICompatible, ProviderAnthropicCompatible:
		return heuristicEfforts(model)
	default:
		return nil
	}
}

// heuristicEfforts resolves the effort set for a live-listed model id from
// its openrouter-family shape (design D2): gpt-5* → the gpt-5 set, o<digit>*
// → the base reasoning set, claude-* and gemini-2.5* → the base set.
// Vendor-prefixed ids ("anthropic/claude-...") match on their final path
// segment. Anything else — the openrouter long tail — has no known effort
// support and yields an empty set.
func heuristicEfforts(id string) []string {
	base := id
	if i := strings.LastIndex(id, "/"); i >= 0 {
		base = id[i+1:]
	}
	switch {
	case strings.HasPrefix(base, "gpt-5"):
		return gpt5Efforts
	case len(base) > 1 && base[0] == 'o' && base[1] >= '0' && base[1] <= '9':
		return baseEfforts
	case strings.HasPrefix(base, "claude-"):
		return baseEfforts
	case strings.HasPrefix(base, "gemini-2.5"):
		return baseEfforts
	default:
		return nil
	}
}

// SupportsImageInput reports whether the model accepts image input: the
// catalog value when listed, false otherwise — an unknown model degrades to
// text-only rather than being trusted with more than it reads.
func SupportsImageInput(providerType, model string) bool {
	if models, ok := modelCatalog[providerType]; ok {
		return models[model].imageInput
	}
	return false
}

// SupportsPDFInput reports whether the model accepts PDF document input,
// with the same unknown-means-false degradation as SupportsImageInput.
func SupportsPDFInput(providerType, model string) bool {
	if models, ok := modelCatalog[providerType]; ok {
		return models[model].pdfInput
	}
	return false
}

// SupportsVideoInput reports whether the model accepts video input, with the
// same unknown-means-false degradation as SupportsImageInput.
func SupportsVideoInput(providerType, model string) bool {
	if models, ok := modelCatalog[providerType]; ok {
		return models[model].videoInput
	}
	return false
}

// CatalogModels returns the catalog's model ids for the provider type,
// sorted. Unknown provider types return nil.
func CatalogModels(providerType string) []string {
	models, ok := modelCatalog[providerType]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(models))
	for id := range models {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// CatalogProviderModels builds enabled store.ProviderModel rows from the
// built-in catalog for a canonical provider type. For non-canonical types
// (openrouter, openai-compatible, anthropic-compatible) or unknown types,
// it returns nil.
func CatalogProviderModels(providerID, providerType string) []*store.ProviderModel {
	models, ok := modelCatalog[providerType]
	if !ok {
		return nil
	}
	ids := CatalogModels(providerType)
	out := make([]*store.ProviderModel, 0, len(ids))
	for _, id := range ids {
		spec := models[id]
		out = append(out, &store.ProviderModel{
			ProviderID:    providerID,
			ModelID:       id,
			ContextWindow: spec.contextWindow,
			ImageInput:    spec.imageInput,
			PDFInput:      spec.pdfInput,
			VideoInput:    spec.videoInput,
			Enabled:       true,
		})
	}
	return out
}
