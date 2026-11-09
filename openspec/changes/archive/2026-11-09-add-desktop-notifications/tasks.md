# Tasks

## 1. Store: notification settings singleton

- [x] 1.1 Add `NotificationSettings` type (Enabled, Turns, Attention, Schedules, Groups, UpdatedAt) + `DefaultNotificationSettings()` (all on) in `internal/store/types.go`, and a `NotificationSettingsStore` sub-interface (`Get` returns nil when no row saved, `Save`) mirroring `gatewaysettings.go`; wire into the `Store` composite. Verify: `go build ./...`.
- [x] 1.2 Migration `internal/store/sqlite/migrations/000022_notification_settings.sql` (singleton table, `gateway_settings` shape) + sqlite implementation of the sub-interface. Verify: `go test ./internal/store/sqlite/ -run TestMigration -count=1`.
- [x] 1.3 Bump the pinned latest schema version in `db_test.go` 21 → 22 and add a store round-trip test (save → get → defaults-when-absent). Verify: `go test ./internal/store/sqlite/ -count=1`.

## 2. Engine: `internal/notifications` decision core

- [x] 2.1 Package scaffold: candidate/payload types (Category enum turns|attention|schedules|groups, deterministic ID scheme per design D5, route {kind,id}), `NativeNotifier` seam interface, and `Engine` holding an atomic settings cache, atomic focus mirror, name-resolver funcs, and the notifier seam — all injected at construction. Verify: `go build ./...`.
- [x] 2.2 Gating logic: master flag, per-category flags, dedup matrix (scheduler-origin turns ignored on the turn path, Telegram-origin dropped, group member turns excluded upstream), foreground suppression (deliver unless window-visible AND focus route == candidate route), payload building (title = agent/schedule/group name, body = outcome + excerpt/error detail). Verify: table-driven unit tests covering the spec's trigger/suppression matrix (`go test ./internal/notifications/ -count=1`).
- [x] 2.3 Focus reporting (`ReportFocus`) and settings-refresh callback (save path → cache reload) with tests for stale/absent focus (absent ⇒ deliver). Verify: `go test ./internal/notifications/ -count=1`.

## 3. Agent manager hooks: turn-finished + session-parked

- [x] 3.1 Add `WithTurnFinished(cb func(sessionID string, origin hooks.Origin, outcome TurnOutcome, turnID string))` and `WithSessionParked(cb func(sessionID string, kind ParkKind, detail string))` manager options (additive; `WithTurnTransition` untouched). Fire `TurnFinished` in `finishTurn` after the busy record clears, mapping the terminal error to completed/cancelled/failed and skipping group-lane turns; fire `SessionParked` from `parkForApproval` (command detail) and `installPendingInput` (prompt/method detail). Verify: `go build ./...` + `go vet ./internal/agents/`.
- [x] 3.2 Manager tests: user-origin turn → one finished callback with outcome; cancelled turn → cancelled; scheduler-origin turn → still fires (engine filters, manager reports truthfully); group-lane turn → no callback; approval park and input park → parked callbacks with details; unparked resolve path → no parked callback. Verify: `go test ./internal/agents/ -run Turn -count=1`.

## 4. Hosts: seam impls, wiring, click-to-open

- [x] 4.1 Desktop `wailsNotifier` in `main.go` wrapping Wails `notifications.New()`: lazy one-time authorization (design D6), send with ThreadID = conversation ID, `OnNotificationResponse` → engine route lookup → show main window (same call as tray `OnClick`) → `emitter.Emit(EventNotificationOpen, …)`; register the notifications service and `application.RegisterEvent` for the open event; inject the notifier into `cli.Assemble` as a new host seam. Verify: `go build ./...` and `go vet ./...`.
- [x] 4.2 Server no-op notifier in `main_server.go` (settings/focus calls succeed, sends drop) + seam default so any third host compiles. Verify: `go build -tags server ./...`.
- [x] 4.3 Composition-root wiring in `internal/cli`: engine construction with name resolvers (agentID/scheduleID/groupID → names via existing store/registry lookups), sources connected — `WithTurnFinished`/`WithSessionParked`, a desktop `RunCompletionAnnouncer` implementation beside Telegram's, scheduler notify chain → engine, groups task-terminal events → engine; save-path callback refreshes the engine cache. Verify: `go build ./...` + `go test ./internal/cli/... -count=1`.
- [x] 4.4 Engine unit test for the announcement path: schedule run completion with excerpt lands as a Schedules notification; foreground viewing that schedule's session suppresses it. Verify: `go test ./internal/notifications/ -count=1`.

## 5. Bridge: bound service + event mirrors

- [x] 5.1 `NotificationsService` in `internal/services` (Get/Save via `SettingsService`, `ReportFocus` → engine) registered in both hosts' service lists; `EventNotificationOpen = "notifications:open"` constant + payload struct in `internal/services/events.go`. Verify: `go build ./...` + `go test ./internal/services/... -count=1`.
- [x] 5.2 Regenerate bindings (`task common:generate:bindings`) and update hand-maintained mirrors: `frontend/src/lib/events.ts` (open-event constant), `frontend/src/lib/client.ts` typed wrappers (`onNotificationOpen`, notifications settings + focus calls), transport passthrough. Verify: frontend typecheck (`cd frontend && npm run build` or repo's typecheck task).

## 6. Frontend: pane, focus hook, deep link

- [x] 6.1 Settings registry: APP section (leading the sidebar) with the `notifications` pane; `PaneKey` union entry; `NotificationsPane.tsx` (HooksPane pattern — shared kit, master toggle + four category toggles, save on toggle, master-off disables category rows visually per spec). Verify: typecheck + component test for toggle→save call and disabled-row rendering.
- [x] 6.2 Focus reporting hook: on active chat session / group view change, call `ReportFocus(kind, id)` (including first mount); no-op-safe on web/server frontend. Verify: component test asserting the call on navigation.
- [x] 6.3 Deep-link handler: `notifications:open` listener navigates to the session/group and leaves Settings if open (mirroring `settings:open` handling). Verify: component test for navigation effect.
- [x] 6.4 Theme parity check for the new pane in light and dark (tokens-based styles only). Verify: manual review against an existing pane in both themes.

## 7. Verification: suite + live smoke

- [x] 7.1 Full Go gate: `gofmt -w .`, `go vet ./...`, `go test ./...` (rerun-once on the known suite flakes). Verify: all green.
- [ ] 7.2 Live smoke, desktop (`task dev` or packaged build): hidden-window turn completion notifies; foreground viewing of the same session does not; approval park notifies and click opens the session; scheduled run completion notifies with excerpt; group task terminal notifies; settings toggles take effect live; notification click shows window + navigates. Verify: observations recorded in the session notes.
- [ ] 7.3 Live smoke, server mode (`task run:server`): settings get/save + focus report over the served frontend succeed with no errors in logs. Verify: curl or UI interaction log clean.
