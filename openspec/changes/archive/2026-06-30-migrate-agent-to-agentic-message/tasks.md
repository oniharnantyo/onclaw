# Implementation Tasks

## 0. Dependencies

- [x] 0.1 `go get` the six eino-ext agentic model packages (`agenticopenai`, `agenticclaude`, `agenticgemini`, `agenticdeepseek`, `agenticqwen`, `agenticark`); `go mod tidy`
- [x] 0.2 Verify compatibility with pinned `eino v0.10.0-alpha.9`; if a coordinated `eino` bump is forced, bump deliberately and re-confirm the ADK APIs used in section 3

## 1. Per-provider agentic adapters

- [x] 1.1 Add `internal/llm/adapter/agentic_openai.go`: build `agenticopenai.ChatConfig` from `store.Profile` + modelName + apiKey; `agenticopenai.NewChatModel` -> `model.AgenticModel`; serve `openai`, `openai-compatible`, `ollama` (via `BaseURL`)
- [x] 1.2 Add `agentic_claude.go` (`agenticclaude`), `agentic_gemini.go` (`agenticgemini`), `agentic_deepseek.go` (`agenticdeepseek`), `agentic_qwen.go` (`agenticqwen`), `agentic_ark.go` (`agenticark`); confirm each package's exact constructor via `go doc`
- [x] 1.3 Move per-provider reasoning config mapping into each adapter (Claude thinking, Gemini safety, OpenAI reasoning_effort/ExtraFields, etc.)
- [x] 1.4 Update `defaults.go` registry: register each provider type to its adapter; retire `openai_compat.go`
- [x] 1.5 Construct-smoke test per adapter (build config, assert `model.AgenticModel` returned, no live API call)

## 2. Provider surface

- [x] 2.1 `Service.BuildWithProfile` returns `model.AgenticModel` (`internal/llm/service.go`)
- [x] 2.2 Stub adapter (`stub.go`): `StubChatModel` satisfies `model.AgenticModel` (Generate/Stream over `[]*schema.AgenticMessage`)

## 3. Finish `agent.go`

- [x] 3.1 `Agent.EinoAgent` -> `*adk.TypedChatModelAgent[*schema.AgenticMessage]`
- [x] 3.2 Config -> `adk.TypedChatModelAgentConfig[*schema.AgenticMessage]`; ctor -> `adk.NewTypedChatModelAgent[*schema.AgenticMessage]`; Handlers -> `[]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]`
- [x] 3.3 Rewrite the summarization callback body over `*schema.AgenticMessage` (pointer-identity diff, `_onclaw_seq` in message-level `Extra`, redact via `tools.RedactAgenticMessage`, marshal, `SaveSummary`, mark persisted)

## 4. History middleware -> agentic

- [x] 4.1 Retype `internal/agent/middlewares/history_middleware.go` to `*schema.AgenticMessage` (embed, all handler methods, `unmarshalMsg`, `saveMessage`, `IsPersisted`)
- [x] 4.2 Redact via `tools.RedactAgenticMessage`; marshal `*schema.AgenticMessage`

## 5. Headless runner

- [x] 5.1 Replace `RunAgent(ctx, a, input, stdout)` with `(*Agent).Run(ctx, input) EventIterator` yielding `*schema.AgenticMessage`, no I/O
- [x] 5.2 Translate the agentic `event.Output.MessageOutput` (`TypedMessageVariant[*schema.AgenticMessage]`); `Interrupted` -> terminal; `event.Err`/`ctx.Err()` -> `Err()` (preserve `context.Canceled`); drop the dead `fullContent` accumulation

## 6. Renderer + redaction helper

- [x] 6.1 Add `tools.RedactAgenticMessage(*schema.AgenticMessage) *schema.AgenticMessage` (walk `ContentBlocks`, redact text via `tools.Redact`)
- [x] 6.2 Add `internal/agent/render/renderer.go` (`Renderer` interface: `Render(*schema.AgenticMessage) error`) and `text.go` (`Text(io.Writer) Renderer`) reproducing today's CLI output from ContentBlocks

## 7. CLI wiring + dedup

- [x] 7.1 Extract `internal/cli/agent_session.go` `resolveAndAssemble(ctx, st, db, mgr, req, convStore, convID) (*agent.Agent, workspace, error)`; call from `run.go` and `chat.go`'s `initAgent`
- [x] 7.2 Replace the three `RunAgent(...)` sites (`run.go`, `chat.go` x2) with draining `assembledAgent.Run(ctx, input)` through `render.Text(os.Stdout)`, surfacing `it.Err()` (preserve `context.Canceled` handling in `chat`)

## 8. Tests

- [x] 8.1 `agent_test.go`: agentic fake over `[]*schema.AgenticMessage`; update ReAct/cancellation/context-window tests to the new API
- [x] 8.2 `history_middleware_test.go`: retype fake + assertions to `*schema.AgenticMessage`
- [x] 8.3 `runner_test.go`: update only if `RunAgent` removal breaks compilation (do not delete the no-op placeholders)
- [x] 8.4 `internal/llm/adapter/*_test.go`, `service_test.go`: update for agentic returns

## 9. Verification

- [x] 9.1 `go build ./...` and `go vet ./...` clean
- [x] 9.2 `make test` green (`./internal/agent/...`, `./internal/llm/...`, `./internal/cli/...`)
- [x] 9.3 `make run ARGS='run "<prompt>"'` output unchanged; `onclaw chat` streaming + Ctrl-C + multi-turn history + summarization
- [x] 9.4 Headless check: `(*Agent).Run` has no `io.Writer` parameter; the `agent` package performs no presentation writes