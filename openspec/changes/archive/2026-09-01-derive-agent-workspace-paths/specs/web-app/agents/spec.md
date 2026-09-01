# web-app/agents (delta)

## ADDED Requirements

### Requirement: Agent slug is create-only
The wizard SHALL expose the slug as an editable, auto-suggested control in create mode only. In edit mode the slug SHALL be displayed read-only (as identity text, not an input), the update payload SHALL omit the slug, and any slug a client still sends SHALL be ignored by the server (see the agents capability).

#### Scenario: Edit mode shows slug read-only
- **WHEN** the agent wizard opens in edit mode
- **THEN** the slug appears as read-only identity text and no slug input is rendered

#### Scenario: Update payload omits slug
- **WHEN** the wizard saves an edit
- **THEN** the PATCH payload carries no slug field
