package agents

import (
	"context"
	"strings"
)

// FilesystemToolNames are the fs middleware tool names the denylist and the
// workspace gate address. A name absent from the effective tool set disables
// that middleware tool via eino's per-tool Disable configuration
// (workspace-tool-catalog D3): denylist resolution subtracts the agent's
// disabled_tools names first (denylist D2), so a middleware tool is absent
// exactly when the denylist names it or the workspace gate strips it.
var FilesystemToolNames = []string{"ls", "read_file", "write_file", "edit_file", "glob", "grep"}

// allowAllToolPolicy is the default gate when no workspace policy is wired:
// every tool resolves per the resolved effective set alone.
type allowAllToolPolicy struct{}

func (allowAllToolPolicy) EnabledTools(context.Context, string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func (allowAllToolPolicy) ToolConfigs(context.Context, string) (map[string]map[string]any, error) {
	return map[string]map[string]any{}, nil
}

// effectiveToolsFromDenylist resolves the agent-tier denylist into the
// turn's effective tool set (agent-tools-denylist D2/D3): the full catalog —
// every registered built-in, the six fs middleware tools, and the reserved
// capability names — minus the denylist. An empty denylist exposes the whole
// catalog; names that match nothing are inert. Denying the browser facade
// alias disables the whole browser tool set (the alias itself is not a
// catalog member — its exposure is carried by the browser.* registry names);
// individual browser.* names disable individually. Channel runs un-scope the
// channel toolset out of the denylist and a facilitator work-session run
// un-scopes session.close: context tools the run's execution context exposes
// regardless of the agent's choice.
func effectiveToolsFromDenylist(reg ToolRegistry, disabled []string, channelRun, exposeSessionClose bool) []string {
	disabledSet := make(map[string]bool, len(disabled))
	for _, name := range disabled {
		disabledSet[name] = true
	}
	if channelRun {
		for _, name := range ChannelToolNames {
			delete(disabledSet, name)
		}
	}
	if exposeSessionClose {
		delete(disabledSet, SessionToolClose)
	}
	browserAliasDenied := disabledSet[BrowserToolAlias]

	catalog := make([]string, 0, len(reg.Names())+len(FilesystemToolNames)+3)
	catalog = append(catalog, reg.Names()...)
	catalog = append(catalog, FilesystemToolNames...)
	catalog = append(catalog, ReservedShellTool, ReservedSubagentsTool, ReservedBackgroundShellTool)
	effective := make([]string, 0, len(catalog))
	for _, name := range catalog {
		if disabledSet[name] {
			continue
		}
		if browserAliasDenied && strings.HasPrefix(name, BrowserToolAlias+".") {
			continue
		}
		effective = append(effective, name)
	}
	return effective
}

// applyToolGate resolves the effective tool set for an execution: expand the
// browser facade alias (the per-turn override path — denylist resolution
// expands it by cascade in effectiveToolsFromDenylist), then filter by the
// workspace's enabled tool set. A tool name is gated by its catalog key —
// individual browser.* names fall under the browser alias's setting.
func (r *Runner) applyToolGate(ctx context.Context, workspaceID string, names []string) ([]string, error) {
	expanded := expandBrowserAlias(r.toolRegistry, names)
	enabled, err := r.toolPolicy.EnabledTools(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	gated := make([]string, 0, len(expanded))
	for _, name := range expanded {
		if toolEnabledByPolicy(enabled, name) {
			gated = append(gated, name)
		}
	}
	return gated, nil
}

// expandBrowserAlias replaces the facade alias in a name set with every
// registered browser.* tool name. Legacy individual names pass through
// unchanged (workspace-tool-catalog design.md D2).
func expandBrowserAlias(reg ToolRegistry, names []string) []string {
	expanded := make([]string, 0, len(names)+4)
	for _, name := range names {
		if name != BrowserToolAlias {
			expanded = append(expanded, name)
			continue
		}
		for _, registered := range reg.Names() {
			if strings.HasPrefix(registered, BrowserToolAlias+".") {
				expanded = append(expanded, registered)
			}
		}
	}
	return expanded
}

// toolEnabledByPolicy consults the workspace enabled set: a direct catalog
// key wins; browser.* members inherit the browser alias's setting; names the
// catalog doesn't govern are enabled by default.
func toolEnabledByPolicy(enabled map[string]bool, name string) bool {
	if on, ok := enabled[name]; ok {
		return on
	}
	if strings.HasPrefix(name, BrowserToolAlias+".") {
		if on, ok := enabled[BrowserToolAlias]; ok {
			return on
		}
	}
	return true
}

// disabledFilesystemTools returns the fs middleware tool names absent from
// the effective tool set — the Disable set for the filesystem middleware.
// Under denylist resolution the agent's disabled_tools names are subtracted
// before this runs, so denylist presence lands here alongside the gate's
// strips (agent-tools-denylist task 3.3).
func disabledFilesystemTools(effective []string) []string {
	allowed := make(map[string]bool, len(effective))
	for _, name := range effective {
		allowed[name] = true
	}
	disabled := make([]string, 0, len(FilesystemToolNames))
	for _, name := range FilesystemToolNames {
		if !allowed[name] {
			disabled = append(disabled, name)
		}
	}
	return disabled
}
