# openresponses — Delta

## MODIFIED Requirements

### Requirement: Usage reporting
Response objects (aggregated and terminal stream events) SHALL report token usage — input, output, and total — captured for the executed turn, plus the turn's final-call input tokens (the input count of the turn's last model call). The usage block MAY carry an optional `context_breakdown` object with labeled segment counts (`instructions`, `tools`, `conversation`, `files`, `server`) describing display-grade estimates of where the final call's input went; the field is additive and clients that ignore it remain fully functional. A turn whose provider reported no usage SHALL omit the `usage` block entirely rather than report zeros, and a breakdown SHALL NOT appear without a usage block.

#### Scenario: Usage on completed response
- **WHEN** a turn completes
- **THEN** the Response object carries `usage` with the turn's input/output/total token counts and the turn's final-call input token count

#### Scenario: Terminal events without a final answer still carry usage
- **WHEN** a turn ends as `response.incomplete` or `response.failed` after model calls were made
- **THEN** the Response object carries `usage` with the counts captured up to the terminal event

#### Scenario: No provider usage omits the block
- **WHEN** a turn's provider reports no usage for any of its model calls
- **THEN** the Response object carries no `usage` block

#### Scenario: Optional breakdown is additive
- **WHEN** a turn's usage carries a context breakdown, and a client reads only the legacy usage fields
- **THEN** the client behaves exactly as before, and the breakdown never appears without a usage block
