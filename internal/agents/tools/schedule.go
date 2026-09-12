package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// NameSchedule is the dotted capability name registered in the tool registry.
// Scheduler-origin runs strip it by this name (runner's schedulerExcludedTools)
// — a scheduled run must never mint schedulers.
const NameSchedule = "schedule"

// ScheduleChannelMembers is the narrow consumer-side port the schedule tool's
// channel-target rule consults (integrate-scheduler 6.2): creating or updating
// a scheduler with channel delivery requires the acting agent to sit on the
// target channel's roster. store.ChannelStore and the channel chokepoint both
// satisfy it structurally.
type ScheduleChannelMembers interface {
	ListChannelMembers(ctx context.Context, workspaceID, channelID string) ([]domain.ChannelMember, error)
}

// Scoping is structural, like the memory tool: the tool is constructed with the
// executing run's workspace, agent, and user identity plus the workspace-local
// timezone, so no argument can create for another principal. The bound agent
// owns every schedule it creates; anti-runaway lets it update and delete only
// its own schedules, while list shows the whole workspace read-only.
type scheduleTool struct {
	schedulers  store.SchedulerStore
	members     ScheduleChannelMembers
	workspaceID string
	agentID     string
	userID      string
	workspaceTZ *time.Location
}

// NewSchedule constructs the schedule built-in bound to the executing run's
// identity. The membership source backs the channel-target rule; the store
// persists through the same SchedulerStore the HTTP surface uses.
func NewSchedule(schedulers store.SchedulerStore, members ScheduleChannelMembers, workspaceID, agentID, userID string, workspaceTZ *time.Location) (tool.BaseTool, error) {
	return &scheduleTool{
		schedulers:  schedulers,
		members:     members,
		workspaceID: workspaceID,
		agentID:     agentID,
		userID:      userID,
		workspaceTZ: workspaceTZ,
	}, nil
}

// Info returns the tool schema surfaced to agentic models. The description is
// the only instruction surface guaranteed to reach every agent — it carries
// the full contract.
func (t *scheduleTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameSchedule,
		Desc: "Create, list, update, and delete schedules: named standing orders that run you automatically on a recurrence or at a one-shot time, with no user present. " +
			"Actions: create schedules a task prompt under a unique name — kind recurring takes a standard 5-field cron expression evaluated in the workspace timezone, kind once takes run_at as an RFC3339 instant that must be in the future; delivery defaults to your thread, or names a channel you are a member of via channel_id. " +
			"list returns every schedule in the workspace (read-only). " +
			"update and delete address a schedule by its id from list — but only schedules bound to you; other agents' schedules are off-limits. " +
			"The task prompt is free text describing what to do when the schedule fires. " +
			"Changing kind, expression, run_at, or enabled recomputes the next fire time from now; disabling pauses firing while keeping everything else. " +
			"Schedule names are unique per agent — a taken name is rejected.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     `One of "create", "list", "update", or "delete".`,
				Required: true,
			},
			"name": {
				Type: schema.String,
				Desc: "Unique-per-agent schedule name, e.g. \"morning-digest\". Required for create; optional new name for update.",
			},
			"prompt": {
				Type: schema.String,
				Desc: "The task prompt — free text describing what to do each time the schedule fires. Required for create; optional replacement for update.",
			},
			"kind": {
				Type: schema.String,
				Desc: `"recurring" (fires on a cron expression) or "once" (fires at run_at). Required for create; optional switch for update.`,
			},
			"expression": {
				Type: schema.String,
				Desc: `Standard 5-field cron expression, e.g. "0 9 * * 1-5" (weekdays at 09:00 workspace time). Required for recurring create; optional replacement for update.`,
			},
			"run_at": {
				Type: schema.String,
				Desc: `One-shot target instant, RFC3339 with offset, e.g. "2026-09-12T09:00:00+07:00". Required for once create and must be in the future; optional replacement for update.`,
			},
			"delivery": {
				Type: schema.String,
				Desc: `Where each run's result goes: "thread" (default — the run's own transcript) or "channel" (the final reply is posted into channel_id).`,
			},
			"channel_id": {
				Type: schema.String,
				Desc: "Target channel id for channel delivery. You must be a member of the channel.",
			},
			"enabled": {
				Type: schema.Boolean,
				Desc: `Update only: false pauses firing, true resumes it.`,
			},
			"id": {
				Type: schema.String,
				Desc: "The schedule id to update or delete, taken from a previous list result.",
			},
		}),
	}, nil
}

