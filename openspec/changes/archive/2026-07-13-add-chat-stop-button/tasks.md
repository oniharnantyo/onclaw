# Tasks: Add Stop Control to Running Chat

## Frontend — cancellation wiring (`web/src/components/ChatProvider.tsx`, `web/src/components/chat/runChatStream.ts`)
- [x] Add `abortRef = useRef<AbortController | null>(null)` in `ChatProvider`.
- [x] Extract the SSE stream-reading loop into a pure, callback-driven module `web/src/components/chat/runChatStream.ts` (injectable `fetch` so it is unit-testable without a DOM). `runChat` creates a fresh `AbortController`, stores it in `abortRef`, builds the request body, and delegates to `runChatStream(body, controller.signal, callbacks)`; clears `abortRef` in a `finally`.
- [x] Expose `stopChat()` (stable via `useCallback`) that calls `abortRef.current?.abort()`; add it to the context value and to the `useComposer` selector.
- [x] In `runChatStream`'s `catch`, branch on `err?.name === 'AbortError'`: abort → `onStopped` (→ `dispatch STREAM_STOPPED`, no toast, no re-fetch); other → `onConnectionError` (→ existing toast + `STREAM_ERROR`).
- [x] Gate the post-stream `setTimeout(fetchMessages)` so it runs only in the `onDone` (success) callback, never on the abort path.

## Frontend — stopped-partial marker (`web/src/types/chat.ts`, reducer)
- [x] Add optional `stopped?: boolean` to `ChatMessage`.
- [x] Add a `STREAM_STOPPED` reducer action that clears `isStreaming` and marks the trailing **assistant** message `stopped: true` (gated to the last assistant message; mirrors `STREAM_DONE` but sets the marker).
- [x] Dispatch `STREAM_STOPPED` on the abort branch (instead of plain `STREAM_DONE`).

## Frontend — composer UI (`web/src/components/Chat.tsx`)
- [x] Extract a `ComposerActions` toggle (driven by `isStreaming`); render `Composer.Cancel` while `isStreaming` and `Composer.Send` while idle, and wire `Composer.Cancel.onClick` to `stopChat`.
- [x] Render a "stopped" affordance (icon + muted label) on assistant messages where `stopped` is true.
- [x] Add CSS for the stop control and the stopped affordance, following `web/design-system/onclaw/MASTER.md`.

## Backend
- [x] None. Cancellation already propagates via `r.Context()`; no endpoint, handler, store, or schema change.

## Tests
- [x] `runChatStream` test: abort dispatches `onStopped`, does NOT call `onDone` (the re-fetch trigger), and does NOT call `onConnectionError`; a non-2xx response dispatches `onConnectionError`; a network throw dispatches `onConnectionError` (not `onStopped`); explicit re-fetch-guard assertion that abort never calls `onDone`.
- [x] `chatReducer` test: `STREAM_STOPPED` retains the partial, sets `stopped`, clears `isStreaming`; targets the last assistant message (not a trailing user turn); `STREAM_DONE` does not set `stopped`.
- [x] `ComposerActions` (Chat.tsx) test via `renderToStaticMarkup`: the stop control renders only while streaming and the send control renders only while idle.
- [x] Regression: `TestCancellationNonPersistence` still passes unchanged — this change touches no Go files, so the invariant (stopped turns write 0 rows) is preserved by construction; verified green.

## Verification
- [x] `tsc --noEmit` and `tsc -b` in `web/` are clean.
- [x] `make test` is green for the affected packages (the unrelated pre-existing `internal/config` / `internal/workspace` failures are environment issues — `ONCLAW_CONCURRENCY` override and a missing `/my/test/workspace` path on macOS — not touched by this change).
- [x] `openspec validate add-chat-stop-button --strict` passes.
- [x] Manual: start a long stream, click stop — confirm the text freezes and the "(stopped)" marker shows; then send a new message and confirm the next turn runs cleanly (no provider 400 from a dangling context).
