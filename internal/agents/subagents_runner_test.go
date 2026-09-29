package agents

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/references"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
)

// ----- scripted model -----

// subagentModelCall records one model invocation: the routed lane and the full
// input, so tests can assert on tool results the model saw (launch copies,
// task_output projections) after the run.
type subagentModelCall struct {
	isChild bool
	input   []*schema.AgenticMessage
}

// laneScript renders one model response for its lane from the model call
// context, the call ordinal (1-based), and the input the model received.
// Steps never fail the test directly — the model runs on the run's goroutine —
// they return marker text the post-run assertions surface instead.
type laneScript func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error)

// subagentsRunnerModel routes each invocation to a parent or child script by
// scanning the input for the delegation marker. Parent and child share one
// model instance (design.md D4), so the scripts must be self-routing.
type subagentsRunnerModel struct {
	mu           sync.Mutex
	calls        []subagentModelCall
	parentScript laneScript
	childScript  laneScript
	parentCalls  int
	childCalls   int
	// names records the tool surface each invocation saw, indexed like calls.
	names [][]string
}

const delegationMarker = "SUBTASK:"

func (m *subagentsRunnerModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	o := model.GetCommonOptions(&model.Options{}, opts...)
	surface := make([]string, 0, len(o.Tools))
	for _, ti := range o.Tools {
		if ti != nil {
			surface = append(surface, ti.Name)
		}
	}
	m.mu.Lock()
	m.names = append(m.names, surface)
	isChild := inputHasDelegationMarker(input)
	m.calls = append(m.calls, subagentModelCall{isChild: isChild, input: append([]*schema.AgenticMessage(nil), input...)})
	var script laneScript
	var call int
	if isChild {
		m.childCalls++
		call, script = m.childCalls, m.childScript
	} else {
		m.parentCalls++
		call, script = m.parentCalls, m.parentScript
	}
	m.mu.Unlock()

	if script == nil {
		return textMessage("script exhausted"), nil
	}
	return script(ctx, call, input)
}

func (m *subagentsRunnerModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

// callCount returns the number of routed invocations for one lane.
func (m *subagentsRunnerModel) laneCallCount(isChild bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.calls {
		if c.isChild == isChild {
			n++
		}
	}
	return n
}

// callInput returns the recorded input of the nth call (1-based) of one lane.
func (m *subagentsRunnerModel) callInput(t *testing.T, isChild bool, n int) []*schema.AgenticMessage {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := 0
	for _, c := range m.calls {
		if c.isChild != isChild {
			continue
		}
		seen++
		if seen == n {
			return c.input
		}
	}
	t.Fatalf("model call %d (child=%v) not recorded", n, isChild)
	return nil
}

// callNames returns the tool surface the nth call (1-based) of one lane saw.
// Indexed like callInput: calls and names are appended under one lock.
func (m *subagentsRunnerModel) callNames(t *testing.T, isChild bool, n int) []string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := 0
	for i, c := range m.calls {
		if c.isChild != isChild {
			continue
		}
		seen++
		if seen == n {
			return m.names[i]
		}
	}
	t.Fatalf("model call %d (child=%v) not recorded", n, isChild)
	return nil
}

// inputHasDelegationMarker reports whether the input carries the delegation
// marker — only the child sees the delegation prompt.
func inputHasDelegationMarker(input []*schema.AgenticMessage) bool {
	for _, msg := range input {
		if strings.Contains(extractAgenticText(msg), delegationMarker) {
			return true
		}
		if strings.Contains(agenticToolResultText(msg), delegationMarker) {
			return true
		}
	}
	return false
}

// ----- message builders -----

func textMessage(text string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: text}},
		},
	}
}

// usageTextMessage is a text message reporting provider token usage.
func usageTextMessage(text string, prompt, completion, total int) *schema.AgenticMessage {
	msg := textMessage(text)
	msg.ResponseMeta = &schema.AgenticResponseMeta{
		TokenUsage: &schema.TokenUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total},
	}
	return msg
}

func scriptedToolCall(id, name, args string) *schema.AgenticMessage {
	msg := toolCallMessage(schema.FunctionToolCall{CallID: id, Name: name, Arguments: args})
	msg.Role = schema.AgenticRoleTypeAssistant
	return msg
}

// ----- input inspection helpers -----

// inputToolResultTexts concatenates every tool-result text the model received.
func inputToolResultTexts(input []*schema.AgenticMessage) string {
	var sb strings.Builder
	for _, msg := range input {
		sb.WriteString(agenticToolResultText(msg))
	}
	return sb.String()
}

var taskIDPattern = regexp.MustCompile(`ID: (\S+)\.`)

