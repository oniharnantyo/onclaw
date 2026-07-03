## Why

onclaw's builtin tools (`shell`, `read_file`, `write_file`, `list_dir`, and forthcoming
browser tools) have no management surface. The only way to restrict tools today is a
per-agent CSV allowlist (`agentConf.Tools`, edited via CLI) — there is no global on/off, no
grouping, and no UI. Newly configurable tools (the browser engine) have nowhere to live
their settings: onclaw persists runtime web config to SQLite (MCP servers, hooks), not to
`.env`, so a tool-category config needs its own table and editor. Users managing onclaw via
the Web UI cannot see, enable, disable, or configure tools at all.

## What Changes

- **Tool categories:** add a `Category() string` method to the builtin `Tool` interface and
  categorize every builtin (`shell`→`Shell`, `read_file`/`write_file`/`list_dir`→`Filesystem`;
  browser tools→`Browser`, added by `add-browser-tool`). The management surface groups tools
  by category.
- **Configurable-category registry:** a package-level `ConfigRegistry` where a category
  registers a JSON config schema plus load/save hooks. A category appears with a "Config"
  button only if it has registered configuration. (Browser registers here.)
- **Global enable/disable:** a new `tool_registry` table (name, category, enabled) seeded
  idempotently from the builtin registry on startup (default `enabled=1`). Tool assembly
  changes from `Builtin(scope)` to `Builtin(scope, enabled EnabledChecker)` and excludes
  globally-disabled tools **before** the existing per-agent allowlist filter — the effective
  tool set is `global-enabled ∩ per-agent allowlist`.
- **Per-category config storage:** a new `tool_group_config` table (category, config JSON).
  This is the source of truth for configurable categories (e.g. the browser engine, bin,
  port, headless), edited via the UI — consistent with how MCP/hook config lives in SQLite,
  not `.env`.
- **REST API:** `GET /api/tools` (tools grouped by category + which categories are
  configurable), `POST /api/tools/{name}/toggle`, `GET /api/tools/categories/{cat}/config`,
  `PUT /api/tools/categories/{cat}/config` — all auth-required, mirroring the MCP/hook
  handler pattern.
- **Web UI:** a Tools view (`web/src/components/Tools.tsx`) listing builtin tools grouped by
  category, a toggle per tool, and a "Config" button beside configurable category headers
  (opens a dialog). Wired as a new `tools` tab in `App.tsx`.
- **Opt-in:** with every tool enabled (the default) and no category config edited, agent
  behavior is identical to today.

## Capabilities

### New Capabilities

- `tools-management`: a management surface for builtin tools — categorization, global
  enable/disable backed by `tool_registry`, per-category configuration backed by
  `tool_group_config` with a registered-schema registry, the intersecting assembly filter,
  the REST API, and the Web UI.

### Modified Capabilities

<!-- None at the spec-requirement level. The change adds Category() to the Tool interface
and a parameter to the tool factory, and integrates with the Web UI, but the existing
agent-tools requirements (workspace confinement, shell policy, redaction) are unchanged. -->

## Impact

- **New files:** `internal/api/handler/tools.go`, `internal/api/service/tools.go` (+ DTOs in
  `internal/api/service/types.go`); `internal/store/sqlite/tool_registry.go`,
  `internal/store/sqlite/tool_group_config.go` (+ interfaces in `internal/store/store.go`,
  DTOs in `internal/store/types.go`); `web/src/components/Tools.tsx`.
- **Modified files:** `internal/agent/tools/tools.go` (`Category()` on `Tool`; config handle
  on `Scope`), `internal/agent/tools/registry.go` (`Builtin(scope, enabled)` + `ConfigRegistry`),
  `internal/agent/agent.go` (build `EnabledChecker`, apply global-enable filter before the
  per-agent allowlist), `internal/store/sqlite/db.go` (migration + idempotent seeding),
  `internal/api/routes.go` (4 routes), `web/src/App.tsx` (wire `tools` tab).
- **New dependency:** none.
- **Database migration:** adds `tool_registry` and `tool_group_config` tables; seeds
  `tool_registry` from the builtin registry (default enabled).
- **Out of scope:** MCP-server tools in the Tools view (MCP keeps its own page);
  per-category bulk enable/disable UI (toggles are per-tool in v1); `.env` provisioning of
  tool config (DB is the source of truth, matching MCP/hooks).
