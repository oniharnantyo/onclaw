package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/store"
)

// schedulerToolDescription is the model-facing description of the scheduler
// tool; the settings surfaces render the catalog entry's shorter one
// (builtin_tools.go).
const schedulerToolDescription = "Create, list, update and delete cron schedules that run an agent on a prompt unattended. " +
	"Pick one action per call: create (name, prompt and cron required; agent defaults to the calling agent; enabled defaults to true), " +
	"list, update (id required; only the supplied fields change), or delete (id required). " +
	"Cron is a five-field expression in local time, for example 0 9 * * 1-5. " +
	"Validation failures and unknown ids are reported in the result's error field; they do not fail the call."

// newSchedulerTool builds the scheduler invokable tool: a thin client over
// the runtime's schedule service (add-scheduler design D1) whose create
// defaults the target agent to the calling agent (D8). Validation failures,
// unknown ids, and unknown actions come back as readable {ok:false} tool
// results so the model can react and the turn continues; a Go error is
// returned only for infrastructure failures (store I/O).
func newSchedulerTool(svc *scheduler.Service, agentsStore store.AgentStore, callerAgentID string) (tool.BaseTool, error) {
	if svc == nil {
		return nil, errors.New("scheduler: no schedule service configured")
	}
	if agentsStore == nil {
		return nil, errors.New("scheduler: no agent store configured")
	}
	st := &schedulerTool{svc: svc, agents: agentsStore, callerID: callerAgentID}
	return utils.InferTool(ToolScheduler, schedulerToolDescription, st.run)
}

// schedulerTool is the scheduler tool's closed-over dependencies: the one
// cron brain it delegates to, the agent store it resolves names against, and
// the calling agent its create defaults to.
type schedulerTool struct {
	svc      *scheduler.Service
	agents   store.AgentStore
	callerID string
}

// schedulerArgs is the tool's whole input: scalars only (add-scheduler
// design D8), one action per call. Enabled is a pointer so update can tell
// "leave it" (absent) from "disable it" (false).
type schedulerArgs struct {
	Action  string `json:"action"` // create | list | update | delete
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Agent   string `json:"agent,omitempty"` // agent id OR agent name (case-insensitive); defaults to the calling agent on create
	Prompt  string `json:"prompt,omitempty"`
	Cron    string `json:"cron,omitempty"` // five-field, local time
	Enabled *bool  `json:"enabled,omitempty"`
}

// scheduleResult is one schedule's full state as the tool reports it. The
// timestamps are RFC3339 strings in the view's location, empty when unset —
// the tool speaks only scalars to the model.
type scheduleResult struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	AgentID          string `json:"agentId"`
	AgentName        string `json:"agentName,omitempty"`
	Cron             string `json:"cron"`
	Description      string `json:"description"`
	Enabled          bool   `json:"enabled"`
	NextFireAt       string `json:"nextFireAt,omitempty"`
	LastRunAt        string `json:"lastRunAt,omitempty"`
	LastRunStatus    string `json:"lastRunStatus,omitempty"`
	LastRunError     string `json:"lastRunError,omitempty"`
	LastRunSessionID string `json:"lastRunSessionId,omitempty"`
}

// schedulerOutput is the tool's whole output: ok=false with Error for every
// rejected request (validation, unknown id, unknown action), and the
// action-specific payload on success.
type schedulerOutput struct {
	OK        bool             `json:"ok"`
	Error     string           `json:"error,omitempty"`
	Schedule  *scheduleResult  `json:"schedule,omitempty"`
	Schedules []scheduleResult `json:"schedules,omitempty"`
	Deleted   string           `json:"deleted,omitempty"`
}

// run executes one scheduler tool call.
func (t *schedulerTool) run(ctx context.Context, in schedulerArgs) (schedulerOutput, error) {
	switch in.Action {
	case "create":
		return t.create(ctx, in)
	case "list":
		return t.list(ctx)
	case "update":
		return t.update(ctx, in)
	case "delete":
		return t.remove(ctx, in)
	default:
		return schedulerOutput{OK: false,
			Error: fmt.Sprintf("unknown action %q: use one of create, list, update, delete", in.Action),
		}, nil
	}
}

