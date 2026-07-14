package middlewares_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
)

func TestFloorSafetyLimit(t *testing.T) {
	if got := middlewares.FloorSafetyLimit(7400); got != 3700 {
		t.Errorf("FloorSafetyLimit(7400) = %d, want 3700", got)
	}
	if got := middlewares.FloorSafetyLimit(64000); got != 32000 {
		t.Errorf("FloorSafetyLimit(64000) = %d, want 32000", got)
	}
}

func newSafetyState(toolInfos []*schema.ToolInfo) *adk.TypedChatModelAgentState[*schema.AgenticMessage] {
	return &adk.TypedChatModelAgentState[*schema.AgenticMessage]{ToolInfos: toolInfos}
}

func TestInputSafetyMiddleware_WithinLimit(t *testing.T) {
	// contextWindow 64000 -> limit 32000; a single small tool stays well under.
	mw := middlewares.NewInputSafetyMiddleware(10, 64000)
	state := newSafetyState([]*schema.ToolInfo{
		{Name: "read_file", Desc: "Reads a file from the workspace."},
	})
	_, got, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("expected no error within limit: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil state")
	}
}

func TestInputSafetyMiddleware_ExceedsLimit(t *testing.T) {
	// contextWindow 7400 -> limit 3700. A tool whose schema alone exceeds it
	// triggers the guard (mirrors the 5000/7400 repro at the safety boundary).
	big := strings.Repeat("x", 20000)
	mw := middlewares.NewInputSafetyMiddleware(100, 7400)
	state := newSafetyState([]*schema.ToolInfo{
		{Name: "huge_tool", Desc: big},
	})
	_, _, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err == nil {
		t.Fatal("expected error exceeding safety limit")
	}
	if !errors.Is(err, middlewares.ErrInputFloorExceedsSafetyLimit) {
		t.Errorf("expected ErrInputFloorExceedsSafetyLimit, got %v", err)
	}
}
