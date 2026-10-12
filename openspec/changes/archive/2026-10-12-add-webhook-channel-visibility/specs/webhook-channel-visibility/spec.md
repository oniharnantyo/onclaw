# Spec Delta

## Purpose

Makes channel-targeted webhook runs visible where they land, natively: the webhook-delivered reply renders in the channel room through the channel's own message feed — the same surface summon and gateway replies already post to — loaded with the room's history and delivered live while the room is open; the workspace run history lists webhook-origin runs beside scheduled ones; and unattended-run transcripts stay out of the human-only session index.

## ADDED Requirements

### Requirement: Webhook replies visible in the channel room

When a channel-targeted delivery's run completes, its reply SHALL be visible in the channel room: the room SHALL load the channel's message feed with its history on open, subscribe to its live event stream while open, and render feed entries as conversation messages — an agent-authored reply rendered as the invoked agent's message. The feed is the channel's shared delivery surface (summon auto-posts and gateway posts ride it too); no second surface is created for webhooks. A delivery whose run produced no reply text posts nothing.

#### Scenario: Reply visible after a delivery

- **WHEN** a member opens a channel after a webhook run completed
- **THEN** the run's reply renders in the room as the invoked agent's message, alongside the channel's other messages

#### Scenario: Reply arrives live

- **WHEN** a delivery's run completes while the member has the channel open
- **THEN** the reply appears in the room without a reload

### Requirement: Workspace webhook run history

The run history screen SHALL list webhook-origin runs beside scheduled ones: every submitted delivery workspace-wide, newest first, each row carrying the endpoint name (as the trigger), the bound agent, the run's live status with duration and token usage when finished, and a transcript opening through the same run-transcript hydration. The listing SHALL be derived from the delivery-log run records — no second write path — and SHALL be bounded like the log. The feed SHALL be gated by integrations write (the webhooks pane's trust tier); a member without it SHALL see the run history without webhook rows rather than an error.

#### Scenario: Webhook runs listed in run history

- **WHEN** an admin opens the run history after a webhook run completed
- **THEN** the run appears with a `webhook · <endpoint>` trigger chip, its agent, its terminal status with duration and tokens, and clicking it opens the run's transcript

#### Scenario: Feed hidden from members without integrations access

- **WHEN** a member without integrations write opens the run history
- **THEN** scheduled runs render normally and no webhook rows or errors appear