// create builds a new schedule. The agent defaults to the caller; an
// explicit agent resolves by id, then by case-insensitive name. Enabled
// defaults to true. Every rejection (including the service's validation) is
// a readable result.
func (t *schedulerTool) create(ctx context.Context, in schedulerArgs) (schedulerOutput, error) {
	agentID, failure, err := t.resolveAgentID(ctx, in.Agent)
	if err != nil {
		return schedulerOutput{}, err
	}
	if failure != "" {
		return schedulerOutput{OK: false, Error: failure}, nil
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	view, err := t.svc.Create(ctx, scheduler.ScheduleInput{
		Name:    in.Name,
		AgentID: agentID,
		Prompt:  in.Prompt,
		Cron:    in.Cron,
		Enabled: enabled,
	})
	if verr := (*scheduler.ValidationError)(nil); errors.As(err, &verr) {
		return schedulerOutput{OK: false, Error: validationErrorText(verr)}, nil
	}
	if err != nil {
		return schedulerOutput{}, fmt.Errorf("create schedule: %w", err)
	}
	return t.successResult(ctx, view), nil
}

// list reports every schedule with its full state.
func (t *schedulerTool) list(ctx context.Context) (schedulerOutput, error) {
	views, err := t.svc.List(ctx)
	if err != nil {
		return schedulerOutput{}, fmt.Errorf("list schedules: %w", err)
	}
	out := schedulerOutput{OK: true, Schedules: make([]scheduleResult, 0, len(views))}
	for _, view := range views {
		out.Schedules = append(out.Schedules, t.resultFor(ctx, view))
	}
	return out, nil
}

// update rewrites only the supplied fields of the schedule identified by id;
// absent fields keep their stored values. Unknown ids are readable results.
func (t *schedulerTool) update(ctx context.Context, in schedulerArgs) (schedulerOutput, error) {
	if in.ID == "" {
		return schedulerOutput{OK: false, Error: "id: id is required for update"}, nil
	}
	existing, err := t.svc.Get(ctx, in.ID)
	if errors.Is(err, store.ErrNotFound) {
		return schedulerOutput{OK: false, Error: fmt.Sprintf("no schedule with id %s", in.ID)}, nil
	}
	if err != nil {
		return schedulerOutput{}, fmt.Errorf("get schedule %s: %w", in.ID, err)
	}

	merged := scheduler.ScheduleInput{
		Name:    existing.Name,
		AgentID: existing.AgentID,
		Prompt:  existing.Prompt,
		Cron:    existing.Cron,
		Enabled: existing.Enabled,
	}
	if in.Name != "" {
		merged.Name = in.Name
	}
	if in.Prompt != "" {
		merged.Prompt = in.Prompt
	}
	if in.Cron != "" {
		merged.Cron = in.Cron
	}
	if in.Agent != "" {
		agentID, failure, err := t.resolveAgentID(ctx, in.Agent)
		if err != nil {
			return schedulerOutput{}, err
		}
		if failure != "" {
			return schedulerOutput{OK: false, Error: failure}, nil
		}
		merged.AgentID = agentID
	}
	if in.Enabled != nil {
		merged.Enabled = *in.Enabled
	}

	view, err := t.svc.Update(ctx, in.ID, merged)
	if verr := (*scheduler.ValidationError)(nil); errors.As(err, &verr) {
		return schedulerOutput{OK: false, Error: validationErrorText(verr)}, nil
	}
	if errors.Is(err, store.ErrNotFound) {
		// The schedule vanished between the Get and the Update; same readable
		// result as the Get-time check.
		return schedulerOutput{OK: false, Error: fmt.Sprintf("no schedule with id %s", in.ID)}, nil
	}
	if err != nil {
		return schedulerOutput{}, fmt.Errorf("update schedule %s: %w", in.ID, err)
	}
	return t.successResult(ctx, view), nil
}

// remove deletes the schedule identified by id; an unknown id is a readable
// result.
func (t *schedulerTool) remove(ctx context.Context, in schedulerArgs) (schedulerOutput, error) {
	if in.ID == "" {
		return schedulerOutput{OK: false, Error: "id: id is required for delete"}, nil
	}
	if err := t.svc.Delete(ctx, in.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return schedulerOutput{OK: false, Error: fmt.Sprintf("no schedule with id %s", in.ID)}, nil
		}
		return schedulerOutput{}, fmt.Errorf("delete schedule %s: %w", in.ID, err)
	}
	return schedulerOutput{OK: true, Deleted: in.ID}, nil
}

