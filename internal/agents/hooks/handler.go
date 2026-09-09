package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Registry errors. Callers (the dispatcher, the REST test endpoint) branch on
// these sentinels:
//
//   - ErrUnknownHandlerType: the type is not in the domain catalog at all —
//     stored data this binary is too old to interpret (D7 graceful skip).
//   - ErrHandlerNotRegistered: a known type whose dependencies were not wired
//     into this registry (mcp_tool without an MCPInvoker, prompt without an
//     evaluator factory) — also a graceful skip.
//   - ErrCommandDisabled: the command handler is switched off for this
//     instance (D10 kill switch) — a skip, not a failure policy.
//   - ErrScriptDisabled: the script handler is switched off for this
//     instance (D22 kill switch) — same shape as ErrCommandDisabled.
//   - ErrSpawnFailed: the command handler could not start the program —
//     callers flip the hook's status to error on top of on_failure.
var (
	ErrUnknownHandlerType   = errors.New("unknown handler type")
	ErrHandlerNotRegistered = errors.New("hook handler not registered")
	ErrCommandDisabled      = errors.New("command handler disabled")
	ErrScriptDisabled       = errors.New("script handler disabled")
	ErrSpawnFailed          = errors.New("hook command spawn failed")
)

// Registry resolves handler types to Handler implementations and applies the
// shared execution contract: config opening, budget enforcement, and the
// no-decision-means-allow rule. http and command are always registered;
// mcp_tool registers when an MCPInvoker is wired (WithMCPInvoker) and prompt
// when an evaluator factory is wired (WithEvaluatorFactory) — unwired types
// report ErrHandlerNotRegistered so upstream can skip gracefully
// (plugins-first: registrations, not edits).
type Registry struct {
	encKey           []byte
	httpClient       *http.Client
	httpAllowPrivate bool
	commandEnabled   bool
	scriptEnabled    bool
	mcpInvoker       MCPInvoker
	evaluatorFactory EvaluatorModelFactory
}

// RegistryOption configures the registry.
type RegistryOption func(*Registry)

// WithEncryptionKey sets the instance AES key used to open sealed hook
// configs. Configs whose secrets are plaintext open unchanged, so tests may
// omit the key; production always injects it.
func WithEncryptionKey(key []byte) RegistryOption {
	return func(r *Registry) { r.encKey = key }
}

// WithHTTPClient overrides the webhook HTTP client (testing seam). The
// production default is the SSRF-guarded client shared with web.fetch.
func WithHTTPClient(c *http.Client) RegistryOption {
	return func(r *Registry) { r.httpClient = c }
}

// WithHTTPAllowPrivate opts the http handler out of the outbound-fetch SSRF
// guard (testing seam and the ONCLAW_FETCH_ALLOW_PRIVATE-style local-dev
// escape hatch; mirrors tools.WithFetchAllowPrivate). Default is guarded.
func WithHTTPAllowPrivate(v bool) RegistryOption {
	return func(r *Registry) { r.httpAllowPrivate = v }
}

// WithCommandEnabled toggles the command handler (D10 operator kill switch,
// ONCLAW_HOOKS_COMMAND_ENABLED; default on).
func WithCommandEnabled(v bool) RegistryOption {
	return func(r *Registry) { r.commandEnabled = v }
}

// WithScriptEnabled toggles the script handler (D22 operator kill switch,
// ONCLAW_HOOKS_SCRIPT_ENABLED; default on).
func WithScriptEnabled(v bool) RegistryOption {
	return func(r *Registry) { r.scriptEnabled = v }
}

// WithMCPInvoker registers the mcp_tool handler against the given workspace
// MCP tool invoker (D11). Absent (or nil), mcp_tool hooks stay
// ErrHandlerNotRegistered — a graceful skip upstream.
func WithMCPInvoker(inv MCPInvoker) RegistryOption {
	return func(r *Registry) { r.mcpInvoker = inv }
}

// WithEvaluatorFactory registers the prompt handler against the given
// sandboxed-evaluator model factory (D12). Absent (or nil), prompt hooks stay
// ErrHandlerNotRegistered — a graceful skip upstream.
func WithEvaluatorFactory(f EvaluatorModelFactory) RegistryOption {
	return func(r *Registry) { r.evaluatorFactory = f }
}

// NewRegistry builds the registry with production defaults: guarded webhook
// client, command and script handlers enabled.
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{
		commandEnabled: true,
		scriptEnabled:  true,
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.httpClient == nil {
		r.httpClient = tools.NewFetchHTTPClient(r.httpAllowPrivate)
	}
	return r
}

// Execute runs one hook handler delivery within the given budget. The config
// is the STORED form: sealed secret values are opened here with the event's
// workspace ID as AAD, so handlers always receive decrypted config. A
// successful execution that produces no decision object resolves to allow;
// returned errors belong to the caller's on_failure policy.
func (r *Registry) Execute(ctx context.Context, handlerType domain.HookHandlerType, cfg json.RawMessage, ev Event, hook HookRef, budget time.Duration) (Result, error) {
	decrypted, err := OpenHookConfig(r.encKey, ev.Workspace.ID, cfg)
	if err != nil {
		return Result{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	switch handlerType {
	case domain.HookHandlerHTTP:
		return r.executeHTTP(ctx, decrypted, ev, hook, budget)
	case domain.HookHandlerCommand:
		if !r.commandEnabled {
			return Result{}, fmt.Errorf("%w: command hooks are disabled on this instance", ErrCommandDisabled)
		}
		return r.executeCommand(ctx, decrypted, ev, hook, budget)
	case domain.HookHandlerScript:
		if !r.scriptEnabled {
			return Result{}, fmt.Errorf("%w: script hooks are disabled on this instance", ErrScriptDisabled)
		}
		return r.executeScript(ctx, decrypted, ev, hook, budget)
	case domain.HookHandlerMCPTool:
		if r.mcpInvoker == nil {
			return Result{}, fmt.Errorf("%w: %q (no MCP invoker wired)", ErrHandlerNotRegistered, handlerType)
		}
		return r.executeMCPTool(ctx, decrypted, ev, hook, budget)
	case domain.HookHandlerPrompt:
		if r.evaluatorFactory == nil {
			return Result{}, fmt.Errorf("%w: %q (no evaluator factory wired)", ErrHandlerNotRegistered, handlerType)
		}
		return r.executePrompt(ctx, decrypted, ev, hook, budget)
	default:
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownHandlerType, handlerType)
	}
}
