# Design

## Context

Wails v3.1.0 (already the project's dependency) ships `pkg/services/notifications` — a cross-platform notification service (macOS UNUserNotificationCenter with cgo and purego variants, Windows Toast via wintoast, Linux D-Bus `org.freedesktop.Notifications`) with authorization requests, ThreadID grouping, and a click-response callback. buddies runs the desktop host from a packaged `.app` bundle (`bin/buddies.dev.app` via `prepare-macos-output`), which is what macOS requires; the headless server build (`//go:build server`) has no native surface and already delegates schedule-run announcements to Telegram.

Every trigger source already has a seam (file:line at exploration time):

| Source | Seam today |
|---|---|
| Turn terminals | `manager.finishTurn` (`internal/agents/manager_impl.go`) calls `notifyTransition(sessionID, false)`; the `WithTurnTransition` option (`manager.go`) carries only `busy bool` — outcome and origin are lost |
| Approval / input parks | `parkForApproval` / `installPendingInput` record `KindApprovalRequired` / `KindInputRequired` session events; parking fires no transition |
| Scheduled-run completion | `RunCompletionAnnouncer` seam (`internal/cli/schedule_runs.go`) with `outcomeCompleted/Cancelled/Failed` + ≤400-char excerpt; sole implementation is Telegram's `AnnounceRunCompletion` |
| Schedule fire/miss/fail | `scheduler.Service` `Subscribe`/`notify` chain (`internal/scheduler/service.go`) |
| Group task terminals | `groups.handleTurnEnd` → `fireGroupEvent` → `EventGroupsChanged` / per-group events |

Composition root: `internal/cli` `Assemble` receives host seams as parameters (`services.Emitter`, `services.URLOpener`) and desktop `main.go` injects the Wails-backed implementations (`wailsEmitter`, `wailsURLOpener`); server `main_server.go` injects its own. Settings singletons follow the `GatewaySettings` pattern (store sub-interface + `Get`-nil-means-defaults). The settings frontend registry (`frontend/src/features/settings/index.tsx`) has sections MODELS / INTEGRATIONS / AUTOMATION; panes are components wired through `paneComponents` (representative pane: `HooksPane.tsx`). The latest schema migration is `000021`; `db_test.go` pins the applied version.

## Goals / Non-Goals

**Goals:**
- One decision engine that owns gating (settings × foreground) and payload building, with the thinnest possible additive seams in existing packages.
- Uniform foreground suppression: silence only when the window is visible AND the exact conversation is on screen.
- Server build compiles and behaves identically except notifications are no-ops.
- Click-to-open deep links reusing the window-show + event-navigate pattern the native menu already uses for `settings:open`.

**Non-Goals:**
- Notification action buttons / reply fields (categories with actions) — click-to-open only in v1.
- Per-agent or per-schedule notification overrides; the four category toggles are the granularity.
- In-app notification center / history; notifications are ephemeral OS surfaces.
- Telegram→desktop or desktop→Telegram cross-delivery changes; the Telegram announcer is untouched.
- Windows/Linux packaging changes (AUMID/app-id defaults from the existing bundle config are assumed adequate; verified at smoke time).

## Decisions

### D1 — New `internal/notifications` package owns decisions; hosts own delivery

A platform-neutral `notifications.Engine` holds: the settings cache, the focus mirror, the native seam, and name resolvers. Trigger sources call `engine.Enqueue(candidate)`; the engine checks category/master flags, applies foreground suppression, builds the payload, and calls the seam. Delivery impls:

- Desktop (`main.go`): `wailsNotifier` wraps `notifications.New()` registered via `application.NewService`, following the `wailsURLOpener` precedent (app handle installed right after `application.New`).
- Server (`main_server.go`): no-op notifier; settings and focus calls still function.

Why not put the engine in `internal/services`: it needs no bridge surface itself, and keeping it out of the bound service keeps the bridge API at three methods (D5). Why a seam rather than importing Wails from the engine: the server build must not link native notification code, mirroring how `URLOpener` isolates `Browser.OpenURL`.

### D2 — Additive manager options for turn and park events; category/source matrix prevents duplicates

Two new manager options, both fired from points that already hold all needed facts:

- `WithTurnFinished(cb func(sessionID string, origin hooks.Origin, outcome TurnOutcome, turnID string))` — fired in `finishTurn` after the busy record clears, derived from the terminal error (`nil` → completed, `ErrTurnCancelled` → cancelled, other → failed). Fired ONLY for non-group-lane turns (`groupLaneTurnDone` already marks those at the same site), so group turns never double-report.
- `WithSessionParked(cb func(sessionID string, kind ParkKind, detail string))` — fired from `parkForApproval` (detail = command) and `installPendingInput` (detail = prompt/method); parks fire no transition today, so without this hook the strongest trigger would be invisible.

Dedup matrix (enforced in the engine, sourced from `origin` + session kind):

| Origin | Reported by |
|---|---|
| `OriginUser` on a non-group session | Turns category |
| `OriginScheduler` | Schedules category (via the announcer seam, not the turn hook — the engine ignores scheduler-origin turns from `WithTurnFinished`) |
| `OriginTelegram` | nothing (channel delivers in-chat) |
| group member turns | Groups category at task terminal only |

Schedule misses/failed fires flow from the scheduler's existing notify chain into the engine at the composition root. Alternatives considered: widening `WithTurnTransition`'s signature (breaks the existing option's contract and its group-rail design reference) and tapping the session event stream globally (ChatService forwards only frontend-subscribed sessions — background turns would be missed).

### D3 — Foreground suppression: Go owns window visibility, frontend owns context

- Window visibility: the desktop host checks its main window handle at enqueue time (native query, no caching).
- Viewed context: the frontend reports `{kind: "session"|"group", id}` whenever the active chat/group view changes, via a bound method (`ReportFocus`); the engine keeps the last report atomically. No report yet ⇒ treat as "nothing on screen" ⇒ deliver (suppression requires positive knowledge).

Why a mirror instead of the Go side asking the frontend synchronously: Wails bridge calls from Go into JS are async and would race the enqueue path; a push-on-change mirror is eventual-but-fresh (updates exactly at navigation time, read at trigger time). Why not skip suppression for v1: the foreground case is the default way people use the chat view; without the gate the feature is noise.

### D4 — Settings: `notification_settings` singleton, defaults on, live-applied

Migration `000022_notification_settings.sql` adds the singleton table (`enabled`, `turns`, `attention`, `schedules`, `groups`, `updated_at`), following `gateway_settings`. Store sub-interface `NotificationSettingsStore` (`Get` returning nil ⇒ defaults all-on, `Save`). `SettingsService` gains get/save methods (bound, validated). The engine refreshes its cache on save (the save path notifies the engine via a callback registered at composition root), so changes apply immediately. The pinned latest-version test bumps 21 → 22. Alternatives: key–JSON row like per-tool settings (rejected — the GatewaySettings singleton pattern is the established shape for scalar preference sets and gives typed columns for free).

### D5 — Bridge surface: one small bound service, one new event

`NotificationsService` (bound in both hosts): `GetNotificationSettings`, `SaveNotificationSettings`, `ReportFocus(kind, id)`. Go→frontend: `EventNotificationOpen = "notifications:open"` with payload `{kind, id}` — registered in `main.go` `init()` (`application.RegisterEvent`), mirrored by hand in `frontend/src/lib/events.ts`, wrapped in `frontend/src/lib/client.ts`. The frontend also regenerates bindings (`task common:generate:bindings`). Click flow: Wails `OnNotificationResponse` → desktop notifier → engine maps the notification ID back to its route → host shows the main window (same call the tray's `OnClick` uses) → `emitter.Emit(EventNotificationOpen, …)` → frontend navigates. Deterministic notification IDs (`turn:<sessionID>:<turnID>`, `park:<sessionID>:<interruptID>`, `sched:<scheduleID>:<runID>`, `group:<groupID>:<taskID>`) make the click route a lookup, not a heuristic.

### D6 — macOS authorization is lazy, denial is silent

The desktop notifier lazily calls `RequestNotificationAuthorization()` once, guarded by an atomic flag, immediately before the first send (not at startup — no unprompted permission prompt on first launch). On denial or error the send is dropped and logged at debug level; the engine treats delivery as best-effort fire-and-forget and never surfaces delivery failure into turns or chat. On Windows/Linux the same lazy call is a no-op. ThreadID = session/group/schedule ID so bursts stack per conversation.

### D7 — Frontend: APP section, pane, focus hook, deep-link handler

- `settingsPaneSections` gains an APP section (first, leading the sidebar) with the `notifications` pane; `PaneKey` union, `paneComponents`, and a `NotificationsPane.tsx` (HooksPane pattern: shared kit components, bound service through `client`).
- A small hook subscribed to the active chat/group selection reports focus via `ReportFocus` on change (including the server-mode web frontend — the call is a no-op-friendly no-error there per the host-parity requirement).
- A `notifications:open` listener navigates to the session/group (mirroring how `settings:open` drives `useNavStore`), and shows the chat layer if Settings was open.

## Risks / Trade-offs

- [Settings-layout requirement is also modified by the in-flight `add-computer-use` change] → This delta's rewritten enumeration already includes Computer Use, so neither archive order regresses the other; noted for archive time.
- [macOS notification delivery can be suppressed by OS focus modes (Do Not Disturb)] → Out of our control by design; notifications are best-effort. Not surfaced as errors.
- [Focus mirror staleness (frontend crashed/webview reloaded without re-reporting)] → The mirror only ever over-suppresses the exact stale conversation; worst case one missed notification after a reload. The chat view re-reports on mount, bounding staleness to one navigation.
- [wintoast/Linux D-Bus behaviors differ (e.g., no subtitle on Windows, action support variance)] → Payloads keep to title+body everywhere; subtitle only on platforms that honor it. Verified per-platform at smoke time.
- [Burst floods for frequent schedules] → ThreadID stacking mitigates; per-schedule cooldown deliberately deferred (Non-Goal) — revisit only if real use shows flooding.
- [Engine cache vs. multi-writer settings] → Single-user app, single engine instance; the save path is the only writer besides defaults.

## Migration Plan

Single forward migration `000022` (new table only — no data backfill; absence of a row means defaults). Rollback: the previous binary ignores the table. No other persisted state changes.

## Open Questions

None blocking. Windows AUMID behavior and Linux D-Bus daemon presence are smoke-time verifications, not design forks: if a Linux session has no notification daemon, the D-Bus send errors and is dropped with the same best-effort path as macOS denial.
