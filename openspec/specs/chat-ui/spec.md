# chat-ui Specification

## Purpose
TBD - created by archiving change refactor-chat-ui. Update Purpose after archive.
## Requirements
### Requirement: Thread-Based Chat Layout
The system SHALL display the chat interface using a Thread-based layout, comprising a persistent sidebar for conversation history (ThreadList) and a main view for the active conversation.

#### Scenario: Navigating conversations
- **WHEN** the user selects a conversation from the ThreadList sidebar
- **THEN** the active conversation is loaded in the main view without navigating to a different page tab

### Requirement: Headless Chat Primitives
The system SHALL implement the chat surface as composable headless primitives — `Thread`, `ThreadList`, `Composer`, and `Message` — built as compound components with render-prop sub-parts, consuming shared state via selector hooks (`useThread`, `useComposer`, `useMessage`, `useThreadList`) over the `ChatProvider` runtime. Primitives SHALL emit classNames/roles only and carry no visual styling of their own; all styling SHALL live in the page composition and stylesheets.

#### Scenario: Nested part reaches state without prop-drilling
- **WHEN** a deeply nested part (e.g. a message action bar or composer send button) needs streaming state or the run action
- **THEN** it reads them via the appropriate selector hook rather than receiving them as props

#### Scenario: Render-prop slots enumerate content
- **WHEN** `Thread.Messages` or `Message.Parts` render
- **THEN** they accept a render-prop that receives each message (or content group) and its index, rather than hard-coding the rendering

### Requirement: Thread Viewport with Auto-scroll and Custom Scrollbar
The system SHALL render the message list inside a scrollable `Thread.Viewport` that auto-scrolls to the latest content during streaming, with a custom themed scrollbar styled via CSS only (no `scroll-area` dependency).

#### Scenario: Auto-scroll during streaming
- **WHEN** new content arrives while the user is at the bottom of the viewport
- **THEN** the viewport scrolls to reveal the new content

#### Scenario: Custom scrollbar respects reduced motion
- **WHEN** the viewport scrollbar is styled and the user has `prefers-reduced-motion` enabled
- **THEN** no motion-based transition is applied to the scrollbar

### Requirement: Multi-line Composer Input
The composer input SHALL be a multi-line text area. Pressing `Enter` SHALL submit the message; pressing `Shift+Enter` SHALL insert a newline without submitting. The composer SHALL make this binding discoverable by stating it in both the input placeholder and the composer fineprint.

#### Scenario: Shift+Enter inserts a newline
- **WHEN** the user presses `Shift+Enter` in the composer
- **THEN** a newline is inserted into the input and the message is not submitted

#### Scenario: Enter submits
- **WHEN** the user presses `Enter` (without Shift) on a non-empty input while not streaming
- **THEN** the message is submitted

#### Scenario: Textarea grows with content
- **WHEN** the input grows beyond one line
- **THEN** the textarea expands up to a bounded maximum height and then scrolls internally, and resets its height after submit

#### Scenario: The multi-line binding is discoverable
- **WHEN** the composer renders its placeholder and fineprint
- **THEN** both indicate that `Enter` sends and `Shift+Enter` inserts a newline

### Requirement: Composer Paste-to-Attach
The composer SHALL accept image and file data pasted from the clipboard, display a removable preview, and transmit the pasted media to the agent as native eino `UserInputImage`/`UserInputFile` content blocks on the `/api/chat` request. Text prompt SHALL remain required.

#### Scenario: Pasting an image shows a preview and is sent
- **WHEN** the user pastes image data into the composer and submits
- **THEN** a preview chip is shown before submit and the image is transmitted as a `UserInputImage` content block

#### Scenario: Removing a pasted attachment before submit
- **WHEN** the user removes the preview chip
- **THEN** the attachment is not transmitted

#### Scenario: Prompt remains required
- **WHEN** the user submits with pasted media but no text prompt
- **THEN** submission is rejected with a "Prompt is required" error

#### Scenario: Unsupported paste is ignored
- **WHEN** the user pastes content the browser does not expose as an image or file
- **THEN** the paste is ignored without error

### Requirement: Slash Command Picker
The composer SHALL support a slash command (`/`) popover for selecting Agent Skills, anchored to the composer (not the caret) because the input is multi-line.

#### Scenario: Selecting a skill
- **WHEN** the user types `/` in the composer
- **THEN** a popover appears showing available skills, filtered as the user continues typing
- **WHEN** the user selects a skill
- **THEN** the skill is inserted into the composer

