package agents

import (
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlantaskCompose_RegisteredAndRenamed verifies that when plantask is composed,
// its tools are registered in the agent's tool set and exposed to the model using
// snake_case names: task_create, task_get, task_update, task_list.
func TestPlantaskCompose_RegisteredAndRenamed(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	callCount := 0
	var offeredTools []string
	var toolResultContent string

	fakeMdl := &fakeModel{
		reply: func(input []*schema.Message) fakeReply {
			callCount++
			if callCount == 1 {
				// Propose calling task_create using snake_case tool name
				return toolCallsReply("call-pt-1", "task_create", `{"subject":"Initial Task","description":"Details here"}`)
			}
			for _, msg := range input {
				if msg.Role == schema.Tool && msg.ToolCallID == "call-pt-1" {
					toolResultContent = msg.Content
				}
			}
			return fakeReply{text: "Task successfully processed"}
		},
	}

	cfg := &composeConfig{
		Name:        "plantask-agent",
		Instruction: "You manage tasks.",
		ChatModel:   fakeMdl,
		Plantask: &plantaskCapability{
			AgentDir: tmpDir,
		},
	}

	agent, toolErrors, err := composeTurnAgent(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, agent)
	require.NotNil(t, toolErrors)

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	})

	iter := runner.Run(ctx, []*schema.Message{schema.UserMessage("please create a task")})
	err = drainRunnerIterator(iter)
	require.NoError(t, err)

	require.Equal(t, 2, callCount)
	offeredTools = fakeMdl.toolsOf(0)

	// All 4 tools must be offered in snake_case
	assert.Contains(t, offeredTools, "task_create")
	assert.Contains(t, offeredTools, "task_get")
	assert.Contains(t, offeredTools, "task_update")
	assert.Contains(t, offeredTools, "task_list")

	// Upstream PascalCase names must NOT be offered to the model
	assert.NotContains(t, offeredTools, "TaskCreate")
	assert.NotContains(t, offeredTools, "TaskGet")
	assert.NotContains(t, offeredTools, "TaskUpdate")
	assert.NotContains(t, offeredTools, "TaskList")

	// Verify task_create execution result and on-disk task persistence
	assert.Contains(t, toolResultContent, "Task #1")
	assert.FileExists(t, filepath.Join(tmpDir, ".tasks", "1.json"))
	assert.FileExists(t, filepath.Join(tmpDir, ".tasks", ".highwatermark"))
}

// TestPlantaskCompose_DisabledToolsWithheld_SnakeCase verifies that tools specified
// in DisabledTools using snake_case are withheld from the agent's offered tools.
func TestPlantaskCompose_DisabledToolsWithheld_SnakeCase(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	fakeMdl := &fakeModel{
		reply: func(input []*schema.Message) fakeReply {
			return fakeReply{text: "Done"}
		},
	}

	cfg := &composeConfig{
		Name:        "plantask-gated-agent",
		Instruction: "You manage tasks.",
		ChatModel:   fakeMdl,
		Plantask: &plantaskCapability{
			AgentDir:      tmpDir,
			DisabledTools: []string{"task_get", "task_update"},
		},
	}

	agent, _, err := composeTurnAgent(ctx, cfg)
	require.NoError(t, err)

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	})

	iter := runner.Run(ctx, []*schema.Message{schema.UserMessage("test gating")})
	err = drainRunnerIterator(iter)
	require.NoError(t, err)

	offered := fakeMdl.toolsOf(0)
	assert.Contains(t, offered, "task_create")
	assert.Contains(t, offered, "task_list")
	assert.NotContains(t, offered, "task_get")
	assert.NotContains(t, offered, "task_update")
}

