package cli

import (
	"context"
	"log/slog"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/groups"
	"github.com/oniharnantyo/buddies/internal/notifications"
	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// Notifier is the host seam for desktop notifications
// (add-desktop-notifications design D1): the delivery sink the engine hands
// built payloads through, plus the host's native window-visibility query for
// foreground suppression (design D3). The desktop host wraps Wails'
// notifications service; the server host injects its no-op; a nil seam
// assembles the internal no-op so any third host compiles.
type Notifier interface {
	notifications.NativeNotifier
	// WindowVisible reports whether the host's main window is on screen.
	// The engine suppresses a candidate only when this is true AND the
	// mirrored focus matches the candidate's route; a host without a window
	// reports false, which delivers everything — the nil-query behavior.
	WindowVisible() bool
}

// noopNotifier is the internal no-op notifier behind a nil seam (design D1's
// server posture, generalized to any host without a notification surface):
// sends drop with a debug log. The engine and its bound settings/focus
// service are constructed regardless, so preferences and focus reports
// succeed on every host (spec host-parity).
type noopNotifier struct{}

func (noopNotifier) Send(p notifications.Payload) error {
	slog.Debug("notifications: no-op host dropped send", "id", p.ID)
	return nil
}

func (noopNotifier) WindowVisible() bool { return false }

// newNotificationEngine builds the notifications decision engine over the
// store (add-desktop-notifications design D1/D4): name resolvers that read
// the agent, schedule, and group rows fresh per enqueue (cheap indexed
// lookups; an unknown id or a failed read yields "" so the engine falls back
// to the route kind's label), the seam's window-visibility query — a nil
// seam passes nil, which never suppresses — and the settings cache loaded
// from the singleton store once at boot; the settings save path refreshes it
// afterwards (design D4).
func newNotificationEngine(ctx context.Context, st store.Store, seam Notifier) *notifications.Engine {
	engine := notifications.NewEngine(notifierForSeam(seam), windowVisibleForSeam(seam), notifications.NameResolvers{
		Agent:    agentName(st),
		Schedule: scheduleName(st),
		Group:    groupName(st),
	})
	engine.SetSettings(loadNotificationValues(ctx, st.NotificationSettings()))
	return engine
}

// notifierForSeam resolves the delivery sink behind the host seam: the seam
// itself, or the internal no-op for a nil seam.
func notifierForSeam(seam Notifier) notifications.NativeNotifier {
	if seam == nil {
		return noopNotifier{}
	}
	return seam
}

// windowVisibleForSeam resolves the suppression query behind the host seam:
// the seam's native window query, or nil for a nil seam — the engine treats
// a nil query as "never suppress" (design D3: suppression requires positive
// knowledge).
func windowVisibleForSeam(seam Notifier) func() bool {
	if seam == nil {
		return nil
	}
	return seam.WindowVisible
}

// loadNotificationValues reads the notification settings singleton into the
// engine's snapshot (add-desktop-notifications design D4): a nil row —
// nothing was ever saved — and a failed read alike apply the all-on
// defaults; boot must never fail for notifications.
func loadNotificationValues(ctx context.Context, settings store.NotificationSettingsStore) notifications.Values {
	if settings == nil {
		return notifications.DefaultValues()
	}
	row, err := settings.Get(ctx)
	if err != nil {
		slog.Warn("assemble: notification settings load failed", "error", err)
		return notifications.DefaultValues()
	}
	if row == nil {
		return notifications.DefaultValues()
	}
	return notifications.Values{
		Enabled:   row.Enabled,
		Turns:     row.Turns,
		Attention: row.Attention,
		Schedules: row.Schedules,
		Groups:    row.Groups,
	}
}

// agentName resolves an agent id to its display name; an unknown id or a
// failed read yields "" and the engine falls back to the route kind's label.
// Called on the enqueue path — a single indexed store read.
func agentName(st store.Store) func(id string) string {
	return func(id string) string {
		row, err := st.Agents().ByID(context.Background(), id)
		if err != nil {
			return ""
		}
		return row.Name
	}
}

// scheduleName resolves a schedule id to its name, falling back to the
// fire-session title: the run-completion path (schedule_runs.go) knows only
// the fire's session, and the fire titles that session "<schedule name> —
// <fire time>", so a session id resolves to the schedule name it carries
// (the design D5 deviation documented at that enqueue site). An unknown id
// or a failed read yields "".
func scheduleName(st store.Store) func(id string) string {
	sessSvc := sessions.NewService(st)
	return func(id string) string {
		ctx := context.Background()
		if row, err := st.Schedules().ByID(ctx, id); err == nil {
			return row.Name
		}
		if sess, err := sessSvc.Session(ctx, id); err == nil {
			return sess.Title
		}
		return ""
	}
}

// groupName resolves a group id to its display name; an unknown id or a
// failed read yields "".
func groupName(st store.Store) func(id string) string {
	return func(id string) string {
		row, err := st.Groups().GroupByID(context.Background(), id)
		if err != nil {
			return ""
		}
		return row.Name
	}
}

// enqueueTurnFinished gates one turn-terminal candidate through the engine
// (add-desktop-notifications design D2): the title resolves through the
// session's agent, the dispatch origin maps onto the engine's dedup
// vocabulary (scheduler- and telegram-origin turns are dropped there), and
// the route is the session itself. Best-effort by contract: the session read
// and the enqueue never break the turn path — a panic or a read failure
// degrades the title to the route kind's label and nothing more.
func enqueueTurnFinished(engine *notifications.Engine, sessSvc sessions.Service, sessionID string, origin hooks.Origin, outcome agents.TurnOutcome, turnID string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug("notifications: turn-finished candidate panicked", "session", sessionID, "panic", r)
		}
	}()
	if engine == nil {
		return
	}
	engine.Enqueue(notifications.Candidate{
		ID:       notifications.TurnNotificationID(sessionID, turnID),
		Category: notifications.CategoryTurns,
		Origin:   notificationOrigin(origin),
		NameKey:  sessionAgentID(sessSvc, sessionID),
		Outcome:  string(outcome),
		Route:    notifications.Route{Kind: notifications.RouteSession, ID: sessionID},
		ThreadID: sessionID,
	})
}