### Requirement: Rich Message Rendering
The system SHALL render agent messages using composable blocks, including Markdown, syntax-highlighted code, and Chain-of-Thought (Reasoning).

#### Scenario: Rendering code blocks
- **WHEN** the agent outputs a markdown code block
- **THEN** the system renders it with syntax highlighting

#### Scenario: Viewing reasoning
- **WHEN** the agent outputs a reasoning block
- **THEN** it is displayed in a collapsible accordion within the message

### Requirement: Semantic Message Parts with Grouping
The system SHALL render a message's content blocks through a `Message.Parts` render-prop driven by a `groupBlocks` pre-pass that clusters consecutive blocks into groups before per-block dispatch. A fallback renderer SHALL render any unrecognized block type so that nothing is silently dropped.

#### Scenario: Unrecognized block is not dropped
- **WHEN** a message contains a content block type with no dedicated renderer
- **THEN** the system renders a visible fallback (e.g. `[<type> block]`) rather than nothing

### Requirement: Tool Call Grouping
The system SHALL collapse a run of two or more consecutive tool-call/result blocks (function or MCP) into a single collapsible `ToolGroup`, while still dispatching each block to its dedicated renderer (e.g. `skill` → `SkillActivated`) inside the group.

#### Scenario: Consecutive tool calls collapse
- **WHEN** an assistant message contains two or more consecutive tool-call/result blocks not preceded by a reasoning block
- **THEN** they render as one collapsed `ToolGroup` showing the count, expandable to reveal each call

### Requirement: Chain-of-Thought Grouping
The system SHALL group a reasoning block immediately followed by one or more tool-call/result blocks into a single collapsible `ChainOfThought` accordion. A reasoning block with no following tool calls SHALL render as a standalone `Reasoning` block.

#### Scenario: Reasoning followed by tools is grouped
- **WHEN** a reasoning block is immediately followed by one or more tool-call/result blocks
- **THEN** they render together inside one `ChainOfThought` accordion

#### Scenario: Lone reasoning is standalone
- **WHEN** a reasoning block is not followed by any tool block
- **THEN** it renders as a standalone `Reasoning` block

### Requirement: Skill Activation Rendering
The system SHALL render a `skill` tool invocation — a tool call whose name is `skill` — using a dedicated `SkillActivated` block that is visually and semantically distinct from the generic tool-call block, displaying the invoked skill's name as the headline. This dispatch SHALL occur before the generic tool-call fall-through, including inside a `ToolGroup`.

#### Scenario: Skill call is not rendered as a generic tool call
- **WHEN** the agent invokes the `skill` tool
- **THEN** the message renders a `SkillActivated` block (not the generic `ToolCall` block) whose headline is the skill name parsed from the tool arguments
- **AND** the skill's tool-result body is rendered collapsed by default within the same block

### Requirement: MCP Tool Rendering
The system SHALL render `MCPToolCall` and `MCPToolResult` blocks using a dedicated `MCPCalled` block that is distinct from the generic function tool-call block.

#### Scenario: MCP call rendered distinctly
- **WHEN** the agent invokes an MCP tool
- **THEN** the message renders an `MCPCalled` block distinct from the generic `ToolCall` block

### Requirement: Inline Media and File Rendering
The system SHALL render image content blocks (`UserInputImage`, `AssistantGenImage`) as inline image previews and file content blocks (`UserInputFile`) as file chips showing the file name.

#### Scenario: Image block renders as a preview
- **WHEN** a message contains an image content block
- **THEN** the system renders an inline image preview

#### Scenario: File block renders as a named chip
- **WHEN** a message contains a file content block
- **THEN** the system renders a file chip showing the file name

### Requirement: Diff Rendering via Markdown Convention
The system SHALL render markdown fenced code blocks tagged `diff` as a rendered diff view rather than plain monospace text.

