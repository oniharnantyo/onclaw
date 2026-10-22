# Design

## Context

See proposal.md — Why. The relevant current state:

- **One store, one reducer.** `frontend/src/features/chat/store.ts` (zustand) owns sessions, the `Entry` union (`user | assistant | reasoning | tool | task | compaction | error`, plus the approval seam and `variant_of` markers in `entries.ts`), turn lifecycle (`busy`, streaming flags, `turnID` stamps), the send queue, context usage, and the right-panel tabs. Live events and the durable replay run through the same reducer, so a reloaded transcript equals the live one. Attachments live in `attachments.ts`. The Go side (`internal/sessions`, `internal/agents`) and the transport are untouched by this change.
- **Hand-rolled view layer.** `Transcript.tsx` (entry switch, `foldRuns`, `withDayDividers`, prepend anchor), `useFollowBottom`, `Minimap`, `RunCard` (anchored to the chat column, hence over the composer), `ToolCallCard` + `tools/registry.ts` body renderers, `DiffView` (jsdiff rows), `CodeBlock` + lazy `shikiHighlight`, `markdownStream.stabilizeMarkdown`, `AssistantMessage` (react-markdown, no `remark-gfm`), `Composer` + pickers/menus, `ContextMeter`/`contextBreakdown.ts`, `ApprovalCard`, `TaskCallCard`, `QueueStack`.
- **assistant-ui is declared, not used.** `@assistant-ui/react ~0.15.16` and `@assistant-ui/react-markdown` have no imports (`enhance-chat-ux` design D15). The registry elements are vendored via `npx assistant-ui@latest add <name>` (shadcn add against `r.assistant-ui.com`), need `components.json` and a tsconfig `@/` alias (Vite already aliases `@` → `src`; `tsconfig.json` has no `paths`), and share a `surfaces.tsx` token file. `markdown-text`, `thread`, `composer`, `message-*`, `attachment` are runtime-bound (no props-driven form); `tool-call`, `terminal-block`, `code-diff`, `web-search`, `tool-timeline`, indicators and `day-separator` also work standalone.
- **Registry names (verified against `r.assistant-ui.com/registry.json`, 172 items).** The short names used throughout this document map onto registry items as follows. Runtime-bound compositions keep their short name: `thread`, `markdown-text`, `reasoning`, `shiki-highlighter`, `mermaid-diagram`, `conversation-map`, `attachment`, `tool-fallback`, `tool-group`, `task-card`, `composer-trigger-popover`, `tooltip-icon-button`, `use-copy-to-clipboard`. Presentational elements carry an `elements-` prefix: `elements-day-separator`, `elements-typing-indicator`, `elements-thinking-indicator`, `elements-message-actions`, `elements-message-branches`, `elements-scroll-anchor`, `elements-tool-call`, `elements-tool-timeline`, `elements-terminal-block`, `elements-code-diff`, `elements-web-search`, `elements-tool-error`, `elements-link-preview`, `elements-agent-plan`, `elements-subagent-list`, `elements-approval-card`, `elements-composer`, `elements-message-queue`, `elements-message-attachment`, `elements-model-picker`, `elements-reasoning-effort`, `elements-context-usage`, `elements-context-breakdown` (transitively `elements-surfaces`, `elements-collapsible-root`, `elements-range`, `elements-receipt-focus`, `elements-href`, `elements-task`). Names this document borrowed from the docs that do **not** exist as items — `composer-slash-commands`, `composer-mentions`, `composer-context`, `composer-attachments`, `composer-model-picker`, `approval-card`, `thread-list-sidebar` — resolve to: one `composer-trigger-popover` configured per trigger (`/`, `@`, `$`); `elements-context-usage` + `elements-context-breakdown`; `attachment` + `elements-message-attachment`; `elements-model-picker` + `elements-reasoning-effort`; `elements-approval-card`; `threadlist-sidebar` (pulls shadcn `sidebar`; evaluated only, per D11).
- **External-store runtime capabilities** (verified against the docs): `useExternalStoreRuntime({ messages | messageRepository, isRunning, onNew, onEdit, onReload, onCancel, setMessages, convertMessage, hasEarlier, onLoadEarlier, queue, onAddToolResult, onResumeToolCall, onRespondToToolApproval, adapters: { attachments, threadList, … } })`; branches via `ExportedMessageRepository.fromBranchableArray` (`id` + `parentId`, `headId`); custom parts via `data-*` prefixed types converted to `DataMessagePart`; tool results matched by `toolCallId`.
- **Payload constraints.** eino's `execute` returns one combined text stream ending in known markers (`[Command failed with exit code N]`, `[Command timed out and was stopped]`, `[Output was truncated due to size limits]`, `[Command executed successfully with no output]`); `duration_ms` rides each terminal tool record (`tools/parse.ts#callDurationMs`). Tool argument/result shapes are already parsed in `tools/parse.ts` and verbalised in `tools/toolDisplay.ts`.
- **Platform constraints.** onclaw-v2 token contract in both themes (`data-theme` on `<html>`, `useThemeStore`), daisyUI grep gate at zero, `vp check src` anti-slop gate, Tailwind v4, Radix rather than Base UI, Wails webview with a 25px native drag band at the top of the window that the chat header's padding already clears.