// enqueueSessionParked gates one attention-park candidate
// (add-desktop-notifications design D2): parks fire no turn transition, so
// this is the only signal a parked approval or browser attention request
// emits. The park's detail — the command awaiting approval or the
// prompt/method of an attention card — rides the body under the outcome
// word. Best-effort like every notification path.
func enqueueSessionParked(engine *notifications.Engine, sessSvc sessions.Service, sessionID string, kind agents.ParkKind, interruptID, detail string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug("notifications: session-parked candidate panicked", "session", sessionID, "panic", r)
		}
	}()
	if engine == nil {
		return
	}
	engine.Enqueue(notifications.Candidate{
		ID:       notifications.ParkNotificationID(sessionID, interruptID),
		Category: notifications.CategoryAttention,
		NameKey:  sessionAgentID(sessSvc, sessionID),
		Outcome:  parkOutcome(kind),
		Detail:   detail,
		Route:    notifications.Route{Kind: notifications.RouteSession, ID: sessionID},
		ThreadID: sessionID,
	})
}

// sessionAgentID resolves the agent a session belongs to — the notification
// title's resolver key; a failed read degrades to "" (label fallback).
func sessionAgentID(sessSvc sessions.Service, sessionID string) string {
	if sessSvc == nil || sessionID == "" {
		return ""
	}
	sess, err := sessSvc.Session(context.Background(), sessionID)
	if err != nil {
		return ""
	}
	return sess.AgentID
}

// notificationOrigin maps a hook dispatch origin onto the engine's Origin
// vocabulary (add-desktop-notifications design D2): scheduler turns report
// through the Schedules category instead, telegram turns deliver in-chat —
// the engine drops both on the turn path — and everything else reads as the
// user default, the runtime's dispatch convention. "channel" is the legacy
// spelling of the one channel that exists (Telegram); both deliver in-chat.
func notificationOrigin(origin hooks.Origin) notifications.Origin {
	switch origin {
	case hooks.OriginScheduler:
		return notifications.OriginScheduler
	case hooks.OriginTelegram, "channel":
		return notifications.OriginTelegram
	default:
		return notifications.OriginUser
	}
}

// parkOutcome names the attention request the body leads with: the
// candidate-outcome vocabulary's approval-required and input-required words
// (internal/notifications types.go).
func parkOutcome(kind agents.ParkKind) string {
	if kind == agents.ParkApproval {
		return "approval required"
	}
	return "input required"
}

