# Implementation Tasks

## 1. Blocking spike — tool-result messages in the event stream

- [x] 1.1 Write a test that runs a tool-calling turn through the assembled agent and asserts the drained event stream (the collected turn buffer) contains a `FunctionToolResult` content block.
- [x] 1.2 If the assertion fails, STOP and surface the design decision (accept tool-result loss + spec update, vs. retain a minimal state-reader for tool results) before proceeding. Do not continue to §2 until resolved.

## 2. Delete the collector

- [x] 2.1 Delete `internal/agent/middlewares/turn_collector.go` and `turn_collector_test.go`.
- [x] 2.2 `internal/agent/agent.go`: remove `type TurnCommitter = middlewares.Committer` (`:64`), `AssembleAgentOpts.Committer` (`:79`), `NewTurnCollector(opts.Committer)` (`:391`), the chain entry (`:414`), the `turnCollector` fields (`:45/:116/:467`), and `Agent.SetEventSink` delegation (`:545`).
- [x] 2.3 Remove the `Committer` interface from `internal/agent/middlewares`.

## 3. Event iterator — reconstruct the turn from the stream

- [x] 3.1 `event_iterator.go`: capture **all** `MessageOutput.Message` events into the collected buffer (not only non-streaming ones).
- [x] 3.2 Reassemble streamed assistant messages from the `MessageStream` chunks into the complete message (with tool-call blocks) and add to the collected buffer.
- [x] 3.3 Expose the collected turn to the entrypoint (add `CollectedTurn() []*schema.AgenticMessage` to the `EventIterator` interface, `iterator.go:37`, or have the entrypoint accumulate during drain).
- [x] 3.4 Tests: a streamed assistant turn reassembles correctly; a tool-calling turn's buffer contains the assistant tool-call message, the tool-result message (per §1), and the final assistant message.

## 4. Business-layer token estimation + response-ID

- [x] 4.1 Move/copy `estimateTokenCount` (chars/4) into `internal/conversation` (or a shared tokens util) so the business layer owns it.
- [x] 4.2 `SessionManager.CommitTurn`: replace the `ResponseMeta.TokenUsage` read (`:127-132`) with a char-based estimate over the turn's messages → prompt/completion/total.
- [x] 4.3 `SessionManager.CommitTurn`: response-ID fallback reads `_eino_msg_id` from the final assistant's `Extra`; drop the OpenAI/Gemini extension reads; empty + warn otherwise.
- [x] 4.4 Tests: char-based estimate matches expected; `_eino_msg_id` fallback; empty when absent.

## 5. Entrypoints drive CommitTurn

- [x] 5.1 `internal/cli/chat.go`, `internal/cli/run.go`: after the drain loop, `sessionMgr.CommitTurn(ctx, collectedTurn)`; drop `Committer` from assembly opts.
- [x] 5.2 `internal/cli/agent_session.go`: drop `Committer` plumbing to `AssembleAgent` (`convStore` is retained — read by tests).
- [x] 5.3 `internal/api/handler/chat.go` + `internal/api/service`: post-drain `CommitTurn(collectedTurn)`; drop `Committer` plumbing.
- [x] 5.4 Usage meter (SSE context meter): emit from the business-layer estimate (per-turn or per-message as the entrypoint drains), replacing the collector's `ResponseMeta` emission.

## 6. Spec + verification

- [x] 6.1 Update `openspec/specs/conversation-history/spec.md` per this change's delta (entrypoint-driven commit; char-based token usage; relaxed response-ID fallback).
- [x] 6.2 `make fmt && make vet && make lint`.
- [x] 6.3 `make test` green; `internal/agent`, `internal/agent/middlewares`, `internal/conversation`, `internal/api/handler`, `internal/cli` each ≥70%.
- [x] 6.4 Integration: one completed turn → one `conversation_messages` row; char-based token columns populated; redaction applied; the persisted turn includes the tool result (per the §1 spike).
- [x] 6.5 Manual: `onclaw chat` (with a tool call), `onclaw run`, web UI chat — turn persists, meter moves.
- [x] 6.6 `openspec validate remove-turn-collector`.