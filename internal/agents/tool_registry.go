package agents

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ToolContext carries the per-execution scope a tool constructor may need to
// bind itself to: the tenancy path, the executing principal's identity
// (workspace, agent, user), the workspace-local timezone, and the agent's
// on-disk workspace jail root plus the chat session that triggered the run.
// Stateless tools ignore it; stateful tools (browser sessions, memory
// documents) scope themselves to it. ToolConfigs carries the workspace's
// resolved per-tool configuration for configurable tools (secrets decrypted,
// env fallbacks merged); constructors of configurable tools read their
// settings from it.
type ToolContext struct {
	WorkspaceSlug string
	AgentSlug     string
	AgentDir      string // agent workspace jail root
	SessionID     string
	WorkspaceID   string
	AgentID       string
	UserID        string
	WorkspaceTZ   *time.Location // workspace-local clock; never nil from the runner
	ToolConfigs   map[string]map[string]any
}

// ToolConstructor builds a tool.BaseTool for a specific execution context.
// Implementations are registered by dotted name (e.g. "web.search") and
// resolved at agent composition time. Eino types are confined to this package
// and the composer boundary (Config.Tools); callers outside internal/agents
// never reference tool.BaseTool directly.
type ToolConstructor func(ToolContext) (tool.BaseTool, error)

// ToolRegistry is the in-process tool surface. An agent's tools allowlist
// selects from it at composition time; unknown names are inert — there are no
// DB rows to validate against.
type ToolRegistry interface {
	// Register adds a tool under its dotted name. The name must be unique
	// within the registry; re-registering the same name replaces it.
	Register(name string, ctor ToolConstructor)

	// Lookup returns the tool for a name, or (nil, false) when absent.
	Lookup(name string) (ToolConstructor, bool)

	// Names returns all registered tool names (sorted).
	Names() []string

	// Filter returns a ToolFilter bound to this registry applying allowlist
	// semantics.
	Filter() ToolFilter
}

// ToolFilter is a domain-level view of the tools available to a given agent
// after applying its tools allowlist. It is consumed by the engine to build
// the agent's tool set.
type ToolFilter interface {
	// Available returns the registered tool names the agent is allowed to use,
	// in a stable order (sorted by name). An empty allowlist selects no tools;
	// allowed names that are not registered are silently ignored (inert).
	Available(allowed []string) []string
}

// SessionTeardown is an optional registry capability: registries whose tools
// hold per-execution resources (browser sessions) expose per-session cleanup
// that the runner calls when an execution ends.
type SessionTeardown interface {
	CloseSession(sessionID string)
}

type toolRegistry struct {
	ctors   map[string]ToolConstructor
	browser *tools.BrowserManager
}

// NewToolRegistry returns a fresh, empty registry.
func NewToolRegistry() *toolRegistry {
	return &toolRegistry{ctors: make(map[string]ToolConstructor)}
}