#### Scenario: Diff fence renders as a diff
- **WHEN** agent output contains a ```` ```diff ```` fenced code block
- **THEN** the system renders it as a diff view

### Requirement: Derived Sources
The system SHALL render derived source citations — URL and title chips with favicon and external link — extracted from search/browser/fetch `FunctionToolResult` content. This is a derived rendering; a typed citation contract is out of scope.

#### Scenario: Tool results with URLs render as sources
- **WHEN** an assistant message includes search/browser/fetch tool results containing URLs
- **THEN** the system renders a deduplicated sources list after the message parts

### Requirement: Message Action Bar
The system SHALL show an action bar on assistant messages with Copy and Regenerate actions. The action bar SHALL auto-hide until the message is hovered or focused, show a transient "copied" state after Copy, and disable Regenerate while streaming.

#### Scenario: Copying assistant text
- **WHEN** the user activates Copy on an assistant message
- **THEN** the message's text content is written to the clipboard and a "copied" state is shown briefly

#### Scenario: Regenerating a response
- **WHEN** the user activates Regenerate on an assistant message while not streaming
- **THEN** the last user prompt is re-run (a new response is generated; no branching is created)

### Requirement: Flat Content-Forward Messages
The system SHALL render chat messages without per-message avatars or role-label rows. Assistant messages SHALL render full-width with no enclosing bubble (content placed directly in the feed); user messages SHALL render right-aligned in a subtle muted bubble. Role SHALL be conveyed by alignment rather than by an explicit label.

#### Scenario: Assistant message is flat and full-width
- **WHEN** an assistant message renders
- **THEN** it has no avatar, no role-label row, and no enclosing bubble, and spans the full message width

#### Scenario: User message is right-aligned
- **WHEN** a user message renders
- **THEN** it is right-aligned in a subtle muted bubble, with no avatar or role-label row

### Requirement: Code Block Copy and Language Label
The system SHALL render fenced code blocks on their own surface with a copy control and the code language labeled, in addition to syntax highlighting.

#### Scenario: Code block has a copy button and language label
- **WHEN** the agent outputs a fenced code block with a language tag
- **THEN** the block renders on its own surface with the language labeled and a copy control that copies the code to the clipboard

### Requirement: Composer File-Picker Attachment
The composer SHALL provide an attachment control that opens a native file dialog; a selected image or file SHALL be attached via the same send path as clipboard paste (transmitted as native eino `UserInputImage`/`UserInputFile`). Drag-drop and multi-attachment management remain out of scope.

#### Scenario: Attaching a file via the picker
- **WHEN** the user opens the file dialog from the composer attachment control, selects an image or file, and submits
- **THEN** the attachment is shown as a removable preview and transmitted as the appropriate content block, with a text prompt still required

### Requirement: Sidebar New-Chat-On-Top Layout
The ThreadList sidebar SHALL render a "New chat" action at the top, followed by a flat conversation history list in which the active conversation is highlighted.

#### Scenario: New chat is reachable from the top of the sidebar
- **WHEN** the sidebar renders
- **THEN** a "New chat" control appears at the top, above the conversation history list

#### Scenario: Active conversation is highlighted
- **WHEN** a conversation is active
- **THEN** its sidebar item is visually highlighted distinct from inactive items

### Requirement: Chat Header Bar
The chat surface SHALL render a header bar across the top of the main thread column, above the message viewport, that persists across conversation turns. The header SHALL follow the application's standard header styling pattern. The agent selector SHALL be placed at the far-left of this header; the context-usage meter SHALL be placed at the far-right. The agent selector SHALL NOT also appear in the composer toolbar.

#### Scenario: The agent selector lives in the header
- **WHEN** the chat surface renders
- **THEN** the agent selector appears at the far-left of the chat header bar and is absent from the composer toolbar

#### Scenario: The header persists during streaming
- **WHEN** the agent is streaming a response
- **THEN** the chat header bar (and the agent selector within it) remains visible and is not replaced by streaming UI

### Requirement: Context Window Usage Meter
The chat header SHALL display a context-window usage meter showing how much of the active agent's context window the current conversation is consuming. The meter SHALL present a `used / max` readout alongside a progress bar whose fill width is the used-to-max ratio clamped to 100%. The `used` value SHALL be the most recent completed turn's `prompt_tokens` (representing the full prompt resent each turn, i.e. true context fill), and `max` SHALL be the active agent's resolved context-window size. The meter SHALL be hidden when the context window is unknown (zero).

#### Scenario: The meter updates after a turn
- **WHEN** a turn completes and the client receives the turn's token usage
- **THEN** the meter's `used` value reflects that turn's `prompt_tokens` against the agent's context window

#### Scenario: The meter shows the context window as the maximum
- **WHEN** the chat stream initializes
- **THEN** the meter's denominator is the agent's resolved context window, received before the first turn completes

#### Scenario: The meter shows on a loaded conversation
- **WHEN** the user opens an existing conversation from history
- **THEN** the meter reflects the last persisted turn's `prompt_tokens` against the context window without requiring a new turn

#### Scenario: The meter hides when the context window is unknown
- **WHEN** the active agent's context window cannot be determined (zero)
- **THEN** the meter is not rendered rather than dividing by zero

#### Scenario: The meter clamps a full window
- **WHEN** the used token count meets or exceeds the context window
- **THEN** the progress bar fills to 100% rather than overflowing

### Requirement: Context Usage Data over the Chat Stream
The chat streaming protocol SHALL surface the data the context meter requires. The `init` event SHALL include the active agent's resolved context-window size. The `turn` event SHALL include the turn's `prompt_tokens`, `completion_tokens`, and `total_tokens` in addition to the existing total. The persisted-turn payload served for conversation history SHALL include per-turn token counts and the agent's context window so the meter can render for loaded conversations.

#### Scenario: The init event carries the context window
- **WHEN** a chat stream begins
- **THEN** the `init` event includes the active agent's resolved context-window token size

#### Scenario: The turn event carries per-turn token usage
- **WHEN** a turn completes and its metadata is emitted
- **THEN** the `turn` event includes `prompt_tokens`, `completion_tokens`, and `total_tokens`

#### Scenario: History turns carry token usage
- **WHEN** the client loads a conversation's persisted turns
- **THEN** each turn exposes its token counts and the conversation exposes the agent's context window

### Requirement: Per-Message Timestamps
Each rendered chat message SHALL display its creation date and time. The optimistic user message SHALL be timestamped at submit time so its timestamp is visible immediately; messages loaded from history SHALL use the server-provided creation time. A message without a creation time (e.g. a streaming assistant message before reload) SHALL render no timestamp line rather than an empty or placeholder time.

#### Scenario: A user message shows the time it was sent
- **WHEN** the user submits a message
- **THEN** the message displays its send time immediately, derived from the client-set creation timestamp

#### Scenario: A history message shows its server time
- **WHEN** a message loaded from conversation history renders
- **THEN** it displays the server-provided creation date and time

#### Scenario: A streaming message shows no timestamp until reload
- **WHEN** an assistant message is actively streaming and has no creation time
- **THEN** no timestamp line is rendered for it

### Requirement: Token-Delta Streaming over the Chat Stream

When streaming is enabled, the chat streaming protocol SHALL deliver token-level assistant content
as it is generated. Each SSE `message` event SHALL carry one delta content block stamped with
`streaming_meta.index`, and the client SHALL merge delta blocks into coherent content blocks by
that index rather than treating each event as a whole message. The client SHALL accumulate
text deltas into the indexed block's text, reasoning deltas into the indexed block's reasoning,
and tool-call argument deltas into the indexed call's raw argument string. This merge SHALL work
uniformly across all provider kinds (OpenAI, Anthropic, Gemini, Ark, DeepSeek, Qwen) without
per-provider branching.

#### Scenario: Tokens appear as they are generated

- **WHEN** the web client receives streaming `message` events during a turn
- **THEN** assistant text appears progressively as tokens arrive, not only after the full message completes

#### Scenario: Delta fragments merge by index

- **WHEN** multiple `message` events carry delta blocks with the same `streaming_meta.index`
- **THEN** their text/argument fragments accumulate into a single content block rather than producing duplicate or fragmented blocks

#### Scenario: A new block is created on first sighting

- **WHEN** a delta block arrives with an index not yet present in the streaming assistant message
- **THEN** the client creates a new content block at that index, carrying the block's identity (e.g. a tool call's name and call id) for subsequent fragments to merge into

#### Scenario: Out-of-order block arrival is handled

- **WHEN** delta blocks arrive in an order other than strictly increasing index
- **THEN** each block is still routed to its correct index and merged correctly

### Requirement: Streaming Tool-Call Rendering

During streaming, a tool call's name SHALL render as soon as its item-added block arrives, before
its arguments complete. Tool-call arguments arrive as JSON fragments and SHALL be accumulated as a
raw string; renderers SHALL NOT attempt to parse incomplete argument JSON. Any renderer that parses
tool arguments SHALL degrade gracefully to the raw string or the tool name when the JSON is
incomplete or invalid. The persisted-message re-fetch after stream completion SHALL replace the
streamed assistant bubble with the authoritative merged message.

#### Scenario: A tool call's name renders before its arguments complete

- **WHEN** a tool-call block arrives during streaming
- **THEN** the tool name renders immediately, ahead of the (still-accumulating) arguments

#### Scenario: Incomplete argument JSON does not break rendering

- **WHEN** a renderer encounters tool-call argument JSON that is partial or invalid mid-stream
- **THEN** it falls back to the raw argument string or the tool name rather than throwing

#### Scenario: The streamed bubble re-syncs to persisted truth

- **WHEN** the stream completes
- **THEN** the streamed assistant message is replaced by the fetched persisted message so the final render is authoritative

### Requirement: Stop Control During Streaming
The composer SHALL surface a stop control that is visible and enabled only while the agent is streaming, and that cancels the in-flight stream when activated. The send control and the stop control SHALL be mutually exclusive by streaming state (send while idle, stop while streaming), rendered in the same composer action slot.

#### Scenario: A stop control appears while streaming
- **WHEN** the agent begins streaming a response
- **THEN** the composer action slot renders an enabled stop control in place of the send control

#### Scenario: The send control returns when streaming ends
- **WHEN** streaming ends for any reason (completion, stop, or error)
- **THEN** the composer action slot renders the send control again

#### Scenario: Activating stop cancels the stream
- **WHEN** the user activates the stop control during streaming
- **THEN** the in-flight chat stream is cancelled and no further streaming content is processed for that turn

### Requirement: Abort-Based Stream Cancellation
The chat runtime SHALL cancel an in-flight stream by aborting the underlying `/api/chat` request via an `AbortController` signal. Cancellation SHALL be signalled to the backend solely by closing the connection (which cancels the request context the agent runs under); no separate stop endpoint SHALL be introduced. A user-initiated abort SHALL be distinguished from a genuine network error so that it is treated as a normal end of the turn, not an error.

#### Scenario: Stop aborts the fetch, not a new request
- **WHEN** the user stops a running stream
- **THEN** the runtime aborts the active `/api/chat` fetch via its `AbortController` rather than issuing a second request

#### Scenario: Abort is not reported as an error
- **WHEN** the fetch is aborted by the stop control
- **THEN** no error toast is shown and the stream is treated as normally ended (the streaming flag is cleared)

#### Scenario: A real network error is still reported
- **WHEN** the fetch fails for a reason other than a user abort
- **THEN** the existing stream-error toast and error state are shown

### Requirement: Stopped-Partial Retention In Memory
When the user stops a stream, the partial assistant content already received SHALL remain visible in the transcript for the remainder of the session and SHALL be marked as stopped. The runtime SHALL NOT re-fetch conversation history from the server as a consequence of a stop (which would otherwise replace the in-memory partial with persisted history, dropping the partial because a stopped turn is not persisted). A stopped partial SHALL NOT be persisted to the conversation history.

#### Scenario: The partial stays visible after stop
- **WHEN** the user stops a stream mid-response
- **THEN** the assistant text received so far remains in the transcript, marked as stopped, without an immediate history re-fetch

#### Scenario: The stopped partial is visually marked
- **WHEN** a stopped assistant message renders
- **THEN** it is visually distinguished from completed messages via a "stopped" affordance

#### Scenario: A stopped turn is not persisted
- **WHEN** a turn is stopped and the conversation is later reloaded from the server
- **THEN** that stopped turn is absent from the persisted history (the partial existed only in the prior session's memory)

### Requirement: Compaction Boundary Marker
The system SHALL render a flagged summary turn as a compaction boundary marker — a divider indicating earlier conversation was summarized, with the summary text available in a collapsible region — and SHALL NOT render it as a normal assistant message. Every flagged summary row SHALL render as its own marker, so re-compaction shows the full compaction history. Non-summary messages SHALL remain fully visible; append-only retention is reflected in the transcript unchanged.

#### Scenario: A summary renders as a marker, not a bubble
- **WHEN** a conversation contains a flagged summary turn between older and newer messages
- **THEN** the summary renders as a divider marker with collapsible summary text
- **AND** it does not render as a flat assistant message

#### Scenario: Older messages remain visible across a compaction
- **WHEN** a compaction marker is rendered
- **THEN** all non-summary messages before and after it remain fully visible

#### Scenario: Re-compaction shows multiple markers
- **WHEN** a conversation has been compacted more than once
- **THEN** each flagged summary renders as its own marker in sequence order

### Requirement: Context Meter Annotates Compaction
The context-window usage meter SHALL display a one-time compaction annotation when the conversation's `compaction_count` increases, so the post-compaction drop in `used` reads as a compaction event rather than a glitch. The meter SHALL continue to display true context fill (the most recent turn's `prompt_tokens`); the annotation explains the drop, it does not redefine the metric. The meter's `used` source SHALL NOT be anchored on a summary row.

#### Scenario: The meter annotates a compaction
- **WHEN** a turn completes and `compaction_count` has increased since the prior turn
- **THEN** the meter shows a one-time compaction annotation alongside the dropped `used` value

#### Scenario: The meter does not anchor on a summary row
- **WHEN** the last persisted row is a summary row (e.g. a turn that did not complete)
- **THEN** the meter's `used` source skips the summary row rather than reading zero

