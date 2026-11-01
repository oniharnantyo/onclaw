# Spec Delta

## ADDED Requirements

### Requirement: Drag-and-drop attachment surface

Dragging files over the chat conversation surface (the transcript and composer column) SHALL surface a drag-over affordance — a highlight on the composer and a drop hint — that appears when a file drag enters the column and clears when it leaves or the drop completes. Dropping files onto the column SHALL attach them through the same acceptance limits, chip tray, upload gating, and degradation warnings as the attach button and paste, with each dropped file judged independently: one rejected file MUST NOT reject the others in the same drop. Drops on surfaces outside the chat column, and drags that carry no files (e.g. selected text), MUST attach nothing and MUST NOT navigate the application away from the chat — the app SHALL stay on the current view for every drop, inside or outside the column. Dropping text into the composer SHALL continue to insert text as it does today.

#### Scenario: Drop a file on the chat column

- **WHEN** the user drops a file within the chat column
- **THEN** a chip mints and uploads exactly as if the file had been picked, and the transcript and composer stay on the current session

#### Scenario: Drop multiple files with one rejected

- **WHEN** the user drops several files at once and one of them exceeds its size cap
- **THEN** the oversized file's chip shows a rejected state with the size-limit reason while the other files in the same drop attach and upload normally

#### Scenario: Drag-over affordance appears and clears

- **WHEN** the user drags a file over the chat column and then either moves out of it or drops
- **THEN** the affordance shows while the drag is over the column and is gone after leaving or dropping

#### Scenario: Drop outside the chat column never navigates

- **WHEN** the user drops a file on a surface outside the chat column (for example the right bar)
- **THEN** nothing attaches and the application remains on the chat view

#### Scenario: Non-file drag stays inert

- **WHEN** the user drags selected text (no files) over the chat column
- **THEN** no drag-over affordance shows and no attachment is created

#### Scenario: Text drop still inserts into the composer

- **WHEN** the user drops selected text onto the composer input
- **THEN** the text is inserted into the draft as before, and no attachment is created
