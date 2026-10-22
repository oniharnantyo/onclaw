/**
 * Unit tests for the client-side slug mirror (task 5.1, design D3): the
 * suggestion and validation answers must match the Go implementations in
 * internal/workspaces (Slugify / ValidSlug), which stay the source of truth.
 */
import { describe, expect, it } from "vite-plus/test";

import { MAX_SLUG_LEN, isValidSlug, suggestSlug } from "../slug";

describe("suggestSlug", () => {
  it("lowercases and folds runs of non-alphanumerics into single dashes", () => {
    expect(suggestSlug("Research Scout")).toBe("research-scout");
    expect(suggestSlug("Data  &  Reports!")).toBe("data-reports");
    expect(suggestSlug("  spaced   out  ")).toBe("spaced-out");
  });

  it("trims leading separators and drops non-ASCII characters", () => {
    expect(suggestSlug("Éclair 3000")).toBe("clair-3000");
    expect(suggestSlug("!!!")).toBe("");
    expect(suggestSlug("")).toBe("");
  });

  it("truncates over-long results at a dash boundary", () => {
    const long = `${"a".repeat(40)} ${"b".repeat(40)}`;
    const slug = suggestSlug(long);
    expect(slug.length).toBeLessThanOrEqual(MAX_SLUG_LEN);
    expect(slug).toBe("a".repeat(40));
  });

  it("hard-cuts when no dash exists in range", () => {
    expect(suggestSlug("a".repeat(70))).toBe("a".repeat(MAX_SLUG_LEN));
  });
});

describe("isValidSlug", () => {
  it("accepts DNS-label shapes", () => {
    expect(isValidSlug("scout")).toBe(true);
    expect(isValidSlug("research-scout")).toBe(true);
    expect(isValidSlug("a")).toBe(true);
    expect(isValidSlug("9lives")).toBe(true);
    expect(isValidSlug("a".repeat(MAX_SLUG_LEN))).toBe(true);
  });

  it("rejects everything else", () => {
    expect(isValidSlug("")).toBe(false);
    expect(isValidSlug("Scout")).toBe(false);
    expect(isValidSlug("under_score")).toBe(false);
    expect(isValidSlug("-lead")).toBe(false);
    expect(isValidSlug("trail-")).toBe(false);
    expect(isValidSlug("dou--ble")).toBe(false);
    expect(isValidSlug("has space")).toBe(false);
    expect(isValidSlug("a".repeat(MAX_SLUG_LEN + 1))).toBe(false);
  });
});
