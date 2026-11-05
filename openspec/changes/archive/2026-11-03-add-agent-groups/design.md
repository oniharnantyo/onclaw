# Design: add-agent-groups

## Context

buddies enforces one agent per conversation structurally: `sessions.agent_id` is NOT NULL, `ManagerImpl.Submit` resolves exactly one agent from the session row (`internal/agents/manager_impl.go:306-308`), and the turn pipeline (`internal/agents/runner.go`) composes persona, tools, memory, and replayed history for that single agent. Sessions are event-sourced (`session_events`, append-only, PK (session_id, seq)); the frontend rail groups sessions by agent; turn attribution renders only the session agent. Delegation exists only as the anonymous background subagent (`internal/agents/subagents/`), whose work stays attributed to the session agent.

The reference model is OpenBot's channels (`docs/architecture/channels-and-messaging.md`, `src/backend/channel-service.ts`, `packages/contracts/src/ipc-chat-channels.ts`): a member roster with one lead, a canonical authored transcript, per-(channel, member) execution threads, bounded context packets with acceptance cursors, a task system with revisions, and deterministic routing with a lead routing session. Motivation and scope are in [proposal.md](proposal.md); the user locked the lead rules and the routing brain during exploration (recorded as decisions D5 and D4 below).

## Goals / Non-Goals

**Goals:**

- Port OpenBot's channel mechanics onto buddies' storage and runtime idioms: real per-member sessions, packet-fed member turns, authored shared transcript, task lifecycle, deterministic routing with a lead default.
- Keep ordinary single-agent sessions and the Telegram `channels` feature untouched; every existing invariant (checkpointing, compaction, rail grouping, attribution for normal sessions) holds.
- All group state durable in SQLite; a restart resumes member threads, queued dispatches, and task states.

**Non-Goals:**

