# Proposal

## Why

A live turn whose model issues tool calls crashes the web chat the moment the run completes: `Duplicate key toolCallId-call_chatcmpl-tool-… in useResources` tears the whole tree into the ErrorBoundary (confirmed 2026-09-26 on a real GLM turn, dev DB session `sess_f572c5ac…`). The same defect chain also leaves live tool cards permanently without their arguments until a reload. Root cause, fully traced: the runner's live span lane emits `tool_call_started` without arguments and without a dedupe guard, the `/v1` translator tracks one open tool item instead of one per call id (mislabelling the first `done` and swallowing the second on parallel calls), and the client's card dedupe only engages when arguments are non-empty — so malformed empty-args events mint duplicate cards sharing one call id.

## What Changes

- **Backend — runner span lane** (`internal/agents/runner.go`): route the ADK span lane's `tool_call_started` emission through the guarded `emitToolStarted` helper instead of emitting directly, and join the call's arguments from the assistant message that requested the call (the span payload carries `assistant_message_event_id`; the join mirrors what hydration already does in `history.go` `toolArguments`). The message-frames lane never fires for executed tool calls on the agentic react path (the ADK react graph reroutes tool-call rounds away from the live message stream — confirmed against eino source and the crash's own error-id math), so the span lane is the only live source and the args-join is required for live cards to ever show arguments.
- **Backend — /v1 translator** (`internal/openresponses/translate.go`): track open tool items per call id instead of a single scalar (`t.fc`/`t.fcOpen`). Each `tool_call_started` mints exactly one `output_item.added` (dropping a re-start for an already-open call id), and each `tool_call_finished` emits its `output_item.done` carrying that call's own id and complete arguments — no more wrong-call `done` and no more swallowed `done` on parallel calls.
- **Web — card identity** (`web/src/chat/runtime.tsx`, both stream handlers): a card is identified by call id. An `onToolCall` whose call id already has a card updates it (filling arguments when present) instead of pushing a second card, regardless of whether arguments are empty on that event. Also dedupe by `toolCallId` in `convertMessage` as a last line of defense: any future duplicate source degrades to one card instead of a tree crash.
- **Web — stale comment** (`web/src/lib/openresponses.ts`): the "args can be partial at `.added`" contract comment describes the frames lane, which never fires for executed tool calls; reword to describe the span lane + joined args reality.
- **Regression test**: a fake agentic model driven through the real ADK agent + runner with a scripted stream of id-bearing parallel tool calls asserts exactly one `tool_call_started` per call id, arguments present, and the `/v1` wire pairing (one `added` + one `done` per call id, each `done` carrying its own call's arguments).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-runtime`: the live tool-call emission contract — exactly one `tool_call_started` per executed call, carrying that call's arguments, so the live stream and the hydrated transcript agree field-for-field (the spec already requires live/history parity; the delta pins it for tool calls).
- `openresponses`: per-call-id pairing on the wire — for each tool call exactly one `function_call` `output_item.added` (with arguments) and exactly one `output_item.done` carrying that same call's id and complete arguments, holding under parallel calls.
- `web-app/chat-runtime`: card identity is the call id — one card per call id for the life of a turn; an update event for an existing call id mutates that card (never mints a second), and message conversion cannot emit two tool-call parts sharing one `toolCallId`.

## Impact

- `internal/agents/runner.go` — span lane emission + assistant-message args stash/join; new/updated runner tests.
- `internal/openresponses/translate.go` — open-item map keyed by call id; `translate_test.go` cases for parallel calls, re-start, and swallowed-done regression.
- `web/src/chat/runtime.tsx` — two `onToolCall` handlers + `convertMessage` dedupe; `runtime.test.ts` cases for empty-args done and duplicate call ids.
- `web/src/lib/openresponses.ts` — comment correction only (no behavior).
- No database, migration, or wire-format changes: the `/v1` stream and session events become *compliant* with their existing specs; no client depends on the buggy behavior. Hydration (`history.go`, `livechat.ts`) is already correct and untouched.
