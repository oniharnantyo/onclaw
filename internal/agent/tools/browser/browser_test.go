package browser_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/agent/tools/browser"
)

func TestBrowserToolsRegistration(t *testing.T) {
	// Verify that the browser category is configurable
	if !tools.IsConfigurable("Browser") {
		t.Error("expected Browser category to be configurable")
	}

	entry, ok := tools.GetConfigEntry("Browser")
	if !ok {
		t.Fatal("expected Browser config entry to exist")
	}
	if entry.JSONSchema == "" {
		t.Error("expected non-empty JSON schema for Browser category")
	}

	// Verify that browser tools are registered
	registeredTools := tools.GetRegistry()
	var browserTools []tools.Tool
	for _, tl := range registeredTools {
		if tl.Category() == "Browser" {
			browserTools = append(browserTools, tl)
		}
	}

	// At minimum, we expect the 11 tools
	expectedNames := map[string]bool{
		"browser_start":      true,
		"browser_stop":       true,
		"browser_open":       true,
		"browser_close":      true,
		"browser_navigate":   true,
		"browser_snapshot":   true,
		"browser_act":        true,
		"browser_screenshot": true,
		"browser_tabs":       true,
		"browser_status":     true,
		"browser_console":    true,
	}

	if len(browserTools) < 11 {
		t.Errorf("expected at least 11 browser tools registered, got %d", len(browserTools))
	}

	for _, tl := range browserTools {
		delete(expectedNames, tl.Name())
	}

	if len(expectedNames) > 0 {
		t.Errorf("missing registered browser tools: %v", expectedNames)
	}
}

func TestBrowserToolsBuild(t *testing.T) {
	// Test building a browser tool
	registeredTools := tools.GetRegistry()
	var startTool tools.Tool
	for _, tl := range registeredTools {
		if tl.Name() == "browser_start" {
			startTool = tl
			break
		}
	}

	if startTool == nil {
		t.Fatal("browser_start tool not registered")
	}

	scope := &tools.Scope{
		Workspace:    "test_ws",
		ToolGroupCfg: &dummyToolGroupCfg{},
	}

	invokable := startTool.Build(scope)
	if invokable == nil {
		t.Fatal("failed to build browser_start tool")
	}

	info, err := invokable.Info(context.Background())
	if err != nil {
		t.Fatalf("failed to get tool info: %v", err)
	}
	if info.Name != "browser_start" {
		t.Errorf("expected tool name 'browser_start', got %q", info.Name)
	}
}

type dummyToolGroupCfg struct{}

func (d *dummyToolGroupCfg) GetConfig(ctx context.Context, category string) (string, error) {
	return `{"engine":"lightpanda"}`, nil
}

// Import browser package to run side effects
var _ = browser.Mgr

// TestBrowserNavigateNoActivePageReturnsObservation verifies that calling a
// browser op with no active page/engine returns a recoverable observation (nil
// error) so the agent can start a browser, rather than terminating the turn.
func TestBrowserNavigateNoActivePageReturnsObservation(t *testing.T) {
	scope := &tools.Scope{Workspace: "test_ws", ToolGroupCfg: &dummyToolGroupCfg{}}
	var navTool tools.Tool
	for _, tl := range tools.GetRegistry() {
		if tl.Name() == "browser_navigate" {
			navTool = tl
			break
		}
	}
	if navTool == nil {
		t.Fatal("browser_navigate tool not registered")
	}
	invokable := navTool.Build(scope)
	res, err := invokable.InvokableRun(context.Background(), `{"url":"http://example.com"}`)
	if err != nil {
		t.Fatalf("expected nil error (recoverable observation), got %v", err)
	}
	if !strings.Contains(res, "could not complete") {
		t.Errorf("expected recoverable observation for no active page, got %q", res)
	}
}

// TestBrowserNavigate_ContextCancelledPropagated verifies context cancellation
// is returned as a fatal error and not converted to an observation.
func TestBrowserNavigate_ContextCancelledPropagated(t *testing.T) {
	scope := &tools.Scope{Workspace: "test_ws", ToolGroupCfg: &dummyToolGroupCfg{}}
	var navTool tools.Tool
	for _, tl := range tools.GetRegistry() {
		if tl.Name() == "browser_navigate" {
			navTool = tl
			break
		}
	}
	if navTool == nil {
		t.Fatal("browser_navigate tool not registered")
	}
	invokable := navTool.Build(scope)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := invokable.InvokableRun(ctx, `{"url":"http://example.com"}`)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled to propagate, got %v", err)
	}
}

// TestBrowserAllOpsNoFatalError exercises every browser op and asserts the
// core contract: no op ever returns a fatal error that would terminate the
// agent turn. On the happy path each op returns a nil error (whether a success
// result or a recoverable observation); and with a cancelled context the
// cancellation is propagated (covering each op's ctx.Err guard).
func TestBrowserAllOpsNoFatalError(t *testing.T) {
	scope := &tools.Scope{Workspace: "test_ws", ToolGroupCfg: &dummyToolGroupCfg{}}
	argsByTool := map[string]string{
		"browser_navigate": `{"url":"http://example.com"}`,
		"browser_act":      `{"kind":"click","ref":"e1"}`,
	}
	for _, tl := range tools.GetRegistry() {
		if tl.Category() != "Browser" {
			continue
		}
		inv := tl.Build(scope)
		args := argsByTool[tl.Name()]
		if args == "" {
			args = "{}"
		}
		// Happy path: must never return a fatal error.
		res, err := inv.InvokableRun(context.Background(), args)
		if err != nil {
			t.Errorf("%s: expected nil error (never fatal), got %v", tl.Name(), err)
			continue
		}
		_ = res
		// Cancelled path: cancellation must propagate (covers the guard branch).
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, errC := inv.InvokableRun(ctx, args)
		if !errors.Is(errC, context.Canceled) {
			t.Errorf("%s: expected context.Canceled to propagate, got %v", tl.Name(), errC)
		}
	}
}
