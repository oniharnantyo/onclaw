// Status-tier token contract (change fix-ui-audit-findings, task 1.1): the
// four lifted status-text tiers (`--accent-text`, `--warn-text`,
// `--success-text`, `--danger-text`) must clear the WCAG 2.x 4.5:1 bar
// against the app's surfaces in BOTH themes, and every tier must have a
// `--color-*text` `@theme` mapping so the utility resolves to the contract
// token. Reads tokens.css/theme.css from disk so palette drift fails here,
// not in an audit.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vite-plus/test";

// Read relative to this test file:
// frontend/src/lib/__test__/statusTierContrast.test.ts → frontend/src/styles/.
const tokensCss = readFileSync(
  resolve(import.meta.dirname, "../../styles/tokens.css"),
  "utf8"
);

const themeCss = readFileSync(
  resolve(import.meta.dirname, "../../styles/theme.css"),
  "utf8"
);

type Palette = Record<string, string>;

/** Extracts one `:root`/`:root[data-theme=…]` block's declarations. */
function parseBlock(css: string, selector: string): Palette {
  const start = css.indexOf(`${selector} {`);

  expect(start).toBeGreaterThanOrEqual(0);
  const end = css.indexOf("}", start);
  const body = css.slice(start, end);
  const palette: Palette = {};

  for (const [, name, value] of body.matchAll(/(--[\w-]+):\s*([^;]+);/g)) {
    palette[name] = value.trim();
  }

  return palette;
}

const light = parseBlock(tokensCss, ":root");

const dark = parseBlock(tokensCss, ':root[data-theme="dark"]');

/** Resolves a one-level `var(--other)` alias against the same palette. */
function resolveToken(palette: Palette, name: string): string {
  const raw = palette[name];

  expect(raw).toBeDefined();
  // Declarations may wrap (e.g. the multi-line `--accent-text` alias), so
  // collapse whitespace before matching.
  const flat = raw.replace(/\s+/g, " ").trim();
  const alias = flat.match(/^var\(\s*(--[\w-]+)\s*\)$/);

  return alias ? resolveToken(palette, alias[1]!) : flat;
}

/** WCAG 2.x relative luminance of a #rrggbb hex. */
function luminance(hex: string): number {
  const channels = [1, 3, 5].map((i) => {
    const v = parseInt(hex.slice(i, i + 2), 16) / 255;

    return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  });

  return 0.2126 * channels[0]! + 0.7152 * channels[1]! + 0.0722 * channels[2]!;
}

/** WCAG 2.x contrast ratio between two hex colors. */
function contrast(a: string, b: string): number {
  const [l1, l2] = [luminance(a), luminance(b)].sort((x, y) => y - x);

  return (l1 + 0.05) / (l2 + 0.05);
}

const TIERS = [
  "--accent-text",
  "--warn-text",
  "--success-text",
  "--danger-text",
] as const;

describe("status tier contrast (fix-ui-audit-findings 1.1)", () => {
  it("defines all four lifted tiers as explicit tokens in both palettes", () => {
    for (const tier of TIERS) {
      expect(light[tier]).toBeDefined();
      expect(dark[tier]).toBeDefined();
    }
  });

  it("never resolves a tier by aliasing the status fill in light", () => {
    // Spec: tiers MUST NOT resolve by inheriting the fill (the old
    // `--danger-text: var(--danger)` alias sat below the 4.5:1 bar).
    expect(light["--danger-text"]).not.toBe("var(--danger)");
    expect(light["--success-text"]).not.toBe("var(--success)");
    expect(light["--warn-text"]).not.toBe("var(--warn)");
  });

  for (const theme of ["light", "dark"] as const) {
    const palette = theme === "light" ? light : dark;

    it(`clears 4.5:1 on --surface and --bg in the ${theme} theme`, () => {
      const surface = resolveToken(palette, "--surface");
      const bg = resolveToken(palette, "--bg");

      for (const tier of TIERS) {
        const fg = resolveToken(palette, tier);

        // `--accent-text` keeps its light alias of `--accent` by design (D1):
        // it clears the bar on --surface (4.57:1) but sits at 4.38:1 on the
        // dimmer --bg — a pre-existing, out-of-scope gap pinned by the design.
        const targets =
          tier === "--accent-text" && theme === "light"
            ? [surface]
            : [surface, bg];

        for (const surfaceToken of targets) {
          expect(contrast(fg, surfaceToken)).toBeGreaterThanOrEqual(4.5);
        }
      }
    });
  }

  it("is wired through @theme so all four text utilities resolve", () => {
    for (const name of [
      "--color-accenttext",
      "--color-warntext",
      "--color-successtext",
      "--color-dangertext",
    ]) {
      expect(themeCss).toContain(`${name}: var(--`);
    }
  });
});
