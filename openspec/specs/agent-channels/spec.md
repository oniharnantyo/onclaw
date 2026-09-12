## Purpose

Channels as live collaboration rooms where workspace members (humans) and agents converse in one attributed feed: a message chokepoint with an observe-decide-speak silence policy, channel context assembly for cold summons, and agent participation through auto-posted replies and channel tools.

## Requirements

### Requirement: Channel entity and slug identity
A channel SHALL belong to a workspace and carry a display `name`, a URL `slug`, a `purpose` line, and freeform `conventions` text. The slug SHALL be unique per workspace, SHALL be used in routes and rendered as the `#` handle, and SHALL reject case-insensitive duplicates. Channel create/update/delete SHALL be served over the workspace REST API under channel permissions.

#### Scenario: Slug conflict rejected
- **WHEN** a channel is created with a slug that differs only by case from an existing channel in the same workspace
- **THEN** the API rejects the request with a conflict error

#### Scenario: Display and handle split
- **WHEN** a channel is named "Production Ops" with slug "ops"
- **THEN** routes and the room header address it as `#ops` while lists may show the full name

### Requirement: Mixed membership with specialization
Channel membership SHALL be a single ordered list of workspace members (users) and workspace agents. Each membership SHALL carry an optional specialization note describing that member's role in this channel. At most one member MAY hold the **facilitator** role (see the `channel-teams` capability). The member list SHALL be the single source for the member panel, the mention menu, the mention fan-out, the CHANNEL.md roster, and the decider broadcast. A member SHALL NOT be added twice to the same channel; agents and users from outside the workspace SHALL NOT be added.

#### Scenario: Agent added with specialization
- **WHEN** an agent is added to a channel with the note "metrics & dashboards"
- **THEN** the member panel and mention menu show the agent with that specialization

#### Scenario: Duplicate member rejected
- **WHEN** the same agent is added to a channel it already belongs to
- **THEN** the API rejects the request

#### Scenario: Single facilitator enforced
- **WHEN** a facilitator is designated while another member already holds the role
- **THEN** the API rejects the request (or the caller must first clear the existing facilitator)

### Requirement: Channel message feed
Every channel utterance SHALL be persisted as an append-only feed message with author type (user or agent), attributed author, body, mentions resolved at post time against channel members, a workspace-scoped monotonic sequence, and — for agent-authored messages — a run link `(session_id, turn_id)` plus a run summary written when the run finishes. Messages SHALL be listed through a cursor-paginated API keyed on the sequence. The feed has no edit or delete.

#### Scenario: Human message persists with resolved mentions
- **WHEN** a member posts "@atlas check the error rate"
- **THEN** the stored message attributes the human author and carries the resolved mention reference to agent Atlas

#### Scenario: Agent message links its run
- **WHEN** an agent's reply is posted to the feed
- **THEN** the message carries the session id and turn id of the run that produced it, and its run summary appears once the run finishes

### Requirement: Message chokepoint
All channel messages — human posts via the API, an agent's auto-posted final reply, and an agent's `channel.post` tool call — SHALL pass through one pipeline: persist, resolve mentions against channel members, apply the silence policy, fan out agent runs, and broadcast feed events. No other path SHALL mint agent runs in response to channel traffic.

#### Scenario: Tool post fans out like a human post
- **WHEN** an agent interjects mid-run with `channel.post` mentioning another agent
- **THEN** the mentioned agent is summoned through the same pipeline with the same caps as a human-authored mention

### Requirement: Deterministic summon on mention
A message mentioning one or more agent members SHALL summon each mentioned agent — one run each, `origin` channel — regardless of author. Mentioning a human member SHALL NOT trigger any run.

#### Scenario: Two agents mentioned
- **WHEN** a message mentions agents Atlas and Beacon
- **THEN** each is summoned with its own run and both replies appear in the feed

#### Scenario: Human mention is inert
- **WHEN** a message mentions only a human member
- **THEN** no agent run is minted by that mention

### Requirement: Observe-decide-speak for untagged messages
A message that mentions no agent member SHALL be observed by every agent member: each runs a per-agent decider — a small LLM call with no tools and no session, receiving the agent's identity line, the channel document (roster, specializations, conventions), the recent feed tail, and the message — returning engage/silent with a reason. Multiple agents MAY elect to speak, capped at two runs per untagged message; deciders run in parallel and the first two electing win the cap. A decider failure or timeout SHALL resolve to stay-silent. Declines SHALL be logged (application log only) with their reason and SHALL NOT appear as runs, feed messages, or transcript entries.

#### Scenario: Specialist self-selects
- **WHEN** an untagged message describes a metrics problem in a channel where an agent's specialization is "metrics & dashboards"
- **THEN** that agent's decider elects to engage and the agent responds in the feed

#### Scenario: Decider failure stays silent
- **WHEN** a decider call errors or times out
- **THEN** the agent stays silent, the decline is logged with the failure reason, and no run is minted

#### Scenario: Responder cap
- **WHEN** three agents' deciders all elect to engage on one untagged message
- **THEN** only the first two electing agents are summoned

