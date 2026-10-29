package hooks

// The run origins a hook event can carry (spec add-scheduler "Scheduled runs
// are distinguishable to hooks", design D6): Origin names who started the
// run, so a hook can treat an unattended run differently from a user turn.
// The agents runtime stamps it from the send request; trigger surfaces
// extend the set — the scheduler fire today, and the Telegram channel
// (add-telegram-channel) now.
const (
	// OriginUser is the origin of a turn a user started — the default: an
	// unknown or empty origin reads as a user turn.
	OriginUser = "user"
	// OriginScheduler is the origin of a run a scheduler fire started.
	OriginScheduler = "scheduler"
	// OriginTelegram is the origin of a turn an inbound Telegram channel
	// message started (add-telegram-channel design D4): a channel turn that
	// hooks, the turn environment block, and memory treat generically as a
	// non-user trigger, and that names the chat the reply delivers to (the
	// initiator delivery rule).
	OriginTelegram = "telegram"
)
