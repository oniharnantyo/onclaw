# Tasks

## 1. Schema & domain

- [ ] 1.1 Migration `000070_agent_tools_denylist`: `ALTER TABLE agents ADD COLUMN disabled_tools text[] NOT NULL DEFAULT '{}', DROP COLUMN tools`; down is the exact inverse with an empty `tools`. Verify: `go run . migrate up` + `migrate down --steps 1` + `migrate up` against a scratch database.
- [ ] 1.2 Domain: replace `Agent.Tools []string` with `Agent.DisabledTools []string` (`json:"disabled_tools"`); update `internal/domain/agent.go` and compile dependents. Verify: `go build ./...`.
- [ ] 1.3 Stores: scan/args for `disabled_tools` in `internal/store/postgres/agents.go` and the fake (`internal/store/fake`); no fat-config structs — positional parameters per house convention. Verify: `go test ./internal/store/...` plus `-tags=integration` agents store tests.

## 2. API & handlers

- [ ] 2.1 Create/update handlers (`internal/server/handlers/agents.go`, `workspaces.go` starter agent): request field `disabled_tools` with nil → `{}`; PATCH replaces the stored denylist; a `tools` field in any payload is accepted and ignored. Verify: handler tests for create default, patch replace, and legacy `tools` ignored.
- [ ] 2.2 v1 narrowing (`internal/server/handlers/v1.go`): intersect request tool names with the agent's effective tool set (catalog minus denylist, workspace-gated) instead of the allowlist. Verify: v1 handler tests — request tools cannot extend the effective set; `tool_choice: "none"` still strips all tools.

## 3. Runtime resolution

- [ ] 3.1 Runner `resolve()` (`internal/agents/runner.go`): invert the tool stage — effective = catalog − `agent.DisabledTools`; per-turn `AllowedTools` override unchanged (explicit allowlist replaces denylist resolution when provided); channel/session scoping becomes un-scoping (context tools pulled out of the disabled set on those runs); workspace gate and context strips unchanged. Verify: runner tests — empty denylist exposes everything, denylisted name absent, unknown denylist name inert, gate wins, channel un-scoping.
- [ ] 3.2 Reserved names: `execute` (shell), `browser` facade (disables the whole set), `subagents`, `background_shell` default on; denylisting disables. Individual `browser.*` names disable individually. Verify: runner tests for facade disable cascade and individual browser names.
- [ ] 3.3 Filesystem middleware disable computation: middleware tools disabled by denylist presence (absent = attached), including the todo-exposure check (`agentExposesTodoTools` → denylist form). Verify: runner/agent unit tests.

## 4. Skills

- [ ] 4.1 Skills service (`internal/skills/service.go`): "enable everywhere" adds required tools to the workspace gate and removes them from every agent's `disabled_tools` in one transaction (replaces `hasAllTools`/`mergeTools`); unmet-dependency checks read the effective set. Verify: skills service tests — enable-everywhere satisfies tool dependencies on denylist-only agents.

## 5. Web

- [ ] 5.1 Types + API payload (`web/src/data/types.ts`, `web/src/lib/api.ts`): `disabled_tools` replaces `tools` on agent create/update/read. Verify: `pnpm test` type-level and api tests.
- [ ] 5.2 `AgentConfigModal` Step 3: chips render selected unless the key is in `disabled_tools`; toggle-off stores the key; Browser/Shell single-chip deselection writes `browser`/`execute`; workspace-disabled chips stay greyed; unmet skill-dependency warning reads the effective set. Verify: modal tests — default-selected chips, toggle-off stores denylist, workspace-disabled unselectable, unmet-dependency warning.

## 6. Fixtures & smoke

- [ ] 6.1 Eval seed fixtures (`internal/memory/eval/seed.go`): set `DisabledTools` instead of allowlists. Verify: `go test ./internal/memory/...`.
- [ ] 6.2 Smoke suite (`scripts/smoke.sh`, `scripts/v1smoke/`): agent create defaults to empty `disabled_tools`; patch replaces it; legacy `tools` payload ignored; v1 narrowing. Verify: `./scripts/smoke.sh` full pass.

## 7. Verification

- [ ] 7.1 Full gates: `go build ./...`, `go vet ./...`, `go test ./...`, `go test -tags=integration ./...`, `pnpm test`, `./scripts/smoke.sh`. Verify: all green before archive.
