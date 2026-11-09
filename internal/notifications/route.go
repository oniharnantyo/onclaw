package notifications

// RouteKind names the frontend surface a notification's click opens. The
// values mirror the focus report's kind field (design D3) so the composition
// root maps it without translation.
type RouteKind string

const (
	// RouteSession opens a chat session — turns, parks, and schedule runs.
	RouteSession RouteKind = "session"
	// RouteGroup opens a group's view — group task terminals.
	RouteGroup RouteKind = "group"
)

// Route is the conversation a notification opens when clicked (design D5):
// the session for turn and attention notifications, the group for group
// notifications, or the schedule's session for schedule notifications.
type Route struct {
	// Kind names the surface — session or group.
	Kind RouteKind
	// ID is the session or group identifier the frontend navigates to.
	ID string
}

// TurnNotificationID builds the deterministic notification ID for a turn
// terminal (design D5): turn:<sessionID>:<turnID>.
func TurnNotificationID(sessionID, turnID string) string {
	return "turn:" + sessionID + ":" + turnID
}

// ParkNotificationID builds the deterministic notification ID for a
// needs-attention park (design D5): park:<sessionID>:<interruptID>.
func ParkNotificationID(sessionID, interruptID string) string {
	return "park:" + sessionID + ":" + interruptID
}

// ScheduleNotificationID builds the deterministic notification ID for a
// schedule trigger — run completion or missed fire (design D5):
// sched:<scheduleID>:<runID>.
func ScheduleNotificationID(scheduleID, runID string) string {
	return "sched:" + scheduleID + ":" + runID
}

// GroupNotificationID builds the deterministic notification ID for a group
// task terminal (design D5): group:<groupID>:<taskID>.
func GroupNotificationID(groupID, taskID string) string {
	return "group:" + groupID + ":" + taskID
}
