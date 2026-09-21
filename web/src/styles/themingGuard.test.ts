// Theming guard (openspec change add-generative-ui-fence, design D9).
//
// Pins the shadcn-install theming class as silent-proof: a `shadcn add` of a
// registry style item re-writes the theming blocks in index.css / theme.css,
// and the reintroduced bugs (unresolved var() names, a `.dark`-bound custom
// variant, a `.dark` class selector this app never sets) fail *silently* —
// utilities and inline styles just resolve to nothing in the browser. These
// guards make the same failures loud at test time.
import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const WEB_SRC = join(__dirname, "..");

const CSS_FILES = [
  join(WEB_SRC, "index.css"),
  join(WEB_SRC, "styles", "theme.css"),
  join(WEB_SRC, "styles", "tokens.css"),
] as const;

/** Remove /* ... *​/ comments so documentation mentions of `var(----x)` or
 * `.dark` (index.css discusses the removed upstream bugs) are not scanned. */
function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, " ");
}

function loadStripped(): { file: string; css: string }[] {
  return CSS_FILES.map((file) => ({
    file,
    css: stripComments(readFileSync(file, "utf8")),
  }));
}

describe("theming guard (shadcn install class, design D9)", () => {
  describe("guard A: every var() referenced in index.css resolves", () => {
    it("resolves against custom-property definitions in index.css + theme.css + tokens.css", () => {
      const files = loadStripped();

      // Definitions: `--name:` (colon-delimited), anywhere (inside :root,
      // @theme, or selector blocks). Applies to all three files.
      const definitions = new Set<string>();
      for (const { css } of files) {
        for (const m of css.matchAll(/(--[a-zA-Z0-9-]+)\s*:/g)) {
          definitions.add(m[1]!);
        }
      }

      // References: only index.css is scanned. `var(--x, fallback)` is still a
      // reference to --x; the fallback may contain nested var() calls, so the
      // first captured name per var( token is what we check.
      const indexCss = files[0]!.css;
      const unresolved = new Map<string, number>(); // name -> first line no.
      const lines = indexCss.split("\n");
      for (const m of indexCss.matchAll(/var\(\s*(--[a-zA-Z0-9-]+)/g)) {
        const name = m[1]!;
        if (definitions.has(name)) continue;
        const lineNo =
          lines.findIndex((line) => line.includes(m[0])) + 1 || 0;
        if (!unresolved.has(name)) unresolved.set(name, lineNo);
      }

      expect(
        unresolved.size,
        `unresolved var() references in index.css (name @ first offending line):\n` +
          [...unresolved.entries()]
            .map(([name, line]) => `  ${name} @ index.css:${line}`)
            .join("\n") +
          `\nDefine them (tokens.css / theme.css / index.css) or fix the reference.`
      ).toBe(0);
    });
  });

  describe("guard B: dark variant bound to data-theme, no .dark selector", () => {
    it("@custom-variant dark appears exactly once, in theme.css, bound to :root[data-theme=\"dark\"]", () => {
      const files = loadStripped();
      const occurrences = files.flatMap(({ file, css }) =>
        [...css.matchAll(/@custom-variant\s+dark\b[^;]*/g)].map((m) => ({
          file,
          text: m[0],
        }))
      );

      expect(occurrences).toHaveLength(1);
      expect(occurrences[0]!.file).toBe(CSS_FILES[1]);
      expect(occurrences[0]!.text).toContain(':root[data-theme="dark"]');
    });

    it("no .dark selector ships in any of the three files", () => {
      const offenders: string[] = [];
      for (const { file, css } of loadStripped()) {
        // Prefix class includes parens so the historical bug shape —
        // `@custom-variant dark (&:is(.dark *))` — is caught too.
        for (const m of css.matchAll(/(^|[{},;>()\[\]\s+~])\.dark(?![\w-])/g)) {
          const line = css.slice(0, m.index).split("\n").length;
          offenders.push(`${file}:${line}`);
        }
      }
      expect(
        offenders,
        `.dark selector must not ship (this app toggles :root[data-theme="dark"]):\n` +
          offenders.join("\n")
      ).toHaveLength(0);
    });
  });
});
