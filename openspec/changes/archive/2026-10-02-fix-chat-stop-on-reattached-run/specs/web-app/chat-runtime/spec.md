# Spec Delta — web-app/chat-runtime

## MODIFIED Requirements

### Requirement: Live cancel
The stop control during a live turn SHALL cancel the server-side run, whether the client started the turn in this view or is following a run it re-attached to (page reload mid-run, another tab, or a conflict-queued send's catch-up stream). The runtime SHALL address the native session-scoped cancel endpoint using the in-flight turn's minted response identity when one was captured from the stream, and SHALL fall back to addressing the run by the chat's bound server session when no response identity is held — the endpoint is session-scoped, so the bound session alone is sufficient addressing. The stop SHALL also detach the client from the followed stream, and the running state SHALL stay cleared: subsequent events from a detached stream SHALL NOT re-assert the running indicator or the stop control. Cancelling SHALL leave partial text and any completed tool cards in the transcript — the streamed turn tail SHALL remain rendered in the live transcript through the run's terminal state, without a reload. Stopping a followed run that carries a conflict-queued send SHALL NOT auto-dispatch that send; the queued message stays in the transcript for the user to resend.

#### Scenario: Stop stops the server run
- **WHEN** the user presses stop while a live turn is streaming
- **THEN** the native cancel endpoint is called for the in-flight turn and no further reply text arrives after the stream ends

#### Scenario: Partial work preserved
- **WHEN** a live turn is cancelled after partial text and a completed tool call
- **THEN** the partial text and the tool card remain in the transcript

#### Scenario: Stop reaches a followed run
- **WHEN** the user presses stop while the client is following a run it did not start in this view (the page reloaded mid-run, or the attach came from a conflict queue)
- **THEN** the native cancel endpoint is called addressing the bound session and the run stops producing events

#### Scenario: Running state stays cleared
- **WHEN** the user presses stop on a followed run
- **THEN** the composer returns to send immediately and stays in the send state — no subsequent stream event restores the running indicator or the stop control

#### Scenario: Followed run tail survives stop
- **WHEN** a stop is pressed during a followed run and the run later reaches its terminal state
- **THEN** every part the turn streamed (tool cards, reasoning, text) remains rendered in the live transcript without a reload

#### Scenario: Stop during a conflict-queued catch-up holds the queued send
- **WHEN** the user presses stop while the client follows a run under a conflict-queued send
- **THEN** the queued send is not redispatched and remains in the transcript for the user to resend
