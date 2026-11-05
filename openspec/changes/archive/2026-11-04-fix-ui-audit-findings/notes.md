# Change notes — fix-ui-audit-findings

## Implementation (2026-11-02)

Implemented via `$team` (4 parallel executors → streaming split 5a/5b + bundle
→ verifier PASS → live smoke → 2 fix loops). All 24 tasks done; evidence in
`.team/smoke/fix-ui-audit-findings/` (RESULTS.md, 7 screenshots, probe
scripts, `scan_chunks.py` gate).

## Audit score (task 8.3)

| Audit | Score | Notes |
| --- | --- | --- |
| Baseline (2026-11-02, pre-change) | **14/20** | 4×P1: warn text 1.92:1, streaming O(transcript)/token + 6.05 MB entry, no transcript live region; palette drift at the vendored seam; ARIA/keyboard gaps |
| Re-audit immediately after implementation | **15/20** (Good) | Performance 2→3 (all four P1 fixes verified real; entry 1.46 MB production, 0 chunk cycles). A11y held at 3 because the re-audit found a same-class P1 the original sweep missed |
| Post-fix-loop state | not re-scored | The re-audit's P1 (`TaskControls.tsx:141` "Stopped:" at `text-warn` 1.84:1 light) and all small-text `text-danger`/`text-accent` stragglers are re-based onto the lifted tiers; the contract test now bans fill-tier-as-text; CodeBlock wrap target at 24px. Full gate green (tsc 0, vitest 1388, build+cycle scan PASS, go gates clean) |

## Deviations from design (recorded)

- **D6c vendor `manualChunks` groups dropped** (D6 fallback): explicit vendor
  groups produced static import cycles across chunks — `vendor-radix`
  binding base-ui's eval-time CJS requires back to the entry
  (`require_shim is not a function`), `vendor-assistant-ui` statically
  importing first-party kit that imports it back
  (`memoizeMarkdownComponents is not a function`), plus `lib ↔
  vendor-markdown` and `dist ↔ vendor-mermaid`. Cycles kill boot; default
  chunking is cycle-safe. The on-demand goal never needed groups — the
  dynamic boundaries (mermaid renderer, markdown-text chains, shiki loaders)
  keep renderers out of the entry. Permanent gate:
  `.team/smoke/fix-ui-audit-findings/scan_chunks.py` (Tarjan SCC over static
  chunk edges + entry ≤ 3.5 MB + mermaid/shiki markers absent). Follow-up
  candidate: wire that scan into the repo build gate.
- **Light `--danger-text` became a defined hex** (`#b91c1c`, replacing the
  `var(--danger)` alias): the alias computes to 4.45:1 on white — under the
  4.5:1 tier bar the new token-contract test enforces (D1's "never aliases"
  principle applied to danger too).
- `--warn-text` uses the design's pinned hexes (light `#b45309`, dark
  `#eab308`), replacing the earlier lighter values.

## Follow-ups (out of scope, recorded)

- Tab→`aria-controls` on `useTablist` strips (APG recommendation; spec delta
  required panel→`aria-labelledby` only).
- `loading="lazy"` on attachment/screenshot `<img>` sites.
- Large-glyph `text-accent`/`text-success` uses are review-enforced (size
  rule not grep-able) — contract test documents this.
- px-locked typography scale, sub-xl right-bar overlay, minimap ticks:
  deferred per proposal.
