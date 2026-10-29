# agent-runtime Specification

## Purpose

Execute agent conversations on the eino runtime: select a configured provider and model, stream each turn's output incrementally, account for context-window usage, and compact long sessions so chats can continue indefinitely.

## Requirements

### Requirement: Provider registry

The runtime SHALL execute turns through providers configured in settings (each with its API credential and available models). An agent MUST reference a configured provider and model to run. Selecting an unconfigured or unreachable provider MUST surface a clear error to the chat UI instead of silently failing.

Each turn SHALL execute on the provider's native agentic message protocol — content blocks covering reasoning, multimodal input, and tool calls — rather than a legacy flat message schema. A provider whose endpoint does not implement the protocol its family requires SHALL be surfaced as unavailable for the turn with a clear reason, not attempted and failed mid-turn.

Each turn SHALL resolve its effective model from the session's model override when one is set, otherwise the agent's configured model, and SHALL apply the session's effective reasoning effort — session override, else agent default, else none — through the provider's native reasoning parameter whenever the model supports one. A model override MUST be rejected with a clear per-turn error when the provider exposes a model listing that does not include it (providers that allow free-form ids may accept unknown models). An effort MUST be rejected with a clear per-turn error when the selected model's capability set does not include it, rather than being silently dropped or sent anyway.

#### Scenario: Turn on a configured provider

- **WHEN** the user sends a message to an agent whose provider and model are configured and reachable
- **THEN** the agent responds using that model

#### Scenario: Unreachable provider

- **WHEN** the configured provider cannot be reached
- **THEN** the session records an error event and the chat UI shows the failure for that turn

#### Scenario: Session model override wins

- **WHEN** a session has a model override set from the composer while its agent is configured with a different model
- **THEN** the turn executes on the session's overridden model

#### Scenario: Session effort reaches the provider

- **WHEN** a session has an effort set for a model that supports reasoning effort
- **THEN** the provider request carries that effort as its native reasoning parameter

#### Scenario: Unsupported effort rejected

- **WHEN** a session's effort is not in the selected model's supported set
- **THEN** the turn fails with a clear error event in the transcript instead of running at some other effort

#### Scenario: Endpoint without the required protocol is unavailable

- **WHEN** an agent's configured provider endpoint does not implement the agentic protocol its provider family requires
- **THEN** the turn fails with a clear unavailability reason before any model request is made

### Requirement: Streamed turns

A turn's output SHALL be delivered incrementally to the UI as it is produced — assistant text deltas, reasoning, and tool activity — rather than appearing only at completion. The final transcript MUST be persisted regardless of whether the client stays connected.

Each tool activity's terminal record SHALL carry the call's wall-clock duration, measured from the moment the call was announced as running to the moment its result landed, so a reloaded transcript shows the same latency the live view did. Records persisted before durations were recorded carry none, and the runtime MUST NOT fabricate one for them.

#### Scenario: Token-by-token rendering

- **WHEN** an agent is producing a response
- **THEN** the chat view renders partial output as it arrives

#### Scenario: Disconnected client does not lose the turn

- **WHEN** the web client disconnects mid-turn and reconnects after completion
- **THEN** the completed turn is present in the session transcript

#### Scenario: Tool duration persists

- **WHEN** a tool call completes and the session is later reloaded
- **THEN** the call's terminal record carries the duration observed by the runtime and the reloaded card shows the same latency as the live card did

### Requirement: Turn cancellation

The runtime SHALL support cancelling an in-flight turn for a session through a bound call. Cancellation SHALL stop further model and tool activity for that turn, persist a terminal cancelled outcome with the partial output retained in the transcript, and release the session so the next queued or new message dispatches normally. Cancelling a turn paused on a pending approval MUST discard the run and clear the pending approval. Invoking cancellation when no turn is in flight MUST be safe and MUST NOT disturb session state.

#### Scenario: Cancel mid-stream

- **WHEN** the user cancels a turn while text is streaming
- **THEN** streaming stops, the partial text remains in the transcript with a terminal cancelled outcome recorded, and the session accepts a new send

#### Scenario: Cancel releases the queue

- **WHEN** messages are queued behind the cancelled turn
- **THEN** the next queued message dispatches automatically after the cancellation

#### Scenario: Cancel with no active turn

- **WHEN** cancellation is invoked while the session has no turn in flight
- **THEN** the call completes safely without altering the transcript or session state

### Requirement: Multi-turn context

Within a session, each turn SHALL include the session's prior context so the agent can refer to earlier messages. Context MUST persist across app restarts.

#### Scenario: Follow-up question

- **WHEN** the user asks a follow-up that references an earlier message in the same session
- **THEN** the agent's answer is consistent with that earlier message

### Requirement: Context-window accounting

The runtime SHALL track token usage for the session's current context and expose it to the UI — used tokens relative to the model's context limit, plus the last completed turn's input and output token counts — so the chat view can display context fill state and the last turn's cost.

#### Scenario: Context ring reflects usage

- **WHEN** turns accumulate in a session
- **THEN** the context indicator in the chat view shows growing usage up to the model's limit

#### Scenario: Last-turn usage exposed

- **WHEN** a turn completes
- **THEN** the exposed context usage includes that turn's input and output token counts alongside the used total and the limit

### Requirement: Compaction

When a session's context approaches the model's limit, the runtime SHALL compact the session: summarize older history into a checkpoint, persist it, and continue the session within the limit. The user MUST NOT lose the visible transcript; compaction affects the model's working context, not the stored history.

#### Scenario: Long session continues

- **WHEN** a session's context exceeds the compaction threshold
- **THEN** the runtime compacts, the transcript in the UI shows a compaction marker, and the next turn succeeds within the context limit

