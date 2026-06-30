## Why

The agent needed to be migrated from eino's legacy `*schema.Message` path to the
`*schema.AgenticMessage` ("agentic") path to resolve type disagreement end-to-end. Previously,
the model-construction layer was partially migrated while the consumers (the stub adapter,
history middleware, runner, and CLI) remained legacy, causing compilation failures.

This migration was completed in this change, retyping the remaining components to
`*schema.AgenticMessage`. In addition, dedicated agentic model adapters were built for each
provider type to support native provider capabilities (such as Claude thinking), and the runner
was refactored to be headless for cleaner web/SSE UI rendering.

## What Changes

- **Complete the agentic migration** end-to-end so the tree builds: retype `Agent.EinoAgent`,
  the agent config/constructor/Handlers, the summarization callback body, and the history
  middleware to `*schema.AgenticMessage`; make `Service.BuildWithProfile` return
  `model.AgenticModel`; make the stub satisfy `model.AgenticModel`. **BREAKING** to the
  conversation-history persistence format (`schema.Message` JSON -> `schema.AgenticMessage` JSON).
- **Integrate dedicated agentic model adapters per provider** from eino-ext
  `components/model/` (`agenticopenai`, `agenticclaude`, `agenticgemini`, `agenticdeepseek`,
  `agenticqwen`, `agenticark`) — one adapter file per provider — replacing the single
  OpenAI-compat adapter. `ollama` and `openai-compatible` route through `agenticopenai` +
  `BaseURL` (no native agentic Ollama). Anthropic gains a real implementation.
- **Make `Run` headless**: `(*Agent).Run` yields a stream of `*schema.AgenticMessage` and
  performs no I/O; a new `internal/agent/render` package formats them for the CLI with
  byte-identical output. The free `RunAgent(ctx, a, input, stdout)` is removed.
- **Dedup** the agent-session gathering into one shared CLI helper used by `run` and `chat`.

## Capabilities

### Modified Capabilities

- `agent-core`: the agent runs on the agentic message path; the runner yields a structured
  message stream instead of writing to `stdout` (the streaming guarantee is preserved — the CLI
  renderer writes tokens progressively); model construction builds an agentic model per provider.
- `conversation-history`: persisted message format changes from `*schema.Message` to
  `*schema.AgenticMessage`; the history middleware and summarization callback operate on
  agentic messages.

## Impact

- **Code:** `internal/agent/{agent.go, runner.go, render/ (NEW), middlewares/history_middleware.go}`,
  new `tools.RedactAgenticMessage`, `internal/llm/{service.go, adapter/{agentic_*.go (NEW),
  openai_compat.go (retired), stub.go, defaults.go}}`, `internal/cli/{run.go, chat.go,
  agent_session.go (NEW)}`, and the affected `_test.go` files.
- **Dependencies:** add six eino-ext `components/model/agentic*` modules; verify compatibility
  with the pinned `eino v0.10.0-alpha.9` (may require a coordinated `eino` bump).
- **Schema migration:** none (table shape unchanged); persisted message JSON format changes —
  clean break, no read-side legacy shim (the feature is newly shipped and the tree is
  non-building, so existing rows are test-only).
- **Behavior:** `onclaw run`/`onclaw chat` output unchanged; agents run on the agentic path;
  Anthropic works natively; the agent core no longer performs presentation I/O.
