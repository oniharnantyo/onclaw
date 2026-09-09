## Why

Tool-call cards render raw JSON: the collapsed header pastes the args JSON string and CSS-truncates it, and the expanded body shows three labeled raw strings. Every ingredient for human-readable rendering already exists — structured arg envelopes, typed result envelopes (snapshots, search results), catalog display metadata, and a single proven card pipeline — but none of it is used at the card. Cards are the primary surface users audit agent behavior on, so raw JSON is the default face of the product's most important debug view.

## What Changes

- Collapsed header one-liner: curated verb sentences per built-in tool ("Appended to USER.md", "Searched for 'onclaw agent' — 8 results", shell shows the command verbatim), driven by a small state machine — intent form while running, outcome form when done, intent form styled as error on failure.
- Expanded view: labeled field rows with kind-shaped values (monospace chips for paths/refs/selectors/URLs, quoted literals for queries/patterns, clamped quote blocks for content), per-tool field specs; args fields render only when present.
- `edit_file` renders `old_string`/`new_string` as a stacked red/green diff block.
- Result rendering: `web.search` results as a numbered mini list; text results as clamped blocks; generic shape-detect (JSON object → humanized key-value rows, array → list, plain text → clamped block) for unknown tools.
- `{ }` raw toggle in every expanded card — today's full-JSON view stays one click away.
- Per-tool icons from the catalog's `icon_key` (the frontend mirror currently drops it).
- Tool card gains `ts` (from the event's `occurred_at`) shown in the expanded view — clock time matters for cron turns.
- Expand toggle gets `aria-expanded`; running→done one-liner swap is announced.
- Approval card adopts the shell formatter's command rendering so the approval → resolved-execute transition reads as one continuous story.
- No API or wire changes; no migration. Live, hydrated, catch-up, and cron-replayed transcripts are covered by construction — all cards mint through `TranscriptTranslator` into one shape consumed by the one renderer (`ToolCall.tsx`).

Deliberately deferred: inline screenshot images (needs capability-URL serving for agent files — backend plumbing), web.search provider attribution ("via Tavily" — result envelope lacks a provider field), ref→element lookup staying best-effort when snapshots are absent.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `web-app/chat`: the tool-call card rendering requirement changes — cards present a human-readable one-liner and labeled expanded fields instead of raw JSON args, with a raw-JSON toggle, per-tool icons, and a generic fallback for non-catalog tools.

## Impact

- `web/src/components/chat/ToolCall.tsx` — the only tool-card renderer in the app (verified single consumer).
- New `web/src/lib/toolDisplay.ts` — the declarative formatter layer (one-liner table, field specs, generic fallback).
- `web/src/lib/toolCatalog.ts` — also mirror `icon_key` from the tools catalog.
- `web/src/lib/livechat.ts` — store `ts` on the card push in `TranscriptTranslator` (one-line data-shape addition).
- No backend, API, store, or migration changes.
