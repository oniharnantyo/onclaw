## MODIFIED Requirements

### Requirement: Seeded workspace content
On boot after login, the app SHALL hydrate workspace identity, memberships, and roles from the API, and SHALL seed remaining domain data locally so every screen renders: named agents across providers and models (including at least one running, one idle, and one in error state), channels with primary agents, teammates with presence, populated threads (including cron-origin messages), cron schedules, run history, integrations, MCP servers, skills, and API keys. One seeded agent SHALL have a long session list (on the order of one hundred sessions) to exercise list rendering.

#### Scenario: First load
- **WHEN** the app boots after login
- **THEN** workspace identity/members/roles come from the API while agents/channels/threads/cron/runs/integrations/MCP/skills/keys render from seed data

#### Scenario: Long session list
- **WHEN** the user opens the seeded agent with a hundred sessions
- **THEN** the sidebar session list renders and remains interactable

### Requirement: Ephemeral mutations
Mutations in the integrated identity/tenancy domain (workspace create/update, member add/role-change/remove/leave, profile/avatar) SHALL persist through the API; mutations in domains not yet integrated (agents, channels, threads, cron, runs, integrations, MCP, skills, keys, notifications) SHALL remain session-local and reload restores seed content for those domains.

#### Scenario: Reload resets data
- **WHEN** the user adds a member and edits an agent, then reloads
- **THEN** the member persists (API-backed) while the agent edit resets to seed; UI position is preserved

### Requirement: Replaceable data seam
Workspace identity, membership, and role data SHALL flow to screens through the same single data-access seam as seed data, backed by the API client; screens SHALL NOT import seed data for the integrated domain. Replacing remaining domains (agents, cron, runs, chat transport) with HTTP sources SHALL require no changes to screen or component code and SHALL NOT change user-visible behavior.

#### Scenario: Source swap is invisible
- **WHEN** a remaining domain moves to the API later
- **THEN** screens render and behave identically, and no component imports seed data for integrated domains
