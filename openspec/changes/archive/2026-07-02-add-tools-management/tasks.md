# Tasks

## 1. Tool metadata & config registry

- [x] 1.1 Add `Category() string` to the `Tool` interface (`internal/agent/tools/tools.go`); set categories on builtins (`shell`→`Shell`, `read_file`/`write_file`/`list_dir`→`Filesystem`).
- [x] 1.2 Add a package-level `ConfigRegistry` (`internal/agent/tools/config.go`): `RegisterConfig(category, jsonSchema, load, save)`, `IsConfigurable(category)`, `ConfigurableCategories()`.
- [x] 1.3 Add a `ToolGroupCfg` handle to `Scope` (for configurable tools to read category config); leave existing fields untouched.
- [x] 1.4 Unit tests: every builtin returns a non-empty category; ConfigRegistry register/query.

## 2. Storage layer

- [x] 2.1 Add `ToolRegistry` and `ToolGroupConfig` DTOs to `internal/store/types.go`.
- [x] 2.2 Add `ToolRegistryStore` (List/Upsert/Toggle/Get) and `ToolGroupConfigStore` (Get/Put) interfaces to `internal/store/store.go`.
- [x] 2.3 Implement `sqliteToolRegistryStore` + `sqliteToolGroupConfigStore` in `internal/store/sqlite/tool_registry.go` and `tool_group_config.go`, mirroring `mcp_server.go`.
- [x] 2.4 Add `tool_registry` and `tool_group_config` tables (idempotent `CREATE TABLE IF NOT EXISTS`) to `Migrate()`; seed `tool_registry` from the builtin registry (insert-if-absent, default `enabled=1`).
- [x] 2.5 Store tests: seeding idempotency, toggle persists, config JSON round-trip, unknown-category config.

## 3. Assembly wiring

- [x] 3.1 Define `EnabledChecker` in `internal/agent/tools`; change `Builtin(scope)` → `Builtin(scope, enabled EnabledChecker)`.
- [x] 3.2 In `AssembleAgent` (`internal/agent/agent.go`): build an `EnabledChecker` over the `ToolRegistryStore`; apply the global-enable filter before the existing per-agent allowlist filter (`agent.go:73-89`).
- [x] 3.3 Tests: a globally-disabled tool is excluded; re-enabling restores it; intersection with per-agent allowlist holds.

## 4. REST API

- [x] 4.1 Add DTOs to `internal/api/service/types.go` (`ToolView{name, category, enabled}`, category-config get/put).
- [x] 4.2 Implement `internal/api/service/tools.go` + `internal/api/handler/tools.go`: `GET /api/tools` (grouped by category + `configurable` flags), `POST /api/tools/{name}/toggle`, `GET /api/tools/categories/{cat}/config`, `PUT /api/tools/categories/{cat}/config` — model on `handler/mcp.go`.
- [x] 4.3 Register the 4 routes in `internal/api/routes.go` (wrapped in `auth.RequireAuth`).
- [x] 4.4 API tests: list grouping/configurable flags, toggle persists, category config GET/PUT, 404 for unknown category, auth required.

## 5. Web UI

- [x] 5.1 Implement `web/src/components/Tools.tsx` (clone `MCP.tsx` patterns): `@tanstack/react-table` grouped by category, Phosphor `ToggleRight/ToggleLeft` per tool, `.modal-*` config dialog, local `useState` + `fetch()`, `showToast`.
- [x] 5.2 Category header renders a "Config" button only when `configurable`; dialog reads/writes the category-config endpoints.
- [x] 5.3 Wire the `tools` tab in `web/src/App.tsx` (`Tab` type, nav item, `HEADER_TITLES`, render clause); rebuild bundled assets.

## 6. Verification

- [x] 6.1 `CGO_ENABLED=0 go build ./...`; `go vet`/`gofmt` clean; `make build-all`.
- [x] 6.2 `go test ./internal/agent/tools/... ./internal/store/sqlite/... ./internal/api/...`.
- [x] 6.3 Web build + typecheck clean.
- [x] 6.4 Manual: toggle a tool off in the UI → it disappears from an agent's tool list; toggle on → returns; edit a configurable category → persists; per-agent allowlist still intersects.
- [x] 6.5 `openspec validate` (or `openspec validate --changes add-tools-management`) passes.
