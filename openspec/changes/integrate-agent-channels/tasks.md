# Tasks: integrate-agent-channels

## 1. Domain & migrations

### 1.1 Channel domain entities
- [ ] `domain.Channel` (ID, WorkspaceID, Name, Slug, Purpose, Conventions, CreatedBy, timestamps), `domain.ChannelMember` (MemberType, UserID, AgentID, Specialization), `domain.ChannelMessage` (AuthorType, author refs, Body, Mentions, SessionID, TurnID, RunSummary, RootMessageID, ChainDepth, Seq, CreatedAt)
- [ ] Slug validation (kebab-case, lowercase, length bounds) and `ErrChannelSlugConflict` / duplicate-member error sentinels
- [ ] Mention resolution type (`{Type, ID, Handle}`) shared by store and chokepoint

### 1.2 Permission catalog + backfill
- [ ] Add `channels.read` / `channels.write` to the permission catalog with checks wired for channel routes
- [ ] Migration `000031_channels_permission_backfill`: idempotent backfill of the new permissions into builtin role snapshots (000022/000027 pattern)

### 1.3 Schema migration
- [ ] Migration `000030_agent_channels`: `channels`, `channel_members`, `channel_messages` exactly per design D6 (CHECKs, partial unique indexes, seq identity, indexes)
- [ ] Bump `latestSchemaVersion`; down migrations reverse cleanly

## 2. Store layer

### 2.1 Ports and fake
- [ ] `store.ChannelStore` (+ narrow sub-interfaces per DI convention): channel CRUD, membership add/remove/update/list, message insert/list-by-cursor, lookup by (workspace, slug), root-message chain queries
- [ ] In-memory fake with workspace isolation, matching postgres semantics (case-insensitive slug conflict, duplicate-member rejection)

### 2.2 Postgres implementation
- [ ] `store/postgres` adapter for all three tables; `seq` returned from insert; `(workspace_id, lower(slug))` uniqueness enforced at store level
- [ ] Cursor pagination on `(channel_id, seq DESC)`; chain lookup by `root_message_id`; integration tests (`-tags=integration`)

## 3. Chokepoint & silence policy

### 3.1 Mention parsing and resolution
- [ ] Parse `@handle` from message bodies against the channel roster (agents and humans); unresolved mentions stay plain text
- [ ] Fan-out candidate builder: mentioned agents (Tier 1) vs observing agents (Tier 2)

### 3.2 Silence decider
- [ ] Per-agent decider call: identity line + channel doc + tail + message → strict `{engage, reason}`; agent's own model, no tools, ~10s timeout, silent-on-failure
- [ ] Parallel deciders with responder cap (first 2 electing win); decline logging with reason

### 3.3 Caps and fan-out
- [ ] Chain bookkeeping: root message id + depth propagation; depth ≤ 3 and no-re-summon-within-chain enforcement at the chokepoint; suppression logging
- [ ] Run submission per summoned agent: deterministic session id `chan_<channelID>_<agentID>`, `ExecRequest{Origin: channel, ChannelID, RootMessageID, ChainDepth}`, attributed turn input `[#slug] @author: body`
- [ ] Feed event broadcast: `message_posted`, `summon_considering`, `summon_decided`, `run_started`, `run_finished`

## 4. Runner integration

### 4.1 ExecRequest and composition
- [ ] `ExecRequest` gains `ChannelID`, `RootMessageID`, `ChainDepth`; validation updated
- [ ] `composeAgent` channel branch: CHANNEL.md virtual doc (roster + specializations + conventions + tail) in fixed position; tail builder (last 30, attributed, run-summary one-liners, trigger excluded, cold-summon note)

### 4.2 Speaking paths
- [ ] Auto-post final assistant message on completed channel runs through the chokepoint; nothing on failed/cancelled
- [ ] `run_summary` writeback at run finish (auto-posted and `channel.post` messages)

### 4.3 Hooks interplay
- [ ] Verify `origin: channel` flows through `run_started`/`user_prompt_submit`/`run_finished` payloads and matchers; `user_prompt_submit` fires on the summoning message

## 5. Tools

- [ ] `channel.post` registry tool (body arg; posts via chokepoint; pre_tool_use-gateable; exposed only in channel runs)
- [ ] `channel.history` registry tool (backward cursor, limit 50, attributed output; exposed only in channel runs)
- [ ] Tool docs/catalog entries with display names

## 6. HTTP API & SSE

- [ ] Channel CRUD handlers (permission-guarded, slug conflict → 409, case-insensitive)
- [ ] Membership handlers: add/remove/update specialization, roster listing
- [ ] Messages handlers: cursor list (`after`, `limit`), post (returns persisted message)
- [ ] Channel SSE endpoint: broadcaster wiring, seq-keyed dedup, keepalive, terminal/no-buffer headers consistent with existing streams
- [ ] Router registration under workspace scoping; error envelopes per existing translation

## 7. Web API client & state

- [ ] `api.ts` `channels` client: CRUD, membership, messages, post
- [ ] Channel SSE consumer: connect after REST load, seq dedup, reconnect-with-gap-recovery
- [ ] Store wiring: channels hydrate from API on boot; sidebar section binds live channels; seed channels retired from the channel path

## 8. UI (GATED — see 8.0)

### 8.0 ASCII gallery — HARD GATE
- [ ] Draw the full-surface ASCII gallery: channel creation dialog, membership editor (specialization editing), live room (considering indicator lifecycle, footprint + work disclosure, mention menu with humans + specialization subtitles), sidebar states
- [ ] Obtain user approval; append approved gallery to design.md as the binding UI contract
- [ ] Do NOT start 8.1+ before approval

### 8.1 Room liveness
- [ ] `ChatRoute` channel target resolves server channels; room loads feed via REST then applies stream events
- [ ] Considering indicator per observing agent (considering → streamed reply | fade out), driven by stream events
- [ ] Agent message footprint one-liner + collapsed "view work" disclosure rendering linked run tool-call cards (standard card components)

### 8.2 Composer and menus
- [ ] Mention menu lists humans and agents with specialization subtitles; posting goes through the bridge to the feed API
- [ ] No regenerate/edit affordances in channels (existing rule preserved)

### 8.3 Channel management
- [ ] Channel creation dialog (name, slug, purpose, conventions textarea)
- [ ] Membership editor (add humans/agents, edit specialization, remove) from the member panel

## 9. Verification

- [ ] Unit + fake tests per package green; `go build ./...`, `go vet ./...`, `go test ./...`
- [ ] Integration tests for store + migrations against PostgreSQL
- [ ] Smoke script section: login → create channel → add agent with specialization → post mention → agent auto-posts → untagged message observed (decider mock) → caps enforced
- [ ] `openspec validate --strict` clean; manual browser pass left open as the final task
