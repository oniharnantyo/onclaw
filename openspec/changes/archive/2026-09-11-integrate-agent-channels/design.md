# Design: integrate-agent-channels

Contextual decisions from the exploration session of 2026-09-08. The user's vision: a channel where agents and humans collaborate — humans @-tag anyone, agents tag each other based on specialization, and every message is observed so agents can decide to contribute uninvited.

## D1 — Room + per-agent sessions (architecture fork)

A channel is a **room with a shared feed**, not a shared ADK session. Each (channel, agent) pair executes runs in its own persistent private session; the feed is the only shared surface. Rejected alternative — channel-as-one-shared-session — was ruled out because events carry no agent attribution (hydration would show other agents' turns as one's own) and two agents can run concurrently on one session (the 409 guard serializes same-agent runs only; interleaved replay risks corrupting turn pairing). Room+sessions keeps the entire runner machinery untouched (checkpoints, approvals, cancel, summarization, 409 serialization all stay per-session) and makes identity attribution free: agents read an attributed feed, never each other's raw transcripts.

## D2 — Single message chokepoint

Every utterance — human REST post, agent auto-posted final reply, `channel.post` tool call — passes one pipeline:

```
persist → resolve mentions vs members → silence policy (tiers) → caps → fan-out runs → broadcast
```

It is the only path that mints agent runs from channel traffic, the only place loop caps are enforced, and the single spot future policy (hooks, rate limits) attaches to. A future cron poster rides the same pipeline.

## D3 — Silence policy: observe-decide-speak

Three tiers, in order:

1. **Deterministic summon** — message mentions agent member(s) → one run each, no decider call. Applies to human- AND agent-authored mentions (the user locked literal "agent tags another agent to do something" semantics; safety comes from caps, not from mention parsing weakness).
2. **Observe-decide** — untagged message → every agent member runs a decider in parallel. Decider = tiny LLM call, **no tools, no session, not a run**: input is identity line + channel doc + tail + the message; output is strict `{engage: bool, reason: string}`. Failure/timeout → **silent** (soft-gate doctrine, same shape as the hooks `prompt` decide-only evaluator). Overlap allowed but **capped at 2 responders** per untagged message (first two electing win; parallel race). Declines: application log only — no DB table, no feed trace, no run.
3. **Caps bind regardless of tier** (D4).

No primary-agent fallback — the prototype's `Channel.agentId` concept is dropped. An untagged message nobody elects goes unanswered.

## D4 — Loop caps (cost fuses)

- **Chain depth ≤ 3**: hops from the root human message (`root_message_id`, `chain_depth` on feed rows).
- **No re-summon within a chain**: an agent already summoned in a chain is not re-triggered by that chain (store lookup on `root_message_id`).
- **Responder cap 2** on untagged messages.
- Suppressed summons are logged; the suppressed mention renders as plain text in the feed.
- Values are code constants (v1), tuned on evidence.

## D5 — Membership modeling

One `channel_members` table with `member_type` + nullable `user_id`/`agent_id` + exactly-one CHECK + partial unique indexes. The MCP two-table precedent was considered and rejected: MCP's tables encode two different attachment semantics; membership is one heterogeneous list consumed as a unit by five surfaces (member panel, mention menu, fan-out iteration, CHANNEL.md roster, decider broadcast) — two tables would put a UNION under all of them. FK integrity is preserved on both sides.

## D6 — Schema (final, includes user corrections: slug, conventions)

House conventions: uuid PKs + `gen_random_uuid()`, denormalized `workspace_id` on every table, `timestamptz`, named `uq_`/`idx_` constraints. Three tables, migration `000030_agent_channels`:

```sql
CREATE TABLE channels (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         text NOT NULL,              -- display name ("Production Ops")
    slug         text NOT NULL,              -- URL + #handle ("ops")
    purpose      text NOT NULL DEFAULT '',   -- header line
    conventions  text NOT NULL DEFAULT '',   -- freeform CHANNEL.md section (textarea exception)
    created_by   uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_channels_workspace_id_slug UNIQUE (workspace_id, slug)
);
```

