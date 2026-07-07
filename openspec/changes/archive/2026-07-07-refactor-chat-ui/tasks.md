# Tasks: Refactor Chat UI (assistant-ui style — primitives + interaction components)

> Refinement of the change. The architecture is being rebuilt on a headless-primitive model with the interaction layer added, so tasks are re-opened against the refined scope. Re-implement via `/opsx:apply`.

## Runtime & hooks (Tier 1)
- [x] Keep `ChatProvider` as the runtime (reducer FSM, single unified message array, `isStreaming` on last message, SSE loop) — no re-architecture.
- [x] Add selector hooks `useThread`, `useComposer`, `useMessage`, `useThreadList` over `ChatProvider` so nested parts read state/actions without prop-drilling.

## Headless primitives (Tier 2) — `web/src/components/primitives/`
- [x] Build `Thread` compound primitive: `Thread.Root`, `Thread.Viewport` (scroll + auto-scroll), `Thread.Empty`, `Thread.Messages` (render-prop `(msg, idx) => …`), `Thread.ScrollToBottom`.
- [x] Build `ThreadList` compound primitive: `ThreadList.Root`, `ThreadList.Items` (render-prop `(conv, active) => …`), `ThreadList.New`.
- [x] Build `Composer` compound primitive: `Composer.Root`, `Composer.Input`, `Composer.Send`, `Composer.Cancel`, `Composer.TriggerPopover`, `Composer.PastePreview`.
- [x] Build `Message` compound primitive: `Message.Root` (hover scope), `Message.IfUser`/`Message.IfAssistant`, `Message.Parts` (render-prop over grouped content blocks), `Message.ActionBar`.

## Composer upgrades
- [x] Make `Composer.Input` an auto-growing `<textarea>`: `Enter` submits, `Shift+Enter` inserts a newline (no `preventDefault`); cap height (~160px) then internal scroll; reset height after submit; disable + placeholder while streaming.
- [x] Re-anchor `Composer.TriggerPopover` (the `/` skill picker) to the composer's bottom-left; generalize trigger logic to any trigger char; keep open-on-`/`, filter-as-you-type, insert-on-select.
- [x] Add clipboard paste-to-attach: trap `paste`, capture image/file data, render a removable `Composer.PastePreview` chip; ignore unsupported pastes without error.

## Message rendering & grouping — `web/src/components/chat/`
- [x] Implement `groupBlocks(content_blocks)` pre-pass: `ChainOfThought` (reasoning + ≥1 following tool), `ToolGroup` (run of ≥2 tools), single-block otherwise; memoize per `content_blocks` reference.
- [x] Wire `Message.Parts` to map `groupBlocks` output → group renderers / per-block dispatch; keep `pickToolRenderer(name)` (skill before generic) and a fallback `Unknown` renderer.
- [x] Re-home existing renderers from `MessageBubble.tsx` into `chat/`: `Markdown` (+ ```` ```diff ```` → `Diff` sub-renderer), `Reasoning`, `SkillActivated`, `MCPCalled`, `ToolCall`, `ToolResult`, `Image` (`UserInputImage` + `AssistantGenImage` → `<img>` preview), `File` (chip with file name).
- [x] Implement `ToolGroup` — collapsible "N tool calls" accordion; per-block dispatch applies inside.
- [x] Implement `ChainOfThought` — collapsible "Thought process" accordion grouping reasoning + its tool calls.
- [x] Implement derived `Sources` — scan search/browser/fetch `FunctionToolResult` for URL/title, dedupe, render favicon + title + external-link chips after `Message.Parts`.
- [x] Remove the monolithic `MessageBubble.tsx` once renderers are re-homed.

## Interaction
- [x] Implement `Message.ActionBar` — Copy (concatenate `assistant_gen_text`, clipboard write, transient "copied" state) + Regenerate (`runChat(lastUserPrompt)`, disabled while streaming); auto-hide until hover/focus.

## Styling
- [x] Add CSS-only custom scrollbar on `Thread.Viewport` (`::-webkit-scrollbar` + `scrollbar-width`), themed to design-system tokens, hover-fade, `prefers-reduced-motion`-safe.
- [x] Update `index.css` for the primitive styling (headless classNames/`data-*`), action bar, tool group, chain-of-thought, sources, paste preview — following `web/design-system/onclaw/MASTER.md`.

## Page composition (Tier 3)
- [x] Rewrite `Chat.tsx` as pure styled composition of the primitives (`Thread`, `ThreadList`, `Thread.Messages` → `Message`, `Composer`); remove direct state plumbing now served by hooks.
- [x] Confirm `App.tsx` mounts `<Thread>` wrapping `<Chat>` (no standalone Conversations tab; `Conversations.tsx` must not exist).

## Backend — paste-to-attach (minimal, native eino fields)
- [x] Extend the `/api/chat` request DTO (`internal/api/service/types.go`) with an optional `content_blocks` field for `UserInputImage`/`UserInputFile`; keep `Prompt` required.
- [x] Extend the handler (`internal/api/handler/chat.go`) to build the user message with `UserInputImage`/`UserInputFile` when present (native eino fields — no fork, no new type, no `Extra`-map).

## Testing (black-box where possible; ≥70% per package)
- [x] Unit-test `groupBlocks` and `pickToolRenderer` (ChainOfThought vs ToolGroup vs single; skill-before-generic; fallback) — private-algorithm exemption.
- [x] Component/integration tests: ActionBar (copy/regenerate, disabled while streaming), Composer (Enter submits / Shift+Enter newline, paste capture + preview removal, popover), `Message.Parts` grouping.
- [x] Backend black-box HTTP tests: image request, file request, prompt-only regression, empty-prompt 400.
- [x] Map each spec scenario to a test; verify `make test` and `tsc --noEmit` pass.

## Visual refinement (structure on onclaw tokens — ChatGPT-style)
- [x] Flatten messages: remove avatar/role-label rows; assistant full-width bubble-less, user right-aligned muted bubble (alignment conveys role); 8px radius + 16px/12px spacing on chat surfaces.
- [x] Code blocks: own surface + per-block copy control + language label (keep `rehype-highlight`).
- [x] Composer restyle: rounded 8px rect; `+` file-picker (left, native file dialog → reuse paste send path, no backend change); agent dropdown (restyle existing select); brand-green send arrow (right).
- [x] Sidebar restyle: "New chat" on top; flat history list; active item highlighted with brand-green.
- [x] Update `index.css` for the visual treatment following `web/design-system/onclaw/MASTER.md` (tokens unchanged).
- [x] Add/update tests for the new visual scenarios (flat messages, code copy, file-picker attachment, sidebar new-chat/active highlight); verify `tsc --noEmit` and `make test` pass.

## Deferred (separate changes)
- Follow-up `enhance-chat-ui-metadata`: Message Timing, Context Display, Model Selector, Follow-Up Suggestions + additive SSE response fields.
- Future: BranchPicker (branching backend); typed Sources citation contract; full Attachments UI (picker/drag-drop/multi); `@` workspace picker; Mermaid; Voice.