// newScheduleChangeListener builds the scheduler listener feeding missed and
// failed fires into the engine (add-desktop-notifications proposal,
// Schedules category). The Change carries only ids, so the listener re-reads
// the schedule row and keys off the just-recorded last-run status: missed
// and failed enqueue; ran fires report through the scheduleRuns completion
// path instead (no duplicate), and roster mutations (create/update/delete/
// enable) are not triggers at all. The notification id is deterministic per
// fire slot, and the route is the fire's session when the change or the row
// carries one — a schedule that has never run has no session, and that
// notification's click then only surfaces the window. Best-effort: the row
// read and the enqueue never break the scheduler's notify chain.
func newScheduleChangeListener(engine *notifications.Engine, st store.Store) scheduler.Listener {
	return func(ch scheduler.Change) {
		defer func() {
			if r := recover(); r != nil {
				slog.Debug("notifications: schedule listener panicked", "schedule", ch.ScheduleID, "panic", r)
			}
		}()
		if engine == nil || ch.ScheduleID == "" {
			return
		}
		row, err := st.Schedules().ByID(context.Background(), ch.ScheduleID)
		if err != nil {
			return
		}
		var outcome, detail string
		switch row.LastRunStatus {
		case store.ScheduleRunStatusMissed:
			outcome = "missed"
		case store.ScheduleRunStatusFailed:
			outcome = "failed"
			detail = row.LastRunError
		default:
			return
		}
		sessionID := ch.SessionID
		if sessionID == "" {
			sessionID = row.LastRunSessionID
		}
		engine.Enqueue(notifications.Candidate{
			ID:       notifications.ScheduleNotificationID(row.ID, fireSlotID(row.LastRunAt)),
			Category: notifications.CategorySchedules,
			NameKey:  row.ID,
			Outcome:  outcome,
			Detail:   detail,
			Route:    notifications.Route{Kind: notifications.RouteSession, ID: sessionID},
			ThreadID: row.ID,
		})
	}
}

// fireSlotID renders a fire's slot time as the deterministic run-id half of
// the schedule notification id: the UTC RFC3339 stamp of the recorded slot.
func fireSlotID(slot *time.Time) string {
	if slot == nil {
		return "unknown"
	}
	return slot.UTC().Format(time.RFC3339)
}

// notifyingGroupsEvents interposes the notifications engine beside the
// groups service's UI event forwarder (add-desktop-notifications proposal,
// Groups category): every event forwards unchanged, and a task-updated event
// whose task just reached a terminal state (completed, failed, paused — the
// store's GroupTaskState* vocabulary) enqueues a Groups candidate. The
// GroupEvent payload carries no task state, so the wrapper reads the
// just-committed task row at event time (the groups service persists the
// state before firing the event). It never calls back into the groups
// service — its Events contract forbids re-entry while a group write lock is
// held; only the store and the engine are touched.
type notifyingGroupsEvents struct {
	inner  groups.Events
	engine *notifications.Engine
	tasks  store.AgentGroupStore
}

// newNotifyingGroupsEvents wraps the UI event forwarder with the
// notifications enqueue side.
func newNotifyingGroupsEvents(inner groups.Events, engine *notifications.Engine, tasks store.AgentGroupStore) groups.Events {
	return notifyingGroupsEvents{inner: inner, engine: engine, tasks: tasks}
}

// GroupChanged forwards roster- and list-level changes unchanged — no
// notification trigger.
func (e notifyingGroupsEvents) GroupChanged() {
	e.inner.GroupChanged()
}

// GroupEvent forwards the per-group update to the UI, then checks whether it
// marks a task terminal.
func (e notifyingGroupsEvents) GroupEvent(groupID string, ev groups.GroupEvent) {
	e.inner.GroupEvent(groupID, ev)
	e.notifyTaskTerminal(ev)
}

// notifyTaskTerminal enqueues a Groups candidate when the updated task's
// committed state is one of the three terminal outcomes the spec reports
// (completed, failed, paused). Best-effort: every failure is logged at debug
// and dropped — the forwarder above has already run, so the notification can
// never break the event path.
func (e notifyingGroupsEvents) notifyTaskTerminal(ev groups.GroupEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug("notifications: group task terminal panicked", "group", ev.GroupID, "task", ev.TaskID, "panic", r)
		}
	}()
	if e.engine == nil || ev.Kind != groups.GroupEventTaskUpdated || ev.TaskID == "" || ev.GroupID == "" {
		return
	}
	task, err := e.tasks.TaskByID(context.Background(), ev.TaskID)
	if err != nil {
		return
	}
	var detail string
	switch task.State {
	case store.GroupTaskStateCompleted:
	case store.GroupTaskStateFailed:
		detail = derefString(task.Error)
	case store.GroupTaskStatePaused:
		detail = derefString(task.StopReason)
	default:
		return // queued, running, waiting, cancelled: not reported terminals
	}
	e.engine.Enqueue(notifications.Candidate{
		ID:       notifications.GroupNotificationID(ev.GroupID, ev.TaskID),
		Category: notifications.CategoryGroups,
		NameKey:  ev.GroupID,
		Outcome:  task.State,
		Detail:   detail,
		Route:    notifications.Route{Kind: notifications.RouteGroup, ID: ev.GroupID},
		ThreadID: ev.GroupID,
	})
}

// derefString reads an optional string column; nil reads as "".
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
