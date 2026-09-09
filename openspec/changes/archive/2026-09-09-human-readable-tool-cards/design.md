## Context

`ToolCall.tsx` renders cards from string fields: `t.args` and `t.res` are the raw JSON strings stored on card items by `TranscriptTranslator` (`livechat.ts`). Every tool card in the product — live, hydrated history, catch-up, cron replay — is minted by that one class and rendered by that one component (verified single consumer; `RunsView` renders no tool cards). The tools catalog already ships display metadata (`display_name`, `icon_key`) but the frontend mirror (`toolCatalog.ts`) keeps only the display name. Tool arg/result shapes are confirmed from source: eino fs middleware (`file_path`, `old_string`, `new_string`, `pattern`, `path`, `command`), memory (`path`, `action`, `content` → `{path, result}` / `{path, content}`), web.search (`query` → `{query, max_results, results: [{title, url, content}]}`), browser facade (numeric element refs; snapshots return `{title, elements: [{ref, role, name, value}]}`).

See proposal.md — Why for motivation; the delta spec in specs/web-app/chat/spec.md for the behavior contract.

## Goals / Non-Goals

**Goals:**
- One declarative formatter layer (`toolDisplay.ts`) driving collapsed one-liners, expanded field rendering, and result shaping.
- Graceful degradation: generic fallback for non-catalog tools, raw-text fallback for unparseable strings, raw JSON toggle everywhere.

**Non-Goals:**
- No backend, API, event-payload, or migration changes. The `t.res`/`t.args` strings stay exactly as delivered.
- No inline screenshot images (needs capability-URL serving for agent files — separate change).
- No web.search provider attribution in the one-liner (result envelope has no provider field; would require a backend addition — deliberately deferred).
- No runs-surface rendering (none exists today).
- No i18n of the verb table (app is English-only).

## Decisions

**D1 — Declarative verb table, not tense algorithms.** One-liners come from a per-tool table: `{intent, outcome, object: {key, style}, facts?}` (~17 entries for 12 catalog tools + 10 facade members). Rationale: irregular verbs (Write→Wrote) and memory's `action`-driven variants kill mechanical conjugation; a two-string table entry is auditable. The four sentence shapes: verb + chip object (`Read `file_path``), command-as-sentence (shell, verbatim), verb phrase + quoted literal (`Searching for 'x'`), bare verb phrase (snapshot/screenshot). MCP tools get NO one-liner — invented English for unknown tools is a trap; they keep display name + status only.

**D2 — One-liner is a state machine over the card's own data.** Running (args parsed) → intent form. Done → outcome form + facts. Error → intent form in error style (past tense asserts success). Facts derive only from trusted envelopes: `web.search` `results.length`; `write_file` size from args `content` length. Grep/glob match counting is heuristic over freeform output_mode-dependent text — skipped. If args fail to parse (streaming edge), no one-liner mints: display name + dots.

**D3 — Field specs per tool, present-only rendering.** Expanded view renders labeled rows from a per-tool field list `{key, label, kind}`; absent arg keys render nothing (grep's ~15 optional knobs stay invisible unless passed). Kinds: `chip` (path/ref/selector/url/command), `quote` (query/pattern), `content` (clamped block), `enum`/plain. Any arg key not in the spec appends humanized at the end — no silent drops. One clamp mechanism serves content blocks, diffs, and text results ("show all N lines").

**D4 — Result shaping by envelope kind.** JSON object → humanized key-value rows; JSON array → list; plain text (fs middleware and shell results are text, `formatReadResult` already line-numbers reads) → clamped block; empty → existing "No output returned." `web.search` results render as a numbered title+host list; snapshot envelopes render as a role/name/ref element table; ref-action results collapse to "Snapshot refreshed — N elements" (every interaction returns a fresh snapshot).

**D5 — ref→name lookup from sibling cards, no global cache.** `browser.click {ref:"42"}` scans the sibling tool cards of the same agent message (latest snapshot envelope first) for `elements.find(e => e.ref === ref)?.name` → "Clicked \"Sign in\""; unnamed refs stay as chips. Rationale: the snapshot envelope is typed JSON in a prior card's `res` on the same message — a render-time walk, not a turn-scoped cache. Cannot lie about stale refs: the server rejects unknown refs, so a resolved name came from a snapshot where the ref was live. Double-delivery (replay/tap boundary window) is idempotent.

**D6 — Raw toggle in every expanded card.** Shows the unmodified args/result strings. Rationale: the formatted view must never be the only view — this is the product's debug surface; devs today rely on the raw JSON.

**D7 — Formatter sits downstream of the single card pipeline.** One renderer covers all transcript sources by construction (live/hydrated/catch-up fold through `TranscriptTranslator`). The only translator touch: store `ts` from the event's `occurred_at` on the card push — needed for expanded wall-clock time on cron turns. Approval card adopts the shell command rendering (same icon, same chip) so approval → resolved-execute reads continuously.

**D8 — Icons from the catalog.** Mirror `icon_key` alongside `display_name` in `toolCatalog.ts`; MCP ids get a default plug/puzzle icon. Removes the hardcoded `terminal` icon.

**Alternatives rejected:** backend display hints on `ToolCatalogEntry` (ConfigField-style) — principled for plugins but the curated set is small and static, the generic fallback covers everything else, and the frontend already owns display knowledge precedent (facade names, provider registry); layered on later without frontend churn if ever needed. Result summaries emitted by tools — pollutes model-visible JSON or needs a separate event channel; biggest plumbing for least payoff.

## Risks / Trade-offs

- [Curated table drifts from tool schemas] → one Go test comparing catalog keys + facade members against the table's coverage (task list includes it); unknown tools degrade to the generic fallback, so drift degrades gracefully, never breaks.
- [Args parse fail mid-stream] → formatter no-ops to raw text + name-only header; same path as today.
- [Very large args/res strings already flow to the client today] → unchanged by this design; the one-liner excludes content fields by construction, and clamping bounds DOM size in the expanded view.
- [ref→name lookup on long transcripts] → scan bounded to sibling cards of one message; worst case linear in ~dozens of cards.
- [Dropped raw header text changes muscle memory] → raw toggle preserves the full view; one-liner is strictly more information per pixel.

## Migration Plan

Pure frontend change; deploy with the next build. No flag needed — the current rendering remains as the fallback paths (raw text when parsing fails). Rollback = revert the commit.

## Open Questions

(none — browser.act's `action` enum confirmed click/type/scroll from source; all card shapes verified)
