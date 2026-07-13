# Design: Per-Channel Token Streaming

## The switch already exists — per-call, not per-agent
Eino controls streaming via `TypedAgentInput.EnableStreaming` (`adk/interface.go`), which is set
on the **input** to each `Run()`, not on the agent config. That means a single assembled agent
can be run streamed or unstreamed per call — streaming is a caller decision, not an agent flavor.
onclaw assembles the agent per request already (`service.Chat` → `resolve` → `AssembleAgent`, and
the CLI's `resolveAndAssemble`), so the flag threads through naturally with no rebuild.

Today `Agent.Run` (`internal/agent/agent.go`) builds the input without setting `EnableStreaming`,
so it is always `false` → the model is called via `Generate()` → each turn-step is one buffered
message. `EnableStreaming=true` routes the model through `Stream()` and the run emits token-level
delta chunks. `event_iterator.go` already handles both paths (it drains `MessageStream` when
`mv.IsStreaming`), so it needs no change.

## The delta contract: stable index, for free
When streaming, each `Stream()` chunk is one `*schema.AgenticMessage` carrying exactly **one**
`*schema.ContentBlock` built by `schema.NewContentBlockChunk(payload, &StreamingMeta{Index: N})`.
The adapter assigns a monotonically-increasing, stable `Index` per logical block, so every delta
fragment of the same block carries the same index. `StreamingMeta` serializes as
`streaming_meta:{index}` (omitempty), so the frontend receives the merge key on every event
without backend wrapping.

This is why merging is deterministic, not heuristic: the frontend routes each delta to
`content_blocks[index]` rather than guessing "append to the last text block." Text deltas append
to `assistant_gen_text.text`; reasoning deltas to `reasoning.text`; tool-call argument deltas
append to the matching call's `.arguments` (a raw JSON fragment string until complete).

## Universal across providers
All six registered adapters emit the same `ContentBlock` + `StreamingMeta.Index` delta contract:
- `agenticopenai`, `agenticclaude`, `agenticark` — `NewContentBlockChunk` with incremental deltas.
- `agenticdeepseek`, `agenticqwen` — delegate to the shared OpenAI ACL (identical shape).
- `agenticgemini` — post-stamps the index via `populateStreamingMeta`; different mechanism,
  **same output shape**.

So one merge reducer serves every provider. Uniformity holds by **convention + tooling**, not by
interface enforcement: the model interface (`components/model`) does not compel adapters to stamp
indices, but the ADK runner's reassembly (`schema.ConcatAgenticMessages`, invoked at
`adk/interface.go`) depends on it, so every conforming adapter does.

## Persistence is already correct
`schema.ConcatAgenticMessages` is the framework's canonical merge: it groups delta blocks by
`StreamingMeta.Index` and merges them. The runner calls it when materializing the turn for the
session/history. Therefore `ConversationStore` receives the fully-merged final message regardless
of streaming — history is byte-for-byte the same streaming or not. The only consumer that sees raw
deltas is the live SSE path, which is exactly what the web wants.

## Protocol choice: delta (A) — backend forwards, frontend merges
Three places fragments could be merged into blocks: backend-forward (A), backend-snapshot (B), or
backend-merge-to-block-boundary (C). This change uses **A** — the backend forwards each delta
chunk as-is to SSE, and the frontend merges by index. Rationale: A is the industry-standard
streaming shape (matches the OpenAI Responses delta protocol the adapters consume), gives true
token-by-token typing, and has the lowest wire cost (important for the Pi-served browser). The
frontend cost is the merge-by-index reducer, mirroring `ConcatAgenticMessages` in TypeScript. The
persisted re-sync after `STREAM_DONE` is the safety net for any imperfect live rendering.

## Why the CLI stays non-streaming (and why the spec is amended)
The CLI renders via `render.Text` per message and already consumes the iterator progressively at
message granularity. Keeping `EnableStreaming=false` on the CLI preserves that cheap, buffered
behavior appropriate to the low-resource target, and matches the requested channel split. Because
the archived `agent-core` requirement mandated that the CLI "render that stream to stdout as the
tokens arrive," this change MODIFIES that requirement to make granularity a per-channel property
(web token-streams; CLI message-level) rather than silently diverging from the spec.

## Injection mechanism
A context-value helper, not a `Run` signature change. `Agent.Run` already has a variadic
`contentBlocks` param, so adding a positional `bool` would churn every caller and order poorly.
The existing `middlewares.WithPreviousResponseID` / `PreviousResponseIDFromContext` precedent
(peer per-call option, set by the handler, read inside the run) is the idiomatic fit and keeps
`Run`'s signature stable.

## Risk: tool-argument completion
Tool-call argument deltas are partial JSON, unparseable until the block completes. v1 handles this
by (a) showing the tool **name** as soon as the item-added block arrives (before any arg fragment),
(b) accumulating argument fragments as a raw string, (c) never `JSON.parse`-ing arguments inside a
renderer without a try/catch fallback, and (d) relying on the persisted re-sync to correct the
final render. A possible follow-up surfaces `item_status` in `block.extra` for explicit per-block
completion gating.
