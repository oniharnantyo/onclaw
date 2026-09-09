// Package hooks implements the agent lifecycle-hook pipeline (design.md
// D8–D12): event matchers, the handler registry with its unified decision
// contract, the http/command/mcp_tool/prompt handlers, and the per-run
// dispatcher that resolves the three governance levels and delivers events.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/dop251/goja"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Event is the payload delivered to every hook handler — the same JSON hits
// webhook bodies (http handler) and command standard input. These keys are
// the wire contract and must not change shape without a version bump.
type Event struct {
	Event      string     `json:"event"`            // run_started|user_prompt_submit|pre_tool_use|post_tool_use|run_finished
	DeliveryID string     `json:"delivery_id"`      // unique per evaluation
	Origin     string     `json:"origin"`           // user|cron|channel
	Status     string     `json:"status,omitempty"` // run_finished only: completed|failed|cancelled
	Workspace  EventRef   `json:"workspace"`        // {id,name}
	Agent      EventRef   `json:"agent"`            // {id,name}
	SessionID  string     `json:"session_id"`       // chat/thread session the run belongs to
	User       *EventRef  `json:"user,omitempty"`   // originating user, absent for cron/channel runs
	Tool       *EventTool `json:"tool,omitempty"`   // pre/post_tool_use only
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
	Name  string
	Level domain.HookLevel
	// CompiledScript is the script handler's per-run compile cache (D22):
	// the program compiled once when the run's hook chain resolved. Nil —
	// the REST dry-run path — makes the handler compile per delivery.
	CompiledScript *goja.Program
}

// Decision is the decision object every handler type MAY emit
// ({"decision": "allow"|"block", "reason"}). Parsed by ParseDecisionJSON.
type Decision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

// Result is the uniform outcome of one handler execution. Decision is always
// "allow" or "block"; the detail fields are handler-specific and stay nil
// elsewhere (command → ExitCode, http → HTTPStatus, prompt → TokenCount,
// script → ConsoleLines).
type Result struct {
	Decision   string
	Reason     string
	ExitCode   *int
	HTTPStatus *int
	TokenCount *int64
	// ConsoleLines carries the script handler's captured console output
	// (D22: capped buffer surfaced by the D18 Test panel); nil for every
	// other handler kind.
	ConsoleLines []string
}

// Handler is one registered hook handler kind (D9). All kinds share the same
// event payload and decision contract: a successful execution that returns no
// decision object means allow; any returned error is the caller's to resolve
// against the hook's on_failure policy. Budget bounds the whole execution.
type Handler interface {
	Execute(ctx context.Context, ev Event, hook HookRef, budget time.Duration) (Result, error)
}

// ParseDecisionJSON applies the strict decision-body rule shared by all
// handler kinds: the WHOLE trimmed input must parse as a single JSON object
// carrying "decision" of exactly "allow" or "block" ("reason" is an optional
// string). Log output, prose, or partial JSON is never a decision — lenient
// find-the-JSON parsing would be a decision-injection channel (D10).
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
	case "allow", "block":
		return parsed, true
	default:
		return Decision{}, false
	}
}
