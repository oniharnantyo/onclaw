package agents

import (
	"context"
	"strings"
)

// FilesystemToolNames are the fs middleware tool allowlist names. A name
// absent from the effective allowlist disables that middleware tool via
// eino's per-tool Disable configuration (design.md D3).
var FilesystemToolNames = []string{"ls", "read_file", "write_file", "edit_file", "glob", "grep"}

// allowAllToolPolicy is the default gate when no workspace policy is wired:
// every tool resolves per its allowlist alone.
type allowAllToolPolicy struct{}

func (allowAllToolPolicy) EnabledTools(context.Context, string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func (allowAllToolPolicy) ToolConfigs(context.Context, string) (map[string]map[string]any, error) {
	return map[string]map[string]any{}, nil
}

// applyToolGate resolves the effective allowlist for an execution: expand the
// browser facade alias to the current browser tool set, then filter by the
// workspace's enabled tool set. A tool name is gated by its catalog key —
// individual browser.* names fall under the browser alias's setting.
func (r *Runner) applyToolGate(ctx context.Context, workspaceID string, allowlist []string) ([]string, error) {
	expanded := expandBrowserAlias(r.toolRegistry, allowlist)
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

// expandBrowserAlias replaces the facade alias in an allowlist with every
// registered browser.* tool name. Legacy individual names pass through
// unchanged (design.md D2).
func expandBrowserAlias(reg ToolRegistry, allowlist []string) []string {
	expanded := make([]string, 0, len(allowlist)+4)
	for _, name := range allowlist {
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
// the effective allowlist — the Disable set for the filesystem middleware.
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
