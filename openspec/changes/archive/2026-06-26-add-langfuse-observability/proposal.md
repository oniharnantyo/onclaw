## Why

onclaw has strong *local* observability — an append-only per-turn `.jsonl` transcript (cross-ref `agent-core`), `slog` logging, and secret redaction at the tool and transcript boundaries (cross-ref `agent-tools`, `providers`) — but **no external observability**. When something goes wrong in a multi-turn, tool-calling run, there is no remote trace of the model calls, tool calls, and loop iterations to inspect. This change adds **opt-in Langfuse tracing** of the full agent execution tree, so a run can be observed end-to-end in a Langfuse project.

Eino's callback model makes the integration lightweight: a single `callbacks.Handler` registered globally via `callbacks.AppendGlobalHandlers` is fanned out automatically across every model call, tool call, and agent-loop iteration of the ADK `ChatModelAgent` — no per-node wiring. Langfuse ships exactly such a handler in `github.com/cloudwego/eino-ext/callbacks/langfuse` (verified importable at v0.1.1 alongside the pinned eino v0.9.9). Because tracing egresses model I/O to an external server, the integration **reuses onclaw's existing redaction by default** so it does not weaken the `providers` secret-disclosure contract.

## What Changes

- Add an opt-in **`langfuse.*` config section** (`host`, `public_key`, `secret_key`, plus optional `session_id`/`release` and `mask`) backed by `ONCLAW_LANGFUSE_*` env vars, following the existing `defaults < file < env < flag` layering. The `secret_key` is a credential and SHOULD be supplied via env to avoid plaintext in `config.yaml`.
- Add a new **`internal/observability` leaf package** that, when enabled, builds a Langfuse callback handler from config, registers it globally on the eino callback bus, and returns a flush function. It takes the mask function as an injected parameter (reusing `tools.Redact`) and depends on nothing but eino + the langfuse module + stdlib — no dependency on `internal/agent/tools` or `internal/config`.
- Wire **`onclaw run`** to call `observability.Setup(...)` (injecting `tools.Redact`) immediately before `agent.RunAgent`, and `defer` the returned flusher so buffered events are delivered during run teardown.
- **Mask secrets by default** before egress (the same redaction used for transcripts/tools); `langfuse.mask: false` disables it.
- Treat `langfuse.secret_key` like provider API keys: **never logged, redacted in `onclaw config show`** (governed by the existing `providers` secret-disclosure requirement — no spec change there).
- Tracing is **disabled (no-op, no error) by default**; partially-configured Langfuse (e.g. host set but no key) is a hard error naming the missing fields.

**Out of scope (later changes):** other tracers (Langsmith, OpenTelemetry); per-agent or per-run enable toggles beyond the global config (a `--no-trace` flag is a natural follow-up); linking Langfuse session IDs to onclaw transcripts/`--resume`; tracing commands other than `onclaw run`.

## Capabilities

### New Capabilities

- `agent-observability`: opt-in Langfuse tracing of the agent execution tree (model calls, tool calls, multi-turn loop) via a globally-registered eino callback handler; default-on secret masking before egress; flush-before-exit; credential non-disclosure.

### Modified Capabilities

_None._ The existing `providers` requirement that secrets are never disclosed in config output or logs already governs `langfuse.secret_key`, so no edit to `providers` is required.

## Impact

- **Code:** `internal/config/` (new `LangfuseConfig` + defaults + `SetDefault` lines), new `internal/observability/` package, `internal/cli/run.go` (register + defer flush before the agent run), `internal/cli/config_cmd.go` (redact `langfuse.secret_key` in `config show`), `internal/logging/` (extend the redacted-field set if it keys on field names).
- **Dependencies:** add `github.com/cloudwego/eino-ext/callbacks/langfuse@v0.1.1`. MUST remain pure-Go (`CGO_ENABLED=0`) so `make build-all` still cross-compiles to arm64/armv7; preserve the ~2 GB RAM discipline (one agent running at a time; async/best-effort event flush).
- **CLI:** no new commands or flags in this change.
- **Specs:** adds one new capability spec (`agent-observability`).
- **Basis:** `.claude/plans/integrate-agent-to-langfuse-buzzing-shannon.md`.
