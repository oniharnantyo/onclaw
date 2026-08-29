# Proposal: adopt-assistant-ui-chat

## Why

The chat scaffold hand-rolls streaming, branching, message editing, and cancelation as mock-timer behavior that will be discarded when a real backend exists. `web/src/store/index.ts` implements `respondFor`/`refreshMessage`/`branchNav`/`editSubmit` with `setTimeout` and text-template replies; every polish pass on `BranchPicker`, edit modals, and fake streaming carets is spend on throwaway infrastructure. Adopting assistant-ui now replaces that infrastructure with library-provided interaction machinery while the mock transport is still small enough to swap — its handlers isolate transport, so the later real-Go-backend change swaps handlers only, not components.

## What Changes

- Add `@assistant-ui/react` (0.15.x line) and its required dependencies to `web/package.json`.
- Add a runtime bridge module (`web/src/chat/runtime.tsx`) that wraps the existing zustand store with `useExternalStoreRuntime`: messages read from the active session, `convertMessage` maps OnClaw messages to `ThreadMessageLike` (agent identity, cron-origin, tool cards as parts), and `onNew`/`onEdit`/`onReload`/`onCancel`/`onAddToolResult` handlers drive the existing mock response engine through the runtime's lifecycle.
- Migrate the chat transcript and composer to assistant-ui **headless primitives** (no styled components — the `web/Web-Prototype/` design contract stays the pixel source of truth).
- Replace hand-rolled behavior state with runtime capabilities: streaming text lands incrementally through the runtime, running state drives ThinkingRow, cancel/edit/reload flow through runtime handlers, branch picking flows through the runtime's branch state.
- Restrict edit/reload/branch affordances to agent direct chats; channel messages lose the regenerate control (multi-author turn semantics intentionally deferred).
- **BREAKING (internal)**: store actions `respondFor`, `refreshMessage`, `branchNav`, `editSubmit`, and `ui.typing`/`ui.streamId` are superseded by the runtime bridge; their call sites move into bridge handlers.
BREAKING is internal-only: no API, no persisted data, no visual parity change except where a spec delta explicitly changes behavior.
- Out of scope: Go backend, `/v1/responses` wire contract, real SSE transport, MCP — a later change swaps the bridge handlers for real transport.

## Capabilities

### New Capabilities
- `web-app/chat-runtime`: the assistant-ui runtime bridge over the mock store — message conversion, handler-driven response lifecycle, running-state streaming, and behavior scoping to agent DMs.

### Modified Capabilities
- `web-app/chat`:
  - *Message send and simulated response*: reply arrives as incremental streamed text through the runtime (not a whole message + caret); running state and cancel flow through the runtime.
  - *Branching and regeneration*: variant navigation moves to runtime branch state; edit/reload/branch affordances apply to agent DMs only — channel agent messages no longer offer regenerate.
  - *Transcript rendering*: tool-call cards gain a running → completed lifecycle (latency shown on completion).

## Impact

- **Code**: `web/src/components/chat/**` (transcript, composer, message components), `web/src/store/index.ts` (actions superseded by bridge handlers), new `web/src/chat/runtime.tsx` bridge module, `web/src/App.tsx` mounts the provider around the chat view.
- **Dependencies**: `@assistant-ui/react` 0.15.x + peer deps (radix primitives, zod, `@assistant-ui/core`, `assistant-stream`, …). Bundle estimate ~60–100KB gzip on the chat route (code-split).
- **Verification**: Playwright visual-parity suite stays green; new bridge-level tests cover conversion, handler fan-out, running/cancel state, DM-only affordances.
- **Systems**: none beyond the web app — no backend, no API, no data migration.
