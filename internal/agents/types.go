package agents

import "errors"

// ContextUsage is a session's context-window fill state: tokens used by the
// working context relative to the model's declared context limit. Used is
// display-grade accounting derived from persisted usage — the last
// completed turn's final input plus its output — not a billing figure.
type ContextUsage struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
	// LastInput and LastOutput are the last completed turn's input and
	// output token counts (design D7) — read from the same usage block Used
	// derives from, so the chat view can show the last turn's cost. Both
	// zero before any completed turn.
	LastInput  int `json:"last_input"`
	LastOutput int `json:"last_output"`
}

// Sentinel errors surfaced by the runtime. Callers test them with errors.Is;
// concrete failures wrap them with %w and context.
var (
	// ErrTurnInProgress is returned by Send when the session already has a
	// turn running. Senders queue the message and retry when the running
	// Turn completes.
	ErrTurnInProgress = errors.New("a turn is already running for this session")
	// ErrProviderNotConfigured is returned when an agent references a
	// provider that is missing, disabled, or has no API credential.
	ErrProviderNotConfigured = errors.New("provider not configured")
	// ErrProviderUnsupported is returned when the agent's provider has a
	// type the runtime has no connector for.
	ErrProviderUnsupported = errors.New("provider type not supported")
	// ErrModelNotListed is returned when a turn's model override is not in
	// the provider's model listing (the free-form-id types accept anything).
	ErrModelNotListed = errors.New("model not in provider listing")
	// ErrEffortUnsupported is returned when a turn's reasoning effort is not
	// in the effective model's capability set.
	ErrEffortUnsupported = errors.New("reasoning effort not supported by model")
	// ErrTurnCancelled is the terminal outcome of a cancelled turn: the run
	// was stopped on the user's request and a turn_cancelled event is on the
	// transcript with any partial output retained.
	ErrTurnCancelled = errors.New("turn cancelled")
	// ErrApprovalNotPending is returned by ResolveApproval when the named
	// interrupt is not the session's pending approval — a stale id, an
	// already-resolved decision, or a session with nothing pending. The
	// pending state is left intact (spec chat-approvals: conflict error).
	ErrApprovalNotPending = errors.New("no pending approval for this interrupt")
)
