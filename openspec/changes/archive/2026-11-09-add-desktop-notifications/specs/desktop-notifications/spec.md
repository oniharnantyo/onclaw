# desktop-notifications delta

## Purpose

Deliver OS-native desktop notifications so the user learns about finished agent turns, agents blocked waiting on them, schedule outcomes, and group task results without keeping the window open — with per-category settings and suppression when the user is already looking at the relevant conversation.

## ADDED Requirements

### Requirement: Notification trigger categories

The system SHALL emit desktop notifications for four trigger categories, each individually controllable in settings:

1. **Turns** — when a user-originated turn on a non-group session reaches a terminal outcome (completed, failed, or cancelled).
2. **Needs attention** — when a session parks waiting on the user: a shell approval request or an input request (browser sign-in / takeover).
3. **Schedules** — when a scheduled run completes (with its outcome and a short excerpt), and when a schedule misses or fails to fire.
4. **Groups** — when a group task reaches a terminal state (completed, failed, or paused).

Turns originating from the Telegram channel SHALL NOT produce desktop notifications (the channel delivers results in-chat). Individual group member turns SHALL NOT produce turn notifications; group activity is reported at task level by the Groups category. A notification SHALL name the agent, schedule, or group it concerns, and SHALL include a short excerpt or error detail where the trigger source provides one.

#### Scenario: Turn completes while window hidden

- **WHEN** a user-originated turn completes on a non-group session while the main window is hidden to the tray
- **THEN** a desktop notification appears naming the agent, and no notification appears for group member turns or Telegram-origin turns

#### Scenario: Approval parks a session

- **WHEN** a session parks on a shell approval request while the user is not viewing that session
- **THEN** a needs-attention notification appears identifying the session, and the session remains parked exactly as without notifications

#### Scenario: Scheduled run finishes

- **WHEN** a scheduled run completes with outcome failed and the Schedules category is enabled
- **THEN** a notification appears carrying the schedule's name, the failed outcome, and the run excerpt

#### Scenario: Group task terminal

- **WHEN** a group task reaches its completed state
- **THEN** a notification appears naming the group and the task outcome

### Requirement: Foreground suppression

The system SHALL suppress a candidate notification when the user is already looking at the conversation it concerns: specifically when the main window is visible AND the frontend's currently viewed context (session or group) is the one the notification is about. Suppression applies per trigger candidate; parked-attention and schedule-miss triggers follow the same rule. When the window is hidden, minimized to the tray, or viewing a different session or group, the notification SHALL be delivered.

#### Scenario: User watching the session

- **WHEN** a turn completes on a session the user is currently viewing in a visible window
- **THEN** no desktop notification is delivered

#### Scenario: User viewing another session

- **WHEN** a turn completes on session A while the user views session B in a visible window
- **THEN** the notification for session A is delivered

#### Scenario: Window hidden

- **WHEN** any enabled trigger fires while the main window is hidden
- **THEN** the notification is delivered regardless of which context was last viewed

### Requirement: Notification settings

The system SHALL persist notification preferences as a singleton: a master enabled flag and one flag per trigger category (turns, needs attention, schedules, groups), stored in the SQLite database. Absence of a saved row SHALL mean all flags default to enabled. A disabled category SHALL produce no notifications; the master flag disabled SHALL produce none at all. Preference changes SHALL take effect immediately without an app restart. Needs-attention and other notifications MUST NOT be delivered while the master flag is off, even if category flags are on.

#### Scenario: Fresh install defaults

- **WHEN** the app runs with no notification settings row saved
- **THEN** the master flag and all four category flags are enabled

#### Scenario: Category disabled

- **WHEN** the user disables the Schedules category and a scheduled run completes
- **THEN** no desktop notification is delivered for that run, and turn notifications continue to work

#### Scenario: Live change

- **WHEN** the user toggles the master flag off while the app is running
- **THEN** subsequent triggers produce no notifications without restarting the app

### Requirement: Notification click opens the conversation

Activating a desktop notification SHALL make the main window visible (if hidden) and navigate the frontend to the conversation the notification concerns — the session for turn and attention notifications, the group for group notifications, or the schedule's session for schedule notifications. Clicking a notification MUST NOT lose the frontend's prior state beyond the navigation itself.

#### Scenario: Click from hidden window

- **WHEN** the user clicks a turn notification while the window is hidden
- **THEN** the window becomes visible and the frontend shows that session's conversation

#### Scenario: Click on group notification

- **WHEN** the user clicks a group task notification
- **THEN** the window becomes visible and the frontend shows that group's view

### Requirement: Host parity for notifications

The desktop host SHALL deliver notifications through the operating system's native notification facility on every shipped platform (macOS, Windows, Linux), grouping notifications per conversation so bursts stack instead of flooding. The server host SHALL run the same services with notifications as a silent no-op: enabling, disabling, and reporting focus MUST work over the server API without error, and no notification surface is rendered.

#### Scenario: Server mode accepts settings calls

- **WHEN** the frontend served by the server host reads and saves notification settings and reports focus
- **THEN** the calls succeed and no error reaches the caller, and no desktop notification appears

#### Scenario: Bursts stack

- **WHEN** several turns on the same session complete in quick succession
- **THEN** the notifications group under that conversation rather than appearing as unrelated alerts

### Requirement: Platform authorization handling

On platforms where the OS requires explicit authorization to deliver notifications, the system SHALL request authorization lazily when the first notification would be delivered, rather than at app startup. If the user denies authorization, the app MUST continue to operate normally: notifications are silently not delivered, and no error is surfaced to the user's chat or turned into a failed turn.

#### Scenario: First notification requests authorization

- **WHEN** the first qualifying trigger fires on a platform that requires authorization
- **THEN** the system requests notification authorization and delivers the notification if granted

#### Scenario: Authorization denied

- **WHEN** the user denies the OS notification authorization
- **THEN** the app continues running normally, turns and schedules behave identically, and no error is reported in the chat or logs as a failure of the turn
