# Proposal: integrate-agent-channels

## Why

Channels are the collaboration surface where agents and humans work as one team, but today they exist only as a client-side simulation over seed data — no backend, no real agent participation. The hooks system already reserved `Origin: "channel"` on runs, sessions are workspace-scoped, and the prototype pins the UX contract. Making channels real turns the product's core promise — named agents reached through direct chats, team channels, and schedules — into a working loop: agents that observe the room, decide when to speak based on their specialization, and hand context to each other mid-issue.

## What Changes

- **Channel domain + storage**: channels (name, goal, editable norms), mixed membership — workspace members (humans) and agents side by side — with a per-membership **specialization note** ("metrics & dashboards"), and an attributed **channel message feed** (author, body, parsed mentions, nullable run link + run footprint).
- **Message chokepoint**: one pipeline every utterance passes through, human or agent — persist → parse mentions → silence policy → fan-out agent runs. No side channels.
- **Observe-decide-speak silence policy**: a message @mentioning an agent summons it deterministically; an untagged message is **observed by every member agent**, each running a tiny per-agent decider (LLM, no tools) that returns engage/stay-silent with a reason; caps bind on top — chain depth limit, no-retrigger-within-chain, responder cap on untagged messages. No primary-agent fallback: an untagged message nobody elects goes unanswered.
- **Channel context assembly** (the cold-summon answer): `CHANNEL.md` virtual doc composed only for channel runs (members + specializations + norms), a verbatim attributed **catch-up tail** (last 30 feed messages, run footprint one-liners included, triggering message excluded), the attributed trigger as turn input, and a cursor-paginated `channel.history` tool for deeper reads.
- **Agent participation**: a channel run's final reply auto-posts to the feed as the agent (parsed for mentions like any message); a `channel.post` tool lets an agent interject mid-run; each (channel, agent) pair gets a persistent private session — tool calls and reasoning stay out of the room, surfaced only as linked run cards.
- **REST + SSE**: channel CRUD, membership management (humans, agents, specialization notes), message list/post, channel event stream (feed events, run lifecycle, decider considering/decided).
- **Web**: the live channel room replaces the client-side mock — **BREAKING** for the mock's mention fan-out and primary-agent fallback behaviors, which the silence policy replaces.

## Capabilities

### New Capabilities

- `agent-channels`: channel entity, mixed membership with specialization notes, message feed, the message chokepoint pipeline, the observe-decide-speak silence policy, loop caps, channel context assembly (CHANNEL.md, catch-up tail, footprints, history tool), agent speaking paths, per-(channel, agent) sessions, and the channel REST/SSE API.

### Modified Capabilities

- `agent-runtime`: instruction composition gains a channel-context branch (CHANNEL.md + catch-up tail injected only for channel runs); `ExecRequest` carries channel attribution so a run can be triggered outside the HTTP chat path.
- `web-app/chat`: the "Channel mentions" requirement moves from simulated staggered replies to live summons; mention menu lists humans alongside agents with specialization subtitles; considering indicator while deciders run; run cards and footprints render from real runs.
- `web-app/chat-runtime`: channel conversation lifecycle moves from the client-side mention fan-out to live channel events (feed SSE + run re-attach).
- `web-app/data-layer`: channels leave the "not yet integrated" seed-only domain list — channel data and messages persist through the API.

## Impact

- **Backend**: new `domain`/`store`/`handlers` surface for channels; migrations for `channels`, `channel_members`, `channel_messages` (+ permission catalog additions with the established builtin-role backfill); runner changes (`ExecRequest` fields, `composeAgent` channel branch, auto-post on completion); tool registry additions (`channel.post`, `channel.history`).
- **Frontend**: `ChatRoute`/`ChatView` channel target goes live; `api.ts` channel client; channel SSE consumer; Sidebar/mention-menu wiring.
- **Explicitly unchanged**: the `/v1` OpenResponses facade and direct-chat flows; cron stays decoupled (the `ExecRequest` extension is shaped so a future schedule posting into a channel rides the same chokepoint, but no scheduler work lands here).
