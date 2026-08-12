## ADDED Requirements

### Requirement: An in-process message bus dispatches memory domain events to registered workers

The system SHALL provide an in-process message bus (`internal/membus`) that accepts typed events via a `Publish` method and dispatches them asynchronously to registered workers. The bus SHALL use a buffered Go channel with a configurable buffer size (default 64). Publishing SHALL be non-blocking: if the buffer is full, the event SHALL be dropped and a warning logged.

#### Scenario: An event is delivered to all matching workers

- **WHEN** a producer publishes an `EpisodeCreated` event to the bus
- **THEN** every registered worker whose `Subscribes()` includes `"episode_created"` receives the event

#### Scenario: Publishing does not block the caller

- **WHEN** a producer publishes an event and the bus buffer is not full
- **THEN** the `Publish` call returns immediately without waiting for worker processing

#### Scenario: A full buffer drops the event gracefully

- **WHEN** a producer publishes an event and the bus buffer is full
- **THEN** the event is discarded, a warning is logged, and the caller is not blocked

### Requirement: Workers process events sequentially within their own goroutine

Each registered worker SHALL receive events in its own goroutine via a per-worker channel. Events SHALL be processed sequentially within a single worker (no concurrent handling for the same worker). Multiple workers MAY process the same event concurrently with each other.

#### Scenario: A slow worker does not block other workers

- **WHEN** the KG extraction worker takes 5 seconds to process an event
- **THEN** the Dreamer worker still receives and processes the same event without waiting

#### Scenario: A single worker processes events in order

- **WHEN** two `EpisodeCreated` events are published in sequence
- **THEN** a subscribing worker processes the first event before starting the second

### Requirement: Worker errors are non-fatal and logged

A worker that returns an error SHALL have the error logged via `slog.Warn`. The error SHALL NOT propagate to the event publisher, block subsequent events, or terminate the bus.

#### Scenario: A failing worker does not affect other workers

- **WHEN** the KG extraction worker returns an error for an event
- **THEN** the Dreamer worker still processes the same event normally and the bus continues operating

#### Scenario: A worker error is observable in logs

- **WHEN** a worker returns an error
- **THEN** a structured log entry is emitted containing the worker name, event name, and error message

### Requirement: The bus supports graceful shutdown with drain

The bus SHALL provide a `Stop` method that signals all workers to finish their current event, drains remaining buffered events (with a timeout), and waits for all worker goroutines to exit. After `Stop` returns, no further events SHALL be processed.

#### Scenario: Graceful shutdown processes remaining buffered events

- **WHEN** `Stop` is called with events still in the buffer
- **THEN** buffered events are delivered to workers before shutdown completes (subject to a drain timeout)

#### Scenario: Stop blocks until all workers exit

- **WHEN** `Stop` is called while a worker is processing an event
- **THEN** `Stop` waits for the worker to finish its current event before returning

### Requirement: Workers declare their event subscriptions at registration time

Each worker SHALL implement a `Subscribes() []string` method returning the event names it handles. The bus SHALL only dispatch events to workers whose subscription list includes the event's name.

#### Scenario: A worker only receives events it subscribes to

- **WHEN** a `DreamCompleted` event is published
- **THEN** the KG extraction worker (subscribed to `episode_created` only) does NOT receive it

### Requirement: Timer-based events are supported for periodic workers

The bus SHALL support registering timer-based workers that fire at a configured interval. The timer SHALL publish an internal event on each tick, routed to the subscribing worker. Timer workers SHALL respect the same shutdown semantics as event-driven workers.

#### Scenario: The pruner fires periodically

- **WHEN** the pruner worker is registered with a 1-hour interval
- **THEN** it receives a tick event approximately every hour and executes its pruning logic
