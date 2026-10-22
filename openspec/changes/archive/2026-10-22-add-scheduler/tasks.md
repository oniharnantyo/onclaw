# Tasks

Integration contracts (names every task codes against; see design D1–D14):

- `store.Schedule{ID, Name, AgentID, Prompt, Cron, Enabled, NextFireAt, LastRunAt, LastRunStatus, LastRunError, LastRunSessionID, CreatedAt, UpdatedAt}` (`*time.Time` for the optionals) + `store.ScheduleStore{Create, ByID, ByName, List, ListDue, Update, Delete, RecordFire}` reached through `Store.Schedules()`; `store.ScheduleRunStatusRan/Failed/Missed`.
- `scheduler.Service` (Create/Update/Delete/SetEnabled/List/RunNow/Preview/Attach/Stop/Tick + listener registry), `scheduler.ScheduleView`, `scheduler.ScheduleInput`, `scheduler.Preview{OK, Error, Description, Next []time.Time}`, `scheduler.Describe(expr)`, `scheduler.ParseFiveField`, `scheduler.LateTolerance` (2 min); `scheduler.Runs` seam `{Create(ctx, agentID, title) (string, error); Submit(ctx, sessionID, prompt string) error}`.
- `agents.SendRequest{SessionID, Message, AttachmentKeys, Origin}` + `Manager.Submit(ctx, SendRequest)`; `hooks.OriginUser = "user"`, `hooks.OriginScheduler = "scheduler"`.
- Catalog tool `ToolScheduler = "scheduler"` (display "Scheduler", IconKey `calendar-clock`); `buildDirectTool(name string, scope toolScope)` where `toolScope{AgentID, Workspace}`; `agents.WithScheduler(*scheduler.Service)`.
- `services.ScheduleService`; `services.EventSchedulesChanged = "schedules:changed"` with `SchedulesChangedEvent{ScheduleID, SessionID, AgentID}`; `ServiceSet.Schedule`; `cli.Assemble` wires the ticker, `App.Close` stops it.
- Frontend: `Menu = "chat" | "agents" | "schedules" | "settings"`; `features/scheduler/` (list view, `ScheduleEditorModal`, `RecurrenceBuilder`); `lib/cronPresets.ts` (pure compose/decompose over the five preset shapes); `client.schedules.*` + `events.onSchedulesChanged`.

## 1. Schema and store

- [x] 1.1 Migration `000013_scheduler.sql`: `CREATE TABLE schedules (id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE, agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE, prompt TEXT NOT NULL, cron TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, next_fire_at TEXT, last_run_at TEXT, last_run_status TEXT NOT NULL DEFAULT '', last_run_error TEXT NOT NULL DEFAULT '', last_run_session_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`; unique index on `name`, due index on `(enabled, next_fire_at)`; schema-version pins in `db_test.go` move 12 → 13. Verify: `go test ./internal/store/sqlite/...` including the migration-count test.
- [x] 1.2 `store.Schedule` + `ScheduleStore` port (`schedules.go`), sqlite implementation, fake parity, `Store.Schedules()` accessor; `RecordFire` writes last-run fields and the advanced `next_fire_at` in one `UPDATE`; `ListDue(now)` returns enabled rows with `next_fire_at <= now`. Verify: sqlite tests — round-trip, `ErrNotFound`, NOCASE duplicate name rejected, cascade on agent delete, due filter, `RecordFire` atomicity; fake mirrors behavior for service tests.

## 2. Cron brain and domain service

- [x] 2.1 `go get github.com/robfig/cron/v3`; `internal/scheduler`: `ParseFiveField` (exactly-five-fields gate before `cron.ParseStandard`, `@`-descriptors rejected with the expected-form message), `Describe` (human sentence for the preset shapes, generic fallback), `Next3`. Verify: table-driven tests for valid/invalid/described/next-fire cases.
- [x] 2.2 `scheduler.Service` CRUD over the store with the spec's validation (name/prompt/agent/cron present, agent exists, case-insensitive duplicate name error identifying the existing schedule's id), clock seam, listener registry. Verify: service tests against the fake store + fake clock, including every rejection message shape.
- [x] 2.3 `Tick(ctx, now)` state machine per design D3 (fire when Running within `LateTolerance`; miss when overdue or Paused, recording the most recent slot; wait when within tolerance but not Running; future `next_fire_at` recomputed each tick) and the fire path through the `Runs` seam with `RecordFire` before the run. Verify: tick tests for fire / paused-miss / overdue-miss / multi-slot-miss / wait-at-boot, with a fake `Runs`.
- [x] 2.4 `RunNow` (state machine bypassed, `next_fire_at` untouched) and change notifications on every mutation, fire, and miss. Verify: service tests assert listener calls and that RunNow leaves an disabled schedule disabled with its next fire unchanged.

## 3. Runtime origin and gateway attach

