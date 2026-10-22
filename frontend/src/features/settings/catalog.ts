/**
 * Provider type catalog (design D1): the presentation half of the six LLM
 * provider types. Membership lives in Go — `internal/agents/catalog.go`
 * KnownProviderTypes is the source of truth and rejects unknown ids — while
 * this module owns the display label, the Base URL placeholder mirroring the
 * backend's DefaultBaseURL, and whether the form must require a Base URL.
 * A divergence between the two sets surfaces in catalog.test.ts.
 */

/** One provider type's form metadata. */
export interface ProviderTypeMeta {
  /** Provider type id exactly as the backend knows it. */
  id: string;
  /** Display label for the type select and row badges. */
  label: string;
  /** Base URL field placeholder; mirrors the backend's DefaultBaseURL. */
  baseURLPlaceholder: string;
  /** Whether submission without a Base URL is rejected client-side. */
  requiresBaseUrl: boolean;
}

/** The six LLM provider types, in the backend's KnownProviderTypes order. */
export const PROVIDER_CATALOG: ProviderTypeMeta[] = [
  {
    id: "anthropic",
    label: "Anthropic",
    baseURLPlaceholder: "https://api.anthropic.com",
    requiresBaseUrl: false,
  },
  {
    id: "openai",
    label: "OpenAI",
    baseURLPlaceholder: "https://api.openai.com/v1",
    requiresBaseUrl: false,
  },
  {
    id: "gemini",
    label: "Gemini",
    baseURLPlaceholder: "https://generativelanguage.googleapis.com",
    requiresBaseUrl: false,
  },
  {
    id: "openrouter",
    label: "OpenRouter",
    baseURLPlaceholder: "https://openrouter.ai/api/v1",
    requiresBaseUrl: false,
  },
  {
    id: "openai-compatible",
    label: "OpenAI-compatible",
    baseURLPlaceholder: "https://your-endpoint.example.com/v1",
    requiresBaseUrl: true,
  },
  {
    id: "anthropic-compatible",
    label: "Anthropic-compatible",
    baseURLPlaceholder: "https://your-endpoint.example.com",
    requiresBaseUrl: true,
  },
];

/** Type a fresh provider form starts on (the first catalog entry). */
export const DEFAULT_PROVIDER_TYPE: string = PROVIDER_CATALOG[0].id;

/** Catalog entry for a provider type, or undefined for unknown ids. */
export function providerCatalogEntry(
  type: string
): ProviderTypeMeta | undefined {
  return PROVIDER_CATALOG.find(
    (entry: ProviderTypeMeta): boolean => entry.id === type
  );
}

/** Display label for a provider type; unknown ids render as the raw type. */
export function providerTypeLabel(type: string): string {
  return providerCatalogEntry(type)?.label ?? type;
}

/**
 * Canonical provider types backed by built-in model catalogs:
 * anthropic, openai, gemini. Other types are live-listed / custom.
 */
export function isCanonicalProviderType(type: string): boolean {
  return type === "anthropic" || type === "openai" || type === "gemini";
}