// resolveAgentID maps the tool's agent argument onto an agent id: empty
// means the calling agent; otherwise the value is tried as an id, then as a
// case-insensitive name. Rejections come back as a readable failure text
// naming the field (the caller returns it as the tool result); store I/O
// failures are Go errors.
func (t *schedulerTool) resolveAgentID(ctx context.Context, agent string) (agentID, failure string, err error) {
	if agent == "" {
		agent = t.callerID
	}
	if agent == "" {
		return "", "agent: agent is required", nil
	}
	found, err := t.agents.ByID(ctx, agent)
	if err == nil {
		return found.ID, "", nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return "", "", fmt.Errorf("look up agent %s: %w", agent, err)
	}
	all, err := t.agents.List(ctx)
	if err != nil {
		return "", "", fmt.Errorf("list agents: %w", err)
	}
	matches := make([]store.Agent, 0, 1)
	for _, a := range all {
		if strings.EqualFold(a.Name, agent) {
			matches = append(matches, a)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0].ID, "", nil
	case 0:
		return "", fmt.Sprintf("agent: no agent with id or name %q", agent), nil
	default:
		return "", fmt.Sprintf("agent: the name %q matches %d agents — use the agent id", agent, len(matches)), nil
	}
}

// successResult renders a created or updated view, resolving the agent name
// for the model.
func (t *schedulerTool) successResult(ctx context.Context, view scheduler.ScheduleView) schedulerOutput {
	return schedulerOutput{OK: true, Schedule: ptr(t.resultFor(ctx, view))}
}

// resultFor renders one view into the tool's result shape. The agent name
// best-effort degrades to empty when its row cannot be read — the list must
// report every schedule even if one agent row is unreadable.
func (t *schedulerTool) resultFor(ctx context.Context, view scheduler.ScheduleView) scheduleResult {
	agentName := ""
	if agent, err := t.agents.ByID(ctx, view.AgentID); err == nil {
		agentName = agent.Name
	}
	return scheduleResult{
		ID:               view.ID,
		Name:             view.Name,
		AgentID:          view.AgentID,
		AgentName:        agentName,
		Cron:             view.Cron,
		Description:      view.Description,
		Enabled:          view.Enabled,
		NextFireAt:       rfc3339(view.NextFireAt),
		LastRunAt:        rfc3339(view.LastRunAt),
		LastRunStatus:    view.LastRunStatus,
		LastRunError:     view.LastRunError,
		LastRunSessionID: view.LastRunSessionID,
	}
}

// validationErrorText renders a rejected write: the service's message, plus
// the update hint for a duplicate name (add-scheduler design D8).
func validationErrorText(verr *scheduler.ValidationError) string {
	text := verr.Error()
	if verr.Field == "name" && verr.ExistingID != "" {
		text += fmt.Sprintf(" — use action \"update\" with id %s", verr.ExistingID)
	}
	return text
}

// rfc3339 renders a nullable timestamp as an RFC3339 string in the time's
// own location, empty when unset.
func rfc3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// ptr is a small helper for the one-pointer output field.
func ptr[T any](v T) *T { return &v }
