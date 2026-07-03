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
	Register(&editFileTool{})
}

type editFileTool struct{}

func (e *editFileTool) Name() string {
	return "edit_file"
}

func (e *editFileTool) Desc() string {
	return "Replace a unique, contiguous block of text in an existing file inside the workspace"
}

func (e *editFileTool) Category() string {
	return "Filesystem"
}

type EditFileInput struct {
	Path      string `json:"path" jsonschema_description:"The file path to edit, relative to the workspace"`
	OldString string `json:"old_string" jsonschema_description:"The exact block of text to be replaced (must be unique)"`
	NewString string `json:"new_string" jsonschema_description:"The replacement text"`
}

func (e *editFileTool) Build(scope *Scope) tool.InvokableTool {
	t, err := utils.InferTool(e.Name(), e.Desc(), func(ctx context.Context, input *EditFileInput) (string, error) {
		absPath, err := ValidatePath(scope.Workspace, input.Path)
		if err != nil {
			return "", err
		}

		contentBytes, err := os.ReadFile(absPath)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}
		content := string(contentBytes)

		count := strings.Count(content, input.OldString)
		if count == 0 {
			return "", fmt.Errorf("the old_string was not found in the file")
		}
		if count > 1 {
			return "", fmt.Errorf("the old_string matches %d occurrences in the file, but it must be unique", count)
		}

		newContent := strings.Replace(content, input.OldString, input.NewString, 1)

		if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
			return "", fmt.Errorf("failed to write file: %w", err)
		}

		return "File edited successfully.", nil
	})
	if err != nil {
		panic(err)
	}
	return t
}
