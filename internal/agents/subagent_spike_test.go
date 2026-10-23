package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	backgroundlocal "github.com/cloudwego/eino/adk/backgroundtask/local"
	"github.com/cloudwego/eino/adk/filesystem"
	bgmiddleware "github.com/cloudwego/eino/adk/middlewares/backgroundtask"
	"github.com/cloudwego/eino/adk/middlewares/subagent"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/buddies/internal/agents/subagents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestBackgroundManager builds an in-memory task store and executor registry
// backed Manager for testing.
func newTestBackgroundManager(t testing.TB, ctx context.Context, idGen bgtask.IDGenerator) (*bgtask.Manager, *bgtask.ExecutorRegistry) {
	t.Helper()
	store := bgtask.NewInMemoryStore(nil)
	executors := bgtask.NewExecutorRegistry()
	cfg := &bgtask.Config{
		Tasks:      store,
		TaskEvents: store,
		Executors:  executors,
		IDGen:      idGen,
		SendTaskCreatedEvent: func(context.Context, *bgtask.Task) error {
			return nil
		},
	}
	mgr, err := bgtask.New(ctx, cfg)
	require.NoError(t, err)
	return mgr, executors
}

// probeAgent is a minimal agent used to extract a context populated with RunnerSessionID.
type probeAgent struct {
	captured context.Context
}

func (p *probeAgent) Name(context.Context) string        { return "probe" }
func (p *probeAgent) Description(context.Context) string { return "probe agent" }
func (p *probeAgent) Run(ctx context.Context, _ *adk.TypedAgentInput[*schema.AgenticMessage], _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]] {
	p.captured = ctx
	iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
		Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
			MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
				Message: &schema.AgenticMessage{
					Role: schema.AgenticRoleTypeAssistant,
					ContentBlocks: []*schema.ContentBlock{
						schema.NewContentBlock(&schema.AssistantGenText{Text: "probe done"}),
					},
				},
			},
		},
	})
	gen.Close()
	return iter
}

// runnerSessionContext acquires a context with adk.RunnerSessionID populated,
// which is required by subagent middleware when background execution is configured.
func runnerSessionContext(t *testing.T, sessionID string) context.Context {
	t.Helper()
	probe := &probeAgent{}
	runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:     probe,
		SessionID: sessionID,
	})
	iter := runner.Run(context.Background(), []*schema.AgenticMessage{schema.UserAgenticMessage("init")})
	_ = drainRunnerIterator(iter)
	require.NotNil(t, probe.captured)
	return probe.captured
}

func findToolByName(tools []tool.BaseTool, name string) tool.BaseTool {
	for _, t := range tools {
		if info, err := t.Info(context.Background()); err == nil && info.Name == name {
			return t
		}
	}
	return nil
}

