## Context

onclaw's agent assembles its tools in `AssembleAgent` (`internal/agent/agent.go`):
`tools.Builtin(&tools.Scope{...})` iterates a package-level `registry` of `Tool` values
(`internal/agent/tools/registry.go`) and wraps each in the redaction decorator. The only
existing restriction is a per-agent CSV allowlist (`agentConf.Tools`, filtered at
`agent.go:73-89`). There is no global enable/disable, no category metadata on `Tool`
(the interface is just `Name/Desc/Build`), and no Web UI for tools.

Runtime web configuration in onclaw is persisted to SQLite dedicated tables — MCP servers
(`mcp_servers`), hooks (`agent_hooks`) — never to `.env`, which Viper reads once at startup
and treats as read-only thereafter (`internal/config/config.go`). The Web UI follows a fixed
pattern: a `*.tsx` component (e.g. `MCP.tsx`, `Hooks.tsx`) using local `useState` +
`fetch()`, `@tanstack/react-table`, Phosphor toggle icons, and `.modal-*` dialogs, wired as
a tab in `App.tsx`; the API is `net/http` + `http.ServeMux` with `auth.RequireAuth`
(`internal/api/routes.go`, `handler/mcp.go`, `service/mcp.go`).

## Goals / Non-Goals

**Goals:**
- Let users enable/disable any builtin tool globally from the Web UI, mirroring how MCP
  servers toggle.
- Group tools by category and surface a per-category Config editor for categories that have
  configuration (browser being the first consumer, via `add-browser-tool`).
- Persist tool enable state and category config in SQLite, consistent with MCP/hooks.
- Preserve existing behavior when nothing is toggled (opt-in).

**Non-Goals:**
- Managing MCP-server tools here (MCP has its own page).
- Replacing the per-agent CSV allowlist (it remains, intersected with the global enable).
- Provisioning tool config via `.env` (DB is the source of truth).
- Per-category bulk toggles in v1 (each tool toggles individually).

## Decisions

### D1 — Global master list, not per-agent
The Tools page toggles a tool system-wide via `tool_registry.enabled`, not by editing each
agent's `Tools` CSV. Rationale: matches the MCP-server toggle mental model, gives one master
view, and composes cleanly with the existing per-agent allowlist (the effective set is the
intersection). The per-agent allowlist stays for agent-specific narrowing.

_Alternatives considered:_ per-agent toggles only (rejected — no single master view, and the
user-facing request is a global "manage our tools"); replacing the allowlist entirely
(rejected — additive is safer and preserves existing agent configs).

### D2 — SQLite source of truth for tool config, not `.env`
Category config (e.g. browser engine/bin/port/headless) lives in `tool_group_config`
(JSON per category), edited via the UI. This follows the MCP/hooks precedent exactly;
`.env` is read-only post-startup and cannot be the runtime-mutable source. Code defaults
apply when a category's row is absent.

### D3 — `Category()` on `Tool` + a separate `ConfigRegistry`
Every builtin declares a category via a new `Category() string` method (small, required,
one-liner per tool). Configuration is per-category, not per-tool, so config-schema methods
are NOT bolted onto `Tool`; instead a package-level `ConfigRegistry` lets a category
register `{schema, load, save}`. The UI shows a Config button only for categories present in
`ConfigRegistry`. This keeps `Tool` minimal and makes "configurable" opt-in per category.

### D4 — `Builtin(scope, enabled)` with an `EnabledChecker` interface in the `tools` package
To filter globally-disabled tools without `tools` importing `internal/store` (layering), the
`tools` package defines `type EnabledChecker interface { Enabled(name string) bool }` and
`Builtin` takes one. `agent.go` wraps the `ToolRegistryStore` as the checker. The
global-enable filter runs before the existing per-agent allowlist filter, so the pipeline is:
`registry → redaction wrap → global-enabled filter → per-agent allowlist`.

### D5 — ConfigRegistry-driven Config buttons
The Tools UI calls `GET /api/tools`, whose response marks each category `configurable: true`
iff it is registered in `ConfigRegistry`. The category header renders a Config button only
when `configurable`. The dialog reads/writes `tool_group_config` via the category-config
endpoints. No category is hard-coded in the UI.

### D6 — Built-in tools only in v1
The Tools view lists only builtin tools. MCP-server tools remain on the MCP page; mixing
them in would duplicate MCP toggle/config logic and couple the two views.

### D7 — Idempotent seeding of `tool_registry`
On startup (migration), every builtin tool is inserted into `tool_registry` if absent, with
`enabled=1`. Existing rows are never overwritten, so user toggles survive restarts. New
builtins added later appear enabled by default.

### D8 — `Scope` carries the config store handle for consumers
The browser Manager (added by `add-browser-tool`) reads its config from
`tool_group_config["browser"]`. Rather than thread a store through every tool's `Build`,
`Scope` gains a `ToolGroupCfg` handle, consistent with `Scope`'s existing grab-bag role
(`Workspace`, `ShellPolicy`, `ShellAllowlist`). Non-browser tools ignore it.

## Risks / Trade-offs

- **[Per-tool granularity for multi-tool categories]** Browser exposes 11 tools; toggling
  each individually is verbose. → accepted for v1 (matches the "each tool enabled/disabled"
  request); a category-level "disable all" convenience can follow.
- **[Scope growth]** `Scope` gains a store handle; it is no longer purely config-scalars.
  → consistent with its existing role; documented.
- **[Two sources of enable truth]** global `tool_registry` and per-agent `Tools`. → the
  intersection is well-defined and documented; both remain useful.
- **[Config drift]** a category's registered schema vs. stored JSON. → schema validated on
  PUT; unknown keys ignored.

## Migration Plan

Greenfield tables — no data migration. `Migrate()` adds `tool_registry` and
`tool_group_config`, then seeds `tool_registry` from the builtin registry (default enabled).
Existing agents' `Tools` CSV allowlists are untouched and continue to intersect with the new
global enable. With all tools enabled (default), every agent behaves exactly as before.

## Open Questions

- Per-tool vs per-category disable granularity → **decided: per-tool in v1**; category
  convenience deferred.
- MCP tools in the Tools view → **decided: no; built-in tools only.**
- `.env` provisioning of tool config → **decided: no; DB source of truth.**
