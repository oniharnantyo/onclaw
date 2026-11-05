# Proposal: add-agent-groups

## Why

Every buddies conversation today binds to exactly one agent (`sessions.agent_id` is NOT NULL and the whole turn pipeline resolves a single agent), so there is no way to put several agents into one conversation and coordinate them as a team. OpenBot (nightly-labs/openbot) ships this experience as its "channels" feature — one shared transcript, a member roster with a single lead, per-member execution threads, and visible assignment — and it is the model this change ports, with buddies' own storage and runtime idioms.

## What Changes

- **Agent groups**: a new entity with a name, title, instructions, a roster of existing agents, and exactly one lead. The first member added becomes lead; the lead is reassignable to any member; removing or deleting the lead falls back to the first remaining member, so a group with members always has a lead.
- **Shared transcript**: a group owns one canonical, sequence-ordered transcript whose every message carries its author — the user, a member agent, or a coordinator/system line (e.g. "Assigned to Scout"). Messages have stable IDs and per-group sequences.
- **Member threads**: each (group, member) pair gets a real session bound to that agent. A member's group turn is built from a bounded context packet — group purpose, responsibilities, the current request, source messages, replies, shared decisions, recent messages, attachment references, and a versioned summary for older history — with a per-member acceptance cursor so a resumed session receives only what it has not read. The group transcript never becomes the member's replayed history.
- **Routing**: deterministic targets route without a model — a leading `@Name`/`@everyone` mention, a reply to a task, a follow-up on an open task, a single-member group. Everything untargeted goes to the lead, which answers directly and can delegate to members through its own in-channel tools. No separate routing session in this change (deferred; see below).
- **Group tasks**: request-scoped work units with an owner, a parent/root chain, a state machine (queued, running, waiting, paused, completed, failed, cancelled), a revision that invalidates stale assignments, and Stop / Continue / Reassign / Cancel controls surfaced in the transcript.
- **Concurrency**: an agent runs at most one turn at a time across its own chats and all of its group threads; other addressed work queues behind the active turn. A group bounds its concurrently active assignments.
- **Agent deletion cleanup**: deleting an agent removes its memberships, falls the lead back per the rule above, and releases its owned tasks.
- **Group UI**: groups appear in the chat menu beside the agent session groups; a group settings pane edits name, title, instructions, members, and the lead (crown affordance); the transcript renders per-message authorship; the composer gains an `@` mention picker.
- **Events**: `groups:changed` plus a per-group live event push mirroring the existing per-session pump.
- **Deferred explicitly**: per-group memories, per-group scheduled routines, webhooks, any multi-host/remote surface, and the lead-routing LLM session (a future change may replace the lead-default with a model-driven router; the transcript and task schema leave room for it).

## Capabilities

### New Capabilities

- `agent-groups`: shared multi-agent group chats — the group entity and lead rules, the authored shared transcript, per-member execution threads fed by context packets, deterministic routing with a lead default, group tasks and their lifecycle controls, agent-level turn lanes, and the group surfaces in the chat menu, settings pane, composer, and transcript.

### Modified Capabilities

- `chat-view`: turn attribution changes — in a group conversation the attribution of each entry comes from the entry's author (the addressed member agent, or a coordinator line for assignments), not from a session agent; the existing session-agent attribution rule continues to apply to ordinary sessions.
- `data-store`: agent groups and their transcripts, contexts, and tasks are persisted in the SQLite database; deleting an agent also cleans up its group memberships and falls the lead back, alongside the existing guarantee that sessions survive agent deletion.
- `agent-runtime`: a new agent-level lane requirement — one turn at a time per agent across its own sessions and group threads, with group requests queueing behind an active turn.

## Impact

- **Backend (Go)**
  - `internal/store`: new `AgentGroup`-family types in `types.go`; SQLite repositories and migration `000020` adding the group, member, message, context, task, and summary tables.
  - New `internal/groups` (service + store access) owning transcript, routing, and task lifecycle; bound into the Wails app and server host like the other services.
  - `internal/agents`: a per-agent lane above the per-session turn serializer in the manager; a packet-based turn-input variant beside the existing session-replay input assembly in the runner; member-turn events copied back into the group transcript.
  - `internal/services/events.go`: `groups:changed` and per-group event names; `internal/services/chat.go`-style subscribe/forward pump for groups.
  - `AgentsService.Delete` gains group-membership cleanup and lead fallback.
- **Frontend (TypeScript)**
  - Chat store navigation state gains an active-group context beside `activeAgentID`; the session rail renders a groups section; the chat header, transcript entries (`entries.ts`), and turn attribution render per-entry authors; the composer gains a mention picker; a new group settings pane.
  - Wails bindings regenerate (`task common:generate:bindings`), widening the generated client mocks as usual.
- **No new external dependencies**; storage stays SQLite. Existing single-agent sessions, the Telegram `channels` feature (unrelated spec of the same English word), and the subagent machinery are untouched; no legacy data migrates.
