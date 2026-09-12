## MODIFIED Requirements

### Requirement: Per-agent session lists
Each agent chat SHALL support multiple named sessions. The sidebar SHALL list sessions for the active agent chat with controls to start a new session, switch sessions, and delete one. Deleting the last session SHALL spawn a fresh empty session, so a chat never has zero sessions. The session list SHALL be sourced from the server's per-agent session index — scoped to the signed-in user, ordered by last activity (newest first) so continuing an old session moves it to the top — with the browser-local cache serving only as the immediate paint before the server responds and as history for sessions that predate the index. When the user sends a message in a new session, the sidebar SHALL title it immediately from the typed text using the same rule the server applies (first line, trimmed, truncated with an ellipsis), so no blank or stale title is ever shown while the server row is being created. The list SHALL refresh without a manual reload when the user's own turn completes, when the window regains focus or visibility, and when another tab of the same app persists chat state (storage event); a session whose stored title is empty SHALL display the "New chat" fallback. Deleting a session SHALL soft-delete it server-side and remove it locally; if the deleted session was active, an adjacent session SHALL become active, falling back to a fresh empty session when none remains.

#### Scenario: Session lifecycle
- **WHEN** the user deletes the only session of an agent chat
- **THEN** a new empty session titled "New chat" becomes active and the server row is soft-deleted

#### Scenario: List survives a fresh browser
- **WHEN** the user opens the workspace in a browser with no local cache (e.g. incognito)
- **THEN** the sidebar lists the user's server-indexed sessions for the agent, newest activity first, and selecting one hydrates its transcript from the session events endpoint

#### Scenario: Rechat bumps an old session to the top
- **WHEN** the user sends a message in a session that was not the most recently active
- **THEN** that session moves to the top of the sidebar list after the turn's activity is applied

#### Scenario: Optimistic title matches the server
- **WHEN** the user sends the first message of a new session
- **THEN** the sidebar shows the input-derived title immediately and the server-confirmed title from the refreshed list agrees with it

#### Scenario: Refresh without reload
- **WHEN** the user's own turn finishes, the tab regains focus, or another tab writes chat state
- **THEN** the sidebar refetches the session list and applies new, retitled, reordered, or newly running entries without a page reload

## ADDED Requirements

### Requirement: Session running indicator
A sidebar session row whose session has a live run SHALL show a leading spinner before the title AND pulse the title (opacity animation) while the run executes; idle rows SHALL carry no running marker at all. The indicator SHALL merge two sources — the app's own run state for the session (instant, no round-trip) and the server list's running flag (runs live in other tabs, devices, or origins) — so exactly one row animates per running session. The pulse SHALL stop when the turn reaches a terminal state locally or when a refreshed list no longer reports the session as running.

#### Scenario: Own run pulses instantly
- **WHEN** the user sends a message in a session
- **THEN** that session's title starts pulsing and its leading spinner starts immediately, before any server confirmation

#### Scenario: Foreign run pulses on refresh
- **WHEN** a run is live on one of the user's sessions from another tab or device and the list refreshes
- **THEN** that session's title pulses and shows the leading spinner even though this tab never started it

#### Scenario: Pulse ends with the run
- **WHEN** the run finishes (or is cancelled) and the state source updates
- **THEN** the title stops pulsing and the spinner disappears without a page reload