Slug uniqueness is case-insensitive at the store level (the MCP `lower()` precedent); name duplicates are a store-level concern, not a DB constraint. **No `agent_id` primary-agent column** (D3).

```sql
CREATE TABLE channel_members (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id     uuid NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    member_type    text NOT NULL CHECK (member_type IN ('user', 'agent')),
    user_id        uuid REFERENCES users(id)  ON DELETE CASCADE,
    agent_id       uuid REFERENCES agents(id) ON DELETE CASCADE,
    specialization text NOT NULL DEFAULT '',  -- per-channel role note
    added_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_channel_members_one_ref
        CHECK ((user_id IS NOT NULL)::int + (agent_id IS NOT NULL)::int = 1)
);
CREATE UNIQUE INDEX uq_channel_members_channel_user  ON channel_members(channel_id, user_id)  WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX uq_channel_members_channel_agent ON channel_members(channel_id, agent_id) WHERE agent_id IS NOT NULL;
```

```sql
CREATE TABLE channel_messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id      uuid NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    seq             bigint GENERATED ALWAYS AS IDENTITY,
    author_type     text NOT NULL CHECK (author_type IN ('user', 'agent')),
    author_user_id  uuid REFERENCES users(id)  ON DELETE CASCADE,
    author_agent_id uuid REFERENCES agents(id) ON DELETE CASCADE,
    body            text NOT NULL,
    mentions        jsonb NOT NULL DEFAULT '[]',  -- resolved [{"type","id","handle"}]
    session_id      text,              -- run link (with turn_id); NULL for human msgs
    turn_id         text,
    run_summary     jsonb,             -- footprint, written at run finish: {"tools":{"grafana.query":2},"duration_ms":..}
    root_message_id uuid REFERENCES channel_messages(id) ON DELETE SET NULL,
    chain_depth     int  NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_channel_messages_author
        CHECK ((author_user_id IS NOT NULL)::int + (author_agent_id IS NOT NULL)::int = 1)
);
CREATE INDEX idx_channel_messages_channel_seq ON channel_messages(channel_id, seq DESC);
CREATE INDEX idx_channel_messages_root        ON channel_messages(root_message_id);
```

Explicitly **not** in the DB: per-(channel,agent) session registry (deterministic session id, below); decider outcomes (log-only v1); CHANNEL.md storage (rendered virtual doc); cron tables.

## D7 — Run identity and per-(channel, agent) sessions

There is **no runs table** — a run's durable identity is its `session_events`. Feed rows link `(session_id, turn_id)`. Channel session ids are deterministic: `chan_<channelID>_<agentID>` — globally unique, stable across summons, and replay accumulates the agent's own tool history in that channel. Tool detail never enters the feed; the room renders it via the linked run's cards (existing history/reattach machinery).

## D8 — Context assembly (the cold-summon answer)

`composeAgent` grows a channel branch. `ExecRequest` gains `ChannelID`, `RootMessageID`, `ChainDepth` (and the channel branch pre-computes the deterministic session id). Layered contract:

- **L1 CHANNEL.md** (virtual doc, fixed position between USER.md and BOOTSTRAP.md, only for channel runs):

```
# Channel: #incidents — Production Ops

Purpose: Coordinate production incident response end to end.

Members:
  - @sarah (human) — incident commander
  - @atlas (agent) — metrics & dashboards
  - @beacon (agent) — stakeholder comms

You are @beacon.

## Conventions
<freeform conventions text>
```

- **L2 Catch-up tail**: last **30** feed messages, verbatim, attributed, oldest→newest, **excluding** the triggering message; agent lines annotated with run-summary one-liners; cold summons prepend "You have not spoken in this channel yet."
- **L3 Turn input**: the attributed trigger, e.g. `[#incidents] @sarah: @beacon can you analyze the payment part?`
- **L4 `channel.history` tool**: backward cursor pagination, attributed, bounded per call (50).

