## ADDED Requirements

### Requirement: Right panel shell
The chat surface SHALL offer a right panel — a tabbed side surface docked at the transcript's right edge on wide viewports and presented as a full-height overlay sheet below the wide breakpoint — toggled from the chat header's top-right control. In direct agent chats the toggle SHALL occupy the slot of the agent-configure button: the configure button SHALL be removed from the chat header (agent configuration remains reachable from the Agents screen), and the show/hide panel toggle renders in its place. The panel hosts one tab per opened artifact, each tab rendering its content through the source registered for that tab's kind. Opening an artifact that already has a tab SHALL focus that tab instead of duplicating it. Closing the last tab SHALL close the panel. Switching to a different chat SHALL reset the panel's tabs; tabs from the previous chat SHALL NOT carry over. The panel SHALL preserve the design contract's tokens, radius, and motion, and SHALL NOT introduce horizontal overflow at any supported viewport width.

#### Scenario: Open from the header
- **WHEN** the user activates the panel toggle in the chat header
- **THEN** the right panel opens docked beside the transcript on a wide viewport, or as an overlay sheet with a dismissal backdrop below the wide breakpoint

#### Scenario: Toggle replaces the agent settings button
- **WHEN** a direct agent chat renders its header
- **THEN** the top-right control is the panel show/hide toggle and no agent-configure button renders in the header

#### Scenario: Duplicate open focuses the existing tab
- **WHEN** the user opens a file that already has a panel tab
- **THEN** that tab becomes the active tab and no second tab is created

#### Scenario: Last tab closed closes the panel
- **WHEN** the user closes the only open tab
- **THEN** the panel closes

#### Scenario: Chat switch resets tabs
- **WHEN** the user navigates from one chat to another
- **THEN** the panel in the new chat starts with no tabs from the previous chat

### Requirement: Tool-card panel affordances
A transcript tool call whose producing tool has a registered panel candidate SHALL render an explicit open affordance on its tool card — on live turns and hydrated history alike, since both derive from the same transcript tool-card data. Activating the affordance SHALL open the panel (or focus the artifact's existing tab) with that artifact. When the panel is closed and a tool finishes producing a panel-able artifact, the panel toggle SHALL show a dot badge; activating the toggle SHALL clear the badge. The panel SHALL NEVER open itself, animate itself open, or interrupt the transcript in response to a tool event — the only openers are the user's clicks.

#### Scenario: Open from a live tool card
- **WHEN** an agent's live turn finishes a screenshot tool call and the user activates the card's open affordance
- **THEN** the panel opens with the browser-mirror tab for that session focused

#### Scenario: Open from a hydrated tool card
- **WHEN** the user opens an old conversation and activates the open affordance on a historical document tool card
- **THEN** the panel opens with that document's file tab focused, exactly as a live card would

#### Scenario: Badge on finished artifact while closed
- **WHEN** a panel-able tool finishes while the panel is closed
- **THEN** the panel toggle shows a dot badge without opening the panel, and the badge clears the next time the panel opens

#### Scenario: Panel never self-opens
- **WHEN** any tool finishes, regardless of artifact type
- **THEN** the panel's open state, tab set, and active tab change only through user activation

### Requirement: File source
The panel's file source SHALL fetch a file's bytes through the workspace-files API and render by content: markdown through a pre-render pass that lifts frontmatter into a meta chip, rewrites relative asset references to resolve against the file's own directory through the same API, and renders the body with the same markdown, code-highlight, and fence-card pipeline the transcript uses; code files with syntax highlighting; PDF and images inline; CSV as a table; spreadsheets and unrecognized binary types as a captioned degrade card with a download affordance. While loading the tab SHALL show a loading state; a file that can no longer be fetched SHALL show an explicit not-found state — never a blank pane.

#### Scenario: Markdown renders with its assets
- **WHEN** the user opens a markdown file that references `assets/chart.png` relative to itself and the file's directory contains that image
- **THEN** the panel renders the markdown body with the image resolved through the workspace-files API and charts, diagrams, and code fences rendered as the transcript renders them

#### Scenario: Frontmatter becomes a meta chip
- **WHEN** an opened markdown file begins with YAML frontmatter
- **THEN** the frontmatter renders as a compact meta chip above the body, not as a broken table or raw text

#### Scenario: Spreadsheet degrades with a download
- **WHEN** the user opens an `.xlsx` file
- **THEN** the panel shows a captioned degrade card naming the type with a download affordance, never a blank pane or raw bytes

#### Scenario: File no longer exists
- **WHEN** a file tab's file can no longer be fetched
- **THEN** the tab shows an explicit not-found state naming the file

### Requirement: Browser mirror source
The panel's browser source SHALL mirror an agent's browser session — one tab per agent and session pair — showing the latest page screenshot, the current page address, and a feed of that session's browser tool calls drawn from the transcript. The screenshot SHALL carry an honest staleness caption stating when it was captured and how many browser actions have happened since; the caption SHALL make a stale mirror visible, never leave it looking live. The source SHALL present four states: idle (no browser session yet), mirroring (session active), frozen (the run finished — last screenshot retained), and closed (the session was torn down). The mirror refreshes from transcript data; it SHALL NOT open a live remote-browser channel.

#### Scenario: Mirror shows the session's latest capture
- **WHEN** an agent's live run captures a page screenshot
- **THEN** the browser tab for that session shows the new screenshot with its capture time and the running count of actions since

#### Scenario: Staleness stays visible
- **WHEN** the agent performs browser actions after the last screenshot
- **THEN** the caption names how many actions have happened since the capture

#### Scenario: Run finish freezes the mirror
- **WHEN** the run that owned the browser session finishes
- **THEN** the tab retains the last screenshot in a frozen state, distinguished from a live session

#### Scenario: Idle before first browser use
- **WHEN** the user opens a browser tab in a chat whose agent has not used the browser
- **THEN** the tab shows the idle state explaining the mirror fills when the agent browses

### Requirement: Members source
In a channel, the right panel SHALL offer the channel members surface — the member list with add and remove for eligible members — rendered as a panel source under the panel's tab mechanics. Outside channels no members tab SHALL be offered. The members surface's behavior (listing, presence and kind markers, add/remove eligibility) SHALL match the channel members behavior otherwise.

#### Scenario: Channel members render as a source
- **WHEN** the user opens the panel in a channel and activates the members tab
- **THEN** the panel shows the channel's member list with the same add and remove controls the standalone members panel had

#### Scenario: No members tab outside channels
- **WHEN** the user opens the panel in a direct agent chat
- **THEN** no members tab is offered
