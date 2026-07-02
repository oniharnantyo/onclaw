package browser

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	sysbrowser "github.com/oniharnantyo/onclaw/internal/browser"
)

func init() {
	tools.Register(&snapshotTool{})
}

type snapshotTool struct{}

func (t *snapshotTool) Name() string {
	return "browser_snapshot"
}

func (t *snapshotTool) Desc() string {
	return "Get the accessibility tree and visible body text of the active page"
}

func (t *snapshotTool) Category() string {
	return "Browser"
}

type SnapshotInput struct{}

func (t *snapshotTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *SnapshotInput) (string, error) {
		page, err := Mgr.GetActivePage()
		if err != nil {
			return "", err
		}
		snap, err := page.Snapshot(ctx, sysbrowser.SnapshotOpts{})
		if err != nil {
			return "", err
		}

		res := fmt.Sprintf("--- ACCESSIBILITY TREE ---\n%s\n\n--- PAGE TEXT ---\n%s", snap.AXTree, snap.Text)
		return res, nil
	})
	if err != nil {
		panic(err)
	}
	return inv
}
