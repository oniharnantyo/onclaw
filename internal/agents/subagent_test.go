package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The subagent turn tests (tasks.md 5.2–5.4, spec agent-subagents): the agent
// tool delegates a task to a general-purpose clone of the parent — same
// persona/model/tools, fresh context, no recursion — and returns the child's
// answer as the tool result; the background lane lets the parent continue,
// collect the output, and cancel a run; every delegation materializes a
// transcript under the workspace .tasks directory that the parent's own file
// tools can read.

// newWorkspaceFixture builds a fixture whose agent owns a workspace: the
// manager captures the workspaces option at construction, and the fixtured
// agent then gets the slug that names that workspace.
func newWorkspaceFixture(t *testing.T, reply func(input []*schema.Message) fakeReply, opts ...Option) (*fixture, *workspaces.Manager) {
	t.Helper()
	ws := workspaces.NewManager(t.TempDir())
	all := append([]Option{WithWorkspaces(ws)}, opts...)
	f := newFixture(t, reply, all...)

	agent := f.agent
	agent.Slug = "tester"
	// The store preserves the column verbatim (its own default lives in the
	// DDL), and the services layer is what stamps the create-time default:
	// a turn fixture therefore opts the capability in explicitly.
	agent.SubagentsEnabled = true
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
	f.agent = agent
	return f, ws
}

// childCall reports that a model call is a delegated child's and returns the
// delegated task text. The child's context is exactly the delegated task as
// one user message (beside the shared system instruction), while every
// parent call either offers tools or carries its own call's tool result — so
// the task fragments are the discriminator: the parent's own prompt never
// contains them.
func childCall(input []*schema.Message, tasks ...string) (string, bool) {
	var user *schema.Message
	for _, m := range input {
		switch m.Role {
		case schema.Tool, schema.Assistant:
			return "", false
		case schema.User:
			if user != nil {
				return "", false
			}
			user = m
		}
	}
	if user == nil {
		return "", false
	}
	for _, task := range tasks {
		if strings.Contains(user.Content, task) {
			return user.Content, true
		}
	}
	return "", false
}

// sawToolMessage reports whether any message of a model call is a tool
// result (the parent's own delegate/collect call answering back).
func sawToolMessage(input []*schema.Message) bool {
	for _, msg := range input {
		if msg.Role == schema.Tool {
			return true
		}
	}
	return false
}

// delegateOnceReply scripts the canonical delegating parent under the
// always-background contract: the first call delegates task, the child's
// fresh context (whose user message IS the task text) gets the child answer,
// the parent collects the detached child's answer through a blocking
// task_output, and once a tool result carries that answer the parent closes
// the turn.
func delegateOnceReply(task, childAnswer, finalAnswer string) func([]*schema.Message) fakeReply {
	return func(input []*schema.Message) fakeReply {
		for _, m := range input {
			if m.Role == schema.User && strings.Contains(m.Content, task) {
				return fakeReply{text: childAnswer, promptTokens: 5, completionTokens: 3}
			}
		}
		if !sawToolMessage(input) {
			return toolCallsReply("call-1", ToolAgent, fmt.Sprintf(`{"task":%q}`, task))
		}
		if strings.Contains(textOfMessages(input, schema.Tool), childAnswer) {
			return fakeReply{text: finalAnswer, promptTokens: 30, completionTokens: 5}
		}
		// Yield to the detached child before polling, so the parent's loop
		// cannot outrun the child's completion at fake-model speed.
		time.Sleep(5 * time.Millisecond)
		return toolCallsReply("call-2", ToolTaskOutput, `{"task_id":"task-1"}`)
	}
}

