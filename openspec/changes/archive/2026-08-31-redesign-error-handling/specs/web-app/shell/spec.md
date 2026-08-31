## MODIFIED Requirements

### Requirement: Screen routing
Each user-facing screen SHALL be its own route: `/login` (session), `/c/:chatId` (agent chat, channel chat, or teammate direct message), `/agents`, `/cron`, `/runs`, `/welcome` for a zero-agent workspace, and `/admin` (instance administration for qualified master-tenant members: `/admin/workspaces` for tenant management, `/admin/accounts` for user management). The active workspace (tenant) SHALL NOT be part of the URL. A URL matching no route SHALL render a 404 error state in place (see the error-states capability), not a redirect to an arbitrary screen.

#### Scenario: Unknown chat identifier
- **WHEN** a visitor opens /c/ with an identifier that does not exist in the active workspace
- **THEN** the app renders a not-found error state in place instead of redirecting to another chat

#### Scenario: Unknown URL
- **WHEN** a visitor opens a URL matching no route
- **THEN** the app renders a 404 error state in place instead of redirecting to a random chat

#### Scenario: Zero-agent workspace
- **WHEN** the active workspace has no agents and the visitor opens a chat route (/ or /c/*)
- **THEN** the app routes to /welcome instead; the Agents, Cron, and Runs screens SHALL render normally in zero-agent workspaces with their empty states

#### Scenario: Unauthenticated access
- **WHEN** a logged-out visitor opens any route other than /login
- **THEN** the app redirects to /login
