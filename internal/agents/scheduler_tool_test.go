package agents

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// noopRuns is the tool tests' no-op scheduler Runs seam: the scheduler tool
// never fires schedules, so no run is ever recorded.
type noopRuns struct{}

func (noopRuns) Create(_ context.Context, _, _ string) (string, error) { return "", nil }
func (noopRuns) Submit(_ context.Context, _, _ string) error           { return nil }

// stubSchedulerGateway reports a fixed gateway state. The tool tests never
// tick, so any state works; Running is the production steady state.
type stubSchedulerGateway struct{}

func (stubSchedulerGateway) State() gateway.State { return gateway.StateRunning }

// schedulerClockNow is the tool tests' fixed "now": Wednesday
// 2026-10-21 08:00 UTC, so next-fire expectations are deterministic.
var schedulerClockNow = time.Date(2026, time.October, 21, 8, 0, 0, 0, time.UTC)

// schedulerFixture wires the scheduler tool over a fresh fake store and a
// scheduler service with the fixed clock, seeding the calling agent ("Alice")
// and a second agent ("Bob") for the by-name resolution tests.
type schedulerFixture struct {
	st     *fake.Store
	svc    *scheduler.Service
	tool   tool.BaseTool
	caller string
}

func newSchedulerFixture(t *testing.T) *schedulerFixture {
	t.Helper()
	st := fake.New()
	svc := scheduler.New(st, noopRuns{}, stubSchedulerGateway{},
		scheduler.WithClock(func() time.Time { return schedulerClockNow }))
	caller := "agent-alice"
	require.NoError(t, st.Agents().Create(t.Context(), &store.Agent{ID: caller, Name: "Alice"}))
	require.NoError(t, st.Agents().Create(t.Context(), &store.Agent{ID: "agent-bob", Name: "Bob"}))
	tl, err := newSchedulerTool(svc, st.Agents(), caller)
	require.NoError(t, err)
	return &schedulerFixture{st: st, svc: svc, tool: tl, caller: caller}
}

// runSchedulerTool runs one tool call through the InvokableTool surface the
// constructor returns as BaseTool and decodes the result.
func runSchedulerTool(t *testing.T, base tool.BaseTool, args string) schedulerOutput {
	t.Helper()
	invokable, ok := base.(tool.InvokableTool)
	require.True(t, ok, "scheduler tool %T does not implement InvokableTool", base)
	out, err := invokable.InvokableRun(t.Context(), args)
	require.NoError(t, err, "validation and unknown ids must be results, not errors: args %s", args)
	var res schedulerOutput
	require.NoError(t, json.Unmarshal([]byte(out), &res), "result %q", out)
	return res
}

// createForCaller issues the spec's create-for-self call: name, prompt and
// cron only — agent and enabled omitted.
func createForCaller(t *testing.T, f *schedulerFixture, name string) schedulerOutput {
	t.Helper()
	return runSchedulerTool(t, f.tool, `{"action":"create","name":`+quote(name)+
		`,"prompt":"Check the status page","cron":"0 9 * * 1-5"}`)
}

// quote renders s as a JSON string literal.
func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestSchedulerTool_Info pins the generated schema: the action enum plus the
// optional scalars, with enabled rendering as a boolean — the pointer-bool
// input must not degrade the inference.
func TestSchedulerTool_Info(t *testing.T) {
	f := newSchedulerFixture(t)
	info, err := f.tool.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, ToolScheduler, info.Name)
	require.NotEmpty(t, info.Desc)

	// ParamsOneOf's payload is only reachable through the JSON round-trip;
	// InferTool-built tools marshal it as json_schema (the browser test's
	// idea, adapted to this shape).
	b, err := json.Marshal(info)
	require.NoError(t, err)
	var decoded struct {
		JSONSchema struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
			Required []string `json:"required"`
		} `json:"json_schema"`
	}
	require.NoError(t, json.Unmarshal(b, &decoded))
	props := decoded.JSONSchema.Properties
	for _, name := range []string{"action", "id", "name", "agent", "prompt", "cron", "enabled"} {
		require.Contains(t, props, name)
	}
	require.Equal(t, "string", props["action"].Type)
	require.Equal(t, []string{"action"}, decoded.JSONSchema.Required, "only action is required")
	require.Equal(t, "boolean", props["enabled"].Type, "enabled must infer as boolean")
}

