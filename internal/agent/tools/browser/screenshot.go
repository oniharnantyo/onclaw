package browser

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	sysbrowser "github.com/oniharnantyo/onclaw/internal/browser"
)

func init() {
	tools.Register(&screenshotTool{})
}

type screenshotTool struct{}

func (t *screenshotTool) Name() string {
	return "browser_screenshot"
}

func (t *screenshotTool) Desc() string {
	return "Capture a PNG screenshot of the active page and return it base64 encoded"
}

func (t *screenshotTool) Category() string {
	return "Browser"
}

type ScreenshotInput struct {
	FullPage bool `json:"fullPage,omitempty" jsonschema_description:"Whether to capture the entire height of the page"`
}

func (t *screenshotTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *ScreenshotInput) (string, error) {
		page, err := Mgr.GetActivePage()
		if err != nil {
			return "", err
		}

		opts := sysbrowser.ShotOpts{
			FullPage: input.FullPage,
		}

		data, err := page.Screenshot(ctx, opts)
		if err != nil {
			return "", err
		}

		encoded := base64.StdEncoding.EncodeToString(data)
		return fmt.Sprintf("data:image/png;base64,%s", encoded), nil
	})
	if err != nil {
		panic(err)
	}
	return inv
}
