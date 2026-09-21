## Why

The heavy-turn timeline fold (adopt-assistant-ui-elements 10.1) only hides tool cards: reasoning bubbles render outside the collapse regardless of its state, so a collapsed "6 steps" turn still stacks five orphaned "Thought" rows under the header, and the expanded stack shows more rows than the header counts. Live turns read the same way — every live surface (pending card dots, the "Thinking" tail bubble) sits behind the fold, so a collapsed streaming turn shows unlabeled orphan rows and no sign of activity.

## What Changes

- On heavy turns (≥4 tool calls) the fold owns everything except the reply text: tool cards AND reasoning rows render inside the collapse. Collapsed = header + text. Expanded = the familiar cards and Thought rows in stream order, exactly as today.
- The timeline header adopts the turn-status contract from assistant-ui's ToolTimeline `activeLabel` + ThinkingIndicator: while the turn streams, a shimmering label names the live activity — "Thinking…" when the model is generating, "Running <tool>…" when a tool call is pending — with client-measured elapsed time; at rest it renders the existing resting summary "N steps · M files changed".
- The resting step count stays tool-calls-only: folded Thought rows are content, not steps.
- ThinkingRow (the pre-first-token row) joins the same status vocabulary: a shimmering "Thinking" label with elapsed time replaces the bare pulsing dot.
- New `od-shimmer` CSS primitive (animated gradient text sweep) with a static-label fallback under `prefers-reduced-motion`.
- Light turns (<4 tool calls) are unchanged. The legacy flat-reasoning render path is covered by the same fold rule.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `web-app/chat`: the "Tool group timeline collapse" requirement is rewritten — the fold gates reasoning rows as well as tool cards, the step count is defined as tool calls only, and the header carries the streaming activity status; a new "Turn activity status line" requirement pins the shimmering label, elapsed time, and its ThinkingRow adoption.

## Impact

- `web/src/components/chat/AgentMessage.tsx` — fold gating extended to reasoning segments and the legacy flat-reasoning path.
- `web/src/components/chat/ToolTimelineHeader.tsx` — streaming status label + elapsed, resting summary unchanged.
- `web/src/components/chat/ThinkingRow.tsx` — label + elapsed upgrade.
- Shared shimmer label component (new) and the `od-shimmer` primitive in the global stylesheet.
- Tests: AgentMessage, ToolTimelineHeader, ThinkingRow. No wire, backend, or schema changes.
