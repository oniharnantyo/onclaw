# Design

## Context

The runtime is stateless per turn: every turn rehydrates model input from the sqlite event log plus the persisted checkpoint (message-shaped), runs one ephemeral ADK run, and drains events back into the log as buddies' own `sessions.Event` vocabulary. `adopt-eino-middlewares` (committed immediately before this change) landed the eino middleware set on `*schema.Message` with the subagent/backgroundtask swap in `internal/agents/subagent.go` — but the tree also carries an orphaned near-duplicate in `internal/agents/subagents/` (`BuildSubagentCapabilities`, zero callers), and the live path bridges task identity through a wrapper middleware that swaps the eino `agent` tool at `BeforeAgent` and falls back to parsing `"with ID: "` out of the tool's result string. Upstream, eino's ADK is typed over a sealed `MessageType` (`*schema.Message | *schema.AgenticMessage`), and eino-ext ships per-provider agentic model components plus a stock local filesystem backend. The reference for the delegation stack's shape is eino's `adk/prebuilt/deep/deep.go` at the pinned tag.

## Goals / Non-Goals

**Goals:**
- One message type runtime-wide (`*schema.AgenticMessage`) with per-provider agentic model components in the catalog.
- One subagent implementation in `internal/agents/subagents/` with deep.go's shape: config, validation, single constructor, native middleware configuration.
- The delegation tool contract identical to eino's native schema, so no tool-swapping wrappers and no string-parsed ID handoff.
- `read_file` multimodal rendering via `eino-ext/adk/backend/local` composed beneath the existing jail.
- Historical sessions stay readable (versioned checkpoint payload).