// scheduleArgs is the deserialized tool-call argument shape. Update fields are
// pointers so "provided" is distinguishable from "left unchanged".
type scheduleArgs struct {
	Action     string  `json:"action"`
	ID         string  `json:"id"`
	Name       *string `json:"name"`
	Prompt     *string `json:"prompt"`
	Kind       *string `json:"kind"`
	Expression *string `json:"expression"`
	RunAt      *string `json:"run_at"`
	Delivery   *string `json:"delivery"`
	ChannelID  *string `json:"channel_id"`
	Enabled    *bool   `json:"enabled"`
}

func (a scheduleArgs) str(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// InvokableRun satisfies tool.InvokableTool. Failures return errors: the
// runtime's tool-error middleware converts them into JSON error results the
// model reads, so the run continues.
func (t *scheduleTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args scheduleArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("schedule: %w", err)
	}

	switch args.Action {
	case "create":
		return t.create(ctx, args)
	case "list":
		return t.list(ctx)
	case "update":
		return t.update(ctx, args)
	case "delete":
		return t.remove(ctx, args)
	default:
		return "", errors.New(`schedule: action must be "create", "list", "update", or "delete"`)
	}
}

// create binds the new schedule to the acting agent, workspace, and user,
// validates through the shared domain validator (the same one the API uses),
// and persists via the scheduler store. Name conflicts surface as tool errors
// naming the name.
func (t *scheduleTool) create(ctx context.Context, args scheduleArgs) (string, error) {
	s := &domain.Scheduler{
		WorkspaceID: t.workspaceID,
		AgentID:     t.agentID,
		Name:        args.str(args.Name),
		Prompt:      args.str(args.Prompt),
		Kind:        args.str(args.Kind),
		Expr:        args.str(args.Expression),
		Delivery: domain.SchedulerDelivery{
			Type:      args.str(args.Delivery),
			ChannelID: args.str(args.ChannelID),
		},
		Enabled: true,
	}
	s.CreatedBy = &t.userID
	if s.Name == "" {
		return "", errors.New("schedule: name is required to create a schedule")
	}
	if s.Prompt == "" {
		return "", errors.New("schedule: prompt is required to create a schedule")
	}
	if s.Kind == "" {
		return "", errors.New(`schedule: kind is required to create a schedule — "recurring" or "once"`)
	}
	runAt, err := parseRunAt(args.RunAt, s.Kind == domain.SchedulerKindOnce)
	if err != nil {
		return "", err
	}
	s.RunAt = runAt

	now := time.Now()
	if err := domain.ValidateScheduler(s, now, true); err != nil {
		return "", fmt.Errorf("schedule: %w", err)
	}
	if err := t.ensureChannelMembership(ctx, s.Delivery); err != nil {
		return "", err
	}
	if err := t.schedulers.CreateScheduler(ctx, t.workspaceID, s); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return "", fmt.Errorf("schedule: %w — pick a different name", err)
		}
		return "", fmt.Errorf("schedule: create %s: %w", s.Name, err)
	}
	return scheduleCreatedResult(s, now, t.workspaceTZ)
}

// list is workspace-wide and read-only: every schedule in the acting
// workspace, each carrying its id, name, bound agent, human label, next fire
// time, enabled flag, and last run status.
func (t *scheduleTool) list(ctx context.Context) (string, error) {
	rows, err := t.schedulers.ListSchedulers(ctx, t.workspaceID)
	if err != nil {
		return "", fmt.Errorf("schedule: list: %w", err)
	}
	type entry struct {
		ID         string     `json:"id"`
		Name       string     `json:"name"`
		AgentID    string     `json:"agent_id"`
		Schedule   string     `json:"schedule"`
		NextRunAt  *time.Time `json:"next_run_at"`
		Enabled    bool       `json:"enabled"`
		LastStatus string     `json:"last_status"`
	}
	entries := make([]entry, 0, len(rows))
	for _, s := range rows {
		status := ""
		if s.LastRun != nil {
			status = s.LastRun.Status
		}
		entries = append(entries, entry{
			ID:         s.ID,
			Name:       s.Name,
			AgentID:    s.AgentID,
			Schedule:   scheduleLabel(&s, t.workspaceTZ),
			NextRunAt:  s.NextRunAt,
			Enabled:    s.Enabled,
			LastStatus: status,
		})
	}
	result := fmt.Sprintf("%d schedules in this workspace (list is read-only — update and delete only schedules bound to you)", len(entries))
	if len(entries) == 0 {
		result = "No schedules in this workspace yet."
	}
	out, err := json.Marshal(map[string]any{"schedules": entries, "result": result})
	if err != nil {
		return "", fmt.Errorf("schedule: encode result: %w", err)
	}
	return string(out), nil
}

