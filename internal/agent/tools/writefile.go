package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

func init() {
	Register(&writeFileTool{})
}

type writeFileTool struct{}

func (w *writeFileTool) Name() string {
	return "write_file"
}

func (w *writeFileTool) Desc() string {
	return "Write contents to a file in the workspace (creates parent directories if needed)"
}

func (w *writeFileTool) Category() string {
	return "Filesystem"
}

type WriteFileInput struct {
	Path    string `json:"path" jsonschema_description:"The file path to write, relative to the workspace"`
	Content string `json:"content" jsonschema_description:"The content to write to the file"`
}

func (w *writeFileTool) Build(scope *Scope) tool.InvokableTool {
	t, err := utils.InferTool(w.Name(), w.Desc(), func(ctx context.Context, input *WriteFileInput) (string, error) {
		absPath, err := ValidatePath(scope.Workspace, input.Path)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return "", fmt.Errorf("failed to create directories: %w", err)
		}
		if err := os.WriteFile(absPath, []byte(input.Content), 0644); err != nil {
			return "", fmt.Errorf("failed to write file: %w", err)
		}
		return "File written successfully.", nil
	})
	if err != nil {
		panic(err)
	}
	return t
}
