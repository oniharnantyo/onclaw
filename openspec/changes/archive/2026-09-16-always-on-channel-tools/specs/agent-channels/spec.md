## MODIFIED Requirements

### Requirement: Channel history tool
Agents in channel runs SHALL have a `channel.history` tool that reads older feed messages by backward cursor pagination with attributed authorship, in the same format as the catch-up tail, bounded per call. The tool SHALL be always active in channel runs: workspace tool settings SHALL NOT disable it, and a stored disabled row SHALL NOT remove it from a channel run's toolset.

#### Scenario: Deep read
- **WHEN** an agent needs context older than the tail window
- **THEN** it pages back through `channel.history` and receives attributed messages

#### Scenario: Workspace settings cannot withdraw it
- **WHEN** the workspace holds a disabled settings row for `channel.history` and an agent runs in a channel
- **THEN** the run's toolset still includes `channel.history`

### Requirement: Channel post tool
Agents in channel runs SHALL have a `channel.post` tool that publishes a message to the feed mid-run through the chokepoint. The tool call SHALL be subject to `pre_tool_use` hooks like any tool call, and the agent's pending run summary SHALL be backfilled onto the posted message when the run finishes. The tool SHALL be always active in channel runs: workspace tool settings SHALL NOT disable it, and a stored disabled row SHALL NOT remove it from a channel run's toolset.

#### Scenario: Mid-run interjection
- **WHEN** an agent posts via `channel.post` mentioning a specialist agent before finishing
- **THEN** the message appears in the feed immediately and the mentioned agent is summoned under the loop caps

#### Scenario: Workspace settings cannot withdraw it
- **WHEN** the workspace holds a disabled settings row for `channel.post` and an agent runs in a channel
- **THEN** the run's toolset still includes `channel.post`
