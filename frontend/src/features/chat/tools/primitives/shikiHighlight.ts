/**
 * Lazy Shiki loader (task 9.2, design D13 verdict B / D14): the ONLY module
 * that touches shiki, and it touches it exclusively through dynamic
 * `import()` — nothing from shiki (core, engine, themes, grammars) lands in
 * the entry chunk. Callers get React-ready token data or `null`; every
 * failure path — unknown language, chunk load failure, grammar parse error —
 * degrades to `null` so the caller keeps plain text (never crashes, never
 * blocks the render, which happened before this promise resolved).
 *
 * The engine is the JavaScript regex engine (`@shikijs/engine-javascript`,
 * `forgiving` mode) — no wasm download. Both app themes load once (github
 * light/dark); per-language grammars load on first use and are remembered.
 * Tokens carry the dual-theme custom properties `--shiki-light` /
 * `--shiki-dark`; highlight.css maps whichever the app's
 * `:root[data-theme="dark"]` switch selects onto `color`.
 */
import type { HighlighterCore, ThemedToken } from "shiki/core";

/** One highlighted source line: the tokens to render in order. */
export type HighlightedLine = ThemedToken[];

/** Lazy grammar loaders, keyed by the grammar's CANONICAL shiki name (what
 * `loadLanguage` registers and `codeToTokens` resolves). Kept to a pragmatic
 * set of the languages agent turns emit; anything else degrades to plain
 * text. Each loader names its return: a bundled grammar module's default
 * export. */
const GRAMMAR_FILES = {
  bash: (): Promise<{ default: unknown }> => import("shiki/langs/bash.mjs"),
  c: (): Promise<{ default: unknown }> => import("shiki/langs/c.mjs"),
  cpp: (): Promise<{ default: unknown }> => import("shiki/langs/cpp.mjs"),
  css: (): Promise<{ default: unknown }> => import("shiki/langs/css.mjs"),
  diff: (): Promise<{ default: unknown }> => import("shiki/langs/diff.mjs"),
  docker: (): Promise<{ default: unknown }> => import("shiki/langs/docker.mjs"),
  go: (): Promise<{ default: unknown }> => import("shiki/langs/go.mjs"),
  graphql: (): Promise<{ default: unknown }> =>
    import("shiki/langs/graphql.mjs"),
  html: (): Promise<{ default: unknown }> => import("shiki/langs/html.mjs"),
  ini: (): Promise<{ default: unknown }> => import("shiki/langs/ini.mjs"),
  java: (): Promise<{ default: unknown }> => import("shiki/langs/java.mjs"),
  javascript: (): Promise<{ default: unknown }> =>
    import("shiki/langs/javascript.mjs"),
  json: (): Promise<{ default: unknown }> => import("shiki/langs/json.mjs"),
  jsx: (): Promise<{ default: unknown }> => import("shiki/langs/jsx.mjs"),
  kotlin: (): Promise<{ default: unknown }> => import("shiki/langs/kotlin.mjs"),
  markdown: (): Promise<{ default: unknown }> =>
    import("shiki/langs/markdown.mjs"),
  php: (): Promise<{ default: unknown }> => import("shiki/langs/php.mjs"),
  python: (): Promise<{ default: unknown }> => import("shiki/langs/python.mjs"),
  ruby: (): Promise<{ default: unknown }> => import("shiki/langs/ruby.mjs"),
  rust: (): Promise<{ default: unknown }> => import("shiki/langs/rust.mjs"),
  sql: (): Promise<{ default: unknown }> => import("shiki/langs/sql.mjs"),
  swift: (): Promise<{ default: unknown }> => import("shiki/langs/swift.mjs"),
  toml: (): Promise<{ default: unknown }> => import("shiki/langs/toml.mjs"),
  tsx: (): Promise<{ default: unknown }> => import("shiki/langs/tsx.mjs"),
  typescript: (): Promise<{ default: unknown }> =>
    import("shiki/langs/typescript.mjs"),
  xml: (): Promise<{ default: unknown }> => import("shiki/langs/xml.mjs"),
  yaml: (): Promise<{ default: unknown }> => import("shiki/langs/yaml.mjs"),
};

