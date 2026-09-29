package agents

import (
	"context"
	"slices"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
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
	reg := NewDefaultToolRegistry(nil)
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
	runner := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), "/tmp/o",
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

	// An empty effective set disables all six: the workspace gate can strip
	// every middleware tool, and disabledFilesystemTools must report each.
	all := disabledFilesystemTools(nil)
	if len(all) != len(FilesystemToolNames) {
		t.Errorf("expected all fs tools disabled for an empty effective set, got %v", all)
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

// TestApplyToolGate_AlwaysOnSurvivesStaleDisabledRows pins the gate half of
// the always-on exemption (always-on-channel-tools 2.1). The exemption lives
// in ToolSettingsService.EnabledTools, so the full gate path a channel run
// uses — scope the toolset in, then the gate — must not strip an always-on
// key even while stale enabled=false rows sit in the workspace's settings.
func TestApplyToolGate_AlwaysOnSurvivesStaleDisabledRows(t *testing.T) {
	ctx := context.Background()
	svc, tstore, wsID := toolSettingsFixture(t)

	// enabled=false rows for all three keys, written straight through the
	// store — the stale shape the exemption must ignore.
	for _, key := range []string{ChannelToolPost, ChannelToolHistory, SessionToolClose} {
		seedDisabledRow(t, tstore, wsID, key)
	}

	runner := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), "/tmp/o",
		WithToolPolicy(svc))

	// Channel-run composition (runner.go resolve): the channel toolset rides
	// the scoped-in allowlist through the gate.
	gated, err := runner.applyToolGate(ctx, wsID, scopeChannelToolsIn([]string{"memory"}, true))
	if err != nil {
		t.Fatalf("applyToolGate: %v", err)
	}
	for _, name := range ChannelToolNames {
		if !slices.Contains(gated, name) {
			t.Errorf("stale disabled row must not strip %s from a channel run: %v", name, gated)
		}
	}

	// Facilitator inside an open work session (exposeSessionClose): the same
	// stale rows must not strip session.close either.
	sessionGated, err := runner.applyToolGate(ctx, wsID,
		scopeSessionToolsIn(scopeChannelToolsIn([]string{"memory"}, true), true))
	if err != nil {
		t.Fatalf("applyToolGate session: %v", err)
	}
	if !slices.Contains(sessionGated, SessionToolClose) {
		t.Errorf("stale disabled row must not strip %s from a facilitator session run: %v", SessionToolClose, sessionGated)
	}
}

// TestApplyToolGate_NonChannelRunsStillStrip pins the existing context
// guarantee (always-on-channel-tools 2.2): always-on only exempts a tool from
// the workspace enabled set — it never widens where the toolset is exposed.
// A non-channel run strips all three keys even when the agent's tool
// selection carries them and the workspace policy would allow them.
func TestApplyToolGate_NonChannelRunsStillStrip(t *testing.T) {
	ctx := context.Background()
	// A policy that explicitly allows every key: the strip must come from the
	// run's execution context, not the gate.
	runner := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), "/tmp/o",
		WithToolPolicy(&fakeToolPolicy{enabled: map[string]bool{
			ChannelToolPost:    true,
			ChannelToolHistory: true,
			SessionToolClose:   true,
		}}))

	// Non-channel composition (runner.go resolve): nothing scoped in, the
	// gate passes the allowlisted keys, then both context strips apply.
	allowlist := scopeChannelToolsIn([]string{"memory", ChannelToolPost, ChannelToolHistory, SessionToolClose}, false)
	allowlist = scopeSessionToolsIn(allowlist, false)
	gated, err := runner.applyToolGate(ctx, "ws-1", allowlist)
	if err != nil {
		t.Fatalf("applyToolGate: %v", err)
	}
	gated = withoutChannelTools(gated)
	gated = withoutSessionTools(gated)

	for _, name := range []string{ChannelToolPost, ChannelToolHistory, SessionToolClose} {
		if slices.Contains(gated, name) {
			t.Errorf("non-channel run must strip %s despite the allowlist and an allowing policy: %v", name, gated)
		}
	}
	if !slices.Contains(gated, "memory") {
		t.Errorf("non-channel run must keep ungoverned tools: %v", gated)
	}
}

// TestToolGate_DocumentSearch pins the 4.4 gate matrix for the new family
// member: document.search is an ordinary registry tool — denylist resolution
// exposes it when the references service is wired, the agent denylist removes
// it by name, and the workspace tool gate strips it independently. It is
// default-on in exactly the same sense as every other registered tool (no
// always-on exemption, no separate permission path), while document.read
// stays available with or without the references service.
func TestToolGate_DocumentSearch(t *testing.T) {
	ctx := context.Background()
	reg := NewDefaultToolRegistry(nil, WithDocumentTools(&fakeDocumentTools{}))

	// Denylist resolution: wired, an empty denylist exposes both document
	// tools; denying document.search removes it alone.
	full := effectiveToolsFromDenylist(reg, nil, false, false)
	if !slices.Contains(full, tools.NameDocumentSearch) || !slices.Contains(full, tools.NameDocumentRead) {
		t.Errorf("empty denylist must expose document.search and document.read: %v", full)
	}
	denied := effectiveToolsFromDenylist(reg, []string{tools.NameDocumentSearch}, false, false)
	if slices.Contains(denied, tools.NameDocumentSearch) {
		t.Errorf("denylisted document.search must be absent from the effective set: %v", denied)
	}
	if !slices.Contains(denied, tools.NameDocumentRead) {
		t.Errorf("denying document.search must not touch document.read: %v", denied)
	}

	// Workspace gate: gate-off strips document.search after denylist
	// resolution, again without touching document.read.
	runner := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), "/tmp/o",
		WithToolPolicy(&fakeToolPolicy{enabled: map[string]bool{
			tools.NameDocumentSearch: false,
		}}))
	gated, err := runner.applyToolGate(ctx, "ws-1", effectiveToolsFromDenylist(reg, nil, false, false))
	if err != nil {
		t.Fatalf("applyToolGate: %v", err)
	}
	if slices.Contains(gated, tools.NameDocumentSearch) {
		t.Errorf("gate-off document.search must be dropped: %v", gated)
	}
	if !slices.Contains(gated, tools.NameDocumentRead) {
		t.Errorf("gate-off document.search must not strip document.read: %v", gated)
	}
}

// TestToolGate_DocumentSearchAbsentWhenUnwired pins the composition end: a
// registry built without WithDocumentTools offers no document.search to the
// denylist catalog at all, so no agent — however permissive its denylist —
// resolves the tool, and the gate has nothing to strip.
func TestToolGate_DocumentSearchAbsentWhenUnwired(t *testing.T) {
	reg := NewDefaultToolRegistry(nil)
	effective := effectiveToolsFromDenylist(reg, nil, false, false)
	if slices.Contains(effective, tools.NameDocumentSearch) {
		t.Errorf("unwired registries must not expose document.search: %v", effective)
	}
	if !slices.Contains(effective, tools.NameDocumentRead) {
		t.Errorf("document.read must stay exposed without the references service: %v", effective)
	}
}
