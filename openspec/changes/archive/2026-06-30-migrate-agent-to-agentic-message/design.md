## Context

The agent has been migrated to eino's agentic message path. This change finishes that migration, makes the runner headless, dedups CLI gathering, and replaces the single OpenAI-compat adapter with dedicated per-provider agentic adapters.

Established facts (eino `v0.10.0-alpha.9` + eino-ext `main`):
- `type AgenticModel = BaseModel[*schema.AgenticMessage]` (`components/model/interface.go:109`).
- Agentic agent ctor `adk.NewTypedChatModelAgent[M]` (`adk/chatmodel.go:788`); config
  `adk.TypedChatModelAgentConfig[M]`, `Model model.BaseModel[M]` (`:575`).
- Summarization is agentic-ready: `summarization.NewTyped[M]`.
- eino-ext ships dedicated agentic model packages under `components/model/` (`agenticopenai`,
  `agenticclaude`, `agenticgemini`, `agenticdeepseek`, `agenticqwen`, `agenticark`).
- No public `Message`<->`AgenticMessage` converter exists.

## Goals / Non-Goals

**Goals**
- Tree builds; the agent runs on the agentic path end-to-end.
- One dedicated agentic adapter per supported provider.
- `(*Agent).Run` yields `*schema.AgenticMessage` and does no I/O; CLI output unchanged.
- CLI agent-session gathering deduped into one helper.

**Non-Goals**
- JSON/SSE/Web renderer or HTTP server (the `add-web-management-ui` change owns that; this
  change only makes the stream consumable).
- Resumable human-in-the-loop (`Interrupted` stays a terminal signal).
- Persistence migration shim (clean format break).

## Decisions

### 1. Go fully agentic, no bridge
Adopt `*schema.AgenticMessage` throughout; do not convert at a boundary. No public
`Message`<->`AgenticMessage` converter exists in eino, so a bridge would be hand-rolled and
fragile. Going fully agentic removes the need.

### 2. Dedicated agentic adapter per provider
One adapter file per provider maps `store.Profile` -> the package's typed Config and calls its
constructor -> `model.AgenticModel`. This replaces the lossy single-OpenAI-schema mapping and
restores provider-native capabilities (Claude thinking, Gemini safety, etc.). `ollama` and
`openai-compatible` use `agenticopenai` with `BaseURL` (no native agentic Ollama package).

### 3. Headless Run yields `*schema.AgenticMessage`, no custom Event type
`(*Agent).Run(ctx, input) EventIterator` pulls from `a.EinoAgent.Run` (agentic) and yields
`*schema.AgenticMessage` values; errors and completion ride on the iterator, not as message
kinds. A `render.Renderer` interface + `Text(io.Writer)` implementation reproduce today's CLI
bytes. No invented event vocabulary.

### 4. Clean persistence break
Persisted message JSON changes `schema.Message` -> `schema.AgenticMessage`. The feature is
newly shipped (archived `2026-06-30-add-sqlite-conversation-history`) and the tree is
non-building, so a clean break with no legacy read shim is acceptable.

### 5. Dedup via a shared CLI helper
Extract the ~150-line agent-session gathering (resolve agent -> workspace -> provider ->
profile -> model -> reasoning -> context window -> assemble) into
`internal/cli/agent_session.go`. The caller keeps the only divergence: conversation lifecycle
(`run` creates fresh; `chat` caches per agent).

## Risks / Trade-offs

- **Dependency compat (top risk):** the six `agentic*` packages are on eino-ext `main` and may
  require a newer `eino` than `v0.10.0-alpha.9`. Resolve first; a coordinated `eino` bump
  changes the agentic ADK surface and must be re-verified against Decision 1-3. Fallback: keep
  `libs/acl/openai.NewAgenticClient` for the OpenAI-compat path (it already returns an
  agentic-capable model) and defer the native per-provider packages — the rest of the migration
  is independent of model source.
- **Per-provider config/reasoning mapping** differs per package; cover each with a
  construct-smoke test (no live API call).
- **Summarization callback + history redaction on ContentBlocks** is the fiddliest existing
  logic to retype; verify with the existing summarization/history tests.
- **Cross-change:** `add-web-management-ui/design.md` Decision #5 references
  `RunAgent(ctx, a, input, sseWriter)`; update to `(*Agent).Run` + structured stream after this
  lands.