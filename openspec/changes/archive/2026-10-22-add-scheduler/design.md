# Design

## Context

The seams this change fills already exist and were left open for it: `internal/gateway` is a lifecycle Manager (Start/Stop/Pause/Resume/State/Subscribe) whose doc names cron as the first future trigger adapter; both hosts start it at boot (`main.go:214`, `server_host.go:148`) and the tray reflects its state; `agents.Manager.CreateSession` + `Send` run a turn headlessly and persist its transcript regardless of subscribers; the runner auto-titles only untitled sessions (`runner.go:142`); `hookBaseEvent` hardcodes origin `"user"` (`hooks.go:30`); the tool catalog constructs direct tools per turn inside `resolveTurnTools` (`gate.go:132`), which holds the agent; SQLite runs with `foreign_keys(1)` (`db.go:101`) and a new table already cascades on parent delete (`mcp_oauth_tokens`); the next migration number is 13. Services stay Wails-free behind narrow seams (`Emitter`, `URLOpener` are the precedents); `internal/cli/app.go` is the composition root.

See proposal.md for motivation and scope.

## Goals / Non-Goals

**Goals:**

- Firing correctness the user can trust: due schedules fire within a minute while the process runs; everything else (paused, quit, asleep) is visibly skipped, never silently lost or surprise-run late.
- One validation and description path shared by the editor and the agent tool.
- Zero bespoke wiring for the tool (catalog, Tools pane, allowlist chips all flow) and for live UI updates (one event).

**Non-Goals:**

- Webhook/Telegram adapters (they attach to the same seam later); `gateway_settings.default_agent_id` stays reserved for channel triggers.
- Per-schedule timezones; a run-log table; notifications; catch-up execution.
- Changing `Send`'s signature or any existing caller; the chat UX is untouched apart from sessions appearing.

## Decisions

**D1 — A scheduler domain service owns the cron brain; both surfaces are thin clients.** `internal/scheduler.Service` (over the schedule store, the agents runtime, the sessions service, and a clock seam) exposes Create/Update/Delete/SetEnabled/List/RunNow/Preview plus the tick. The Wails `ScheduleService` and the `scheduler` tool both call it, so the spec's "same validation as the editor" is literal — one place parses cron, computes next fires, describes recurrences, and announces changes. Alternatives rejected: validation duplicated in the tool and the service (they drift); the tool writing the store directly (bypasses validation and change notification).

**D2 — The adapter attaches to the gateway; the ticker starts at the first Running.** `Service.Attach(gw)` uses `Manager.Subscribe`: the first `running` transition starts a minute-aligned loop (sleep to the next :00 rather than a fixed `Ticker` — no drift), which keeps ticking through Paused (missed marks need ticks) and stops on `stopped` or teardown. Firing requires `State() == running` at the tick. Alternative rejected — an always-on goroutine from `Assemble`: its first tick can race the host's `Gateway.Start` and mark a just-due slot missed at boot. Subscribe-first makes boot deterministic and is exactly the attachment doc.go reserved. Both hosts already start the gateway, so both get the scheduler.

**D3 — One persisted `next_fire_at` drives a three-way due state machine.** Each tick, for every enabled schedule with `next_fire_at <= now`: **fire** when the gateway is Running and `now - next_fire_at <= LateTolerance` (2 min — absorbs tick jitter and brief laptop sleep); **miss** when overdue beyond tolerance (process off or asleep) or the gateway is Paused — recording the most recent missed slot (iterate `Next` from the stored slot while `<= now`, capped at 10k iterations, falling back to the first slot); **wait** (leave due, re-evaluate next tick) when within tolerance but not Running, which only occurs transiently around boot. Mark-and-advance is one store update (`RecordFire`) *before* the run starts, so a crash mid-fire can never double-fire — the worst case is a `ran` row with no session. While a stored `next_fire_at` is still in the future it is recomputed from the expression each tick (N rows × one `Next` call — trivial), so DST and local-timezone changes self-heal instead of leaving a stale persisted time. Alternatives rejected: no persisted column (cannot detect process-off misses); a jobs queue table (overkill for one local user).

**D4 — robfig/cron/v3 parses; five-field is enforced before parsing.** `cron.ParseStandard` over `time.Local` covers ranges, lists, steps, and day names; the service rejects anything that is not exactly five fields (including `@daily`-style descriptors) *before* parsing, so stored expressions stay canonical, the error text can show the expected form, and the frontend's preset mapper has exactly one grammar. New Go dependency; small and stable. Alternative rejected: a hand-rolled parser duplicating tested code. *(Implemented with `cron.NewParser(cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow)` — ParseStandard's grammar minus the `@` descriptors — which enforces the descriptor rejection this decision mandates; behavior is identical for every five-field expression.)*

**D5 — Local time only.** Expressions evaluate in the host's local zone at computation time; the library handles DST. The editor says "local time" (spec); per-schedule zones can be a column later without touching this design.

**D6 — Run origin rides the send request, not the session.** The runtime's internal send request gains `Origin`; the public `Send` keeps its exact signature and means `user`; a new `Submit(ctx, SendRequest)` carries it. `hookBaseEvent` stamps the request's origin, with named constants `hooks.OriginUser` / `hooks.OriginScheduler` replacing the `hooks.go:30` literal. Session-level origin was rejected: a user's follow-up message inside a scheduled session must still be a `user` turn — origin distinguishes who started a *run*, and scheduled sessions remain fully usable chat sessions. `Regenerate` also stays `user`.

**D7 — Fire = CreateSession + pre-title + Submit.** The service calls `runtime.CreateSession(agentID)`, then `sessions.SetTitle("<schedule name> — <local fire time>")` *before* the send (the runner only auto-titles empty titles, `runner.go:142`), then `Submit` with the prompt and `Origin: scheduler`. Overlapping fires need no locking — each fire creates its own session, and the Manager's per-session serialization is never contended. Model/provider failures surface inside the transcript per the existing turn semantics, so `failed` last-run status is reserved for "could not start" (store error, agent gone).

