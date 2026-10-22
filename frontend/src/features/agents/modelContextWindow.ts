/**
 * Frontend mirror of the runtime's static model catalog (internal/agents/
 * catalog.go, design D8): the create wizard's advanced section auto-fills
 * the context-window field from it when the selected model is known. The
 * stored value is an override — 0/empty means "use the catalog default" —
 * so an unknown model leaves the field empty rather than pinning a guess.
 */

/** Context windows (tokens) per model id, keyed by provider type. */
interface ModelContextWindows {
  [providerType: string]: Record<string, number>;
}

/**
 * Mirrors modelCatalog in Go, including which provider types carry a
 * catalog; other provider types have no known windows.
 */
const MODEL_CONTEXT_WINDOWS: ModelContextWindows = {
  anthropic: {
    "claude-opus-4-1": 200000,
    "claude-sonnet-4-5": 200000,
    "claude-haiku-4-5": 200000,
  },
  openai: {
    "gpt-5": 400000,
    "gpt-4o": 128000,
    "gpt-4o-mini": 128000,
  },
  gemini: {
    "gemini-2.5-pro": 1048576,
    "gemini-2.5-flash": 1048576,
  },
};

/**
 * The catalog's context window for the provider type + model, or null when
 * the pair is not listed (unknown models resolve to the runtime's default).
 */
export function catalogContextWindow(
  providerType: string,
  model: string
): number | null {
  const models = MODEL_CONTEXT_WINDOWS[providerType];
  const known = models?.[model];

  return known === undefined ? null : known;
}
