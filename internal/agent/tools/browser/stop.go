package browser

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func init() {
	tools.Register(&stopTool{})
}

type stopTool struct{}

func (t *stopTool) Name() string {
	return "browser_stop"
}

func (t *stopTool) Desc() string {
	return "Stop the browser engine and close all active sessions and tabs"
}

func (t *stopTool) Category() string {
	return "Browser"
}

type StopInput struct{}

func (t *stopTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *StopInput) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		err := Mgr.Stop(ctx)
		if err != nil {
			return fmt.Sprintf("%s could not complete: %s", "browser_stop", err.Error()), nil
		}
		return "Browser engine stopped successfully", nil
	})
	if err != nil {
		panic(err)
	}
	return inv
}
