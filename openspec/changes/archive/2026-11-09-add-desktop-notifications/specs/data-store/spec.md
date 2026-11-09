# data-store delta

## MODIFIED Requirements

### Requirement: Configuration persisted in the database

Agent definitions, provider settings, MCP server settings, hooks, gateway settings, and notification settings SHALL be stored in the SQLite database. An agent's prompt documents are the exception: they SHALL live as files in the agent's workspace directory, with the database row holding the slug, brief, prompt status, and the agent's stored procedural-avatar config (opaque JSON, empty when the avatar is derived from the id) but no document content. Every configuration change MUST be applied atomically and durably: a change is either fully persisted or not at all, and a crash between steps never leaves partial configuration.

#### Scenario: Config survives restart

- **WHEN** the user edits an agent's model and quits the app, then starts it again
- **THEN** the agent shows the edited model

#### Scenario: Document edit survives restart

- **WHEN** the user edits an agent's SOUL document and quits the app, then starts it again
- **THEN** the document file still holds the edited content

#### Scenario: Notification settings survive restart

- **WHEN** the user disables the Schedules notification category, quits the app, and starts it again
- **THEN** the Notifications pane shows the Schedules category still disabled
