# web-app/shell Specification

## MODIFIED Requirements

### Requirement: Screen routing
Each user-facing screen SHALL be its own route: `/login` (session), `/c/:chatId` (agent chat, channel chat, or teammate direct message), `/agents`, `/cron`, `/runs`, `/welcome` for a zero-agent workspace, `/settings` and `/settings/:section` (workspace settings: workspace, providers, members, integrations, mcp, skills, keys, notifications), and `/admin` (instance administration for qualified master-tenant members: `/admin/workspaces` for tenant management, `/admin/accounts` for user management). The active workspace (tenant) SHALL NOT be part of the URL.

#### Scenario: Unknown chat identifier
- **WHEN** a visitor opens /c/ with an identifier that does not exist in the active workspace
- **THEN** the app redirects to the first agent's chat

#### Scenario: Zero-agent workspace
- **WHEN** the active workspace has no agents and the visitor opens a chat route (/ or /c/*)
- **THEN** the app routes to /welcome instead; the Agents, Cron, and Runs screens SHALL render normally in zero-agent workspaces with their empty states

#### Scenario: Unauthenticated access
- **WHEN** a logged-out visitor opens any route other than /login
- **THEN** the app redirects to /login

#### Scenario: Settings deep link
- **WHEN** a member opens /settings/keys directly
- **THEN** the keys section of workspace settings renders at that URL; /settings redirects to /settings/workspace
