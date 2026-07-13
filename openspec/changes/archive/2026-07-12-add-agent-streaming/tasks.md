# Tasks: Per-Channel Token Streaming

> Backend is small; the work is the frontend delta merge reducer. CLI is intentionally left
> non-streaming (see proposal + the `agent-core` amendment). Implement via `/opsx:apply`.

## Backend — per-channel flag
- [x] Add `WithStreaming(ctx, bool)` / `StreamingFromContext(ctx) bool` to `internal/agent/middlewares` (new `streaming.go`), modeled on `WithPreviousResponseID`/`PreviousResponseIDFromContext` in `history_middleware.go`. Default false; exported; doc-commented; tabs.
- [x] In `internal/agent/agent.go` `Run()`, set `EnableStreaming: middlewares.StreamingFromContext(ctx)` on the constructed `adk.TypedAgentInput`.
- [x] In `internal/api/handler/chat.go` `Chat()`, set `ctx = middlewares.WithStreaming(ctx, true)` next to the existing `middlewares.WithPreviousResponseID` call.
- [x] Confirm `internal/cli/run.go` and `internal/cli/chat.go` need no change (leave streaming absent → off).

## Backend — testability prerequisite
- [x] Enhance `internal/llm/adapter/stub.go` `Stream()` to emit ≥2 same-`streaming_meta.index` text-delta chunks (it currently does not stamp `StreamingMeta`), so streaming + merge are unit-testable without a real provider.

## Frontend — delta merge-by-index
- [x] Confirm `web/src/types/chat.ts` exposes `streaming_meta?: { index: number }` on `ContentBlock` (add if missing).
- [x] Rewrite the `STREAM_MESSAGE` reducer in `web/src/components/ChatProvider.tsx` (currently appends whole blocks) to merge by `streaming_meta.index`: create on first sighting, append text/reasoning/argument fragments to the indexed block; route via a `Map<index, block>` for O(1) + out-of-order tolerance; fall back to append for blocks with no index.
- [x] Extract the per-block append into a pure `mergeBlockDelta(target, delta)` helper; cover all argument-bearing block types (function tool call, MCP tool call, server tool call) with the same append-arguments pattern.

## Frontend — harden tool-argument rendering
- [x] Audit renderers that `JSON.parse` tool `arguments` (`web/src/components/chat/Renderers.tsx`, `chat/ToolGroup.tsx`, `chat/ChainOfThought.tsx`): wrap parses in try/catch, fall back to raw string / tool name on incomplete JSON. (Already satisfied — all `JSON.parse` calls in `Renderers.tsx` are guarded; no code change required.)
- [x] Ensure a tool call's **name** renders as soon as its item-added block arrives, before arguments complete. (Already satisfied — `tc.name` renders ahead of the parsed `arguments`; no code change required.)

## Testing (black-box where possible; ≥70% per package)
- [x] Go: unit-test the streaming context helper round-trip (true/false/default) — `internal/agent/middlewares`.
- [x] Go: verify `Agent.Run` sets `EnableStreaming` on the input when the flag is present, using the enhanced stub adapter.
- [x] Web: unit-test `mergeBlockDelta` / the reducer (mirror `web/src/components/chat/groupBlocks.test.ts`): text-delta merge, multi-index blocks, tool-arg accumulation, out-of-order arrival, partial-JSON safety. Wired into `web/src/main.tsx` bootstrap.

## Verification
- [x] `make test` passes for touched packages (`internal/agent`, `internal/agent/middlewares`, `internal/api/handler`, `internal/llm/adapter`). NOTE: `internal/config` and `internal/workspace` have pre-existing environmental failures from `ONCLAW_*` env vars in the shell; they are unrelated to this change.
- [x] `npm run build` (tsc + vite) passes; `mergeBlockDelta.test.ts` is type-checked and exercised both at app bootstrap (`main.tsx`) and via `npm test` (headless `tsx src/runAllTests.ts`), mirroring `groupBlocks.test.ts`.
- [ ] Manual (configured provider): web shows **token-by-token** typing; tool calls show name first then args; final bubble matches persisted re-fetch.
- [ ] Manual: `onclaw run "<prompt>"` still prints whole-message output (no regression).
- [ ] Spot-check a second provider (e.g. Claude or Gemini) to confirm the universal merge contract.

## Spec
- [x] `specs/agent-core/spec.md` — MODIFIED "Assistant tokens stream to the user as they arrive" (granularity is per-channel) + ADDED "Per-channel streaming control".
- [x] `specs/chat-ui/spec.md` — ADDED "Token-Delta Streaming over the Chat Stream" + "Streaming Tool-Call Rendering".
