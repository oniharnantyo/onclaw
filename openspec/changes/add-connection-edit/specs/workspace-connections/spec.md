## ADDED Requirements

### Requirement: Connection attachment management

An authorized user SHALL set the complete set of agents attached to a connection from the connection itself: the request names the desired agent set, the system computes the difference against the current attachment, and exactly the affected agents change — either every affected agent's allowlist is updated or none is. Attachment applied from the connection side SHALL be indistinguishable from attachment applied in the agent's configuration: it stores the connection's materialized server id for MCP-kind connections and the connection's own id for http-kind connections. Requests naming an agent that does not exist in the workspace SHALL be rejected with a validation error and change nothing.

#### Scenario: Attaching several agents in one save

- **WHEN** an authorized user saves a connection's edit surface with two agents selected and one deselected
- **THEN** exactly the two selected agents' allowlists gain the connection and the deselected agent's loses it in one atomic update, and the connection view lists the new attached agents

#### Scenario: Http-kind attachment stores the connection id

- **WHEN** an agent is attached to an http-kind connection
- **THEN** the agent's allowlist carries the connection's own id, and the connection's declared verbs become that agent's tools on its next run

#### Scenario: Unknown agent rejected atomically

- **WHEN** an attachment update names an agent id that does not exist in the workspace
- **THEN** the request is rejected with a validation error and no agent's attachment changes

#### Scenario: Member cannot manage attachment

- **WHEN** a user without `integrations.write` attempts an attachment update
- **THEN** the request is rejected with a permission error

### Requirement: Connect completion offers agent selection

After a connection activates, the connect flow SHALL present the workspace's agents for attachment regardless of auth kind: token-auth connects present the selection as the completion step of the connect dialog, and OAuth connects present it when the consent callback activates the connection. Skipping the selection SHALL be allowed — the connection stays active with no agents attached, and attachment remains available from the connection's edit surface.

#### Scenario: Token connect completion lists agents

- **WHEN** a token-auth connect succeeds and the workspace holds multiple agents
- **THEN** the completion step lists every agent with an independent toggle, and the toggles saved there take effect as ordinary attachment

#### Scenario: OAuth callback success lands on selection

- **WHEN** an OAuth connect returns to the integrations surface with a success status
- **THEN** the activation lands on the new connection's agent-selection surface without repeating the connect flow

#### Scenario: Skipping selection attaches no one

- **WHEN** the user dismisses the completion selection without toggling any agent
- **THEN** the connection remains active with no agents attached and no error

### Requirement: Connection token replacement

Token-auth and http-kind connections SHALL accept a replacement access token from an authorized user: the system SHALL verify the candidate token with the recipe's probe before storing, replace the stored token in place only on success, and preserve the connection's identity, origin, access level, and every agent attachment. A failed probe SHALL keep the previously stored token unchanged and surface the probe failure. An empty submission SHALL leave the stored token untouched. OAuth-kind connections SHALL NOT accept token replacement — reauthorization is their credential-rotation path.

#### Scenario: Replacement swaps the token in place

- **WHEN** an authorized user submits a valid replacement token for a connected service
- **THEN** the stored token is replaced, subsequent calls authenticate with the new token, the last-4 hint updates, and every attached agent keeps its tools without re-attachment

#### Scenario: Failed probe keeps the stored token

- **WHEN** the submitted replacement token fails the recipe's probe
- **THEN** nothing is stored, the previously stored token keeps authenticating, and the probe failure is surfaced so the user can correct the token

#### Scenario: Empty submission changes nothing

- **WHEN** an edit is saved with the token field left empty
- **THEN** the stored token is kept unchanged and the save still applies any attachment changes

#### Scenario: OAuth-kind replacement refused

- **WHEN** a token replacement is requested for an OAuth-kind connection
- **THEN** the request is rejected with an error directing to reauthorization

#### Scenario: Member cannot replace a token

- **WHEN** a user without `integrations.write` attempts a token replacement
- **THEN** the request is rejected with a permission error
