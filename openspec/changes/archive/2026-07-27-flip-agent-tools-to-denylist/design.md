## Context

Per-agent tool selection is an **allowlist** today: `agents.tools` holds a comma-separated list of
tool names, and `internal/agent/agent.go buildTools` keeps only those tools (plus any that pass the
global `tool_registry.enabled` gate). An empty list means "all globally-enabled tools." The
`master` agent carried a non-empty, curated allowlist that predated the memory tools, so
`memory_search` was silently dropped and the model got `tool memory_search not found in toolsNode`
when the persona told it to search memory.

The two selection layers in `buildTools`:

1. **Global:** `tool_registry.enabled` (seeded from the builtin registry on startup, default on) —
   the `enabledChecker` drops globally-disabled tools.
2. **Per-agent:** `agents.tools` allowlist — `if AgentConf.Tools != ""` keeps only listed names.

Layer 1 is fine. Layer 2 is the staleness source: every new tool requires every curated agent
allowlist to be re-edited, or the tool is invisible to that agent.

## Goals / Non-Goals

**Goals:**

- Invert per-agent selection from allowlist to **denylist** (`agents.disabled_tools`): effective set
  = `(globally-enabled builtin tools) MINUS (agent.disabled_tools)`.
- Make "all tools enabled" the durable default so new tools added in a future version are
  automatically available to existing agents with no manual enabling.
- Preserve the existing global `tool_registry.enabled` gate and its composition with per-agent
  selection.
- Rename the field consistently through the stack (column, store struct, service DTO, API, CLI flag,
  web UI).

**Non-Goals:**

- Changing the global `tool_registry.enabled` model or the management UI for it (the top-level Tools
  view is unaffected).
- Per-category or per-tool-group enablement (out of scope; the denylist is per individual tool name).
- Migrating legacy allowlists into equivalent denylists (we clear them — see D2).
- Tool versioning / migration of *which* tools exist (handled by the existing startup seed).

## Decisions

### D1 — Denylist, not allowlist; field `disabled_tools`
`agents.disabled_tools` (comma-separated tool names) is subtracted from the globally-enabled set.
Empty = all globally-enabled tools. This is the structural fix for staleness: a tool added in a new
version is registry-enabled by the startup seed and immediately available to every agent unless
explicitly listed in that agent's `disabled_tools`. *Alternative considered:* keep the `tools` column
name but invert its meaning — rejected as confusing (a column named `tools` holding a disabled list).
*Alternative considered:* keep allowlist but auto-extend it on startup — rejected; it still requires
write-on-startup and races with user curation.

### D2 — Hard-cutover migration: clear legacy allowlists
On migration, rename `agents.tools` → `agents.disabled_tools` and **clear all existing values**
(every agent becomes fully enabled). Rationale: the user's stated intent is default-all, and the only
existing agent (`master`) is already empty. *Alternative rejected:* compute
`disabled_tools = (all current tools) − (legacy allowlist)` per agent to preserve "these were off."
It is fragile (depends on enumerating the full tool universe at migration time) and — critically —
it preserves the exact staleness this change removes for any tool added after migration. Given the
goal, a clean clear is simpler and more honest.

### D3 — Two-layer model preserved; global gate composes
`buildTools` continues to apply the global `enabledChecker` first (drop registry-disabled tools),
then the per-agent denylist (drop `disabled_tools`). Effective set =
`builtin_enabled − disabled_tools`. No change to `tool_registry`, its seed, or the middleware-toggle
behavior for filesystem/shell tools.

### D4 — Rename through the stack (BREAKING, intentionally)
`store.Agent.Tools` → `DisabledTools`; `service.AgentInput.Tools`/`AgentView.Tools` →
`DisabledTools`; SQLite column `agents.tools` → `agents.disabled_tools`; CLI flag `--tools` →
`--disabled-tools` on `agent add`/`edit`; `agent show` prints `Disabled Tools`. The web Tools tab
flips from an allowlist picker to a denylist picker. A clean, single rename is preferred over
back-compat aliases (YAGNI on a single-binary on-device app with one existing agent).

### D5 — Empty denylist is the default for new agents
`getOrSeedMasterAgent` (`internal/cli/context.go`) and `onclaw agent add` already create agents with
no `tools` field, so they already get all tools; after the rename they simply carry an empty
`disabled_tools`. No change to first-run behavior — only the field name.

### D6 — Web UI: opt-out, render empty as "all tools enabled"
The per-agent Tools tab shows every globally-enabled tool with the ability to **disable** individual
tools (the inverse of today's opt-in checkboxes). An empty `disabled_tools` renders as a clear
"all tools enabled" state. This is the surface that produced `master`'s stale list, so the UI flip is
what actually prevents recurrence — a user saving the tab no longer writes an allowlist that omits
future tools.

## Risks / Trade-offs

- **[BREAKING] API/CLI field rename** → Mitigation: single existing agent, single-binary app; default
  name in the change and migration. External API clients reading `tools` must switch to
  `disabled_tools`.
- **[BREAKING] Legacy allowlists are cleared** → Mitigation: default-all is the intended state; the
  change is documented; a user who genuinely wanted a tool off can re-add it to `disabled_tools`.
- **A user disabling a tool that a later version makes load-bearing** → Mitigation: the global
  `tool_registry` and the persona already assume tool availability; disabling is an explicit operator
  choice. The denylist is per-agent, so it never affects other agents.
- **Forgetting to flip one layer (store/DTO/UI/CLI)** → Mitigation: compile-time field rename catches
  Go layers; an end-to-end test (set `disabled_tools`, assert the tool is withheld) guards the
  full path.

## Migration Plan

1. Schema migration: `ALTER TABLE agents RENAME COLUMN tools TO disabled_tools;` (SQLite ≥ 3.25
   supports `RENAME COLUMN`), then `UPDATE agents SET disabled_tools = '';` to clear legacy
   allowlists. (If the minimum SQLite is older, the fallback is add-new-column / copy-as-empty /
   drop-old, handled in `db.go` migrations alongside the existing pattern.)
2. Code: field/flag rename + `buildTools` filter inversion land in the same release.
3. Deploy is a code + schema update (single binary). On startup the migration runs once.
4. Rollback: revert the code; the `disabled_tools` column is harmless to old code (it reads
   `tools`, which no longer exists → old code fails to select). Because this is a single-binary
   on-device app with one agent, rollback means restoring the prior binary **and** the prior DB
   (or running the inverse rename). Acceptable given the small blast radius; call out in the change.

## Open Questions

- Naming: `disabled_tools` vs `excluded_tools`. `disabled_tools` reads naturally alongside
  `tool_registry.enabled`, though the two are different layers (per-agent exclusion vs global
  enable). Defaulting to `disabled_tools`; trivially adjustable before implementation.
- Whether to also surface "all tools enabled" as an explicit, named default state in the management
  API (e.g. a computed flag) rather than only "empty list." Deferred — empty-list-as-all is already
  the established convention.
