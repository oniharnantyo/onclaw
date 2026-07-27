## Why

Per-agent tool selection is an **allowlist** (`agents.tools`): an empty list means "all enabled
tools," but a non-empty list is a fixed allowlist that goes stale the moment a new version adds a
tool — the agent must be manually re-edited to gain the new tool. This just broke the `master`
agent: its curated allowlist predated the memory tools, so `memory_search` was silently unavailable
(the model called it and got `tool memory_search not found in toolsNode`) until the whitelist was
cleared by hand. A **denylist** inverts the default: every new tool is on by default, and operators
only ever opt *out* of tools they explicitly want off. New versions then "just work" for existing
agents with no manual enabling.

## What Changes

- **BREAKING:** Rename the `agents.tools` allowlist column to `agents.disabled_tools` (a denylist).
  Tool assembly SHALL offer `(globally-enabled builtin tools) MINUS (the agent's disabled_tools)`,
  instead of `(globally-enabled) INTERSECT (allowlist)`.
- **BREAKING migration:** existing non-empty `agents.tools` values SHALL be cleared on migration
  (every agent becomes fully enabled). Carrying them forward as denylists would preserve exactly the
  staleness this change removes; default-all is the intended post-migration state. (Rationale and the
  rejected compute-on-migration alternative are in design.md.)
- Invert the filter in `internal/agent/agent.go buildTools`: drop tools whose name is in
  `disabled_tools`, rather than keeping only allowlisted names.
- Rename the field through the stack: `store.Agent.Tools` → `DisabledTools`;
  service DTO `AgentInput`/`AgentView.Tools` → `DisabledTools`; SQLite CRUD on the `agents` table.
- **BREAKING CLI:** rename the `agent add`/`agent edit` `--tools` flag to `--disabled-tools`;
  `agent show` prints `Disabled Tools`.
- Web UI: flip the per-agent Tools tab from "check the tools you want" to "all on; uncheck the ones
  you don't." An empty denylist renders as "all tools enabled."
- An empty `disabled_tools` (the default for new agents and the first-run `master` seed) yields all
  globally-enabled builtin tools — identical to today's empty-allowlist behavior, but no longer
  fragile to new tools being added.

## Capabilities

### New Capabilities

_(none — this inverts an existing selection model rather than introducing a new one.)_

### Modified Capabilities

- `tools-management`: the "Global tool enable/disable" requirement changes from an allowlist model
  ("apply the agent's per-tool allowlist"; "a non-empty allowlist SHALL restrict the agent to the
  intersection of globally-enabled tools and the allowlisted names") to a denylist model
  ("exclude the agent's `disabled_tools`"; an empty denylist offers all globally-enabled tools). The
  global `tool_registry.enabled` gate is unchanged and composes with the denylist.
- `web-ui`: the per-agent tool-selection UI requirement changes from an allowlist picker to a
  denylist picker. An empty denylist renders as "all tools enabled"; the user opts tools **out**
  rather than **in**, so newly added tools appear automatically until explicitly disabled.

## Impact

- **Store/schema:** `agents` table column rename `tools` → `disabled_tools`, with a migration that
  clears legacy allowlist values. `internal/store/sqlite/agent.go` (CRUD) and `internal/store/types.go`
  (struct field).
- **Agent assembly:** `internal/agent/agent.go` `buildTools` filter inversion (~5 lines).
- **API/service:** `internal/api/service/types.go` + `agent.go` DTO field rename across
  `GET`/`PUT /api/agents`.
- **CLI:** `internal/cli/agent_cmd.go` — `--disabled-tools` flag on `add`/`edit`, "Disabled Tools"
  label on `show`.
- **Web UI:** agent-detail Tools tab in `web/src/pages/AgentDetailPage.tsx`.
- **Tests:** store migration (clears legacy allowlist), agent-assembly filter (denylist excludes;
  empty = all), API DTO round-trip, CLI flag, and the flipped web tab — all updated/added.
- **Compatibility:** breaking for any agent row or API client relying on the `tools` allowlist field;
  mitigated by the default-all migration and called out in the change.