// update loads by workspace+id, rejects other agents' schedules, applies the
// provided fields, revalidates (creating=false: a once schedule whose run_at
// is unchanged and past still passes), recomputes the next fire time for
// schedule-affecting changes, and persists.
func (t *scheduleTool) update(ctx context.Context, args scheduleArgs) (string, error) {
	if args.ID == "" {
		return "", errors.New("schedule: id is required to update a schedule")
	}
	s, err := t.schedulers.GetScheduler(ctx, t.workspaceID, args.ID)
	if err != nil {
		return "", fmt.Errorf("schedule: load %s: %w", args.ID, err)
	}
	if s == nil {
		return "", fmt.Errorf("schedule: no schedule %s in this workspace — list first", args.ID)
	}
	if s.AgentID != t.agentID {
		return "", fmt.Errorf("schedule: %q is bound to another agent — you can only update schedules bound to you", s.Name)
	}

	scheduleChanged := false
	if args.Name != nil {
		s.Name = *args.Name
	}
	if args.Prompt != nil {
		s.Prompt = *args.Prompt
	}
	if args.Kind != nil && *args.Kind != s.Kind {
		s.Kind = *args.Kind
		scheduleChanged = true
	}
	if args.Expression != nil {
		s.Expr = *args.Expression
		scheduleChanged = true
	}
	if args.RunAt != nil {
		parsed, err := parseRunAt(args.RunAt, s.Kind == domain.SchedulerKindOnce)
		if err != nil {
			return "", err
		}
		if s.RunAt == nil || !parsed.Equal(*s.RunAt) {
			s.RunAt = parsed
			scheduleChanged = true
		}
	}
	if args.Delivery != nil {
		s.Delivery.Type = *args.Delivery
	}
	if args.ChannelID != nil {
		s.Delivery.ChannelID = *args.ChannelID
	}
	if args.Enabled != nil && *args.Enabled != s.Enabled {
		s.Enabled = *args.Enabled
		scheduleChanged = true
	}

	now := time.Now()
	if err := domain.ValidateScheduler(s, now, false); err != nil {
		return "", fmt.Errorf("schedule: %w", err)
	}
	if err := t.ensureChannelMembership(ctx, s.Delivery); err != nil {
		return "", err
	}
	if scheduleChanged {
		s.NextRunAt = computeNextRunAt(s, now, t.workspaceTZ)
	}
	if err := t.schedulers.UpdateScheduler(ctx, t.workspaceID, s); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return "", fmt.Errorf("schedule: %w — pick a different name", err)
		}
		return "", fmt.Errorf("schedule: update %s: %w", s.Name, err)
	}
	out, err := json.Marshal(map[string]any{
		"id":          s.ID,
		"name":        s.Name,
		"schedule":    scheduleLabel(s, t.workspaceTZ),
		"next_run_at": s.NextRunAt,
		"enabled":     s.Enabled,
		"result":      fmt.Sprintf("Updated schedule %q.", s.Name),
	})
	if err != nil {
		return "", fmt.Errorf("schedule: encode result: %w", err)
	}
	return string(out), nil
}

