## ADDED Requirements

### Requirement: Token-Delta Streaming over the Chat Stream

When streaming is enabled, the chat streaming protocol SHALL deliver token-level assistant content
as it is generated. Each SSE `message` event SHALL carry one delta content block stamped with
`streaming_meta.index`, and the client SHALL merge delta blocks into coherent content blocks by
that index rather than treating each event as a whole message. The client SHALL accumulate
text deltas into the indexed block's text, reasoning deltas into the indexed block's reasoning,
and tool-call argument deltas into the indexed call's raw argument string. This merge SHALL work
uniformly across all provider kinds (OpenAI, Anthropic, Gemini, Ark, DeepSeek, Qwen) without
per-provider branching.

#### Scenario: Tokens appear as they are generated

- **WHEN** the web client receives streaming `message` events during a turn
- **THEN** assistant text appears progressively as tokens arrive, not only after the full message completes

#### Scenario: Delta fragments merge by index

- **WHEN** multiple `message` events carry delta blocks with the same `streaming_meta.index`
- **THEN** their text/argument fragments accumulate into a single content block rather than producing duplicate or fragmented blocks

#### Scenario: A new block is created on first sighting

- **WHEN** a delta block arrives with an index not yet present in the streaming assistant message
- **THEN** the client creates a new content block at that index, carrying the block's identity (e.g. a tool call's name and call id) for subsequent fragments to merge into

#### Scenario: Out-of-order block arrival is handled

- **WHEN** delta blocks arrive in an order other than strictly increasing index
- **THEN** each block is still routed to its correct index and merged correctly

### Requirement: Streaming Tool-Call Rendering

During streaming, a tool call's name SHALL render as soon as its item-added block arrives, before
its arguments complete. Tool-call arguments arrive as JSON fragments and SHALL be accumulated as a
raw string; renderers SHALL NOT attempt to parse incomplete argument JSON. Any renderer that parses
tool arguments SHALL degrade gracefully to the raw string or the tool name when the JSON is
incomplete or invalid. The persisted-message re-fetch after stream completion SHALL replace the
streamed assistant bubble with the authoritative merged message.

#### Scenario: A tool call's name renders before its arguments complete

- **WHEN** a tool-call block arrives during streaming
- **THEN** the tool name renders immediately, ahead of the (still-accumulating) arguments

#### Scenario: Incomplete argument JSON does not break rendering

- **WHEN** a renderer encounters tool-call argument JSON that is partial or invalid mid-stream
- **THEN** it falls back to the raw argument string or the tool name rather than throwing

#### Scenario: The streamed bubble re-syncs to persisted truth

- **WHEN** the stream completes
- **THEN** the streamed assistant message is replaced by the fetched persisted message so the final render is authoritative
