# Design

## D1: Raw value plumb-through, substitution moves into the Claude builder

`buildModel` (`internal/agents/registry.go:91-94`) today substitutes `DefaultMaxTokens` (4096) before dispatching, so the family builders cannot distinguish "agent pinned 4096" from "unset". Change: pass `agent.MaxTokens` raw into `buildModel`/`buildAgentic*Model`; each family decides its own unset semantics. The Claude builder performs the `<= 0 → DefaultMaxTokens` substitution itself (its API requires `max_tokens`), keeping the existing thinking-budget headroom raise (`connectors.go:61-66`) byte-for-byte. OpenAI-family and Gemini builders send the cap only when `> 0`.

## D2: Family wirings

- **OpenAI / OpenRouter / openai-compatible (incl. Z.AI)**: set `ChatConfig.MaxCompletionTokens` (the agenticopenai connector's documented field — "visible output tokens and reasoning tokens", exactly the budget that truncates reasoning mid-stream). Do NOT use `ExtraFields["max_tokens"]`: `max_completion_tokens` is the modern parameter and the field already exists.
- **Gemini**: set `genai.ClientConfig.MaxOutputTokens` in `buildAgenticGeminiModel`. Verify the field exists on the pinned genai version during implementation (task 1.2); if the pinned version lacks it, fall back to the connector's own config field or an upgrade note.
- **Claude**: unchanged beyond receiving the raw value (`cfg.MaxTokens`, thinking headroom).

## D3: What "cap" counts

Reasoning tokens count against the cap on every family — that is the observed failure mode (long bilingual reasoning died mid-sentence at the provider default). The spec sentence says so explicitly so future family additions inherit the semantic. No client-side truncation detection is added: a `length` finish already surfaces as a completed partial turn with the composer's resume affordance, which is the desired UX; documenting it in the spec prevents someone later from turning it into a turn error.

## D4: Non-goals

- No UI change: `AgentConfigPane`/`CreateAgentModal` already bind the field, and its help copy ("0 means the provider default") becomes true on all families after this change.
- No per-session override of the cap (the session override surface stays model + effort only).
- No change to `DefaultMaxIterations` / the iteration-cap notice path — the original suspicion in the live incident; that mechanism posts its explicit notice and was not involved.
