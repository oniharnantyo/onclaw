# Design

## Context

eino v0.10.0-alpha.35's `ChatModelAgent` loop dispatches tool calls through a ToolsNode keyed by tool name; with the default config, a call naming an unregistered tool returns a node-level error (`tool X not found in toolsNode indexes`) that aborts the graph. buddies' `ToolErrorResult` middleware (internal/agents/compose/toolerror.go) wraps tool endpoints, so it converts every failure that happens INSIDE a tool — but a default-config dispatch failure happens before any endpoint is invoked, and the turn dies with `turn_error`. Observed live on PoolSide laguna-s-2.1 (`list`, `read` for `ls`/`read_file`); any model can hallucinate a name.

## Goals / Non-Goals

**Goals:**
- An unknown tool name costs the model one retry, not the turn.
- The correction loop uses the same shapes the turn already has: tool-result message, error JSON payload, failure registry, transcript record.

**Non-Goals:**
- No fuzzy name matching or auto-substitution ("list" → `ls` is the model's job once told the name is wrong).
- No changes to eino, the ToolsNode, MCP tool gating, or the iteration cap.
- No new user-facing UI beyond the existing failed-tool record.

## Decisions

- **D1 — Seam: `ToolsNodeConfig.UnknownToolsHandler`** (amended 2026-10-29 after empirical probe tests disproved the original AfterModelRewriteState plan). The original D1 rewrote the model response in `AfterModelRewriteState`; probes proved that seam mechanically broken: whatever the hook appends to state becomes the model node's output, so a response reduced to zero call blocks terminates the loop before known tools dispatch (probe A `modelCalls=1 lsRan=0`; probe B2, the live incident shape, `modelCalls=1`), drop-only dies with `no tool call found in input message`, and inserting results before the response orphans them (openai/anthropic 400). eino's ToolsNode has a purpose-built hook: unknown names route to `UnknownToolsHandler` INSIDE the tool-execution machinery and pass through the invokable `ToolCallMiddlewares` (eino compose/tool_node.go) — which include `ToolErrorResult`. So the wiring is ~10 lines in `ComposeTurnAgent`: a handler that only ever returns an error (`unknown tool %q; use one of the tools offered in this request`) — its non-error return would bypass the error-result shape — and `ToolErrorResult` (always composed) converts it exactly like a real tool failure. Probes C2/C3: mixed and all-unknown responses both loop (`modelCalls=2`), the known tool runs, and `FailureOf(callID)` returns the error text.
- **D2 — Validation set: the ToolsNode indexes themselves.** The handler fires exactly for names outside the offered surface (the same set `state.ToolInfos` mirrors, implicit and late-registered tools included) — no false positives by construction, and no duplicate validation logic to drift.
- **D3 — Provider pairing untouched.** The assistant message is never rewritten: the unknown call stays in the response and is answered by the handler's error result through the normal result path, so every call id is paired exactly once — the strictness openai/anthropic require holds without message surgery.
- **D4 — One failure record, no duplicates.** The error flows the REAL drain path (runner.go's tool-result recording) with `ToolErrorResult` marking the call failed — the transcript shows exactly one failed record carrying the hallucinated name, with no new recording plumbing.
- **D5 — No new middleware in the chain.** The handler lives in the ToolsNode config composed per turn in `ComposeTurnAgent`; endpoint middleware order is unchanged.

## Risks / Trade-offs

- [Rewriting the model response mid-loop confuses a provider] → The mutation preserves the assistant tool_calls ↔ tool results pairing exactly (D3); a model that keeps hallucinating burns iterations until the cap notice, which is the existing bounded outcome.
- [A tool registered AFTER composition (deferred/MCP late-bind) would false-positive] → Validation reads `state.ToolInfos` per invocation, not a composition-time snapshot, so late-registered tools are included from the first invocation that offers them.

## Migration Plan

No data migration; a new middleware in the per-turn composition. Rollback is a plain revert.

## Open Questions

None.