// TestPlantaskCompose_DisabledToolsWithheld_PascalCase verifies that tools specified
// in DisabledTools using PascalCase are also recognized and withheld.
func TestPlantaskCompose_DisabledToolsWithheld_PascalCase(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	fakeMdl := &fakeModel{
		reply: func(input []*schema.Message) fakeReply {
			return fakeReply{text: "Done"}
		},
	}

	cfg := &composeConfig{
		Name:        "plantask-gated-agent",
		Instruction: "You manage tasks.",
		ChatModel:   fakeMdl,
		Plantask: &plantaskCapability{
			AgentDir:      tmpDir,
			DisabledTools: []string{"TaskCreate", "TaskList"},
		},
	}

	agent, _, err := composeTurnAgent(ctx, cfg)
	require.NoError(t, err)

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	})

	iter := runner.Run(ctx, []*schema.Message{schema.UserMessage("test gating")})
	err = drainRunnerIterator(iter)
	require.NoError(t, err)

	offered := fakeMdl.toolsOf(0)
	assert.NotContains(t, offered, "task_create")
	assert.NotContains(t, offered, "task_list")
	assert.Contains(t, offered, "task_get")
	assert.Contains(t, offered, "task_update")
}

// TestPlantaskCompose_DisabledPlantaskTools_Config verifies that DisabledPlantaskTools
// at the composeConfig root level successfully withholds the specified tool.
func TestPlantaskCompose_DisabledPlantaskTools_Config(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	fakeMdl := &fakeModel{
		reply: func(input []*schema.Message) fakeReply {
			return fakeReply{text: "Done"}
		},
	}

	cfg := &composeConfig{
		Name:                  "plantask-agent",
		Instruction:           "Instruction",
		ChatModel:             fakeMdl,
		DisabledPlantaskTools: []string{"task_create"},
		Plantask: &plantaskCapability{
			AgentDir: tmpDir,
		},
	}

	agent, _, err := composeTurnAgent(ctx, cfg)
	require.NoError(t, err)

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	})

	iter := runner.Run(ctx, []*schema.Message{schema.UserMessage("hi")})
	err = drainRunnerIterator(iter)
	require.NoError(t, err)

	offered := fakeMdl.toolsOf(0)
	assert.NotContains(t, offered, "task_create")
	assert.Contains(t, offered, "task_get")
	assert.Contains(t, offered, "task_update")
	assert.Contains(t, offered, "task_list")
}

// TestPlantaskCompose_WorkspaceInferredFromFilesystem verifies that if
// Plantask.AgentDir is empty, it inherits from Filesystem.AgentDir.
func TestPlantaskCompose_WorkspaceInferredFromFilesystem(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	fakeMdl := &fakeModel{
		reply: func(input []*schema.Message) fakeReply {
			return fakeReply{text: "Done"}
		},
	}

	cfg := &composeConfig{
		Name:        "plantask-agent",
		Instruction: "Instruction",
		ChatModel:   fakeMdl,
		Filesystem: &filesystemCapability{
			AgentDir: tmpDir,
		},
		Plantask: &plantaskCapability{},
	}

	agent, _, err := composeTurnAgent(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, agent)

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	})

	iter := runner.Run(ctx, []*schema.Message{schema.UserMessage("hi")})
	err = drainRunnerIterator(iter)
	require.NoError(t, err)

	offered := fakeMdl.toolsOf(0)
	assert.Contains(t, offered, "task_create")
}

// TestPlantaskCompose_MissingWorkspaceErrors verifies that composeTurnAgent fails
// if Plantask is enabled but no workspace directory is provided.
func TestPlantaskCompose_MissingWorkspaceErrors(t *testing.T) {
	ctx := t.Context()

	fakeMdl := &fakeModel{
		reply: func(input []*schema.Message) fakeReply {
			return fakeReply{text: "Done"}
		},
	}

	cfg := &composeConfig{
		Name:        "plantask-agent",
		Instruction: "Instruction",
		ChatModel:   fakeMdl,
		Plantask: &plantaskCapability{
			AgentDir: "",
		},
	}

	agent, _, err := composeTurnAgent(ctx, cfg)
	require.Error(t, err)
	require.Nil(t, agent)
	assert.Contains(t, err.Error(), "workspace is not rooted")
}
