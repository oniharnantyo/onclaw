## ADDED Requirements

### Requirement: User memory editor in the user menu
The user info menu SHALL include an entry opening the signed-in member's own `USER.md` memory editor for the active workspace: a modal with a free-form textarea (memory is unstructured markdown), a live size/token counter fed by the server cap, and save via the user memory endpoint. Saving past the cap SHALL surface the 422 field error inline; successful save SHALL confirm. Other members' memories are never reachable from this surface.

#### Scenario: Member opens and edits own memory
- **WHEN** a member opens the user menu, chooses the memory entry, edits the textarea, and saves
- **THEN** the content persists via the user memory endpoint and the editor reflects the saved state

#### Scenario: Over-cap save shows field error
- **WHEN** the member saves content past the size cap
- **THEN** the 422 error is shown inline and the modal stays open with the content intact
