package cli

import (
	"context"
	"testing"
)

func TestMockStores(t *testing.T) {
	ctx := context.Background()

	mhs := &mockHookStore{}
	_ = mhs.AddHook(ctx, nil)
	_, _ = mhs.GetHook(ctx, "")
	_, _ = mhs.ListHooks(ctx)
	_, _ = mhs.ListHooksByScopeAndEvent(ctx, "", "")
	_ = mhs.UpdateHook(ctx, nil)
	_ = mhs.RemoveHook(ctx, "")
	_ = mhs.ToggleHook(ctx, "", false)

	mes := &mockExecStore{}
	_ = mes.AppendExecution(ctx, nil)
	_, _ = mes.ListExecutions(ctx)
}
