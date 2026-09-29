package agents

import (
	"context"
	"slices"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// reservedCatalogNames are the reserved capability names the denylist
// resolution appends to the catalog (agent-tools-denylist D3).
var reservedCatalogNames = []string{ReservedShellTool, ReservedSubagentsTool, ReservedBackgroundShellTool}

func catalogSet(t *testing.T) []string {
	t.Helper()
	reg := NewDefaultToolRegistry(nil)
	names := append([]string{}, reg.Names()...)
	names = append(names, FilesystemToolNames...)
	names = append(names, reservedCatalogNames...)
	return names
}

// TestEffectiveToolsFromDenylist_EmptyExposesEverything pins D2's base case:
// an empty denylist exposes the whole catalog — every registry tool, every
// fs middleware tool, and the reserved capability names.
func TestEffectiveToolsFromDenylist_EmptyExposesEverything(t *testing.T) {
	effective := effectiveToolsFromDenylist(NewDefaultToolRegistry(nil), nil, false, false)
	catalog := catalogSet(t)
	if len(effective) != len(catalog) {
		t.Fatalf("effective set %d names, want the full catalog's %d", len(effective), len(catalog))
	}
	for _, name := range catalog {
		if !slices.Contains(effective, name) {
			t.Errorf("empty denylist must expose %q, got %v", name, effective)
		}
	}
}

// TestEffectiveToolsFromDenylist_NameAbsent pins the subtraction: a
// denylisted name is the one name missing, everything else stays.
func TestEffectiveToolsFromDenylist_NameAbsent(t *testing.T) {
	reg := NewDefaultToolRegistry(nil)
	effective := effectiveToolsFromDenylist(reg, []string{"web.search"}, false, false)
	if slices.Contains(effective, "web.search") {
		t.Errorf("denylisted web.search must be absent: %v", effective)
	}
	empty := effectiveToolsFromDenylist(reg, nil, false, false)
	if len(effective) != len(empty)-1 {
		t.Errorf("exactly one name must go missing, went from %d to %d", len(empty), len(effective))
	}
	if !slices.Contains(effective, "web.fetch") {
		t.Errorf("siblings must survive the subtraction: %v", effective)
	}
}

// TestEffectiveToolsFromDenylist_UnknownNamesInert pins D2's inertness: a
// denylist naming nothing the catalog provides changes nothing.
func TestEffectiveToolsFromDenylist_UnknownNamesInert(t *testing.T) {
	reg := NewDefaultToolRegistry(nil)
	empty := effectiveToolsFromDenylist(reg, nil, false, false)
	inert := effectiveToolsFromDenylist(reg, []string{"no.such.tool", "also.missing"}, false, false)
	if len(inert) != len(empty) {
		t.Fatalf("unknown names must be inert: %d names, want %d", len(inert), len(empty))
	}
	for _, name := range empty {
		if !slices.Contains(inert, name) {
			t.Errorf("unknown-name denylist lost %q", name)
		}
	}
}

// TestEffectiveToolsFromDenylist_FacadeCascade pins D3's facade rule:
// denying the browser alias disables the whole browser tool set; other tools
// stay.
func TestEffectiveToolsFromDenylist_FacadeCascade(t *testing.T) {
	reg := NewDefaultToolRegistry(nil)
	effective := effectiveToolsFromDenylist(reg, []string{BrowserToolAlias}, false, false)
	if slices.Contains(effective, BrowserToolAlias) {
		t.Errorf("the alias itself is not a catalog member: %v", effective)
	}
	for _, name := range reg.Names() {
		if len(name) > 8 && name[:8] == "browser." && slices.Contains(effective, name) {
			t.Errorf("denying the facade must cascade to %q: %v", name, effective)
		}
	}
	if !slices.Contains(effective, ReservedShellTool) || !slices.Contains(effective, "web.search") {
		t.Errorf("non-browser tools must survive the cascade: %v", effective)
	}
}

// TestEffectiveToolsFromDenylist_IndividualBrowserNames pins the legacy
// shape: individual browser.* names disable individually, leaving the rest
// of the set exposed.
func TestEffectiveToolsFromDenylist_IndividualBrowserNames(t *testing.T) {
	reg := NewDefaultToolRegistry(nil)
	effective := effectiveToolsFromDenylist(reg, []string{"browser.navigate", "browser.read"}, false, false)
	if slices.Contains(effective, "browser.navigate") || slices.Contains(effective, "browser.read") {
		t.Errorf("individually denied names must be absent: %v", effective)
	}
	if !slices.Contains(effective, "browser.snapshot") {
		t.Errorf("the rest of the browser set must remain: %v", effective)
	}
	if !slices.Contains(effective, "web.search") {
		t.Errorf("non-browser tools must survive: %v", effective)
	}
}

// TestEffectiveToolsFromDenylist_ReservedNamesOptOut pins D3's opt-out:
// the reserved names are present by default and each denies independently —
// denying background_shell keeps execute.
func TestEffectiveToolsFromDenylist_ReservedNamesOptOut(t *testing.T) {
	reg := NewDefaultToolRegistry(nil)
	for _, name := range reservedCatalogNames {
		denied := effectiveToolsFromDenylist(reg, []string{name}, false, false)
		if slices.Contains(denied, name) {
			t.Errorf("denied %q must be absent: %v", name, denied)
		}
		for _, other := range reservedCatalogNames {
			if other != name && !slices.Contains(denied, other) {
				t.Errorf("denying %q must not touch sibling %q: %v", name, other, denied)
			}
		}
	}
}

// TestEffectiveToolsFromDenylist_ChannelUnscoping pins D2's un-scoping: the
// context toolsets are pulled out of the disabled set on the runs whose
// execution context exposes them.
func TestEffectiveToolsFromDenylist_ChannelUnscoping(t *testing.T) {
	reg := NewDefaultToolRegistry(nil)
	denied := append([]string{}, ChannelToolNames...)
	denied = append(denied, SessionToolClose)

	plain := effectiveToolsFromDenylist(reg, denied, false, false)
	for _, name := range ChannelToolNames {
		if !slices.Contains(plain, name) {
			continue
		}
		t.Errorf("%q is registry-exposed; the denylist must hold on a non-channel run: %v", name, plain)
	}

	channel := effectiveToolsFromDenylist(reg, denied, true, false)
	for _, name := range ChannelToolNames {
		if !slices.Contains(channel, name) {
			t.Errorf("channel run must un-scope %q out of the denylist: %v", name, channel)
		}
	}
	if slices.Contains(channel, SessionToolClose) {
		t.Errorf("session.close stays denied without the facilitator session: %v", channel)
	}

	facilitator := effectiveToolsFromDenylist(reg, denied, true, true)
	if !slices.Contains(facilitator, SessionToolClose) {
		t.Errorf("facilitator work-session run must un-scope session.close: %v", facilitator)
	}
}

// TestResolve_OverrideReplacesDenylist pins D2's override rule: a per-turn
// AllowedTools allowlist replaces denylist resolution — an empty override
// strips every catalog tool an empty denylist would expose.
func TestResolve_OverrideReplacesDenylist(t *testing.T) {
	_, runner, _, _, req := setupCompactRunner(t, &compactModel{})
	ctx := context.Background()

	loadedWs, loadedAgent, _, _, err := runner.load(ctx, req)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	loadedAgent.DisabledTools = nil
	override := req
	override.AllowedTools = []string{}
	_, resolved, err := runner.resolve(ctx, override, loadedWs, loadedAgent, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved) != 0 {
		t.Fatalf("empty override must replace the empty denylist's full catalog, got %d tools", len(resolved))
	}
}

// TestResolve_GateWinsOverDenylistAndOverride pins the gate's supremacy: a
// workspace-disabled name is stripped whether the denylist exposure or the
// per-turn override exposed it. Stage-chained exactly as resolve orders them
// (denylist/override → gate), the same shape the channel gate tests use.
func TestResolve_GateWinsOverDenylistAndOverride(t *testing.T) {
	ctx := context.Background()
	governed := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), "/tmp/o",
		WithToolPolicy(&fakeToolPolicy{enabled: map[string]bool{"web.search": false}}))

	// Denylist exposure: the agent has not disabled web.search, the
	// workspace has.
	effective := effectiveToolsFromDenylist(NewDefaultToolRegistry(nil), nil, false, false)
	gated, err := governed.applyToolGate(ctx, "ws-1", effective)
	if err != nil {
		t.Fatalf("applyToolGate: %v", err)
	}
	if slices.Contains(gated, "web.search") {
		t.Fatalf("the gate must strip workspace-disabled web.search from the denylist exposure: %v", gated)
	}
	if !slices.Contains(gated, "web.fetch") {
		t.Errorf("ungoverned siblings must survive the gate: %v", gated)
	}

	// Override exposure: the per-turn allowlist names it anyway.
	override := scopeChannelToolsIn([]string{"web.search", "web.fetch"}, false)
	override = scopeSessionToolsIn(override, false)
	gated, err = governed.applyToolGate(ctx, "ws-1", override)
	if err != nil {
		t.Fatalf("applyToolGate override: %v", err)
	}
	if !slices.Contains(gated, "web.fetch") {
		t.Errorf("the override's allowed tool must resolve: %v", gated)
	}
	if slices.Contains(gated, "web.search") {
		t.Errorf("the gate must win over the per-turn override too: %v", gated)
	}
}

