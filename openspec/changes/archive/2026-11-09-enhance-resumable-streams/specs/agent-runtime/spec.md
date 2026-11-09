# Spec Delta: agent-runtime

## MODIFIED Requirements

### Requirement: Subscribe-time live-state catch-up

When a session's live event stream is subscribed while one of its turns is
in flight, the runtime SHALL deliver the turn's in-flight state to the new
subscriber before any subsequent live events: the model call's partial
output streamed so far, the in-flight call's reasoning partial, the tool
calls currently executing (announced with their identifiers and arguments),
and the delegated tasks currently running (announced with their
identifiers and lifecycle state). These catch-up events are live-only —
MUST NOT be persisted to the transcript — and a reloaded transcript MUST
show only the durable events. Catch-up state MUST reconcile with the
turn's own records: a running tool's terminal record finalizes the
reconstructed row, and the turn's durable assistant message lands exactly
once. Subscribing to a session with no turn in flight SHALL deliver no
catch-up events.

The runtime SHALL also expose a resync: a bound call that re-delivers the
same in-flight state to a busy session's subscribers, so a consumer that
lost live events without re-subscribing — a transport blip on a
subscription that stays registered — can recover exactly. Each resync
delivery SHALL carry a synthetic identifier unique to that delivery, so
repeated resyncs are distinguishable and a consumer's deduplication never
swallows a later delivery. Resync state is live-only like catch-up state
and MUST NOT be persisted. Resyncing an idle or unknown session SHALL be a
safe no-op.

#### Scenario: Late subscriber sees streamed-so-far text

- **WHEN** a session is subscribed while a model call is mid-stream
- **THEN** the new subscriber receives the partial output streamed so far
  before the next live delta, and the turn's terminal transcript is not
  duplicated by the reconstruction

#### Scenario: Late subscriber sees the in-flight reasoning

- **WHEN** a session is subscribed while the model call is mid-reasoning
- **THEN** the new subscriber receives the reasoning streamed so far before
  the next live reasoning delta, and the turn's durable reasoning record
  lands exactly once

#### Scenario: Late subscriber sees the running tool

- **WHEN** a session is subscribed while a tool call is executing
- **THEN** the new subscriber receives that call as a running announcement
  carrying its identifier, and the call's terminal record later finalizes it

#### Scenario: Late subscriber sees the running task

- **WHEN** a session is subscribed while a delegated task is running
- **THEN** the new subscriber receives that task as a running row, and the
  task's terminal record later finalizes it

#### Scenario: Catch-up is not persisted

- **WHEN** a session that received catch-up events is reloaded later
- **THEN** the transcript contains only the durable events — no
  reconstruction rows

#### Scenario: Subscribe to an idle session

- **WHEN** a session with no turn in flight is subscribed
- **THEN** no catch-up events are delivered and the stream behaves as before

#### Scenario: Resync recovers a lost stretch without re-subscribing

- **WHEN** a subscriber misses deltas emitted while its transport was
  interrupted and then requests the resync for the still-busy session
- **THEN** it receives the full in-flight state including the text streamed
  so far, and subsequent live deltas continue from the producer's current
  position without duplication

#### Scenario: Repeated resyncs stay distinguishable

- **WHEN** a consumer requests the resync for a busy session twice
- **THEN** each delivery carries a distinct identifier and both are
  observable by the consumer

#### Scenario: Resync on an idle session

- **WHEN** the resync is requested for a session with no turn in flight
- **THEN** the call completes safely and delivers no in-flight state

## ADDED Requirements

### Requirement: Stream liveness beacon

While a turn streams on a session's live event stream, the runtime SHALL
emit a periodic liveness beacon on that stream carrying the in-flight
turn's identifier and a count of the deltas emitted for that turn so far.
The count SHALL be monotonic per turn. The beacon SHALL flow even when the
turn emits no deltas — a tool call executing or a silent model — so a
consumer can distinguish a healthy quiet turn from an interrupted one. The
beacon is live-only — MUST NOT be persisted to the transcript — and SHALL
stop when the turn reaches a terminal outcome. A consumer comparing the
beacon's count against the number of deltas it has received SHALL be able
to detect missed deltas exactly rather than inferring loss from silence.

#### Scenario: Beacon flows through tool silence

- **WHEN** a turn is executing a long tool call and emits no deltas
- **THEN** the session's live stream still carries periodic beacons for the
  in-flight turn

#### Scenario: Counter detects a missed delta

- **WHEN** a subscriber misses a delta and the next beacon arrives
- **THEN** the beacon's count exceeds the number of deltas the subscriber
  has received, exposing the gap exactly

#### Scenario: Beacon stops at the terminal

- **WHEN** the in-flight turn reaches a terminal outcome
- **THEN** no further beacons are emitted for that turn

#### Scenario: Beacon is not persisted

- **WHEN** a session whose turn streamed with beacons is reloaded later
- **THEN** the transcript contains no beacon rows