// TestSchedulerTool_CreateForSelf is spec agent-tools "Agent creates a
// schedule for itself from chat": omitting agent defaults to the calling
// agent, enabled defaults to true, and the result carries the id, the
// human-readable recurrence, and the next fire time.
func TestSchedulerTool_CreateForSelf(t *testing.T) {
	f := newSchedulerFixture(t)
	res := createForCaller(t, f, "Morning check")
	require.True(t, res.OK, "error: %s", res.Error)
	require.NotNil(t, res.Schedule)
	require.NotEmpty(t, res.Schedule.ID)
	require.Equal(t, "Morning check", res.Schedule.Name)
	require.Equal(t, f.caller, res.Schedule.AgentID, "omitted agent defaults to the caller")
	require.Equal(t, "Alice", res.Schedule.AgentName)
	require.Equal(t, "0 9 * * 1-5", res.Schedule.Cron)
	require.Equal(t, "Every weekday at 09:00", res.Schedule.Description)
	require.True(t, res.Schedule.Enabled)
	require.Equal(t, "2026-10-21T09:00:00Z", res.Schedule.NextFireAt)
	require.Empty(t, res.Schedule.LastRunStatus, "never fired")

	// The schedule is visible in the store (the Schedules menu lists it).
	row, err := f.st.Schedules().ByID(t.Context(), res.Schedule.ID)
	require.NoError(t, err)
	require.Equal(t, f.caller, row.AgentID)
	require.True(t, row.Enabled)
}

// TestSchedulerTool_CreateDefaultsEnabledFalse covers the explicit
// enabled:false override on create.
func TestSchedulerTool_CreateDefaultsEnabledFalse(t *testing.T) {
	f := newSchedulerFixture(t)
	res := runSchedulerTool(t, f.tool,
		`{"action":"create","name":"Muted","prompt":"p","cron":"0 9 * * 1-5","enabled":false}`)
	require.True(t, res.OK, "error: %s", res.Error)
	require.False(t, res.Schedule.Enabled)
	require.Empty(t, res.Schedule.NextFireAt, "a disabled schedule carries no next fire")
}

// TestSchedulerTool_InvalidCronIsReadableResult is spec agent-tools "Invalid
// expression is a readable error": the five-field teaching message comes
// back as a result and nothing is created; the turn can continue.
func TestSchedulerTool_InvalidCronIsReadableResult(t *testing.T) {
	f := newSchedulerFixture(t)
	res := runSchedulerTool(t, f.tool,
		`{"action":"create","name":"Weekly","prompt":"p","cron":"every monday"}`)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "five-field cron expression")
	require.Nil(t, res.Schedule)

	rows, err := f.st.Schedules().List(t.Context())
	require.NoError(t, err)
	require.Empty(t, rows, "a rejected create must store nothing")
}

// TestSchedulerTool_DuplicateNamePointsAtUpdate is spec agent-tools
// "Duplicate name points at update": the error names the existing schedule's
// id and suggests update.
func TestSchedulerTool_DuplicateNamePointsAtUpdate(t *testing.T) {
	f := newSchedulerFixture(t)
	first := createForCaller(t, f, "Morning check")
	require.True(t, first.OK, "error: %s", first.Error)

	res := createForCaller(t, f, "Morning check")
	require.False(t, res.OK)
	require.Contains(t, res.Error, first.Schedule.ID, "the error names the existing id")
	require.Contains(t, res.Error, `use action "update" with id `+first.Schedule.ID)
}