## Goals / Non-Goals

**Goals:**

- The store remains the single source of truth; the runtime adapter is pure derivation of reducer output and only mutates through existing store actions.
- Every transcript and composer surface renders through assistant-ui primitives and vendored elements; no hand-rolled equivalent survives beside an element that covers it.
- Every requirement in `chat-view`, `chat-tool-cards`, `chat-panels`, `chat-approvals`, `chat-attachments` keeps passing, scenario for scenario, on the new layer.
- Vendored sources stay re-vendorable (`add -o`); theming happens through one alias layer and `surfaces.tsx`, never per-file edits.
- Heavy dependencies (mermaid, shiki grammars, motion) load lazily; the first paint of an empty chat does not pay for them.

**Non-Goals:**

- Changing the Go event model, persistence, approvals protocol, or any binding (no `task common:generate:bindings`).
- Adopting assistant-ui's own persistence/cloud thread storage or its `LocalRuntime`; sessions stay in SQLite via the existing services.
- Redesigning the shell: the navigation rail, the right preview panel, the sessions column's resize/collapse grammar, and Settings are untouched (the sessions rail is swapped only under the condition in D11).
- Server-side result envelopes or a stderr lane for shell calls (the payload cannot carry them).

## Decisions

### D1. Runtime via `useExternalStoreRuntime` over the zustand store

Mount `AssistantRuntimeProvider` at the chat view root with a runtime built from a memoised adapter over store selectors. The adapter never holds state of its own: `messages`/`messageRepository`, `isRunning`, `hasEarlier`, `queue` are selected from the store; `onNew`, `onCancel`, `onReload`, `onLoadEarlier`, `setMessages` (branch switch), `onRespondToToolApproval`, `adapters.attachments` call the store's existing actions (`send`, `stop`, `regenerate`, `loadEarlier`, `setActiveVariant`, approve/deny, attachment add/remove).

*Why:* keeps the replay-equals-live invariant, the transport, durable approvals and queue semantics exactly as they are; this is the architecture onclaw-v2 shipped. *Alternatives:* migrating state into assistant-ui's `LocalRuntime` (would duplicate the Go-backed session model and break replay); staying runtime-free with standalone elements only (cannot use `markdown-text`, `thread`, `composer`, `message-*` — rejected by scope).

### D2. Entry → message/part mapping