### Requirement: Loop caps
Agent runs triggered through the chokepoint SHALL carry the root message id and chain depth of the conversation chain they belong to. A chain SHALL NOT exceed three agent hops from its root human message, and an agent already summoned within a chain SHALL NOT be re-summoned by that chain. Agent-authored mentions summon deterministically but remain subject to both caps. Suppressed summons SHALL be logged and mentions in the suppressed agent's posted text render as plain text. Inside an **open work session** (see the `channel-teams` capability), the session's hop budget SHALL govern instead of the chain caps: in-session mentions summon deterministically until the budget is exhausted.

#### Scenario: Depth cap stops ping-pong
- **WHEN** a chain reaches three agent hops and the last reply mentions another agent
- **THEN** the mention is parsed but no run is minted and the suppression is logged

#### Scenario: No re-summon within a chain
- **WHEN** an agent that already spoke in a chain is mentioned again by a later message of the same chain
- **THEN** no new run is minted for that agent within the chain

#### Scenario: In-session traffic bypasses chain caps
- **WHEN** an in-session reply chain runs deeper than three hops with budget remaining
- **THEN** each mention summons its agent normally

### Requirement: Catch-up tail
Every channel run SHALL receive a catch-up tail in its composed context: the most recent 30 feed messages rendered verbatim with author attribution, excluding the triggering message, with agent messages annotated by their run summary one-liner. A summoned agent that has never posted in the channel SHALL be told so.

#### Scenario: Cold summon sees the thread
- **WHEN** an agent is mentioned for the first time after ten messages between a human and another agent
- **THEN** its run context contains those messages verbatim and attributed, and its own turn input is the triggering message alone

#### Scenario: Footprints annotate the tail
- **WHEN** the tail includes an agent message whose run used grafana.query twice
- **THEN** the tail line carries a run summary noting the tool usage

### Requirement: Channel history tool
Agents in channel runs SHALL have a `channel.history` tool that reads older feed messages by backward cursor pagination with attributed authorship, in the same format as the catch-up tail, bounded per call.

#### Scenario: Deep read
- **WHEN** an agent needs context older than the tail window
- **THEN** it pages back through `channel.history` and receives attributed messages

### Requirement: Channel post tool
Agents in channel runs SHALL have a `channel.post` tool that publishes a message to the feed mid-run through the chokepoint. The tool call SHALL be subject to `pre_tool_use` hooks like any tool call, and the agent's pending run summary SHALL be backfilled onto the posted message when the run finishes.

#### Scenario: Mid-run interjection
- **WHEN** an agent posts via `channel.post` mentioning a specialist agent before finishing
- **THEN** the message appears in the feed immediately and the mentioned agent is summoned under the loop caps

### Requirement: Agent final reply auto-post
When a channel run completes, its final assistant message SHALL be posted to the feed as the agent and processed through the chokepoint like any message. Runs ending in failure or cancellation SHALL NOT post a feed message.

#### Scenario: Reply lands in the room
- **WHEN** a summoned agent completes its run
- **THEN** the feed shows the reply as an agent message and members see it live

#### Scenario: Failed run posts nothing
- **WHEN** a channel run fails with a provider error
- **THEN** no feed message is posted for it

### Requirement: Per-channel agent sessions
Each (channel, agent) pair SHALL execute its runs in a persistent private session with a deterministic session id. The session's tool calls and reasoning SHALL never enter the feed; room members see agent work only through run summaries and the linked run's tool-call cards. A later summon in the same channel SHALL reuse and extend the same session.

#### Scenario: Session persists across summons
- **WHEN** an agent is summoned in a channel, and again later in the same channel
- **THEN** both runs append to the same session and the second run's replay includes the first

#### Scenario: Work stays private
- **WHEN** an agent runs tool calls during a channel run
- **THEN** the feed shows the agent's posted message with its run link, and full tool detail lives behind the run, not in the feed body

### Requirement: Channel origin attribution
Runs minted through the chokepoint SHALL carry `origin` `channel` through the existing run event and hook payload contracts, and `user_prompt_submit` hooks SHALL evaluate on the summoning message before the model is called. No new hook event types are introduced.

#### Scenario: Hooks observe a channel run
- **WHEN** an agent is summoned from a channel
- **THEN** its `run_started` and `user_prompt_submit` hook payloads carry `origin: channel`

### Requirement: Channel REST API
The workspace API SHALL expose channel CRUD, membership add/remove/update (including the specialization note), cursor-paginated message listing, and message posting — all JWT-scoped to the workspace and guarded by channel permissions. Posting returns the persisted feed message.

#### Scenario: Post and read back
- **WHEN** a member posts a message and then lists the feed
- **THEN** the posted message appears with its author, mentions, and sequence

### Requirement: Channel event stream
Each channel SHALL expose an SSE stream broadcasting feed events: message posted, per-agent summon lifecycle (considering, decided, run started, run finished). Clients load the room via the REST feed and apply live events; events SHALL carry enough identity to deduplicate against the REST load.

#### Scenario: Live reply in another member's room
- **WHEN** an agent's reply is auto-posted while another member views the channel
- **THEN** the viewer's room appends the message from the event stream without a reload