// TestSchedulerTool_ListReportsFullState is spec agent-tools "List reports
// schedule state": every schedule with id, name, agent, expression,
// recurrence, enabled flag, next fire, and last-run outcome.
func TestSchedulerTool_ListReportsFullState(t *testing.T) {
	f := newSchedulerFixture(t)
	first := createForCaller(t, f, "Morning check")
	require.True(t, first.OK, "error: %s", first.Error)
	second := runSchedulerTool(t, f.tool,
		`{"action":"create","name":"Evening","agent":"bob","prompt":"Say goodnight","cron":"30 22 * * *"}`)
	require.True(t, second.OK, "error: %s", second.Error)

	res := runSchedulerTool(t, f.tool, `{"action":"list"}`)
	require.True(t, res.OK, "error: %s", res.Error)
	require.Len(t, res.Schedules, 2)

	byName := make(map[string]scheduleResult, len(res.Schedules))
	for _, s := range res.Schedules {
		byName[s.Name] = s
	}
	morning := byName["Morning check"]
	require.Equal(t, first.Schedule.ID, morning.ID)
	require.Equal(t, f.caller, morning.AgentID)
	require.Equal(t, "Alice", morning.AgentName)
	require.Equal(t, "0 9 * * 1-5", morning.Cron)
	require.Equal(t, "Every weekday at 09:00", morning.Description)
	require.True(t, morning.Enabled)
	require.Equal(t, "2026-10-21T09:00:00Z", morning.NextFireAt)

	evening := byName["Evening"]
	require.Equal(t, "agent-bob", evening.AgentID)
	require.Equal(t, "Bob", evening.AgentName)
	require.True(t, evening.Enabled)
	require.Equal(t, "2026-10-21T22:30:00Z", evening.NextFireAt)
}

// TestSchedulerTool_UpdateDisablesByID is spec agent-tools "Update disables
// by id": enabled:false disables the schedule, everything else is unchanged,
// and the next fire clears.
func TestSchedulerTool_UpdateDisablesByID(t *testing.T) {
	f := newSchedulerFixture(t)
	created := createForCaller(t, f, "Morning check")
	require.True(t, created.OK, "error: %s", created.Error)

	res := runSchedulerTool(t, f.tool,
		`{"action":"update","enabled":false,"id":`+quote(created.Schedule.ID)+`}`)
	require.True(t, res.OK, "error: %s", res.Error)
	require.NotNil(t, res.Schedule)
	require.Equal(t, created.Schedule.ID, res.Schedule.ID)
	require.False(t, res.Schedule.Enabled)
	require.Empty(t, res.Schedule.NextFireAt, "a disabled schedule carries no next fire")
	// The untouched fields come back exactly as stored.
	require.Equal(t, "Morning check", res.Schedule.Name)
	require.Equal(t, f.caller, res.Schedule.AgentID)
	require.Equal(t, "0 9 * * 1-5", res.Schedule.Cron)
	require.Equal(t, "Every weekday at 09:00", res.Schedule.Description)

	row, err := f.st.Schedules().ByID(t.Context(), created.Schedule.ID)
	require.NoError(t, err)
	require.False(t, row.Enabled)
	require.Equal(t, "Check the status page", row.Prompt, "the prompt is unchanged")
	require.Equal(t, "0 9 * * 1-5", row.Cron)
}

// TestSchedulerTool_UpdateMergeOnlySuppliedFields proves absent fields keep
// their stored values through a full-replace update: changing only the cron
// leaves name, prompt, agent, and enabled alone.
func TestSchedulerTool_UpdateMergeOnlySuppliedFields(t *testing.T) {
	f := newSchedulerFixture(t)
	created := createForCaller(t, f, "Morning check")
	require.True(t, created.OK, "error: %s", created.Error)

	res := runSchedulerTool(t, f.tool,
		`{"action":"update","cron":"30 8 * * 1-5","id":`+quote(created.Schedule.ID)+`}`)
	require.True(t, res.OK, "error: %s", res.Error)
	require.Equal(t, "30 8 * * 1-5", res.Schedule.Cron)
	require.Equal(t, "Every weekday at 08:30", res.Schedule.Description)
	require.True(t, res.Schedule.Enabled, "absent enabled keeps the stored true")

	row, err := f.st.Schedules().ByID(t.Context(), created.Schedule.ID)
	require.NoError(t, err)
	require.Equal(t, "Morning check", row.Name)
	require.Equal(t, "Check the status page", row.Prompt)
	require.Equal(t, f.caller, row.AgentID)
}

// TestSchedulerTool_UpdateUnknownID is a readable result, not a turn failure.
func TestSchedulerTool_UpdateUnknownID(t *testing.T) {
	f := newSchedulerFixture(t)
	res := runSchedulerTool(t, f.tool, `{"action":"update","enabled":false,"id":"ghost"}`)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "no schedule with id ghost")
}

