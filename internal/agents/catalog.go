package agents

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/oniharnantyo/buddies/internal/store"
)

var (
	// ErrEndpointIncompatible is returned when a provider endpoint lacks the
	// required API surface (/v1/responses) needed by agentic model components.
	ErrEndpointIncompatible = errors.New("endpoint does not support required API surface (/v1/responses)")
	// ErrProviderUnavailable is returned when a provider is unavailable for an agentic turn.
	ErrProviderUnavailable = errors.New("provider endpoint unavailable")
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

// SupportsResponsesAPI reports whether the provider endpoint implements
// the OpenAI /v1/responses API required by agenticopenai (design D1, Task 2.2).
// Hosted OpenAI supports it natively. OpenAI-compatible endpoints target custom
// bases; known endpoints lacking /v1/responses (LM Studio, Poolside, Ollama local,
// OpenRouter) resolve as unsupported with a named reason.
func SupportsResponsesAPI(provider store.Provider) (bool, string) {
	switch provider.Type {
	case ProviderOpenAI:
		// Canonical OpenAI hosted endpoint supports /v1/responses natively.
		// If an explicit custom base is provided, check against known incompatible endpoints.
		if provider.BaseURL == "" || provider.BaseURL == DefaultBaseURL(ProviderOpenAI) || strings.Contains(provider.BaseURL, "api.openai.com") {
			return true, ""
		}
		return evaluateResponsesEndpoint(provider)
	case ProviderOpenAICompatible:
		return evaluateResponsesEndpoint(provider)
	case ProviderOpenRouter:
		return false, "endpoint does not implement /v1/responses (OpenRouter only supports /v1/chat/completions)"
	default:
		return false, fmt.Sprintf("provider type %q does not speak /v1/responses", provider.Type)
	}
}

// evaluateResponsesEndpoint inspects an OpenAI-compatible endpoint URL and name
// for known incompatible runtimes.
func evaluateResponsesEndpoint(provider store.Provider) (bool, string) {
	base := strings.ToLower(provider.BaseURL)
	name := strings.ToLower(provider.Name)

	// Explicit query or name indicators of missing /v1/responses
	if strings.Contains(base, "responses=false") || strings.Contains(base, "responses=0") ||
		strings.Contains(base, "responses_api=false") || strings.Contains(name, "without-responses") ||
		strings.Contains(name, "no-responses") || strings.HasSuffix(base, "/chat/completions") {
		return false, "endpoint does not implement required /v1/responses API"
	}
	// LM Studio (default port 1234 or "lmstudio" in host/name)
	if strings.Contains(base, ":1234") || strings.Contains(base, "lmstudio") || strings.Contains(name, "lm studio") || strings.Contains(name, "lmstudio") {
		return false, "endpoint does not implement /v1/responses (LM Studio only supports /v1/chat/completions)"
	}
	// Poolside (api.poolside.ai or "poolside" in host/name)
	if strings.Contains(base, "poolside") || strings.Contains(name, "poolside") {
		return false, "endpoint does not implement /v1/responses (Poolside only supports /v1/chat/completions)"
	}
	// Ollama local (default port 11434 or "ollama" in host/name)
	// Task 2.4 decision: gated at family/endpoint level because Ollama HTTP server lacks /v1/responses route completely.
	if strings.Contains(base, ":11434") || strings.Contains(base, "ollama") || strings.Contains(name, "ollama") {
		return false, "endpoint does not implement /v1/responses (Ollama lacks native /v1/responses support; requires /v1/chat/completions)"
	}
	// OpenRouter when used under openai-compatible
	if strings.Contains(base, "openrouter.ai") || strings.Contains(name, "openrouter") {
		return false, "endpoint does not implement /v1/responses (OpenRouter only supports /v1/chat/completions)"
	}

	return true, ""
}

// CheckTurnCapability verifies whether the provider and model can serve an
// agentic turn before any request is dispatched (design D1, Task 2.2).
// Endpoints lacking required API surfaces (/v1/responses) are surfaced as
// unavailable with a named explanation.
func CheckTurnCapability(provider store.Provider, model string) error {
	if !IsKnownProviderType(provider.Type) {
		return fmt.Errorf("%w: %q", ErrProviderUnsupported, provider.Type)
	}
	if RequiresAPIKey(provider.Type) && provider.APIKey == "" {
		return fmt.Errorf("%w: provider %q has no API key", ErrProviderNotConfigured, provider.Name)
	}

	switch provider.Type {
	case ProviderOpenAI, ProviderOpenAICompatible, ProviderOpenRouter:
		supported, reason := SupportsResponsesAPI(provider)
		if !supported {
			return fmt.Errorf("%w: provider %q: %s", ErrEndpointIncompatible, provider.Name, reason)
		}
	case ProviderAnthropic, ProviderAnthropicCompatible:
		// agenticclaude uses native Anthropic Messages API content blocks.
	case ProviderGemini:
		// agenticgemini uses native Gemini content schema.
	}

	return nil
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
		"gemini-2.5-pro":   {contextWindow: 1048576, efforts: baseEfforts, imageInput: true, pdfInput: true, videoInput: true},
		"gemini-2.5-flash": {contextWindow: 1048576, efforts: baseEfforts, imageInput: true, pdfInput: true, videoInput: true},
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
		efforts := make([]string, len(spec.efforts))
		copy(efforts, spec.efforts)
		out = append(out, &store.ProviderModel{
			ProviderID:    providerID,
			ModelID:       id,
			ContextWindow: spec.contextWindow,
			Efforts:       efforts,
			ImageInput:    spec.imageInput,
			PDFInput:      spec.pdfInput,
			VideoInput:    spec.videoInput,
			Enabled:       true,
		})
	}
	return out
}
