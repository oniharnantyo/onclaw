# Design: chat-context-meter

## Context

Usage already flows end to end but dies at the last hop: the runner accumulates per-turn `UsagePayload` from provider-reported model-call usage (streaming frames, message metadata, span events), persists it, rebuilds it on history reads (`internal/agents/history.go`), and the `/v1` translator puts it on the terminal Response objects (`internal/openresponses/translate.go`). The web client handles `response.completed` but only reads the response id — usage is dropped.

The window side is equally settled: the stored `context_window` is catalog-filled at agent create/update (agent-runtime spec, "Context window resolution"), and execution resolves it as stored-value-or-200k (`domain.ResolveContextWindow(agent.ContextWindow, nil)`). Summarization fires at `window × DefaultSummarizationMargin` (0.75, an unexported constant; the `WithSummarizationMargin` option exists but nothing passes it — surface-only per the locked decision).

## Goals / Non-Goals

**Goals:**
- One new number everywhere: **final-call input tokens** — what the model's context last held.
- Meter reflects server truth: window and trigger line come from the agents API; the client holds no constants and no resolution logic.
- Meter survives reload using the existing history join.

**Non-Goals:**
- No live (mid-stream) context estimation — per-terminal-event updates only.
- No meter on channels or member DMs; no per-mentioned-agent meters.
- No `ONCLAW_SUMMARIZATION_MARGIN` config — the margin stays a compile-time constant (locked: surface only). Catalog-aware runtime resolution — already effectively achieved via write-time fill; not revisited here.
- No change to how the model receives context, billing numbers, or the Runs screen.

## Decisions

**D1 — Numerator is the last model call's input, not the per-turn input total.**
A ReAct turn re-sends the growing context each iteration; the summed input total (40k + 45k + 50k = 135k) is correct for billing but triples-counts context. The meter divides the *last* call's input (50k) by the window. Alternatives rejected: summed input (lies upward on every tool-using turn), client-side estimation from message text (fabricated, tokenizer-dependent).

**D2 — `UsagePayload.FinalInputTokens`, captured at each accumulation site.**
The runner already sees each call's usage before accumulating (`frameUsage` reassignment in the streaming path, message metadata in the agentic path, span events elsewhere); it additionally records the latest input seen into `FinalInputTokens` instead of summing it. `usageOf`'s all-zeros nil check includes the new field so providerless usage still omits the block. History rebuild (`history.go`) mirrors this: per turn, keep the last span-model-end input rather than summing. Additive and backward-compatible — old persisted events simply lack the field until their turn re-runs.

**D3 — Wire transport: extend the `usage` block, no new event type.**
`usage.final_input_tokens` rides the existing Response object on `response.completed` / `response.incomplete` / `response.failed`, and the transcript's turn events for reload. The project's `/v1` facade already carries `onclaw.*` extensions, but a sibling of the three existing usage fields is the smallest, most discoverable surface. Rejected: a dedicated `onclaw:context` event (a second thing to join against the terminal event) and top-level response fields (dilutes the usage block's cohesion).

**D4 — Trigger/window exposure computed in the agents handler from existing primitives.**
`effective_context_window = domain.ResolveContextWindow(a.ContextWindow, nil)`; `summarization_trigger_tokens = int(float64(effective) * agents.DefaultSummarizationMargin)` — the margin constant is exported from `internal/agents` (the handler already imports that package; no domain move, no new dependency). Computing at read time from the same function execution uses keeps the payload truthful even if resolution changes later. Fields are response-only: create/update requests ignore them.

**D5 — Meter state lives per-thread in web runtime state, updated on terminal events.**
`openresponses.ts` captures `usage` on all three terminal events; the runtime stores `{finalInput, at}` on the thread. Reload path: `livechat.ts`'s `turn_completed` hydration reads the same field from the rebuilt transcript. Rendering lives in `ChatHeader` (top-right control row, left of member stack/configure): a compact bar + mono `%`, hover tooltip `68k / 200k`, amber at `>= summarization_trigger_tokens`, hidden when no usage data exists or the target isn't an agent. No mid-stream updates: a streaming turn shows the previous turn's value until the terminal event lands.

## Risks / Trade-offs

- [Providers that report no usage] → meter hidden (spec'd), never a fake 0%; no behavioral risk.
- [Meter lags one turn during long streaming turns] → accepted: per-terminal-event honesty over fabricated live estimates; the previous value remains displayed.
- [Final-call input ≠ true context when the provider under-reports prompt tokens (e.g. cache-discounted accounting)] → first cut accepts provider-reported numbers; no client-side adjustment.
- [Web test suite has ~66 pre-existing localStorage failures] → judge by touched suites and build, per established practice.
- [Old persisted turns lack the new field] → their reload shows the meter from usage totals' absence of final-call input as hidden until the next turn lands; acceptable for an additive field.

## Migration Plan

Purely additive: no schema migration (usage rides existing persisted events), no breaking API change. Deploy backend first (new fields appear), web second (meter lights up). Rollback = revert web; unused backend fields are inert.

## Open Questions

None — margin surfacing locked to surface-only; placement, scope, and transport locked during exploration.
