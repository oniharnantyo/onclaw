# Design: always-on-channel-tools

## Context

The runner already grants and strips the channel toolset by execution context (`scopeChannelToolsIn` / `withoutChannelTools`, `scopeSessionToolsIn` / `withoutSessionTools` in `internal/agents`). The gap is purely in the config surfaces and the workspace gate: the catalog, tools API, Tools pane, and agent picker all treat the three tools as ordinary selectable/toggleable tools, and the gate honors `enabled=false` rows for them — silently stripping them from channel runs. The catalog already declares `Group: "channel"` and icon keys (`message`, `history`, `check-circle`) for all three; the frontend icon set simply lacks those glyphs, and the static mirror in `toolCatalog.ts` predates the channel tools.

User-locked surface decisions (explore session, 2026-09-15): all three tools go always-on together (Channel Post gets no special treatment), and the Tools pane stays a flat list with only a "Channel Tool" section added — no full re-grouping of the pane.

## Goals / Non-Goals

- Goals: one data-driven toggleability marker end-to-end (catalog → API → panes); gate exemption so workspace settings can never strip an always-on tool; real icons everywhere the three render.
- Non-Goals: no re-grouping of the other 15 tools in the Tools pane; no sentence-table entries for channel tool cards (human-readable-card layer stays as-is); no data migration for stale settings rows or stale agent allowlist keys; no change to the runner's context scoping.

## Decisions

- **D1 — Catalog field `AlwaysOn bool`, wire field `toggleable`.** The Go entry carries `AlwaysOn` (zero value = ordinary toggleable tool, so future tools can't accidentally become non-toggleable by forgetting the field); the handler computes `toggleable: !AlwaysOn` for the payload so the frontend filters on the positive form (`t.toggleable`). Alternative considered: wire field `always_on` — rejected: absence-vs-false ambiguity reads worse on the client and the positive filter reads better.
- **D2 — Gate exemption lives in `ToolSettingsService.EnabledTools`, not the runner.** `EnabledTools` forces `true` for the three always-on keys regardless of stored rows; `applyToolGate` and `toolEnabledByPolicy` stay untouched. Single chokepoint, and every gate consumer (agent runs, per-turn overrides, facade expansion) inherits the exemption. Alternative: filter inside the runner after the gate — rejected: second chokepoint, easy to bypass later.
- **D3 — View matches the gate: `ViewForWorkspace` reports always-on entries as `enabled: true`** regardless of stored rows, so what the UI shows is what the runtime does. No divergence for a stale row to expose.
- **D4 — PATCH guard rejects, never ignores.** `enabled` supplied (non-nil pointer) on an always-on key returns 422 via `domain.ErrInvalid` ("channel tools are always active and cannot be disabled" wording family). Rejected over silently ignoring so the API doesn't pretend the write landed. Config-only patches on these keys keep today's behavior (upsert nils config for non-configurable entries).
- **D5 — UI derives everything from the payload.** ToolsPane splits `tools` into `alwaysOn` / flat list by the marker; AgentConfigModal renders `toolCatalog.filter(t => t.toggleable)`. No hardcoded tool-key list in either component — a future always-on tool lands in the section without frontend edits.
- **D6 — Section title is a pane constant.** The pane labels the always-on section "Channel Tool" (the user-locked name; catalog group key stays `channel`). Generalizing group→header rendering for all groups is deliberately out of scope (D2 of the locked decisions).
- **D7 — Icons: add the three glyphs to `Icon.tsx`, keys unchanged.** `message` (speech bubble), `history` (clock with counterclockwise arrow), `check-circle` (circled check) drawn in the existing 24×24 / stroke-1.8 style. Keys stay as the Go catalog already declares them — no backend rename, no key remap to existing glyphs.
- **D8 — Static mirror gains the three.** `builtinToolNames` / `builtinToolIcons` in `toolCatalog.ts` add `channel.post`, `channel.history`, `session.close` so transcript cards resolve name + icon before the catalog fetch. The `CATALOG_TOOL_KEYS` sentence-table guard is untouched (the three were never in it — deferred non-goal).

## Risks / Trade-offs

- [422 on previously-accepted PATCHes breaks any client that toggled these keys] → Only consumer is our UI, which stops offering the control; smoke coverage updated in the same change.
- [`AlwaysOn` drift between catalog and the `without*Tools` strip lists] → The strip lists already name exactly these three keys; a catalog test pins that every `AlwaysOn` entry is a member of `ChannelToolNames ∪ SessionToolNames`, so adding a fourth always-on tool forces a conscious decision.
- [Stale `enabled=false` rows persist forever in `workspace_tool_settings`] → Inert by D2/D3; accepted (no migration), documented in the proposal.
- [Agent `tools` arrays carrying dead channel keys] → Harmless (runtime strips/scopes them); spec pins the "kept invisibly" behavior so a future cleanup is a conscious change, not an accident.

## Migration Plan

No schema migration. Deploy order is irrelevant: the backend flag + gate are self-contained, and older UI builds degrade gracefully (they'd still show the three with toggles, but patching now 422s — surfaced as an error toast by the existing handler). Rollback is a plain revert.

## Open Questions

None — the PATCH rejection wording is the only unpinned copy, and it lives in the 422 detail message where exact phrasing is not user-contractual.