One user message per `user` entry. One assistant message per turn (`turnID`), whose `content` is the turn's entries in order: `reasoning` → `{ type: "reasoning" }`, `assistant` → `{ type: "text" }`, `tool` → `{ type: "tool-call", toolCallId, toolName, args, argsText, result, isError }`, `task` → `{ type: "data-task", id, data }`, `error` → `{ type: "data-error" }` and message `status: { type: "incomplete", reason: "error" }`. A `compaction` entry becomes its own `data-compaction` message so the divider sits between turns. Every message carries `createdAt` from the entry's `at` (feeds `day-separator`). Entries without a `turnID` (records persisted before stamping) group by adjacency, preserving today's `foldRuns` behaviour. Streaming entries map to a message with `status: running` and partial parts; part `id`s are the entry ids so component state survives reorders.

*Why:* assistant-ui's actions (copy, regenerate, branches, tool grouping) are per message, and a turn is the unit those actions mean in buddies. *Alternative:* one message per entry — breaks tool grouping, message actions and branch semantics.

### D3. Variants as a branch tree

Variant turns become siblings: the regenerated turn's assistant message takes the original turn's `parentId` (the user message), `fromBranchableArray` builds the tree, and `headId` is the active variant. `message-branches` renders the `n / total` picker; switching calls `setActiveVariant` through `setMessages` — local, no request, satisfying "Switching variants is offline". `onReload` calls `regenerate(turnID)`, a real backend turn that streams live; a failed regeneration leaves `headId` on the previous variant (store already does this).

*Alternative:* flat `messages` plus a custom picker — forfeits the element and the runtime's branch model.

### D4. Approvals through the native tool-approval flow, with a typed fallback

A pending approval is its tool-call part awaiting approval; `onRespondToToolApproval` maps to the existing approve/deny actions, and `approval-card` renders it. Grouping (D6) treats a pending approval as a hard split so it never folds. If the spike shows the runtime's approval lifecycle cannot represent buddies' durable, reload-surviving approvals (pending approvals are rebuilt from the event log), the part becomes `data-approval` rendered by the same card with the same actions — the spec's scenarios hold either way.

### D5. Markdown, code and diagrams

`markdown-text` renders text parts (GFM is wired inside it, which fixes tables, task lists, strikethrough, autolinks and numbered lists at once); `shiki-highlighter` is registered as its `SyntaxHighlighter` and keeps today's lazy per-language grammar loading and dual-theme CSS variables; `mermaid-diagram` is registered under `componentsByLanguage.mermaid` — the vendored `markdown-text` exposes `componentsByLanguage` as a prop, so this is a wrapper-side registration, not an edit. The registry's mermaid element renders through `beautiful-mermaid` (not the `mermaid` package); it is a lazy `import()` behind the first diagram, renders only once its part has settled (the element's own deferral), shows the fenced source while streaming, falls back to the source plus a quiet failure note when parsing fails, and re-renders when `useThemeStore` flips (mermaid bakes colours into the SVG, unlike Shiki's CSS variables). "Streaming-safe markdown" is verified against the element's streaming parser with the existing `markdownStream` tests ported; if a scenario fails, `stabilizeMarkdown` stays as a pre-pass on the text part — it is byte-identical on complete text, so the final-render-equals-static-render scenario is unaffected.

*Alternative:* keep react-markdown and add `remark-gfm` + a hand-owned `MermaidBlock` — rejected by scope, and `markdown-text` is runtime-bound anyway once D1 lands.

### D6. Tool renderers through a toolkit, grouped by `tool-timeline`

`defineToolkit` + `Tools` in `AuiConfig` registers one `render` per shipped tool name, each a thin wrapper that reuses `tools/parse.ts` and `tools/toolDisplay.ts` (sentences, icons, latency) and feeds an element:

