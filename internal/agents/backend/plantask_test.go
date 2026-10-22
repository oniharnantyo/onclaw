package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudwego/eino/adk"
	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/plantask"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestPlantask_BackendCRUD(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	b, err := NewPlantaskBackend(dir)
	require.NoError(t, err)

	// 1. Write
	filePath := "/workspace/test.txt"
	err = b.Write(ctx, &einofs.WriteRequest{
		FilePath: filePath,
		Content:  "hello plantask",
	})
	require.NoError(t, err)

	hostPath := filepath.Join(dir, "test.txt")
	content, err := os.ReadFile(hostPath)
	require.NoError(t, err)
	assert.Equal(t, "hello plantask", string(content))

	// 2. Read
	fileContent, err := b.Read(ctx, &einofs.ReadRequest{
		FilePath: filePath,
	})
	require.NoError(t, err)
	assert.Equal(t, "hello plantask", fileContent.Content)

	// 3. LsInfo
	infos, err := b.LsInfo(ctx, &einofs.LsInfoRequest{
		Path: "/workspace",
	})
	require.NoError(t, err)
	var found bool
	for _, info := range infos {
		if strings.HasSuffix(info.Path, "test.txt") {
			found = true
			assert.Equal(t, "/workspace/test.txt", info.Path)
			assert.Equal(t, int64(len("hello plantask")), info.Size)
		}
	}
	assert.True(t, found, "test.txt should be in LsInfo results")

	// 4. Delete
	err = b.Delete(ctx, &plantask.DeleteRequest{
		FilePath: filePath,
	})
	require.NoError(t, err)

	_, err = os.Stat(hostPath)
	assert.True(t, os.IsNotExist(err), "file should be deleted from host")

	// 5. Delete non-existent file
	err = b.Delete(ctx, &plantask.DeleteRequest{
		FilePath: filePath,
	})
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestPlantask_DeleteJailEscape(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "secret.txt")
	err := os.WriteFile(externalFile, []byte("super secret"), 0644)
	require.NoError(t, err)

	b, err := NewPlantaskBackend(dir)
	require.NoError(t, err)

	// 1. Path escape with ..
	err = b.Delete(ctx, &plantask.DeleteRequest{
		FilePath: "/workspace/../secret.txt",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "..")

	// 2. Absolute path on host outside jail
	err = b.Delete(ctx, &plantask.DeleteRequest{
		FilePath: externalFile,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute paths are not allowed")

	// 3. Workspace root deletion
	err = b.Delete(ctx, &plantask.DeleteRequest{
		FilePath: "/workspace",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot delete the workspace root")

	// 4. Symlink escape
	symlinkPath := filepath.Join(dir, "symlink_escape")
	err = os.Symlink(externalFile, symlinkPath)
	require.NoError(t, err)

	err = b.Delete(ctx, &plantask.DeleteRequest{
		FilePath: "/workspace/symlink_escape",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes jail")

	// Verify external file was not deleted
	_, err = os.Stat(externalFile)
	assert.NoError(t, err, "external file must remain untouched")
}

func TestPlantask_MiddlewareRoundtrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	baseDir := DefaultMountPoint + "/.tasks"

	backend, err := NewPlantaskBackend(dir)
	require.NoError(t, err)

	// Turn 1: create tasks and update dependencies
	mw, err := plantask.New(ctx, &plantask.Config{
		Backend: backend,
		BaseDir: baseDir,
	})
	require.NoError(t, err)

	runCtx := &adk.ChatModelAgentContext[*schema.Message]{}
	_, newRunCtx, err := mw.BeforeAgent(ctx, runCtx)
	require.NoError(t, err)
	require.Len(t, newRunCtx.Tools, 4)

	var createTool, updateTool, listTool, getTool tool.InvokableTool
	for _, toolItem := range newRunCtx.Tools {
		info, infoErr := toolItem.Info(ctx)
		require.NoError(t, infoErr)
		inv, ok := toolItem.(tool.InvokableTool)
		require.True(t, ok)
		switch info.Name {
		case "TaskCreate":
			createTool = inv
		case "TaskUpdate":
			updateTool = inv
		case "TaskList":
			listTool = inv
		case "TaskGet":
			getTool = inv
		}
	}
	require.NotNil(t, createTool)
	require.NotNil(t, updateTool)
	require.NotNil(t, listTool)
	require.NotNil(t, getTool)

	// 1. Create Task #1
	res, err := createTool.InvokableRun(ctx, `{"subject":"Task 1","description":"Initial task"}`)
	require.NoError(t, err)
	assert.Contains(t, res, "Task #1")

	// 2. Create Task #2
	res, err = createTool.InvokableRun(ctx, `{"subject":"Task 2","description":"Follow-up task"}`)
	require.NoError(t, err)
	assert.Contains(t, res, "Task #2")

	// Verify files created on host under .tasks/
	task1Path := filepath.Join(dir, ".tasks", "1.json")
	require.FileExists(t, task1Path)
	task2Path := filepath.Join(dir, ".tasks", "2.json")
	require.FileExists(t, task2Path)
	highwatermarkPath := filepath.Join(dir, ".tasks", ".highwatermark")
	require.FileExists(t, highwatermarkPath)

	// 3. Update Task #2 to be blocked by Task #1
	_, err = updateTool.InvokableRun(ctx, `{"taskId":"2","addBlockedBy":["1"]}`)
	require.NoError(t, err)

	// 4. List tasks in Turn 1
	listRes, err := listTool.InvokableRun(ctx, `{}`)
	require.NoError(t, err)
	assert.Contains(t, listRes, "#1 [pending] Task 1")
	assert.Contains(t, listRes, "#2 [pending] Task 2")
	assert.Contains(t, listRes, "[blocked by #1]")

	// 5. Update Task #1 to in_progress
	_, err = updateTool.InvokableRun(ctx, `{"taskId":"1","status":"in_progress"}`)
	require.NoError(t, err)

	getRes, err := getTool.InvokableRun(ctx, `{"taskId":"1"}`)
	require.NoError(t, err)
	assert.Contains(t, getRes, "Status: in_progress")

	// 6. Complete Task #1
	_, err = updateTool.InvokableRun(ctx, `{"taskId":"1","status":"completed"}`)
	require.NoError(t, err)

	// Turn 2: Simulate next turn with a fresh plantask middleware instance
	// verifying that tasks persisted across turns in the workspace directory.
	backend2, err := NewPlantaskBackend(dir)
	require.NoError(t, err)

	mw2, err := plantask.New(ctx, &plantask.Config{
		Backend: backend2,
		BaseDir: baseDir,
	})
	require.NoError(t, err)

	_, newRunCtx2, err := mw2.BeforeAgent(ctx, &adk.ChatModelAgentContext[*schema.Message]{})
	require.NoError(t, err)

	var listTool2 tool.InvokableTool
	for _, toolItem := range newRunCtx2.Tools {
		info, infoErr := toolItem.Info(ctx)
		require.NoError(t, infoErr)
		if info.Name == "TaskList" {
			listTool2 = toolItem.(tool.InvokableTool)
		}
	}
	require.NotNil(t, listTool2)

	listRes2, err := listTool2.InvokableRun(ctx, `{}`)
	require.NoError(t, err)
	assert.Contains(t, listRes2, "#1 [completed] Task 1")
	assert.Contains(t, listRes2, "#2 [pending] Task 2")
}
