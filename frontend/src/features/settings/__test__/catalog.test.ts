import { describe, expect, it } from "vite-plus/test";

import {
  DEFAULT_PROVIDER_TYPE,
  PROVIDER_CATALOG,
  isCanonicalProviderType,
  providerCatalogEntry,
  providerTypeLabel,
} from "../catalog";

/**
 * The backend's known provider types, mirrored verbatim from
 * internal/agents/catalog.go KnownProviderTypes (design D1: membership in
 * Go, presentation here). A divergence on either side must fail this test.
 */
const backendKnownTypes: string[] = [
  "anthropic",
  "openai",
  "gemini",
  "openrouter",
  "openai-compatible",
  "anthropic-compatible",
];

describe("PROVIDER_CATALOG", () => {
  it("covers exactly the backend's known provider types", () => {
    expect(
      PROVIDER_CATALOG.map((e: { id: string }): string => e.id).sort()
    ).toEqual([...backendKnownTypes].sort());
  });

  it("carries the canonical labels and DefaultBaseURL placeholders", () => {
    expect(providerCatalogEntry("anthropic")).toEqual({
      id: "anthropic",
      label: "Anthropic",
      baseURLPlaceholder: "https://api.anthropic.com",
      requiresBaseUrl: false,
    });
    expect(providerCatalogEntry("openai")).toEqual({
      id: "openai",
      label: "OpenAI",
      baseURLPlaceholder: "https://api.openai.com/v1",
      requiresBaseUrl: false,
    });
    expect(providerCatalogEntry("gemini")).toEqual({
      id: "gemini",
      label: "Gemini",
      baseURLPlaceholder: "https://generativelanguage.googleapis.com",
      requiresBaseUrl: false,
    });
    expect(providerCatalogEntry("openrouter")).toEqual({
      id: "openrouter",
      label: "OpenRouter",
      baseURLPlaceholder: "https://openrouter.ai/api/v1",
      requiresBaseUrl: false,
    });
  });

  it("requires a Base URL only for the compatible types", () => {
    for (const entry of PROVIDER_CATALOG) {
      expect(entry.requiresBaseUrl).toBe(entry.id.endsWith("-compatible"));
    }

    expect(providerCatalogEntry("openai-compatible")?.label).toBe(
      "OpenAI-compatible"
    );
    expect(providerCatalogEntry("anthropic-compatible")?.label).toBe(
      "Anthropic-compatible"
    );
  });

  it("falls back to the raw type for unknown ids", () => {
    expect(providerCatalogEntry("decision-class")).toBeUndefined();
    expect(providerTypeLabel("decision-class")).toBe("decision-class");
    expect(providerTypeLabel("openai")).toBe("OpenAI");
  });

  it("defaults new-provider forms to a known type", () => {
    expect(backendKnownTypes).toContain(DEFAULT_PROVIDER_TYPE);
  });

  it("identifies canonical provider types correctly", () => {
    expect(isCanonicalProviderType("anthropic")).toBe(true);
    expect(isCanonicalProviderType("openai")).toBe(true);
    expect(isCanonicalProviderType("gemini")).toBe(true);
    expect(isCanonicalProviderType("openrouter")).toBe(false);
    expect(isCanonicalProviderType("openai-compatible")).toBe(false);
    expect(isCanonicalProviderType("anthropic-compatible")).toBe(false);
  });
});
