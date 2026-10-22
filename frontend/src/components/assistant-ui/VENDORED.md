# VENDORED — `src/components/assistant-ui/`

Vendored from the assistant-ui registry (`r.assistant-ui.com`) via the
`assistant-ui` CLI (which delegates to `shadcn add`). Regime per
`adopt-assistant-ui-chat` design D13: these files are **vendored, not owned** —
buddies behaviour lives in wrappers under `features/chat/`, never here. The one
sanctioned buddies edit is `elements/surfaces.tsx` (re-apply it after any
re-vendor; the retarget is marked with a `BUDDIES RETARGET` header comment).

Re-vendoring recipe: run the `add` commands below with `-o`, re-apply the
`surfaces.tsx` retarget, rewrite the shadcn primitives' `from "cn"` imports to
`from "@/lib/utils"` (see Divergences), then run the spike gate at the bottom.

## (a) Exact commands run

```sh
# from frontend/
npx assistant-ui@latest add -y -o \
  thread markdown-text reasoning shiki-highlighter mermaid-diagram \
  conversation-map attachment tool-fallback tool-group task-card \
  composer-trigger-popover tooltip-icon-button use-copy-to-clipboard

npx assistant-ui@latest add -y -o \
  elements-day-separator elements-typing-indicator elements-thinking-indicator \
  elements-message-actions elements-message-branches elements-scroll-anchor \
  elements-tool-call elements-tool-timeline elements-terminal-block \
  elements-code-diff elements-web-search elements-tool-error elements-link-preview \
  elements-agent-plan elements-subagent-list elements-approval-card \
  elements-composer elements-message-queue elements-message-attachment \
  elements-model-picker elements-reasoning-effort elements-context-usage \
  elements-context-breakdown

# shadcn primitive the element set needs but no listed item pulls (design D13 list)
npx shadcn@latest add -y -o input
```

