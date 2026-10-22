# Design

## Context

Model capability data lives in a static Go map (`internal/agents/catalog.go`:
`modelSpec` per provider type) and `SettingsService.ProviderModels` computes
availability per call — static catalog for the three canonical types, a live
`ListModels` fetch for the three live-listed types, an empty list on fetch
failure. Nothing about models is persisted. The shell swaps SettingsView in
place of rail + header (`App.tsx` `inSettings` branch); agents are a card grid
whose editor is `AgentConfigModal` (five tabs, mounted as a modal). SQLite
migrations are forward-only, currently at 000009; schema-version tests pin
the migration count. The window's top 25px is the native invisible titlebar
drag band — island columns clear it with top padding, and nothing interactive
may sit in it.

## Goals / Non-Goals

**Goals**

- One shell layout for every menu: rail always visible, settings in the island.
- Provider model lists as stored, user-authored data with a store-first
  capability path for the runtime.
- Master-detail for Providers (inside settings) and Agents (agents menu),
  reusing the established island-column grammar.

**Non-Goals**

- No new provider types; decision/routing providers stay out of scope.
- No new model metadata beyond context window, image/PDF input, enabled.
  Effort options stay derived from the model-id heuristic — the donor UI has
  no effort field either.
- No remote catalog sync (no models.dev-style service); "refresh" means the
  existing `ListModels` fetch or the built-in catalog.
- The create-agent wizard stays a modal; only the *edit* surface moves.
- No drag-resize on the new list columns (the SessionRail/RightSidebar
  pattern can be applied later if wanted).

## Decisions

**D1 — Settings joins the standard shell.** Delete the `inSettings` special
case: SettingsView mounts inside the content `main` island like AgentsView,
keeping its own pane nav as the island's left column. The Back header and the
dedicated settings strip row are deleted; the strip overlay gains a Settings
title like the agents view's. Rail state (active row) already follows the nav
store. *Alternative:* keep full-bleed and only restyle panes — rejected, the
persistent rail is the point of the change.

**D2 — `provider_models` table.** One row per (provider, model):
`id` (store-assigned), `provider_id` FK `ON DELETE CASCADE`,
`model_id TEXT`, `context_window INT`, `image_input BOOL`, `pdf_input BOOL`,
`enabled BOOL` (default on), timestamps, `UNIQUE(provider_id, model_id)`.
New `ProviderModelStore` interface in `internal/store` with sqlite and fake
implementations (the fake store is part of the repo's test contract).
Service DTO mirrors `ModelCapability` plus `Enabled` and the row id.

**D3 — Seeding runs in Go, not SQL.** Migration 000010 only creates the
table. The built-in catalog is Go code; hardcoding its rows into SQL would
duplicate it. A startup seed sweep (same pattern as the prompt-status sweep)
inserts, for every provider of a canonical type, catalog rows the provider
does not have yet — preserving existing rows and toggles — and the provider
create path seeds canonical types the same way. Schema-version tests update
to the new count in the same change. *Alternative:* SQL-literal backfill —
rejected (two sources of truth for the catalog).

**D4 — Store-first capability resolver.** New resolver in `internal/agents`
that resolves (providerID, modelID) through a store-backed lookup first,
then the static catalog (by provider type), then today's defaults
(`DefaultContextWindow`, id-heuristic efforts, `false` inputs). Turn-scoped
call sites — context accounting, effort validation, attachment
image/PDF degradation — take the resolver; the store is already reachable
from the runtime compose path. The static package functions remain only as
the resolver's fallback tier and the seeding source; no caller resolves
capabilities around it. *Alternative:* keep static lookups and patch each
call site — rejected, the fallback ordering must not drift between surfaces.

**D5 — Refresh adds, never destroys.** Refresh on a live-listed provider
runs the existing `ListModels` and inserts stored rows for ids the list
lacks (enabled, default window, heuristic efforts); on canonical types it
re-runs the catalog seed. Nothing is deleted, disabled, or overwritten —
user-created rows and all enable states survive, and a failed fetch leaves
the list untouched. This is deliberately narrower than "sync": predictability
beats completeness for hand-curated lists.

**D6 — The stored list is the only model door.** Agent-config and composer
pickers list the provider's *enabled* stored rows (`ProviderModels` becomes a
store read, no live fetch). The agent-level "Custom model ID…" free-form
entry retires — a model must exist on its provider's list first, which is
what makes the badges and runtime resolution trustworthy. A turn or override
referencing a disabled/removed model fails with the existing clear-error
path (the agent-runtime spec already permits rejecting unknown models).
*Alternative:* union of stored + computed (weaker) or free-form pass-through
(badges would lie) — both rejected. *Upgrade note:* an agent on a
live-listed provider whose model was never stored fails after upgrade with
a clear error until the user refreshes or adds the row; the per-turn error
names the provider, so the fix is one click away.

**D7 — Provider detail view replaces both dialog modes.** The detail view
holds a draft (name, Base URL, type disabled on edit, key blank-keeps) with
an explicit Save; the header carries brand tile, name, type label, the
enable toggle, verify, and delete (existing two-step + in-use-confirm flows
unchanged). "Add provider" renders the same detail layout as a create form
(type enabled, draft verification) — `ProviderFormDialog` retires and its
type-catalog field logic ports into the form. The list column groups entries
under "Providers" (canonical types) and "Custom providers" (live-listed
types) like the donor page; status dot = green enabled-with-key, amber
disabled or keyless.

**D8 — Agent detail pane is a re-mount, not a rewrite.** `AgentConfigModal`'s
content (tab strip, sections, save/delete logic) moves into
`AgentConfigPane` with the same props surface; only the mount shell changes
(modal → detail column). The model select drops the custom-id option (D6).
List entries carry avatar, name, model line, prompt status with retry, and
the chat affordance; selection auto-picks the first agent on load. Column
widths are fixed (providers list ~w-64, agents list ~w-72).

## Risks / Trade-offs

- [Resolver fallback returns a wrong context window for a custom model] → the
  resolver is the single resolution path (D4) with table tests over all three
  tiers; the ContextMeter reads the same resolver, so UI and runtime cannot
  disagree.
- [Upgrade strands agents on live-listed providers whose model was never
  stored] → accepted break (D6), clear per-turn error naming the provider,
  refresh/add recovers in one step; recorded in the tasks' migration notes.
- [42KB modal re-mount regresses editor behavior] → content and props stay
  intact (D8); existing editor tests re-target the pane, not rewritten.
- [Catalog code and stored rows drift apart on later catalog edits] →
  canonical re-seed on refresh (D5) reconciles; stored user edits always win.
- [Migration count pin tests break] → expected; updated in the same change
  (D3), as they were for 000006–000009.

## Migration Plan

Forward-only 000010 (table) plus the Go startup seed; no rollback path by
policy. After `task build`, regenerate Wails bindings (recurring repo
gotcha). Seeding is idempotent and safe to re-run; it never touches
live-listed providers (no network during startup).

## Open Questions

None — the deferred choices (column resize, list grouping polish) are
recorded as non-goals.
