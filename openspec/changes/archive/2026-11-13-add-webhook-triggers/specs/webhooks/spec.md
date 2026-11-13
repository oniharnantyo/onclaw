# webhooks — Delta Spec

## Purpose

The third inbound trigger surface: external systems (GitLab, GitHub, Stripe, CI, automation tools) start agent or group work by delivering an HTTP webhook to the running gateway, authenticated by per-endpoint credentials, with every delivery recorded and inspectable.

## ADDED Requirements

### Requirement: Receiver lifecycle

An active webhook listener SHALL run while the gateway is Running, bound to the configured address (loopback default; a non-loopback bind is an explicit configuration act). A paused gateway SHALL answer deliveries with 503 and a Retry-After hint rather than holding or dropping them; a stopped gateway or closed app SHALL close the listener with bounded teardown that never blocks app startup or shutdown. Listener configuration changes SHALL take effect without an app restart.

#### Scenario: Delivery while paused

- **WHEN** a valid, authenticated delivery arrives while the gateway is paused
- **THEN** the receiver responds 503 with a Retry-After hint, records the delivery as paused, fires nothing, and the sender's retry policy re-delivers later

#### Scenario: App closed

- **WHEN** the app is closed
- **THEN** the listener closes (deliveries during teardown get connection errors, the normal "receiver down" condition every sender already handles)

### Requirement: Endpoints target an agent or a group

A webhook endpoint SHALL carry a unique name, a target resolving to either an existing agent or an existing group, a required prompt template, an enabled flag, and at least one credential. An agent-targeted delivery SHALL start a fresh titled session per delivery (titled `<endpoint name> — <delivery time>`), submit the prompt template with the payload appended as its payload block, and stamp the turn with the webhook origin so hooks and prompt context treat it as an autonomous run. A group-targeted delivery SHALL append the rendered text as an ordinary user message into the group's shared transcript through the group's normal send path — mentions route to named members, untargeted messages route to the lead and task machinery, and every member turn spawned reads as a user turn. A delivery whose target no longer resolves SHALL answer 4xx (misconfiguration, per sender-retry semantics) and be recorded as failed.

#### Scenario: Agent target fires a fresh session

- **WHEN** a delivery arrives for an enabled endpoint targeting an agent
- **THEN** a new session is created and titled for this delivery, the turn is submitted with the rendered prompt and webhook origin, and the response is 202 once the turn is accepted

#### Scenario: Group target speaks as the user

- **WHEN** a delivery arrives for an enabled endpoint targeting a group, with a template containing an `@Name` mention
- **THEN** one user-authored transcript row appears with the rendered text, the mentioned member's turn runs, and no transcript artifact marks the message as machine-authored

#### Scenario: Target deleted

- **WHEN** a delivery arrives for an endpoint whose target agent or group has been deleted
- **THEN** the receiver answers 4xx, records the delivery as failed with the reason, and starts nothing

### Requirement: Payload block carries the body only

A delivery's body SHALL ride the prompt or message as a fenced JSON block when it parses as JSON, or a fenced raw-text block when it does not. Bodies beyond a configured size cap SHALL be refused with 413 and recorded. Request headers SHALL NOT be included in the payload block (v1).

#### Scenario: Non-JSON sender

- **WHEN** a delivery's body is form-encoded text
- **THEN** the turn's payload block contains the raw body text and the delivery is processed normally

#### Scenario: Oversized body

- **WHEN** a delivery exceeds the size cap
- **THEN** the receiver answers 413, records the delivery as too-large, and starts nothing

### Requirement: Authenticated by per-endpoint credentials

Each endpoint SHALL hold an ordered set of verifiers; saving requires at least one. The receiver SHALL support five verifier engines: URL path token, static header equality, raw-body HMAC (configurable header, SHA-256/SHA-1, hex/base64 encoding, optional prefix), Standard Webhooks HMAC-SHA256 (`webhook-id`/`webhook-timestamp`/`webhook-signature` over `{id}.{timestamp}.{body}`, `whsec_`-prefixed key), and timestamp-composite HMAC (Stripe and Slack/Zoom compositions). Signature verification SHALL use constant-time comparison, and a Standard Webhooks verifier SHALL reject a `webhook-timestamp` outside a bounded recency window. A request SHALL be accepted when any configured verifier validates it; when a request carries a signature header the endpoint expects, a failed verification SHALL answer 401 with no fall-through to weaker credentials. Sender preset configs (GitLab, GitHub, Stripe, Slack, Zoom, Shopify, Linear) SHALL be offered as one-field templates over these engines, and a manual builder SHALL expose the engines' raw fields. Verifier secrets SHALL follow the channel-credential precedent: returned to no frontend read (presence and a short derived hint at most), a blank secret save keeps the stored secret, and a saved secret is replaceable and regenerable.

