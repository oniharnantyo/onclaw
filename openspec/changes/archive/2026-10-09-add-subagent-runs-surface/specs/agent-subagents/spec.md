## MODIFIED Requirements

### Requirement: Child transcript isolation
Sub-agent execution events SHALL NOT persist onto the parent session's transcript and SHALL NOT be delivered onto the parent's live stream as transcript events. To the parent transcript — live and hydrated — a delegation is exactly one tool call with started/finished events carrying the delegation arguments and the sub-agent's final message as the result. The one exception is the user-facing observability surface (agent-runs-surface): a task-keyed progress side-channel on the dedicated session tasks endpoints MAY carry sub-agent execution records to UI clients that explicitly subscribe. That side-channel SHALL NOT write to the parent session's persisted events, SHALL NOT enter the parent's context, and SHALL NOT appear on the turn stream or the session-events stream.

#### Scenario: Live stream shows one card
- **WHEN** a delegated sub-agent executes multiple model turns and tool calls
- **THEN** the parent's live stream carries one delegation tool-call started event and one finished event, and no sub-agent text, reasoning, or inner tool-call events

#### Scenario: Reload shows one card
- **WHEN** a transcript containing a completed delegation is reloaded from durable storage
- **THEN** the delegation renders as the same single tool call with its result, identical to what the live stream showed

#### Scenario: Subscribed trajectory stays off the transcript
- **WHEN** a client subscribed to the session tasks progress stream receives a delegation's child records
- **THEN** the parent session's persisted events and transcript remain exactly as they would have been without any subscriber