// lastTaskID extracts the most recent background task id a launch result
// reported ("...running in background with ID: <id>."), or "" when the input
// carries none. Control-tool results render "Task ID: <id>" without a period
// and deliberately do not match.
func lastTaskID(input []*schema.AgenticMessage) string {
	matches := taskIDPattern.FindAllStringSubmatch(inputToolResultTexts(input), -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

// ----- harness -----

// setupSubagentsRunner seeds a delegating agent over the fake store and wires
// the routed scripted model. Under the denylist the reserved names are
// default-on (agent-tools-denylist D3): an agent with an empty disabled_tools
// delegates, so no allowlist seeding is needed.
func setupSubagentsRunner(t *testing.T, mdl *subagentsRunnerModel) (runner *Runner, wsID, agentID, sessionID, userID string) {
	t.Helper()
	ctx := context.Background()
	st := storefake.New()

	ws := &domain.Workspace{Slug: "acme", Name: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	user := &domain.User{Email: "u@example.com", Name: "U"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner"}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("create member: %v", err)
	}

	memWorker, memSearch, memGate := newTestMemoryPipeline(st)
	runner = NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(), st.AgentSessions(),
		st.GatewayLinks(),
		memWorker, memSearch, memGate,
		[]byte("test-key-32-bytes-long-12345678"),
		t.TempDir(),
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (Model, error) {
			return mdl, nil
		}),
		WithInstructionComposer(stubComposer{}),
	)

	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Seed the agent's on-disk workspace directory (the jail root).
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(runner.onClawDir), ws.Slug, ag.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}

	return runner, ws.ID, ag.ID, "sess-" + ag.Slug, user.ID
}

