# Tasks

## 1. Schema + store: rename `tools` → `disabled_tools` (denylist)

- [ ] 1.1 In `internal/store/sqlite/db.go`, rename the `tools` column to `disabled_tools` in the `agents` CREATE TABLE statement (fresh-DB path)
- [ ] 1.2 Add a migration in `db.go` (alongside the existing migration pattern) for existing DBs: `ALTER TABLE agents RENAME COLUMN tools TO disabled_tools;` then `UPDATE agents SET disabled_tools = '';` (hard-cutover clear of legacy allowlists, per design D2). If the supported SQLite is older than 3.25, use the add-new-column / copy-as-empty / drop-old fallback
- [ ] 1.3 TDD the migration in `internal/store/sqlite/agent_test.go` (or `db_test.go`): open a DB seeded with a legacy non-empty `tools` row, run migrations, assert the column is `disabled_tools` and the value is empty (all tools enabled)
- [ ] 1.4 Rename `Agent.Tools` → `Agent.DisabledTools` in `internal/store/types.go`
- [ ] 1.5 Update `internal/store/sqlite/agent.go` CRUD (INSERT/UPDATE/SELECT column list + Scan) to `disabled_tools` / `DisabledTools`
- [ ] 1.6 `rtk go build ./...` and `rtk go test ./internal/store/sqlite/...` pass

## 2. Agent assembly: invert the per-agent filter to a denylist

- [ ] 2.1 In `internal/agent/agent.go buildTools`, replace the allowlist filter (the `if AgentConf.Tools != ""` block that keeps only allowlisted names) with a denylist filter: build a set from `AgentConf.DisabledTools` and drop tools whose `info.Name` is in it
- [ ] 2.2 TDD in `internal/agent/agent_test.go`: (a) an agent with `DisabledTools="memory_search"` does NOT receive `memory_search` but DOES receive other enabled tools; (b) an agent with empty `DisabledTools` receives all enabled tools, including a newly-registered tool that was never known when the denylist was curated; (c) a globally-disabled tool (`tool_registry.enabled=0`) is still withheld regardless of the denylist
- [ ] 2.3 `rtk go test ./internal/agent/...` passes

## 3. API/service: DTO field rename

- [ ] 3.1 Rename `Tools` → `DisabledTools` in `AgentInput` and `AgentView` (`internal/api/service/types.go`); update the JSON tag to `disabled_tools`
- [ ] 3.2 Update the mapping in `internal/api/service/agent.go` (CreateAgent/UpdateAgent/ToView) to read/write `DisabledTools`
- [ ] 3.3 TDD in `internal/api/...`: GET/PUT `/api/agents/:name` round-trips `disabled_tools`; a PUT with `disabled_tools=["execute"]` persists and a subsequent GET returns it
- [ ] 3.4 `rtk go test ./internal/api/...` passes

## 4. CLI: `--disabled-tools` flag

- [ ] 4.1 In `internal/cli/agent_cmd.go`, rename the `--tools` flag to `--disabled-tools` on `agent add` and `agent edit`; wire it to `DisabledTools`
- [ ] 4.2 Update `agent show` output from "Tools Allowed" to "Disabled Tools", printing the denylist (or "(all enabled)" when empty)
- [ ] 4.3 Grep-verify no remaining `--tools` / `Tools Allowed` / `.Tools` references for the agent selection concept: `rtk grep -rn -e "--tools" -e "Tools Allowed" -e "AgentConf.Tools" -e "\.Tools" internal/cli/ internal/agent/ internal/api/ internal/store/` (only unrelated `.Tools` hits remain)
- [ ] 4.4 `rtk go build ./...` and `rtk go test ./internal/cli/...` pass

## 5. Web UI: flip the per-agent Tools tab to opt-out

- [ ] 5.1 In `web/src/pages/AgentDetailPage.tsx`, change the Tools tab from opt-in selection (check tools you want) to opt-out (all on; toggle tools off), backed by the agent's `disabled_tools`
- [ ] 5.2 Render an empty `disabled_tools` as an explicit "all tools enabled" state; toggling a tool off adds it to `disabled_tools`, toggling back on removes it
- [ ] 5.3 Update the API client/types in `web/src` so the agent DTO uses `disabled_tools` (no stale `tools` field)
- [ ] 5.4 Verify: load an agent, disable one tool, save, reload — the disabled tool persists; an agent with no edits shows "all tools enabled"
- [ ] 5.5 `rtk tsc` (or the repo's web build/lint) passes

## 6. Verification

- [ ] 6.1 `rtk gofmt -w` on every touched Go file; `rtk gofmt -l` empty
- [ ] 6.2 `make build` (static, `CGO_ENABLED=0`); `make vet`
- [ ] 6.3 `rtk go test ./internal/store/sqlite/... ./internal/agent/... ./internal/api/... ./internal/cli/...` all pass, ≥70% coverage per package
- [ ] 6.4 End-to-end manual: with a fresh `master` (empty `disabled_tools`), `memory_search` is offered; add `memory_search` to `disabled_tools` via the UI/CLI, restart, and confirm it is withheld while other tools remain; remove it and confirm it returns without re-enabling anything else
- [ ] 6.5 Migration manual: take a DB with a legacy non-empty `agents.tools` row, start the new binary, confirm the column is renamed and cleared and the agent receives all tools
- [ ] 6.6 Confirm no stale allowlist references remain: `rtk grep -rn "allowlist" internal/ web/src/` returns only unrelated shell/env allowlist contexts
