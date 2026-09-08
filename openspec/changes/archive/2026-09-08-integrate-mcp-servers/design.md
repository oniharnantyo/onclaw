# Design — integrate-mcp-servers

## Context

Three disconnected MCP fragments exist today: the web pane renders `seed.ts` fabrications, `agents.disabled_mcps text[]` is persisted and API-plumbed but read by nothing, and no MCP client exists in `go.mod`. The tool system already provides every pattern this change reuses — see proposal.md for motivation. Decisions D1–D3 were locked with the user (opt-in selection; all three transports; server-level granularity); the storage split below supersedes the earlier single-table sketch after review.

## Goals / Non-Goals

**Goals:**
- Real MCP tool surface for agent executions, driven by workspace-registered and agent-private servers.
- Credential handling identical in strength to the existing tool-settings scheme.
- Runtime integration that cannot destabilize runs (a dead MCP server degrades, never fails).

**Non-Goals:**
- OAuth authorization flows for MCP servers (static credentials only in v1).
- Per-tool selection within a server; per-turn overrides of MCP tools.
- Background health checks / auto-reconnect loops; MCP resources, prompts, and sampling; elicitation.

## Decisions

**D1. Opt-in selection via `agents.enabled_mcps` (user-locked).** New `text[] NOT NULL DEFAULT '{}'` column; `disabled_mcps` is dropped. Safe because nothing ever consumed `disabled_mcps` — no data migration, just a rename sweep through `domain.Agent`, the postgres store, handlers (create/update/starter-agent payloads), and tests. Precedent for dropping a dead denylist: `disabled_skills` in workspace-skills-ui. The opt-in flip vs the prototype's look-opt-in seed lists: the seed arrays were mock; the schema's own evolution (denylist → allowlist, like `disabled_tools` → `tools`) and the token-cost argument decided it. References are server-assigned UUIDs, not names — names would break on rename. Not referentially validated (consistent with `tools` semantics; deletion leaves stale ids inert).

**D2. Two scope tables, one shared connection type (revised after review — supersedes the single-table sketch).** `workspace_mcp_servers (id, workspace_id, name, transport, command, args, env, url, headers, enabled, timestamps)` and `agent_mcp_servers (same + agent_id → agents ON DELETE CASCADE)`. The earlier idea of one table with nullable `agent_id` was rejected: the repo's convention is scope-prefixed tables (`workspace_api_keys`, `agent_user_memories`), and every consumer would carry an `agent_id IS NULL` predicate forever. Duplication stays thin because both entities embed one shared `domain.MCPConnection` value type (`transport`, `command`, `args`, `env []{name,value}`, `url`, `headers []{name,value}`) and all validation, encryption, and connection logic operates on that type, written once. Name uniqueness per scope (workspace id, or agent id).

**D3. Transports via mcp-go clients behind one config branch.** `github.com/cloudwego/eino-ext/components/tool/mcp` wraps `mark3labs/mcp-go` clients; `client.NewStdioMCPClient(command, env, args...)`, `client.NewStreamableHttpClient(url, headers...)`, `client.NewSSEMCPClient(url, headers...)`, then the initialize handshake and `mcpp.GetTools(ctx, &mcpp.Config{Cli})` → eino `tool.BaseTool`s. All three transports cost the same branch; stdio is non-negotiable because most off-the-shelf servers are npx-based (user-locked).

**D4. Secret handling reuses the tool-settings scheme, name-keyed.** Env/header values encrypt with `secrets.Encrypt(encKey, workspaceID, …)` (workspace-AAD, same derivation as provider keys and web.search entries), `value_hint` last-4 plaintext beside each, write-only over the API (`configWithHints`-style view). List-row merge keys on the env-var/header **name** (unique within a server) instead of web.search's server-assigned entry ids — names are the natural stable key here and the UI edits rows in place. The merge/encrypt/decrypt/hint helpers generalize out of `ToolSettingsService` rather than being copied.

