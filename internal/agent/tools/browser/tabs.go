package browser

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func init() {
	tools.Register(&tabsTool{})
}

type tabsTool struct{}

func (t *tabsTool) Name() string {
	return "browser_tabs"
}

func (t *tabsTool) Desc() string {
	return "List all open browser tabs and which one is active, or activate a specific tab"
}

func (t *tabsTool) Category() string {
	return "Browser"
}

type TabsInput struct {
	ActivateIndex *int `json:"activateIndex,omitempty" jsonschema_description:"Index of the tab to set as active"`
}

func (t *tabsTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *TabsInput) (string, error) {
		if input.ActivateIndex != nil {
			err := Mgr.SetActivePage(*input.ActivateIndex)
			if err != nil {
				return "", err
			}
		}

		list, err := Mgr.ListPages(ctx)
		if err != nil {
			return "", err
		}

		bytes, err := json.Marshal(list)
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