`components.json`: `style` is **`radix-vega`**, not the bare `"radix"` the task
text suggested. The shadcn schema (https://ui.shadcn.com/schema.json) rejects
bare `"radix"`, and with it the shadcn CLI resolves primitive
registryDependencies against the non-existent
`https://ui.shadcn.com/r/styles/radix/button.json`. Any `radix-*` style listed
in the schema works; for assistant-ui items every non-`base-` style resolves to
the same root tree `https://r.assistant-ui.com/<name>.json` (the Radix flavor,
per https://www.assistant-ui.com/docs/base-ui), so the style choice only picks
which shadcn primitive flavor tree backs `ui.shadcn.com/r/styles/<style>/`.
`radix-vega` is the first schema entry; all primitives landed in
`src/components/assistant-ui/ui/` as expected.

## (b) npm dependencies (exact installed versions)

Read from `node_modules/<pkg>/package.json` after install.

| package | installed | declared by | package.json range |
|---|---|---|---|
| `@assistant-ui/react` | **0.15.30** | pinned by task 1.2 | `0.15.30` (exact) |
| `@assistant-ui/react-markdown` | **0.14.23** | pinned by task 1.2 | `0.14.23` (exact) |
| `remark-gfm` | 4.0.1 | manifests | `^4.0.1` |
| `react-shiki` | 0.11.2 | manifests | `^0.11.2` |
| `tw-shimmer` | 0.4.14 | manifests | `^0.4.14` |
| `class-variance-authority` | 0.7.1 | manifests | `^0.7.1` |
| `radix-ui` | 1.8.1 | manifests | `^1.8.1` |
| `beautiful-mermaid` | 1.1.3 | manifests (the mermaid renderer, NOT `mermaid`) | `^1.1.3` |
| `@base-ui/react` | 1.9.0 | manifests | `^1.9.0` |
| `tw-animate-css` | 1.4.0 | **registry-implied, not declared** (see Divergences) | `^1.4.0` |
| `@types/node` (dev) | 26.6.5 | spike tsconfig fix (see Divergences) | `^26.6.5` |

Notes:
- The `add` runs rewrite `package.json` deps of registry items to `^` ranges
  and **re-range the two pinned `@assistant-ui/*` packages to `^`** — re-pin
  them exact after every re-vendor.
- The manifests also declared a spurious `cn@^0.5.0` (the npm package) because
  the radix-vega primitives import `cn` bare; removed after rewriting those
  imports (see Divergences).
- Already present before the spike and reused: `lucide-react@1.57.0`,
  `shiki@4.5.0`, `@shikijs/engine-javascript@4.5.0`, `diff@9.0.0`,
  `react-markdown@10.1.0` (still imported by the legacy
  `features/chat/components/AssistantMessage`; removed at task 6.1).

## (c) Registry item → files written

All under `frontend/src/components/assistant-ui/` (paths relative to it).

| item | files |
|---|---|
| `thread` | `elements/thread.aui.tsx` |
| `markdown-text` | `elements/markdown-text.tsx` |
| `reasoning` | `elements/reasoning.aui.tsx` |
| `shiki-highlighter` | `elements/shiki-highlighter.aui.tsx` |
| `mermaid-diagram` | `elements/mermaid-diagram.aui.tsx` |
| `conversation-map` | `elements/conversation-map.aui.tsx` |
| `attachment` | `elements/attachment.aui.tsx` |
| `tool-fallback` | `elements/tool-fallback.aui.tsx` |
| `tool-group` | `elements/tool-group.aui.tsx` |
| `task-card` | `elements/task-card.aui.tsx` |
| `composer-trigger-popover` | `elements/composer-trigger-popover.aui.tsx` |
| `tooltip-icon-button` | `elements/tooltip-icon-button.tsx` |
| `use-copy-to-clipboard` | `hooks/use-copy-to-clipboard.ts` |
| `elements-day-separator` | `elements/day-separator.tsx` |
| `elements-typing-indicator` | `elements/typing-indicator.tsx` |
| `elements-thinking-indicator` | `elements/thinking-indicator.tsx` |
| `elements-message-actions` | `elements/message-actions.tsx` |
| `elements-message-branches` | `elements/message-branches.tsx` |
| `elements-scroll-anchor` | `elements/scroll-anchor.tsx` |
| `elements-tool-call` | `elements/tool-call.tsx` |
| `elements-tool-timeline` | `elements/tool-timeline.tsx` |
| `elements-terminal-block` | `elements/terminal-block.tsx` |
| `elements-code-diff` | `elements/code-diff.tsx` |
| `elements-web-search` | `elements/web-search.tsx` |
| `elements-tool-error` | `elements/tool-error.tsx` |
| `elements-link-preview` | `elements/link-preview.tsx` |
| `elements-agent-plan` | `elements/agent-plan.tsx` |
| `elements-subagent-list` | `elements/subagent-list.tsx` |
| `elements-approval-card` | `elements/approval-card.tsx` |
| `elements-composer` | `elements/composer.tsx` |
| `elements-message-queue` | `elements/message-queue.tsx` |
| `elements-message-attachment` | `elements/message-attachment.tsx` |
| `elements-model-picker` | `elements/model-picker.tsx` |
| `elements-reasoning-effort` | `elements/reasoning-effort.tsx` |
| `elements-context-usage` | `utils/context-usage.ts` |
| `elements-context-breakdown` | `elements/context-breakdown.tsx` |

Transitive registryDependencies (pulled automatically, also vendored):

| item | files |
|---|---|
| `elements-surfaces` | `elements/surfaces.tsx`, `utils/useIsomorphicLayoutEffect.ts` |
| `elements-collapsible-root` | `elements/collapsible-root.tsx` |
| `elements-range` | `utils/range.ts` |
| `elements-receipt-focus` | `elements/receipt-focus.ts` |
| `elements-href` | `utils/href.ts` |
| `elements-task` | `utils/task.ts` |
| `follow-up-suggestions` (via `thread`) | `elements/follow-up-suggestions.aui.tsx` |
| `file` (via `thread`) | `elements/file.tsx` |
| `image` (via `thread`) | `elements/image.tsx` |
| `media-player` (via `thread`) | `elements/media-player.tsx` |
| presentational halves of the compositions (via the `elements-*` deps of the runtime-bound `.aui` items) | `elements/reasoning.tsx`, `elements/shiki-highlighter.tsx`, `elements/mermaid-diagram.tsx`, `elements/conversation-map.tsx`, `elements/task-card.tsx` |
| shadcn primitives | `ui/button.tsx`, `ui/tooltip.tsx`, `ui/dialog.tsx`, `ui/avatar.tsx`, `ui/skeleton.tsx`, `ui/textarea.tsx`, `ui/input.tsx` |

**Checksum proof (task 1.3):** the onclaw kit in `src/components/ui/` is
byte-identical before and after vendoring —
`find src/components/ui -type f -exec shasum {} + | shasum` =
`13316ee857b7ba514e230750a1c8524083eaa323` both times (the `ui` alias pointed
the registry at `src/components/assistant-ui/ui/`, so the PascalCase kit was
never touched).

## (d) D4 verdict — approvals: NATIVE tool-call part, no `data-approval` fallback

**Verdict: use the native approval representation.** A pending approval is a
`tool-call` part carrying the `approval` field, and it survives reload because
the field is plain data on the part — buddies rebuilds it from the event log
through the D2 mapping on every mount, and the runtime resolves it against the
adapter's own `messages` snapshot, not against runtime-internal state.

Types that justify it (read from `@assistant-ui/core@0.15.30` dist types):

- `ToolCallMessagePart` (`core/dist/types/message.d.ts:279`) carries:
  ```ts
  readonly approval?: {
      readonly id: string;
      readonly prompt?: string;
      readonly display?: ToolApprovalDisplay;
      readonly allowFreeform?: boolean;
      readonly dismissible?: boolean;
      readonly approved?: boolean;          // undefined = pending
      readonly reason?: string;
      readonly isAutomatic?: boolean;
      readonly options?: readonly ToolApprovalOption[];
      readonly optionId?: string;           // resolution record
      readonly text?: string;
      readonly answers?: Readonly<Record<string, ToolApprovalAnswer>>;
      readonly resolution?: "cancelled" | "expired";
  };
  ```
- `ToolCallMessagePartStatus` (`message.d.ts:386`) has a dedicated pending
  state: `{ type: "requires-action", reason: "tool-calls" | "interrupt" }`
  (as does `MessageStatus`).
- `ExternalStoreAdapter` (`core/dist/runtimes/external-store/external-store-adapter.d.ts:197`)
  declares
  `onRespondToToolApproval?: (options: RespondToToolApprovalOptions) => Promise<void> | void`.
- The external-store runtime's `respondToToolApproval`
  (`external-store-thread-runtime-core.js:966`) looks the part up with
  `messages.findLast(... part.approval?.id === options.approvalId)` — i.e.
  **purely from the adapter's `messages`** — then calls the adapter callback,
  so a reload-rebuilt pending part is immediately answerable.

Exact shapes later waves use:

```ts
// part shape emitted by the D2 mapping for a pending approval entry:
{
  type: "tool-call",
  toolCallId: <entry id>,      // part id = entry id (D2)
  toolName: <tool>,
  args, argsText,
  approval: { id: <buddies approval id> },   // approved omitted = pending
}

// adapter wiring (task 2.3):
onRespondToToolApproval: ({ approvalId, approved }) =>
  approved ? store.approve(approvalId) : store.deny(approvalId),

// runtime types:
RespondToToolApprovalOptions = {
  approvalId: string;
  approved: boolean;
  optionId?: string; text?: string;
  answers?: Readonly<Record<string, ToolApprovalAnswer>>;
  reason?: string;
}
```

Once the store records the decision (also in the event log), the re-mapped part
carries `approval.approved: true/false` — the resolved state is reload-stable
too. `elements-approval-card.tsx` renders the four states
(`ApprovalState = "request" | "running" | "done" | "denied"`); D6 grouping must
treat `approval?.approved === undefined` as a hard split so a pending approval
never folds into a `tool-timeline`.

## (e) D11 verdict — sessions rail: KEEP `SessionRail`

Fetched `https://r.assistant-ui.com/threadlist-sidebar.json` and
`thread-list.json` to `/tmp/threadlist-sidebar.json` / `/tmp/thread-list.json`
and read both sources. Judged against the five sessions-rail behaviours:

1. **Per-agent grouping — NOT expressible.** `useThreadListGroups` buckets by
   *date* (Today / Yesterday / Earlier) and the item rows render from
   `s.threads.threadIds`; an agent dimension does not exist in the
   `threadList` adapter state (`ExternalStoreThreadData` has no group field).
2. **Title search — expressible.** `ThreadListSearch` filters by title
   client-side.
3. **Delete-with-confirmation incl. in-flight refusal — NOT expressible.**
   `ThreadListItemPrimitive.Delete` in the "more" dropdown deletes
   immediately; there is no confirmation step and no surface for a refusal.
   (The adapter's `onDelete` promise semantics — resolution drops, rejection
   keeps — exist on `ExternalStoreAdapter`, but the element renders neither a
   confirm dialog nor an error message; the runtime only logs a rejection.)
4. **Running indicator — expressible.** `s.threadListItem.isRunning` renders a
   live dot + `sr-only` "Running".
5. **Drag-resize with four-most-recent cap — NOT expressible.** The sidebar
   shell is shadcn `Sidebar` (collapsible rail, no drag resize); no recency
   cap exists anywhere in the element.

Three of five fail, and `threadlist-sidebar` itself is a demo shell (hardcoded
assistant-ui header, GitHub footer) that additionally pulls shadcn `sidebar`
plus its `sheet`/`tooltip`/`input`/`skeleton`/`separator` tail.
**Verdict: keep `SessionRail`** (the D11 default).

## (f) `markdown-text.tsx` — `components` prop: YES

The vendored `elements/markdown-text.tsx` exposes BOTH hooks:

```ts
type MarkdownTextProps = Partial<TextMessagePartProps> & {
  components?: Parameters<typeof memoizeMarkdownComponents>[0];   // merged OVER defaultComponents
  componentsByLanguage?: MarkdownTextPrimitiveProps["componentsByLanguage"];
};
```

`components` is shallow-stabilised (`useShallowStable`) and merged over the
element's `defaultComponents`; `componentsByLanguage` is passed through to
`MarkdownTextPrimitive` (GFM is wired inside via `remarkPlugins={[remarkGfm]}`).
So later waves register the shiki `SyntaxHighlighter` and the mermaid wrapper
as props — no edit to the vendored file needed:

```tsx
<MarkdownText
  componentsByLanguage={{ mermaid: MermaidWrapper }}   // D5
  components={{ pre: ShikiPre, code: ShikiCode }}       // or per-language via componentsByLanguage
/>
```

## (g) API cheat-sheet for waves 2–5

All library names verified present at runtime in `@assistant-ui/react@0.15.30`
(`node --input-type=module -e "import * as aui from '@assistant-ui/react'"`).

### Library imports (all from `"@assistant-ui/react"` unless noted)

- `useExternalStoreRuntime(adapter)` — builds the runtime; adapter fields used
  by D1: `messages | messageRepository`, `isRunning`, `onNew`, `onEdit`,
  `onReload`, `onCancel`, `setMessages`, `hasEarlier`, `onLoadEarlier`,
  `queue`, `onAddToolResult`, `onResumeToolCall`, `onRespondToToolApproval`,
  `convertMessage`, `adapters: { attachments, threadList, … }`.
- `AssistantRuntimeProvider` — `FC<PropsWithChildren<{ runtime; aui?; config? }>>`;
  mount at the chat view root.
- `ExportedMessageRepository.fromBranchableArray(items, options?)` with
  `items: readonly { message: ThreadMessageLike; parentId: string | null }[]`,
  `options?: { headId?: string | null }` → `ExportedMessageRepository` for the
  adapter's `messageRepository` (D3 branch tree). Also `fromArray`.
- `useAuiState(selector)` — selector over `AssistantState`
  (`s.threads.threadIds`, `s.threadListItem.isRunning`, …).
- `useAui()`, `AuiIf`, `AuiProvider`, `useAuiEvent` — also exported.
- `useThreadViewport()` — viewport store hook (from
  `context/react/ThreadViewportContext`; re-exported at the package root).
- `useThreadViewportAutoScroll` — the auto-scroll behaviour D9 builds on.
- `useToolCallElapsed` — live elapsed-time hook (running-tool status line, 3.2).

### Primitives (namespace objects)

- `ThreadPrimitive`: `Root`, `Empty`, `If`, `Viewport`, `ViewportProvider`,
  `ViewportFooter`, `Messages`, `MessageByIndex`, `Unstable_MessageById`,
  `Row`, `ScrollToBottom`, `LoadEarlier`, `Suggestion`, `Suggestions`.
- `MessagePrimitive`: `Root`, `Parts` (alias `Content`), `PartByIndex`, `If`,
  `Attachments`, `AttachmentByIndex`, `Quote`, `Error`, `GroupedParts`,
  `GenerativeUI`, `Unstable_PartsGrouped`.
- `ComposerPrimitive`: `Root`, `Input`, `Send`, `Resume`, `Cancel`,
  `AddAttachment`, `Attachments`, `AttachmentByIndex`, `AttachmentDropzone`,
  `Dictate`, `StopDictation`, `DictationTranscript`, `If`, `Quote`,
  `QuoteText`, `QuoteDismiss`, `Queue`, `TriggerPopover`.
- Also exported: `ActionBarPrimitive`, `ActionBarMorePrimitive`,
  `BranchPickerPrimitive`, `AttachmentPrimitive`, `MessagePartPrimitive`,
  `ErrorPrimitive`, `QueueItemPrimitive`, `SuggestionPrimitive`,
  `ThreadListPrimitive`, `ThreadListItemPrimitive`,
  `ThreadListItemMorePrimitive`, `SelectionToolbarPrimitive`,
  `AssistantModalPrimitive`, `ChainOfThoughtPrimitive`.

### Tool renderers (D6)

- `defineToolkit` + `Tools` exist at the package root (`Tools` is an FC;
  `defineToolkit` builds a `ToolkitDefinition`; also `stubTool`,
  `externalTool`, `hitl`/`hitlTool`, `DataRenderers`, `Interactables`).
- Legacy per-tool UI registration also exists:
  `makeAssistantToolUI` / `useAssistantToolUI` (`AssistantToolUI`),
  `makeAssistantDataUI` / `useAssistantDataUI`, `makeAssistantTool`,
  `useAssistantInstructions`, `useInlineRender`, `useToolArgsStatus`.

### Key part/status types (from `@assistant-ui/core`, re-exported at the root)

`ToolCallMessagePart` (fields: `type:"tool-call"`, `toolCallId`, `toolName`,
`args`, `argsText`, `result?`, `isError?`, `isPreliminary?`,
`responseDeliveryStatus?`, `timing?`, `interrupt?`, `approval?`,
`messages?` (nested sub-agent thread), `parentId?`),
`ToolCallMessagePartStatus` (`requires-action`/`incomplete`/complete),
`MessageStatus` (`running` / `requires-action{tool-calls|interrupt}` /
`complete` / `incomplete{cancelled|tool-calls|length|content-filter|other|error}`),
`DataMessagePart` (custom `data-*` parts), `ReasoningMessagePart`,
`ThreadMessageLike`, `AppendMessage`, `RespondToToolApprovalOptions` (see D4).

### Vendored element exports (one line each, read from the files)

Compositions (runtime-bound, read assistant-ui context):

- `thread.aui.tsx` — `Thread({ ...ThreadProps })`, types `ThreadProps`,
  `ThreadComponents`, `ThreadGroupPart`.
- `reasoning.aui.tsx` — re-exports `Reasoning`, `ReasoningGroup`,
  `ReasoningRoot`, `ReasoningTrigger`, `ReasoningContent`, `ReasoningText` +
  `ReasoningRootProps` (from `./reasoning`).
- `shiki-highlighter.aui.tsx` — `SyntaxHighlighter: FC<HighlighterProps>`
  (`HighlighterProps = Omit<SyntaxHighlighterProps,...>`, binds markdown code
  context).
- `mermaid-diagram.aui.tsx` — `MermaidDiagram`, `MermaidZoom`;
  `MermaidDiagramProps = SyntaxHighlighterProps & {...}`.
- `conversation-map.aui.tsx` — `ConversationMapAui(...)` (binds thread
  messages; wraps presentational `ConversationMap`).
- `attachment.aui.tsx` — `UserMessageAttachments: FC`,
  `ComposerAttachments: FC`, `ComposerAddAttachment: FC`.
- `tool-fallback.aui.tsx` — `ToolFallback`, `ToolFallbackRoot`,
  `formatUnknownValue`, `offersInterruptAction` + `ToolFallbackRootProps`.
- `tool-group.aui.tsx` — `ToolGroup`, `ToolGroupRoot`, `ToolGroupTrigger`,
  `ToolGroupContent` + `ToolGroupRootProps`.
- `task-card.aui.tsx` — `TaskPart` (type: `ToolCallMessagePart & {...}`),
  `isTaskPart(part)`, `TaskCard({ part, className })`, `TaskGroup({ ... })`.
- `composer-trigger-popover.aui.tsx` — `ComposerTriggerPopover` (exactly one
  of `directive` / `action` props; dev-only warn otherwise).
- `follow-up-suggestions.aui.tsx` — `ThreadFollowupSuggestions`.
- `tooltip-icon-button.tsx` — `TooltipIconButton` (`TooltipIconButtonProps`).

Presentational elements (props-driven, standalone):

- `day-separator.tsx` — `DaySeparator({ messages: readonly DatedMessage[] })`;
  `DatedMessage = { id, day, time, role: "user"|"assistant", text }`; renders
  a divider when consecutive messages differ in `day`.
- `typing-indicator.tsx` — `TypingIndicator({ variant?: "bubble"|"bare" })`.
- `thinking-indicator.tsx` — `ThinkingIndicator({ label: string, elapsed?: string })`.
- `message-actions.tsx` — `MessageActions({ copied, reaction, regenerating,
  onCopy, onReactionChange, onRegenerate, onMore })`; type `Reaction`.
- `message-branches.tsx` — `MessageBranches({ variants: readonly string[],
  index, onIndexChange })` (renders `n / total` when `variants.length > 1`).
- `scroll-anchor.tsx` — `ScrollAnchor({ messages: readonly
  ScrollAnchorMessage[], paused?: boolean, onSettled? })`;
  `ScrollAnchorMessage = { role, text }` (jump-to-latest pill; owns its own
  pinned/seen logic off the viewport ref).
- `tool-call.tsx` — `ToolCall({ label, activeLabel, query, request, result,
  running, open, onOpenChange })` (disclosure card, `max-w-sm`).
- `tool-timeline.tsx` — `ToolTimeline({ steps: readonly TimelineStep[],
  visibleSteps: number, streaming: boolean, open, onOpenChange,
  restingLabel, activeLabel, stats: TimelineStat[] })`;
  `TimelineStep = { id?, verb, chip, icon: LucideIcon }`;
  `TimelineStat = { file, added?, removed? }`.
- `terminal-block.tsx` — `TerminalBlock({ command, lines: readonly string[],
  visibleCount: number, done: boolean, variant?: "paper"|"ink",
  exitCode?: number, stderr?: readonly string[], cwd?: string,
  durationMs?: number, truncated?: boolean, maxCollapsedLines?: number })`;
  also exports `parseAnsi(line): AnsiSegment[]`, types `AnsiColor`,
  `AnsiSegment`. Line-by-line reveal is a timer replay over `visibleCount`.
- `code-diff.tsx` — `CodeDiff({ filename, additions, deletions,
  lines: readonly DiffLine[], cycle })`; `DiffLine = { kind:
  "context"|"added"|"removed", text }`; `cycle` forces re-measure.
- `web-search.tsx` — `WebSearch({ query, results: readonly WebSearchResult[],
  visibleResults: number, searching: boolean, cycle })`;
  `WebSearchResult = { title, domain }` (staggered landing via
  `visibleResults` + `cycle`).
- `tool-error.tsx` — `ToolError({ name, target, message, attempt, maxAttempts,
  retrying, onRetry?, onSkip? })`.
- `link-preview.tsx` — `LinkPreview({ href, title?, description?, image?,
  imageAlt?, siteName?, favicon?, layout?: "card"|"compact" })`.
- `agent-plan.tsx` — `AgentPlan({ steps: readonly (string|AgentPlanStep)[],
  activeIndex: number, title?: string })`;
  `AgentPlanStep = { id?, label, description? }`.
- `subagent-list.tsx` — `SubagentList({ agents: readonly SubagentItem[],
  completedCount: number, progress: readonly number[], showSummary: boolean,
  summaryAgent: SubagentItem })`; `SubagentItem = { name, model }`.
- `approval-card.tsx` — `ApprovalCard({ state: ApprovalState, command?,
  title, subtitle, description?, details?: readonly { label, value }[],
  variant?: "default"|"destructive", icon?, onAllowOnce?, onAlwaysAllow?,
  onDeny?, allowOnceLabel?, alwaysAllowLabel?, denyLabel?, statusLabel? })`;
  `ApprovalState = "request" | "running" | "done" | "denied"`.
- `composer.tsx` — `Composer` (root), `ComposerBar`, `ComposerMenu`,
  `ComposerMenuItem`, `ComposerCommandItem`, `ComposerPersonItem`,
  `ComposerAttachments`, `ComposerAttachmentChip`, `ComposerInput`,
  `ComposerVoice`, `ComposerToolbar`, `ComposerActions`,
  `ComposerAttachButton`, `ComposerModelTrigger`, `ComposerModelItem`,
  `ComposerContext`, `ComposerVoiceButton`, `ComposerSend`; hooks
  `useSlashMatches`, `useMentionMatches`, `applyMention`.
- `message-queue.tsx` — `MessageQueue({ running: string,
  queued: readonly QueuedMessage[], onCancel?: (id) => void })`;
  `QueuedMessage = { id, text }`.
- `message-attachment.tsx` — `MessageAttachments({ attachments: readonly
  MessageAttachmentItem[], onOpen?: (id) => void })`;
  `MessageAttachmentItem = { id, name, size, kind:
  "image"|"document"|"file", pages?, swatch? }`.
- `model-picker.tsx` — `ModelPicker({ models: readonly PickableModel[],
  selectedId: string, onSelect?: (id) => void })`; `PickableModel = { id,
  name, family, context, price, capabilities: readonly string[] }`.
- `reasoning-effort.tsx` — `ReasoningEffort({ levels: readonly EffortLevel[],
  selectedKey, spent: number, onSelect?: (key) => void })`;
  `EffortLevel = { key, label, budget }`.
- `context-usage` — `utils/context-usage.ts` (no component): `TokenUsage`,
  `getUsagePercent`, `getStrokeColor`, `getBarColor`, `getPercentColor`,
  `getContextSegments`.
- `context-breakdown.tsx` — `ContextBreakdown({ segments: readonly
  ContextSegment[], limit: number })`; `ContextSegment = { label, tokens,
  tint }`.
- `conversation-map.tsx` — `ConversationMap({ entries: readonly
  ConversationMapEntry[], activeId?, visibleIds?, onSelect?, side?:
  "left"|"right" })`; `ConversationMapEntry = { id, title, preview? }`.
- `collapsible-root.tsx` — `CollapsibleRoot`, `CollapsibleTrigger`,
  `CollapsibleContent` (uncontrolled height animation via
  `--collapsible-panel-height`).
- Shared surfaces — `surfaces.tsx` exports `paper`, `floating`, `field`,
  `fieldInteractive`, `pressable`, `ghostButton`, `inkButton`, `iconSwap(+-In/Out)`,
  `labelSwap(+-In/Out)`, `collapsePanel`, `live`, `mono`, `codeScroll`,
  `codeSurface`, `ShimmerLabel`, `SwapLabel` (**buddies-retargeted — see
  Divergences**).
- Utils — `utils/range.ts` (`clamp`, `take`, `indexIn`, `at`, `pct`,
  `announced`, `progressOf`), `utils/task.ts` (`TaskViewState`, `taskStateOf`,
  `taskLabel`, `taskMeta`, `formatElapsed`, `useTaskElapsed`,
  `TASK_PAGE_SIZE`), `utils/href.ts` (`safeHref`, `hostOf`),
  `elements/receipt-focus.ts` (`useReceiptFocus`),
  `hooks/use-copy-to-clipboard.ts` (`useCopyToClipboard`),
  `hooks/use-attachment-src.ts` (`useAttachmentSrc`),
  `utils/useIsomorphicLayoutEffect.ts`.

### `.aui.tsx` vs plain `.tsx`

Runtime-bound compositions end in `.aui.tsx` (they read assistant-ui context
and only render inside `AssistantRuntimeProvider`); the same-named plain
`.tsx` file, where present, is the standalone presentational half. buddies
composes its own thread/composer in `features/chat/` from primitives plus the
presentational halves — it does not mount `Thread`/`Composer` wholesale
(D13).

## Divergences from the registry output (re-apply after re-vendoring)

1. **`surfaces.tsx` buddies retarget (design D12)** — `paper`/`floating` →
   `bg-card border border-border/60` (was `bg-background … dark:bg-popover`);
   `inkButton` → `bg-primary text-primary-foreground` with the buddies
   `--accent-hover`/`--accent-active` hovers (was inverted
   `bg-foreground text-background`); `live` → `text-accenttext` (was hardcoded
   `text-blue-500 dark:text-blue-400`); `mono` → `text-xs` (12px, was 11px).
   `field`/`fieldInteractive`/`ghostButton` keep the registry's fg-alpha
   tints — the same grammar as buddies chips. Marked with a `BUDDIES RETARGET`
   header comment.
2. **`from "cn"` → `from "@/lib/utils"`** in the seven `ui/*.tsx` shadcn
   primitives (the radix-vega tree imports the bare `cn` npm package; buddies
   keeps one `cn` in `src/lib/cn.ts`). Also removed the spurious `cn`
   dependency the CLI had installed.
3. **`tw-animate-css` import** — the vendored elements use the shadcn
   `animate-in` family (~100 call sites) but no manifest declares it; wired
   `@import "tw-animate-css"` in `src/styles.css` (plus the CLI-added
   `@import "tw-shimmer"` for the `shimmer` utility used by `ShimmerLabel`).
4. **tsconfig `lib: ES2022` + dev `@types/node`** — vendored code uses
   `Array.prototype.at` and `Object.hasOwn` (ES2022 lib) and a
   `process.env.NODE_ENV` guard in `composer-trigger-popover.aui.tsx`. Target
   stays ES2020; the runtimes targeted (WKWebView, modern browsers) ship both
   APIs natively.
5. **tsconfig has `paths` without `baseUrl`** — the task text specified
   `"baseUrl": "."`, but tsgolint (the type-aware linter behind
   `vp check`) rejects `baseUrl` as an invalid tsconfig, which would disable
   type-aware lint repo-wide. `paths` alone (TS ≥ 4.1) resolves `@/*` fine;
   `npx tsc --noEmit` and `vp check src` both pass.
6. **`input.tsx` vendored explicitly** — listed by design D13 but not pulled
   by any listed item (nothing vendored imports it yet; task 5.x wrappers
   will).
7. **`styles.css` additions the CLI made** (kept): `@custom-variant data-open`
   / `data-closed`, collapsible keyframes in `@theme inline`. The duplicated
   `input[type="range"]` block the CLI appended was already identical to the
   existing one — nothing to remove.
8. **`elements/code-diff.tsx` carries no gutter numbers — the buddies wrapper
   supplies them** (spec `chat-tool-cards` "Edit renders a diff": removed and
   added lines *with line numbers*). Its `DiffLine` is `{ kind, text }` and a
   row renders only its `+`/`−` marker, so
   `features/chat/tools/aui/FileTools.tsx` puts a buddies-side gutter column
   beside the element inside one shared `overflow-x-auto` surface, and moves
   the element out of its way through `className` only — the vendored file
   stays untouched: `border-0 rounded-none` (the column wrapper owns the
   card), the inner scroll off so both columns scroll together, the header
   path truncated so it cannot wrap and shift the rows, `animate-none` on the
   staggered row reveal (a static gutter cannot fade in with it), and the same
   `min-h-[1.46875rem]` row box on both sides (a blank line has no line box,
   so unpinned columns drift). The numbers come from the row builder in
   `features/chat/tools/aui/diffRows.ts` — the one computation the element's
   lines are cut from. The row tints stay the registry's
   `bg-emerald-500/10` / `bg-red-500/10` pair with their `dark:` halves (both
   themes legible), not buddies tokens.
9. **The flat transcript rides the vendored elements' own props — no vendored
   edit** (change `flatten-chat-transcript`, design D1). The transcript's flat
   look — the reasoning as a disclosure line, the tool call as a quiet line,
   the turn rule — is expressed entirely through props the vendored elements
   already expose: `ReasoningRoot variant="ghost"` plus caller `className`
   merges on the root, the trigger, the content, and the text, and the tool
   card's own wrapper (`features/chat/tools/aui/ToolCallFrame.tsx`), which
   simply stops consuming `surfaces.paper` and paints its line itself. **No
   file under `src/components/assistant-ui/` changed for it** — the reasoning
   element's `outline` default and the `paper` surface are still the
   registry's, and `paper` is still what the other vendored elements that
   consume it use. A re-vendor therefore needs no re-application for this
   look; what it does need is for the vendored `variant` values (`ghost`) and
   the `className` merge seams to survive, so the wrappers assert on the
   vendored `data-slot` hooks and on buddies-owned test ids rather than on
   vendored class names.

## Spike gate (task 1.7) — RE-RECORDED at the closing wave (2026-10-21)

The wave-0 spike is re-recorded here with the fresh output of the closing wave
(tasks 6.1-6.5): the old layer is retired, the dependencies are pinned, the
wrap toggle and the theme-parity suite landed.

### `npm run build` (tsc + vp build, production)

```
built in 1.39s
BUILD EXIT: 0
```

The mermaid element stays its own chunk (`mermaid-*.js`) and the shiki
grammars stay per-language chunks (`go-*.js`, `python-*.js`, ...). The
completed text path loads LAZILY from the buddies wrapper
(`features/chat/thread/ChatAssistantMessage.tsx`), so the vendored markdown
element and the shiki element no longer weigh the first paint: the main chunk
went from 3,087,227 B to 2,774,693 B. The shiki CORE still lands in the main
chunk — the retained `features/chat/tools/primitives/shikiHighlight.ts`
imports `shiki/core` statically, exactly as the pre-change layer did, so that
is not a regression of this change.

### `npm test` (vitest run, jsdom)

```
 Test Files  93 passed (93)
      Tests  784 passed (784)
TEST EXIT: 0
```

Three consecutive full-suite runs green. The count is lower than the wave-0
baseline because the retired layer's 20 suites went with it — every scenario
they carried was ported onto the new layer first (see the porting map in the
run handoff).

### `vp check src`

```
pass: All 244 files are correctly formatted (279ms, 10 threads)
Found 0 errors and 36 warnings in 239 files (2.2s, 10 threads)
CHECK EXIT: 0
```

0 errors. The 36 warnings are the pre-existing baseline (the wave-0 spike
recorded 49; the retirement removed the warnings that lived in the deleted
files).

### `grep -rni daisyui src | wc -l`

Raw count: **17** — NOT zero, and never was (the wave-0 spike recorded 13).
Every hit is a *comment or test description* referencing the retired
`replace-daisyui-with-styled-components` change (`src/styles.css` header
notes, `SessionRail.test.tsx`, `ModalShell.test.tsx`, `theme.ts`/`theme.test.ts`,
kit component doc comments) plus this file's own gate documentation. No
daisyUI import, dependency or class string exists anywhere in `src/`; the
gate's intent (daisyUI is gone) holds. Zeroing the raw grep needs a comment
sweep in files outside this change's scope.

### Checksum re-verification (end of wave)

```
find src/components/ui -type f -exec shasum {} + | shasum
a88024e519b0925ecb4f390bf842cc7d534c0a34  -
```

The kit is untouched by the vendoring: every file under `src/components/ui/`
still carries its pre-vendor mtime (the newest is 2026-10-20 10:34, the
vendoring landed 2026-10-21 00:00). The digest differs from the wave-0
spike's string only because `find` walks the directory in its own order —
the file set and contents are the same.
