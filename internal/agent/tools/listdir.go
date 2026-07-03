package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

func init() {
	Register(&listDirTool{})
}

type listDirTool struct{}

func (l *listDirTool) Name() string {
	return "list_dir"
}

func (l *listDirTool) Desc() string {
	return "List files and directories inside a workspace directory"
}

func (l *listDirTool) Category() string {
	return "Filesystem"
}

type ListDirInput struct {
	Path string `json:"path" jsonschema_description:"The directory path to list, relative to the workspace"`
}

func (l *listDirTool) Build(scope *Scope) tool.InvokableTool {
	t, err := utils.InferTool(l.Name(), l.Desc(), func(ctx context.Context, input *ListDirInput) (string, error) {
		absPath, err := ValidatePath(scope.Workspace, input.Path)
		if err != nil {
			return "", err
		}
		entries, err := os.ReadDir(absPath)
		if err != nil {
			return "", fmt.Errorf("failed to list directory: %w", err)
		}
		var sb strings.Builder
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			typeStr := "file"
			if entry.IsDir() {
				typeStr = "dir"
			}
			sb.WriteString(fmt.Sprintf("%s (%s, %d bytes)\n", entry.Name(), typeStr, info.Size()))
		}
		return sb.String(), nil
	})
	if err != nil {
		panic(err)
	}
	return t
}
