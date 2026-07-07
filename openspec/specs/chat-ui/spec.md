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
The composer input SHALL be a multi-line text area. Pressing `Enter` SHALL submit the message; pressing `Shift+Enter` SHALL insert a newline without submitting.

#### Scenario: Shift+Enter inserts a newline
- **WHEN** the user presses `Shift+Enter` in the composer
- **THEN** a newline is inserted into the input and the message is not submitted

#### Scenario: Enter submits
- **WHEN** the user presses `Enter` (without Shift) on a non-empty input while not streaming
- **THEN** the message is submitted

#### Scenario: Textarea grows with content
- **WHEN** the input grows beyond one line
- **THEN** the textarea expands up to a bounded maximum height and then scrolls internally, and resets its height after submit

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

