// Lazy shiki lane (markdown-card-elements 1.6, design D9) — the KaTeX
// pattern from MarkdownBody: shiki ships in its own chunk, loaded by dynamic
// import on first need. Until it lands the highlighter renders plain code
// and upgrades in place; a failed load degrades permanently to plain (the
// next mount retries).
//
// The engine behind this lane is `react-shiki` — what the vendored
// shiki-highlighter element dictates. Only its types are imported
// statically (erased at build), so the shiki bundle stays out of the
// initial graph.
//
// Theme mapping: the app theme (light/dark, read off <html data-theme> and
// tracked live) picks exactly one bundled theme — github-light-default /
// github-dark-default, the pair the vendored element ships as its default —
// so highlighted code always follows the in-app toggle, not the OS scheme
// (the alternative `light-dark()` pairing keys off color-scheme, which this
// app does not set).
import { useEffect, useState } from "react";
import { useAppTheme, type AppCodeTheme } from "./appTheme";

type ShikiModule = typeof import("react-shiki");

// Cached dynamic import — every caller shares one chunk load. A failed load
// clears the cache so the next mount retries instead of caching the failure.
let loader: Promise<ShikiModule> | null = null;
export function loadShikiModule(): Promise<ShikiModule> {
  loader ??= import("react-shiki").catch((err: unknown) => {
    loader = null;
    throw err;
  });
  return loader;
}

export const SHIKI_CODE_THEMES: Record<AppCodeTheme, string> = {
  light: "github-light-default",
  dark: "github-dark-default",
};

export function shikiThemeFor(theme: AppCodeTheme): string {
  return SHIKI_CODE_THEMES[theme];
}

// Resolves the highlighter theme from the live app theme.
export function useShikiTheme(): string {
  return shikiThemeFor(useAppTheme());
}

// The loaded react-shiki module, or null while the chunk is in flight —
// the sync "not ready" state callers render plain from. Callers must not
// call `mod.useShikiHighlighter` conditionally in the same component; gate
// the real hook behind a child component that only mounts once the module
// exists (see the vendored SyntaxHighlighter).
export function useShikiModule(): ShikiModule | null {
  const [mod, setMod] = useState<ShikiModule | null>(null);
  useEffect(() => {
    if (mod) return;
    let alive = true;
    loadShikiModule()
      .then((loaded) => {
        if (alive) setMod(loaded);
      })
      .catch(() => {
        // Present-only degradation: plain code forever this mount.
      });
    return () => {
      alive = false;
    };
  }, [mod]);
  return mod;
}