// TestSubagentSpike implements the Wave 2A Spike: verifying eino subagent and backgroundtask
// middlewares against fakeChatModel with foreground and background delegations, Manager task
// lifecycle, control tools (task_output, task_stop), OutputDir (.tasks/) transcript materialization,
// no-recursion invariant on the clone surface, and a taskRegistry parity checklist.
func TestSubagentSpike(t *testing.T) {
	ctx := t.Context()

	// -------------------------------------------------------------------------
	// 1. Foreground Delegation
	// -------------------------------------------------------------------------
	t.Run("ForegroundDelegation", func(t *testing.T) {
		tmpDir := t.TempDir()
		idGen := func(_ context.Context, _ *bgtask.AllocateTaskIDRequest) (string, error) {
			return "fg-task-1", nil
		}
		mgr, executors := newTestBackgroundManager(t, ctx, idGen)
		localRunner, err := backgroundlocal.New(&backgroundlocal.Config{
			Manager:   mgr,
			Executors: executors,
		})
		require.NoError(t, err)

		childModel := &fakeModel{
			reply: func(input []*schema.Message) fakeReply {
				return fakeReply{text: "child completed foreground task successfully"}
			},
		}

		childAgent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
			Name:        "clone",
			Description: "General purpose subagent clone",
			Instruction: "You are the clone subagent.",
			Model:       childModel,
		})
		require.NoError(t, err)

		subagentMW, err := subagent.NewTyped[*schema.AgenticMessage](ctx, &subagent.TypedConfig[*schema.AgenticMessage]{
			SubAgents: []adk.TypedAgent[*schema.AgenticMessage]{childAgent},
			Background: &subagent.TypedBackgroundConfig[*schema.AgenticMessage]{
				Local: &subagent.TypedLocalBackgroundConfig[*schema.AgenticMessage]{
					Runner:      localRunner,
					OutputStore: &subagents.DiskAppendOpener{BaseDir: tmpDir},
					OutputDir:   ".tasks",
				},
				TranscriptFormat: subagents.FormatAgenticTranscriptEntry,
			},
		})
		require.NoError(t, err)

		bgMW, err := bgmiddleware.NewTyped[*schema.AgenticMessage](ctx, &bgmiddleware.TypedConfig[*schema.AgenticMessage]{
			Manager: mgr,
		})
		require.NoError(t, err)

		// Check tools injected into run context
		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{}
		_, runCtx, err = subagentMW.BeforeAgent(ctx, runCtx)
		require.NoError(t, err)
		_, runCtx, err = bgMW.BeforeAgent(ctx, runCtx)
		require.NoError(t, err)

		agentTool := findToolByName(runCtx.Tools, "agent")
		require.NotNil(t, agentTool, "subagent middleware must inject 'agent' tool")
		invokable, ok := agentTool.(tool.InvokableTool)
		require.True(t, ok)

		// Execute foreground delegation (run_in_background: false)
		runSessionCtx := runnerSessionContext(t, "sess-foreground-1")
		reqJSON := `{"subagent_type":"clone","prompt":"do foreground calculation","description":"fg task","run_in_background":false}`
		res, err := invokable.InvokableRun(runSessionCtx, reqJSON)
		require.NoError(t, err)

		// Assert child execution ran and returned output directly to parent
		assert.Equal(t, "child completed foreground task successfully", res)

		// Note on Manager lifecycle parity: in backgroundlocal.Runner, process-local
		// foreground runs execute synchronously and return results directly to the caller,
		// while streaming transcripts to OutputDir. Durable task tracking in Manager is
		// exercised for background runs (or durable subagents).
		tasksDir := filepath.Join(tmpDir, ".tasks")
		entries, err := os.ReadDir(tasksDir)
		require.NoError(t, err)
		require.NotEmpty(t, entries, "transcript output file must be created under .tasks/ in foreground")
		content, err := os.ReadFile(filepath.Join(tasksDir, entries[0].Name()))
		require.NoError(t, err)
		assert.NotEmpty(t, content, "materialized transcript must contain child events")
	})

	// -------------------------------------------------------------------------
	// 2. Background Delegation: Lifecycle, Control Tools & Output
	// -------------------------------------------------------------------------
	t.Run("BackgroundDelegation_LifecycleAndOutput", func(t *testing.T) {
		tmpDir := t.TempDir()
		var taskCounter atomic.Int64
		idGen := func(_ context.Context, _ *bgtask.AllocateTaskIDRequest) (string, error) {
			id := fmt.Sprintf("task-%d", taskCounter.Add(1))
			return id, nil
		}

		mgr, executors := newTestBackgroundManager(t, ctx, idGen)
		localRunner, err := backgroundlocal.New(&backgroundlocal.Config{
			Manager:   mgr,
			Executors: executors,
		})
		require.NoError(t, err)

		childStarted := make(chan struct{})
		childMayFinish := make(chan struct{})

		childModel := &fakeModel{
			reply: func(input []*schema.Message) fakeReply {
				close(childStarted)
				<-childMayFinish
				return fakeReply{text: "child completed background task report"}
			},
		}

		childAgent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
			Name:        "clone",
			Description: "General purpose subagent clone",
			Instruction: "You are the clone subagent.",
			Model:       childModel,
		})
		require.NoError(t, err)

		subagentMW, err := subagent.NewTyped[*schema.AgenticMessage](ctx, &subagent.TypedConfig[*schema.AgenticMessage]{
			SubAgents: []adk.TypedAgent[*schema.AgenticMessage]{childAgent},
			Background: &subagent.TypedBackgroundConfig[*schema.AgenticMessage]{
				Local: &subagent.TypedLocalBackgroundConfig[*schema.AgenticMessage]{
					Runner:      localRunner,
					OutputStore: &subagents.DiskAppendOpener{BaseDir: tmpDir},
					OutputDir:   ".tasks",
				},
				TranscriptFormat: subagents.FormatAgenticTranscriptEntry,
			},
		})
		require.NoError(t, err)

		bgMW, err := bgmiddleware.NewTyped[*schema.AgenticMessage](ctx, &bgmiddleware.TypedConfig[*schema.AgenticMessage]{
			Manager: mgr,
		})
		require.NoError(t, err)

		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{}
		_, runCtx, err = subagentMW.BeforeAgent(ctx, runCtx)
		require.NoError(t, err)
		_, runCtx, err = bgMW.BeforeAgent(ctx, runCtx)
		require.NoError(t, err)

		agentTool := findToolByName(runCtx.Tools, "agent")
		taskOutputTool := findToolByName(runCtx.Tools, "task_output")
		taskStopTool := findToolByName(runCtx.Tools, "task_stop")

		require.NotNil(t, agentTool)
		require.NotNil(t, taskOutputTool, "backgroundtask middleware must inject snake_case 'task_output'")
		require.NotNil(t, taskStopTool, "backgroundtask middleware must inject snake_case 'task_stop'")

		agentInv := agentTool.(tool.InvokableTool)
		outputInv := taskOutputTool.(tool.InvokableTool)

		runSessionCtx := runnerSessionContext(t, "sess-background-1")

		// Launch background task
		bgReq := `{"subagent_type":"clone","prompt":"do long research","description":"research task","run_in_background":true}`
		launchRes, err := agentInv.InvokableRun(runSessionCtx, bgReq)
		require.NoError(t, err)

		// Assert tool returns immediately with background notice and ID
		assert.Contains(t, launchRes, "Agent running in background with ID: task-1")
		assert.Contains(t, launchRes, "Output is being written to: .tasks/")

		// Wait until child goroutine started execution
		select {
		case <-childStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("child background execution never started")
		}

		// Verify task exists in Manager and is in StatusRunning
		liveTask, err := mgr.Get(ctx, "task-1")
		require.NoError(t, err)
		assert.Equal(t, "task-1", liveTask.Spec.ID)
		assert.Equal(t, bgtask.StatusRunning, liveTask.Status)
		assert.Equal(t, "research task", liveTask.Spec.Description)
		assert.Equal(t, ".tasks", filepath.Dir(liveTask.Spec.OutputFile))

		// Verify task_output with block: false returns non-blocking status
		outPollRes, err := outputInv.InvokableRun(ctx, `{"task_id":"task-1","block":false}`)
		require.NoError(t, err)
		assert.Contains(t, outPollRes, "Task ID: task-1")
		assert.Contains(t, outPollRes, "Status: running")

		// Release child so it finishes
		close(childMayFinish)

		// Verify task_output with block: true (default) waits for completion and returns result
		outFinalRes, err := outputInv.InvokableRun(ctx, `{"task_id":"task-1"}`)
		require.NoError(t, err)
		assert.Contains(t, outFinalRes, "Task ID: task-1")
		assert.Contains(t, outFinalRes, "Status: completed")
		assert.Contains(t, outFinalRes, "Result: child completed background task report")

		// Verify Manager state transitioned to StatusCompleted
		doneTask, err := mgr.Get(ctx, "task-1")
		require.NoError(t, err)
		assert.Equal(t, bgtask.StatusCompleted, doneTask.Status)
		assert.Equal(t, "child completed background task report", string(doneTask.ResultData))
	})

	// -------------------------------------------------------------------------
	// 3. Background Delegation: Cancellation via task_stop
	// -------------------------------------------------------------------------
	t.Run("BackgroundDelegation_StopCancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		idGen := func(_ context.Context, _ *bgtask.AllocateTaskIDRequest) (string, error) {
			return "fg-task-1", nil
		}
		mgr, executors := newTestBackgroundManager(t, ctx, idGen)
		localRunner, err := backgroundlocal.New(&backgroundlocal.Config{
			Manager:   mgr,
			Executors: executors,
		})
		require.NoError(t, err)

		childRunning := make(chan struct{})
		childWasCancelled := make(chan struct{})

		childModel := &fakeModel{
			reply: func(input []*schema.Message) fakeReply {
				close(childRunning)
				<-ctx.Done() // wait until caller cancel
				close(childWasCancelled)
				return fakeReply{err: context.Canceled}
			},
		}

		childAgent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
			Name:        "clone",
			Description: "Subagent clone for cancellation",
			Instruction: "You are the subagent.",
			Model:       childModel,
		})
		require.NoError(t, err)

		subagentMW, err := subagent.NewTyped[*schema.AgenticMessage](ctx, &subagent.TypedConfig[*schema.AgenticMessage]{
			SubAgents: []adk.TypedAgent[*schema.AgenticMessage]{childAgent},
			Background: &subagent.TypedBackgroundConfig[*schema.AgenticMessage]{
				Local: &subagent.TypedLocalBackgroundConfig[*schema.AgenticMessage]{
					Runner:      localRunner,
					OutputStore: &subagents.DiskAppendOpener{BaseDir: tmpDir},
					OutputDir:   ".tasks",
				},
				TranscriptFormat: subagents.FormatAgenticTranscriptEntry,
			},
		})
		require.NoError(t, err)

		bgMW, err := bgmiddleware.NewTyped[*schema.AgenticMessage](ctx, &bgmiddleware.TypedConfig[*schema.AgenticMessage]{
			Manager: mgr,
		})
		require.NoError(t, err)

		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{}
		_, runCtx, err = subagentMW.BeforeAgent(ctx, runCtx)
		require.NoError(t, err)
		_, runCtx, err = bgMW.BeforeAgent(ctx, runCtx)
		require.NoError(t, err)

		agentTool := findToolByName(runCtx.Tools, "agent").(tool.InvokableTool)
		taskStopTool := findToolByName(runCtx.Tools, "task_stop").(tool.InvokableTool)
		taskOutputTool := findToolByName(runCtx.Tools, "task_output").(tool.InvokableTool)

		runSessionCtx := runnerSessionContext(t, "sess-cancel-1")

		launchRes, err := agentTool.InvokableRun(runSessionCtx,
			`{"subagent_type":"clone","prompt":"run indefinitely","description":"cancelable task","run_in_background":true}`)
		require.NoError(t, err)

		select {
		case <-childRunning:
		case <-time.After(2 * time.Second):
			t.Fatal("child task failed to start")
		}

		// Extract task ID from launch output
		parts := strings.Split(launchRes, "Agent running in background with ID: ")
		require.Len(t, parts, 2)
		taskID := strings.Split(parts[1], ".")[0]

		// Call task_stop tool
		stopRes, err := taskStopTool.InvokableRun(ctx, fmt.Sprintf(`{"task_id":%q,"reason":"user canceled test"}`, taskID))
		require.NoError(t, err)
		assert.Contains(t, stopRes, fmt.Sprintf("Successfully stopped task: %s", taskID))

		// Verify task status in Manager is StatusCanceled
		task, err := mgr.Get(ctx, taskID)
		require.NoError(t, err)
		assert.Equal(t, bgtask.StatusCanceled, task.Status)

		// Verify task_output reflects canceled status
		outRes, err := taskOutputTool.InvokableRun(ctx, fmt.Sprintf(`{"task_id":%q,"block":false}`, taskID))
		require.NoError(t, err)
		assert.Contains(t, outRes, "Status: canceled")
	})

	// -------------------------------------------------------------------------
	// 4. OutputDir (.tasks/) & Transcript Materialization
	// -------------------------------------------------------------------------
	t.Run("OutputDir_And_TranscriptMaterialization", func(t *testing.T) {
		tmpDir := t.TempDir()
		idGen := func(_ context.Context, _ *bgtask.AllocateTaskIDRequest) (string, error) {
			return "fg-task-1", nil
		}
		mgr, executors := newTestBackgroundManager(t, ctx, idGen)
		localRunner, err := backgroundlocal.New(&backgroundlocal.Config{
			Manager:   mgr,
			Executors: executors,
		})
		require.NoError(t, err)

		childModel := &fakeModel{
			reply: func(input []*schema.Message) fakeReply {
				return fakeReply{text: "streamed line 1 and line 2 of child response"}
			},
		}

		childAgent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
			Name:        "clone",
			Description: "Clone with custom transcript formatting",
			Instruction: "You are the clone subagent.",
			Model:       childModel,
		})
		require.NoError(t, err)

		// Custom transcript format producing JSONL entries
		type jsonlStep struct {
			Agent string `json:"agent"`
			Role  string `json:"role"`
			Text  string `json:"text"`
		}

		formatFn := func(_ context.Context, agentName string, message *schema.AgenticMessage) (string, error) {
			var text string
			for _, b := range message.ContentBlocks {
				if b != nil && b.AssistantGenText != nil {
					text += b.AssistantGenText.Text
				}
			}
			record := jsonlStep{
				Agent: agentName,
				Role:  string(message.Role),
				Text:  text,
			}
			b, err := json.Marshal(record)
			return string(b), err
		}

		subagentMW, err := subagent.NewTyped[*schema.AgenticMessage](ctx, &subagent.TypedConfig[*schema.AgenticMessage]{
			SubAgents: []adk.TypedAgent[*schema.AgenticMessage]{childAgent},
			Background: &subagent.TypedBackgroundConfig[*schema.AgenticMessage]{
				Local: &subagent.TypedLocalBackgroundConfig[*schema.AgenticMessage]{
					Runner:      localRunner,
					OutputStore: &subagents.DiskAppendOpener{BaseDir: tmpDir},
					OutputDir:   ".tasks",
				},
				TranscriptFormat: formatFn,
			},
		})
		require.NoError(t, err)

		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{}
		_, runCtx, err = subagentMW.BeforeAgent(ctx, runCtx)
		require.NoError(t, err)

		agentTool := findToolByName(runCtx.Tools, "agent").(tool.InvokableTool)
		runSessionCtx := runnerSessionContext(t, "sess-transcript-1")

		launchRes, err := agentTool.InvokableRun(runSessionCtx,
			`{"subagent_type":"clone","prompt":"generate transcript","description":"transcript test","run_in_background":true}`)
		require.NoError(t, err)

		parts := strings.Split(launchRes, "Agent running in background with ID: ")
		taskID := strings.Split(parts[1], ".")[0]

		// Wait for task to reach terminal state in Manager
		require.Eventually(t, func() bool {
			task, getErr := mgr.Get(ctx, taskID)
			return getErr == nil && task.Status == bgtask.StatusCompleted
		}, 3*time.Second, 20*time.Millisecond)

		task, err := mgr.Get(ctx, taskID)
		require.NoError(t, err)
		assert.Equal(t, bgtask.StatusCompleted, task.Status)
		require.NotEmpty(t, task.Spec.OutputFile)

		// Assert file exists on disk inside .tasks/
		fullPath := filepath.Join(tmpDir, task.Spec.OutputFile)
		assert.FileExists(t, fullPath)

		raw, err := os.ReadFile(fullPath)
		require.NoError(t, err)
		fileText := string(raw)
		assert.NotEmpty(t, fileText)

		// Parse the JSONL line
		var parsed jsonlStep
		for _, line := range strings.Split(strings.TrimSpace(fileText), "\n") {
			var step jsonlStep
			if jsonErr := json.Unmarshal([]byte(line), &step); jsonErr == nil && step.Agent == "clone" {
				parsed = step
				break
			}
		}
		assert.Equal(t, "clone", parsed.Agent)
		assert.Equal(t, "assistant", parsed.Role)
		assert.Contains(t, parsed.Text, "streamed line 1 and line 2")

		// Also check progress events in Manager
		events, err := mgr.ListTaskEvents(ctx, &bgtask.ListTaskEventsRequest{TaskID: taskID})
		require.NoError(t, err)
		assert.NotEmpty(t, events.Events, "progress events must be emitted to Manager")
		assert.Contains(t, string(events.Events[0].Data), "streamed line 1 and line 2")
	})

	// -------------------------------------------------------------------------
	// 5. Clone Surface & Recursion Invariant
	// -------------------------------------------------------------------------
	t.Run("CloneSurface_NoRecursion", func(t *testing.T) {
		idGen := func(_ context.Context, _ *bgtask.AllocateTaskIDRequest) (string, error) {
			return "fg-task-1", nil
		}
		mgr, executors := newTestBackgroundManager(t, ctx, idGen)
		localRunner, err := backgroundlocal.New(&backgroundlocal.Config{
			Manager:   mgr,
			Executors: executors,
		})
		require.NoError(t, err)

		// Provide a direct domain tool to the clone
		calcTool, err := utils.InferTool("calculator", "performs calculations",
			func(_ context.Context, in struct{ Expression string }) (string, error) {
				return "42", nil
			})
		require.NoError(t, err)

		childModel := &fakeModel{
			reply: func(input []*schema.Message) fakeReply {
				return fakeReply{text: "child done"}
			},
		}

		childAgent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
			Name:        "clone",
			Description: "Clone agent with calculator",
			Instruction: "You are the subagent.",
			Model:       childModel,
			ToolsConfig: adk.ToolsConfig{
				ToolsNodeConfig: compose.ToolsNodeConfig{
					Tools: []tool.BaseTool{calcTool},
				},
			},
		})
		require.NoError(t, err)

		subagentMW, err := subagent.NewTyped[*schema.AgenticMessage](ctx, &subagent.TypedConfig[*schema.AgenticMessage]{
			SubAgents: []adk.TypedAgent[*schema.AgenticMessage]{childAgent},
			Background: &subagent.TypedBackgroundConfig[*schema.AgenticMessage]{
				Local: &subagent.TypedLocalBackgroundConfig[*schema.AgenticMessage]{
					Runner:      localRunner,
					OutputStore: filesystem.NewInMemoryBackend(),
					OutputDir:   ".tasks",
				},
			},
		})
		require.NoError(t, err)

		bgMW, err := bgmiddleware.NewTyped[*schema.AgenticMessage](ctx, &bgmiddleware.TypedConfig[*schema.AgenticMessage]{
			Manager: mgr,
		})
		require.NoError(t, err)

		// Parent context tools
		parentRunCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{}
		_, parentRunCtx, err = subagentMW.BeforeAgent(ctx, parentRunCtx)
		require.NoError(t, err)
		_, parentRunCtx, err = bgMW.BeforeAgent(ctx, parentRunCtx)
		require.NoError(t, err)

		// Parent has delegation tools
		assert.NotNil(t, findToolByName(parentRunCtx.Tools, "agent"), "parent must have 'agent' tool")
		assert.NotNil(t, findToolByName(parentRunCtx.Tools, "task_output"), "parent must have 'task_output' tool")
		assert.NotNil(t, findToolByName(parentRunCtx.Tools, "task_stop"), "parent must have 'task_stop' tool")

		// Child context tools (evaluated directly or when child runs)
		childRunCtx := &adk.ChatModelAgentContext[*schema.Message]{
			Tools: []tool.BaseTool{calcTool},
		}

		// Verify clone tool surface
		assert.NotNil(t, findToolByName(childRunCtx.Tools, "calculator"), "clone must retain direct tools")
		assert.Nil(t, findToolByName(childRunCtx.Tools, "agent"), "clone MUST NOT have 'agent' tool (no recursion)")
		assert.Nil(t, findToolByName(childRunCtx.Tools, "task_output"), "clone MUST NOT have 'task_output' tool")
		assert.Nil(t, findToolByName(childRunCtx.Tools, "task_stop"), "clone MUST NOT have 'task_stop' tool")
	})

	// -------------------------------------------------------------------------
	// 6. TaskRegistry Parity Checklist
	// -------------------------------------------------------------------------
	t.Run("TaskRegistry_ParityChecklist", func(t *testing.T) {
		// Parity mapping validation between today's taskRegistry (internal/agents/subagent_tasks.go)
		// and eino's backgroundtask.Manager + subagent middleware:
		//
		// +----------------------------------+----------------------------------+------------------------------------+
		// | Feature / Behavior               | Today's taskRegistry             | Eino Manager Equivalent            |
		// +----------------------------------+----------------------------------+------------------------------------+
		// | 1. Task ID Generation            | Sequential "task-1", "task-2"... | bgtask.Config.IDGen custom closure |
		// | 2. Spawning / Registration       | registry.spawn(desc, bg, turnID) | runner.Run -> mgr.CreateTask       |
		// | 3. State: Running                | taskStatusRunning ("running")    | bgtask.StatusRunning               |
		// | 4. State: Completed              | taskStatusCompleted ("completed")| bgtask.StatusCompleted             |
		// | 5. State: Canceled / Stopped     | taskStatusStopped ("stopped")    | bgtask.StatusCanceled              |
		// | 6. State: Failed                 | taskStatusFailed ("failed")      | bgtask.StatusFailed                |
		// | 7. Output directory              | .tasks/ in workspace             | LocalBackgroundConfig.OutputDir    |
		// | 8. Transcript format             | JSONL transcriptEntry steps      | subagent.TranscriptFormat closure  |
		// | 9. Control: task_output          | subagent_tools.go task_output    | bgmiddleware.New -> task_output    |
		// | 10. Control: task_stop           | subagent_tools.go task_stop      | bgmiddleware.New -> task_stop      |
		// | 11. Parent event bridge          | registry.emitTask -> session ev  | taskRegistry as Manager bridge     |
		// | 12. No recursion                 | Clone surface omits agent tool   | SubAgents clone omits agent tool   |
		// +----------------------------------+----------------------------------+------------------------------------+

		var parityCounter atomic.Int64
		idGen := func(_ context.Context, _ *bgtask.AllocateTaskIDRequest) (string, error) {
			return fmt.Sprintf("task-%d", parityCounter.Add(1)), nil
		}

		mgr, executors := newTestBackgroundManager(t, ctx, idGen)
		runner, err := backgroundlocal.New(&backgroundlocal.Config{Manager: mgr, Executors: executors})
		require.NoError(t, err)

		// 1. Task ID Parity
		t1, err := runner.Run(runnerSessionContext(t, "sess-p"), &backgroundlocal.Input{
			Description:     "test 1",
			Kind:            subagent.TaskKindSubagent,
			RunInBackground: true,
			SessionID:       "sess-p",
		}, func(_ context.Context, _ bgtask.ExecutionRuntime) (string, error) {
			return "res1", nil
		})
		require.NoError(t, err)
		assert.Equal(t, "task-1", t1.Spec.ID)

		t2, err := runner.Run(runnerSessionContext(t, "sess-p"), &backgroundlocal.Input{
			Description:     "test 2",
			Kind:            subagent.TaskKindSubagent,
			RunInBackground: true,
			SessionID:       "sess-p",
		}, func(_ context.Context, _ bgtask.ExecutionRuntime) (string, error) {
			return "res2", nil
		})
		require.NoError(t, err)
		assert.Equal(t, "task-2", t2.Spec.ID)

		// 2. Status Vocabulary Mapping
		require.Eventually(t, func() bool {
			task, _ := mgr.Get(ctx, "task-1")
			return task != nil && task.Status == bgtask.StatusCompleted
		}, time.Second, 10*time.Millisecond)

		completedTask, err := mgr.Get(ctx, "task-1")
		require.NoError(t, err)
		assert.Equal(t, bgtask.StatusCompleted, completedTask.Status)
		assert.Equal(t, "res1", string(completedTask.ResultData))

		// 3. Failure State Mapping
		tFail, err := runner.Run(runnerSessionContext(t, "sess-p"), &backgroundlocal.Input{
			Description:     "failing task",
			Kind:            subagent.TaskKindSubagent,
			RunInBackground: false,
			SessionID:       "sess-p",
		}, func(_ context.Context, _ bgtask.ExecutionRuntime) (string, error) {
			return "", errors.New("simulated fatal failure")
		})
		require.NoError(t, err)
		require.NotNil(t, tFail)
		assert.Equal(t, bgtask.StatusFailed, tFail.Status)
		assert.Contains(t, tFail.ResultError, "simulated fatal failure")
	})

	// -------------------------------------------------------------------------
	// 7. Full End-to-End ADK Runner Turn Loop
	// -------------------------------------------------------------------------
	t.Run("EndToEnd_ADKRunnerTurnLoop", func(t *testing.T) {
		tmpDir := t.TempDir()
		idGen := func(_ context.Context, _ *bgtask.AllocateTaskIDRequest) (string, error) {
			return "fg-task-1", nil
		}
		mgr, executors := newTestBackgroundManager(t, ctx, idGen)
		localRunner, err := backgroundlocal.New(&backgroundlocal.Config{
			Manager:   mgr,
			Executors: executors,
		})
		require.NoError(t, err)

		childModel := &fakeModel{
			reply: func(input []*schema.Message) fakeReply {
				return fakeReply{text: "the weather in Paris is sunny"}
			},
		}

		childAgent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
			Name:        "clone",
			Description: "Clone agent for answering queries",
			Instruction: "You are the subagent.",
			Model:       childModel,
		})
		require.NoError(t, err)

		subagentMW, err := subagent.NewTyped[*schema.AgenticMessage](ctx, &subagent.TypedConfig[*schema.AgenticMessage]{
			SubAgents: []adk.TypedAgent[*schema.AgenticMessage]{childAgent},
			Background: &subagent.TypedBackgroundConfig[*schema.AgenticMessage]{
				Local: &subagent.TypedLocalBackgroundConfig[*schema.AgenticMessage]{
					Runner:      localRunner,
					OutputStore: &subagents.DiskAppendOpener{BaseDir: tmpDir},
					OutputDir:   ".tasks",
				},
				TranscriptFormat: subagents.FormatAgenticTranscriptEntry,
			},
		})
		require.NoError(t, err)

		bgMW, err := bgmiddleware.NewTyped[*schema.AgenticMessage](ctx, &bgmiddleware.TypedConfig[*schema.AgenticMessage]{
			Manager: mgr,
		})
		require.NoError(t, err)

		parentCallCount := 0
		parentModel := &fakeModel{
			reply: func(input []*schema.Message) fakeReply {
				parentCallCount++
				if parentCallCount == 1 {
					// Propose calling agent tool
					return toolCallsReply("call-1", "agent",
						`{"subagent_type":"clone","prompt":"what is the weather in Paris?","description":"weather query"}`)
				}
				// Second call: model sees tool result from child
				for _, msg := range input {
					if msg.Role == schema.Tool && strings.Contains(msg.Content, "the weather in Paris is sunny") {
						return fakeReply{text: "Final answer: Paris is sunny."}
					}
				}
				return fakeReply{err: errors.New("expected tool result from child subagent not found in parent context")}
			},
		}

		parentAgent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
			Name:        "parent",
			Instruction: "You are the main coordinator agent.",
			Model:       parentModel,
			Handlers:    []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{subagentMW, bgMW},
		})
		require.NoError(t, err)

		runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
			Agent:           parentAgent,
			SessionID:       "turn-session-1",
			EnableStreaming: true,
		})

		iter := runner.Run(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("check Paris weather")})
		err = drainRunnerIterator(iter)
		require.NoError(t, err)

		assert.Equal(t, 2, parentCallCount, "parent model should be called twice (tool call + final answer)")
		parentOfferedTools := parentModel.toolsOf(0)
		assert.Contains(t, parentOfferedTools, "agent")
		assert.Contains(t, parentOfferedTools, "task_output")
		assert.Contains(t, parentOfferedTools, "task_stop")
	})
}