**D5. MCPManager: lazy per-workspace connections, TTL-idle stdio, invalidate on write.** Modeled on `BrowserManager` but keyed per workspace+server, not per session: first resolve that needs a server connects, lists tools, and caches `{client, tools, status}`; subsequent runs reuse it. stdio subprocesses idle out on a TTL reaper (browser's idle-timeout precedent); HTTP connections idle out the same way. Every mutating write (update, delete, master-disable) invalidates the cached entry so config changes take effect immediately. Connections are workspace-scoped and never shared across workspaces. Teardown of the manager rides the composition root's lifecycle, not `SessionTeardown` (connections outlive single sessions deliberately — stdio spawn per run would dominate latency).

**D6. Resolution appends MCP tools after built-ins.** `Runner.resolve` extends: built-ins resolve exactly as today, then MCP tools append from (workspace servers enabled ∧ id ∈ `agent.EnabledMCPSs`) + agent-private rows, using a narrow `MCPPolicy` port next to `ToolPolicy` (granular DI per AGENTS.md; composition root wires the implementation). MCP is deliberately **independent of the `tools` allowlist and the workspace tool gate** — the gate keys on the static catalog, which MCP is not; the master switch on each server is its gate. Known eino constraint (fs-jail memory): `ToolsNode` executes via the streamable wrapper chain, so the existing tool-error→JSON middleware path covers MCP tools; the approval/interrupt flow passes through untouched. Tool-call cards and usage capture work because MCP tools are ordinary eino tools in the same loop.

**D7. Tool naming `mcp__<server>__<tool>`, sanitized, collision-suffixed.** Dots are illegal in OpenAI-style function names (`^[a-zA-Z0-9_-]{1,64}$`); `mcp__` prefix follows the Claude Code convention and namespaces against built-ins (`web.search` etc. never collide). Server names sanitize case-insensitively (`[a-z0-9_-]`); a collision after sanitization appends `_<n>`. Names can exceed 64 chars for pathologically long tool ids — sanitize-and-truncate with a suffix hash; documented, rare. The web `toolCatalog` display-name prettifier gains a `mcp__*` branch (e.g. `mcp__github__create_issue` → "GitHub · Create Issue") so transcript cards render human names with zero per-server client config.

**D8. Failure semantics: skip, mark, continue.** A server unreachable or failing `tools/list` at resolution contributes zero tools; its stored status flips to `error` with the message (best-effort write — a failing status write never fails the run); the run proceeds. This contrasts deliberately with web.search's fail-fast: web.search is one tool the agent may retry, while a hard-failing MCP server would otherwise hold every run hostage. Probe-on-save gives admins the fast feedback loop; runtime errors surface through the pane's status, not the transcript.

**D9. Permissions: `tools.write` for the workspace registry, `agents.write` for private servers.** stdio config executes a command on the server host — Owner/Admin-only via the existing `tools.write` grants (per the builtin-role-backfills memory, no new permission is introduced, so no backfill migration is needed). Reads use `tools.read` (every built-in role). Private servers ride agent edit rights: whoever can edit the agent can wire its private integrations. API keys/subscription creds inside env/header values are covered by D4's encryption.

**D10. Store port shape.** One `store.MCPServers` sub-interface per scope granularism: `WorkspaceMCPServers` and `AgentMCPServers` (postgres + fake impls), consumed positionally by one `MCPSettingsService` (validation, encryption, view hints) — mirrors `ToolSettingsStore`. Endpoints: `/api/workspaces/:ws/mcp-servers` (GET/POST, `/:id` PATCH/DELETE, `/:id/probe` POST) and `/api/workspaces/:ws/agents/:slug/mcp-servers` (same shape, `agents.write`). Probe runs inline in the handler via the manager (bounded timeout ~10s); status persists on the row.

**D11. Migration 000024.** Single pair: create both tables (JSONB for env/headers, text for command/url, uuid FKs, cascade from agents), `ALTER TABLE agents ADD COLUMN enabled_mcps text[] NOT NULL DEFAULT '{}'`, `DROP COLUMN disabled_mcps`. Down reverses (re-adding `disabled_mcps` empty — nothing to restore). Built-in roles need no permission backfill (D9).

## Risks / Trade-offs

- **stdio executes arbitrary commands on the host** → `tools.write` gating (D9), config form shows the exact command/args, secrets never logged; documented in the pane's helper text.
- **Opt-in default-off means silent no-op for existing agents** → the agent modal and pane show selection state; the pane's used-by column makes under-subscription visible. Accepted trade-off (user-locked).
- **Tool-schema bloat per opted-in server** (a GitHub MCP is 20+ tool schemas every turn) → server-level toggles keep the unit of control coarse; per-tool filtering is the escape hatch if needed (non-goal now). Summarization already bounds context growth.
- **Long-lived stdio children can leak** (panic paths, SIGKILL'd parent) → TTL reaper + `Wait`-and-reap on exit + command groups killed on invalidate; worst case an orphan process dies with its idle timer.
- **Probe latency on save** (unreachable endpoint blocks the response) → bounded probe timeout (~10s); the create/update still persists with status `error` so the UI never hangs past the bound.
- **SSE transport is legacy-adjacent** (streamable HTTP is the modern default) → still shipped because many deployed servers only speak SSE; cost is one client branch.
- **Stale server binary returns different tools after restart** → tool lists re-list on each manager (re)connect, and reconnect happens after any config write or TTL idle; worst case tools lag one TTL behind a server-side change.

## Migration Plan

1. Ship migration 000024 (tables + column swap) — additive except the dead `disabled_mcps` drop.
2. Deploy backend (migrate up on start per repo convention), then web. Agents created between deploy steps behave identically (empty `enabled_mcps`).
3. Rollback: `migrate down` restores `disabled_mcps` (empty) and drops the tables; the previous binary ignores the new tables. No encrypted data is lost (both directions keep rows).

## Open Questions

None — D1–D3 were locked with the user; the storage split was confirmed in review.
