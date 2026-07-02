package browser

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func init() {
	tools.Register(&openTool{})
}

type openTool struct{}

func (t *openTool) Name() string {
	return "browser_open"
}

func (t *openTool) Desc() string {
	return "Open a new tab in the browser context and set it as active"
}

func (t *openTool) Category() string {
	return "Browser"
}

type OpenInput struct{}

func (t *openTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *OpenInput) (string, error) {
		page, err := Mgr.OpenPage(ctx, scope.Workspace, scope.ToolGroupCfg, scope.KVStore)
		if err != nil {
			return "", err
		}
		url, _ := page.URL(ctx)
		title, _ := page.Title(ctx)
		return fmt.Sprintf("Opened a new tab. Active page: %s (%s)", title, url), nil
	})
	if err != nil {
		panic(err)
	}
	return inv
}
