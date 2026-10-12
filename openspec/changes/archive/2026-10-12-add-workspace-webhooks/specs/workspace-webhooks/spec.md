# Spec Delta

## Purpose

Lets workspace admins create standalone webhook endpoints — outside the connection/recipe catalog — that turn verified JSON deliveries into agent runs: each endpoint carries its own credential and signature scheme, a save-time-validated filter expression over the payload, and a binding to an agent plus a thread or channel, managed from a dedicated Settings surface.

## ADDED Requirements

### Requirement: Webhook endpoint lifecycle

An authorized workspace admin SHALL be able to create standalone webhook endpoints scoped to the workspace, each declaring: a name; an auth credential and signature scheme (plain token, body HMAC-SHA256, or timestamped HMAC); an event source (a request header name, a payload field path, or a fixed value); a delivery-id source (a request header name); an optional filter expression; an optional turn template; and a target binding of one agent plus a thread or a channel. Creating an endpoint without a caller-supplied secret SHALL generate one, encrypted at rest and revealed exactly once on create and on rotate; a caller-supplied (bring-your-own) secret SHALL be accepted and never re-revealed. An endpoint SHALL be pausable and resumable, and disabling or deleting it SHALL stop ingestion while leaving other state intact. Every write SHALL require `integrations.write`.

#### Scenario: Create with generated secret

- **WHEN** an admin creates an endpoint named "gitlab-mr-review" without supplying a secret, binding the Atlas agent to the `#code-review` channel
- **THEN** a secret is generated, stored encrypted, returned in the create response exactly once, and the endpoint's ingest URL is displayed for configuration at the sender

#### Scenario: Create with bring-your-own secret

- **WHEN** an admin creates an endpoint and supplies their own secret chosen in the sender's dashboard
- **THEN** the secret is stored encrypted and never displayed again; a last-4 hint is shown thereafter

#### Scenario: Rotate invalidates immediately

- **WHEN** an admin rotates an endpoint's credential
- **THEN** a replacement secret is generated and shown once, and deliveries signed with the prior secret fail verification from that moment

#### Scenario: Paused endpoint ignores deliveries

- **WHEN** a delivery arrives for a paused endpoint
- **THEN** it is acknowledged without verification side effects and no agent run occurs

### Requirement: Verified, deduplicated ingress

The public ingest path for standalone endpoints SHALL accept only JSON request bodies and SHALL authenticate each delivery with the endpoint's declared scheme: a plain-token header compared in constant time; an HMAC-SHA256 hex digest over the raw body; or a timestamped HMAC — `t=<unix>,v1=<hex>` over `{timestamp}.{body}` — whose timestamp SHALL be within the acceptance skew of the server clock. A configured endpoint whose required signature material is absent or invalid SHALL be rejected without side effects, indistinguishably from an unknown endpoint (no enumeration help). Each endpoint SHALL deduplicate deliveries by its declared delivery-id source within a retention window, acknowledging provider retries without a second run; for the timestamped scheme, a repeated valid signature within the skew window SHALL also be rejected as a replay, using the same durable store. Verified deliveries SHALL be enqueued rather than processed inline, and the endpoint SHALL enforce a body-size cap and a per-endpoint request rate, answering excess with `429` and `Retry-After`.

#### Scenario: GitLab plain token accepted

- **WHEN** a delivery arrives with `X-Gitlab-Token` matching the endpoint's secret and the endpoint's scheme is plain token
- **THEN** it is verified in constant time and enqueued for processing

#### Scenario: Timestamped HMAC rejects stale capture

- **WHEN** a delivery signed with the timestamped scheme is replayed with a timestamp older than the acceptance skew
- **THEN** it is rejected with no run and no store writes

#### Scenario: Timestamped replay within the window

- **WHEN** the identical timestamped delivery — same timestamp, body, and signature — arrives twice within the skew window
- **THEN** the second is rejected as a replay

#### Scenario: Invalid signature is unenumerable

- **WHEN** a delivery arrives with a wrong signature, or for an endpoint id that does not exist
- **THEN** both receive the same generic error status with no distinguishing detail

#### Scenario: Duplicate delivery id acknowledged once

- **WHEN** the same delivery id is delivered twice for an endpoint
- **THEN** the second is acknowledged without producing a second agent run

#### Scenario: Oversized body rejected

- **WHEN** a request body exceeds the endpoint's body cap
- **THEN** it is rejected before signature processing

### Requirement: Filter expression over the payload

