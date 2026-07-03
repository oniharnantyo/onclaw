package browser_test

import (
	"context"
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