// TestSchedulerTool_DeleteUnknownID is spec agent-tools "Delete of an unknown
// id is a readable error".
func TestSchedulerTool_DeleteUnknownID(t *testing.T) {
	f := newSchedulerFixture(t)
	res := runSchedulerTool(t, f.tool, `{"action":"delete","id":"ghost"}`)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "no schedule with id ghost")
}

// TestSchedulerTool_DeleteRemoves proves a successful delete is visible
// immediately (the Schedules menu reads the store).
func TestSchedulerTool_DeleteRemoves(t *testing.T) {
	f := newSchedulerFixture(t)
	created := createForCaller(t, f, "Morning check")
	require.True(t, created.OK, "error: %s", created.Error)

	res := runSchedulerTool(t, f.tool,
		`{"action":"delete","id":`+quote(created.Schedule.ID)+`}`)
	require.True(t, res.OK, "error: %s", res.Error)
	require.Equal(t, created.Schedule.ID, res.Deleted)

	rows, err := f.st.Schedules().List(t.Context())
	require.NoError(t, err)
	require.Empty(t, rows)
}

// TestSchedulerTool_ResolveAgentByName covers the case-insensitive agent
// name resolution and its readable rejections: unknown names and ambiguous
// names both come back as results naming the field.
func TestSchedulerTool_ResolveAgentByName(t *testing.T) {
	t.Run("by name, case-insensitive", func(t *testing.T) {
		f := newSchedulerFixture(t)
		res := runSchedulerTool(t, f.tool,
			`{"action":"create","name":"For Bob","agent":"bob","prompt":"p","cron":"0 9 * * 1-5"}`)
		require.True(t, res.OK, "error: %s", res.Error)
		require.Equal(t, "agent-bob", res.Schedule.AgentID)
		require.Equal(t, "Bob", res.Schedule.AgentName)
	})

	t.Run("unknown agent is a readable result", func(t *testing.T) {
		f := newSchedulerFixture(t)
		res := runSchedulerTool(t, f.tool,
			`{"action":"create","name":"Ghosted","agent":"nobody","prompt":"p","cron":"0 9 * * 1-5"}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "agent")
		require.Contains(t, res.Error, "nobody")

		rows, err := f.st.Schedules().List(t.Context())
		require.NoError(t, err)
		require.Empty(t, rows, "a rejected create must store nothing")
	})

	t.Run("ambiguous name names the field and the count", func(t *testing.T) {
		f := newSchedulerFixture(t)
		require.NoError(t, f.st.Agents().Create(t.Context(), &store.Agent{ID: "agent-twin-1", Name: "Twin"}))
		require.NoError(t, f.st.Agents().Create(t.Context(), &store.Agent{ID: "agent-twin-2", Name: "twin"}))
		res := runSchedulerTool(t, f.tool,
			`{"action":"create","name":"Ambiguous","agent":"TWIN","prompt":"p","cron":"0 9 * * 1-5"}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "agent:")
		require.Contains(t, res.Error, "2 agents")
	})
}

// TestSchedulerTool_UnknownAction lists the four actions in the readable
// error.
func TestSchedulerTool_UnknownAction(t *testing.T) {
	f := newSchedulerFixture(t)
	for _, action := range []string{"explode", ""} {
		res := runSchedulerTool(t, f.tool, `{"action":`+quote(action)+`}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "create, list, update, delete")
	}
}

// TestSchedulerTool_UpdateByAgentName proves the update path resolves the
// agent argument exactly like create.
func TestSchedulerTool_UpdateByAgentName(t *testing.T) {
	f := newSchedulerFixture(t)
	created := createForCaller(t, f, "Morning check")
	require.True(t, created.OK, "error: %s", created.Error)

	res := runSchedulerTool(t, f.tool,
		`{"action":"update","agent":"BOB","id":`+quote(created.Schedule.ID)+`}`)
	require.True(t, res.OK, "error: %s", res.Error)
	require.Equal(t, "agent-bob", res.Schedule.AgentID)
	require.Equal(t, "Bob", res.Schedule.AgentName)
}
