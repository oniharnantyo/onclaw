package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Registry errors. Callers (the dispatcher, the dry-run test path) branch on
// these sentinels:
//
//   - ErrInvalid: a definition or config this binary cannot validate — the
//     save-time counterpart lives in the services layer.
//   - ErrUnknownHandlerType: the type is not in the supported set at all —
//     stored data this binary is too old to interpret (graceful skip).
//   - ErrCommandDisabled / ErrScriptDisabled: the handler is switched off
//     for this instance (operator kill switch) — a skip, not a failure
//     policy.
//   - ErrSpawnFailed: the command handler could not start the program —
//     callers may surface this distinctly on top of on_failure.
var (
	ErrInvalid            = errors.New("invalid hook definition")
	ErrUnknownHandlerType = errors.New("unknown handler type")
	ErrCommandDisabled    = errors.New("command handler disabled")
	ErrScriptDisabled     = errors.New("script handler disabled")
	ErrSpawnFailed        = errors.New("hook command spawn failed")
)

// Registry resolves handler types to Handler implementations and applies the
// shared execution contract: budget enforcement and the
// no-decision-means-allow rule. Both supported handler types register by
// default; each can be kill-switched through its option (design D9: handler
// options so they can be turned off later without an edit).
type Registry struct {
	commandEnabled bool
	scriptEnabled  bool
}

// RegistryOption configures the registry.
type RegistryOption func(*Registry)

// WithCommandEnabled toggles the command handler (operator kill switch;
// default on).
func WithCommandEnabled(v bool) RegistryOption {
	return func(r *Registry) { r.commandEnabled = v }
}

// WithScriptEnabled toggles the script handler (operator kill switch;
// default on).
func WithScriptEnabled(v bool) RegistryOption {
	return func(r *Registry) { r.scriptEnabled = v }
}

// NewRegistry builds the registry with production defaults: both supported
// handler types enabled.
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{
		commandEnabled: true,
		scriptEnabled:  true,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Execute runs one hook handler delivery within the given budget. A
// successful execution that produces no decision object resolves to allow;
// returned errors belong to the caller's on_failure policy.
func (r *Registry) Execute(ctx context.Context, handlerType string, cfg json.RawMessage, ev Event, hook HookRef, budget time.Duration) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	switch handlerType {
	case HandlerCommand:
		if !r.commandEnabled {
			return Result{}, fmt.Errorf("%w: command hooks are disabled on this instance", ErrCommandDisabled)
		}
		return r.executeCommand(ctx, cfg, ev, hook, budget)
	case HandlerScript:
		if !r.scriptEnabled {
			return Result{}, fmt.Errorf("%w: script hooks are disabled on this instance", ErrScriptDisabled)
		}
		return r.executeScript(ctx, cfg, ev, hook, budget)
	default:
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownHandlerType, handlerType)
	}
}
