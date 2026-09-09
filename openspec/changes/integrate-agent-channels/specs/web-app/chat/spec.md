# web-app/chat delta

## MODIFIED Requirements

### Requirement: Channel mentions
In a channel, typing `@` SHALL open a menu of channel members — both agents and humans — filtered by handle prefix, with each agent entry showing its channel specialization. Posting a message mentioning one or more agents SHALL summon each mentioned agent through the live backend: each responds in the thread as itself, streamed. A message with no agent mentions SHALL NOT wake any agent by default; member agents observe it and may respond per the silence policy (observe-decide-speak). There is no primary-agent fallback. The room's history SHALL load from the server feed and new messages SHALL arrive via the channel event stream.

#### Scenario: Mentioned agent responds
- **WHEN** the user posts "@Warden can you check the budget?" in `#incidents`
- **THEN** Warden (a real backend run, not a simulation) replies in the thread

#### Scenario: Humans appear in the menu
- **WHEN** the user types `@` in a channel whose members include agents and humans
- **THEN** the menu lists both, agents with their specialization subtitle and humans without

#### Scenario: No fallback on untagged message
- **WHEN** a message mentions no agent and no member agent's decider elects to engage
- **THEN** the message posts to the feed and no agent replies

## ADDED Requirements

### Requirement: Considering indicator
While an untagged message's deciders are running, the room SHALL show a per-agent "considering" affordance for each observing agent member; an agent that elects to speak transitions into its streamed reply, and an agent that declines SHALL have its affordance fade out. The affordance state SHALL be driven by channel stream events, not client-side simulation.

#### Scenario: Considering resolves to silence
- **WHEN** an untagged message is posted and an observing agent's decider declines
- **THEN** the agent's considering affordance appears and then fades without a reply

### Requirement: Run work disclosure in channels
An agent's channel message SHALL render its run summary one-liner and offer a collapsed work disclosure that expands into the linked run's tool-call cards, reusing the standard card rendering. The disclosure SHALL be unavailable when the message has no run link (human messages).

#### Scenario: Footprint expands to cards
- **WHEN** a channel member expands an agent message's work disclosure
- **THEN** the linked run's tool-call cards render with arguments, results, and latency
