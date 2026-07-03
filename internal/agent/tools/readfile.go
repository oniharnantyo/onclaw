package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

func init() {
	Register(&readFileTool{})
}

type readFileTool struct{}

func (r *readFileTool) Name() string {
	return "read_file"
}

func (r *readFileTool) Desc() string {
	return "Read the contents of a file in the workspace"
}

func (r *readFileTool) Category() string {
	return "Filesystem"
}

type ReadFileInput struct {
	Path string `json:"path" jsonschema_description:"The file path to read, relative to the workspace"`
}

func (r *readFileTool) Build(scope *Scope) tool.InvokableTool {
	t, err := utils.InferTool(r.Name(), r.Desc(), func(ctx context.Context, input *ReadFileInput) (string, error) {
		absPath, err := ValidatePath(scope.Workspace, input.Path)
		if err != nil {
			return "", err
		}
		content, err := os.ReadFile(absPath)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}
		return string(content), nil
	})
	if err != nil {
		panic(err)
	}
	return t
}
