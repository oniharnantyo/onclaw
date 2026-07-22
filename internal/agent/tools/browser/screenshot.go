package browser

import (
	"context"
	"fmt"
	"net/url"

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
	return "Capture a PNG screenshot of the active page and save it to a session file, returning a path reference"
}

func (t *screenshotTool) Category() string {
	return "Browser"
}

type ScreenshotInput struct {
	FullPage bool `json:"fullPage,omitempty" jsonschema_description:"Whether to capture the entire height of the page"`
}

func (t *screenshotTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *ScreenshotInput) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		page, err := Mgr.GetActivePage()
		if err != nil {
			return fmt.Sprintf("%s could not complete: %s", "browser_screenshot", err.Error()), nil
		}

		opts := sysbrowser.ShotOpts{
			FullPage: input.FullPage,
		}

		data, err := page.Screenshot(ctx, opts)
		if err != nil {
			return fmt.Sprintf("%s could not complete: %s", "browser_screenshot", err.Error()), nil
		}

		title := "page"
		if rawURL, urlErr := page.URL(ctx); urlErr == nil && rawURL != "" {
			if u, parseErr := url.Parse(rawURL); parseErr == nil && u.Host != "" {
				title = tools.Slugify(u.Host + u.Path)
			} else {
				title = tools.Slugify(rawURL)
			}
		} else if rawTitle, titleErr := page.Title(ctx); titleErr == nil && rawTitle != "" {
			title = tools.Slugify(rawTitle)
		}

		rel, wErr := tools.WriteSpillFile(scope, t.Name(), title, ".png", data)
		if wErr != nil {
			return fmt.Sprintf("%s could not complete: %s", "browser_screenshot", wErr.Error()), nil
		}

		return fmt.Sprintf("Screenshot saved to a session file.\nPath: %s\nUse the read_file tool to view it.", rel), nil
	})
	if err != nil {
		panic(err)
	}
	return inv
}