### Requirement: Tool-output reduction

A turn SHALL bound the model-facing size of tool results. A tool output exceeding the configured length bound SHALL be stored inside the agent's workspace, and the result carried on the transcript SHALL become a short notice naming the stored file, so the model can read the full content back through its file tools. When a turn's accumulated working state crosses its share of the model's context limit, older tool arguments and results SHALL likewise be offloaded to the workspace while the most recent tool-call rounds stay intact. Reduction MUST NOT fail the turn: any failure while offloading leaves the original content in place.

#### Scenario: Oversized output becomes a readable file

- **WHEN** a tool returns output larger than the configured bound
- **THEN** the full content is stored in the agent's workspace, the transcript carries a notice with the stored path, and the model can read the content back through its file tools

#### Scenario: Old tool payloads clear under budget pressure

- **WHEN** a turn's working state crosses the context budget and older tool calls exist beyond the retained recent rounds
- **THEN** those arguments and results are offloaded to the workspace and the turn continues within the limit

#### Scenario: Offload failure keeps the original

- **WHEN** storing an oversized output fails
- **THEN** the original tool result stays on the transcript and the turn proceeds

### Requirement: In-run context safety net

Checkpoint compaction remains the persisted cross-turn mechanism. Additionally, when a single turn's working state crosses a safety threshold of the model's context limit mid-turn, the runtime SHALL summarize that state and continue the turn within the limit. An in-run summary is ephemeral: it MUST NOT be persisted as the session checkpoint, MUST NOT advance the checkpoint boundary, and the stored transcript MUST NOT be truncated.

#### Scenario: Long turn stays within the limit

- **WHEN** a single turn's accumulated state crosses the safety threshold before the model produces its answer
- **THEN** the state is summarized in-run, the turn continues, and it completes within the context limit

#### Scenario: Checkpoint compaction unchanged

- **WHEN** a session's context crosses the compaction threshold between turns
- **THEN** the runtime compacts into a persisted checkpoint exactly as before, regardless of any in-run summary

### Requirement: In-flight turn state query

The runtime SHALL expose, per session, whether a turn is in flight and the
in-flight turn's identifier, through a bound call the chat UI can consult. A
turn parked on a pending approval SHALL report as in flight. A session with
no turn in flight SHALL report not busy; reporting state for an unknown
session SHALL be safe and read as not busy.

#### Scenario: Query during a turn

- **WHEN** the turn state of a session with a running turn is queried
- **THEN** the answer reports the session busy and carries that turn's
  identifier

#### Scenario: Approval-parked turn reports in flight

- **WHEN** the turn state of a session parked on a pending approval is
  queried
- **THEN** the answer reports the session busy with the parked turn's
  identifier

#### Scenario: Idle session reports not busy

- **WHEN** the turn state of a session with no turn in flight is queried
- **THEN** the answer reports the session not busy

### Requirement: Subscribe-time live-state catch-up

When a session's live event stream is subscribed while one of its turns is
in flight, the runtime SHALL deliver the turn's in-flight state to the new
subscriber before any subsequent live events: the model call's partial
output streamed so far and the tool calls currently executing, announced
with their identifiers and arguments. These catch-up events are live-only —
MUST NOT be persisted to the transcript — and a reloaded transcript MUST
show only the durable events. Catch-up state MUST reconcile with the
turn's own records: a running tool's terminal record finalizes the
reconstructed row, and the turn's durable assistant message lands exactly
once. Subscribing to a session with no turn in flight SHALL deliver no
catch-up events.

#### Scenario: Late subscriber sees streamed-so-far text

- **WHEN** a session is subscribed while a model call is mid-stream
- **THEN** the new subscriber receives the partial output streamed so far
  before the next live delta, and the turn's terminal transcript is not
  duplicated by the reconstruction

#### Scenario: Late subscriber sees the running tool

- **WHEN** a session is subscribed while a tool call is executing
- **THEN** the new subscriber receives that call as a running announcement
  carrying its identifier, and the call's terminal record later finalizes it

#### Scenario: Catch-up is not persisted

- **WHEN** a session that received catch-up events is reloaded later
- **THEN** the transcript contains only the durable events — no
  reconstruction rows

#### Scenario: Subscribe to an idle session

- **WHEN** a session with no turn in flight is subscribed
- **THEN** no catch-up events are delivered and the stream behaves as before

### Requirement: Turn in-flight state observable app-wide

The runtime SHALL expose the set of sessions that currently have a turn in
flight — including turns parked on a pending approval — through a bound
call the UI can consult without subscribing to any session's event stream,
and SHALL announce every transition into and out of the in-flight state as
an application event naming the affected session. The query and the events
MUST cover turns the UI did not start or observe, including turns fired by
the scheduler or a gateway trigger into a session the UI has never opened.
The query alone MUST be sufficient to reconstruct the current busy set —
for example at startup or after a missed event — and reporting an empty set
when no turn is in flight is a normal state, not an error. The existing
per-session in-flight query keeps its meaning and is unaffected.

#### Scenario: Scheduler-fired turn lights the indicator

- **WHEN** a scheduled fire starts a turn in a session the UI has never
  opened
- **THEN** the transition event names that session, and a subsequent query
  reports the session as in flight

#### Scenario: Busy set reconstructs at startup

- **WHEN** the UI starts or reconnects while a turn is in flight anywhere
- **THEN** the bound query reports every in-flight session without any
  session subscription

#### Scenario: Terminal outcome clears the indicator

- **WHEN** an in-flight turn reaches a terminal outcome
- **THEN** the transition event names the session, and a subsequent query no
  longer reports it