// NewDefaultToolRegistry returns the registry pre-loaded with every built-in
// tool. The composition root uses this single constructor; individual
// registrations stay internal to this package. The memory store backs the
// memory tool's three persistent scopes; the executing run's identity binds
// per construction through ToolContext. Instance-level tool configuration
// (search provider, browser CDP endpoint) is read from the environment at
// registration time.
func NewDefaultToolRegistry(memories store.MemoryStore) ToolRegistry {
	reg := NewToolRegistry()
	reg.browser = tools.NewBrowserManager()

	reg.Register(tools.Name, func(tctx ToolContext) (tool.BaseTool, error) {
		provider, err := searchProviderFor(tctx)
		if err != nil {
			return nil, err
		}
		return tools.NewWebSearch(tools.WithSearchProvider(provider))
	})

	reg.Register(tools.NameWebFetch, func(ToolContext) (tool.BaseTool, error) {
		return tools.NewWebFetch()
	})

	reg.Register(tools.NameMemory, func(tctx ToolContext) (tool.BaseTool, error) {
		return tools.NewMemory(memories, tctx.WorkspaceID, tctx.AgentID, tctx.UserID, tctx.WorkspaceTZ)
	})

	reg.Register(tools.NameDeleteFile, func(tctx ToolContext) (tool.BaseTool, error) {
		return tools.NewDeleteFile(tctx.AgentDir)
	})

	reg.Register(tools.NameBrowserNavigate, func(tctx ToolContext) (tool.BaseTool, error) {
		core, err := browserCore(reg, tctx)
		if err != nil {
			return nil, err
		}
		return &tools.BrowserNavigateTool{BrowserToolCore: core}, nil
	})
	reg.Register(tools.NameBrowserAct, func(tctx ToolContext) (tool.BaseTool, error) {
		core, err := browserCore(reg, tctx)
		if err != nil {
			return nil, err
		}
		return &tools.BrowserActTool{BrowserToolCore: core}, nil
	})
	reg.Register(tools.NameBrowserRead, func(tctx ToolContext) (tool.BaseTool, error) {
		core, err := browserCore(reg, tctx)
		if err != nil {
			return nil, err
		}
		return &tools.BrowserReadTool{BrowserToolCore: core}, nil
	})
	reg.Register(tools.NameBrowserScreenshot, func(tctx ToolContext) (tool.BaseTool, error) {
		core, err := browserCore(reg, tctx)
		if err != nil {
			return nil, err
		}
		return &tools.BrowserScreenshotTool{BrowserToolCore: core}, nil
	})
	reg.Register(tools.NameBrowserSnapshot, func(tctx ToolContext) (tool.BaseTool, error) {
		core, err := browserCore(reg, tctx)
		if err != nil {
			return nil, err
		}
		return &tools.BrowserSnapshotTool{BrowserToolCore: core}, nil
	})
	for _, refTool := range []struct {
		name         string
		action       string
		requiresText bool
	}{
		{tools.NameBrowserClick, "click", false},
		{tools.NameBrowserType, "type", true},
		{tools.NameBrowserHover, "hover", false},
		{tools.NameBrowserDrag, "drag", false},
		{tools.NameBrowserSelectOption, "select_option", true},
	} {
		refTool := refTool
		reg.Register(refTool.name, func(tctx ToolContext) (tool.BaseTool, error) {
			core, err := browserCore(reg, tctx)
			if err != nil {
				return nil, err
			}
			return tools.NewRefActionTool(core, refTool.action, refTool.requiresText), nil
		})
	}

	return reg
}

// browserCore builds the shared browser tool plumbing for an execution: the
// session manager plus the workspace's browser configuration (design.md D8).
func browserCore(reg *toolRegistry, tctx ToolContext) (tools.BrowserToolCore, error) {
	config := tools.DefaultBrowserConfig()
	if raw, ok := tctx.ToolConfigs[BrowserToolAlias]; ok {
		config = tools.BrowserConfigFromToolConfig(raw)
	}
	reg.browser.ApplyConfig(config)
	return tools.BrowserToolCore{
		Sessions:      reg.browser,
		SessionID:     tctx.SessionID,
		AgentDir:      tctx.AgentDir,
		ActionTimeout: config.ActionTimeout,
	}, nil
}

// CloseSession implements SessionTeardown.
func (r *toolRegistry) CloseSession(sessionID string) {
	if r.browser != nil {
		r.browser.CloseSession(sessionID)
	}
}

// errWebSearchNotConfigured is the construction error for a workspace with no
// usable search provider entries (design.md D4): explicit and actionable,
// raised before any network attempt, with no credential-free fallback.
var errWebSearchNotConfigured = errors.New("web.search is not configured — add a provider in Settings → Tools")

// webSearchWindow is the positional failover window: the first N entries in
// list order serve; entries below it never do (design.md D2).
const webSearchWindow = 3

