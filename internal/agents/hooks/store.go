package hooks

import "context"

// Definition is the dispatcher's view of one stored hook definition. The
// adapter over store.Store maps its rows into this shape so this package
// speaks no store types (the backend/tools/mcp subpackage pattern). Config
// is the raw stored JSON document; the handler for Definition.HandlerType
// owns its shape.
type Definition struct {
	ID          string
	Name        string
	Event       string
	Matcher     string
	HandlerType string
	Config      string
	// TimeoutMS is the hook's own budget; <= 0 selects the handler default
	// (defaultTimeoutMS).
	TimeoutMS int
	// OnFailure resolves a handler failure: "" or "allow" continues the
	// chain (fail-open), "block" fails closed.
	OnFailure string
	Enabled   bool
}

// Execution is one audit row of a hook evaluation (spec agent-hooks
// "Execution audit"). AgentID/SessionID/TurnID locate the turn; they are
// empty when there is no session context (a dry-run). Rows outlive the hook:
// HookID is stored denormalized and is not validated against the definitions
// table at write time.
type Execution struct {
	HookID     string
	AgentID    string
	SessionID  string
	TurnID     string
	Event      string
	Outcome    string
	Error      string
	DurationMS int64
}

// Audit outcome values — the audit spelling of the decision contract. The
// adapter maps them onto the store's HookOutcome* constants, which carry the
// same strings.
const (
	OutcomeAllowed = "allowed"
	OutcomeBlocked = "blocked"
	OutcomeError   = "error"
)

// outcomeOf maps a dispatch outcome onto its audit value.
func outcomeOf(decision string, err error) string {
	switch {
	case err != nil:
		return OutcomeError
	case decision == DecisionBlock:
		return OutcomeBlocked
	default:
		return OutcomeAllowed
	}
}

// Store is the dispatcher's persistence seam (design D1: narrow interfaces
// in the ported package's own words, satisfied by adapters over store.Store).
// Resolution reads both tiers fresh on every call — never cached at boot —
// so content changes converge immediately.
type Store interface {
	// AssignedHooks returns the definitions assigned to the agent (design
	// D4), in assignment order (position, then name). The result is never
	// nil.
	AssignedHooks(ctx context.Context, agentID string) ([]Definition, error)
	// GlobalHooks returns the unassigned definitions — the global tier:
	// every hook no agent is assigned, in list order (position, then name).
	// The result is never nil.
	GlobalHooks(ctx context.Context) ([]Definition, error)
	// RecordExecution appends one audit row. Implementations stamp identity
	// and time; the dispatcher treats a write failure as best-effort.
	RecordExecution(ctx context.Context, execution Execution) error
}
