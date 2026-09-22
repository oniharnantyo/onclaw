## Purpose

Lets agents react to service events: a connection can receive verified webhook deliveries from its provider and turn them into agent runs in a bound thread or channel — the push counterpart to the pull-only connection tools.

## ADDED Requirements

### Requirement: Webhook enablement

A connection whose recipe declares webhook support SHALL be enableable for webhooks by an authorized user: the system SHALL generate an HMAC secret (encrypted at rest, workspace-bound, displayed once), expose the workspace's ingest URL for configuration at the provider, and store a target binding — one agent plus a thread or channel — together with the selected subset of the recipe's event catalog. Disabling SHALL stop ingestion and leave the connection otherwise intact.

#### Scenario: Enable GitHub webhooks on a connection

- **WHEN** an admin enables webhooks on the workspace's GitHub connection, picks the Atlas agent with the workspace `#incidents` channel as target, and selects `pull_request.opened` and `issues.assigned`
- **THEN** an HMAC secret is generated and shown once, the ingest URL is displayed for configuration at the provider, and only the selected events are accepted for delivery

#### Scenario: Secret is shown once

- **WHEN** the admin revisits the webhooks section later
- **THEN** the secret appears only as a last-4 hint, with a rotate action that generates a replacement

### Requirement: Verified, deduplicated ingress

The ingest endpoint SHALL verify each delivery's signature against the connection's secret using the recipe-declared scheme and constant-time comparison, rejecting invalid signatures without side effects. It SHALL deduplicate by the provider's delivery id, rejecting replays, and SHALL enqueue verified deliveries rather than processing them inline, so a slow agent turn never blocks the provider's delivery expectations.

#### Scenario: Invalid signature rejected

- **WHEN** a delivery arrives with a signature that does not verify against the connection's secret
- **THEN** it is rejected with an error status, nothing is queued, and no agent turn occurs

#### Scenario: Replay deduplicated

- **WHEN** the same delivery id is delivered twice
- **THEN** the second delivery is acknowledged without producing a second agent turn

### Requirement: Event-to-turn routing

A verified, deduplicated delivery for a selected event SHALL render the provider payload through the recipe's template into a turn input for the bound agent in the bound thread or channel. Event content SHALL be embedded as labeled data — quoted and delimited — never as bare instructions, and the rendered turn SHALL name the connection and event that triggered it.

#### Scenario: PR opened triggers a review turn

- **WHEN** a verified `pull_request.opened` delivery arrives for a connection bound to Atlas in `#incidents`
- **THEN** Atlas receives a turn in `#incidents` whose input contains the event's rendered data — repository, PR number, title, and author — labeled as GitHub event content

#### Scenario: Unselected events ignored

- **WHEN** a verified delivery arrives for an event type the connection did not select
- **THEN** it is acknowledged without producing an agent turn

### Requirement: Service-authority attribution

Event-triggered runs SHALL be attributed to a service-authority identity — naming the connection and event — in run metadata and traces, so they are distinguishable from user-requested runs. V1 event runs SHALL start from the recipe's read-flavored default event set; write-flavored reactions through tools ride the existing tool gates, and tiered gating is the authority-gate change's scope.

#### Scenario: Trace distinguishes a webhook run

- **WHEN** Atlas processes a `pull_request.opened` delivery
- **THEN** the run's metadata and trace show the service authority (connection and event) instead of a requesting user, alongside the normal agent and workspace identity

#### Scenario: Write reactions ride existing gates

- **WHEN** an event-triggered run attempts a tool call outside any allowed set configured for the agent
- **THEN** the call is gated exactly as it would be in a user-requested run
