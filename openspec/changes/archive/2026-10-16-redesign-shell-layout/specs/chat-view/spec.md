# Spec Delta

## Purpose

Promote the session layer out of the chat surface into its own Sessions
island column with a search filter, matching the preview-validated three-
column workbench (rail → Sessions island → conversation).

## MODIFIED Requirements

### Requirement: Sessions organized per agent

The chat menu SHALL list agents and, for each agent, its sessions with latest
activity. The session layer SHALL render as its own island column between the
navigation rail and the conversation, with a header row ("Sessions" plus a
new-session button) and session entries showing the session title and one
meta line (relative time and, when known, the model). The active session
SHALL be highlighted with the primary tint. The user SHALL be able to start a
new session for an agent and switch between sessions without losing history.
The island SHALL be hidden while Settings is open.

#### Scenario: Start a new session

- **WHEN** the user selects an agent and creates a new session from the
  Sessions island header
- **THEN** an empty chat view opens for that agent and the session appears in
  the agent's session list

#### Scenario: Switch sessions

- **WHEN** the user switches from one session to another of the same agent
- **THEN** the transcript of the selected session is displayed and the
  selected entry carries the primary-tint active state

#### Scenario: Sessions island reads as its own island

- **WHEN** the chat view is open on desktop
- **THEN** the session layer renders as a separate rounded island column with
  its own header, distinct from both the navigation rail and the conversation
  surface

## ADDED Requirements

### Requirement: Session search filter

The Sessions island SHALL provide a search input that filters the visible
session entries by title as the user types, and clearing the input SHALL
restore the full list. Filtering SHALL NOT change which session is active or
reload history.

#### Scenario: Filter sessions by title

- **WHEN** the user types text into the Sessions island search input
- **THEN** only sessions whose title matches the text are listed

#### Scenario: Clearing the filter

- **WHEN** the user clears the search input
- **THEN** the full session list is restored with the previously active
  session still selected
