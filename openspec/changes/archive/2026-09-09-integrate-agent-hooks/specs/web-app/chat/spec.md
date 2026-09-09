## ADDED Requirements

### Requirement: Hook enforcement rendering
The transcript SHALL render hook enforcement where it occurs: a tool call prevented by a hook SHALL render as a tool card marked blocked, showing the hook's reason in place of a result; a prompt prevented by a hook SHALL render as a notice entry carrying the reason in place of an assistant reply. Both renderings SHALL persist across reloads, hydrated from the same history the live stream wrote.

#### Scenario: Blocked tool call in the transcript
- **WHEN** a pre-tool hook blocks the agent's shell call during a live chat
- **THEN** the tool card shows a blocked state with the hook's reason, and the conversation continues from the model's reaction to the block

#### Scenario: Blocked prompt after reload
- **WHEN** a turn whose submitted prompt was blocked by a hook is viewed after a page reload
- **THEN** the transcript shows the notice entry with the reason, and no spinner or empty assistant bubble appears
