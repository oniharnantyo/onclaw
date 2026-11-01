# Design

## D1: Evidence chain

1. Truncation: Z.AI Anthropic-compatible endpoint, agent cap unset → Claude-family 4096 default; `create_document` arguments embed the full document as JSON-escaped markdown → stream ends mid `input_json_delta`; eino's tool node logs `[LocalFunc] failed to unmarshal arguments in json, toolName=create_document, err=unexpected EOF` and returns that text as the tool result.
2. Poisoned retry: the truncated arguments string stays in the ADK message history; `agenticclaude@v0.1.6/convertor.go` `functionToolCallToBlockParam` splices it as `json.RawMessage(call.Arguments)` with no validity check → the request body itself is malformed JSON → provider 400 `Invalid request body: body.8176778: JSON decode error` → `NodeRunError`, turn dead, unrecoverable on retry.

## D2: Fix at the model seam, not the connector

Wrap the model returned by `registry.Build` in a buddies-owned decorator implementing `model.BaseModel[*schema.AgenticMessage]`. `Generate` and `Stream` sanitize the input slice before delegating: for every message, if any `FunctionToolCall` content block has non-empty arguments that fail `json.Valid`, build a copied message (new `AgenticMessage` value, copied `ContentBlocks` slice, new block + new `FunctionToolCall` for the offender only — never mutate shared history) with arguments `"{}"`. The connector's existing empty-args path emits `{}`. Applies to every family uniformly (harmless where the family serializes arguments as a quoted string; decisive where it splices raw JSON). ADK state keeps the original fragment — only the outbound view is sanitized, and the tool-level error result already in history tells the model what happened.

## D3: Default cap 4096 → 16384 (reviewable)

At 4096, the sanitizer turns a hard failure into a likely retry loop: the model re-issues the same oversized `create_document` call, which re-truncates, bounded only by the 100-iteration cap — expensive and unproductive. 16384 fits realistic document payloads on modern providers while staying modest; the agent-pinned cap (now honored on all families) remains the real control. Flagged in the proposal for easy revert.

## D4: Non-goals

- Request body size / context bloat (compaction's domain).
- Detecting and labeling `length` stops differently in the UI (already a completed partial turn with resume).
- Vendored connector patches or upgrades.