| tool | element | notes |
|---|---|---|
| `execute` | `terminal-block` | D7 |
| `edit_file` | `code-diff` | rows from the existing jsdiff pass; `additions`/`deletions` counted from rows; keeps the all-occurrences marker and path chip in the wrapper |
| `write_file`, `read_file` | `tool-call` + `shiki-highlighter` preview | line count / numbered lines preserved |
| `grep`, `glob`, `ls` | `tool-call` with grouped match / file-list body | highlight of the pattern kept |
| `web_search` | `web-search` | query pill, searching shimmer while `status.running`, "read N sources" computed from results, results landing one by one |
| `web_fetch` | `tool-call` (address + clamped text) | may move to `link-preview` later (open question) |
| `browser` | `tool-call` + `image` / existing lightbox | screenshot bytes still via the bound read |
| `todo_write`, `todo_read` | `agent-plan` snapshot | repeated writes in a turn collapse per spec |
| MCP / unknown / unparsable | `tool-fallback` | no sentence; pretty-printed when structured |

Every path shown keeps opening the right panel through `toolPaths.ts`. Four or more consecutive tool/task parts in one message fold behind `tool-timeline` (threshold unchanged): steps carry the verb/icon/chip grammar from `toolDisplay.ts`, the last visible step shimmers while running, trailing stats come from completed edit/write results, a failed step tints and reveals the fold, a pending approval splits it, and each row opens its full card. Reduced motion is honoured through the existing `useReducedMotion` wrapper where an element lacks its own guard.

### D7. Terminal block bounded by eino's output

The wrapper parses the trailing markers into `exitCode`, `timedOut`, `truncated`; `done` follows the call status; `durationMs` is `duration_ms`; `cwd` is the agent's workspace label; `lines` is the output split by line with ANSI escapes stripped defensively (the jailed shell has no TTY). No stderr lane is offered. The element's line-by-line reveal is a timer-driven `visibleCount` replay on arrival — results land whole, so this is presentation, not streaming. The failure note line itself is not duplicated in the body once it has been parsed into the status chip.

### D8. Run surfaces float top-right of the thread viewport

`agent-plan` (plan lane from `latestPlanItems`) and `subagent-list` (tasks lane from `task` entries) compose one floating card positioned inside the thread viewport's relative wrapper — top-right, clear of the `conversation-map` rail, therefore never over the composer — and render only when a lane has content. The viewport is the anchor so the card can never reach the composer, which is what today's chat-column anchoring broke.

### D9. Follow, jump-to-latest and windowing

The `thread` viewport's auto-scroll plus `scroll-anchor` replace `useFollowBottom`: following while at the bottom, pause on the reader's own upward scroll, jump pill resumes, own send returns to the bottom, disclosure toggles hold the viewport. Windowing uses `hasEarlier`/`onLoadEarlier`; the prepend height-delta anchor is re-implemented as a layout effect on the viewport if the element does not preserve the reader's position on prepend (verified in phase 2 against the "Load earlier keeps reading position" scenario).

### D10. Composer on assistant-ui elements

`composer` with `composer-trigger-popover` driving the `/` commands, `@` mentions and `$` skills menus from today's sources (`promptDocs`, skills, slash catalogue); `model-selector`/`composer-model-picker` for agent, model and effort — the agent choice remains a conversation-context switch through the existing store action, not a message attribute; `composer-context` renders the ring and the breakdown popover from `contextBreakdown.ts`; `attachment`/`composer-attachments` bind to `attachments.ts` through `adapters.attachments`; `message-queue` renders `queue`; the send/stop control follows `isRunning` with `onCancel`. Enter sends, Shift+Enter inserts a newline, matching the spec and the element defaults.

### D11. Sessions rail: keep unless the element covers the spec

`SessionRail` lives outside the thread and is not one of the surfaces the user listed. The spike evaluates `thread-list-sidebar` with a `threadList` adapter against the five spec behaviours (per-agent grouping, title search, delete-with-confirmation with the in-flight refusal, running indicator, drag-resize with the four-most-recent cap); it replaces `SessionRail` only if all five can be expressed. Default: keep `SessionRail`, which already passes.

### D12. Theming through an alias layer and `surfaces.tsx`

