## 1. Dependencies & config (Phase 0)

- [x] 1.1 Upgrade `github.com/cloudwego/eino` to v0.10.0-alpha.28; add `eino-ext/components/model/agenticopenai`, `agenticclaude`, `agenticgemini`; verify the classic prompt-generation path and its pinned classic adapters still compile (`go build ./...`, `go vet ./...`, `go test ./...`)
- [x] 1.2 Replace `ONCLAW_WORKSPACE_DIR` with `ONCLAW_DIR` in `internal/config` (default `$HOME/.onclaw`, absolute-path validation at startup); derive workspace root as `<ONCLAW_DIR>/workspaces`; update CLI flags and docs; no compat alias
- [x] 1.3 Centralize path algebra in domain helpers: `WorkspaceRoot(dir)`, `SystemSkillsDir(dir)`, `WorkspaceSkillsDir(dir, tenantSlug)`, `AgentSkillsDir(dir, tenantSlug, agentSlug)` alongside the existing `AgentWorkspaceDir`; update `domain.DefaultWorkspaceDir` callers
- [x] 1.4 Update existing tests referencing `ONCLAW_WORKSPACE_DIR`/`DefaultWorkspaceDir` to the new knob

## 2. Schema migration (Phase 1)

- [x] 2.1 Write migration `agent-runtime` (up/down): `agents.context_window` (nullable bigint), `agents.disabled_tools`/`disabled_skills`/`disabled_mcps` replacing `tools`/`skills`/`mcp` (old contents dropped), `workspaces.description` (nullable text), drop `workspace_skills`, create `session_events` (PK (session_id, event_id), turn_id, seq, kind, payload, occurred_at, workspace_id) and `session_checkpoints` (checkpoint_id PK, data)
- [x] 2.2 Update `domain.Agent`: add `ContextWindow *int`, replace `Tools`/`Skills`/`MCP` with `DisabledTools`/`DisabledSkills`/`DisabledMCPs`; validation: context_window > 0; remove `ValidateSkillName` referential usage
- [x] 2.3 Update `domain.Workspace` with `Description`; thread it through workspace create/update payloads, store, fake, and postgres impl
- [x] 2.4 Update `store.AgentStore`, fake, and postgres impl for the new agent columns; update agents CRUD handlers and payload DTOs (denylist fields, context_window in/out); delete workspace-skills handlers, routes, `WorkspaceSkillStore` port, fake, and postgres impl
- [x] 2.5 Parse `limit.context` in `internal/modelcatalog` (`CatalogModel`), expose context limit lookup by provider type + model id (compatible types resolve nothing)
- [x] 2.6 Auto-fill `context_window` on agent create/update when omitted: catalog lookup by provider/model; client value wins; unit-test the resolution chain (agent → catalog → unset)

## 3. Runtime core (Phase 2)

- [x] 3.1 Define the `Engine` port and domain event vocabulary in `internal/agents` (`ExecRequest`, `EventStream`, `TranscriptEvent` incl. deltas, tool-call lifecycle, compaction, terminal events) — no Eino types exported
- [x] 3.2 Implement the agentic model factory: 6 provider types → 3 adapters, base_url strip contract, decrypted credentials; unit tests per type with endpoint assertions
- [x] 3.3 Implement the instruction composer: read AGENTS/IDENTITY/SOUL/BOOTSTRAP from the agent dir, render WORKSPACE.md (name, description) and USER.md (caller name, email, role via membership), fixed order, missing files skipped; unit tests incl. failed-prompt-generation case
- [x] 3.4 Implement `session_events`/`session_checkpoints` persistence (postgres + fake) with idempotent appends and cursor queries; wire workspace scoping
- [x] 3.5 Implement the ADK session adapter in `internal/agents` (serialize/deserialize events + checkpoints over the store) and pass Eino's `adk/session` conformance suite against the Postgres implementation (build tag `integration`) — `internal/store/postgres/session_events_conformance_test.go` runs `session.RunConformanceTests[*schema.AgenticMessage]` against a live Postgres-backed adapter; note the adapter serializes events with Eino's `schema.HumanReadableSerializer` (not plain JSON) so registered typed payloads survive durable round-trips
- [x] 3.6 Implement `Engine.Run`: compose agent per execution (model from factory, instruction, streaming runner, per-run cancel handle), map ADK events → `TranscriptEvent`s (`defer stream.Close()`), append durable events, return the stream
- [x] 3.7 Implement cancellation at safe points and the incomplete-message marker on interrupted streams; test reload-after-cancel renders partial content from durable data
- [x] 3.8 End-to-end engine test against a scripted fake model: multi-turn thread, history replay, streaming deltas observed, terminal event exactly once

## 4. Tool surface (Phase 3)

- [x] 4.1 Define the tool registry port (register by dotted name → tool constructor; lookup; list names) and register it in the composition root; denylist filter applied at agent composition (`disabled_tools`, unknown names inert)
- [x] 4.2 Implement the `web.search` built-in (DuckDuckGo, no credentials) behind the registry; unit test
- [x] 4.3 Attach the filesystem middleware with a local backend rooted at `AgentWorkspaceDir`; implement path-escape rejection (symlink/`..`) at the backend boundary; no shell attached; test the jail (escape attempts fail as tool errors)
- [x] 4.4 Implement the multi-root skills resolver (system/workspace/agent tiers, precedence agent > workspace > system, system exempt from `disabled_skills`) and back the skill middleware with it (metadata list, body on demand)
- [x] 4.5 Embed system skills (`//go:embed`) and implement the startup mirror into `<ONCLaw_DIR>/skills` (overwrite changed, remove extraneous); test sync idempotence and extraneous-file removal

## 5. Summarization (Phase 4)

- [x] 5.1 Wire the summarization middleware in the handler stack in order (patchtoolcalls → reduction → summarization → skill → filesystem) with the agent's own model and internal events enabled
- [x] 5.2 Implement the trigger: resolved context window × server-configured margin; log the resolved window and margin at execution start
- [x] 5.3 Implement the `transcript.md` offload write inside the agent jail from the summarization callback; test that compaction produces the offload file and a window-replacement record while replay preserves pre-compaction history

## 6. Verification

- [x] 6.1 Full backend suite: `go build ./...`, `go vet ./...`, `go test ./...` green; integration suite (`go test -tags=integration ./...`) green against Postgres
- [x] 6.2 Spec sweep: every scenario in the five delta spec files has a corresponding automated test or an explicit listed exclusion (HTTP-surface scenarios deferred with the API scope) — see `openspec/changes/agent-runtime/spec-sweep.md` (59 scenarios: 49 TESTED, 10 EXCLUDED, 0 uncovered)
- [x] 6.3 Web: Advanced model section renders context-window (auto-filled from the model catalog `context_limit`, editable, reset-to-auto affordance) with create+edit tests; `pnpm build` and `rtk proxy npx vitest run` green (253 tests). NOTE: the three disable-list editors were intentionally NOT built — the capability step keeps its existing allow-list pickers per the product decision to "refine to use allow list as-is".