// runSubagentsTurn executes one turn and returns the live transcript events.
func runSubagentsTurn(t *testing.T, runner *Runner, wsID, agentID, sessionID, userID, input string) []TranscriptEvent {
	t.Helper()
	stream, err := runner.Run(context.Background(), ExecRequest{
		WorkspaceID: wsID,
		AgentID:     agentID,
		SessionID:   sessionID,
		UserID:      userID,
		Input:       input,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return collectStream(t, stream)
}

// subagentsHistory loads the session's hydrated transcript.
func subagentsHistory(t *testing.T, runner *Runner, wsID, agentID, sessionID string) []TranscriptEvent {
	t.Helper()
	hist, err := runner.History(context.Background(), HistoryRequest{WorkspaceID: wsID, AgentID: agentID, SessionID: sessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	return hist.Events
}

// toolCallsOf returns every tool_call_started payload for one tool name.
func toolCallsOf(events []TranscriptEvent, name string) []ToolCallPayload {
	var out []ToolCallPayload
	for i := range events {
		if events[i].Kind == TranscriptEventToolCallStarted && events[i].ToolCall != nil && events[i].ToolCall.Name == name {
			out = append(out, *events[i].ToolCall)
		}
	}
	return out
}

// toolResultsOf returns every tool_call_finished payload for one call id.
func toolResultsOf(events []TranscriptEvent, callID string) []ToolResultPayload {
	var out []ToolResultPayload
	for i := range events {
		if events[i].Kind == TranscriptEventToolCallFinished && events[i].ToolResult != nil && events[i].ToolResult.CallID == callID {
			out = append(out, *events[i].ToolResult)
		}
	}
	return out
}

// countKind counts events of one kind.
func countKind(events []TranscriptEvent, kind TranscriptEventKind) int {
	n := 0
	for i := range events {
		if events[i].Kind == kind {
			n++
		}
	}
	return n
}

// taskCompletionsOf returns the task_completed payloads addressing taskID.
func taskCompletionsOf(events []TranscriptEvent, taskID string) []TaskCompletedPayload {
	var out []TaskCompletedPayload
	for i := range events {
		if events[i].Kind == TranscriptEventTaskCompleted && events[i].TaskCompleted != nil && events[i].TaskCompleted.TaskID == taskID {
			out = append(out, *events[i].TaskCompleted)
		}
	}
	return out
}

// significantEvent is the field-for-field projection the 5.3 comparison uses:
// the transcript surface both the live stream and the hydrated History
// projection carry. Live-only bookkeeping (turn_started, usage stamps) and the
// hydration-only latency recomputation are excluded.
type significantEvent struct {
	Kind    TranscriptEventKind
	Role    string
	Content string
	CallID  string
	Name    string
	Result  string
	IsError bool
	TaskID  string
	Outcome string
}

func significantOf(events []TranscriptEvent) []significantEvent {
	out := make([]significantEvent, 0, len(events))
	for i := range events {
		ev := events[i]
		switch ev.Kind {
		case TranscriptEventMessageCompleted:
			if ev.Message == nil {
				continue
			}
			out = append(out, significantEvent{Kind: ev.Kind, Role: ev.Message.Role, Content: ev.Message.Content})
		case TranscriptEventToolCallStarted:
			if ev.ToolCall == nil {
				continue
			}
			out = append(out, significantEvent{Kind: ev.Kind, CallID: ev.ToolCall.CallID, Name: ev.ToolCall.Name})
		case TranscriptEventToolCallFinished:
			if ev.ToolResult == nil {
				continue
			}
			out = append(out, significantEvent{Kind: ev.Kind, CallID: ev.ToolResult.CallID, Name: ev.ToolResult.Name, Result: ev.ToolResult.Result, IsError: ev.ToolResult.IsError})
		case TranscriptEventTaskCompleted:
			if ev.TaskCompleted == nil {
				continue
			}
			out = append(out, significantEvent{Kind: ev.Kind, TaskID: ev.TaskCompleted.TaskID, Outcome: ev.TaskCompleted.Outcome})
		}
	}
	return out
}

// ----- 5.3: child-event stream isolation -----

// TestRunner_SubagentsDelegationStreamIsolatesChildEvents is task 5.3's
// verify: a foreground delegation whose child runs multiple inner model turns
// and an inner tool call renders on the parent's stream as exactly one
// tool-call card (its own started/finished pair, result = the child's final
// message), with zero child text, reasoning, or inner-tool events — live and
// hydrated views matching field-for-field (design.md D7).
func TestRunner_SubagentsDelegationStreamIsolatesChildEvents(t *testing.T) {
	mdl := &subagentsRunnerModel{
		parentScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			if call == 1 {
				return scriptedToolCall("p-call-1", "agent",
					`{"subagent_type":"general-purpose","prompt":"`+delegationMarker+` research the flaky test","description":"research task"}`), nil
			}
			return textMessage("delegation done"), nil
		},
		childScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			// A text-only child message ends the child run, so the multi-turn
			// child chains tool-call turns before its final report.
			switch call {
			case 1, 2:
				return scriptedToolCall(fmt.Sprintf("c-call-%d", call), "read_file", `{"path":"/workspace/notes.md"}`), nil
			default:
				return usageTextMessage("child final report", 333, 44, 377), nil
			}
		},
	}
	runner, wsID, agentID, sessionID, userID := setupSubagentsRunner(t, mdl)

	events := runSubagentsTurn(t, runner, wsID, agentID, sessionID, userID, "delegate the research")

	if !hasKind(events, TranscriptEventTurnCompleted) || hasKind(events, TranscriptEventError) {
		t.Fatalf("run must complete cleanly, got %+v", events)
	}

	// Exactly one delegation card: the parent's own started/finished pair.
	started := toolCallsOf(events, "agent")
	if len(started) != 1 || started[0].CallID != "p-call-1" {
		t.Fatalf("agent tool_call_started events = %+v, want exactly p-call-1", started)
	}
	finished := toolResultsOf(events, "p-call-1")
	if len(finished) != 1 {
		t.Fatalf("agent tool_call_finished events = %+v, want exactly one", finished)
	}
	// The card's result is the child's final message — nothing else of the
	// child's execution surfaced.
	if finished[0].Result != "child final report" || finished[0].IsError {
		t.Fatalf("delegation result = %+v, want the child's final report without error", finished[0])
	}

	// Zero child content on the stream: no inner-tool cards, no child text or
	// reasoning deltas.
	if leaked := toolCallsOf(events, "read_file"); len(leaked) != 0 {
		t.Fatalf("child inner tool call leaked onto the parent stream: %+v", leaked)
	}
	var text strings.Builder
	for i := range events {
		if events[i].Kind == TranscriptEventTextDelta {
			text.WriteString(events[i].TextDelta)
		}
		if events[i].Kind == TranscriptEventReasoningDelta {
			t.Fatalf("reasoning delta leaked onto the parent stream: %q", events[i].ReasoningDelta)
		}
	}
	if strings.Contains(text.String(), "child ") {
		t.Fatalf("child text leaked onto the parent stream: %q", text.String())
	}
	if text.String() != "delegation done" {
		t.Fatalf("parent stream text = %q, want exactly the parent's final answer", text.String())
	}

	// The parent's next model call read the delegation result: the child's
	// final report rode the normal tool-result lane.
	parentTurn2 := inputToolResultTexts(mdl.callInput(t, false, 2))
	if !strings.Contains(parentTurn2, "child final report") {
		t.Fatalf("parent turn-2 input tool results = %q, want the child final report", parentTurn2)
	}

	// Hydrated transcript matches the live stream field-for-field (design.md
	// D7: live and hydrated views agree by construction). The user bubble is
	// excluded from the comparison — a string-input turn never emits it live
	// (only attachment-carrying turns do), while hydration renders the
	// persisted input; both project the delegation identically.
	live := significantOf(events)
	hydrated := significantOf(subagentsHistory(t, runner, wsID, agentID, sessionID))
	live = withoutUserMessages(live)
	hydrated = withoutUserMessages(hydrated)
	if len(live) == 0 {
		t.Fatal("no significant live events")
	}
	if len(live) != len(hydrated) {
		t.Fatalf("hydrated transcript shape differs: live=%+v hydrated=%+v", live, hydrated)
	}
	for i := range live {
		if live[i] != hydrated[i] {
			t.Fatalf("event %d differs: live=%+v hydrated=%+v", i, live[i], hydrated[i])
		}
	}
	// The hydrated view still carries the user input.
	hydratedAll := significantOf(subagentsHistory(t, runner, wsID, agentID, sessionID))
	for _, ev := range hydratedAll {
		if ev.Kind == TranscriptEventMessageCompleted && ev.Role == "user" && ev.Content == "delegate the research" {
			return
		}
	}
	t.Fatalf("hydrated transcript lost the user input: %+v", hydratedAll)
}

// withoutUserMessages drops user-role message events from a significant
// projection (the live string-turn asymmetry the 5.3 comparison documents).
func withoutUserMessages(events []significantEvent) []significantEvent {
	out := make([]significantEvent, 0, len(events))
	for _, ev := range events {
		if ev.Kind == TranscriptEventMessageCompleted && ev.Role == "user" {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// ----- 5.4: child usage aggregation -----

// TestRunner_SubagentsDelegationUsageAggregatesIntoRunTotals is task 5.4's
// verify: the child's model-call token usage — reported on the child's own
// messages — aggregates into the parent run's terminal usage totals alongside
// the parent's calls (design.md "Token accounting" risk pin).
func TestRunner_SubagentsDelegationUsageAggregatesIntoRunTotals(t *testing.T) {
	mdl := &subagentsRunnerModel{
		parentScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			if call == 1 {
				return scriptedToolCall("p-call-1", "agent",
					`{"subagent_type":"general-purpose","prompt":"`+delegationMarker+` research the flaky test","description":"research task"}`), nil
			}
			// The parent's final call: 100 in / 10 out.
			return usageTextMessage("delegation done", 100, 10, 110), nil
		},
		childScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			switch call {
			case 1:
				// Child turn 1: a tool-call request carrying usage (111/22).
				msg := scriptedToolCall("c-call-1", "read_file", `{"path":"/workspace/notes.md"}`)
				msg.ResponseMeta = &schema.AgenticResponseMeta{
					TokenUsage: &schema.TokenUsage{PromptTokens: 111, CompletionTokens: 22, TotalTokens: 133},
				}
				return msg, nil
			default:
				// Child turn 2: the final report (333/44).
				return usageTextMessage("child final report", 333, 44, 377), nil
			}
		},
	}
	runner, wsID, agentID, sessionID, userID := setupSubagentsRunner(t, mdl)

	events := runSubagentsTurn(t, runner, wsID, agentID, sessionID, userID, "delegate the research")

	if !hasKind(events, TranscriptEventTurnCompleted) || hasKind(events, TranscriptEventError) {
		t.Fatalf("run must complete cleanly, got %+v", events)
	}
	terminal := events[len(events)-1]
	if terminal.Kind != TranscriptEventTurnCompleted || terminal.Usage == nil {
		t.Fatalf("terminal event = %+v, want turn_completed with usage", terminal)
	}
	// Parent turn 2 (100/10/110) + child turns 1 and 2 (111/22/133, 333/44/377):
	// inputs sum, outputs sum, and the chronologically last call's input — the
	// parent's final call — wins FinalInputTokens.
	want := UsagePayload{InputTokens: 544, OutputTokens: 76, TotalTokens: 620, FinalInputTokens: 100}
	if terminal.Usage.InputTokens != want.InputTokens ||
		terminal.Usage.OutputTokens != want.OutputTokens ||
		terminal.Usage.TotalTokens != want.TotalTokens {
		t.Fatalf("run usage = %+v, want %+v (parent + child totals)", terminal.Usage, want)
	}
	if terminal.Usage.FinalInputTokens != want.FinalInputTokens {
		t.Fatalf("final input = %d, want %d (the parent's last call)", terminal.Usage.FinalInputTokens, want.FinalInputTokens)
	}
}

// ----- 4.3: background shell lane end to end -----

// TestRunner_BackgroundShellLaneEndToEnd is task 4.3's verify: a background
// launch through the managed execute path returns a task id and a .tasks
// output path, task_output returns the command's output, task_stop cancels a
// long-running command to a canceled status visible to task_output, and the
// notification pump lands exactly one completion event per task (design.md
// D11/D8).
func TestRunner_BackgroundShellLaneEndToEnd(t *testing.T) {
	var echoID, sleepID string
	var echoLaunched, sleepStopped time.Time
	var pollSeq int
	// Pre-declared so the parent script can hold the turn open while the
	// completion pump drains (the closures below capture them).
	var (
		runner            *Runner
		wsID, agentID     string
		sessionID, userID string
	)

	// pollTaskOutput issues a task_output call with a fresh call id per
	// attempt (block=false: render current state, never wait).
	pollTaskOutput := func(id string) *schema.AgenticMessage {
		pollSeq++
		return scriptedToolCall(fmt.Sprintf("p-poll-%d", pollSeq), "task_output", `{"task_id":"`+id+`","block":false}`)
	}

	// phase advances one step per transition; poll phases stay until their
	// condition shows in the accumulated tool results (one-shot transitions
	// are flagged by advancing, never re-detected).
	phase := 0
	mdl := &subagentsRunnerModel{
		parentScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			results := inputToolResultTexts(input)
			switch phase {
			case 0: // launch the echo in the background
				phase = 1
				echoLaunched = time.Now()
				return scriptedToolCall("p-echo", "execute", `{"command":"echo hello-bg","run_in_background":true}`), nil

			case 1: // read the launch result, poll the echo
				phase = 2
				echoID = lastTaskID(input)
				if echoID == "" {
					return textMessage("marker: launch result carried no task id"), nil
				}
				return pollTaskOutput(echoID), nil

			case 2: // poll the echo until completed, then launch the sleep
				if !strings.Contains(results, "Status: completed") {
					if time.Since(echoLaunched) > 15*time.Second {
						return textMessage("marker: echo task never completed"), nil
					}
					time.Sleep(50 * time.Millisecond)
					return pollTaskOutput(echoID), nil
				}
				phase = 3
				return scriptedToolCall("p-sleep", "execute", `{"command":"sleep 30","run_in_background":true}`), nil

			case 3: // read the sleep launch result, stop it
				phase = 4
				sleepID = lastTaskID(input)
				if sleepID == "" || sleepID == echoID {
					return textMessage("marker: second launch carried no distinct task id"), nil
				}
				sleepStopped = time.Now()
				return scriptedToolCall("p-stop", "task_stop", `{"task_id":"`+sleepID+`","reason":"no longer needed"}`), nil

			default: // poll the stopped task until canceled, then let the pump drain
				if !strings.Contains(results, "Status: canceled") {
					if time.Since(sleepStopped) > 15*time.Second {
						return textMessage("marker: stop never reached the canceled status"), nil
					}
					time.Sleep(50 * time.Millisecond)
					return pollTaskOutput(sleepID), nil
				}
				if !waitForCompletions(runner, wsID, sessionID, echoID, sleepID) {
					return textMessage("marker: completion events never landed"), nil
				}
				return textMessage("background work settled"), nil
			}
		},
	}
	runner, wsID, agentID, sessionID, userID = setupSubagentsRunner(t, mdl)

	events := runSubagentsTurn(t, runner, wsID, agentID, sessionID, userID, "run the checks")
	if !hasKind(events, TranscriptEventTurnCompleted) || hasKind(events, TranscriptEventError) {
		t.Fatalf("run must complete cleanly, got %+v", events)
	}

	// The echo launch carried the task id and a jailed output path.
	echoLaunch := inputToolResultTexts(mdl.callInput(t, false, 2))
	if !strings.Contains(echoLaunch, "Command running in background with ID: "+echoID+".") {
		t.Fatalf("echo launch result = %q, want the background task id %q", echoLaunch, echoID)
	}
	if !strings.Contains(echoLaunch, ".tasks/") {
		t.Fatalf("echo launch result = %q, want an output path under .tasks/", echoLaunch)
	}

	// task_output on the echo returned the command's output.
	var echoOutput string
	for n := 3; n <= mdl.laneCallCount(false); n++ {
		results := inputToolResultTexts(mdl.callInput(t, false, n))
		if strings.Contains(results, "Status: completed") {
			echoOutput = results
			break
		}
	}
	if !strings.Contains(echoOutput, "hello-bg") {
		t.Fatalf("echo task_output = %q, want the completed status with the command output", echoOutput)
	}

	// The stop acknowledged and the canceled status became visible to
	// task_output.
	final := inputToolResultTexts(mdl.callInput(t, false, mdl.laneCallCount(false)))
	if !strings.Contains(final, "Status: canceled") {
		t.Fatalf("final model input = %q, want the canceled task record", final)
	}

	// Exactly one completion event per task, shell lane, from the pump — live
	// and hydrated.
	for _, tc := range []struct {
		id      string
		outcome string
		command string
	}{
		{echoID, "completed", "echo hello-bg"},
		{sleepID, "canceled", "sleep 30"},
	} {
		live := taskCompletionsOf(events, tc.id)
		if len(live) != 1 {
			t.Fatalf("task %s completion events = %+v, want exactly one", tc.id, live)
		}
		if live[0].Outcome != tc.outcome || live[0].Kind != "shell" {
			t.Fatalf("task %s completion payload = %+v, want outcome %q kind shell", tc.id, live[0], tc.outcome)
		}
		if !strings.Contains(live[0].OutputPath, ".tasks/") {
			t.Fatalf("task %s completion output path = %q, want a .tasks/ path", tc.id, live[0].OutputPath)
		}
		if live[0].Summary != tc.command {
			t.Fatalf("task %s completion summary = %q, want the command %q", tc.id, live[0].Summary, tc.command)
		}
		hydrated := taskCompletionsOf(subagentsHistory(t, runner, wsID, agentID, sessionID), tc.id)
		if len(hydrated) != 1 || hydrated[0] != live[0] {
			t.Fatalf("hydrated completion for task %s = %+v, want the live payload %+v exactly once", tc.id, hydrated, live[0])
		}
	}
}

// ----- 5.6: background delegation end to end -----

// TestRunner_BackgroundDelegationEndToEnd is task 5.6's verify: the agent tool
// launched with run_in_background returns promptly with a task id, task_output
// on that id renders the child's transcript progress records while the child
// works, task_stop cancels the child run to a canceled-status task, a
// completion event lands once, and foreground delegations expose no task id at
// all (design.md D5/D11).
func TestRunner_BackgroundDelegationEndToEnd(t *testing.T) {
	var taskID string
	var launched, stopRequested time.Time
	var pollSeq int
	// Pre-declared so the parent script can hold the turn open while the
	// completion pump drains (the closures below capture them).
	var (
		runner            *Runner
		wsID, agentID     string
		sessionID, userID string
	)

	pollTaskOutput := func(id string) *schema.AgenticMessage {
		pollSeq++
		return scriptedToolCall(fmt.Sprintf("p-poll-%d", pollSeq), "task_output", `{"task_id":"`+id+`","block":false}`)
	}

	mdl := &subagentsRunnerModel{
		parentScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			results := inputToolResultTexts(input)
			switch {
			case call == 1:
				launched = time.Now()
				return scriptedToolCall("p-bg", "agent",
					`{"subagent_type":"general-purpose","prompt":"`+delegationMarker+` research the flaky test","description":"research task","run_in_background":true}`), nil

			case taskID == "":
				taskID = lastTaskID(input)
				if taskID == "" {
					return textMessage("marker: background launch carried no task id"), nil
				}
				return pollTaskOutput(taskID), nil

			case stopRequested.IsZero():
				// Poll task_output until the child's transcript progress records
				// render while the child works — the JSONL records, not the bare
				// "Recent progress" header the projection renders even when empty.
				if !strings.Contains(results, `"agent_name"`) {
					if time.Since(launched) > 15*time.Second {
						return textMessage("marker: task_output never rendered the child progress"), nil
					}
					time.Sleep(50 * time.Millisecond)
					return pollTaskOutput(taskID), nil
				}
				stopRequested = time.Now()
				return scriptedToolCall("p-stop", "task_stop", `{"task_id":"`+taskID+`","reason":"enough research"}`), nil

			default:
				if !strings.Contains(results, "Status: canceled") {
					if time.Since(stopRequested) > 15*time.Second {
						return textMessage("marker: stop never reached the canceled status"), nil
					}
					time.Sleep(50 * time.Millisecond)
					return pollTaskOutput(taskID), nil
				}
				if !waitForCompletions(runner, wsID, sessionID, taskID) {
					return textMessage("marker: completion event never landed"), nil
				}
				return textMessage("delegation settled"), nil
			}
		},
		childScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			if call == 1 {
				return scriptedToolCall("c-call-1", "read_file", `{"path":"/workspace/notes.md"}`), nil
			}
			// The child keeps working — blocked mid-research — until the
			// task_stop cancels the task's work context, which cancels this
			// model call.
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	runner, wsID, agentID, sessionID, userID = setupSubagentsRunner(t, mdl)

	events := runSubagentsTurn(t, runner, wsID, agentID, sessionID, userID, "delegate in the background")
	if !hasKind(events, TranscriptEventTurnCompleted) || hasKind(events, TranscriptEventError) {
		t.Fatalf("run must complete cleanly, got %+v", events)
	}

	// The launch returned promptly with a task id and output path — the child
	// was still mid-run while the parent's turn proceeded.
	launch := inputToolResultTexts(mdl.callInput(t, false, 2))
	if !strings.Contains(launch, "Agent running in background with ID: "+taskID+".") {
		t.Fatalf("background launch result = %q, want the running-agent copy with task id %q", launch, taskID)
	}
	if !strings.Contains(launch, ".tasks/") {
		t.Fatalf("background launch result = %q, want an output path under .tasks/", launch)
	}

	// task_output rendered the child's transcript progress records while the
	// child worked: the local lane's EmitProgress records projected through
	// the wired process-local progress reader.
	var progress string
	for n := 3; n <= mdl.laneCallCount(false); n++ {
		results := inputToolResultTexts(mdl.callInput(t, false, n))
		if strings.Contains(results, `"agent_name"`) {
			progress = results
			break
		}
	}
	if !strings.Contains(progress, "Recent progress") || !strings.Contains(progress, "read_file") {
		t.Fatalf("task_output progress = %q, want the child's transcript records", progress)
	}

	// The stop acknowledged and the canceled status became visible.
	final := inputToolResultTexts(mdl.callInput(t, false, mdl.laneCallCount(false)))
	if !strings.Contains(final, "Status: canceled") {
		t.Fatalf("final model input = %q, want the canceled task record", final)
	}

	// Exactly one completion event: the delegation lane, canceled outcome.
	live := taskCompletionsOf(events, taskID)
	if len(live) != 1 {
		t.Fatalf("task %s completion events = %+v, want exactly one", taskID, live)
	}
	if live[0].Outcome != "canceled" || live[0].Kind != "delegation" {
		t.Fatalf("completion payload = %+v, want outcome canceled kind delegation", live[0])
	}
	if live[0].Summary != "research task" {
		t.Fatalf("completion summary = %q, want the task description", live[0].Summary)
	}
	hydrated := taskCompletionsOf(subagentsHistory(t, runner, wsID, agentID, sessionID), taskID)
	if len(hydrated) != 1 || hydrated[0] != live[0] {
		t.Fatalf("hydrated completion = %+v, want the live payload %+v exactly once", hydrated, live[0])
	}
}

// TestRunner_ForegroundDelegationExposesNoTaskID is task 5.6's foreground pin:
// an `agent` call without run_in_background runs the child synchronously and
// its result is the child's final message — no task id, no completion surface.
func TestRunner_ForegroundDelegationExposesNoTaskID(t *testing.T) {
	mdl := &subagentsRunnerModel{
		parentScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			if call == 1 {
				return scriptedToolCall("p-call-1", "agent",
					`{"subagent_type":"general-purpose","prompt":"`+delegationMarker+` summarize the notes","description":"summary task"}`), nil
			}
			return textMessage("delegation done"), nil
		},
		childScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			return usageTextMessage("foreground child report", 50, 5, 55), nil
		},
	}
	runner, wsID, agentID, sessionID, userID := setupSubagentsRunner(t, mdl)

	events := runSubagentsTurn(t, runner, wsID, agentID, sessionID, userID, "delegate the summary")
	if !hasKind(events, TranscriptEventTurnCompleted) || hasKind(events, TranscriptEventError) {
		t.Fatalf("run must complete cleanly, got %+v", events)
	}
	if countKind(events, TranscriptEventTaskCompleted) != 0 {
		t.Fatalf("foreground delegation must emit no completion events, got %+v", events)
	}

	// The tool result is the child's final message — the managed foreground
	// lane creates no addressable task, so no task id appears.
	result := inputToolResultTexts(mdl.callInput(t, false, 2))
	if result != "foreground child report" {
		t.Fatalf("foreground delegation result = %q, want the child's final message", result)
	}
	if taskIDPattern.MatchString(result) {
		t.Fatalf("foreground delegation result = %q, want no task id", result)
	}
	if countKind(events, TranscriptEventToolCallStarted) != 1 {
		t.Fatalf("unexpected foreground stream shape: %+v", events)
	}
}