`frontend/src/styles/theme.css` already carries a shadcn-named `@theme inline` seam (from `markdown-card-elements`) mapping `--color-background`, `--color-foreground`, `--color-muted-foreground`, `--color-border`, `--color-card`/`--color-popover`, `--color-primary`, `--color-destructive`, radius and ring onto the raw contract tokens (`--bg`, `--fg`, `--muted`, `--border`, `--surface`, `--accent`, `--danger`), so `bg-background`, `text-muted-foreground` and friends resolve under both `data-theme` values. This change audits that seam against every token name the vendored sources actually use and adds only the missing names. The elements' shared `surfaces.tsx` (paper, ink, mono, field, collapse panel) is retargeted once to buddies surfaces; it is the elements' sanctioned retheming seam. No other vendored file is edited.

*Alternatives:* editing each vendored component (breaks re-vendoring, drifts on every update); running two token systems (violates the design contract and the hallmark audit).

### D13. Vendoring and lint policy

Elements are generated by the CLI into `frontend/src/components/assistant-ui/` and treated as vendored: buddies-specific behaviour lives in wrappers under `features/chat/`, never in the generated files (except `surfaces.tsx`). The registry ships two tiers and both are vendored verbatim: presentational `elements/*.tsx` (registry items prefixed `elements-`, standalone, props-driven) and runtime-bound compositions (`*.aui.tsx`, `markdown-text.tsx`) that read the assistant-ui context; buddies composes its own thread, message and composer in `features/chat/` from the primitives plus those pieces rather than editing the registry's `thread.aui.tsx`. Registration points the compositions expose as props (`markdown-text`'s `componentsByLanguage` and its `components` override) are used from the wrappers. The shadcn primitives the registry depends on (`button`, `tooltip`, `dialog`, `avatar`, `skeleton`, `textarea`, `input`) are vendored into `frontend/src/components/assistant-ui/ui/` — a separate directory from the onclaw kit in `components/ui/`, because the kit's PascalCase files (`Button.tsx`) would collide with the registry's lowercase names on a case-insensitive filesystem and be overwritten. The anti-slop lint (`vp check`) is scoped to exclude the vendored directory so re-vendoring never fights the gate; wrappers remain fully linted. `frontend/src/components/assistant-ui/VENDORED.md` records the exact `add` commands, every dependency installed with its version, and the two spike verdicts (D4, D11) so re-vendoring is a repeatable task: `add -o`, re-apply `surfaces.tsx`, run the suite.

### D14. Scaffolding and alias

`components.json` (Radix style, Tailwind v4 CSS entry `src/styles.css`, aliases `components` → `@/components`, `ui` → `@/components/assistant-ui/ui`, `hooks` → `@/components/assistant-ui/hooks`, `utils` → `@/lib/utils` re-exporting `cn`); `tsconfig.json` `paths: { "@/*": ["./src/*"] }` so tsc resolves what Vite already does; and `vitest.config.ts` gains the same `resolve.alias` because it is a separate plain-vitest config that does not read `vite.config.ts` or tsconfig paths.

### D15. Dependency versions

The registry currently targets `@assistant-ui/react@^0.15.30` and `@assistant-ui/react-markdown@^0.14.23`; the installed `0.15.28` is bumped to match and both are pinned exact. The manifests for the element set declare `remark-gfm`, `react-shiki`, `tw-shimmer`, `class-variance-authority`, `radix-ui`, `beautiful-mermaid` (the mermaid renderer), and `@base-ui/react` (used only inside the vendored `conversation-map` element — buddies code keeps to Radix); each is added and pinned exact. `react-markdown` stops being imported directly and is removed once nothing outside the vendored directory needs it; `diff` stays as the line-diff producer for `code-diff`. This supersedes `enhance-chat-ux` design D15 ("patterns borrowed, runtime not adopted").

### D16. Tests

The adapter gets the new core unit suite (entry→message mapping, branch tree, data parts, approval mapping, windowing flags, queue). Component tests are ported scenario for scenario to the element wrappers; tests of retired internals (`useFollowBottom`, `Transcript.foldRuns`, `Minimap`, `RunCard`, `markdownStream` if retired) are deleted with their subjects. The old components and tests stay in the tree until the surface that replaces them is green (phase-gated), so the suite never goes red wholesale. The change closes with the server-mode headless live smoke against a real model and a hallmark audit in both themes.