// TestResolve_DenylistDrivesFilesystemDisable pins task 3.3's disable
// computation end to end: a denied middleware tool name lands in the fs
// config's Disable set, an absent name does not.
func TestResolve_DenylistDrivesFilesystemDisable(t *testing.T) {
	_, runner, _, _, req := setupCompactRunner(t, &compactModel{})
	ctx := context.Background()

	loadedWs, loadedAgent, _, _, err := runner.load(ctx, req)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	loadedAgent.DisabledTools = []string{"write_file", "read_file"}
	cfg, _, err := runner.resolve(ctx, req, loadedWs, loadedAgent, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cfg.Filesystem == nil {
		t.Fatal("resolve must wire the filesystem capability")
	}
	if !slices.Contains(cfg.Filesystem.DisabledTools, "write_file") ||
		!slices.Contains(cfg.Filesystem.DisabledTools, "read_file") {
		t.Errorf("denied middleware tools must be disabled, got %v", cfg.Filesystem.DisabledTools)
	}
	for _, keep := range []string{"ls", "edit_file", "glob", "grep"} {
		if slices.Contains(cfg.Filesystem.DisabledTools, keep) {
			t.Errorf("absent-from-denylist %q must stay attached, got %v", keep, cfg.Filesystem.DisabledTools)
		}
	}
}

// TestResolve_DenylistDrivesReservedSignals pins task 3.2's compose signals:
// the reserved names resolve opt-out, and background_shell requires the
// shell tool.
func TestResolve_DenylistDrivesReservedSignals(t *testing.T) {
	_, runner, _, _, req := setupCompactRunner(t, &compactModel{})
	ctx := context.Background()

	loadedWs, loadedAgent, _, _, err := runner.load(ctx, req)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	cfg, _, err := runner.resolve(ctx, req, loadedWs, loadedAgent, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !cfg.SubagentsEnabled || !cfg.ShellBackgroundEnabled {
		t.Fatalf("empty denylist must default both reserved lanes on, got subagents=%t background=%t",
			cfg.SubagentsEnabled, cfg.ShellBackgroundEnabled)
	}

	denied := *loadedAgent
	denied.DisabledTools = []string{ReservedSubagentsTool, ReservedBackgroundShellTool}
	cfg, _, err = runner.resolve(ctx, req, loadedWs, &denied, nil)
	if err != nil {
		t.Fatalf("resolve denied: %v", err)
	}
	if cfg.SubagentsEnabled || cfg.ShellBackgroundEnabled {
		t.Fatalf("denying the reserved names must resolve both lanes off, got subagents=%t background=%t",
			cfg.SubagentsEnabled, cfg.ShellBackgroundEnabled)
	}

	shellOnly := *loadedAgent
	shellOnly.DisabledTools = []string{ReservedBackgroundShellTool}
	cfg, _, err = runner.resolve(ctx, req, loadedWs, &shellOnly, nil)
	if err != nil {
		t.Fatalf("resolve shell-only: %v", err)
	}
	if !cfg.SubagentsEnabled {
		t.Error("denying background_shell must not touch the delegation lane")
	}
	if cfg.ShellBackgroundEnabled {
		t.Error("denying background_shell must resolve the shell lane off")
	}
}

// TestAgentExposesTodoTools pins task 3.3's todo exposure in denylist form:
// default-on, keyed on todo_read (the summary directs the model there), and
// replaced wholesale by the per-turn override.
func TestAgentExposesTodoTools(t *testing.T) {
	req := ExecRequest{WorkspaceID: "ws", AgentID: "ag", SessionID: "s", UserID: "u"}

	if !agentExposesTodoTools(&domain.Agent{}, req) {
		t.Error("empty denylist must expose the todo surface")
	}
	denied := &domain.Agent{DisabledTools: []string{tools.NameTodoRead}}
	if agentExposesTodoTools(denied, req) {
		t.Error("denying todo_read must remove the summary surface")
	}
	if !agentExposesTodoTools(&domain.Agent{DisabledTools: []string{tools.NameTodoWrite}}, req) {
		t.Error("denying todo_write alone keeps todo_read, and the summary with it")
	}

	override := req
	override.AllowedTools = []string{tools.NameTodoRead}
	if !agentExposesTodoTools(&domain.Agent{DisabledTools: []string{tools.NameTodoRead}}, override) {
		t.Error("an override naming todo_read replaces the denylist")
	}
	override.AllowedTools = []string{"web.search"}
	if agentExposesTodoTools(&domain.Agent{}, override) {
		t.Error("an override without todo_read replaces the surface without it")
	}
}
