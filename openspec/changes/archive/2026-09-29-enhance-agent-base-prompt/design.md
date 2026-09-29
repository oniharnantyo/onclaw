# Design

## Context

The base prompt is `internal/promptdocs/AGENTS.md` (76 lines, ~600 tokens), `go:embed`ded as `promptdocs.BasePrompt` and injected into all three compositions (attended, scheduler, heartbeat) at a fixed position — never materialized to disk. Today it teaches the fence catalogue as bare JSON shapes, gives `diagram`/`mermaid` one prose sentence with no example, and carries no behavioral teaching. Peer ground truth (repos read 2026-09-26): OpenClaw `src/agents/system-prompt.ts` (Execution Bias, Promised Work, Collapsible Details sections) and Hermes `agent/prompt_builder.py` (DEFAULT_AGENT_IDENTITY, TASK_COMPLETION_GUIDANCE, PARALLEL_TOOL_CALL_GUIDANCE, execution-discipline blocks). The chat markdown body already renders GFM tables and KaTeX natively; bare mermaid code blocks do not render (fences are the only diagram lane).

## Goals / Non-Goals

**Goals:**
- Per-tag trigger reliability: every fence tag taught with purpose + concrete when-to-use cases + redirects where confusion is likely.
- Exception tags exemplified (few-shot), with the `diagram` missing-title failure mode stated.
- Peer-grade behavioral teaching (execution, honesty, follow-through, reply sizing) in the same compact file.
- Prompt-only change; lands for every agent on next server start.

**Non-Goals:**
- Removing `table`/`math` fence implementations — that is `remove-markdown-redundant-fences` (web-side).
- Provider verify-lane system-prompt canary; instruction-as-user-message fallback; surface-gated teaching for gateway-delivered runs (all separate candidate changes).
- Temporal-context injection (workspace date/TZ in `renderWorkspaceDoc`) — a runner gap, not fixable in prompt text.

## Decisions

- **D1 — Borrow-and-compress, never verbatim.** Peers' sections are compressed to fit a prompt paid on every turn across all profiles with no prefix-cache amortization: OpenClaw's 8-bullet Execution Bias → 6 bullets; Hermes' four XML discipline blocks → one Finishing & Honesty section (~9:1); Promised Work's 7 bullets → 3. Attributions live in the change proposal discussion, not the shipped file.
- **D2 — Catalog keeps one-line-per-tag format** with purpose + triggers + redirect riding inline after each shape. Models already parse this shape; a separate explanatory paragraph would double the section for no gain. Examples are added only for the two raw-source exception tags, where the live failure actually happened.
- **D3 — The catalog ships the end-state 13-tag set** (no `table`, no `math`) with a markdown-first clause for tabular data, sequenced with `remove-markdown-redundant-fences` so the prompt file is edited once. Until that change lands the untaught tags still function; they are scheduled for deletion anyway.
- **D4 — Core Directives reframed 4 → 3.** Tenant Boundaries and Persona & Alignment stay verbatim; tool-usage prose folds into Execution; Communication prose folds into the new Communication & Output section. No normative content is lost.
- **D5 — The spec pins sections and rules, not wording.** The agent-prompts requirement names the teaching sections and their rules; the agent-runtime composition requirement widens only the rich-cards guidance sentence. Wording can evolve without spec churn.
- **D6 — Rejected:** verbatim OpenClaw conditional surface gating (OnClaw's attended chat surface is uniform — nothing to gate on yet); sub-agent minimal prompt modes (scheduler/heartbeat trims already exist).

## Risks / Trade-offs

- **+~500 tokens per turn on every composition** including unattended profiles. Accepted; if trimming is needed later the documented cut order is: literal-preservation bullet → prerequisites bullet → batching bullet → per-tag "not for" redirects. Measured at implementation: ~+710 tokens by the chars/4 heuristic (6251 → 9094 chars); the residual over the estimate is fully spec-mandated content (the baseline "~600 tokens" figure was itself miscalibrated), so the cut order was not applied.
- `promptdocs_test.go` and composer tests may pin current section shapes or counts and need matching updates (tasks cover this).
- Wording changes to system prompts can trip provider-side filters in rare cases (Hermes hit an Anthropic OAuth-credential filter on one phrasing); any post-ship provider errors should be bisected against this file.

## Migration Plan

None required — embedded content, live on next server start; no data, API, or schema changes.

## Open Questions

None.
