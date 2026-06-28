package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/store"
)

type fakeChatModel struct {
	generateFunc func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error)
	streamFunc   func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error)
	boundTools   []*schema.ToolInfo
}

func (f *fakeChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if f.generateFunc != nil {
		return f.generateFunc(ctx, input, opts...)
	}
	return &schema.Message{Role: schema.Assistant, Content: "Default fake response"}, nil
}

func (f *fakeChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if f.streamFunc != nil {
		return f.streamFunc(ctx, input, opts...)
	}
	msg := &schema.Message{Role: schema.Assistant, Content: "Default fake streaming response"}
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func (f *fakeChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	f.boundTools = tools
	return f, nil
}

func TestAssembleAndRunAgent_ReActLoop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-agent-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	userConfigDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(userConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Create a dummy file to read in workspace
	testFile := filepath.Join(workspace, "README.md")
	if err := os.WriteFile(testFile, []byte("Hello onclaw!"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Setup fake ChatModel to simulate a tool-calling loop
	modelCalls := 0
	respondMock := func(input []*schema.Message) (*schema.Message, error) {
		modelCalls++
		if modelCalls == 1 {
			// First call: trigger read_file tool call
			return &schema.Message{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: schema.FunctionCall{
							Name:      "read_file",
							Arguments: `{"path":"README.md"}`,
						},
					},
				},
			}, nil
		}
		// Second call: return final text answer
		return &schema.Message{
			Role:    schema.Assistant,
			Content: "Successfully read README.md. Content: Hello onclaw!",
		}, nil
	}

	fm := &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			return respondMock(input)
		},
		streamFunc: func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
			msg, err := respondMock(input)
			if err != nil {
				return nil, err
			}
			sr, sw := schema.Pipe[*schema.Message](1)
			sw.Send(msg, nil)
			sw.Close()
			return sr, nil
		},
	}

	agentConf := &store.Agent{
		Name:          "test-react-agent",
		Provider:      "fake-prov",
		Tools:         "read_file,write_file", // test subset filtering
		MaxIterations: 5,
	}

	ctx := context.Background()
	agent, err := AssembleAgent(ctx, agentConf, fm, workspace, userConfigDir, "deny", nil, 64000)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}

	var stdout bytes.Buffer
	transcriptPath := filepath.Join(userConfigDir, "transcript.jsonl")

	err = RunAgent(ctx, agent, "Read the README.md file please.", &stdout, transcriptPath)
	if err != nil {
		t.Fatalf("failed to run agent: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Calling tool \"read_file\"") {
		t.Errorf("stdout does not contain tool call info, got: %q", output)
	}
	if !strings.Contains(output, "Successfully read README.md. Content: Hello onclaw!") {
		t.Errorf("stdout does not contain final response, got: %q", output)
	}

	// Verify transcript entries
	entries, err := readTranscript(transcriptPath)
	if err != nil {
		t.Fatalf("failed to read transcript: %v", err)
	}

	expectedTypes := []string{"user", "tool_call", "tool_result", "assistant"}
	if len(entries) < len(expectedTypes) {
		t.Fatalf("expected at least %d transcript entries, got %d", len(expectedTypes), len(entries))
	}

	for i, expected := range expectedTypes {
		if entries[i].Type != expected {
			t.Errorf("expected entry %d to have type %q, got %q", i, expected, entries[i].Type)
		}
	}
}

func TestAssembleAndRunAgent_Cancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-cancel-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	userConfigDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(userConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	fm := &fakeChatModel{
		streamFunc: func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
			// Simulate long-running inference that respects cancellation
			sr, sw := schema.Pipe[*schema.Message](1)
			go func() {
				select {
				case <-ctx.Done():
					sw.Send(nil, ctx.Err())
				case <-time.After(1 * time.Second):
					sw.Send(&schema.Message{Role: schema.Assistant, Content: "Finished"}, nil)
				}
				sw.Close()
			}()
			return sr, nil
		},
	}

	agentConf := &store.Agent{
		Name: "test-cancel-agent",
	}

	ctx, cancel := context.WithCancel(context.Background())
	agent, err := AssembleAgent(ctx, agentConf, fm, workspace, userConfigDir, "deny", nil, 64000)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}

	transcriptPath := filepath.Join(userConfigDir, "transcript.jsonl")

	// Cancel context immediately
	cancel()

	var stdout bytes.Buffer
	err = RunAgent(ctx, agent, "Hello", &stdout, transcriptPath)
	if err == nil {
		t.Error("expected RunAgent to fail with cancellation error, got nil")
	}

	entries, err := readTranscript(transcriptPath)
	if err != nil {
		t.Fatalf("failed to read transcript: %v", err)
	}

	// Last entry should be "interrupted"
	if len(entries) == 0 {
		t.Fatal("expected at least one entry in transcript")
	}
	lastEntry := entries[len(entries)-1]
	if lastEntry.Type != "interrupted" {
		t.Errorf("expected last transcript entry type to be 'interrupted', got %q", lastEntry.Type)
	}
}

func TestAssembleAgent_ContextWindowTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(workspace, 0755)
	userConfigDir := filepath.Join(tmpDir, "config")
	_ = os.MkdirAll(userConfigDir, 0755)

	agentConf := &store.Agent{
		Name: "test-trigger-agent",
	}

	fm := &fakeChatModel{}
	ctx := context.Background()

	// 1. Compile and resolve with 128000 context window (verifies 80% logic runs)
	ag, err := AssembleAgent(ctx, agentConf, fm, workspace, userConfigDir, "deny", nil, 128000)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	if ag == nil {
		t.Fatal("expected non-nil agent")
	}

	// 2. Re-assemble with 64000 context window
	ag2, err := AssembleAgent(ctx, agentConf, fm, workspace, userConfigDir, "deny", nil, 64000)
	if err != nil {
		t.Fatalf("failed to assemble agent second time: %v", err)
	}
	if ag2 == nil {
		t.Fatal("expected non-nil agent on second assembly")
	}
}

func readTranscript(path string) ([]TranscriptEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []TranscriptEntry
	dec := json.NewDecoder(file)
	for {
		var entry TranscriptEntry
		if err := dec.Decode(&entry); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func TestSummarizationTrigger(t *testing.T) {
	tests := []struct {
		window   int
		expected int
	}{
		{128000, 102400},
		{64000, 51200},
		{0, 0},
	}
	for _, tc := range tests {
		got := summarizationTrigger(tc.window)
		if got != tc.expected {
			t.Errorf("summarizationTrigger(%d) = %d; want %d", tc.window, got, tc.expected)
		}
	}
}
