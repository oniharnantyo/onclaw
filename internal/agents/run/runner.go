package run

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/compose"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// ErrTurnCancelled reports that an in-flight turn was cancelled.
var ErrTurnCancelled = errors.New("agents: turn was cancelled")

// ErrTurnParked reports that a turn parked on a pending human approval.
var ErrTurnParked = errors.New("agents: turn parked on approval")

// DrainValues carries the intermediate and terminal states observed by the event drain.
type DrainValues struct {
	Usage      *sessions.Usage
	Partial    string
	ToolErrors *compose.ToolErrorResult
	Terminal   *sessions.Event
	Error      error
	Cancelled  bool
	Parked     bool
}

// RunningTurn holds the in-flight state of a turn being executed.
type RunningTurn struct {
	Turn       *Turn
	SessionID  string
	Agent      adk.Agent
	ToolErrors *compose.ToolErrorResult
	Usage      *sessions.Usage
	Args       map[string]string
	Origin     string

	Ctx             context.Context
	Cancel          context.CancelFunc
	ADKCancel       adk.AgentCancelFunc
	CancelRequested atomic.Bool
	Partial         string
	VariantOf       string
}

// NewRunningTurn constructs a new RunningTurn wrapper.
func NewRunningTurn(turn *Turn, sessionID string, ctx context.Context, cancel context.CancelFunc) *RunningTurn {
	return &RunningTurn{
		Turn:      turn,
		SessionID: sessionID,
		Usage:     &sessions.Usage{},
		Args:      make(map[string]string),
		Ctx:       ctx,
		Cancel:    cancel,
	}
}

// RunnerDependencies provides the services and stores needed to execute turns.
type RunnerDependencies struct {
	Store       store.Store
	Sessions    sessions.Service
	Workspaces  any
	BrowserDir  string
	Attachments any
}

// Runner manages and executes chat turns on the eino framework.
type Runner struct {
	mu   sync.Mutex
	deps RunnerDependencies
}

// NewRunner creates a new turn runner.
func NewRunner(deps RunnerDependencies) *Runner {
	return &Runner{deps: deps}
}

// ExecuteTurn runs one agent turn to completion using the supplied composition.
func (r *Runner) ExecuteTurn(
	ctx context.Context,
	rt *RunningTurn,
	agent adk.Agent,
	input []*schema.Message,
) error {
	if rt == nil || agent == nil {
		return errors.New("run: running turn and agent are required")
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
		SessionID:       rt.SessionID,
	})

	iter := runner.Run(ctx, input)
	for {
		chunk, ok := iter.Next()
		if !ok {
			break
		}
		_ = chunk
	}
	return nil
}
