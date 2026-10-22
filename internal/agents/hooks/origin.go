package hooks

// The run origins a hook event can carry (spec add-scheduler "Scheduled runs
// are distinguishable to hooks", design D6): Origin names who started the
// run, so a hook can treat an unattended run differently from a user turn.
// The agents runtime stamps it from the send request; a later trigger
// surface (channels, Telegram) extends the set.
const (
	// OriginUser is the origin of a turn a user started — the default: an
	// unknown or empty origin reads as a user turn.
	OriginUser = "user"
	// OriginScheduler is the origin of a run a scheduler fire started.
	OriginScheduler = "scheduler"
)
