# Proposal: Per-Channel Token Streaming for the Agent Run

## Intent
Enable real, token-level streaming in the **web** channel so assistant output types
out live as the model generates it, while keeping the **CLI** at message-granular rendering.
Streaming is controlled by Eino's existing per-call `TypedAgentInput.EnableStreaming` flag,
gated explicitly per channel (web on, CLI off). This brings the implementation into a
deliberate, channel-aware stance and amends the `agent-core` streaming requirement to match.

## Problem
The agent iterator and the web SSE transport are already stream-shaped, but token streaming
is effectively off. `Agent.Run` constructs Eino's `TypedAgentInput` without setting
`EnableStreaming` (`internal/agent/agent.go`), so the model is always invoked via `Generate()`
and each turn-step arrives as one buffered message. Consequences:

- The web SSE feed carries **whole-message events per model call** — no live token typing. The
  assistant bubble only updates after each model call completes, not as tokens are produced.
- The archived `agent-core` requirement "Assistant tokens stream to the user as they arrive"
  states the CLI "SHALL render that stream to stdout as the tokens arrive" and "SHALL NOT buffer
  the full response." With `EnableStreaming` always false, no channel literally token-streams
  today; the spec's intent and the implementation are silently out of alignment.

The framework already has the exact knob and the exact delta contract for this — it is unused.

## Proposed Solution
**Backend (small, ~4 edits):**
- Add a per-call context helper `WithStreaming(ctx, bool)` / `StreamingFromContext(ctx) bool` to
  `internal/agent/middlewares` (new `streaming.go`), modeled on the existing
  `WithPreviousResponseID` / `PreviousResponseIDFromContext` in `history_middleware.go`.
- `Agent.Run` reads it and sets `EnableStreaming` on the `TypedAgentInput` it builds.
- The web handler (`internal/api/handler/chat.go`) sets `WithStreaming(ctx, true)` alongside its
  existing `WithPreviousResponseID` call. The CLI (`run.go`, `chat.go`) sets nothing → defaults
  off → current message-granular behavior.
- No changes to `event_iterator.go` (already drains `MessageStream`), the service layer,
  `ChatInput`, or any adapter.

**Frontend (the work — delta merge-by-index):**
- When streaming is on, each SSE `message` event carries one delta `ContentBlock` stamped with
  `streaming_meta.index` (a stable per-block id serialized by Eino). Rewrite the `STREAM_MESSAGE`
  reducer (`web/src/components/ChatProvider.tsx`) from "append whole blocks" to "merge delta
  blocks by `streaming_meta.index`": create a block on first sighting (carries tool name/call_id),
  then append text/reasoning/argument fragments to the indexed block. Cover every argument-bearing
  block type (function, MCP, server tool calls) with the same append pattern.
- Harden tool-argument rendering (`web/src/components/chat/Renderers.tsx`, `ToolGroup.tsx`,
  `ChainOfThought.tsx`): show a tool call's name immediately; never `JSON.parse` incomplete
  argument fragments (try/catch with raw-string fallback). The existing persisted-message re-sync
  ~200ms after stream completion (`ChatProvider.tsx`) replaces the streamed bubble with the
  authoritative merged message — a safety net that self-heals imperfect live rendering.

**Spec amendment:** the `agent-core` "Assistant tokens stream" requirement is MODIFIED to make
streaming granularity a per-channel property: the web token-streams; the CLI renders at message
granularity. A new `agent-core` requirement records the per-channel control. `chat-ui` gains
requirements for token-delta rendering and streaming tool-call rendering.

## Constraints & Dependencies
- **Eino-native, no fork:** uses `TypedAgentInput.EnableStreaming` and the canonical
  `StreamingMeta.Index` delta contract. No eino fork, no new message type.
- **Universal across providers:** all six registered adapters (`agenticopenai`, `agenticclaude`,
  `agenticgemini`, `agenticark`, `agenticdeepseek`, `agenticqwen`) emit the same
  `ContentBlock` + `streaming_meta.index` delta shape (verified). One merge reducer serves all;
  no per-provider branching. Uniformity holds by convention (the ADK runner's
  `schema.ConcatAgenticMessages` reassembly depends on it), not by interface enforcement.
- **Persistence unaffected:** the runner already materializes the final merged message via
  `schema.ConcatAgenticMessages`, so `ConversationStore` history is identical streaming or not.
- **Flag control — hardcoded per channel (confirmed decision):** the web handler enables
  streaming, the CLI leaves it disabled. No request-DTO field, no config plumbing, no UI toggle
  in this change.
- **Go conventions:** black-box tests, ≥70% statement coverage per `internal/...` package;
  context helper follows the `WithPreviousResponseID` precedent; contract/types/impl separation.

## Out of Scope (Deferred)
- **CLI token streaming.** Deliberately deferred; CLI stays message-granular (see spec amendment).
- **Config-driven / per-request streaming toggle.** The flag is hardcoded per channel; a config
  knob (`channels.*.streaming`) or `ChatInput.Stream` override is a follow-up if needed.
- **Token-level streaming for the CLI renderer** (`render.Text` delta handling) — not required
  while the CLI is non-streaming.
- **`item_status` explicit completion gating.** v1 infers block completion from the next index /
  stream end and leans on the persisted re-sync; surfacing `item_status` in `block.extra` for
  cleaner gating is a possible follow-up spike.