// waitForCompletions polls the hydrated history until every given task id has
// exactly one persisted completion chip (bounded), reporting whether all
// landed. The pump dies with the run, so the caller holds the turn open while
// waiting.
func waitForCompletions(runner *Runner, wsID, sessionID string, taskIDs ...string) bool {
	deadline := time.Now().Add(5 * time.Second)
	for {
		hist, err := runner.History(context.Background(), HistoryRequest{WorkspaceID: wsID, SessionID: sessionID})
		if err == nil {
			missing := false
			for _, id := range taskIDs {
				if len(taskCompletionsOf(hist.Events, id)) != 1 {
					missing = true
					break
				}
			}
			if !missing {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ----- 11.2: subagent inheritance (add-reference-documents D9) -----

// manifestRecordingComposer surfaces the pre-rendered reference-documents
// manifest into the instruction exactly where the DefaultInstructionComposer
// places it, recording the params each composition saw. The general-purpose
// clone shares the parent's composed instruction by construction
// (buildGeneralPurposeSubagent D4), so whatever this composer rendered is
// what the child runs with.
type manifestRecordingComposer struct {
	mu   sync.Mutex
	last ComposeParams
}

func (c *manifestRecordingComposer) Compose(_ context.Context, params ComposeParams) (string, error) {
	c.mu.Lock()
	c.last = params
	c.mu.Unlock()
	return "PARENT INSTRUCTION BASE\n\n" + params.ReferenceManifest, nil
}

func (c *manifestRecordingComposer) lastParams() ComposeParams {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// setupReferencesSubagentsRunner seeds a delegating agent over the fake store
// with the references service wired on both seams the capability rides: the
// registry's document.search (WithDocumentTools) and the runner's
// references run assembly (WithReferences).
func setupReferencesSubagentsRunner(t *testing.T, mdl *subagentsRunnerModel) (runner *Runner, composer *manifestRecordingComposer, svc *references.Service, wsID, agentID, sessionID, userID string) {
	t.Helper()
	ctx := context.Background()
	st := storefake.New()
	stor := storagefake.New()
	// NewService takes the workspace storage resolver (route-reference-
	// documents-through-workspace-storage D1); no workspace storage configs
	// are seeded here, so it resolves the instance default exactly as the
	// raw driver did.
	svc = references.NewService(st, resolver.New(stor, st.WorkspaceStorage(), st.Attachments(), []byte(testEncryptionKey), t.TempDir()))

	ws := &domain.Workspace{Slug: "acme", Name: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	user := &domain.User{Email: "u@example.com", Name: "U"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner"}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("create member: %v", err)
	}
	atlas := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: prov.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(ctx, atlas); err != nil {
		t.Fatalf("create atlas: %v", err)
	}
	beacon := &domain.Agent{WorkspaceID: ws.ID, Slug: "beacon", Name: "Beacon", ProviderID: prov.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(ctx, beacon); err != nil {
		t.Fatalf("create beacon: %v", err)
	}

	// The library: one atlas-attached runbook, one beacon-attached, one
	// channel-attached. The parent's direct-chat scope must resolve exactly
	// the promoted + atlas set — for the parent AND the child.
	for _, up := range []struct {
		name     string
		agentIDs []string
		chanIDs  []string
	}{
		{"atlas-runbook.md", []string{atlas.ID}, nil},
		{"beacon-runbook.md", []string{beacon.ID}, nil},
	} {
		if _, err := svc.Upload(ctx, ws.ID, user.ID, references.UploadInput{
			Filename: up.name, Data: []byte(refRunbookMD), AgentIDs: up.agentIDs, ChannelIDs: up.chanIDs,
		}); err != nil {
			t.Fatalf("upload %s: %v", up.name, err)
		}
	}

	memWorker, memSearch, memGate := newTestMemoryPipeline(st)
	composer = &manifestRecordingComposer{}
	registry := NewDefaultToolRegistry(st.Memories(), WithDocumentTools(svc))
	runner = NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(), st.AgentSessions(),
		st.GatewayLinks(),
		memWorker, memSearch, memGate,
		[]byte("test-key-32-bytes-long-12345678"),
		t.TempDir(),
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (Model, error) {
			return mdl, nil
		}),
		WithInstructionComposer(composer),
		WithToolRegistry(registry),
		WithReferences(svc),
	)

	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(runner.onClawDir), ws.Slug, atlas.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}
	return runner, composer, svc, ws.ID, atlas.ID, "sess-atlas-docs", user.ID
}

// TestRunner_SubagentsInheritDocumentToolsAndManifest is task 11.2's verify —
// the D9 regression guard: a child session spawned by an agent with document
// tools resolves `document.search` in its tool registry AND receives the
// reference-documents manifest for the parent's visibility scope. The child's
// search actually executes through the inherited service and returns exactly
// the parent-visible documents — the scope is neither widened nor narrowed.
func TestRunner_SubagentsInheritDocumentToolsAndManifest(t *testing.T) {
	mdl := &subagentsRunnerModel{
		parentScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			if call == 1 {
				return scriptedToolCall("p-call-1", "agent",
					`{"subagent_type":"general-purpose","prompt":"`+delegationMarker+` compare webhook signing across the reference documents","description":"document research"}`), nil
			}
			return textMessage("delegation done"), nil
		},
		childScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			if call == 1 {
				return scriptedToolCall("c-call-1", "document.search", `{"query":"signing"}`), nil
			}
			return usageTextMessage("child final report", 10, 5, 15), nil
		},
	}
	runner, composer, _, wsID, agentID, sessionID, userID := setupReferencesSubagentsRunner(t, mdl)

	events := runSubagentsTurn(t, runner, wsID, agentID, sessionID, userID, "delegate the document research")
	if !hasKind(events, TranscriptEventTurnCompleted) || hasKind(events, TranscriptEventError) {
		t.Fatalf("run must complete cleanly, got %+v", events)
	}

	// The child's tool registry resolved document.search: it is on the tool
	// surface the child's first model call saw (the clone shares the parent's
	// ToolsNodeConfig by construction).
	childSurface := mdl.callNames(t, true, 1)
	found := false
	for _, name := range childSurface {
		if name == "document.search" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("child tool surface = %v, want document.search present", childSurface)
	}

	// The child's composed instruction carries the manifest block — inherited
	// through the shared cfg.Instruction — for the parent's scope: the
	// atlas-attached document listed, the beacon-attached one absent.
	childInput := manifestJSON(t, mdl.callInput(t, true, 1))
	if !strings.Contains(childInput, "## Reference documents") || !strings.Contains(childInput, "atlas-runbook.md") {
		t.Fatalf("child instruction lost the manifest or the parent-visible document:\n%s", childInput)
	}
	if strings.Contains(childInput, "beacon-runbook.md") {
		t.Fatalf("child manifest widened beyond the parent's scope:\n%s", childInput)
	}

	// The child's document.search executed through the inherited service and
	// returned exactly the parent-visible documents.
	childResults := inputToolResultTexts(mdl.callInput(t, true, 2))
	if !strings.Contains(childResults, "atlas-runbook.md") {
		t.Fatalf("child document.search results = %q, want the parent-visible document", childResults)
	}
	if strings.Contains(childResults, "beacon-runbook.md") {
		t.Fatalf("child search widened beyond the parent's scope: %q", childResults)
	}

	// The parent's own manifest rode the same scope: identical membership,
	// pinned against a drifted resolution between parent and clone.
	parentParams := composer.lastParams()
	parentManifest := parentParams.ReferenceManifest
	if !strings.Contains(parentManifest, "atlas-runbook.md") || strings.Contains(parentManifest, "beacon-runbook.md") {
		t.Fatalf("parent manifest = %q, want exactly the parent's visibility scope", parentManifest)
	}
	// The manifest reaches the child's input verbatim: compare JSON-escaped
	// bodies (the outer quotes of a standalone marshal never appear in the
	// embedding).
	escaped := strings.TrimSuffix(strings.TrimPrefix(manifestJSON(t, parentManifest), `"`), `"`)
	if !strings.Contains(childInput, escaped) {
		t.Fatalf("child instruction manifest differs from the parent's rendered manifest:\nparent=%q\nchild input=%s", parentManifest, childInput)
	}
}

// TestRunner_SubagentsUnknownTypeReturnsToolResultAndContinues pins the
// unknown-type contract: an `agent` call naming an undeclared subagent type
// returns eino's not-found error as a tool result the model reads (the
// available types reach the model via the mid-conversation reminder, never
// the error text) and the run continues to a normal finish — never a run
// failure.
func TestRunner_SubagentsUnknownTypeReturnsToolResultAndContinues(t *testing.T) {
	mdl := &subagentsRunnerModel{
		parentScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			if call == 1 {
				return scriptedToolCall("p-call-1", "agent",
					`{"subagent_type":"no-such-type","prompt":"research the flaky test","description":"research task"}`), nil
			}
			return textMessage("standing by after unknown type"), nil
		},
	}
	runner, wsID, agentID, sessionID, userID := setupSubagentsRunner(t, mdl)

	events := runSubagentsTurn(t, runner, wsID, agentID, sessionID, userID, "delegate the research")

	if !hasKind(events, TranscriptEventTurnCompleted) || hasKind(events, TranscriptEventError) {
		t.Fatalf("run must complete cleanly, got %+v", events)
	}

	// The tool result names the requested type and reports not-found.
	result := inputToolResultTexts(mdl.callInput(t, false, 2))
	if !strings.Contains(result, "not found") || !strings.Contains(result, "no-such-type") {
		t.Fatalf("unknown-type tool result = %q, want the not-found error naming the requested type", result)
	}

	// The run continued: the parent read the result and finished the turn.
	if got := mdl.laneCallCount(false); got != 2 {
		t.Fatalf("parent model calls = %d, want 2 (tool call, then finish after reading the result)", got)
	}
	last := ""
	for i := range events {
		if events[i].Kind == TranscriptEventMessageCompleted && events[i].Message != nil && events[i].Message.Role == "assistant" {
			last = events[i].Message.Content
		}
	}
	if last != "standing by after unknown type" {
		t.Fatalf("run finished with %q, want the parent's post-error answer", last)
	}
}
