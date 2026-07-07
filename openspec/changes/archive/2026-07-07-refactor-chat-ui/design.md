## Context

The first pass of this change shipped assistant-ui's *names* (`Thread`, `ThreadList`, `Composer`, `Message`) but not its *model*: the components are styled wrappers rather than headless compound primitives, the interaction layer is missing, and the composer is single-line. This refinement closes that gap — rebuilding on assistant-ui's composable primitive architecture and layering its interaction components on top, **without taking on the library** (no Radix runtime, no mermaid/streamdown, no new deps). `ChatProvider` (the reducer FSM that replaced the original god-component `runChat` and the dual-array split) is retained as the runtime. The wire (eino's `schema.AgenticMessage`) already carries every block type involved.

## Goals / Non-Goals

**Goals:**
- Rebuild the chat on a three-tier headless architecture (runtime → primitives → page) mirroring assistant-ui's compound-component + context-hook shape.
- Add the interaction components that make the chat feel rich: `ActionBar`, `ToolGroup`, `ChainOfThought`, derived `Sources`, custom `Scrollbar`.
- Multi-line composer (`Enter` submits, `Shift+Enter` newline) with clipboard paste-to-attach for images/files.
- Keep dependencies lean and the backend touch minimal and native-field-only.

**Non-Goals:**
- Full compatibility with the official `assistant-ui` API (we mirror the *pattern*, not the exact API surface).
- Branching (`BranchPicker`), typed `Sources` citation contract, full Attachments UI (picker/drag-drop), `@` picker, Mermaid, Voice — all deferred (see proposal).
- Additive SSE response metadata (timing/usage/model) — owned by the follow-up change `enhance-chat-ui-metadata`.

## Decisions

**Decision 1: Three-Tier Architecture — `ChatProvider` Stays the Runtime**
- **Rationale:** `ChatProvider`'s reducer FSM (single unified message array + `isStreaming` on the last message, SSE loop extracted out of render) already solved the god-component problem. Reusing it as the runtime avoids Redux/Zustand and gives nested primitives a single source of truth to select from.
- **Tiers:** (1) Runtime = `ChatProvider` + new selector hooks; (2) Primitives = headless compound components (`web/src/components/primitives/`); (3) Page = `Chat.tsx` styled composition. Visuals live only in `index.css`; primitives emit classNames/`data-*` and no CSS of their own (the assistant-ui headless philosophy).
- **Single unified message array:** ONE message array; the last assistant message carries the `isStreaming` flag. This replaced the original dual-array split which hid persisted history during a stream and destroyed child state (e.g. an open `<Reasoning>` accordion) on every rebuild.
- **Alternatives Considered:** Rewrite the runtime as a Zustand store (Rejected: the reducer FSM is correct and already tested); keep styled-wrapper "primitives" (Rejected: no path to nested affordances without prop-drilling).

**Decision 2: Headless Primitives Mirror assistant-ui's Shape — Without Radix**
- **Rationale:** Compound components with render-prop slots (`Thread.Messages`, `Message.Parts`, `ThreadList.Items`) plus selector hooks (`useThread`, `useComposer`, `useMessage`, `useThreadList`) are what let a deeply nested `Message.ActionBar` or `Composer.Send` reach state cleanly. assistant-ui builds this on Radix; we implement the same shape in plain React (context + slots), preserving the "no new runtime dep" promise.
- **Primitive surface:** `Thread.{Root,Viewport,Empty,Messages,ScrollToBottom}`; `ThreadList.{Root,Items,New}`; `Composer.{Root,Input,Send,Cancel,TriggerPopover,PastePreview}`; `Message.{Root,IfUser,IfAssistant,Parts,ActionBar}`.

**Decision 3: `groupBlocks` Pre-Pass Replaces the Monolithic Dispatcher**
- **Rationale:** `ToolGroup` and `ChainOfThought` are not independent components — they are *grouping strategies* over a flat `content_blocks` stream. Making grouping a pipeline stage keeps per-block dispatch (`pickToolRenderer`) flat and reusable: a `skill` call inside a group still renders as `SkillActivated`.
- **Algorithm:** scan `content_blocks` left-to-right —
  - a `reasoning` block immediately followed by ≥1 tool-call/result block → `ChainOfThought { reasoning, toolBlocks[] }`;
  - else a maximal run of ≥2 consecutive tool-call/result blocks (not part of a CoT) → `ToolGroup { toolBlocks[] }`;
  - a lone tool block → `pickToolRenderer(name)` (skill → `SkillActivated`; mcp → `MCPCalled`; else generic `ToolCall`);
  - text/image/file/lone-reasoning → existing single renderers.
  - `Sources` is computed separately from the message's tool results and appended after `Message.Parts`.
- **Alternatives Considered:** growing `if/else` chain in the dispatcher (Rejected: does not scale to grouping); per-renderer self-grouping (Rejected: O(n²) adjacency checks, tangled).

**Decision 4: Multi-line Composer — Textarea, Enter Submits, Shift+Enter Newlines**
- **Rationale:** Multi-line input is a baseline chat expectation; the single-line `<input>` blocked it. `Enter`/`Shift+Enter` is the assistant-ui/ChatGPT convention the user specified.
- **Behavior:** auto-growing `<textarea>` (grows to ~`max-height: 160px`, then internal scroll; resets after submit). `Enter` submits (non-empty, not streaming); `Shift+Enter` inserts a newline (default behavior — we simply do not `preventDefault`). Disabled with placeholder while streaming.
- **`/` popover anchoring:** with a textarea, inline cursor tracking is brittle, so `Composer.TriggerPopover` anchors to the composer's **bottom-left** (the pre-approved mitigation from the risk note below), not at the cursor. Trigger logic generalizes to any trigger char (`/` today).

**Decision 5: Paste-to-Attach — Frontend Capture + Native eino Field Send**
- **Rationale:** Clipboard capture + preview is frontend-only; but the agent can only see pasted media if the `/api/chat` request carries it as a content block. `UserInputImage`/`UserInputFile` are **native eino fields already on the wire** (eino's `schema` defines them), so sending them is *not* a fork, a new message type, or an `Extra`-map — it is a minimal request-handler edit using existing fields. This expands this change's stance from "frontend-only" to "frontend + minimal `/api/chat` request-side edit"; the SSE *response* path is still untouched here.
- **Scope boundary:** clipboard **paste only** (images robust, files best-effort per browser support). The fuller Attachments UI (picker button, drag-drop zone, multi-attachment management) stays deferred. Text prompt remains required (text + optional media, never media-alone) to keep the handler simple.
- **Alternatives Considered:** defer entirely to the follow-up (Rejected: user wants it now, and the native-field path is clean); embed image as a data URL in the prompt string (Rejected: the LLM would not interpret it as an image).

**Decision 6: Derived `Sources` — No Typed Contract**
- **Rationale:** A typed citation contract is deferred. For now, `Sources` is **derived**: scan the message's `function_tool_result` blocks from search/browser/fetch tools, JSON-extract `url`/`title`, dedupe, and render chips (favicon via `google.com/s2/favicons`, title, external-link icon). This is exactly the "derived from tool results" approach flagged originally; the typed upgrade is a future change.

**Decision 7: CSS-Only Scrollbar — No `scroll-area` Dependency**
- **Rationale:** assistant-ui's custom scrollbar uses Radix `scroll-area`. onclaw avoids Radix, so the scrollbar is pure CSS on `Thread.Viewport` (`::-webkit-scrollbar` + `scrollbar-width`), themed to design-system tokens, hover-fade, `prefers-reduced-motion`-safe.

**Decision 8: Regenerate = Re-run (Honest Divergence from Branching)**
- **Rationale:** assistant-ui's regenerate creates a *branch* (flipped via `BranchPicker`). onclaw has no branching backend, so `ActionBar` regenerate **re-runs the last user prompt** (`runChat(lastUserPrompt)`, disabled while streaming) rather than creating an alternate branch. `BranchPicker` stays deferred. This divergence is recorded explicitly rather than silently approximated.

**Carried-over decisions (still valid):** lean markdown via `react-markdown` + `remark-gfm` + `rehype-highlight` (the `prism-light` substitute, already in place); name-aware `pickToolRenderer(name)` dispatch (skill before generic); fallback renderer so unrecognized blocks are not silently dropped; `reasoning` carried on the frontend `ContentBlock` type; ```` ```diff ```` fences render via a diff sub-renderer.

## Wire → Renderer Table (this change)

| Wire signal | Renderer | Notes |
|---|---|---|
| `reasoning` + ≥1 following tool block | `ChainOfThought` | grouped think→act accordion |
| run of ≥2 `FunctionToolCall`/`Result` (+ `MCPToolCall`/`Result`) | `ToolGroup` | collapsed "N tool calls"; per-block dispatch still applies inside |
| `FunctionToolCall{name:"skill"}` (+ result) | `SkillActivated` | name headline, collapsed body |
| `MCPToolCall` / `MCPToolResult` | `MCPCalled` | distinct from generic |
| `FunctionToolCall` / `FunctionToolResult` (other, lone) | `ToolCall` / `ToolResult` | baseline |
| `Reasoning` (lone) | `Reasoning` | collapsible CoT accordion |
| `AssistantGenText` | `Markdown` | ```` ```diff ```` → diff sub-renderer; `>` → blockquote |
| `UserInputImage` / `AssistantGenImage` | `Image` | inline `<img>` preview |
| `UserInputFile` | `File` | chip with file name |
| search/browser/fetch `FunctionToolResult` | `Sources` | **derived**; typed contract deferred |
| (assistant message hover) | `ActionBar` | copy + regenerate |
| unrecognized block | `Unknown` | `[<type> block]` fallback |

## Risks / Trade-offs

- **[Risk] Complex Composer Implementation** → Tracking cursor position in a standard textarea to render a floating `/` popover is notoriously tricky across browsers.
  - **Mitigation**: `Composer.TriggerPopover` anchors to the composer's bottom-left (Decision 4), not at the cursor — robust and simple.
- **[Risk] Re-render thrash from per-token streaming** → Today each `message` event rebuilds the assistant block array (`activeBlocks = [...activeBlocks, ...newBlocks]`) and re-renders the list; centralizing this in a provider amplifies the blast radius.
  - **Mitigation (deferred)**: The originally planned in-place mutation + rAF flush was not implemented. The reducer instead spreads a new array per `STREAM_MESSAGE` action (`[...state.messages.slice(0, -1), { ...lastMsg, ... }]`). This was considered acceptable because the SSE stream delivers whole `content_blocks[]` arrays (not individual tokens), so array-spread cost is ~1 render per message chunk rather than per-token. On the target hardware (~2 GB ARM), the cost of a single extra array spread per chunk is negligible compared to the DOM work of rendering the blocks themselves. If per-token streaming is introduced later, the in-place + rAF approach can be adopted at that point. `groupBlocks` is additionally memoized per `content_blocks` reference.
- **[Risk] Paste-file browser variance** → pasting files is inconsistently supported across browsers (images are robust; files less so).
  - **Mitigation**: scope paste as **image-primary, file best-effort**; degrade gracefully (unsupported paste is ignored, not errored).
- **[Risk] Drift between wire JSON and frontend `ContentBlock` type** → the frontend type is hand-maintained.
  - **Mitigation**: extend `ContentBlock` from eino's `agentic_message.go` field list (source of truth); the fallback renderer surfaces unrecognized blocks instead of dropping them.

## Visual Treatment (refinement — adopt example structure on onclaw tokens)

**Decision 9: Adopt the example's STRUCTURE, keep onclaw's TOKENS**
- **Rationale:** A ChatGPT-style reference showed a calmer, more content-forward chat than the bubble + role-label layout. The user chose to adopt its *structure* while keeping onclaw's design-system palette (midnight-blue `#0F172A` + brand-green `#22C55E`) — so there is **no `MASTER.md` override**; the chat page stays visually consistent with the rest of onclaw.
- **Structural changes:** no avatars / no role-label rows (alignment conveys role — assistant full-width and bubble-less, user right-aligned in a muted bubble); code blocks on their own surface with a copy control + language label (per-block, distinct from `ActionBar`'s whole-message copy); composer as a rounded `8px` rect with a `+` file-picker (left), the agent dropdown (restyled existing `<select>` — onclaw uses *agents*, not models), and a brand-green send arrow (right); sidebar with "New chat" on top and a flat history list with a brand-green active highlight.
- **Decision 10: `+` file picker reuses the paste send path** — the picker opens a native file dialog and feeds the existing `ComposerPastePreview` → `content_blocks` → `UserInputImage`/`UserInputFile` path; **no backend change**. Drag-drop and multi-attachment management stay deferred.
- **Blast radius:** presentation layer only (`index.css`, `Message` rendering, the code-block renderer, `Composer`, `ThreadList`). The verified runtime, primitives, `groupBlocks`, `ActionBar`, and paste backend are unchanged.
- **Out of scope (unchanged):** drag-drop, multi-attachment management, BranchPicker, typed Sources, metadata components (Timing/Context/Model/Followups), Mermaid, Voice.
