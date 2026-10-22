/**
 * Lazy Shiki loader tests (task 9.2, design D13 verdict B): the REAL engine
 * — dynamic imports of shiki/core, the JavaScript regex engine, both github
 * themes, and a per-language grammar — resolves dual-theme tokens; unknown
 * languages, empty code, and failures degrade to null (the caller keeps
 * plain text; nothing throws). This suite proves the dynamic-import path
 * resolves in the built module graph; the entry-bundle separation is
 * asserted at build time (dist/assets chunk audit).
 */
import { describe, expect, it } from "vite-plus/test";

import {
  highlightTokens,
  isKnownLanguage,
  tokenStyle,
} from "../tools/primitives/shikiHighlight";

describe("shikiHighlight (task 9.2)", () => {
  it("loads the core, themes, and a grammar lazily and highlights dual-theme", async () => {
    const lines = await highlightTokens(
      'package main\n\nfunc x() { return "s" }',
      "go"
    );

    expect(lines).not.toBeNull();
    expect(lines?.length).toBeGreaterThanOrEqual(2);

    const keyword = lines?.[0]?.[0];

    expect(keyword?.content).toBe("package");
    // Dual-theme custom properties for both app themes (github light/dark).
    const style = keyword ? tokenStyle(keyword) : {};

    expect(style["--shiki-light"]).toMatch(/^#/);
    expect(style["--shiki-dark"]).toMatch(/^#/);
  });

  it("resolves aliases and degrades unknown languages, empty code to null", async () => {
    expect(isKnownLanguage("golang")).toBe(true);
    expect(isKnownLanguage("nope")).toBe(false);

    // Alias resolves to the same grammar and highlights.
    const aliased = await highlightTokens("x := 1", "golang");

    expect(aliased).not.toBeNull();

    // Unknown language: null, never a throw.
    await expect(highlightTokens("x", "nope")).resolves.toBeNull();

    // Empty code: nothing to highlight.
    await expect(highlightTokens("", "go")).resolves.toBeNull();
  });

  it("is stable across repeated calls (core and grammar are cached)", async () => {
    const first = await highlightTokens("x := 1", "go");
    const second = await highlightTokens("y := 2", "go");

    expect(first).not.toBeNull();
    expect(second).not.toBeNull();
    expect(second?.[0]?.[0]?.content).toContain("y");
  });
});
