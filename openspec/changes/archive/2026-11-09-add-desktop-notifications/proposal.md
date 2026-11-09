# Proposal

## Why

buddies is a background gateway: closing the window hides it to the tray while agents, schedules, and groups keep running. Today nothing surfaces results to the person away from the window — a finished run, a failed schedule, or an agent parked waiting on a shell approval or a browser sign-in sits invisible until the user happens to reopen the window. OS-native desktop notifications close that gap, and a Notification settings pane lets the user control which of those interruptions exist.

## What Changes

- Add OS-native desktop notifications driven by Wails v3's `pkg/services/notifications` (macOS UNUserNotificationCenter, Windows Toast, Linux D-Bus) — no new dependencies, no per-platform hand-rolling.
- Add a notification decision engine (new `internal/notifications` package) that subscribes to four trigger sources and gates each candidate through the user's category settings and a foreground-suppression check (window visible AND that exact session/group on screen ⇒ stay silent).
- Trigger taxonomy — four categories:
  - **Turns** — user-originated turn terminals (completed, failed, cancelled) for non-group sessions.
  - **Needs attention** — approval-required and input-required parks (shell approval gate, browser sign-in/takeover): the session blocks indefinitely.
  - **Schedules** — scheduled-run completions (outcome + excerpt, via the existing `RunCompletionAnnouncer` seam) and missed/failed schedule fires.
  - **Groups** — group task terminal states (completed, failed, paused).
  - Telegram-origin turns stay out: the Telegram channel already delivers results in-chat. Group member turns are covered by the Groups category at task level, not per-turn.
- Add click-to-open: activating a notification shows the main window (if hidden) and navigates the frontend to the originating session or group.
- Add a focus mirror: the frontend reports the currently viewed session/group to the Go side so suppression can distinguish "user is watching this exact conversation" from "user is elsewhere or away".
- Add a `notification_settings` singleton in SQLite (master + four category toggles) with defaults on, and a **Notifications** pane in Settings under a new APP section to edit them.
- Server mode compiles unchanged with a no-op notifier: the same headless host has no OS notification surface (it keeps the Telegram announcer for schedule outcomes).

## Capabilities

### New Capabilities
- `desktop-notifications`: OS-native desktop notifications for agent turns, needs-attention parks, schedule outcomes, and group task terminals — with category settings, foreground suppression, and click-to-open navigation.

### Modified Capabilities
- `settings`: the settings menu gains a Notifications pane (under a new APP section) presenting the master toggle and the four category toggles.
- `data-store`: the SQLite storage enumeration gains the `notification_settings` singleton (master + category toggles, schema migration).

## Impact

- **New Go package** `internal/notifications` (engine: settings cache, focus mirror, gating, payload building) plus small additive seams: a turn-finished manager option carrying outcome/origin, a session-parked manager option for approval/input parks, a desktop announcer beside the Telegram one, and a `NativeNotifier` host seam (Wails-backed on desktop, no-op in server mode) following the `wailsEmitter`/`wailsURLOpener` pattern in `main.go`.
- **Store**: migration `000022_notification_settings.sql`; the pinned latest-version test (`db_test.go`) bumps 21 → 22.
- **Bindings**: a new bound notifications service (get/save settings, report focus) and one new Go→frontend event (open deep-link); regenerate Wails bindings; hand-maintained mirrors in `frontend/src/lib/events.ts` and `client.ts` updated.
- **Frontend**: new Notifications pane (settings registry: new APP section), focus reporting hook on chat/group navigation, deep-link handler for notification clicks.
- **Platform notes**: macOS authorization is requested lazily at first send (bundled+signed app — the existing `.app` packaging satisfies this); click-to-open uses the notification response callback on all three platforms.
