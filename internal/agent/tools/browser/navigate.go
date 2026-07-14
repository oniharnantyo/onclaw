package browser

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func init() {
	tools.Register(&navigateTool{})
}

type navigateTool struct{}

func (t *navigateTool) Name() string {
	return "browser_navigate"
}

func (t *navigateTool) Desc() string {
	return "Navigate the active page to a given URL"
}

func (t *navigateTool) Category() string {
	return "Browser"
}

type NavigateInput struct {
	URL string `json:"url" jsonschema_description:"The HTTP or HTTPS URL to navigate to"`
}

func (t *navigateTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *NavigateInput) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		page, err := Mgr.GetActivePage()
		if err != nil {
			return fmt.Sprintf("%s could not complete: %s", "browser_navigate", err.Error()), nil
		}
		err = page.Navigate(ctx, input.URL)
		if err != nil {
			return fmt.Sprintf("%s could not complete: %s", "browser_navigate", err.Error()), nil
		}
		u, _ := page.URL(ctx)
		title, _ := page.Title(ctx)
		return fmt.Sprintf("Successfully navigated to %s. Page Title: %s", u, title), nil
	})
	if err != nil {
		panic(err)
	}
	return inv
}