// TestSubagent_DelegationRoundTrip is the core delegation path (spec
// agent-subagents "Delegation returns a result") under the always-background
// contract: the parent calls the agent tool and gets the task id back at
// once, the clone runs detached, the parent collects the child's final
// answer through task_output — and a terminal task_activity record on the
// parent's transcript carries the task identity and its transcript path.
func TestSubagent_DelegationRoundTrip(t *testing.T) {
	f, _ := newWorkspaceFixture(t, delegateOnceReply(
		"summarize the repository layout", "the layout is: cmd, internal, frontend", "Delegated: the layout is known."))

	ctx := t.Context()
	sess := f.newSession(t)
	collector := collect(t, f.mgr, sess.ID)

	turn, err := f.mgr.Send(ctx, sess.ID, "delegate the repository summary")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	// The launch answers with the task id; the collection call carries the
	// child's answer as its result.
	events, err := f.sessions.Events(ctx, sess.ID)
	require.NoError(t, err)
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.GreaterOrEqual(t, len(tools), 2, "the parent's delegate call and its task_output call(s)")
	require.Equal(t, ToolAgent, tools[0].Tool.Name)
	require.Equal(t, sessions.ToolStatusCompleted, tools[0].Tool.Status)
	require.Contains(t, tools[0].Tool.Result, "task-1", "the launch returns the task id, not the answer")
	require.NotContains(t, tools[0].Tool.Result, "the layout is: cmd, internal, frontend")
	last := tools[len(tools)-1]
	require.Equal(t, ToolTaskOutput, last.Tool.Name)
	require.Contains(t, last.Tool.Result, "the layout is: cmd, internal, frontend")

	// The task's terminal record is persisted on the parent transcript.
	tasks := eventsOfKind(events, sessions.KindTaskActivity)
	require.Len(t, tasks, 1, "one terminal task record is persisted")
	require.Equal(t, "task-1", tasks[0].Task.TaskID)
	require.Equal(t, sessions.ToolStatusCompleted, tasks[0].Task.Status)
	require.Equal(t, "summarize the repository layout", tasks[0].Task.Description)
	require.Contains(t, tasks[0].Task.Result, "the layout is: cmd, internal, frontend")
	require.True(t, tasks[0].Task.Background, "every delegation runs as a background task")
	require.Empty(t, tasks[0].Task.Progress, "the terminal record carries no live progress")
	// The transcript path is eino's reserved per-call file, the same path
	// task_output reports to the parent.
	require.Equal(t, filepath.Join(".tasks", "call-1.output"), tasks[0].Task.TranscriptPath)

	// The parent's final answer closes the turn.
	require.Contains(t, textOf(events, sessions.KindAssistantMessage), "Delegated: the layout is known.")

	// The live stream showed the task card while it ran (the running record
	// is live-only, design D10) and then the completion.
	live := eventsOfKind(collector.Wait(), sessions.KindTaskActivity)
	require.NotEmpty(t, live)
	require.Equal(t, sessions.ToolStatusRunning, live[0].Task.Status)
	require.Equal(t, sessions.ToolStatusCompleted, live[len(live)-1].Task.Status)
}

// TestSubagent_CloneToolSurface covers the clone's surface (spec
// agent-subagents "No recursion"): the child is offered the parent's tools —
// including the workspace-rooted ones — and never the agent tool itself.
func TestSubagent_CloneToolSurface(t *testing.T) {
	f, _ := newWorkspaceFixture(t, func(input []*schema.Message) fakeReply {
		if _, ok := childCall(input, "inspect the surface"); ok {
			return fakeReply{text: "the surface is as offered"}
		}
		if !sawToolMessage(input) {
			return toolCallsReply("call-1", ToolAgent, `{"task":"inspect the surface"}`)
		}
		if strings.Contains(textOfMessages(input, schema.Tool), "the surface is as offered") {
			return fakeReply{text: "done"}
		}
		time.Sleep(5 * time.Millisecond)
		return toolCallsReply("call-2", ToolTaskOutput, `{"task_id":"task-1"}`)
	})

	ctx := t.Context()
	agent := f.agent
	// A narrowed allowlist: the clone must be offered exactly these plus the
	// workspace tools the gate resolves.
	agent.Tools = []string{ToolCurrentTime, ToolAgent}
	require.NoError(t, f.store.Agents().Update(ctx, &agent))

	sess, err := f.mgr.CreateSession(ctx, agent.ID)
	require.NoError(t, err)
	turn, err := f.mgr.Send(ctx, sess.ID, "delegate with a narrow surface")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	parent := f.model.toolsOf(0)
	// The child runs detached, so its model call is found by content, not
	// by position.
	child := f.model.toolsOfCallWhere(func(input []*schema.Message) bool {
		_, ok := childCall(input, "inspect the surface")
		return ok
	})
	require.NotNil(t, child, "the clone's model call was recorded")
	require.Contains(t, parent, ToolAgent, "the parent is offered the delegation tool")
	require.Contains(t, child, ToolCurrentTime, "the clone keeps the parent's direct tools")
	require.NotContains(t, child, ToolAgent, "no recursion: the clone cannot delegate")
	require.NotContains(t, child, ToolTaskOutput)
	require.NotContains(t, child, ToolTaskStop)
	require.NotContains(t, child, ToolWebSearch, "tools outside the allowlist stay outside the clone's surface too")
	require.NotContains(t, child, ToolHTTPGet)
}

