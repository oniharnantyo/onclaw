// Lazy mermaid engine lane (markdown-card-elements 1.6, design D9) — the
// KaTeX pattern from MarkdownBody: the engine ships in its own chunk, loaded
// by dynamic import on first need. Until it lands, callers render the raw
// diagram source and upgrade in place; a parse failure surfaces as a typed
// MermaidRenderError instead of a throw, so the caller's fallback (raw
// source) is a plain conditional, never a crashed render.
//
// The engine behind this lane is `beautiful-mermaid` — what the vendored
// mermaid-diagram element dictates (it renders a self-contained SVG, no DOM,
// no global mermaid instance to initialize). Diagram colors must be CONCRETE
// values: beautiful-mermaid emits each option as a custom property on the
// rendered <svg> (`--border: ${colors.border}` …), so a var()-based value
// becomes a self-reference — cyclic, invalid — and nodes lose strokes/text
// loses color (live-pass fix 2026-09-21: diagrams broken on dark). The
// palette is resolved from the app's computed tokens instead, and theme
// flips re-render through the `theme` useMermaidEngine already returns.
import { useEffect, useState } from "react";
import { useAppTheme, type AppCodeTheme } from "./appTheme";

type MermaidModule = typeof import("beautiful-mermaid");

export type MermaidRenderOptions = import("beautiful-mermaid").RenderOptions;

// Typed parse/render failure: `name` and the nominal class let callers
// distinguish "diagram source was invalid" from any other unexpected error.
export class MermaidRenderError extends Error {
  constructor(
    message: string,
    options?: { cause?: unknown },
  ) {
    super(message);
    this.name = "MermaidRenderError";
    if (options?.cause !== undefined) this.cause = options.cause;
  }
}

export type MermaidRenderResult =
  | { svg: string; error: null }
  | { svg: null; error: MermaidRenderError };

// Cached dynamic import — every caller shares one chunk load. A failed load
// clears the cache so the next mount retries instead of caching the failure.
let loader: Promise<MermaidModule> | null = null;
export function loadMermaidEngine(): Promise<MermaidModule> {
  loader ??= import("beautiful-mermaid").catch((err: unknown) => {
    loader = null;
    throw err;
  });
  return loader;
}

// Render API the callers see: synchronous (the module is already on board by
// the time an engine exists) and total — never throws, returns the typed
// error instead.
export interface MermaidEngine {
  render(code: string, options?: MermaidRenderOptions): MermaidRenderResult;
}

export function getMermaidEngine(mod: MermaidModule): MermaidEngine {
  return {
    render(code, options) {
      try {
        return { svg: mod.renderMermaidSVG(code, options), error: null };
      } catch (err) {
        return {
          svg: null,
          error:
            err instanceof MermaidRenderError
              ? err
              : new MermaidRenderError(
                  err instanceof Error ? err.message : String(err),
                  { cause: err },
                ),
        };
      }
    },
  };
}

// Degraded-first-paint hook: `{ engine: null, loading: true }` renders plain
// (raw source); once the chunk arrives the same component upgrades in place.
// `theme` carries the resolved app theme (see appTheme.ts): diagram colors
// are concrete (see readMermaidPalette), so a theme flip re-renders the SVG
// with the new palette.
export function useMermaidEngine(): {
  engine: MermaidEngine | null;
  loading: boolean;
  theme: AppCodeTheme;
} {
  const [engine, setEngine] = useState<MermaidEngine | null>(null);
  const theme = useAppTheme();
  useEffect(() => {
    if (engine) return;
    let alive = true;
    loadMermaidEngine()
      .then((mod) => {
        if (alive) setEngine(getMermaidEngine(mod));
      })
      .catch(() => {
        // Present-only degradation: the chunk never arrived; the caller keeps
        // rendering raw source. The next mount retries.
      });
    return () => {
      alive = false;
    };
  }, [engine]);
  return { engine, loading: engine === null, theme };
}

// Concrete render palette resolved from the design tokens at call time (the
// raw values live in tokens.css; light and dark both resolve from the same
// read, so tokens stay the single source of truth). Fallbacks are the design
// contract's base tier for a read before the stylesheet lands.
export function readMermaidPalette(): MermaidRenderOptions {
  if (typeof document === "undefined") return { transparent: true };
  const cs = getComputedStyle(document.documentElement);
  const read = (name: string, fallback: string): string => {
    const v = cs.getPropertyValue(name).trim();
    return v || fallback;
  };
  return {
    bg: read("--bg", "#fafafa"),
    fg: read("--fg", "#111111"),
    muted: read("--muted", "#6b6b6b"),
    border: read("--border", "#e5e5e5"),
    surface: read("--surface", "#ffffff"),
    // Arrows/highlights follow fg — the monochrome wiring both call sites use.
    accent: read("--fg", "#111111"),
    transparent: true,
  };
}
