# web-app/data-layer Specification

## Purpose

The mock-era data contract: seeded workspace content, in-memory mutation semantics, UI-position persistence, the simulated agent runtime, and the read/write seam that the future Go API will replace.

## Requirements

### Requirement: Seeded workspace content
On first load the app SHALL present two seeded workspaces with full domain data: named agents across providers and models (including at least one running, one idle, and one in error state), channels with primary agents, members, and unread counts, teammates with presence, populated threads (including cron-origin messages), cron schedules, run history, members with roles, integrations, MCP servers, skills, and API keys. One seeded agent SHALL have a long session list (on the order of one hundred sessions) to exercise list rendering.

#### Scenario: First load
- **WHEN** the app boots with empty local storage
- **THEN** both workspaces are fully populated and the app opens on the default workspace's first agent chat

#### Scenario: Long session list
- **WHEN** the user opens the seeded agent with a hundred sessions
- **THEN** the sidebar session list renders and remains interactable

### Requirement: Ephemeral mutations
All data mutations (sending messages, editing agents, changing settings, creating or deleting workspaces, schedules, and keys) SHALL live only in memory for the session. Reloading SHALL restore the seed content exactly; only UI position (workspace, route, chat, panel state) persists across reloads.

#### Scenario: Reload resets data
- **WHEN** the user creates an API key and reloads
- **THEN** the key is gone and the seed state is restored, while the active workspace and chat are preserved

### Requirement: Simulated agent runtime
Until a real backend exists, agent behavior SHALL be simulated: typing indicator before each reply, per-message streaming caret that clears on completion or cancel, reply latency in the sub-two-second range, and mention-driven agent identity in channels. The simulation MUST NOT alter the message, session, or run shapes that the UI renders.

#### Scenario: Streaming caret
- **WHEN** a simulated reply lands
- **THEN** its text renders with a blinking caret that disappears once streaming completes

### Requirement: Replaceable data seam
Workspace, thread/session, and run data SHALL be consumed by screens through a single data-access layer. Replacing the in-memory seed source with an HTTP-backed source SHALL require no changes to screen or component code, and SHALL NOT change any user-visible behavior defined in the other web-app capabilities.

#### Scenario: Source swap is invisible
- **WHEN** the data source is replaced with an equivalent API-backed implementation
- **THEN** every screen renders and behaves identically, and no component imports seed data directly
