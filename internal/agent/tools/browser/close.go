package browser

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func init() {
	tools.Register(&closeTool{})
}

type closeTool struct{}

func (t *closeTool) Name() string {
	return "browser_close"
}

func (t *closeTool) Desc() string {
	return "Close the active tab/page"
}

func (t *closeTool) Category() string {
	return "Browser"
}

type CloseInput struct{}

func (t *closeTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *CloseInput) (string, error) {
		err := Mgr.ClosePage(ctx)
		if err != nil {
			return "", err
		}
		return "Closed active tab successfully", nil
	})
	if err != nil {
		panic(err)
	}
	return inv
}