- The lead-routing LLM session (OpenBot's Tier-2 router with the JSON union response). Untargeted messages go to the lead, which answers directly. A later change can introduce the router without schema change.
- Per-group memories, routines, webhooks, reactions, message editing/superseding.
- Agent-run member management tools (`group_add_member` etc.); member changes are user-only in this change.
- Any multi-host / remote-client surface (OpenBot's Team API, capability flags, mobile).
- Binding Telegram group chats to agent groups (the Telegram `channel_chats` binding stays session-bound as shipped).

## Decisions

### D1: Storage — explicit column-oriented tables, migration 000020

New tables in one forward-only migration, following the repo's column style rather than OpenBot's JSON-blob projections:

- `agent_groups(id PK, name, title, instructions, lead_agent_id REFERENCES agents, archived, revision, created_at, updated_at)`
- `agent_group_members(group_id, agent_id, added_at, PK(group_id, agent_id))`
- `agent_group_messages(group_id, message_id, seq, kind CHECK kind IN ('message','assigned','result','error','change'), author_kind CHECK author_kind IN ('user','agent','coordinator'), author_agent_id NULL, task_id NULL, payload BLOB JSON, occurred_at, PK(group_id, seq), UNIQUE(group_id, message_id))`
- `agent_group_contexts(group_id, agent_id, session_id, through_seq, summary_version, PK(group_id, agent_id))` — the acceptance cursor
- `agent_group_tasks(task_id PK, group_id, parent_task_id NULL, root_task_id, owner_agent_id NULL, request_message_id, instruction, expected_result, state CHECK state IN ('queued','running','waiting','paused','completed','failed','cancelled'), stop_reason NULL, revision, assignment_count, error NULL)`
- `agent_group_summaries(group_id PK, version, through_seq, text)`

*Alternatives considered:* a `groups` concept inside `sessions` (rejected — poisons the one-agent invariant in the rail, store, and runner); OpenBot's single JSON `CHANNEL_JSON` row (rejected — buddies repos are column-oriented and typed-scanned; JSON blobs complicate tests and partial updates). `superseded` and reactions columns are deliberately omitted; the message `payload` JSON leaves room.

### D2: Member threads are real sessions

Each (group, member) pair gets a normal `sessions` row (`agent_id` = the member) created when the member is added. The member's group turn is a real runtime turn on that session: persona documents, tool gate, memory, skills, and compaction all behave as in any session. The member session's event log holds its own tool/reasoning/task activity; the user-visible text and results are **copied back** into `agent_group_messages` as authored messages (D7). The group transcript is never replayed as the member's history.

*Alternatives considered:* one shared session with a rotating agent (rejected — breaks `Submit`'s single-agent resolution, per-session checkpointing, and the rail contract); synthesizing the member turn from the group transcript (rejected — compaction/checkpoint semantics are per-session).

### D3: Packet-based member turn input

A new turn-input variant beside the session-replay assembly (`internal/agents/turninput.go`): the member's packet contains — group title, instructions, the member's responsibility line (derived from the agent's description), the current request text + attachments, source messages and replies the member has not read (per the acceptance cursor), completed task results it depends on (shared decisions), the last N recent messages (bounded window), and the versioned summary for older history. After dispatch the cursor advances to the packet's `through_seq`; a session that was compacted, is new, or changed since the packet was built receives the full packet (cursor reset), mirroring OpenBot's delta logic.

Summary maintenance reuses the compaction summarizer seam: when a member's unread window exceeds a threshold, the group service runs one summarization over the older range after a member turn completes (asynchronous, versioned in `agent_group_summaries`). No summary is built below the threshold.

### D4: Routing — deterministic tier, lead default (user-locked)

Routing order, all deterministic:

1. Leading `@Name` (case-insensitive, longest name wins) or `@everyone` in the message text — parsed host-side, no tag markup required — routes to the named member(s) that can take work.
2. An explicit recipient from the composer's mention picker is the same mechanism.
3. A reply to a message that belongs to a task routes to that task's owner as a follow-up.
4. A reply with no task to a member's message routes to that member.
5. A follow-up on the sole open task routes to its owner.
6. A single-member group routes to that member.
7. Everything untargeted routes to the **lead**, which answers directly in its own member turn and may delegate through its in-channel tools.

Every routing decision that addresses a task or a member writes a coordinator `assigned` line into the transcript ("Assigned to Scout"), so selection is visible; the mention/lead defaults add none. Requiring a visible line only for explicit targeting matches OpenBot.

In-channel tools for a group turn: `group_history` (read the group transcript), `group_assign_task` (create a child task owned by a chosen member; bounded per root), and `group_task_result` (report a result that lands in the transcript and completes the task). These replace the session's generic history tools for that turn, mirroring OpenBot's refusal matrix.

*Alternatives considered:* the lead-routing model session (deferred — see Non-Goals; the `{agentIds|taskId|question|idle}` union slots into step 7 later); always routing through a model (rejected for v1 — latency and cost on every message).

### D5: Lead rules (user-locked)

Exactly one lead. The first member added becomes lead (a fresh group has `lead_agent_id` NULL until the first member arrives). The lead is reassignable to any member via the settings pane's crown toggle. Removing or deleting the lead — by the user or by agent deletion cleanup — falls the lead to the first remaining member; a group with no members has no lead. Changing the lead does not move tasks that already have owners, and untargeted routing uses the lead at dispatch time.

### D6: Concurrency — one lane per agent

The manager gains a per-agent lane above the per-session serializer: while an agent has a turn in flight anywhere (own chat or any group thread), further work addressed to that agent queues instead of starting. The group service enforces OpenBot's budgets as constants: at most 2 active assignments per group, at most 8 automatic child assignments per root request. A queued dispatch persists (`queued` task state), so a restart re-drains it.

*Alternatives considered:* per-session only (rejected — one agent would run concurrent group turns and chat turns, doubling provider contention and interleaving tool side effects); per-group lanes (rejected — does not protect the agent's own chat).

### D7: Copy-back attribution

The group service subscribes to each member session's event stream (the existing `agents.Subscription` pump) and copies final assistant text rows and task results into `agent_group_messages` with `author_kind='agent'`, `author_agent_id` set. Routing writes `coordinator` rows. The frontend's `entries.ts` gains an optional author on entries; turn attribution in a group conversation renders the entry's author (member avatar + name; coordinator lines render as system rows), while ordinary sessions keep today's session-agent attribution unchanged.

### D8: No command log

OpenBot's command queue with `operationId` dedup exists for multi-client replay; buddies is single-host. Group mutations go through the service in SQLite transactions, and task state is persisted **before** dispatch, so a crash between persist and dispatch leaves a `queued`/`waiting` task that restart re-drains. Idempotency comes from state-machine guards (a task cannot be dispatched twice from one revision).

### D9: Events and navigation

`groups:changed` (roster/list-level) and `group:<id>:event` (transcript and task pushes) mirror the session pump in `internal/services/chat.go`. The frontend chat store gains an `activeGroupID` beside `activeAgentID`; selecting a group switches the conversation surface to the group transcript; the session rail renders a groups section beside the agent groups. Group member turn activity (running/waiting) surfaces through the same `turns:changed`-style push so the rail can show busy states.

## Risks / Trade-offs

- [Member misses context its packet left out] → Acceptance cursor plus full-packet fallback on any session change; `group_history` tool lets a member pull older transcript deliberately.
- [Lead becomes a bottleneck — every untargeted message lands on it] → Group assignment budgets cap fan-out; the deferred routing-session change is the designed relief valve and needs no schema change.
- [Per-agent lane stalls a group behind a long chat turn] → Queued dispatches are visible (`queued`/`waiting` states in the transcript) and survive restart; lane ordering is FIFO per agent.
- [Attribution regressions in ordinary sessions] → The per-entry author branch activates only for group conversations; existing chat-view tests pin the session-agent path.
- [Copy-back duplicates content on retry/branch] → Copy-back keys on the turn id and only copies final rows of the winning variant; regenerate replaces the earlier copy (message_id stable per turn).
- [Bindings and mock churn] → Standard `task common:generate:bindings` regen step; client mock widening is expected per repo precedent.
- [SQLite write contention between member-session events and transcript copy-back] → Single-writer WAL already serializes writes; copy-back is sequential per group pump goroutine.

## Migration Plan

Forward-only migration `000020` adds six new tables; no existing table changes and no legacy row migrates, so rollback is "restore pre-migration backup" and the risk of the migration itself is minimal. Groups ship dark (no UI) only in the sense that the tables are inert until a group is created; no feature flag is needed because the feature is additive. Deploy steps: regen bindings, restart the dev app.

## Open Questions

None blocking. Two deferred knobs are deliberately left for later changes: the lead-routing session replacing step 7 of D4, and Telegram group chats binding to agent groups.
