## Context

The fold from adopt-assistant-ui-elements (10.1, D8) gates only `p.k === 'tool'` segments behind `showCards`; reasoning segments render unconditionally (`AgentMessage.tsx` parts map), and the header counts `tools.length`. Result: a collapsed heavy turn still shows its Thought rows as orphans under the header, and the expanded stack outcounts the header. The legacy flat-reasoning branch has the same shape (its `ReasoningBubble` also ignores the fold). Live liveness lives in per-row surfaces — pending card dots and the "Thinking" tail bubble — all of which hide behind the fold. D8 rejected assistant-ui's "mute per-tool renderers" guidance wholesale; the reference elements (ToolTimeline `activeLabel`, ThinkingIndicator) show the rejection was right for cards and wrong for reasoning. Assistant-ui's ToolTimeline suppresses reasoning parts entirely (`Reasoning: () => null`) and carries streaming state on the header; ThinkingIndicator's label hook names the live activity ("Thinking" / "Running ${toolName}") with client-measured elapsed and yields (`undefined`) at rest.

## Goals / Non-Goals

- Goals: one fold rule (everything but text), a header that carries liveness, one status vocabulary across the turn surfaces. Pure frontend; no wire changes.
- Non-Goals: muting or thinning tool cards (D8's card rejection stands); compact-row timeline anatomy (assistant-ui's thin rows — our cards are the design contract); scheduler/channel surfaces; backend changes of any kind.

## Decisions

- **D1 — The fold owns everything except the text (amends D8's ruling, split in two).** D8's "mute per-tool renderers" rejection stays for tool cards; reasoning rows now follow the fold. Alternatives: (a) mute reasoning entirely on heavy turns — assistant-ui literal, but it deletes thought content on exactly the turns doing the most work, making heavy turns less informative than light ones; (b) compact rows — rebuilds the ToolCall surface for no user-reported gain. Both rejected.
- **D2 — Status contract = ToolTimeline `activeLabel` composed with ThinkingIndicator's hook.** While `busy && isLast` on a heavy turn: a pending tool call → "Running <display name>"; otherwise → "Thinking". Deliberate divergence from the element: ThinkingIndicator returns `undefined` once content exists because their reasoning renders in the body — ours is folded, so the header is the *only* live surface when collapsed and must keep showing "Thinking" rather than yielding to the folded content.
- **D3 — Label naming reuses the human-readable display names** from `toolDisplay.ts` (the same vocabulary the cards use — "Running Shell", not "Running execute"). No new mapping; honors the no-raw-tool-ids ruling from human-readable-tool-cards.
- **D4 — Elapsed time is the ReasoningBubble clock at turn scope.** Same client-measured pattern already shipped: clock starts when the live turn first renders, freezes when the run ends, formatted with `formatLatency`, present-only (hydrated history renders neither elapsed nor shimmer). Same honesty rule, one more adopter.
- **D5 — Shimmer is a new primitive: `od-shimmer`.** Animated gradient text sweep (`background-clip: text`), the span keyed on the label text so each label flip replays the sweep, as the element does. `prefers-reduced-motion: reduce` renders the same label and elapsed as static muted text (the ticking elapsed keeps liveness legible). Chosen over reusing the pulsing dot by explicit user decision — the shimmer is the element's signature; the dot stays on pending cards.
- **D6 — Steps count tool calls only.** Folded thoughts are content, not steps; counting them would flicker the resting label as segments land mid-stream and matches the "N steps" convention the header already set. Expanded-row count intentionally differs from the label.
- **D7 — One shared shimmer-label component, two hosts.** A small `ActivityLabel` (label + optional elapsed + shimmer + reduced-motion fallback) is rendered by `ToolTimelineHeader`'s streaming branch and by `ThinkingRow` (keeping its avatar row and `role="status"` semantics). `ToolTimelineHeader` keeps the resting summary; `AgentMessage` keeps owning the fold gating and passes `streaming = busy && isLast` plus the pending-tool display name down.
- **D8 — The expanded fold body nests under the header behind a thin left rail.** Amends the original "expanded = exactly as an unfolded turn renders it" ruling, which left the header and the rows as visual siblings (flagged by the user during the 2026-09-21 live visual pass against assistant-ui's ToolTimeline, where steps indent beneath the header). When the fold is open on a heavy turn, the cards and Thought rows render inside an indented container with a subtle left border, so the header reads as their parent; the reply text stays flush outside the rail. Light turns are unchanged (no header, no rail), cards keep their full anatomy, collapsed shows only the header.

## Risks / Trade-offs

- [Shimmer across many concurrent streaming heavy turns reads as noise] → Shimmer exists only while streaming and only on the header; at rest everything is static, and channels/schedulers keep today's rendering.
- [The existing snap when the 4th tool call folds the turn gets wider — thoughts now fold too] → Accepted: it is one flip at one moment, and the status label immediately explains the fold ("Running …"), so the turn never looks dead.
- [MODIFIED delta targets a requirement still only in the unarchived adopt-assistant-ui-elements delta] → Stacked-change convention: that change archives first (chronological), landing the requirement in main before this delta syncs.
- [Reduced-motion users lose the shimmer cue] → Elapsed keeps ticking and the label keeps flipping; the sweep is the only thing dropped.

## Migration Plan

Frontend-only; ship with the web build. No data, wire, or schema migration. Rollback is a revert of the wave.

## Open Questions

None — shimmer vs pulse, count semantics, and ThinkingRow scope were user-locked during exploration.
