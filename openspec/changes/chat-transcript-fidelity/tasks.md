## 1. Storage: payload text

- [x] 1.1 Check no reachable database has applied migration 000016 (`select version from schema_migrations`); record the outcome — if one has, plan a conversion migration instead of the in-place edit
- [x] 1.2 Edit `migrations/000016_agent_runtime.up.sql`: `payload bytea` → `payload text` (and the matching down migration)
- [x] 1.3 `internal/store/postgres/session_events.go`: bind `string(e.Payload)` in `AppendEvents`; confirm `LoadEvents` scan into `[]byte` still works; update store integration tests if they assert column type

## 2. Runner: reasoning + latency (Go)

- [x] 2.1 `internal/agents/events.go`: add an agentic reasoning extractor (text from `ContentBlockTypeReasoning` blocks) beside `extractAgenticText`
- [x] 2.2 `internal/agents/runner.go` streaming drain: emit `TranscriptEventReasoningDelta` per reasoning chunk (streaming frames and non-streaming messages), keeping text and reasoning as distinct fields
- [x] 2.3 `internal/agents/runner.go`: stamp `ToolResultPayload.Latency` on `emitToolFinished` using a per-call started-at map (both the message-driven and span-driven emit paths)
- [x] 2.4 Go tests: reasoning deltas emitted before completed message; latency non-zero on finished events; existing runner tests stay green (`go test ./internal/agents/...`)

## 3. History: hydration joins (Go)

- [x] 3.1 `internal/agents/history.go`: pre-pass building assistant-message (event id → tool-call blocks) and tool-result-message (event id → result) maps from persisted events
- [x] 3.2 Project `tool_call_started` with arguments (join via `ToolSpanMeta.AssistantMessageEventID`, fallback call-id match) and `tool_call_finished` with result/is-error/latency (join via `ToolResultMessageEventID`, latency from the span pair's timestamps)
- [x] 3.3 Project assistant completed messages with `ReasoningContent` extracted from their persisted reasoning blocks
- [x] 3.4 Go tests: a persisted session with tool calls and reasoning hydrates with the same fields the live stream delivered

## 4. Wire: translator passthrough (Go)

- [x] 4.1 Verify `onclaw:reasoning_delta` end-to-end once the runner emits it (translator test with a reasoning event); confirm `latency_ms` now appears on trace items
- [x] 4.2 Update `internal/openresponses` tests pinning the tool trace shape (dot item inside `output_item.added`/`.done`, arguments at added, `is_error` on failure)

## 5. Web client: stream dispatch

- [x] 5.1 `web/src/lib/openresponses.ts`: add `onReasoningDelta` callback; extend `onToolCall` with `args` from `ev.item.arguments`; route tool outputs from `ev.item?.type === 'onclaw.function_call_output'` inside `output_item.added`/`.done`; delete the dead top-level case
- [x] 5.2 `web/src/chat/runtime.tsx`: accumulate reasoning onto the in-flight agent message (distinct from text) in both `respondFor` and `onReload`; attach args to pushed cards; keep output routing by call id
- [x] 5.3 `web/src/chat/runtime.tsx` `onError` paths: append an `author: 'error'` thread message after `retractIfEmpty()` (non-auth failures only); auth failures keep the connect-state path
- [x] 5.4 Web tests for the stream client and bridge callbacks (touched suites only)

## 6. Web UI: rendering

- [x] 6.1 Add the markdown renderer (react-markdown, memoized) to `AgentMessage` with a text-node override preserving `@mention` highlighting; code blocks styled JetBrains Mono, HTML escaped; build passes with the design tokens intact
- [x] 6.2 `AgentMessage`: collapsible reasoning bubbles — one per reasoning segment, ordered with the tool cards via the message's `parts` list (reasoning → tool → reasoning → text); a segment is expanded only while it is the streaming tail, collapsed after, hidden when empty; hydrated messages build the same ordered body (design D6 amendment, from the 7.1 pass)
- [x] 6.3 `ToolCall.tsx`: resolve display names via a per-workspace catalog cache over `api.tools.list`; show the raw id on the expanded card; delete the fabricated `'ok — rows'` fallback and render an explicit no-output state for empty results
- [x] 6.4 Add the `ErrorEntry` component for `author: 'error'` messages (danger styling, no avatar) and route them in `ChatView`
- [x] 6.5 `web/src/data/seed.ts`: give seeded tool cards result text so demo cards stay complete without the fabricated fallback

## 7. Verification

- [ ] 7.1 Live observation pass: real streaming turn against the dev backend — reasoning streams visible, card shows real args + result + measured latency, markdown renders, forced-error turn shows the in-chat entry; confirm the partial-args risk (design Risks) and apply the fallback (accumulate chunks or take args at `message_completed`) if it fires
- [ ] 7.2 Hydration pass: reload and second-browser convergence with fidelity fields intact (args, results, latency, reasoning)
- [x] 7.3 `go build ./... && go vet ./... && go test ./...` and targeted web suites (judge by touched suites — the repo has pre-existing jsdom localStorage failures); `openspec validate --strict` on this change
