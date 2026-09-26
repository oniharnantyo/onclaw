# Tasks

## 1. Runner — span lane emits guarded, args-complete started events

- [x] 1.1 Add a per-execution `callID → arguments` stash to the runner's live loop, filled wherever the frames/non-streaming lanes extract tool calls (`agenticToolCalls` sites), and route the `SessionEventSpanToolCallStart` emission through `emitToolStarted` with the joined arguments (empty fallback preserves today's shape). Verify: `go build ./...` and `go vet ./...` pass.
- [x] 1.2 Runner regression test: fake agentic model through the real ADK agent issuing two parallel id-bearing tool calls in one assistant message; assert the emitted transcript events contain exactly two `tool_call_started` events (one per call id, each carrying that call's arguments) and two matching `tool_call_finished` events. Verify: `go test ./internal/agents/ -run ToolCall -count=1` passes.
- [x] 1.3 Guard test: a scripted duplicate start for an already-started call id produces exactly one `tool_call_started`. Verify: `go test ./internal/agents/ -run ToolCall -count=1` passes.

## 2. /v1 translator — per-call-id item pairing

- [x] 2.1 Replace the scalar open-item state in `translate.go` with a map keyed by call id (insertion-ordered for output indices): `started` opens an item only when absent; `finished` resolves its own call's item and emits that call's `output_item.done` with its own id and arguments. Verify: `go build ./...` passes.
- [x] 2.2 Translate tests: parallel calls produce one `function_call` added + one done per call id with correct arguments; a re-announced call id yields a single added; no done is dropped. Verify: `go test ./internal/openresponses/ -count=1` passes.
- [x] 2.3 Incident replay test: feed the failing turn's event sequence (started ex, started ws, finished ex, finished ws) and assert the wire contains `added(execute, args)`, `added(web.search, args)`, `done(execute, args)`, `done(web.search, args)` — no mislabelled or swallowed done. Verify: `go test ./internal/openresponses/ -count=1` passes.

## 3. Web — card identity by call id

- [x] 3.1 In both `onToolCall` handlers (`runtime.tsx` respondFor and catch-up paths), replace the `args ?` truthy gate with an unconditional call-id lookup: update the existing card when found (set `args` only when the event carries one), push only for an unseen call id. Verify: `cd web && pnpm test -- src/chat/runtime.test.ts` passes.
- [x] 3.2 Add `convertMessage` dedupe: skip a tool entry whose prefixed `toolCallId` was already emitted for the message. Verify: unit case with two same-callId entries yields one tool-call part; `pnpm test -- src/chat/runtime.test.ts` passes.
- [x] 3.3 Incident replay test in `runtime.test.ts`: drive the malformed wire sequence from the incident (argless added ×2, empty-args done for the second call id, outputs) and assert one card per call id, the second card carrying no phantom arguments, and no duplicate `toolCallId` in conversion output. Verify: `pnpm test -- src/chat/runtime.test.ts` passes.
- [x] 3.4 Correct the stale "args can be partial at `.added`" contract comment in `openresponses.ts` to describe the span-sourced + joined-args behavior. Verify: comment review; no behavior change (`pnpm test -- src/lib/openresponses.test.ts` still green).

## 4. Verification

- [x] 4.1 Full backend suite: `go build ./... && go vet ./... && go test ./...` green.
- [x] 4.2 Full web suite: `cd web && pnpm test` green.
- [ ] 4.3 Live pass on the dev instance: run a tool-using turn on a GLM agent, watch the turn complete without the ErrorBoundary crash, cards show arguments live (no reload needed), and a reload shows identical card fields. Verify: manual browser pass on a fresh direct chat.
