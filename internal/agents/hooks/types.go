package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/dop251/goja"
)

// The five lifecycle points a hook binds (spec agent-hooks "Hook events").
const (
	EventRunStarted       = "run_started"
	EventUserPromptSubmit = "user_prompt_submit"
	EventPreToolUse       = "pre_tool_use"
	EventPostToolUse      = "post_tool_use"
	EventRunFinished      = "run_finished"
)

// Events is the known-event set; a definition carrying anything else is
// skipped gracefully at resolve time.
var Events = []string{EventRunStarted, EventUserPromptSubmit, EventPreToolUse, EventPostToolUse, EventRunFinished}

func isKnownEvent(event string) bool {
	for _, e := range Events {
		if e == event {
			return true
		}
	}
	return false
}

// The two supported handler types (design D9; the http/prompt/mcp_tool
// handler kinds are dropped in the port).
const (
	HandlerCommand = "command"
	HandlerScript  = "script"
)

// HandlerTypes is the known-handler set; a definition carrying anything else
// is skipped gracefully at resolve time.
var HandlerTypes = []string{HandlerCommand, HandlerScript}

func isKnownHandlerType(handlerType string) bool {
	for _, t := range HandlerTypes {
		if t == handlerType {
			return true
		}
	}
	return false
}

// The two assignment tiers (design D4): a hook assigned to the agent runs
// first on its turns; unassigned hooks form the global tier and run for
// everyone.
const (
	TierAssigned = "assigned"
	TierGlobal   = "global"
)

// Audit decision values — the internal decision contract of a handler
// execution. The Store seam's outcome constants (Outcome*) are the audit
// spelling of the same three states.
const (
	DecisionAllow   = "allow"
	DecisionBlock   = "block"
	DecisionFailure = "failure"
)

// Event is the payload delivered to every hook handler — the same JSON hits
// command standard input and the script handler's input object. These keys
// are the wire contract and must not change shape without a version bump.
type Event struct {
	Event      string     `json:"event"`            // run_started|user_prompt_submit|pre_tool_use|post_tool_use|run_finished
	DeliveryID string     `json:"delivery_id"`      // unique per evaluation
	Origin     string     `json:"origin,omitempty"` // user|scheduler|channel|telegram; absent when unset
	Status     string     `json:"status,omitempty"` // run_finished only: completed|failed|cancelled
	Agent      EventRef   `json:"agent"`            // {id,name}
	SessionID  string     `json:"session_id"`       // chat session the run belongs to
	TurnID     string     `json:"turn_id,omitempty"`
	Tool       *EventTool `json:"tool,omitempty"` // pre/post_tool_use only
}

// EventRef is a minimal {id,name} reference embedded in Event.
type EventRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// EventTool identifies the tool call a pre_tool_use / post_tool_use event
// carries. Args is the RAW call arguments as a JSON string (not a nested
// object) so handlers receive exactly what the model produced.
type EventTool struct {
	Name   string `json:"name"`
	CallID string `json:"call_id"`
	Args   string `json:"args"`
}

// HookRef identifies the hook a delivery executes for — carried into Handler
// so handlers can name it in reasons (e.g. the command handler's
// "blocked by hook <name>" default).
type HookRef struct {
	Name string
	Tier string
	// CompiledScript is the script handler's per-run compile cache: the
	// program compiled once when the run's hook chain resolved. Nil — the
	// dry-run path — makes the handler compile per delivery.
	CompiledScript *goja.Program
}

// Decision is the decision object every handler type MAY emit
// ({"decision": "allow"|"block", "reason"}). Parsed by ParseDecisionJSON.
type Decision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

// Result is the uniform outcome of one handler execution. Decision is always
// DecisionAllow or DecisionBlock; the detail fields are handler-specific and
// stay nil elsewhere (command → ExitCode, script → ConsoleLines).
type Result struct {
	Decision string
	Reason   string
	ExitCode *int
	// ConsoleLines carries the script handler's captured console output
	// (capped buffer surfaced by the hooks Test panel); nil for command.
	ConsoleLines []string
}

// Handler is one registered hook handler kind. All kinds share the same
// event payload and decision contract: a successful execution that returns
// no decision object means allow; any returned error belongs to the caller's
// on_failure policy. Budget bounds the whole execution.
type Handler interface {
	Execute(ctx context.Context, ev Event, hook HookRef, budget time.Duration) (Result, error)
}

// ParseDecisionJSON applies the strict decision-body rule shared by all
// handler kinds: the WHOLE trimmed input must parse as a single JSON object
// carrying "decision" of exactly "allow" or "block" ("reason" is an optional
// string). Log output, prose, or partial JSON is never a decision — lenient
// find-the-JSON parsing would be a decision-injection channel.
func ParseDecisionJSON(b []byte) (Decision, bool) {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return Decision{}, false
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	var parsed Decision
	if err := dec.Decode(&parsed); err != nil {
		return Decision{}, false
	}
	// The object must be the entire input: any second token (another value,
	// trailing garbage) disqualifies the body.
	if _, err := dec.Token(); err != io.EOF {
		return Decision{}, false
	}
	switch parsed.Decision {
	case DecisionAllow, DecisionBlock:
		return parsed, true
	default:
		return Decision{}, false
	}
}