## Risks / Trade-offs

- [0.x API churn in `@assistant-ui/react`] → pin exact versions; isolate every runtime touchpoint in the adapter and the wrappers; re-vendor deliberately (D13).
- [Registry dependency manifests unknown — `motion`, Radix, others] → phase-0 spike on a scratch branch records them before any transcript code moves; bundle budget checked against the current `vp build` output; heavy modules stay lazy.
- [`markdown-text`'s streaming parser may not satisfy the streaming-safe scenarios] → ported `markdownStream` tests decide; `stabilizeMarkdown` pre-pass remains available and is a no-op on complete text (D5).
- [Load-earlier prepend may not preserve the reader's position under the element viewport] → layout-effect height-delta anchor re-implemented on the viewport if the scenario fails (D9).
- [Approval lifecycle mismatch with durable approvals] → `data-approval` fallback keeps the same card and actions (D4).
- [shadcn look creeping into the onclaw-parity design] → alias layer + `surfaces.tsx` retarget; hallmark audit is a closing gate; reduced-motion verified for every animated element.
- [~40 chat test files rewritten] → phase-gated migration keeps the old components until each replacement is green (D16).
- [Mermaid payload and SVG injection] → lazy import, `securityLevel: "strict"`, SVG only, failure falls back to source text.
- [Wails native drag band vs. the `thread` header] → keep the header's `pt-6` grammar and the textless top strip; verify `scroll-anchor` under the band in the live smoke.
- [Composer rebuild redoes recently shipped picker work] → accepted scope call by the user; the picker *behaviour* and store actions are reused, only the surface changes.

## Migration Plan

Phases map one-to-one onto task groups and are each a reversible commit; the old view layer stays mounted until phase 4 lands.

0. **Spike** — `components.json`, tsconfig `paths`, vitest alias, seam audit, `npx assistant-ui add` for the full element set; record every installed dependency and the runtime version in `VENDORED.md`; `npm run build` and `vp check` green; decide D4 and D11 from what the registry actually ships. The repository has no commits yet, so the spike runs in-tree rather than on a scratch branch; rollback is deleting `components/assistant-ui/`, `components.json`, `src/lib/utils.ts` and reverting the three config files and `package.json`.
1. **Adapter** — `useExternalStoreRuntime` adapter + `AssistantRuntimeProvider` mounted beside the existing transcript (dev-only route or flag); adapter unit suite green against recorded event fixtures.
2. **Transcript** — `thread` + `markdown-text`/`shiki-highlighter`/`mermaid-diagram`, `reasoning`, indicators, `day-separator`, `message-actions`, `message-branches`, `scroll-anchor`, `conversation-map`; chat-view scenarios ported.
3. **Tools and run surfaces** — toolkit mapping, `terminal-block`, `code-diff`, `web-search`, `tool-timeline`, `tool-fallback`, approvals, top-right `agent-plan` + `subagent-list`; chat-tool-cards and chat-panels scenarios ported.
4. **Composer** — composer elements, pickers, context ring, attachments, queue, stop; composer scenarios ported; old components and tests deleted; lint scopes finalised.
5. **Close** — pin versions, hallmark audit both themes, server-mode live smoke against a real model, `task build` with bindings regenerated (even though Go is untouched, the frontend dist must be rebuilt and verified served).

Rollback at any phase is reverting that phase's commits; before phase 4 the shipped UI is still the current one.

## Open Questions

- ~~Exact dependency set the registry installs~~ — resolved from the registry index before implementation (see D15 and the registry-names bullet in Context); the spike confirms the installed versions.
- Whether `web_fetch` reads better as `link-preview` than `tool-call` (cosmetic; decided in phase 3; default `tool-call`).
- Where `day-separator`'s hover-revealed per-message time sits relative to `message-actions` (cosmetic; decided in phase 2).