## D9 — Agent speaking paths

1. **Auto-post**: on run completion, the final assistant message posts to the feed as the agent and re-enters the chokepoint (mention parsing included). Failed/cancelled runs post nothing.
2. **`channel.post` tool**: mid-run interjection through the same chokepoint; `pre_tool_use`-gateable; the posting run's summary is backfilled onto the message at finish.
3. **Run summary** (`run_summary` jsonb): tool name → call count map plus duration; written once at finish by the runner.

## D10 — Origin and hooks

Channel runs carry `origin: channel` (enum already exists end-to-end in the hooks subsystem). `user_prompt_submit` evaluates on the summoning message before the model call; `run_started`/`run_finished` fire with channel origin. No new hook event types in v1; a future `channel_message_posted` event is a candidate, not a commitment.

## D11 — API surface

REST (JWT, workspace-scoped, permission-guarded):

- `GET/POST /api/v1/workspaces/:ws/channels` · `GET/PATCH/DELETE …/channels/:id`
- `POST/DELETE …/channels/:id/members` · `PATCH …/channels/:id/members/:mid` (specialization)
- `GET …/channels/:id/messages?after=<seq>&limit=` · `POST …/channels/:id/messages`
- `GET …/channels/:id/events?stream=true` (SSE: `message_posted`, `summon_considering`, `summon_decided`, `run_started`, `run_finished`; each carries `seq`/ids for dedup against the REST load)

## D12 — Permissions

`channels.read` and `channels.write` enter the permission catalog; the companion migration backfills builtin Owner/Admin (+Superadmin) role snapshots — the established `000022`/`000027` pattern, mandatory because roles snapshot permissions at creation.

## D13 — Web architecture

- `api.ts` gains a `channels` client; `ChatRoute` channel target resolves server channels; Sidebar section binds live data.
- Room load: REST feed (cursor) → subscribe channel SSE with `seq` dedup; reconnect refetches the gap.
- Agent messages whose run is in flight attach to the run stream via the **existing re-attach machinery**; tool cards reuse standard rendering behind a collapsed "view work" disclosure.
- Mock retirement: the client-side fan-out engine (`runtime.tsx` `respondFor` chain) and seeded channels leave the channel path; **BREAKING** vs the mock's staggered-simulation and primary-agent fallback behaviors.

## D14 — UI contract (ASCII gallery is a hard gate)

Per the standing rule, **no UI implementation starts before the full-surface ASCII gallery is drawn and approved**. Surfaces the gallery must cover: channel creation dialog (name/slug/purpose/conventions), membership editor (add humans/agents, edit specialization, remove), the live room (considering indicator lifecycle, footprint one-liner + work disclosure, mention menu with humans + specialization subtitles), and sidebar states. The approved gallery becomes the binding appendix of this design.

## D15 — Prototype deviations (deliberate)

- `Channel.agentId` (primary agent) dropped — replaced by the silence policy.
- Staggered simulated fan-out replaced by live summons.
- `name` splits into name + slug (workspace precedent); `purpose` retained; `conventions` added.

## D16 — Constants

| Constant | Value | Where |
|---|---|---|
| Catch-up tail | 30 messages | code |
| Chain depth cap | 3 | code |
| Untagged responder cap | 2 | code |
| `channel.history` page | 50 | code |
| Decider timeout | ~10s, silent on expiry | code |

All code constants in v1; none configurable until evidence demands it.

## D17 — Non-goals (deferred, not forgotten)

Threads (Slack-style context bounding), digest-on-cold-summon for over-long conversations, deep handoff (attach/summarize another agent's session), cron posting into channels (ExecRequest shape stays cron-compatible), human mention notifications, decider observability UI, message edit/delete.
