package browser

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func init() {
	tools.Register(&statusTool{})
}

type statusTool struct{}

func (t *statusTool) Name() string {
	return "browser_status"
}

func (t *statusTool) Desc() string {
	return "Get the current status of the browser engine (is started, number of tabs, active tab details)"
}

func (t *statusTool) Category() string {
	return "Browser"
}

type StatusInput struct{}

func (t *statusTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *StatusInput) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		started := Mgr.IsStarted()
		if !started {
			return "Browser engine is not running", nil
		}

		list, err := Mgr.ListPages(ctx)
		if err != nil {
			return fmt.Sprintf("%s could not complete: %s", "browser_status", err.Error()), nil
		}

		activeTitle := "None"
		activeURL := "None"
		for _, info := range list {
			if info.Active {
				activeTitle = info.Title
				activeURL = info.URL
				break
			}
		}

		return fmt.Sprintf("Browser engine is running. Open tabs: %d. Active tab: %q (%s)", len(list), activeTitle, activeURL), nil
	})
	if err != nil {
		panic(err)
	}
	return inv
}
