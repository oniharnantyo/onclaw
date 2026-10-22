package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/store"
)

// HookTestResult is the outcome of one hook dry run: the handler's decision
// (allow/block/failure), the reason when it produced one, whether the
// handler itself succeeded, and the handler-specific detail (command output
// or the script handler's captured console lines).
type HookTestResult struct {
	Success      bool
	Decision     string
	Reason       string
	Output       string
	Err          string
	ConsoleLines []string
	DurationMS   int64
}

// defaultHookTestTimeout bounds a dry run when the hook sets no TimeoutMS of
// its own.
const defaultHookTestTimeout = 10 * time.Second

// hookTestEvents are the events a dry run may synthesize: the full lifecycle
// vocabulary, so a hook bound to any of the five events can be tested.
func isHookEvent(event string) bool {
	for _, e := range hooks.Events {
		if e == event {
			return true
		}
	}
	return false
}

// TestHook performs a sample-event dry run of the hook addressed by id: a
// real handler execution (both supported handler types) against a synthetic
// event, returning the decision, reason, timing, and handler detail. Nothing
// is persisted — the dry run never appends to the execution audit.
func (s *SettingsService) TestHook(ctx context.Context, id string) (HookTestResult, error) {
	rows, err := s.store.Hooks().List(ctx)
	if err != nil {
		return HookTestResult{}, fmt.Errorf("list hooks: %w", err)
	}
	var hook *store.Hook
	for i := range rows {
		if rows[i].ID == id {
			hook = &rows[i]
			break
		}
	}
	if hook == nil {
		return HookTestResult{}, fmt.Errorf("get hook %s: %w", id, store.ErrNotFound)
	}

	// Unknown handler types and events are a validation-style rejection, not
	// a fake failure result: the row predates this binary or was hand-edited.
	if !hookTestHandlerSupported(hook.HandlerType) {
		return HookTestResult{}, fmt.Errorf("%w: unsupported handler type %q", errHookInvalid, hook.HandlerType)
	}
	if !isHookEvent(hook.Event) {
		return HookTestResult{}, fmt.Errorf("%w: unknown event %q", errHookInvalid, hook.Event)
	}
	if err := validateHookConfig(hook.HandlerType, hook.Config); err != nil {
		return HookTestResult{}, err
	}

	event := sampleEvent(hook.Event)
	registry := hooks.NewRegistry()
	budget := defaultHookTestTimeout
	if hook.TimeoutMS > 0 {
		budget = time.Duration(hook.TimeoutMS) * time.Millisecond
	}

	started := time.Now()
	result, execErr := registry.Execute(ctx, hook.HandlerType, json.RawMessage(hook.Config), event,
		hooks.HookRef{Name: hook.Name}, budget)
	duration := time.Since(started)

	out := HookTestResult{
		DurationMS:   duration.Milliseconds(),
		ConsoleLines: result.ConsoleLines,
	}
	switch {
	case execErr != nil:
		out.Decision = "failure"
		out.Reason = result.Reason
		out.Err = execErr.Error()
	case result.Decision == hooks.DecisionBlock:
		out.Success = true
		out.Decision = "block"
		out.Reason = result.Reason
	default:
		out.Success = true
		out.Decision = "allow"
		out.Reason = result.Reason
	}
	if !out.Success {
		if out.Err == "" {
			out.Err = "handler failed"
		}
	}
	return out, nil
}

// sampleEvent synthesizes the dry-run event for one event name: fixed
// identity ("hook-test"), user origin, completed status; tool events carry
// a representative tool call so matchers and handlers have data to act on.
func sampleEvent(event string) hooks.Event {
	ev := hooks.Event{
		Event:      event,
		DeliveryID: "hook-test",
		Origin:     "user",
		Status:     "completed",
		Agent:      hooks.EventRef{ID: "hook-test", Name: "hook-test"},
		SessionID:  "hook-test",
	}
	if event == hooks.EventPreToolUse || event == hooks.EventPostToolUse {
		ev.Tool = &hooks.EventTool{Name: "sample_tool", CallID: "hook-test", Args: `{}`}
	}
	return ev
}

// hookTestHandlerSupported reports whether a dry run can execute the type.
func hookTestHandlerSupported(handlerType string) bool {
	for _, t := range hooks.HandlerTypes {
		if t == handlerType {
			return true
		}
	}
	return false
}

// errHookInvalid is the sentinel behind hook validation and dry-run
// rejections; callers test it with errors.Is.
var errHookInvalid = hooks.ErrInvalid

// validateHookDefinition validates a hook definition before it is saved
// (spec settings "Hooks pane"): the event must be one of the five lifecycle
// events, the handler type must be command or script, and the config must
// satisfy that handler's shape (a non-empty command; a script that compiles
// with the (function(input){ … }) wrapper intact).
func validateHookDefinition(hook store.Hook) error {
	if !isHookEvent(hook.Event) {
		return fmt.Errorf("%w: unknown event %q", hooks.ErrInvalid, hook.Event)
	}
	return validateHookConfig(hook.HandlerType, hook.Config)
}

