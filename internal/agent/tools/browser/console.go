package browser

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func init() {
	tools.Register(&consoleTool{})
}

type consoleTool struct{}

func (t *consoleTool) Name() string {
	return "browser_console"
}

func (t *consoleTool) Desc() string {
	return "Retrieve console log messages emitted by the active page"
}

func (t *consoleTool) Category() string {
	return "Browser"
}

type ConsoleInput struct{}

func (t *consoleTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *ConsoleInput) (string, error) {
		page, err := Mgr.GetActivePage()
		if err != nil {
			return "", err
		}

		msgs, err := page.ConsoleMessages(ctx)
		if err != nil {
			return "", err
		}

		bytes, err := json.Marshal(msgs)
		if err != nil {
			return "", err
		}
		return string(bytes), nil
	})
	if err != nil {
		panic(err)
	}
	return inv
}
