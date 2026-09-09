# agent-channels delta

## MODIFIED Requirements

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