Each endpoint MAY declare a filter expression evaluated against the parsed JSON payload plus the resolved event value. The expression language SHALL be limited to comparisons, boolean logic, and field access over that context — no function calls, no I/O. The expression SHALL be compiled and validated when the endpoint is saved; an invalid expression SHALL be rejected with the parser's message and the endpoint left unchanged. A delivery whose expression evaluates to false, or fails to evaluate (missing field), SHALL be acknowledged `200` with an explicit ignored reason and SHALL NOT produce an agent run. The management surface SHALL provide a dry-run that evaluates a supplied sample payload against the expression and reports match, ignored, or error without side effects.

#### Scenario: Matching delivery triggers a run

- **WHEN** a verified delivery's payload satisfies `event == "merge_request" && project.path_with_namespace == "team/api"`
- **THEN** the delivery proceeds to enqueue and the agent runs

#### Scenario: Non-matching delivery ignored

- **WHEN** a verified delivery's payload does not satisfy the endpoint's expression
- **THEN** the delivery is acknowledged `200` with an ignored reason and no agent run occurs

#### Scenario: Invalid expression rejected at save

- **WHEN** an admin saves an endpoint whose expression does not parse
- **THEN** the save is rejected with the parse error and the prior expression remains in force

#### Scenario: Dry-run reports the outcome

- **WHEN** an admin submits a sample payload to the dry-run for an endpoint
- **THEN** the result — match, ignored, or evaluation error — is reported without enqueuing anything

### Requirement: Event-to-turn routing

A verified, deduplicated delivery that passes the filter SHALL render a turn input for the bound agent in the bound thread or channel. The resolved event — from the endpoint's declared event source — and the payload SHALL be embedded as labeled data, quoted and delimited, never as bare instructions. When no template is declared, the turn SHALL be the payload as labeled, truncated JSON. When a template is declared, its placeholders SHALL be validated against a sample payload at save time and rendering SHALL fail closed on a missing field, dropping the delivery with an explicit error rather than emitting a half-filled turn. The run SHALL be attributed to a service-authority identity naming the webhook endpoint and resolved event; its tool use SHALL be gated exactly as other no-requesting-user runs are gated (read-tier freely, write-tier through the approval flow).

#### Scenario: Default turn carries labeled payload

- **WHEN** a verified delivery passes the filter for an endpoint with no template
- **THEN** the bound agent receives a turn whose input names the endpoint and event and embeds the payload as labeled, truncated JSON

#### Scenario: Template fails closed

- **WHEN** a delivery arrives whose payload is missing a field an endpoint's template references
- **THEN** the delivery is dropped with an explicit error and no partial turn reaches the agent

#### Scenario: Service attribution on the run

- **WHEN** the bound agent processes a delivery for endpoint "gitlab-mr-review"
- **THEN** the run's metadata and trace name the endpoint and resolved event instead of a requesting user

### Requirement: Delivery log

Each endpoint SHALL retain a bounded history of recent deliveries recording, per delivery: the resolved event, the outcome (run submitted with its run id, filtered out, deduplicated, rejected, or failed), and a timestamp. The management surface SHALL present this history per endpoint so an admin can answer "did the sender's event reach the agent, and if not, why not" without server logs.

#### Scenario: Filtered delivery visible in the log

- **WHEN** a delivery is filtered out for endpoint "gitlab-mr-review"
- **THEN** the endpoint's delivery log shows the delivery with the ignored outcome and its resolved event

#### Scenario: Run linked from the log

- **WHEN** a delivery results in an agent run
- **THEN** the log entry carries the run id linking to the run

### Requirement: Webhooks settings surface

Workspace settings SHALL present a dedicated Webhooks section listing the workspace's webhook endpoints: standalone endpoints created and fully managed here, and connection-bound webhooks surfaced read-in from their connections. Creating and editing SHALL use structured labeled fields — name, scheme, credential, event source, delivery-id source, filter expression (monospace input with save-time validation), optional template, and the agent + thread/channel target binding — never a raw-JSON textarea for configuration. The secret SHALL appear only at reveal-once moments (create, rotate) with a last-4 hint thereafter. The section SHALL offer rotate, enable/pause, delete, the dry-run tester, and the per-endpoint delivery log, and SHALL render the states the design contract requires (loading, empty, error, success) from 360px to 1920px without horizontal overflow.

#### Scenario: Standalone endpoint managed end to end

- **WHEN** an admin opens the Webhooks section, creates an endpoint, and rotates its secret later
- **THEN** both secrets are shown exactly once at their moments, and the list reflects the new last-4 hint

#### Scenario: Connection webhooks surfaced read-in

- **WHEN** the workspace's GitLab connection has webhooks enabled through Integrations
- **THEN** the Webhooks section lists that webhook alongside standalone endpoints, linking to its connection for management

#### Scenario: Empty and loading states

- **WHEN** a workspace has no webhook endpoints and the section loads
- **THEN** the empty state explains what webhooks do and offers creation, and loading renders without layout shift
