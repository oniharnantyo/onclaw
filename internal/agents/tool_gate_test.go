package agents

import (
	"context"
	"slices"
	"testing"
)

// fakeToolPolicy is a scripted ToolPolicy for gate tests.
type fakeToolPolicy struct {
	enabled map[string]bool
	configs map[string]map[string]any
	err     error
}

func (f *fakeToolPolicy) EnabledTools(context.Context, string) (map[string]bool, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.enabled, nil
}

func (f *fakeToolPolicy) ToolConfigs(context.Context, string) (map[string]map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.configs, nil
}

func TestExpandBrowserAlias(t *testing.T) {
	reg := NewDefaultToolRegistry()
	expanded := expandBrowserAlias(reg, []string{"web.search", BrowserToolAlias, "execute"})

	if !slices.Contains(expanded, "browser.snapshot") || !slices.Contains(expanded, "browser.click") {
		t.Errorf("alias did not expand to browser set: %v", expanded)
	}
	if slices.Contains(expanded, BrowserToolAlias) {
		t.Errorf("alias itself must not survive expansion: %v", expanded)
	}
	if !slices.Contains(expanded, "web.search") || !slices.Contains(expanded, "execute") {
		t.Errorf("non-browser names must pass through: %v", expanded)
	}

	// Legacy individual names pass through unchanged.
	legacy := expandBrowserAlias(reg, []string{"browser.navigate", "browser.read"})
	if len(legacy) != 2 || !slices.Contains(legacy, "browser.navigate") || !slices.Contains(legacy, "browser.read") {
		t.Errorf("legacy names must pass through: %v", legacy)
	}
}

func TestApplyToolGate(t *testing.T) {
	runner := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), "/tmp/o",
		WithToolPolicy(&fakeToolPolicy{enabled: map[string]bool{
			"web.search": false,
			"browser":    false,
		}}))

	gated, err := runner.applyToolGate(context.Background(), "ws1", []string{
		"web.search", BrowserToolAlias, "web.fetch",
	})
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if slices.Contains(gated, "web.search") {
		t.Errorf("workspace-disabled tool must be dropped: %v", gated)
	}
	for _, name := range gated {
		if len(name) > 8 && name[:8] == "browser." {
			t.Errorf("browser members must fall under the disabled alias: %v", gated)
		}
	}
	if !slices.Contains(gated, "web.fetch") {
		t.Errorf("enabled tool must survive: %v", gated)
	}
}

func TestDisabledFilesystemTools(t *testing.T) {
	disabled := disabledFilesystemTools([]string{"read_file", "glob", ReservedShellTool})
	for _, want := range []string{"ls", "write_file", "edit_file", "grep"} {
		if !slices.Contains(disabled, want) {
			t.Errorf("expected %q disabled, got %v", want, disabled)
		}
	}
	for _, keep := range []string{"read_file", "glob"} {
		if slices.Contains(disabled, keep) {
			t.Errorf("%q must stay enabled", keep)
		}
	}

	// Empty effective allowlist disables all six (empty allowlist exposes
	// nothing — spec scenario).
	all := disabledFilesystemTools(nil)
	if len(all) != len(FilesystemToolNames) {
		t.Errorf("expected all fs tools disabled for empty allowlist, got %v", all)
	}
}

func TestToolEnabledByPolicy(t *testing.T) {
	enabled := map[string]bool{"browser": false}
	if toolEnabledByPolicy(enabled, "browser.click") {
		t.Error("browser member must inherit the alias's disabled state")
	}
	if !toolEnabledByPolicy(map[string]bool{}, "web.search") {
		t.Error("ungoverned names default to enabled")
	}
	if toolEnabledByPolicy(map[string]bool{"web.search": false}, "web.search") {
		t.Error("explicitly disabled tool must be off")
	}
}