- [x] 3.1 `hooks.OriginUser/OriginScheduler` constants; `agents.SendRequest` + `Manager.Submit`; `Send` delegates with origin user (signature untouched); the runner stamps `hookBaseEvent` from the request's origin; `Regenerate` stays user. Verify: `go test ./internal/agents/...` — a scheduler-origin send's hook events carry `scheduler`, a user send still carries `user`.
- [x] 3.2 Real `scheduler.Runs` adapter (CreateSession + `SetTitle` before send + `Submit` with origin scheduler), `Attach(gw)` subscribe-first minute-aligned ticker (sleep to next :00) that keeps ticking through Paused and stops on stopped/teardown, wired in `cli.Assemble` and stopped in `App.Close`. Verify: `internal/cli` tests — ticker starts only after the first Running transition, Close is idempotent; `go build ./... && go build -tags server ./...`.

## 4. Scheduler tool

- [x] 4.1 `buildDirectTool(name, scope toolScope)` refactor across the constructor switch and call sites (`gate.go` passes the turn's agent); catalog entry `ToolScheduler` ("Scheduler", `calendar-clock`). Verify: `go test ./internal/agents/...` — existing tool tests green, catalog listing includes Scheduler.
- [x] 4.2 `agents.WithScheduler` option + the `scheduler` tool: single `action` argument (`create`/`list`/`update`/`delete`) with scalar fields; `create` defaults agent to the caller and enabled to true, returns id + description + next fire; `list` returns full state; `update` changes only supplied fields; readable errors (invalid cron with expected form; duplicate name naming the existing id + suggesting update; unknown id); every mutation lands in the same domain service. Verify: tool tests — the spec's six scenarios, gating unchanged via the standard catalog path.

## 5. Wails service, event, composition

- [x] 5.1 `services.ScheduleService` (List/Create/Update/SetEnabled/Delete/RunNow/Preview) over `scheduler.Service` with `ScheduleInput`/`ScheduleView` (no timestamps on Input; `*time.Time` on View), listener → `EventSchedulesChanged` forwarding through the `Emitter`; `ServiceSet.Schedule` + `Assemble` wiring. Verify: `go test ./internal/services/...` with a fake emitter — CRUD and RunNow emit, Preview passes through OK/error/next.
- [x] 5.2 `main.go`: `application.NewService(app.Services.Schedule)` + `application.RegisterEvent[SchedulesChangedEvent]`; server host untouched. Verify: `go build ./... && go build -tags server ./... && go test ./internal/cli/...`.

## 6. Frontend: navigation and Schedules view

- [x] 6.1 Regenerate bindings (`task common:generate:bindings --force`); `client.ts` gains the `schedules` surface and `events.ts` the `onSchedulesChanged` constant; confirm `*time.Time` fields bind as nullable (fallback: RFC3339 strings per design D13). Verify: `tsc` clean; the generated model shows nullable times.
- [x] 6.2 `stores/nav.ts` `Menu` union + `App.tsx` `menus` entry, pane branch, strip label "Schedules"; `calendar-clock` added to the Icon map. Verify: component tests — rail item renders in expanded, collapsed, and mobile bar states; activating it swaps the pane and the strip text.
- [x] 6.3 `features/scheduler` list view: rows (last-run outcome dot, name, agent, description, relative next run, enabled Toggle, edit/run-now/delete), empty state with manual-add + agent-path note, refetch on `schedules:changed` (and chat session list refetch for the event's agentID); both themes. Verify: component tests — render, toggle calls `setScheduleEnabled`, delete confirms then refetches, run-now calls through, event triggers refetch, empty-state copy names both paths.

## 7. Frontend: editor and preview

- [x] 7.1 `lib/cronPresets.ts`: pure compose/decompose over the five preset shapes (design D11), non-matching expressions decompose to Advanced. Verify: `npm test` unit cases for every preset round-trip, `*/N` minute/hour forms, multi-day weekly, and Advanced fallback.
- [x] 7.2 `ScheduleEditorModal` + `RecurrenceBuilder`: name/agent/prompt/enabled fields, Segmented presets with plain inputs, Advanced cron input, debounced `Preview` for summary + next-three-runs + local-time note, inline field errors gating Save, edit prefill that reconstructs the preset or opens Advanced. Verify: component tests — daily and weekly preview content, invalid cron shows the inline error and disables Save, editing `30 17 * * 1,5` opens Weekly prefilled, editing `*/15 9-17 * * 1-5` opens Advanced, save calls the client with an Input-shaped payload.

## 8. Gates

- [x] 8.1 `gofmt -w .`, `go vet ./...`, `go test ./...` (`-race` on store/scheduler/services/agents), `go build -tags server ./...`, `task build` (bindings regenerated after — stale-dist gotcha), `vp check src`, `npm test` — all green. Verify: fresh output recorded; a manual smoke — create a schedule from the menu, see it fire, then ask an agent to create one and see both in the list.