/** Fenced info-string aliases onto their canonical grammar name. */
const ALIASES = {
  "c++": "cpp",
  dockerfile: "docker",
  golang: "go",
  js: "javascript",
  jsonc: "json",
  kt: "kotlin",
  md: "markdown",
  patch: "diff",
  py: "python",
  rb: "ruby",
  rs: "rust",
  sh: "bash",
  shell: "bash",
  ts: "typescript",
  yml: "yaml",
  zsh: "bash",
};

/** The grammar loaders addressed by arbitrary runtime tags: a tag outside
 * the shipped set reads undefined and the block stays plain. */
interface GrammarIndex {
  [tag: string]: (() => Promise<{ default: unknown }>) | undefined;
}

const LANG_FILES: GrammarIndex = GRAMMAR_FILES;

/** The alias table addressed by arbitrary runtime tags: an unknown tag reads
 * undefined and resolves to itself. */
interface AliasIndex {
  [tag: string]: string | undefined;
}

const ALIAS_INDEX: AliasIndex = ALIASES;

/** The canonical grammar for a fenced info string, or null when unknown. */
function canonicalLang(tag: string): string | null {
  const canonical = ALIAS_INDEX[tag] ?? tag;

  return LANG_FILES[canonical] === undefined ? null : canonical;
}

const THEME_LIGHT = "github-light";

const THEME_DARK = "github-dark";

/** Shared core promise: `null` models "loading failed". A failure resets the
 * slot so a later block can retry (e.g. a transient chunk-load error). */
let corePromise: Promise<HighlighterCore | null> | null = null;

/** Grammars already loaded into the core (the import itself is cached by the
 * module runtime, but this skips re-registering). */
const loadedLangs = new Set<string>();

async function loadCore(): Promise<HighlighterCore | null> {
  if (corePromise === null) {
    corePromise = (async (): Promise<HighlighterCore | null> => {
      try {
        const [
          { createHighlighterCore },
          { createJavaScriptRegexEngine },
          light,
          dark,
        ] = await Promise.all([
          import("shiki/core"),
          import("@shikijs/engine-javascript"),
          import("shiki/themes/github-light.mjs"),
          import("shiki/themes/github-dark.mjs"),
        ]);

        return await createHighlighterCore({
          themes: [light.default, dark.default],
          langs: [],
          engine: createJavaScriptRegexEngine({ forgiving: true }),
        });
      } catch {
        return null;
      }
    })();

    // A failed load must not poison every future attempt: clear the slot so
    // the next block retries (this call's caller still gets `null`).
    void corePromise.then((core: HighlighterCore | null): void => {
      if (core === null) corePromise = null;
    });
  }

  return corePromise;
}

/** Whether the loader knows this language tag at all (cheap, synchronous). */
export function isKnownLanguage(language: string): boolean {
  return canonicalLang(language.toLowerCase()) !== null;
}

/**
 * Highlights `code` as `language` under both app themes. Resolves `null` —
 * never throws — when the language is unknown, the loader fails, or the
 * grammar cannot parse the source; the caller then renders plain text.
 */
export async function highlightTokens(
  code: string,
  language: string
): Promise<HighlightedLine[] | null> {
  if (code === "") return null;

  const tag = canonicalLang(language.toLowerCase());

  if (tag === null) return null;

  const loader = LANG_FILES[tag];
  const core = await loadCore();

  // SAFETY: canonicalLang only returns names present in LANG_FILES, so the
  // loader is defined whenever the tag is non-null.
  if (core === null || loader === undefined) return null;

  try {
    if (!loadedLangs.has(tag)) {
      // SAFETY: the grammar module is a bundled asset with a TextMate JSON
      // default export; the core validates it on load.
      await core.loadLanguage(
        (await loader()) as Parameters<HighlighterCore["loadLanguage"]>[0]
      );

      loadedLangs.add(tag);
    }

    const { tokens } = core.codeToTokens(code.replace(/\n$/, ""), {
      lang: tag,
      themes: { light: THEME_LIGHT, dark: THEME_DARK },
      defaultColor: false,
    });

    // SAFETY: codeToTokens returns ThemedToken rows — the exact shape the
    // HighlightedLine alias names; the call can only differ by theme count.
    return tokens as HighlightedLine[];
  } catch {
    return null;
  }
}

/** Render-side token data: the dual-theme custom properties (plus any font
 * style the grammar sets, e.g. italics) to put on the token span. */
export function tokenStyle(token: ThemedToken): Record<string, string> {
  return token.htmlStyle ?? {};
}