// searchProviderFor builds the per-execution web.search backend from the
// workspace's resolved tool config: each entry's provider is constructed with
// a per-attempt HTTP client bounded by request_timeout_seconds (design.md D7)
// and the first webSearchWindow entries are chained as the failover window
// (design.md D2). Config arrives decrypted with instance env already merged
// by the settings service; with no entries there is no fallback —
// construction fails with the explicit not-configured error (design.md D4).
func searchProviderFor(tctx ToolContext) (tools.SearchProvider, error) {
	named, err := webSearchChainEntries(tctx.ToolConfigs[tools.Name])
	if err != nil {
		return nil, err
	}
	if len(named) == 0 {
		return nil, errWebSearchNotConfigured
	}
	if len(named) > webSearchWindow {
		named = named[:webSearchWindow]
	}
	return tools.NewChainSearchProvider(named)
}

// webSearchChainEntries constructs every configured entry's provider from the
// decrypted config: the credential comes from api_key or base_url per the
// provider's registry kind, and construction errors (unknown provider,
// missing credential) fail the build naming the provider.
func webSearchChainEntries(config map[string]any) ([]tools.NamedSearchProvider, error) {
	entries := configEntries(config)
	timeout := webSearchAttemptTimeout(config)
	named := make([]tools.NamedSearchProvider, 0, len(entries))
	for _, entryItem := range entries {
		provider := stringConfig(entryItem["provider"])
		credential := ""
		if info, known := tools.SearchProviderInfoFor(provider); known {
			switch info.Credential {
			case tools.SearchCredentialAPIKey:
				credential = stringConfig(entryItem["api_key"])
			case tools.SearchCredentialBaseURL:
				credential = stringConfig(entryItem["base_url"])
			}
		}
		chainLink, err := tools.NewSearchProviderByCredential(provider, credential, &http.Client{Timeout: timeout})
		if err != nil {
			return nil, err
		}
		name := stringConfig(entryItem["name"])
		if name == "" {
			name = provider
		}
		named = append(named, tools.NamedSearchProvider{Name: name, Provider: chainLink})
	}
	return named, nil
}

// webSearchAttemptTimeout resolves the per-attempt request timeout: default
// 10s, clamped positive and at most 60s (design.md D7). Values that fail
// validation fall back to the default rather than disabling the bound.
func webSearchAttemptTimeout(config map[string]any) time.Duration {
	seconds := webSearchDefaultTimeoutSeconds
	if raw, present := config["request_timeout_seconds"]; present && !isEmptyConfigValue(raw) {
		if n, ok := numericConfigValue(raw); ok && n > 0 {
			seconds = int(n)
		}
	}
	if seconds > webSearchMaxTimeoutSeconds {
		seconds = webSearchMaxTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (r *toolRegistry) Register(name string, ctor ToolConstructor) {
	r.ctors[name] = ctor
}

func (r *toolRegistry) Lookup(name string) (ToolConstructor, bool) {
	ctor, ok := r.ctors[name]
	return ctor, ok
}

func (r *toolRegistry) Names() []string {
	names := make([]string, 0, len(r.ctors))
	for n := range r.ctors {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Filter returns a ToolFilter bound to this registry that resolves the
// allowlist semantics.
func (r *toolRegistry) Filter() ToolFilter {
	return &allowlistFilter{reg: r}
}

type allowlistFilter struct {
	reg *toolRegistry
}

func (f *allowlistFilter) Available(allowed []string) []string {
	avail := make([]string, 0, len(allowed))
	for _, name := range allowed {
		if _, ok := f.reg.ctors[name]; ok {
			avail = append(avail, name)
		}
	}
	sort.Strings(avail)
	return avail
}

// ResolvedTools builds the concrete tool.BaseTool slice for an agent from the
// registry and its allowlist. An empty allowlist selects no tools; allowed
// names that don't resolve are silently dropped. Returns an error only if a
// registered tool's constructor fails to build.
func ResolvedTools(tctx ToolContext, reg ToolRegistry, allowed []string) ([]string, []tool.BaseTool, error) {
	f := reg.Filter()
	names := f.Available(allowed)
	resolved := make([]tool.BaseTool, 0, len(names))
	for _, name := range names {
		ctor, ok := reg.Lookup(name)
		if !ok {
			continue
		}
		t, err := ctor(tctx)
		if err != nil {
			return nil, nil, fmt.Errorf("build tool %q: %w", name, err)
		}
		resolved = append(resolved, t)
	}
	return names, resolved, nil
}
