package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	sysbrowser "github.com/oniharnantyo/onclaw/internal/browser"
)

func init() {
	tools.Register(&actTool{})
}

type actTool struct{}

func (t *actTool) Name() string {
	return "browser_act"
}

func (t *actTool) Desc() string {
	return "Interact with elements on the active page (click, type, press key, hover, wait, evaluate js)"
}

func (t *actTool) Category() string {
	return "Browser"
}

type ActInput struct {
	Kind  string `json:"kind" jsonschema_description:"The interaction kind: click, type, press, hover, wait, evaluate"`
	Ref   string `json:"ref,omitempty" jsonschema_description:"The element reference (e.g. e1) returned from a prior browser_snapshot"`
	Text  string `json:"text,omitempty" jsonschema_description:"The text to type or the key name (e.g. Enter, Tab) to press"`
	Code  string `json:"code,omitempty" jsonschema_description:"The custom JavaScript code to evaluate"`
	Delay int    `json:"delay,omitempty" jsonschema_description:"Wait delay in milliseconds (for wait kind)"`
}

func (t *actTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *ActInput) (string, error) {
		page, err := Mgr.GetActivePage()
		if err != nil {
			return "", err
		}

		req := sysbrowser.ActRequest{
			Kind:  input.Kind,
			Ref:   input.Ref,
			Text:  input.Text,
			Code:  input.Code,
			Delay: time.Duration(input.Delay) * time.Millisecond,
		}

		err = page.Act(ctx, req)
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("Successfully executed action %q on page", input.Kind), nil
	})
	if err != nil {
		panic(err)
	}
	return inv
}
