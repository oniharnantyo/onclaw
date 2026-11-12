package notifications

import (
	"log/slog"
	"sync"
	"sync/atomic"
)

// maxRemembered caps the last-candidates map (design D5): click-to-open only
// needs recent notifications, so the oldest entries prune FIFO once the cap
// is exceeded.
const maxRemembered = 512

// NameResolvers resolve candidate NameKeys to display titles: Agent for turn
// and attention candidates, Schedule for schedule candidates, Group for group
// candidates. A nil func or an empty result falls back to the route kind's
// label. Resolvers run on the enqueue path — keep them prompt (cached
// lookups, not scans).
type NameResolvers struct {
	// Agent maps an agent ID to its display name.
	Agent func(id string) string
	// Schedule maps a schedule ID to its display name.
	Schedule func(id string) string
	// Group maps a group ID to its display name.
	Group func(id string) string
}

// Engine is the platform-neutral notification decision core (design D1): it
// gates candidates through the settings cache and the foreground-suppression
// rule, builds the payloads, delivers through the native seam, and remembers
// the route of everything it delivered for click-to-open (design D5). It is
// safe for concurrent use — enqueue paths come from goroutines; the settings
// cache and focus mirror are atomics, the last-candidates map is
// mutex-guarded — and it never blocks its callers beyond the seam calls.
type Engine struct {
	names         NameResolvers
	notifier      NativeNotifier
	windowVisible func() bool

	settings atomic.Pointer[Values]
	focus    atomic.Pointer[Route]

	lastMu    sync.RWMutex
	last      map[string]Route
	lastOrder []string
}

// NewEngine wires the engine onto its host seams: notifier is the delivery
// sink (required — the server host injects its no-op), windowVisible is the
// native visibility query called fresh at enqueue time (design D3; nil
// treats the window as not visible, so nothing suppresses), and names
// resolve candidate NameKeys to titles (nil funcs fall back to the route
// kind). The engine starts on DefaultValues; the composition root calls
// SetSettings with the loaded row once the store is open.
func NewEngine(notifier NativeNotifier, windowVisible func() bool, names NameResolvers) *Engine {
	e := &Engine{
		names:         names,
		notifier:      notifier,
		windowVisible: windowVisible,
		last:          make(map[string]Route),
	}
	defaults := DefaultValues()
	e.settings.Store(&defaults)
	return e
}

// SetSettings replaces the cached settings snapshot; the settings save path
// calls it so preference changes apply immediately without a restart
// (design D4). The value is cached by copy — later caller mutations do not
// touch the cache.
func (e *Engine) SetSettings(v Values) {
	e.settings.Store(&v)
}

// ReportFocus mirrors the frontend's currently viewed context (design D3):
// a candidate is suppressed only when the window is visible AND this route
// matches the candidate's route. An empty id or an unknown kind clears the
// mirror — nothing on screen — because suppression requires positive
// knowledge. The engine never caches window visibility here; hosts query it
// natively at enqueue time.
func (e *Engine) ReportFocus(kind RouteKind, id string) {
	if id == "" || (kind != RouteSession && kind != RouteGroup) {
		e.focus.Store(nil)
		return
	}
	e.focus.Store(&Route{Kind: kind, ID: id})
}

// ResolveRoute maps a delivered notification's deterministic ID back to the
// conversation it concerns — the click-to-open lookup (design D5). ok is
// false for unknown ids, suppressed candidates, failed sends, and empty ids.
func (e *Engine) ResolveRoute(id string) (Route, bool) {
	e.lastMu.RLock()
	defer e.lastMu.RUnlock()
	r, ok := e.last[id]
	return r, ok
}

// Enqueue gates one candidate and, when it passes, builds the payload and
// hands it to the notifier. The gate order is the design's (design D2/D3):
// master flag, category flag, dedup matrix, foreground suppression. Every
// drop is silent — flags off, matrix drops, and suppression are expected
// paths — and send errors are logged at debug and dropped. Enqueue never
// returns an error: trigger sources treat it as fire-and-forget.
func (e *Engine) Enqueue(c Candidate) {
	settings := e.settings.Load()
	if settings == nil || !categoryDelivers(*settings, c.Category) {
		return
	}
	if matrixDrops(c) {
		slog.Debug("notifications: candidate dropped by dedup matrix",
			"id", c.ID, "origin", c.Origin)
		return
	}
	if e.suppressed(c) {
		return
	}
	payload := e.buildPayload(c)
	if err := e.notifier.Send(payload); err != nil {
		slog.Debug("notifications: delivery dropped", "id", c.ID, "error", err)
		return
	}
	e.remember(c.ID, c.Route)
}

