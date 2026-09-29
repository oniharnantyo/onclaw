# Proposal

## Why

The L1 base prompt under-teaches what the platform makes possible and how to behave: the rich-cards fence catalogue lists JSON shapes without purpose or trigger guidance (the `diagram` tag gets one prose sentence and no example), and the prompt carries no execution-discipline, honesty, follow-through, or reply-sizing teaching at all. Peer review of OpenClaw (surface-gated affordance sections, when-to-use lists with concrete cases, promised-work ownership) and Hermes (finishing-the-job, anti-fabrication, parallel tool calls, reply sizing) shows mature agents ship exactly this teaching in their base prompts. Meanwhile a live incident (2026-09-26) showed an agent with the teaching undelivered drifting into matplotlib-PNG detours — trigger reliability of the prompt itself is the remaining lever once transport is fixed.

## What Changes

- Rewrite the Rich cards per-tag catalogue so every fence tag carries what it renders and when to reach for it (purpose + concrete triggers + "not for" redirects), keeping the fixed JSON shape syntax.
- Add example fences for the two exception tags (`diagram` with its info-string title, `mermaid` raw source) and an explicit missing-title failure warning; add a cross-tag chooser line and an anti-pattern line ("a diagram is not a substitute for the answer text").
- Add a markdown-first clause: tabular data needs no fence — native markdown tables render in chat.
- Add four new behavioral sections to the base prompt, adapted from OpenClaw and Hermes: **Execution** (act now, batch independent tool calls, prerequisites first, live-check mutable facts, vary-then-conclude, long-work persistence), **Finishing & Honesty** (real artifact over description, verification before finalizing, read-back external writes, never fabricate, literal preservation, missing-context ladder), **Follow-through** (progress is not an answer, promises create ownership, `schedule` beats polling), **Communication & Output** (reply length matches the ask, no filler, earned depth, distinguish facts/tool output/reasoning).
- Reframe the existing four "Core Directives" into Tenant Boundaries, Persona & Alignment, and Capability Scope (content preserved; tool-usage prose folds into the new sections).
- Prompt-only change: no runtime, API, or web behavior changes. The file is platform-embedded (`go:embed`) and reaches every agent on next server start.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-prompts`: the L1 base prompt's carried content becomes a specified requirement — behavioral teaching sections (Execution, Finishing & Honesty, Follow-through, Communication & Output) plus the rich-cards catalogue's per-tag purpose/trigger guidance, exception-tag examples, markdown-first clause, and chooser/anti-pattern lines. The existing "Prompt documents are workspace files" requirement is untouched except that the base-prompt reference stays as-is (embedded content).
- `agent-runtime`: the composition requirement's rich-cards guidance sentence is widened from "one shape per fence tag" to "one shape per fence tag with its rendering purpose and when-to-use triggers, example fences for the raw-source exception tags, a markdown-first clause for tabular data, and a cross-tag selection rule" — same injection position, all three compositions.