**D8 — The tool is constructed turn-scoped.** `buildDirectTool` gains a small `toolScope{AgentID, Workspace}` (replacing the bare workspace string; `resolveTurnTools` already holds the agent), so the `scheduler` tool's `create` defaults `agent` to the calling agent. Scalars-only input like `browser`; failures return readable tool errors (invalid cron → the five-field expected form; duplicate name → the existing schedule's id + "use update"). Gating is the standard three-way — catalog entry, Tools pane, allowlist — with nothing bespoke. The signature change ripples mechanically through existing constructor cases and their tests.

**D9 — SQL cascade deletes schedules with their agent.** `schedules.agent_id REFERENCES agents(id) ON DELETE CASCADE` under the already-enabled `foreign_keys(1)`, matching the `mcp_oauth_tokens` precedent. `AgentsService.Delete` gains no scheduler dependency; sessions keep their existing FK-less table and are untouched. The Schedules view refetches on mount (views unmount on menu switch), so cascade deletions surface on navigation.

**D10 — Change notification: listeners in the domain service, one Wails event.** `scheduler.Service` keeps a listener registry (the `gateway.Manager.Subscribe` pattern); every mutation, every fire, and every miss calls it. The Wails `ScheduleService` subscribes at construction and emits `schedules:changed {scheduleID, sessionID, agentID}` (empty strings when not applicable), registered alongside the other typed events. The Schedules view refetches on the event; the chat session list (`features/chat/store.ts`) refetches for `agentID` so a background-created session appears without a reload. No polling anywhere. Alternative rejected: separate `schedules:*` and `sessions:*` events for this one producer.

**D11 — One describer; the editor's live preview is a service call.** `scheduler.Describe(expr)` and `Preview(expr) → {ok, error, description, next[3]}` live in Go beside the parser; the frontend calls `Preview` debounced (~150 ms) for the summary and next-three-runs block. TS keeps only pure, unit-testable mappers for preset ↔ expression composition/decomposition over the exact preset shapes (`*/N * * * *`, `M */N * * *` / `M * * * *`, `MM HH * * *`, `MM HH * * d,d`, `MM HH D * *` — anything else opens Advanced mode). One brain means the tool's "human-readable recurrence", the list rows, and the editor's summary can never disagree. Alternative rejected: a second TS describer/parser.

**D12 — One list view, one editor modal.** `features/scheduler/`: rows show last-run outcome dot, name, agent avatar, the stored description, relative next run, an enabled `Toggle`, and edit / run-now / delete actions; `ScheduleEditorModal` builds on `ModalShell` + `FormField`/`FormGrid` with a `Segmented` preset strip (Minutely/Hourly/Daily/Weekly/Monthly/Cron), plain inputs, the Advanced cron input, the preview block, and the local-time note; the empty state offers manual add and notes the agent path. Navigation: `Menu` union gains `"schedules"`, `App.tsx` `menus` gains the item (settings stays in its bottom cluster), the pane branch renders the view, and the window strip labels it "Schedules". The Icon map gains `calendar-clock` (`clock` and `calendar` exist; `calendar-clock` does not).

**D13 — The binding surface uses Input/View types, never store structs.** `ScheduleInput{Name, AgentID, Prompt, Cron, Enabled}` for writes — no timestamp fields, per the Wails time-binding rule (never send `""` for a Go `time.Time`); the `Schedule` view carries `*time.Time` for `NextFireAt`/`LastRunAt` (null when unset) — pointer bindings are verified at task time with an RFC3339-string fallback if the generator mishandles them. Bindings regenerate after `task build` (`task common:generate:bindings --force`; stale-dist gotcha).

**D14 — A `Run now` is the same fire with the state machine bypassed.** `RunNow` calls the fire path directly (session, prompt, origin, last-run recording) without consulting the gateway state, the enabled flag, or `next_fire_at`, none of which it changes — which is what makes it safe as a confidence button on a paused or disabled schedule.

## Risks / Trade-offs

- [First tick races the host's `Gateway.Start`] → the ticker starts on the first `running` transition (D2); before that, no tick exists at all.
- [Pointer `*time.Time` fields may not survive binding generation] → verified early in the frontend task; fallback is RFC3339 strings with empty-for-null (D13).
- [`buildDirectTool` signature change touches every direct tool constructor and its tests] → mechanical; all call sites are in one file's switch plus tests.
- [An agent could create runaway schedules] → the Schedules menu shows and deletes everything; v1 accepts the risk rather than adding quotas; revisit only if it bites.
- [`@daily`-style shorthand rejected] → intentional (D4); the error message teaches the five-field form, and the editor never produces one.
- [Sleep/wake beyond 2 minutes marks runs missed] → deliberate: a laptop asleep is a process not running; the missed mark keeps the row honest instead of bursting late runs on wake.
- [Missed-slot iteration on a minutely schedule after long downtime] → capped at 10k `Next` steps with a first-slot fallback (D3); local SQLite, once a minute.

## Migration Plan

Migration `000013` is purely additive — one new `schedules` table with its indexes; no backfill; rollback is `DROP TABLE`. Schema-version pins move 12 → 13. `go.mod` gains `github.com/robfig/cron/v3`. Bindings regenerate after the final `task build`. No existing table, service, or spec behavior changes; the only touched existing code paths are the mechanical ones (origin constant, `buildDirectTool` scope, catalog entry, nav union).

## Open Questions

None blocking. The pointer-time binding question (D13) is a task-time verification with a decided fallback, not a spec-level unknown.