// categoryDelivers reports whether the settings deliver candidates of the
// category: the master flag must be on and the category's own flag with it.
// An unknown category delivers nothing.
func categoryDelivers(v Values, c Category) bool {
	if !v.Enabled {
		return false
	}
	switch c {
	case CategoryTurns:
		return v.Turns
	case CategoryAttention:
		return v.Attention
	case CategorySchedules:
		return v.Schedules
	case CategoryGroups:
		return v.Groups
	case CategoryWebhooks:
		// The webhooks category has no settings toggle of its own yet
		// (add-webhook-triggers): it delivers under the master flag. A
		// per-category toggle arrives with the settings surface and wires
		// here.
		return true
	default:
		return false
	}
}

// matrixDrops reports whether the dedup matrix (design D2) excludes the
// candidate from the turn path: scheduler-origin turns report through the
// Schedules category via the announcer seam instead, telegram-origin turns
// are delivered in-chat by the channel, and webhook-origin turns report
// through the Webhooks category via the webhook announcer — the sole
// notifier for webhook fires. Empty origin reads as user — the runtime's
// dispatch convention — so it delivers.
func matrixDrops(c Candidate) bool {
	if c.Category != CategoryTurns {
		return false
	}
	return c.Origin == OriginScheduler || c.Origin == OriginTelegram || c.Origin == OriginWebhook
}

// suppressed applies the foreground-suppression rule (design D3): silence
// only when the window is visible AND the mirrored focus matches the
// candidate's route exactly. An absent focus report (nil mirror) and a nil
// visibility query both deliver — suppression requires positive knowledge.
func (e *Engine) suppressed(c Candidate) bool {
	if e.windowVisible == nil || !e.windowVisible() {
		return false
	}
	focus := e.focus.Load()
	if focus == nil {
		return false
	}
	return *focus == c.Route
}

// buildPayload renders the candidate's title and body: the title names the
// agent, schedule, or group (falling back to the route kind's label), the
// body leads with the outcome and carries the excerpt or error detail.
func (e *Engine) buildPayload(c Candidate) Payload {
	return Payload{
		ID:       c.ID,
		Category: c.Category,
		Title:    e.title(c),
		Body:     buildBody(c.Outcome, c.Detail),
		ThreadID: c.ThreadID,
	}
}

// title resolves the candidate NameKey through the category's resolver,
// falling back to the route kind's label when nothing resolves.
func (e *Engine) title(c Candidate) string {
	if name := e.resolveName(c); name != "" {
		return name
	}
	return fallbackTitle(c.Route.Kind)
}

// resolveName picks the category's resolver and applies it to the NameKey;
// an unknown category, a nil resolver, or an empty key yields no name. The
// webhooks category resolves through the agent resolver: a webhook fire is
// an agent-target run (add-webhook-triggers).
func (e *Engine) resolveName(c Candidate) string {
	var resolve func(id string) string
	switch c.Category {
	case CategoryTurns, CategoryAttention, CategoryWebhooks:
		resolve = e.names.Agent
	case CategorySchedules:
		resolve = e.names.Schedule
	case CategoryGroups:
		resolve = e.names.Group
	}
	if resolve == nil || c.NameKey == "" {
		return ""
	}
	return resolve(c.NameKey)
}

// fallbackTitle is the label a notification wears when no name resolves:
// the route kind's surface name.
func fallbackTitle(kind RouteKind) string {
	if kind == RouteGroup {
		return "Group"
	}
	return "Session"
}

// buildBody renders the notification body: the outcome, with the excerpt or
// error detail on its own line when the source carries one. A missing
// outcome degrades to the detail alone.
func buildBody(outcome, detail string) string {
	switch {
	case outcome == "":
		return detail
	case detail == "":
		return outcome
	default:
		return outcome + "\n" + detail
	}
}

// remember records the route of a delivered notification for click-to-open.
// Candidates without an ID are not clickable, so they are not remembered;
// entries prune FIFO past maxRemembered.
func (e *Engine) remember(id string, route Route) {
	if id == "" {
		return
	}
	e.lastMu.Lock()
	defer e.lastMu.Unlock()
	if _, exists := e.last[id]; !exists {
		e.lastOrder = append(e.lastOrder, id)
	}
	e.last[id] = route
	for len(e.last) > maxRemembered {
		oldest := e.lastOrder[0]
		e.lastOrder = e.lastOrder[1:]
		delete(e.last, oldest)
	}
}
