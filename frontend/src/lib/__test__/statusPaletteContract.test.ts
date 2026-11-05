// Status palette contract (change fix-ui-audit-findings, task 2.2, design
// D2/D11): status hues reach the UI only through contract tokens — palette
// utilities (`emerald-*`, `red-*`, `amber-*`, `blue-*`) must not encode
// status colors anywhere in first-party source or the vendored elements
// seam. The wave gate re-checks the whole tree; this test keeps the seam
// from eroding between waves.
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve, relative, sep } from "node:path";
import { describe, expect, it } from "vite-plus/test";

const SRC = resolve(__dirname, "..", "..", "..");

// Tailwind palette utility detector. The property before the hue is pinned
// to a known color utility prefix (text|bg|border|…), so prose words that
// merely contain the substring (bordered-, declared-, shared-, hundred-)
// cannot match, and variant prefixes (`dark:text-red-600`) ride the
// `(?:[a-z0-9-]+:)*` group. Comments are stripped before scanning, so
// prose inside comments never trips the contract.
const PALETTE_CLASS =
  /(?:^|[\s"'`])((?:[a-z0-9-]+:)*(?:text|bg|border|stroke|fill|caret|ring|outline|decoration|divide|from|via|to|placeholder|accent|shadow)-(?:emerald|red|amber|blue)-\d{2,3})(?=$|[\s"'`])/g;

// Files still carrying palette classes, allowed only until their owning
// wave lands. Must be emptied before the wave gate passes — RunSurfaces was
// the last entry (task 2.1/5, re-based onto the success/danger text tiers).
const KNOWN_LATE: string[] = [];

// Files whose palette classes are genuinely not status colors. The
// terminal block's ansiColor maps emulate the terminal's 16-color SGR
// palette (31=red … 94=bright-blue): re-basing them onto status tokens
// would collapse distinct ANSI hues and break terminal-output fidelity.
// Every *UI status* class in this file (exit-code glyph and text, stderr
// lines, done/copy checks, the cursor) has been re-based onto the token
// families; only the ANSI emulation tables stay.
const KNOWN_NON_STATUS: string[] = [
  join("src", "components", "assistant-ui", "elements", "terminal-block.tsx"),
];

const EXTS = new Set([".ts", ".tsx"]);

// Naive comment stripper: block comments first, then line comments. Good
// enough for className scanning — a `//` inside a string literal (URLs in
// code) can at worst shorten a line, never fabricate a palette class.
function stripComments(code: string): string {
  return code.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/\/\/[^\n]*/g, " ");
}

function walk(dir: string): string[] {
  const out: string[] = [];

  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);

    if (statSync(full).isDirectory()) {
      // Test fixtures never ship classes; the vendored shadcn primitives
      // under assistant-ui/ui are the sanctioned seam (D2).
      if (entry === "__test__" || full.includes(join("assistant-ui", "ui")))
        continue;
      out.push(...walk(full));
    } else if (EXTS.has(entry.slice(entry.lastIndexOf(".")))) {
      out.push(full);
    }
  }

  return out;
}

function rel(file: string): string {
  return relative(SRC, file).split(sep).join("/");
}

describe("status palette contract (design D2: token-only status hues)", () => {
  it("detector flags a planted palette class and skips prose look-alikes", () => {
    const planted = `<span className="text-emerald-600 dark:text-emerald-400">`;
    expect(planted.match(PALETTE_CLASS)).not.toBeNull();
    // Substring traps: not class tokens, not status utilities.
    expect(
      "a bordered-red-500 and declared-red-400 shared-amber-200 hundred-blue-100".match(
        PALETTE_CLASS
      )
    ).toBeNull();
    expect(
      stripComments("// text-red-500 in prose\nlet x = 1;").match(PALETTE_CLASS)
    ).toBeNull();
  });

  it("production source carries no palette-encoded status classes", () => {
    const violations: { file: string; classes: string[] }[] = [];

    for (const file of walk(SRC)) {
      const id = rel(file);

      if (KNOWN_LATE.some((late) => id === rel(late))) continue;

      const body = stripComments(readFileSync(file, "utf8"));
      const hits = [...body.matchAll(PALETTE_CLASS)].map((m) => m[1]);

      if (hits.length > 0) {
        if (KNOWN_NON_STATUS.some((skip) => id === rel(skip))) continue;

        violations.push({ file: id, classes: hits });
      }
    }

    expect(violations).toEqual([]);
  });

  // Fill-tier-as-text ban (re-audit 2026-11-02): the lifted tiers exist so
  // small text never rides the raw fills — the baseline's 1.92:1 composer
  // warning and its TaskControls "Stopped:" sibling (1.84:1) were both this
  // shape. `text-warn` / `text-danger` are banned outright; fills keep
  // `bg-*` / `border-*`. `text-accent` / `text-success` stay allowed ONLY
  // for the D2 large-glyph rule (≥18px or bold, 3:1 suffices) — a grep
  // cannot judge size, so that half is review-enforced and pinned by the
  // re-based small-text sites.
  it("small status text uses the lifted tiers, never the raw fills", () => {
    const FILL_AS_TEXT =
      /(?:^|[\s"'`])((?:[a-z0-9-]+:)*text-(?:warn|danger))(?=$|["'`\s])/g;

    const planted = `<p className="text-[11px] text-warn">Stopped</p>`;
    expect(planted.match(FILL_AS_TEXT)).not.toBeNull();
    // The tier utilities must not trip the ban, and fill-derived non-text
    // utilities (bg-/border-/opacity variants) are not text.
    expect(`"text-warntext text-dangertext"`.match(FILL_AS_TEXT)).toBeNull();
    expect(`"bg-danger/10 border-danger/40"`.match(FILL_AS_TEXT)).toBeNull();

    const violations: { file: string; classes: string[] }[] = [];

    for (const file of walk(SRC)) {
      const body = stripComments(readFileSync(file, "utf8"));
      const hits = [...body.matchAll(FILL_AS_TEXT)].map((m) => m[1]);

      if (hits.length > 0) violations.push({ file: rel(file), classes: hits });
    }

    expect(violations).toEqual([]);
  });

  it("the late-wave allowance still points at real palette hits", () => {
    // Every KNOWN_LATE entry must still offend — when the owning wave
    // re-bases the file, the stale entry itself fails this test and gets
    // removed.
    for (const late of KNOWN_LATE) {
      const body = stripComments(readFileSync(join(SRC, late), "utf8"));
      expect([...body.matchAll(PALETTE_CLASS)].length).toBeGreaterThan(0);
    }
  });
});