// TestSubagent_ToggleOffersNoAgentTool covers the per-agent toggle (spec
// agent-subagents "Disabled agent cannot delegate"): with subagents disabled
// the turn is composed without any delegation tool, so the model can never
// call one.
func TestSubagent_ToggleOffersNoAgentTool(t *testing.T) {
	f, _ := newWorkspaceFixture(t, func([]*schema.Message) fakeReply {
		return fakeReply{text: "no delegation needed"}
	})

	ctx := t.Context()
	agent := f.agent
	agent.SubagentsEnabled = false
	require.NoError(t, f.store.Agents().Update(ctx, &agent))

	sess, err := f.mgr.CreateSession(ctx, agent.ID)
	require.NoError(t, err)
	turn, err := f.mgr.Send(ctx, sess.ID, "just answer directly")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	for _, name := range f.model.toolsOf(0) {
		require.False(t, isDelegationTool(name), "a disabled agent must be offered no delegation tool, got %q", name)
	}
}

// TestSubagent_BackgroundLane covers the background lane end to end (spec
// agent-subagents "Parent continues while a task runs", "Output is
// collected"): the parent launches a delegation and finishes its own turn
// while the child keeps working, then a later turn collects the finished
// task's output — and the completion landed on the transcript meanwhile.
func TestSubagent_BackgroundLane(t *testing.T) {
	childStarted := make(chan struct{})
	childMayFinish := make(chan struct{})

	f, _ := newWorkspaceFixture(t, func(input []*schema.Message) fakeReply {
		if _, ok := childCall(input, "long research"); ok {
			close(childStarted)
			<-childMayFinish
			return fakeReply{text: "the child's report", promptTokens: 12, completionTokens: 3}
		}
		if !sawToolMessage(input) {
			return toolCallsReply("call-1", ToolAgent, `{"task":"long research","run_in_background":true}`)
		}
		return fakeReply{text: "Started the research in the background.", promptTokens: 20, completionTokens: 4}
	})

	ctx := t.Context()
	sess := f.newSession(t)
	collector := collect(t, f.mgr, sess.ID)

	turn, err := f.mgr.Send(ctx, sess.ID, "research this in the background")
	require.NoError(t, err)

	select {
	case <-childStarted:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the background child never started")
	}

	// The parent turn finishes while the child still runs: the launch tool
	// result carries the task id, not the child's answer.
	require.NoError(t, turn.Wait(), "the parent turn must finish while the child still runs")
	events, err := f.sessions.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.Contains(t, textOf(events, sessions.KindAssistantMessage), "Started the research in the background.")
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, tools, 1)
	require.Contains(t, tools[0].Tool.Result, "task-1")
	require.NotContains(t, tools[0].Tool.Result, "the child's report",
		"a background launch returns the task id, not the answer")

	// The live stream carried the running card (design D10).
	liveTasks := eventsOfKind(collector.Wait(), sessions.KindTaskActivity)
	require.NotEmpty(t, liveTasks, "the running task card must ride the live stream")
	require.Equal(t, sessions.ToolStatusRunning, liveTasks[0].Task.Status)
	require.True(t, liveTasks[0].Task.Background)

	// Let the child finish: its completion lands on the parent transcript
	// (spec agent-subagents "Task completion surfacing") even though the
	// parent turn already ended.
	close(childMayFinish)
	require.Eventually(t, func() bool {
		stored, err := f.sessions.Events(ctx, sess.ID)
		if err != nil {
			return false
		}
		tasks := eventsOfKind(stored, sessions.KindTaskActivity)
		return len(tasks) == 1 && tasks[0].Task.Status == sessions.ToolStatusCompleted
	}, 3*time.Second, 20*time.Millisecond, "the background completion must be recorded on the transcript")

	stored, err := f.sessions.Events(ctx, sess.ID)
	require.NoError(t, err)
	tasks := eventsOfKind(stored, sessions.KindTaskActivity)
	require.Equal(t, "task-1", tasks[0].Task.TaskID)
	require.True(t, tasks[0].Task.Background)
	require.Contains(t, tasks[0].Task.Result, "the child's report")
	require.Equal(t, filepath.Join(".tasks", "call-1.output"), tasks[0].Task.TranscriptPath)

	// The materialized transcript holds the child's output (streamed, so
	// reconstructed from the text steps like any JSONL reader would).
	raw, err := os.ReadFile(filepath.Join(f.mgr.workspaces.Root(), "tester", ".tasks", "call-1.output"))
	require.NoError(t, err)
	var text strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		var step struct {
			Step    string `json:"step"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(line), &step); err == nil && step.Step == "text" {
			text.WriteString(step.Content)
		}
	}
	require.Contains(t, text.String(), "the child's report")
}

// subagentTestLatch is the process-wide latch a scripted child waits on: the
// main test goroutine releases it once the child has started, so the parent's
// later control calls observe a live task. Reset by each test that uses it.
var subagentTestLatch chan struct{}

// subagentTestStopGate holds the scripted parent's task_stop call until the
// test has observed the task running, so the cancellation cannot race the
// registration. Nil outside the TaskStop test.
var subagentTestStopGate chan struct{}

// TestSubagent_TaskOutputCollectsCompletedRun covers task_output from the
// model's side (spec agent-subagents "Output is collected"): the parent
// launches a background task, the child finishes, and the parent collects the
// report through the control tool in the same run — the task space's
// lifetime.
func TestSubagent_TaskOutputCollectsCompletedRun(t *testing.T) {
	subagentTestLatch = make(chan struct{})
	t.Cleanup(func() { subagentTestLatch = nil })

	f, _ := newWorkspaceFixture(t, func(input []*schema.Message) fakeReply {
		if _, ok := childCall(input, "finish quickly"); ok {
			<-subagentTestLatch
			return fakeReply{text: "finished in the background", promptTokens: 8, completionTokens: 2}
		}
		if !sawToolMessage(input) {
			return toolCallsReply("call-1", ToolAgent, `{"task":"finish quickly","run_in_background":true}`)
		}
		if strings.Contains(textOfMessages(input, schema.Tool), "completed") {
			return fakeReply{text: "Collected: finished in the background."}
		}
		// Yield to the detached child before polling again, so the parent's
		// loop cannot outrun the child's completion at fake-model speed.
		time.Sleep(5 * time.Millisecond)
		return toolCallsReply("call-2", ToolTaskOutput, `{"task_id":"task-1"}`)
	})

	ctx := t.Context()
	sess := f.newSession(t)
	turn, err := f.mgr.Send(ctx, sess.ID, "delegate and collect")
	require.NoError(t, err)

	// The child runs detached; release it once the launch returned, then let
	// the parent's turn continue into its collection call.
	require.Eventually(t, func() bool {
		return taskRunning(f, sess.ID, "task-1")
	}, 2*time.Second, 10*time.Millisecond, "the background launch must register task-1")
	close(subagentTestLatch)
	require.NoError(t, turn.Wait())

	events, err := f.sessions.Events(ctx, sess.ID)
	require.NoError(t, err)
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.GreaterOrEqual(t, len(tools), 2, "the parent's delegate call and its task_output call(s)")
	require.Equal(t, ToolAgent, tools[0].Tool.Name)
	last := tools[len(tools)-1]
	require.Equal(t, ToolTaskOutput, last.Tool.Name)
	require.Contains(t, last.Tool.Result, "finished in the background")
	require.Contains(t, last.Tool.Result, "completed")
}

// TestSubagent_SequentialDelegationsGetDistinctIDs covers the run-scoped
// task-ID space (spec agent-subagents "Task identifiers SHALL be unique
// within the run"): two delegations in one turn are task-1 and task-2, each
// launched and then collected through task_output.
func TestSubagent_SequentialDelegationsGetDistinctIDs(t *testing.T) {
	f, _ := newWorkspaceFixture(t, func(input []*schema.Message) fakeReply {
		if childText, ok := childCall(input, "first task", "second task"); ok {
			// The child answers with its own task text, so the parent's
			// result checks can tell the two delegations apart.
			return fakeReply{text: "child says: " + childText}
		}
		toolMsgs := textOfMessages(input, schema.Tool)
		switch {
		case !sawToolMessage(input):
			return toolCallsReply("call-1", ToolAgent, `{"task":"first task"}`)
		case !strings.Contains(toolMsgs, "child says: first task"):
			time.Sleep(5 * time.Millisecond)
			return toolCallsReply("call-2", ToolTaskOutput, `{"task_id":"task-1"}`)
		case !strings.Contains(toolMsgs, "task-2"):
			return toolCallsReply("call-3", ToolAgent, `{"task":"second task"}`)
		case !strings.Contains(toolMsgs, "child says: second task"):
			time.Sleep(5 * time.Millisecond)
			return toolCallsReply("call-4", ToolTaskOutput, `{"task_id":"task-2"}`)
		default:
			return fakeReply{text: "both delegations returned"}
		}
	})

	ctx := t.Context()
	sess := f.newSession(t)
	turn, err := f.mgr.Send(ctx, sess.ID, "run two delegations")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	events, err := f.sessions.Events(ctx, sess.ID)
	require.NoError(t, err)
	tasks := eventsOfKind(events, sessions.KindTaskActivity)
	require.Len(t, tasks, 2)
	require.Equal(t, "task-1", tasks[0].Task.TaskID)
	require.Equal(t, "first task", tasks[0].Task.Description)
	require.Equal(t, "task-2", tasks[1].Task.TaskID)
	require.Equal(t, "second task", tasks[1].Task.Description)
}

// taskRunning reports whether the session's task space holds the given task
// in the running state. It is a read-only probe over the live delegation
// state: tests use it to sequence the main goroutine against a detached
// child. The map read takes the manager lock — the turn goroutine creates
// the entry concurrently.
func taskRunning(f *fixture, sessionID, taskID string) bool {
	f.mgr.mu.Lock()
	tasks := f.mgr.sessionTasks[sessionID]
	f.mgr.mu.Unlock()
	if tasks == nil {
		return false
	}
	snapshot, err := tasks.Lookup(taskID)
	if err != nil {
		return false
	}
	return !snapshot.Terminal()
}

// TestSubagent_TaskStopCancelsRunningTask covers the cancellation path
// (spec agent-subagents "Task is cancelled"): a running background
// delegation is cancelled by name through task_stop and reported stopped.
func TestSubagent_TaskStopCancelsRunningTask(t *testing.T) {
	subagentTestLatch = make(chan struct{})
	subagentTestStopGate = make(chan struct{})
	t.Cleanup(func() { subagentTestLatch = nil; subagentTestStopGate = nil })

	childCancelled := make(chan struct{})
	f, _ := newWorkspaceFixture(t, func(input []*schema.Message) fakeReply {
		if _, ok := childCall(input, "cancel me"); ok {
			<-subagentTestLatch
			ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
			defer cancel()
			<-ctx.Done()
			close(childCancelled)
			return fakeReply{err: context.Canceled}
		}
		if !sawToolMessage(input) {
			return toolCallsReply("call-1", ToolAgent, `{"task":"cancel me","run_in_background":true}`)
		}
		if strings.Contains(strings.ToLower(textOfMessages(input, schema.Tool)), "stop") {
			return fakeReply{text: "Stopped the task."}
		}
		// The launch returned the task id; hold the stop call until the test
		// has observed the task running (subagentTestStopGate).
		<-subagentTestStopGate
		return toolCallsReply("call-2", ToolTaskStop, `{"task_id":"task-1"}`)
	})

	ctx := t.Context()
	sess := f.newSession(t)
	turn, err := f.mgr.Send(ctx, sess.ID, "start and stop a task")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return taskRunning(f, sess.ID, "task-1")
	}, 2*time.Second, 10*time.Millisecond, "the background launch must register task-1")
	// Release the parent's stop first (the task is observed running), then
	// the child's blocked model call.
	close(subagentTestStopGate)
	close(subagentTestLatch)
	require.NoError(t, turn.Wait())

	select {
	case <-childCancelled:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "task_stop must cancel the child's run")
	}

	events, err := f.sessions.Events(ctx, sess.ID)
	require.NoError(t, err)
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, tools, 2, "the parent's delegate call and its task_stop call")
	require.Equal(t, ToolTaskStop, tools[1].Tool.Name)
	require.Contains(t, strings.ToLower(tools[1].Tool.Result), "stop")

	tasks := eventsOfKind(events, sessions.KindTaskActivity)
	require.NotEmpty(t, tasks)
	require.Equal(t, sessions.ToolStatusStopped, tasks[len(tasks)-1].Task.Status)
	require.NotContains(t, tasks[len(tasks)-1].Task.Result, "child", "a cancelled task reports no answer")
}

// TestSubagent_TranscriptReadableByParentTools covers spec agent-subagents
// "Child transcripts": the finished delegation's transcript is on disk under
// the workspace .tasks directory, and the parent's jailed read_file tool can
// read it through the workspace mount.
func TestSubagent_TranscriptReadableByParentTools(t *testing.T) {
	f, _ := newWorkspaceFixture(t, delegateOnceReply(
		"name the modules", "the modules are cmd, internal, frontend", "Delegated."))

	ctx := t.Context()
	sess := f.newSession(t)
	turn, err := f.mgr.Send(ctx, sess.ID, "delegate the module listing")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	// The file exists on disk inside the workspace.
	file := filepath.Join(f.mgr.workspaces.Root(), "tester", ".tasks", "call-1.output")
	raw, err := os.ReadFile(file)
	require.NoError(t, err, "the child transcript must be materialized under .tasks/")
	body := string(raw)
	require.Contains(t, body, `"step":"task"`)
	require.Contains(t, body, "name the modules")
	require.Contains(t, body, `"step":"text"`)
	require.Contains(t, body, `"status":"completed"`)

	// The child streamed its answer, so the sentence only exists as the
	// concatenation of the text steps' contents — reconstruct it the way a
	// reader of the JSONL transcript would.
	var text strings.Builder
	for _, line := range strings.Split(body, "\n") {
		var step struct {
			Step    string `json:"step"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(line), &step); err == nil && step.Step == "text" {
			text.WriteString(step.Content)
		}
	}
	require.Contains(t, text.String(), "the modules are cmd, internal, frontend")

	// And the parent's own file tools read it through the workspace mount:
	// run a second turn that reads /workspace/.tasks/call-1.output.
	f.model.reply = func(input []*schema.Message) fakeReply {
		if sawToolMessage(input) {
			return fakeReply{text: "the transcript is readable"}
		}
		return toolCallsReply("read-1", ToolReadFile, `{"file_path":"/workspace/.tasks/call-1.output"}`)
	}

	turn, err = f.mgr.Send(ctx, sess.ID, "read the task transcript back")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	events, err := f.sessions.Events(ctx, sess.ID)
	require.NoError(t, err)
	tools := eventsOfKind(events, sessions.KindToolActivity)
	last := tools[len(tools)-1]
	require.Equal(t, ToolReadFile, last.Tool.Name)
	require.Equal(t, sessions.ToolStatusCompleted, last.Tool.Status)
	require.Contains(t, last.Tool.Result, "the modules are cmd, internal, frontend",
		"the parent's jailed read_file must read the child transcript")
}

// textOfMessages concatenates the content of a call's messages with the given
// role, so a scripted reply can assert on what the model was shown.
func textOfMessages(input []*schema.Message, role schema.RoleType) string {
	var sb strings.Builder
	for _, msg := range input {
		if msg.Role == role {
			sb.WriteString(msg.Content)
		}
	}
	return sb.String()
}
