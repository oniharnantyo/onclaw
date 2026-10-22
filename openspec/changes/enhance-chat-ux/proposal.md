# Proposal

## Why

The chat view ported onclaw-v2's surfaces but stopped short of its behavior in four places that users hit every turn: the transcript never follows a live reply (new messages and streaming tokens land below the fold — `Transcript.tsx` only anchors prepends), tool calls render as one always-expanded block of raw arguments and raw result, the context ring has no breakdown behind it, and the sessions rail lacks onclaw's row grammar. Research across onclaw-v2, assistant-ui, and ZCode shows every gap is a known, solved pattern — and that buddies' tool payloads (eino's Claude-Code-shaped file tools, typed browser and web-search JSON) already carry enough data to render **interactive, tool-aware cards** that go beyond onclaw's params-and-result box.

## What Changes

- **Transcript follows the live tail.** While the reader is at the bottom, new entries and streaming deltas keep the transcript pinned; a genuine user scroll-up pauses following, a jump-to-latest control re-engages it, and expanding a card or reasoning bubble holds the scroll instead of fighting it. The existing prepend anchor for "load earlier" is kept.
- **Tool-call cards get onclaw's chrome and a per-tool renderer registry.** Collapsed cards show a catalog icon, display name, a human one-liner (intent while running, outcome when done), and latency; expanded cards show labeled sections with a line clamp and a raw toggle. Each builtin tool gets an interactive body: a real unified line diff for `edit_file`, code previews for `write_file`/`read_file`, a terminal block for `execute`, grouped matches for `grep`, file lists for `glob`/`ls`, result cards for `web_search`, an a11y-tree table and **inline screenshots with a lightbox** for `browser`, a checklist snapshot for `todo_write`. Unknown and MCP tools fall back to an honest pretty-JSON card with no invented sentence. Turns with many tool calls fold behind a summary. Collapse state survives re-renders and completed edits/reads auto-open.
- **Context ring opens a breakdown popover**: stacked bar and legend (Instructions / Tools & skills / Files / Conversation / Server context), headroom, and last-turn input/output — client estimates at face value, never scaled to fit the provider's total.
- **Sessions rail adopts onclaw's row grammar**: compact title-only rows, a running session shows an inline spinner with a pulsing title, and the list caps at four with a "Show N older sessions" expander (the active session always stays visible). Search, drag-resize, and delete confirmation are unchanged.
- **Assistant messages gain a copy action**, and code blocks gain syntax highlighting once a message completes (streaming stays plain). Streaming markdown MUST NOT paint transient artifacts from unclosed fences; a spike decides whether the current renderer is kept or replaced by a streaming-safe one.
- **Runtime exposes two more numbers**: tool activity terminal records carry the call's duration (the running form is live-only, so reload-safe latency must be persisted), and context usage carries the last turn's input and output tokens alongside used/limit.
- **A bound read serves browser screenshots** to the webview — the same capability-style mechanism attachments use — restricted to the browser tool's screenshots directory and to file names of the shape the tool writes.

Out of scope: moving the sessions rail back into the navigation sidebar (the island placement is a deliberate shell decision), a "New chat" row inside the list, server-side result envelopes (envelope detection stays client-side), side-by-side diffs, and the `@assistant-ui/*` dependency decision — the packages are present with no imports while the project context names assistant-ui as the chat stack; this change neither adopts nor removes them and records the finding in the design for a separate call.

## Capabilities

### New Capabilities

- `chat-tool-cards`: the tool-call card — collapsed/expanded chrome, one-liner sentences, latency, the renderer registry with per-tool interactive bodies (diff, code, terminal, matches, file lists, search results, browser snapshot and screenshot, plan snapshot), honest fallback behind an error boundary, heavy-turn folding, collapse-state persistence, and the screenshot read's constraints.

### Modified Capabilities

- `chat-view`: "Chat transcript rendering" gains follow-the-tail autoscroll with a jump-to-latest control and disclosure-pause, and delegates card anatomy to `chat-tool-cards`; "Transcript windowing" narrows "live streaming MUST NOT disturb it" to readers who have scrolled away; "Sessions organized per agent" switches to compact title-only rows, the inline running spinner with pulsing title, and the capped list with an expander; "Chat right sidebar" adds the context breakdown popover behind the composer-rail ring; new requirements cover the copy-message action, code-block presentation, and streaming-safe markdown.
- `agent-runtime`: "Streamed turns" requires tool activity terminal records to carry duration; "Context-window accounting" additionally exposes the last turn's input and output tokens.

## Impact

- **Frontend** (`frontend/src/features/chat/`): `Transcript.tsx` (follow/jump/disclosure-pause), `ToolCallCard.tsx` replaced by a `tools/` module (registry, `toolDisplay.ts` sentences, renderers, shared `DiffView`/`CodeBlock`/`FileChip`/`Lightbox` primitives), `ContextMeter.tsx`/`ContextRing.tsx` plus a new `contextBreakdown.ts` and popover, `SessionRail.tsx` row grammar, `AssistantMessage.tsx` copy action and shared `CodeBlock`. Light and dark themes must be specified for every new surface.
- **Backend**: `sessions.ToolActivity` gains `DurationMs` (JSON payload field, no migration) stamped by the runner on terminal records; `agents.ContextUsage` gains `LastInput`/`LastOutput` (non-breaking); a new bound screenshot read beside the existing attachment read, jailed to `~/.buddies/browser/screenshots/` with a strict file-name shape. Bindings regenerate.
- **Dependencies**: adds `diff` (jsdiff, zero-dep, MIT) for line-level diffs; the Shiki/Streamdown spike may add one highlighting/rendering dependency. `@assistant-ui/*` unchanged.
- **Specs**: `chat-tool-cards` created; `chat-view` and `agent-runtime` deltas as above. `chat-panels` is unchanged — its run card keeps owning the live plan lane, and the in-card todo body is a snapshot that defers to it.
