package middlewares_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"

	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

type mockFSChecker struct {
	disabled map[string]bool
}

func (m *mockFSChecker) Enabled(name string) bool {
	if m.disabled == nil {
		return true
	}
	if v, ok := m.disabled[name]; ok {
		return !v
	}
	return true
}

// ensure mockFSChecker satisfies the tools.EnabledChecker interface.
var _ tools.EnabledChecker = (*mockFSChecker)(nil)

func TestFSToggleMiddlewareBlocksDisabled(t *testing.T) {
	mw := middlewares.NewFSToggleMiddleware(&mockFSChecker{disabled: map[string]bool{"glob": true}})

	passthrough := func(_ context.Context, args string, _ ...tool.Option) (string, error) {
		return "PASSED:" + args, nil
	}

	ep, err := mw.WrapInvokableToolCall(context.Background(), passthrough, &adk.ToolContext{Name: "glob"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	out, err := ep(context.Background(), "args")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !strings.Contains(out, "tool glob is disabled") {
		t.Errorf("expected disabled message, got %q", out)
	}
	if strings.Contains(out, "PASSED") {
		t.Errorf("disabled tool should not have invoked passthrough: %q", out)
	}
}

func TestFSToggleMiddlewarePassesEnabled(t *testing.T) {
	mw := middlewares.NewFSToggleMiddleware(&mockFSChecker{disabled: map[string]bool{"glob": true}})

	passthrough := func(_ context.Context, args string, _ ...tool.Option) (string, error) {
		return "PASSED:" + args, nil
	}

	ep, err := mw.WrapInvokableToolCall(context.Background(), passthrough, &adk.ToolContext{Name: "read_file"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	out, err := ep(context.Background(), "args")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if out != "PASSED:args" {
		t.Errorf("enabled tool should pass through, got %q", out)
	}
}

func TestFSToggleMiddlewareNonFSToolPasses(t *testing.T) {
	mw := middlewares.NewFSToggleMiddleware(&mockFSChecker{disabled: map[string]bool{"glob": true}})

	passthrough := func(_ context.Context, args string, _ ...tool.Option) (string, error) {
		return "PASSED:" + args, nil
	}

	// A non-filesystem tool (e.g. memory_search) must pass even if "disabled"
	// map thinks otherwise; the toggle only governs fs tools.
	ep, err := mw.WrapInvokableToolCall(context.Background(), passthrough, &adk.ToolContext{Name: "memory_search"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	out, _ := ep(context.Background(), "args")
	if out != "PASSED:args" {
		t.Errorf("non-fs tool should pass through, got %q", out)
	}
}
