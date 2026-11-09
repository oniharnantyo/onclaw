package notifications

// Category is one of the four user-visible trigger groups a settings flag
// controls (spec desktop-notifications "Notification trigger categories").
// The values mirror the notification_settings column names (design D4) so
// the composition root maps the store row onto the engine without
// translation.
type Category string

const (
	// CategoryTurns reports user-originated turn terminals (completed,
	// failed, cancelled) for non-group sessions.
	CategoryTurns Category = "turns"
	// CategoryAttention reports sessions parked waiting on the user: shell
	// approval requests and input requests (browser sign-in / takeover).
	CategoryAttention Category = "attention"
	// CategorySchedules reports scheduled-run completions (outcome +
	// excerpt) and missed or failed schedule fires.
	CategorySchedules Category = "schedules"
	// CategoryGroups reports group task terminals (completed, failed,
	// paused).
	CategoryGroups Category = "groups"
)

// Origin names who started the run that produced a turn candidate — the
// dedup matrix's first gate (design D2). The values mirror the agent
// runtime's hook origins so the composition root maps them by identity.
type Origin string

const (
	// OriginUser is the origin of a turn a user started — the only origin
	// the turn path delivers.
	OriginUser Origin = "user"
	// OriginScheduler is the origin of a run a scheduler fire started;
	// scheduler-origin turns are dropped on the turn path because the run
	// reports through the Schedules category instead.
	OriginScheduler Origin = "scheduler"
	// OriginTelegram is the origin of a turn an inbound Telegram channel
	// started; the channel delivers results in-chat, so these are dropped
	// entirely.
	OriginTelegram Origin = "telegram"
)

// Values is the engine's cached settings snapshot: the master flag plus one
// flag per category (design D4). The zero value disables everything;
// DefaultValues is the all-on default.
type Values struct {
	// Enabled is the master flag — false produces no notifications at all,
	// regardless of the category flags.
	Enabled bool
	// Turns gates the Turns category.
	Turns bool
	// Attention gates the Needs-attention category.
	Attention bool
	// Schedules gates the Schedules category.
	Schedules bool
	// Groups gates the Groups category.
	Groups bool
}

// DefaultValues returns the all-on default settings — the spec's
// absence-of-a-saved-row meaning (fresh install defaults).
func DefaultValues() Values {
	return Values{
		Enabled:   true,
		Turns:     true,
		Attention: true,
		Schedules: true,
		Groups:    true,
	}
}

// Candidate is one trigger-source event handed to the engine for gating: a
// turn terminal, a park, a scheduled-run completion or missed fire, or a
// group task terminal. The composition root builds it from its real types —
// the engine imports none of them.
type Candidate struct {
	// ID is the candidate's deterministic notification ID (design D5) — one
	// of the route.go constructor outputs — and the key a notification click
	// resolves back to the route with.
	ID string
	// Category is the settings category the candidate reports under.
	Category Category
	// Origin names who started the run, set on turn candidates only: the
	// dedup matrix (design D2) drops scheduler-origin turns (reported
	// through Schedules instead) and telegram-origin turns (reported
	// in-chat) on the turn path. Empty reads as user, matching the runtime's
	// dispatch convention.
	Origin Origin
	// NameKey is the identifier the engine resolves to the notification's
	// title through the constructor's name resolvers — the agent ID for
	// turn and attention candidates, the schedule ID for schedule
	// candidates, the group ID for group candidates. Unresolvable keys fall
	// back to the route kind's label.
	NameKey string
	// Outcome is the terminal result word the body leads with — completed,
	// failed, cancelled, paused, missed, approval required, input required.
	Outcome string
	// Detail is the excerpt or error text the trigger source carries; it
	// rides the body after the outcome. Empty when the source has none.
	Detail string
	// Route is the conversation a notification click opens: the session for
	// turns, parks, and schedule runs, the group for group tasks. Schedule
	// candidates resolve to their run's session route upstream.
	Route Route
	// ThreadID is the conversation ID the platform groups bursts under
	// (design D6) — the session, group, or schedule ID the source owns.
	ThreadID string
}

// Payload is the built notification the engine delivers through the native
// seam (design D1). The seam carries title+body only — platforms that honor
// subtitles derive them at the host (design risk note: Windows and D-Bus
// variance).
type Payload struct {
	// ID is the deterministic notification ID the click response reports
	// back for route resolution.
	ID string
	// Category is the settings category the payload reports under.
	Category Category
	// Title names the agent, schedule, or group the notification concerns.
	Title string
	// Body carries the outcome, with the excerpt or error detail after it
	// when the source provided one.
	Body string
	// ThreadID is the conversation ID so bursts stack per conversation
	// (design D6).
	ThreadID string
}
