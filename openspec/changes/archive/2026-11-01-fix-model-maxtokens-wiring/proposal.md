# Proposal

## Why

The agent's per-response output-token cap (`Agent.MaxTokens`, "per-turn output cap; 0 means the provider default" per `internal/store/types.go`) is honored only by the Claude connector. The openai-family and Gemini connectors never receive it, so agents on those providers run at the provider's own default output budget. On reasoning models that budget is consumed by thinking tokens first, so long reasoning gets chopped mid-stream (observed live: a turn's reasoning block died mid-sentence with no error, and the composer showed its resume affordance — a provider-side `length` finish, not the tool-call iteration cap, which always posts an explicit notice).

## What Changes

- Pass the agent's raw `MaxTokens` value through model construction to every provider family instead of pre-substituting the 4096 default in the registry.
- Wire the cap natively per family: openai-family sends `max_completion_tokens` (the connector's `MaxCompletionTokens`, which covers reasoning + visible output), Gemini sends `MaxOutputTokens`, Claude keeps today's behavior (its API requires the field; registry default 4096 applies there, thinking-budget headroom preserved).
- `0` (unset) now means provider default on openai-family and Gemini — matching the stored field's documented meaning; Claude keeps the 4096 substitution because its API has no omit option.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-runtime`: the Provider registry requirement gains per-turn output-cap resolution — the agent's configured cap rides the provider request through each family's native parameter, and an unset cap falls back to the provider default (except the Claude family, which requires an explicit value and keeps its built-in default).

## Impact

- `internal/agents/registry.go` (`buildModel` stops pre-substituting `DefaultMaxTokens`), `internal/agents/connectors.go` (`buildAgenticOpenAIModel` / `buildAgenticGeminiModel` signatures gain the cap; Claude builder takes over its default substitution).
- `internal/agents/compose/compose.go` untouched (`DefaultMaxIterations` is unrelated; `DefaultMaxTokens` stays referenced by the Claude path).
- No UI change (the agent config field already exists); no stored-data change; turn behavior for agents that leave the field 0 changes only in that openai-family/Gemini requests stop overriding the provider default — they never sent one before, so unset agents keep identical provider behavior.