**Non-Goals:**
- Dual-mode message types (per-model `*schema.Message` fallback): the compatible lane is gated instead (D2).
- Server-side tool / MCP content blocks, durable subagent runs, background shell lanes (deep's `RecoverableShell`/`LocalShell` on the filesystem middleware): buddies owns search, MCP, and shell client-side.
- Frontend redesign: the drain's `sessions.Event` vocabulary is unchanged; only the tool-schema mapping moves.
- Migrating eino past the pinned `v0.10.0-alpha.35` (spike may force a bump; see D6).

## Decisions

### D1: Agentic migration is runtime-wide, gated at the provider registry

The turn pipeline (compose, run, drain, checkpoint, transcript, subagent lane) standardizes on `*schema.AgenticMessage`. Each catalog provider family binds to its eino-ext agentic component (`agenticclaude` on native content blocks, `agenticgemini`, `agenticopenai` on the Responses API); openai-compatible endpoints ride `agenticopenai` with the configured base URL. Endpoints that do not implement `/v1/responses` are gated unavailable at turn resolution — a capability predicate next to the existing `RequiresAPIKey` gate — with the reason surfaced per turn, not silently dropped. Alternative considered: dual-mode (message type chosen per model, dual-shape checkpoints) — rejected: two code paths through compose/run/drain and a second checkpoint shape to maintain, to serve endpoints that mostly cannot answer; gating is honest and reversible when upstream grows a bridge or servers grow the API.

The drain boundary is the blast-radius firewall: `sessions.Event` vocabulary (text, tool activity, task activity, usage) is already provider-agnostic and does not change, so the frontend mapping survives with only the `agent` tool schema moves (D4).

### D2: Checkpoint payload gets a version marker with dual-read

`compact.go`'s persisted checkpoint stores model-shaped messages; rehydration replays them into model input. The payload gains a `version` marker (`agentic-v1`); unmarked payloads are read as legacy `*schema.Message` and converted to content blocks on rehydrate (assistant text → gen-text block, reasoning → reasoning block, tool calls/results → function blocks). Alternative considered: converting historical checkpoints lazily on write — rejected: a turn may run several sessions before any write; read-time conversion is total and stateless.

### D3: The subagents package owns the stack, deep.go-shaped

`internal/agents/subagents/` becomes the single home (root `subagent.go` and the orphaned duplicate deleted). The package mirrors deep.go's structure: a `Config` struct carrying persona/instruction, model, child tool surface, the manager-owned deps (workspaces, checkpoint store for the shell approval gate, context limit, skills/hooks capabilities), tasks/output dirs, and the task registry; `validateConfig` with explicit mutual-exclusion errors; one `New(ctx, cfg)` that composes the child clone internally (builtin middlewares without delegation tools — background is never propagated to children), the `bgtask.Manager` over the bridging store, and the two middlewares; nil-safe helper extractors for optional config. Import direction stays one-way (`subagents → compose`; the compose package never imports subagents). Alternatives considered: caller passes the composed child (smaller package, but the clone recipe leaks back into the root); `deep.New` wholesale (fights plantask/persona/jail decisions — mostly turning deep off).

### D4: Native tool contract; the bridging store is the only registry seam

The `subagent` middleware is configured natively (`ToolName` defaults to `"agent"`; eino's arg schema `subagent_type`/`prompt`/`description`/`run_in_background`; eino's result shapes). Both wrapper middlewares die. The `taskRegistry` survives solely as the `sessions.TaskActivity` bridge, and its birth point moves from the tool call to the bridging store's `Create` — eino routes **all** runs (foreground included) through the Manager once `Background` is set, so one seam covers both lanes; `setPending`, `latestAllocatedID`, and the `"with ID: "` parse are deleted. Transcript materialization keeps the current `transcriptEntry` JSONL shape through the `TranscriptFormat` closure (the fs tools and right-panel trajectory reader are its consumers). Hooks matching `agent`/`task_output`/`task_stop` by name keep working: tool names are unchanged.

### D5: Upstream local backend composed beneath the jail

`internal/agents/backend` keeps its public surface (workspace rooting, jailed shell with the checkpoint-store approval gate) and delegates the file operations (read/write/edit/glob/grep/ls) to `eino-ext/adk/backend/local`'s `Local` behind a path-translation shim (workspace-relative → jailed absolute), gaining `MultiModalRead` for `read_file` (image parts, whole or paged PDFs via in-process WASM pdfium). The upstream `Execute` (`/bin/sh`, no approval gate, no Windows) is not adopted — buddies' jailed shell stays the exec path. Size caps for multimodal reads come from the upstream defaults unless a buddies-side limit is stricter. Alternative considered: replacing the jail with upstream entirely — rejected: it loses workspace rooting and approvals, both spec-pinned.

### D6: Version spike before any swap

The eino-ext agentic components and `adk/backend/local` target eino `v0.9.x` while the repo pins `v0.10.0-alpha.35` (Go MVS keeps alpha.35). First task: a compile-and-smoke spike — add the four modules, build the catalog, run one fake-model turn through `AgenticMessage` end to end. If API drift breaks the components, the fallback is bumping eino to the newest tag the components support (the middlewares adopted in `adopt-eino-middlewares` are re-verified against the bump in the same spike). No swap work starts until the spike is green.

**Outcome / Resolution:**
- API drift inspection revealed that `eino-ext` components (`agenticclaude`, `agenticgemini`, `agenticopenai`, `libs/acl/openai`) access `PromptTokenDetails.CacheWriteTokens`, which was added to upstream `eino` main in `v0.9.21` (commit `ba04fde`), but was missing on the `v0.10.0-alpha.35` branch. Meanwhile, `v0.9.x` lacks `adk/backgroundtask` and `adk/middlewares/subagent` adopted in `adopt-eino-middlewares`.
- Resolution: `v0.10.0-alpha.35` is vendored at `third_party/eino` with `replace github.com/cloudwego/eino => ./third_party/eino` in `go.mod`. Upstream commit `ba04fde` was applied to include `CacheWriteTokens` in `schema.PromptTokenDetails` and `model.PromptTokenDetails`.
- Verification: `go build ./...` and `go vet ./...` are completely clean. Existing test suite across `internal/agents/...` passes 100%. The fake-model spike (`TestAgenticRuntimeSpike_EndToEnd` in `internal/agents/agentic_spike_test.go`) passed verifying end-to-end reasoning, function tool call, and function tool result handling over `*schema.AgenticMessage`. All swap work is unblocked.

## Risks / Trade-offs

- [eino-ext components drift against pinned eino alpha] → D6 spike gates all swap work; fallback is a coordinated eino bump with middleware re-verification.
- [Compatible-endpoint gating strands configured providers] → The turn error names the missing API and the catalog marks affected models unavailable, so the failure is legible; lifting the gate later is a capability-predicate change, not a migration.
- [Checkpoints converted at read could mistranslate exotic legacy payloads (multi-part multimodal content)] → The converter covers the vocabulary the runtime actually persisted (text, reasoning, function calls/results) and passes unknown parts through as text with a marker, never dropping them silently; covered by round-trip tests over real-session fixtures.
- [Agentic streaming reshapes drain event timing] → The drain already maps message chunks to `sessions.Event`s; wave-level streaming tests pin delta parity (text and reasoning) against the current behavior before the swap deletes the old path.
- [`buildTags`/bindings churn] → Bindings regen at wave end only (documented gotcha: breaks parallel frontend work).

## Migration Plan

Commit `adopt-eino-middlewares` first (rollback base). Waves: W1 version spike (D6) → W2 provider catalog + agentic model components → W3 runtime message-type migration (compose/run/drain/checkpoint dual-read) with streaming parity tests → W4 subagents consolidation on agentic types (wrapper deletion, `Create`-birth bridge) → W5 backend/local composition + multimodal reads + frontend tool-schema updates + bindings → W6 spec sync + verify. Rollback is per-wave: `go test ./...` green and the app functional at each boundary.

## Open Questions

- Whether Ollama's partial `/v1/responses` support (no vision input, some item types rejected) earns a narrower capability predicate (e.g. per-model feature bits) instead of the family-level gate.
  **Decision / Outcome (Task 2.4):**
  Inspection of Ollama's HTTP surface reveals that Ollama does not implement `/v1/responses` natively (returning 404). Because `agenticopenai` communicates directly over `/v1/responses`, requests cannot be served regardless of model capabilities (vision vs text). Therefore, Ollama local is gated at the endpoint/family level with a named explanation (`Ollama lacks native /v1/responses support; requires /v1/chat/completions`).
- Whether the in-run summarization token counter needs agentic-aware sizing (content blocks vs flat strings) — resolved in W3 by measuring the estimator against real block payloads; the layered 0.8/~0.9 thresholds stay.
