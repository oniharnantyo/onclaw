# Spec Delta

## ADDED Requirements

### Requirement: Empty transcript greeting

An empty agent transcript SHALL render a centered identity greeting in place of the placeholder hint line: the agent's avatar above an animated greeting line ("How can I help you today?") above an identity line carrying the agent's name and the first line of its description when one exists. The identity SHALL resolve from the chat view's active agent — the same identity the chat header shows before a session exists — so a brand-new chat and a fresh session of the same agent render the same greeting. The entrance animation (avatar, greeting, identity in a short stagger) SHALL be suppressed under reduced motion, and all greeting surfaces SHALL ride the ported theme tokens so both themes render the block. When no agent identity applies — a group transcript, whose identity the chat header already carries, or an agent that cannot be resolved — the block SHALL render the greeting without the avatar and identity line rather than an identity mismatch.

#### Scenario: Fresh chat shows the agent's identity greeting

- **WHEN** a new chat is opened for an agent whose description is non-empty
- **THEN** the empty transcript shows the agent's avatar centered above the greeting line, above an identity line carrying the agent's name and the first line of its description

#### Scenario: Empty description degrades to the name alone

- **WHEN** the active agent has an empty description and its transcript is empty
- **THEN** the identity line carries the agent's name without a description segment, and the block renders no empty gap in its place

#### Scenario: Group transcript falls back to the generic greeting

- **WHEN** a group transcript with no messages is open
- **THEN** the greeting renders centered without an avatar or identity line, and the group's identity stays in the chat header

#### Scenario: Reduced motion stills the entrance

- **WHEN** reduced motion is enabled and an empty agent transcript renders the greeting
- **THEN** the avatar, greeting, and identity line render fully visible without entrance animation

#### Scenario: Loading history never flashes the greeting

- **WHEN** the chat view switches to a session whose transcript history is still loading
- **THEN** the transcript shows the loading skeleton, and the identity greeting MUST NOT render before the history lands
