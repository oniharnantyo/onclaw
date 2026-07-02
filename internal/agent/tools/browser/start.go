package browser

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func init() {
	tools.Register(&startTool{})
}

type startTool struct{}

func (t *startTool) Name() string {
	return "browser_start"
}

func (t *startTool) Desc() string {
	return "Start the browser engine using the configured rendering backend"
}

func (t *startTool) Category() string {
	return "Browser"
}

type StartInput struct{}

func (t *startTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *StartInput) (string, error) {
		err := Mgr.Start(ctx, scope.Workspace, scope.ToolGroupCfg, scope.KVStore)
		if err != nil {
			return "", err
		}
		return "Browser engine started successfully", nil
	})
	if err != nil {
		panic(err)
	}
	return inv
}
