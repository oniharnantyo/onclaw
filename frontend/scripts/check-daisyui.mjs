#!/usr/bin/env node
// baseline 2026-10-17: 264 — shrunk to 0 at the Wave-4 cutover (tasks.md
// 1.5/5.4); --strict is the standing gate that keeps it at 0.
//
// Standing daisyUI-usage gate (openspec change replace-daisyui-with-styled-components,
// design.md D4). Scans frontend/src/**/*.{ts,tsx} for daisyUI class families used as
// class-string tokens in className/class attributes (and querySelector('.cls')
// strings in tests). Bare identifiers like the nav view id "chat" are NOT class
// tokens and are ignored. Matching is case-sensitive: `Modal`/`ModalShell`
// component names (capital M) never trip the lowercase `modal` family.
//
// Default mode is a baseline tracker (always exits 0); `--strict` exits 1 when
// any usage is found — the cutover gate (tasks.md 5.4).
//
// Usage: node scripts/check-daisyui.mjs [--strict]

import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "src");

// Families are lowercase daisyUI class tokens (or buddies-custom retirements
// like btn-wizard / card-lift riding the same wave, per tasks.md 1.5).
const FAMILIES = [
  ["btn", /^(btn|btn-[a-z0-9-]+)$/],
  ["badge", /^(badge|badge-[a-z0-9-]+)$/],
  ["modal", /^(modal|modal-[a-z0-9-]+)$/],
  ["menu", /^(menu|menu-[a-z0-9-]+)$/],
  ["card", /^(card|card-[a-z0-9-]+)$/],
  ["alert", /^(alert|alert-[a-z0-9-]+)$/],
  ["toggle", /^toggle$/],
  ["checkbox", /^checkbox$/],
  ["loading", /^(loading|loading-[a-z0-9-]+)$/],
  ["progress", /^(progress|radial-progress)$/],
  ["divider", /^divider$/],
  ["indicator", /^(indicator|indicator-[a-z0-9-]+)$/],
  ["table", /^table$/],
  ["tab", /^(tab|tabs|tab-[a-z0-9-]+|tabs-[a-z0-9-]+)$/],
  ["link", /^(link|link-[a-z0-9-]+)$/],
  ["list-row", /^list-row$/],
  ["steps", /^(steps|step)$/],
  ["fieldset", /^(fieldset|fieldset-[a-z0-9-]+)$/],
  ["avatar", /^(avatar|avatar-[a-z0-9-]+)$/],
  ["chat", /^(chat|chat-(?:start|end|bubble(?:-[a-z0-9-]+)?|row|header|footer|image))$/],
  ["swap", /^swap$/],
  ["join", /^(join|join-item)$/],
  // daisyUI UTILITY classes that retire with the theme tokens (design.md D1).
  // Cutover prune (task 5.4): `accent` and `success` left the theme-colors
  // family — styles/tokens.css + theme.css now define --color-accent and
  // --color-success, so text/bg/border-accent|success are legitimate contract
  // utilities Tailwind emits under the same names daisyUI used; the family
  // cannot tell them apart. primary/secondary/neutral/info/warning/error stay:
  // they remain daisyUI-only names with no contract counterpart.
  ["base-content", /^(text-base-content|bg-base-(?:100|200|300)|border-base-(?:100|200|300))$/],
  ["rounded", /^(rounded-box|rounded-field|rounded-selector)$/],
  ["theme-colors", /^(?:text|bg|border)-(?:primary|secondary|neutral|info|warning|error)(?:-content)?$/],
];

// A token is a daisyUI hit only in a class-string context: className/class
// attribute values (plain, single-quoted, or template literal) and test
// querySelector('.cls') strings. Quoted substrings inside template-literal
// className bodies are class tokens too (ternary conditionals).
const CLASS_STRING_PATTERNS = [
  /(?:className|class)\s*=\s*"([^"]*)"/g,
  /(?:className|class)\s*=\s*'([^']*)'/g,
  /(?:className|class)\s*=\s*\{?`([^`]*)`/g,
  /querySelector(?:All)?\(\s*['"`]\.([^'"`\s]+)/g,
];
const QUOTED_IN_TEMPLATE = /"([^"]*)"|'([^']*)'/g;

const isSourceFile = (name) => /\.(ts|tsx)$/.test(name);

function* walk(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) yield* walk(path);
    else if (isSourceFile(entry.name)) yield path;
  }
}

// class-string candidates on one line → iterable of tokens.
function* classTokens(line) {
  for (const re of CLASS_STRING_PATTERNS) {
    re.lastIndex = 0;
    for (const m of line.matchAll(re)) {
      const body = m[1] ?? "";
      for (const token of body.split(/[\s"'`]+/).filter(Boolean)) yield token;
      // Conditional classes quoted inside a template body.
      for (const q of body.matchAll(QUOTED_IN_TEMPLATE)) {
        for (const token of (q[1] ?? q[2] ?? "").split(/\s+/).filter(Boolean)) {
          yield token;
        }
      }
    }
  }
}

const hits = []; // { file, line, token, family }

for (const file of walk(root)) {
  const rel = relative(root, file);
  const lines = readFileSync(file, "utf8").split("\n");
  lines.forEach((line, i) => {
    const seen = new Set();
    for (const token of classTokens(line)) {
      if (seen.has(token)) continue;
      seen.add(token);
      for (const [family, re] of FAMILIES) {
        if (re.test(token)) hits.push({ file: rel, line: i + 1, token, family });
      }
    }
  });
}

const byFamily = new Map();
for (const hit of hits) byFamily.set(hit.family, (byFamily.get(hit.family) ?? 0) + 1);

for (const hit of hits) {
  console.log(`${hit.file}:${hit.line}: ${hit.token} (${hit.family})`);
}

console.log("");
console.log("per-family breakdown (desc):");
for (const [family, count] of [...byFamily].sort((a, b) => b[1] - a[1])) {
  console.log(`  ${family}: ${count}`);
}

console.log("");
console.log(`daisyUI usage: ${hits.length}`);

if (process.argv.includes("--strict") && hits.length > 0) process.exit(1);
