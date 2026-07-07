# Proposal: Refactor Chat UI to Assistant-UI Style (Primitives + Interaction Components)

## Intent
Refactor the web chat interface to faithfully match **assistant-ui's** composable architecture and interaction richness — **without installing the library**. The work proceeds on two axes: (1) **deepen** the existing primitive-shaped components into true headless primitives (compound components with render-prop slots + context hooks over the existing `ChatProvider` runtime), and (2) **broaden** the surface with assistant-ui's interaction components — `ActionBar`, `ToolGroup`, `ChainOfThought`, derived `Sources`, and a custom `Scrollbar`. It also upgrades the composer to multi-line (`Enter` submits, `Shift+Enter` newline) with **clipboard paste-to-attach** for images and files. No new runtime dependencies are introduced (no Radix, no mermaid); the only backend touch is a minimal, native-field-only edit to the `/api/chat` request handler to carry pasted media.

## Problem
The current UI adopted assistant-ui's *names* but not its *model*. It is a minimal skeleton:
- `Chat.tsx` / `Thread.tsx` / `ThreadList.tsx` / `Composer.tsx` are simple styled wrappers, not compound headless primitives — no render-prop slots, no context hooks for nested parts, so deeply nested affordances (action bars, send buttons) must prop-drill.
- The interaction layer is absent: no `ActionBar` (copy/regenerate), no `ToolGroup` (consecutive tool calls render flat and identically), no `ChainOfThought` (reasoning + its tool calls are not grouped), no `Sources`, no custom scrollbar.
- `Composer` uses a single-line `<input>`, so multi-line input is impossible and Shift+Enter does nothing; there is no paste-to-attach.
- `MessageBubble.tsx` holds a monolithic `renderContentBlocks` dispatch with no grouping pre-pass.

The agent wire (eino's `schema.AgenticMessage`) already carries every block type these features need.

## Proposed Solution
**Deepen — three-tier headless architecture:**
- **Runtime (keep):** `ChatProvider` (the existing reducer FSM with the single unified message array + `isStreaming` flag) remains the single source of truth.
- **Primitives (new, headless/unstyled):** `Thread`, `ThreadList`, `Composer`, `Message` as compound components with render-prop sub-parts (`Thread.Messages`, `ThreadList.Items`, `Composer.Input`, `Message.Parts`, …) consumed via selector hooks (`useThread`, `useComposer`, `useMessage`, `useThreadList`). No Radix — plain React context + slots.
- **Page (styled):** `Chat.tsx` becomes pure composition with design-system styling; all visuals live in `index.css`.

**Broaden — interaction components (frontend-only):**
- `Message.ActionBar` — copy + regenerate, auto-hides until hover, copy-state feedback, disabled while streaming.
- `ToolGroup` — collapses runs of ≥2 consecutive tool calls into one "N tool calls" accordion.
- `ChainOfThought` — groups a reasoning block immediately followed by tool calls into one "Thought process" accordion; lone reasoning stays a bare `Reasoning` block.
- `Sources` — **derived**: URL/title chips parsed from search/browser/fetch tool results (no typed citation contract).
- `Scrollbar` — CSS-only themed custom scrollbar on the viewport (no `scroll-area` dependency).

**Composer upgrades:**
- `Composer.Input` becomes an auto-growing `<textarea>`: `Enter` submits, `Shift+Enter` inserts a newline.
- `Composer.TriggerPopover` generalizes the `/` skill picker, anchored to the composer's bottom-left (cursor tracking in a textarea is brittle).
- **Paste-to-attach:** `Composer` traps `paste`, captures image/file data, shows a removable preview chip, and transmits it as native eino `UserInputImage`/`UserInputFile` content blocks via the `/api/chat` request body.

**Backend (minimal, native-field-only):**
- Extend the `/api/chat` request DTO (`internal/api/service/types.go`) with an optional `content_blocks` field.
- Extend the handler (`internal/api/handler/chat.go`) to build the user message with `UserInputImage`/`UserInputFile` when present. Text prompt remains required.

## Constraints & Dependencies
- **Lean Dependencies:** No new runtime deps. No Radix (primitives are plain React; scrollbar is CSS-only), no `mermaid.js`, no `streamdown`. Markdown stays `react-markdown` + `remark-gfm` + `rehype-highlight` (the `prism-light` substitute, already in place).
- **Native eino fields only — no fork, no new type:** The `/api/chat` request-side edit uses eino's existing `UserInputImage`/`UserInputFile` fields. There is **no** eino fork, **no** `AgenticMessage` type change, **no** `Extra`-map convention, and **no** new tool.
- **SSE response path unchanged in this change:** Additive SSE response fields (timing, usage, model id) belong to the follow-up change `enhance-chat-ui-metadata`.
- **Design system:** All visuals follow `web/design-system/onclaw/MASTER.md` (dark OLED, Inter, Phosphor icons, 150–300ms transitions, visible focus, `prefers-reduced-motion`).

## Out of Scope (Deferred)
- **→ Follow-up change `enhance-chat-ui-metadata`:** `Message Timing`, `Context Display`, `Model Selector`, `Follow-Up Suggestions` + the additive SSE response fields they need.
- **→ Future changes (blocked/deferred):** `BranchPicker` (needs a branching backend); **typed** `Sources` citation contract; full **Attachments UI** (picker button, drag-drop zone, multi-attachment management — this change does clipboard paste only); `@` workspace file picker; `Mermaid` diagrams; `Voice`.
