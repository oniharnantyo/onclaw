package agents

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	einofs "github.com/cloudwego/eino/adk/filesystem"
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// This file is the D4 spike (task 3.1): prove that a tool.Interrupt returned
// from a filesystem.Shell implementation surfaces as
// event.Action.Interrupted through the filesystem middleware's execute tool,
// and that ResumeWithParams with the interrupt ID re-runs the shell with the
// decision as resume data.

func init() {
	schema.Register[spikeApproval]()
}

// interruptingShell is a minimal filesystem.Shell that always interrupts,
// mirroring what JailedShell does for dangerous commands.
type interruptingShell struct {
	interrupts int
	executions int
}

func (s *interruptingShell) Execute(ctx context.Context, req *einofs.ExecuteRequest) (*einofs.ExecuteResponse, error) {
	isTarget, hasData, approved := tool.GetResumeContext[bool](ctx)
	if isTarget {
		s.executions++
		if !hasData || !approved {
			return &einofs.ExecuteResponse{Output: "denied", ExitCode: intPtr(126)}, nil
		}
		return &einofs.ExecuteResponse{Output: "ran: " + req.Command, ExitCode: intPtr(0)}, nil
	}
	s.interrupts++
	return nil, tool.Interrupt(ctx, spikeApproval{Command: req.Command})
}

// spikeApproval must be a named struct: the checkpoint is gob-encoded, and
// interface payloads need concrete types.
type spikeApproval struct {
	Command string
}

func intPtr(i int) *int { return &i }

// scriptedToolCallModel first emits a tool call, then a final text message.
type scriptedToolCallModel struct {
	calls int
}

func (m *scriptedToolCallModel) Generate(ctx context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.calls++
	if m.calls == 1 {
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{Type: schema.ContentBlockTypeFunctionToolCall, FunctionToolCall: &schema.FunctionToolCall{
					CallID:    "call-1",
					Name:      "execute",
					Arguments: `{"command":"rm -rf /"}`,
				}},
			},
		}, nil
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "done"}},
		},
	}, nil
}

func (m *scriptedToolCallModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

// memCheckpointStore is an in-memory adk.CheckPointStore for tests.
type memCheckpointStore struct {
	data map[string][]byte
}

func newMemCheckpointStore() *memCheckpointStore {
	return &memCheckpointStore{data: map[string][]byte{}}
}

func (m *memCheckpointStore) Get(_ context.Context, id string) ([]byte, bool, error) {
	cp, ok := m.data[id]
	return cp, ok, nil
}

func (m *memCheckpointStore) Set(_ context.Context, id string, cp []byte) error {
	m.data[id] = cp
	return nil
}

var _ = newMemCheckpointStore // retained for ad-hoc experiments

func drainEvents(t *testing.T, iter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) []*adk.TypedAgentEvent[*schema.AgenticMessage] {
	t.Helper()
	var out []*adk.TypedAgentEvent[*schema.AgenticMessage]
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event != nil && event.Err != nil {
			t.Logf("event error: %v", event.Err)
		}
		out = append(out, event)
	}
	return out
}

func findInterruptEvent(events []*adk.TypedAgentEvent[*schema.AgenticMessage]) *adk.InterruptInfo {
	for _, e := range events {
		if e != nil && e.Action != nil && e.Action.Interrupted != nil {
			return e.Action.Interrupted
		}
	}
	return nil
}

func findInterrupted(events []*adk.TypedAgentEvent[*schema.AgenticMessage]) *adk.InterruptInfo {
	return findInterruptEvent(events)
}

func TestSpike_ShellInterruptSurfacesThroughMiddleware(t *testing.T) {
	ctx := context.Background()

	shell := &interruptingShell{}
	fsMW, err := fsmw.NewTyped[*schema.AgenticMessage](ctx, &fsmw.MiddlewareConfig{Shell: shell})
	if err != nil {
		t.Fatalf("filesystem middleware: %v", err)
	}

	agenticModel, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        "spike",
		Instruction: "spike",
		Model:       &scriptedToolCallModel{},
		Handlers:    []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{fsMW},
		ToolsConfig: adk.ToolsConfig{},
	})
	if err != nil {
		t.Fatalf("compose agent: %v", err)
	}

	// Production wiring: the ADK session adapter backs both checkpoint and
	// session stores, so the interrupt checkpoint is durably persisted.
	fakeStore := fake.New()
	sessionAdapter := NewADKSessionAdapter(
		fakeStore.SessionEvents(), fakeStore.SessionCheckpoints(), "ws-spike")
	runner := adk.NewTypedRunner[*schema.AgenticMessage](adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           agenticModel,
		CheckPointStore: sessionAdapter,
		SessionID:       "spike-session",
		SessionStore:    sessionAdapter,
	})

	iter := runner.Query(ctx, "run the command")
	events := drainEvents(t, iter)

	interrupted := findInterrupted(events)
	if interrupted == nil {
		t.Fatalf("expected an interrupt event, got %d events", len(events))
	}
	if len(interrupted.InterruptContexts) == 0 || interrupted.InterruptContexts[0].ID == "" {
		t.Fatalf("expected interrupt context with ID, got %+v", interrupted.InterruptContexts)
	}
	if interrupted.CheckPointID == "" {
		t.Fatal("expected non-empty CheckPointID for persistence")
	}
	if shell.executions != 0 {
		t.Fatalf("shell must not execute before approval, got %d executions", shell.executions)
	}

	// Resume targeting the interrupt with approval=true: the tool re-runs with
	// the decision as resume data, the agent completes the turn.
	interruptID := interrupted.InterruptContexts[0].ID
	resumeIter, err := runner.ResumeWithParams(ctx, interrupted.CheckPointID, &adk.ResumeParams{
		Targets: map[string]any{interruptID: true},
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	resumeEvents := drainEvents(t, resumeIter)
	if again := findInterrupted(resumeEvents); again != nil {
		t.Fatalf("unexpected second interrupt after approval: %+v", again)
	}
	if shell.executions != 1 {
		t.Fatalf("expected 1 execution after approval, got %d", shell.executions)
	}
}