// validateHookConfig validates the handler type and config shape alone —
// shared by save-time validation and the Test dry run.
func validateHookConfig(handlerType, config string) error {
	switch handlerType {
	case hooks.HandlerCommand:
		var conf struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(config), &conf); err != nil {
			return fmt.Errorf("%w: config is not valid JSON: %v", hooks.ErrInvalid, err)
		}
		if conf.Command == "" {
			return fmt.Errorf("%w: config.command is required", hooks.ErrInvalid)
		}
	case hooks.HandlerScript:
		var conf struct {
			Script string `json:"script"`
		}
		if err := json.Unmarshal([]byte(config), &conf); err != nil {
			return fmt.Errorf("%w: config is not valid JSON: %v", hooks.ErrInvalid, err)
		}
		if err := hooks.ValidateScript(conf.Script); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unsupported handler type %q (supported: command, script)", hooks.ErrInvalid, handlerType)
	}
	return nil
}

// CreateHook stores a new hook definition, assigning its ID and timestamps.
// A duplicate hook name is an error; unknown events, unsupported handler
// types, and invalid configs are rejected before the write.
func (s *SettingsService) CreateHook(ctx context.Context, hook store.Hook) (store.Hook, error) {
	if err := validateHookDefinition(hook); err != nil {
		return store.Hook{}, err
	}
	if err := s.store.Hooks().Create(ctx, &hook); err != nil {
		return store.Hook{}, fmt.Errorf("create hook: %w", err)
	}
	return hook, nil
}

// Hooks returns all hooks ordered by position then name. The result is never
// nil.
func (s *SettingsService) Hooks(ctx context.Context) ([]store.Hook, error) {
	hooksRows, err := s.store.Hooks().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list hooks: %w", err)
	}
	return hooksRows, nil
}

// UpdateHook rewrites an existing hook definition, addressed by hook.ID;
// store.ErrNotFound when absent. Validation matches CreateHook.
func (s *SettingsService) UpdateHook(ctx context.Context, hook store.Hook) (store.Hook, error) {
	if err := validateHookDefinition(hook); err != nil {
		return store.Hook{}, err
	}
	if err := s.store.Hooks().Update(ctx, &hook); err != nil {
		return store.Hook{}, fmt.Errorf("update hook: %w", err)
	}
	return hook, nil
}

// DeleteHook removes the hook; store.ErrNotFound when absent.
func (s *SettingsService) DeleteHook(ctx context.Context, id string) error {
	if err := s.store.Hooks().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete hook: %w", err)
	}
	return nil
}

// -------------------------------------------------------------------------
// Per-agent hook assignment (design D4)
// -------------------------------------------------------------------------

// AgentHookAssignment is one assignment row as bound to the frontend: the
// hook's id and display name plus its evaluation position for the agent.
type AgentHookAssignment struct {
	HookID   string
	HookName string
	Position int
}

// AgentHooks returns the hooks assigned to an agent, in evaluation order
// (position then name). The result is never nil.
func (s *SettingsService) AgentHooks(ctx context.Context, agentID string) ([]AgentHookAssignment, error) {
	rows, err := s.store.AgentHooks().Assignments(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent hook assignments: %w", err)
	}
	if len(rows) == 0 {
		return []AgentHookAssignment{}, nil
	}
	defs, err := s.store.AgentHooks().HooksForAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list assigned hooks: %w", err)
	}
	names := make(map[string]string, len(defs))
	for _, d := range defs {
		names[d.ID] = d.Name
	}
	out := make([]AgentHookAssignment, 0, len(rows))
	for _, row := range rows {
		out = append(out, AgentHookAssignment{
			HookID:   row.HookID,
			HookName: names[row.HookID],
			Position: row.Position,
		})
	}
	return out, nil
}

// AssignHook links a hook to an agent; assigning a pair already linked
// replaces its position. The hook moves out of the global tier for every
// agent (design D4). Absent agent or hook is store.ErrNotFound.
func (s *SettingsService) AssignHook(ctx context.Context, agentID, hookID string, position int) error {
	if err := s.store.AgentHooks().Assign(ctx, agentID, hookID, position); err != nil {
		return fmt.Errorf("assign hook: %w", err)
	}
	return nil
}

// UnassignHook removes the link; store.ErrNotFound when absent. The hook
// returns to the global tier.
func (s *SettingsService) UnassignHook(ctx context.Context, agentID, hookID string) error {
	if err := s.store.AgentHooks().Unassign(ctx, agentID, hookID); err != nil {
		return fmt.Errorf("unassign hook: %w", err)
	}
	return nil
}

// HookExecutions returns the audit trail, newest first. The result is never
// nil.
func (s *SettingsService) HookExecutions(ctx context.Context) ([]store.HookExecution, error) {
	rows, err := s.store.HookExecutions().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list hook executions: %w", err)
	}
	return rows, nil
}