// remove deletes by workspace+id after the cross-agent guard; run records die
// with the schedule in the store.
func (t *scheduleTool) remove(ctx context.Context, args scheduleArgs) (string, error) {
	if args.ID == "" {
		return "", errors.New("schedule: id is required to delete a schedule")
	}
	s, err := t.schedulers.GetScheduler(ctx, t.workspaceID, args.ID)
	if err != nil {
		return "", fmt.Errorf("schedule: load %s: %w", args.ID, err)
	}
	if s == nil {
		return "", fmt.Errorf("schedule: no schedule %s in this workspace — list first", args.ID)
	}
	if s.AgentID != t.agentID {
		return "", fmt.Errorf("schedule: %q is bound to another agent — you can only delete schedules bound to you", s.Name)
	}
	if err := t.schedulers.DeleteScheduler(ctx, t.workspaceID, args.ID); err != nil {
		return "", fmt.Errorf("schedule: delete %s: %w", s.Name, err)
	}
	out, err := json.Marshal(map[string]string{
		"id":     s.ID,
		"result": fmt.Sprintf("Deleted schedule %q — it will not fire again.", s.Name),
	})
	if err != nil {
		return "", fmt.Errorf("schedule: encode result: %w", err)
	}
	return string(out), nil
}

// ensureChannelMembership applies the tool-layer rule (integrate-scheduler
// 6.2): channel delivery requires the acting agent to sit on the target
// channel's roster. The rejection names the channel.
func (t *scheduleTool) ensureChannelMembership(ctx context.Context, delivery domain.SchedulerDelivery) error {
	if delivery.Type != domain.SchedulerDeliveryChannel {
		return nil
	}
	roster, err := t.members.ListChannelMembers(ctx, t.workspaceID, delivery.ChannelID)
	if err != nil {
		return fmt.Errorf("schedule: read the roster of channel %s: %w", delivery.ChannelID, err)
	}
	for _, m := range roster {
		if m.MemberType == domain.ChannelMemberTypeAgent && m.AgentID == t.agentID {
			return nil
		}
	}
	return fmt.Errorf("schedule: channel delivery requires membership — you are not a member of channel %s; ask an admin to add you, or deliver to your thread instead", delivery.ChannelID)
}

// parseRunAt decodes an RFC3339 one-shot instant; required enforces presence
// for once-kind creates and once-kind switches.
func parseRunAt(v *string, required bool) (*time.Time, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		if required {
			return nil, errors.New("schedule: run_at is required for a one-shot scheduler (RFC3339, e.g. 2026-09-12T09:00:00+07:00)")
		}
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, *v)
	if err != nil {
		return nil, fmt.Errorf("schedule: run_at %q must be an RFC3339 instant (e.g. 2026-09-12T09:00:00+07:00): %v", *v, err)
	}
	return &parsed, nil
}

// computeNextRunAt derives the next fire time from the current time (the same
// rule the stores apply on write): paused schedules carry no next fire,
// one-shots fire at their instant, recurring schedules at their next
// workspace-time occurrence.
func computeNextRunAt(s *domain.Scheduler, now time.Time, tz *time.Location) *time.Time {
	if !s.Enabled {
		return nil
	}
	if s.Kind == domain.SchedulerKindOnce {
		return s.RunAt
	}
	next, err := domain.NextRun(s.Expr, now, tz)
	if err != nil {
		return nil
	}
	return next
}

// scheduleLabel renders the human-readable schedule description: the derived
// cron label for recurring schedules, the workspace-local target instant for
// one-shots — never user-entered.
func scheduleLabel(s *domain.Scheduler, tz *time.Location) string {
	if s.Kind == domain.SchedulerKindOnce && s.RunAt != nil {
		return s.RunAt.In(tz).Format(time.RFC3339)
	}
	return domain.HumanLabel(s.Expr)
}

// scheduleCreatedResult encodes the create confirmation naming the schedule,
// its human label, and the next fire time (compute via domain.NextRun for the
// reply; a one-shot names its run_at).
func scheduleCreatedResult(s *domain.Scheduler, now time.Time, tz *time.Location) (string, error) {
	next := computeNextRunAt(s, now, tz)
	fire := "the first time it comes due"
	if next != nil {
		fire = next.Format(time.RFC3339)
	}
	out, err := json.Marshal(map[string]any{
		"id":          s.ID,
		"name":        s.Name,
		"kind":        s.Kind,
		"schedule":    scheduleLabel(s, tz),
		"next_run_at": next,
		"delivery":    s.Delivery.Type,
		"result":      fmt.Sprintf("Created schedule %q (%s) — it fires next at %s.", s.Name, scheduleLabel(s, tz), fire),
	})
	if err != nil {
		return "", fmt.Errorf("schedule: encode result: %w", err)
	}
	return string(out), nil
}