#### Scenario: GitLab signing token accepted

- **WHEN** a delivery arrives with `webhook-signature` verifying against the endpoint's Standard Webhooks secret over the exact raw body
- **THEN** the delivery is authenticated and dispatched

#### Scenario: Tampered signature fails closed

- **WHEN** a delivery carries a `webhook-signature` that does not verify, on an endpoint that also has a path token configured
- **THEN** the receiver answers 401, records an auth-failed delivery, and does not accept the delivery via the path token

#### Scenario: Replay refused

- **WHEN** a delivery's Standard Webhooks `webhook-timestamp` is hours old with a valid signature
- **THEN** the receiver answers 401 and records the refusal as an auth failure

#### Scenario: Path token for bare senders

- **WHEN** an unsigned request arrives whose path carries an endpoint's correct token
- **THEN** the delivery is authenticated

### Requirement: Retry dedupe on sender delivery ids

When a sender provides a stable delivery id (the Standard Webhooks `webhook-id` family, stable across that sender's retries), a repeat delivery with the same id for the same endpoint within the log's retention SHALL answer 200 with a duplicate marker and SHALL NOT fire a second session or message. The duplicate delivery SHALL be recorded. Senders that provide no id (bare scripts) SHALL NOT be deduplicated.

#### Scenario: GitLab retries after a dropped response

- **WHEN** GitLab re-sends a webhook with the same `webhook-id` after the first delivery already fired a session
- **THEN** the repeat answers 200 duplicate without creating a second session, and both deliveries appear in the log

### Requirement: Deliveries are recorded and inspectable

Every delivery attempt SHALL append a bounded (oldest-evicted) record per endpoint: timestamp, HTTP status answered, outcome (accepted / auth-failed / paused / too-large / duplicate / failed / ran), the session id or transcript message id it produced, the verifier (preset label) that authenticated it, and the sender delivery id when present. The UI SHALL list an endpoint's recent deliveries with their outcomes and link accepted ones to the session or group they produced.

#### Scenario: Auth failure is diagnosable

- **WHEN** an integration silently fails because the secret was rotated on the sender side
- **THEN** the endpoint's delivery log shows the auth-failed entries with the answering status, next to earlier accepted deliveries

### Requirement: Agent-target completions notify; group deliveries surface in the room

When an agent-targeted delivery's turn reaches a terminal state, a best-effort desktop notification SHALL be enqueued (a webhooks category, routing to the fire session), mirroring the scheduled-run completion path. Group-targeted deliveries SHALL surface as transcript rows and delivery-log entries only. A failed or skipped notification MUST NOT affect the delivery's recorded outcome.

#### Scenario: Agent-target fire completes

- **WHEN** a webhook-fired session's turn completes away from the desk
- **THEN** a desktop notification for the webhooks category offers the session as its click target

#### Scenario: Group delivery stays in the room

- **WHEN** a group-targeted delivery's member turns run to completion
- **THEN** no completion notification is raised; the transcript and delivery log carry the outcome

### Requirement: Test delivery exercises the whole path

An endpoint SHALL offer a Test action: the backend composes a synthetic payload, signs it with the endpoint's own credentials, and delivers it through the real listener, so address binding, authentication, and dispatch are proven in one step. The test SHALL appear in the delivery log attributed as a self-test, and a failed test SHALL surface its error in the UI.

#### Scenario: Fresh endpoint smoke

- **WHEN** the user saves a new GitLab-preset endpoint and taps Test
- **THEN** a self-signed delivery authenticates, fires (or appends), and the log shows one accepted self-test entry

### Requirement: Channels-pane webhook management

The Channels pane SHALL gain a Webhooks section that lists endpoints with their enabled state and last-delivery outcome, and provides the add-endpoint wizard: basics (name, agent-or-group target picker, prompt template), the preset gallery with per-preset secret fields and a where-to-find-it hint, the generated URL with copy affordance, the verifier editor (add, edit, regenerate secrets, remove), the Test action, and the delivery log. All surfaces SHALL render legibly in both app themes. Secrets SHALL appear only once at creation or regeneration.

#### Scenario: Round-trip edit

- **WHEN** the user opens an existing GitHub-preset endpoint for editing and rotates its secret
- **THEN** the new secret is accepted, shown once, the old one stops authenticating, and the UI still shows the endpoint as GitHub-preset
