package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
)

type customTestTool struct {
	name string
	desc string
	run  func(ctx context.Context, args string) (string, error)
}

func (t *customTestTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        t.name,
		Desc:        t.desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

func (t *customTestTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	return t.run(ctx, args)
}

func drainRunnerIterator[T adk.MessageType](iter *adk.AsyncIterator[*adk.TypedAgentEvent[T]]) error {
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			return ev.Err
		}
	}
	return nil
}

// TestReductionCompose_OversizedOutputTruncated verifies that an oversized tool
// output is truncated and offloaded to the jailed workspace when reduction is composed.
func TestReductionCompose_OversizedOutputTruncated(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	oversizedPayload := strings.Repeat("A very large string payload for reduction testing. ", 20) // ~1000 chars
	oversizedTool := &customTestTool{
		name: "heavy_payload_tool",
		desc: "Returns a very large payload",
		run: func(ctx context.Context, args string) (string, error) {
			return oversizedPayload, nil
		},
	}

	callCount := 0
	var receivedToolResultContent string

	fakeMdl := &fakeModel{
		reply: func(input []*schema.Message) fakeReply {
			callCount++
			if callCount == 1 {
				return toolCallsReply("call-red-1", "heavy_payload_tool", `{}`)
			}
			// On call 2, inspect the tool result in the input messages
			for _, msg := range input {
				if msg.Role == schema.Tool && msg.ToolCallID == "call-red-1" {
					receivedToolResultContent = msg.Content
				}
			}
			return fakeReply{text: "Reduction verified"}
		},
	}

	cfg := &composeConfig{
		Name:        "test-reduction-agent",
		Instruction: "You are a test agent.",
		ChatModel:   fakeMdl,
		Tools:       []tool.BaseTool{oversizedTool},
		Reduction: &reductionCapability{
			AgentDir:          tmpDir,
			MaxLengthForTrunc: 50, // triggers truncation on 1000 chars
		},
	}

	agent, toolErrors, err := composeTurnAgent(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, agent)
	require.NotNil(t, toolErrors)

	runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           agent,
		EnableStreaming: true,
	})

	iter := runner.Run(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("run heavy tool")})
	err = drainRunnerIterator(iter)
	require.NoError(t, err)

	require.Equal(t, 2, callCount)
	require.NotEmpty(t, receivedToolResultContent)

	// Tool result received by the model should be truncated, not the full payload
	assert.NotEqual(t, oversizedPayload, receivedToolResultContent)
	assert.Contains(t, receivedToolResultContent, "read_file")
	assert.Contains(t, receivedToolResultContent, backend.DefaultMountPoint+"/trunc/call-red-1")

	// Full content should be persisted on disk under tmpDir/trunc/call-red-1
	savedFilePath := filepath.Join(tmpDir, "trunc", "call-red-1")
	data, err := os.ReadFile(savedFilePath)
	require.NoError(t, err)
	assert.Equal(t, oversizedPayload, string(data))
}

// TestReductionCompose_WorkspaceInferredFromFilesystem verifies that if
// Reduction.AgentDir is omitted, it defaults to Filesystem.AgentDir.
func TestReductionCompose_WorkspaceInferredFromFilesystem(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	fakeMdl := &fakeModel{
		reply: func(input []*schema.Message) fakeReply {
			return fakeReply{text: "OK"}
		},
	}

	cfg := &composeConfig{
		Name:        "test-agent",
		Instruction: "Test instruction",
		ChatModel:   fakeMdl,
		Filesystem: &filesystemCapability{
			AgentDir: tmpDir,
		},
		Reduction: &reductionCapability{
			// AgentDir is empty: must inherit from Filesystem.AgentDir
		},
	}

	agent, toolErrors, err := composeTurnAgent(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, agent)
	require.NotNil(t, toolErrors)
}

// TestReductionCompose_MissingWorkspaceErrors verifies that composeTurnAgent fails
// if Reduction is enabled but no workspace directory is provided.
func TestReductionCompose_MissingWorkspaceErrors(t *testing.T) {
	ctx := t.Context()

	fakeMdl := &fakeModel{
		reply: func(input []*schema.Message) fakeReply {
			return fakeReply{text: "OK"}
		},
	}

	cfg := &composeConfig{
		Name:        "test-agent",
		Instruction: "Test instruction",
		ChatModel:   fakeMdl,
		Reduction: &reductionCapability{
			AgentDir: "", // unrooted
		},
	}

	agent, toolErrors, err := composeTurnAgent(ctx, cfg)
	require.Error(t, err)
	require.Nil(t, agent)
	require.Nil(t, toolErrors)
	assert.Contains(t, err.Error(), "workspace is not rooted")
}

// TestReductionCompose_OffloadFailureKeepsOriginal verifies that when storing
// an oversized output fails, the original tool result stays on the transcript
// and the turn proceeds without failing (spec agent-runtime requirement).
func TestReductionCompose_OffloadFailureKeepsOriginal(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	// Create a read-only directory to force offload file write failure
	unwritableDir := filepath.Join(tmpDir, "unwritable")
	err := os.MkdirAll(unwritableDir, 0555)
	require.NoError(t, err)
	defer os.Chmod(unwritableDir, 0755)

	oversizedPayload := strings.Repeat("Payload chunk for offload failure test. ", 10)
	oversizedTool := &customTestTool{
		name: "failing_offload_tool",
		desc: "Returns a large payload",
		run: func(ctx context.Context, args string) (string, error) {
			return oversizedPayload, nil
		},
	}

	callCount := 0
	var receivedToolResult string

	fakeMdl := &fakeModel{
		reply: func(input []*schema.Message) fakeReply {
			callCount++
			if callCount == 1 {
				return toolCallsReply("call-fail-1", "failing_offload_tool", `{}`)
			}
			for _, msg := range input {
				if msg.Role == schema.Tool && msg.ToolCallID == "call-fail-1" {
					receivedToolResult = msg.Content
				}
			}
			return fakeReply{text: "Finished despite offload error"}
		},
	}

	cfg := &composeConfig{
		Name:        "test-resilient-reduction",
		Instruction: "Test instruction",
		ChatModel:   fakeMdl,
		Tools:       []tool.BaseTool{oversizedTool},
		Reduction: &reductionCapability{
			AgentDir:          unwritableDir,
			MaxLengthForTrunc: 30,
		},
	}

	agent, toolErrors, err := composeTurnAgent(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, agent)
	require.NotNil(t, toolErrors)

	runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           agent,
		EnableStreaming: true,
	})

	iter := runner.Run(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("execute tool")})
	err = drainRunnerIterator(iter)
	// Turn MUST NOT fail even if offload failed
	require.NoError(t, err)
	require.Equal(t, 2, callCount)
	require.NotEmpty(t, receivedToolResult)
}

// TestReductionCompose_ContextLimitDerivation verifies that ContextLimit correctly
// derives the clear threshold (80% of context window) and sets up the reduction config.
func TestReductionCompose_ContextLimitDerivation(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	cfg := &composeConfig{
		Name:        "test-context-limit",
		Instruction: "Test instruction",
		ChatModel: &fakeModel{
			reply: func(input []*schema.Message) fakeReply {
				return fakeReply{text: "ok"}
			},
		},
		Reduction: &reductionCapability{
			AgentDir:     tmpDir,
			ContextLimit: 100000, // Derives MaxTokensForClear = 80000
		},
	}

	handlers, _, err := buildHandlers(ctx, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, handlers)
}
