## MODIFIED Requirements

### Requirement: Screen routing
Each user-facing screen SHALL be its own route: `/login` (session), `/c/:chatId` (agent chat, channel chat, or teammate direct message), `/agents`, `/cron`, `/runs`, `/welcome` for a zero-agent workspace, and `/admin` (instance administration, tenant management for qualified master-tenant members). The active workspace (tenant) SHALL NOT be part of the URL.

#### Scenario: Unknown chat identifier
- **WHEN** a visitor opens `/c/` with an identifier that does not exist in the active workspace
- **THEN** the app redirects to the first agent's chat

#### Scenario: Zero-agent workspace
- **WHEN** the active workspace has no agents and the visitor opens any chat route
- **THEN** the app routes to /welcome instead

#### Scenario: Unauthenticated access
- **WHEN** a logged-out visitor opens any route other than /login
- **THEN** the app redirects to /login
