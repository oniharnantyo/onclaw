# Spec Delta

## MODIFIED Requirements

### Requirement: Chat surfaces
In direct-chat and channel transcripts, `document.search` and `document.read` calls SHALL render as tool cards with verb, target, and latency. Agent citations of a reference document SHALL render as clickable chips carrying the document name and locator (page/slide/sheet/heading) that open the document preview in the right panel. The composer toolbar SHALL offer a documents affordance that opens the right panel's Documents listing — listing exactly the conversation-visible documents (direct chats resolving by agent attachments plus promoted, channels by channel attachments plus promoted) — replacing the former floating popover. The panel Documents listing SHALL offer preview on row click and an insert-as-mention action per row. Inserting a document mention SHALL add a pill to the composer text and attach a pointer note (document identity only, no content) to the turn. On surfaces without a right-panel host — workspace settings and the agent-config dialog — opening a document preview SHALL render the document source in a standalone modal rather than routing through the panel store.

#### Scenario: Citation chip opens the page
- **WHEN** an agent answers citing `twilio-api.pdf` page 31 and the user clicks the citation chip
- **THEN** the right panel opens the document preview positioned at that document

#### Scenario: Mention carries identity, not content
- **WHEN** a user inserts `integration-notes.md` as a mention from the panel Documents listing and sends "compare this with the official limits"
- **THEN** the turn carries a pointer note naming the document, and the agent uses its search and read tools to consult it

#### Scenario: Panel listing matches the run's visibility
- **WHEN** a user opens the Documents listing from the composer affordance in a channel where only one document is channel-attached
- **THEN** the listing shows that document plus promoted documents, and never a document the run could not read

#### Scenario: Preview works outside the chat route
- **WHEN** a user clicks a document preview control in workspace settings or the agent-config dialog
- **THEN** the document source renders in a modal on the current page, without navigating to chat
