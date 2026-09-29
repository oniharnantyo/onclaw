package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/memory"
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

	// Document-family bindings (add-document-read-tool /
	// add-document-create-tool / add-reference-documents).
	// ReadOnlyRoots lists the run's extra read-only jail roots (the workspace
	// skills tree plus the turn's materialized drop-lane mount) so document.read
	// can convert chat-attached documents and document.create can read
	// chat-delivered templates. DocumentPublisher publishes a created document
	// into the workspace's blob storage and returns its capability URL; set
	// only on runners wired with the workspace blob store. Documents carries
	// the workspace's reference-document library port for the compose-side
	// consumers (manifest injection D6, references mount D8): nil on runners
	// without the references service — document.search is then not registered
	// (WithDocumentTools, the memory-search precedent), while document.read
	// stays available without it.
	ReadOnlyRoots     []string
	DocumentPublisher DocumentPublisher
	Documents         DocumentTools

	// Channel-run bindings (integrate-agent-channels D8): set only for
	// channel runs; non-channel runs leave them zero and never resolve the
	// channel tools. ChannelFeed posts into the room, ChannelContext reads
	// the feed back, ChannelHandles resolves author handles for output.
	ChannelID      string
	ChannelContext ChannelContext
	ChannelFeed    ChannelFeed
	ChannelHandles ChannelHandles

	// IsChannelSession marks a run executing inside a team room (the
	// document-visibility scope's channel half, add-reference-documents D7):
	// channel-attached documents are visible only to these runs. Run assembly
	// sets it alongside ChannelID — deliberately separate, so a scheduled run
	// that knows its delivery channel still resolves visibility by agent
	// bindings only (the delivery target never widens scope).
	IsChannelSession bool

	// Work-session bindings (channel-teams D2/D5): set only for a channel
	// run minted inside an active work session. ChannelRole is the running
	// agent's roster role; session.close resolves only for facilitators.
	WorkSessionID string
	ChannelRole   domain.ChannelMemberRole
	WorkSessions  WorkSessions
}

// DocumentTools is the run-side port over the workspace's reference-document
// library (add-reference-documents D4/D7): visibility-filtered section search
// for document.search and visible-document listing for the compose-side
// manifest and references mount. The references service implements it;
// internal/references imports internal/agents/tools, so this aggregate port
// lives in the agents package and satisfies the tool-side search seam
// (tools.DocumentSearcher) structurally — never the reverse. Nil on runners
// without the references service wired.
type DocumentTools interface {
	SearchDocuments(ctx context.Context, workspaceID string, scope domain.DocumentRunScope, query string, limit int) ([]domain.DocumentSectionHit, error)
	VisibleDocuments(ctx context.Context, workspaceID string, scope domain.DocumentRunScope) ([]domain.ReferenceDocument, error)
}

// The runner-side port carries the tool-side search seam — a drift breaks
// this build, not search (the DocumentPublisher pattern).
var _ tools.DocumentSearcher = DocumentTools(nil)

// ToolConstructor builds a tool.BaseTool for a specific execution context.
// Implementations are registered by dotted name (e.g. "web.search") and
// resolved at agent composition time. Eino types are confined to this package
// and the composer boundary (Config.Tools); callers outside internal/agents
// never reference tool.BaseTool directly.
type ToolConstructor func(ToolContext) (tool.BaseTool, error)

// ToolRegistry is the in-process tool surface. The turn's effective tool set
// (denylist resolution: catalog minus disabled_tools) selects from it at
// composition time; unknown names are inert — there are no DB rows to
// validate against.
type ToolRegistry interface {
	// Register adds a tool under its dotted name. The name must be unique
	// within the registry; re-registering the same name replaces it.
	Register(name string, ctor ToolConstructor)

	// Lookup returns the tool for a name, or (nil, false) when absent.
	Lookup(name string) (ToolConstructor, bool)

	// Names returns all registered tool names (sorted).
	Names() []string

	// Filter returns a ToolFilter bound to this registry that filters the
	// resolved effective set down to registered names.
	Filter() ToolFilter
}

// ToolFilter is a domain-level view of the tools available to a given agent
// after intersecting the effective tool set with the registry. It is consumed
// by the engine to build the agent's tool set.
type ToolFilter interface {
	// Available returns the registered tool names the agent's effective set
	// carries, in a stable order (sorted by name). Names that are not
	// registered are silently ignored (inert).
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
	// schedule carries the optional schedule tool's dependencies (set only by
	// WithSchedulerTools); nil means the schedule tool is not registered.
	schedule *scheduleToolDeps
	// memorySearch carries the optional memory.search tool's searcher (set
	// only by WithMemorySearch); nil means the tool is not registered.
	memorySearch *memory.Searcher
	// todos carries the optional todo tools' store (set only by
	// WithTodoTools); nil means the todo tools are not registered.
	todos *todoToolDeps
	// documents carries the optional document.search port (set only by
	// WithDocumentTools); nil means the tool is not registered.
	documents DocumentTools
}

// scheduleToolDeps bundles the stores the schedule tool needs at construction
// time; both are required whenever the tool is registered.
type scheduleToolDeps struct {
	schedulers store.SchedulerStore
	members    tools.ScheduleChannelMembers
}

// todoToolDeps carries the store the todo tools need at construction time;
// required whenever they are registered.
type todoToolDeps struct {
	todos store.TodoStore
}

// ToolRegistryOption configures optional built-in registrations on the default
// registry.
type ToolRegistryOption func(*toolRegistry)

// WithSchedulerTools registers the schedule tool (integrate-scheduler 6.1)
// backed by the workspace's scheduler store and the channel-membership source
// its channel-target rule consults. Unset, the schedule tool is simply not
// registered — the deployment surfaces no schedule tool at all, not a broken
// one.
func WithSchedulerTools(schedulers store.SchedulerStore, members tools.ScheduleChannelMembers) ToolRegistryOption {
	return func(r *toolRegistry) {
		r.schedule = &scheduleToolDeps{schedulers: schedulers, members: members}
	}
}

// WithMemorySearch registers the memory.search tool (integrate-agent-zero-
// memory 4.3) backed by the scope-filtered Searcher over the extracted
// stores. Unset, the tool is simply not registered — the deployment surfaces
// no memory.search at all, not a broken one (the schedule-tool precedent).
func WithMemorySearch(searcher *memory.Searcher) ToolRegistryOption {
	return func(r *toolRegistry) {
		r.memorySearch = searcher
	}
}

// WithTodoTools registers the todo_write and todo_read tools
// (adopt-assistant-ui-elements D5) backed by the per-session todo store.
// Unset, the tools are simply not registered — the deployment surfaces no
// todo tools at all, not broken ones (the schedule-tool precedent).
func WithTodoTools(todos store.TodoStore) ToolRegistryOption {
	return func(r *toolRegistry) {
		r.todos = &todoToolDeps{todos: todos}
	}
}

// WithDocumentTools registers the document.search tool
// (add-reference-documents 4.1) backed by the workspace's reference-document
// library port — visibility-filtered section search bound to the run's scope
// at construction. Unset, the tool is simply not registered — the deployment
// surfaces no document.search at all, not a broken one (the memory-search
// precedent). document.read deliberately does not depend on it: reading a
// jailed file never requires the library.
func WithDocumentTools(documents DocumentTools) ToolRegistryOption {
	return func(r *toolRegistry) {
		r.documents = documents
	}
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
func NewDefaultToolRegistry(memories store.MemoryStore, opts ...ToolRegistryOption) ToolRegistry {
	reg := NewToolRegistry()
	reg.browser = tools.NewBrowserManager()
	for _, opt := range opts {
		opt(reg)
	}

	// web.search (fix-tool-config-error-degrades-to-tool-result D1): the
	// provider chain resolves lazily through the resolver on first invocation,
	// so the registration is infallible — a workspace with no usable provider
	// entries still builds the tool, and the not-configured / unknown-provider
	// / missing-credential errors surface as error results on the tool call
	// while the run completes (design.md D4).
	reg.Register(tools.Name, func(tctx ToolContext) (tool.BaseTool, error) {
		return tools.NewWebSearch(tools.WithSearchProviderResolver(func() (tools.SearchProvider, error) {
			return searchProviderFor(tctx)
		}))
	})

	reg.Register(tools.NameWebFetch, func(ToolContext) (tool.BaseTool, error) {
		return tools.NewWebFetch()
	})

	reg.Register(tools.NameMemory, func(tctx ToolContext) (tool.BaseTool, error) {
		return tools.NewMemory(memories, tctx.WorkspaceID, tctx.UserID)
	})

	// Memory search (integrate-agent-zero-memory 4.3): the read-only search
	// over the extracted stores, registered only when the composition root
	// wired the searcher. Identity binds per construction through the
	// ToolContext — the query arguments carry no identity fields.
	if s := reg.memorySearch; s != nil {
		reg.Register(tools.NameMemorySearch, func(tctx ToolContext) (tool.BaseTool, error) {
			return tools.NewMemorySearch(s, tctx.WorkspaceID, tctx.UserID, tctx.AgentID)
		})
	}

	// Schedule tool (integrate-scheduler 6.1): an ordinary registration, but
	// optional at the registry level — it registers only when wired with the
	// scheduler store and membership source, so the tool exists exactly where
	// the scheduler does. Scheduler-origin runs strip it by name regardless
	// (runner's schedulerExcludedTools).
	if d := reg.schedule; d != nil {
		reg.Register(tools.NameSchedule, func(tctx ToolContext) (tool.BaseTool, error) {
			return tools.NewSchedule(d.schedulers, d.members, tctx.WorkspaceID, tctx.AgentID, tctx.UserID, tctx.WorkspaceTZ)
		})
	}

	// Todo tools (adopt-assistant-ui-elements D5): write and read the
	// session's durable plan, registered only when wired with the todo store
	// (the schedule-tool precedent). Identity binds per construction through
	// the ToolContext — the arguments carry no session fields.
	if d := reg.todos; d != nil {
		reg.Register(tools.NameTodoWrite, func(tctx ToolContext) (tool.BaseTool, error) {
			return tools.NewTodoWrite(d.todos, tctx.WorkspaceID, tctx.AgentID, tctx.SessionID)
		})
		reg.Register(tools.NameTodoRead, func(tctx ToolContext) (tool.BaseTool, error) {
			return tools.NewTodoRead(d.todos, tctx.WorkspaceID, tctx.AgentID, tctx.SessionID)
		})
	}

	// Generative-UI cards (markdown-card-elements D1): the echo tools
	// (ui.chart/ui.timeline/ui.preview) are deleted — the model renders cards
	// by writing tagged code fences in its replies, taught by the injected
	// base prompt's rich-cards section. No tool round trip.

	reg.Register(tools.NameDeleteFile, func(tctx ToolContext) (tool.BaseTool, error) {
		return tools.NewDeleteFile(tctx.AgentDir)
	})

	// Document tools (add-document-read-tool / add-document-create-tool):
	// the document.* family registers behind the same seam — per-verb
	// constructors bound to the run's ToolContext. Both resolve paths through
	// the jailed workspace plus the run's read-only roots (the skills tree
	// and the turn's drop-lane mount), so chat-attached documents and
	// templates are addressable. document.create's delivery rides the
	// workspace blob store's publisher; an unwired publisher omits the
	// capability URL from results (the create tool's documented contract).
	reg.Register(tools.NameDocumentRead, func(tctx ToolContext) (tool.BaseTool, error) {
		return tools.NewDocumentRead(tctx.AgentDir, tools.WithReadOnlyRoots(tctx.ReadOnlyRoots...))
	})
	reg.Register(tools.NameDocumentCreate, func(tctx ToolContext) (tool.BaseTool, error) {
		return tools.NewDocumentCreate(tctx.AgentDir, tctx.DocumentPublisher, rodPDFRenderer{},
			tools.WithWorkspaceID(tctx.WorkspaceID),
			tools.WithDocumentCreateReadOnlyRoots(tctx.ReadOnlyRoots...))
	})

	// Document search (add-reference-documents 4.1): visibility-filtered
	// full-text search over the workspace's indexed reference documents,
	// registered only when the composition root wired the references service
	// (the memory-search precedent). Identity and visibility scope bind per
	// construction through the ToolContext — the query arguments carry no
	// identity fields. The scope mirrors domain.DocumentRunScope: scheduled
	// runs resolve by agent bindings only (IsChannelSession false), so a
	// delivery channel never widens what a run can search.
	if d := reg.documents; d != nil {
		reg.Register(tools.NameDocumentSearch, func(tctx ToolContext) (tool.BaseTool, error) {
			return tools.NewDocumentSearch(d, tctx.WorkspaceID, domain.DocumentRunScope{
				AgentID:          tctx.AgentID,
				ChannelID:        tctx.ChannelID,
				IsChannelSession: tctx.IsChannelSession,
			})
		})
	}

	// Channel tools (integrate-agent-channels task 5): registry tools so
	// pre_tool_use hooks target them by name, bound per run through the
	// ToolContext channel bindings. Exposure is decided at resolution —
	// channel runs un-scope them from the denylist (or append them to a
	// per-turn override), non-channel runs strip them — so the constructors
	// below only ever build against wired channel state and fail explicitly
	// otherwise.
	reg.Register(ChannelToolPost, newChannelPostTool)
	reg.Register(ChannelToolHistory, newChannelHistoryTool)

	// Session tools (channel-teams task 4): the facilitator's session.close,
	// registered like the channel tools so pre_tool_use hooks target it by
	// name. Exposure is decided at resolution — session runs append it for
	// the facilitator only, every other run strips it — so the constructor
	// below only ever builds against wired session state and fails
	// explicitly otherwise.
	reg.Register(SessionToolClose, newSessionCloseTool)

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

// rodPDFRenderer renders agent-authored HTML to PDF through headless
// Chromium (add-document-create-tool design.md D4), mirroring the browser
// tool's launch pattern (tools/browser_rod.go): a fresh local launcher per
// render with a temp user-data-dir, closed when the render ends. Renderer
// failures — Chrome absent above all — propagate as errors; the create tool
// converts them into structured per-document results. Each render is bounded
// by a deadline so a hung page cannot stall the run.
type rodPDFRenderer struct{}

// documentPDFRenderTimeout bounds one HTML→PDF render: browser launch, page
// load, and print.
const documentPDFRenderTimeout = 60 * time.Second

func (rodPDFRenderer) RenderHTMLToPDF(ctx context.Context, html string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, documentPDFRenderTimeout)
	defer cancel()

	l := launcher.New().
		UserDataDir(filepath.Join(os.TempDir(), "onclaw-pdf-render", fmt.Sprintf("%x", time.Now().UnixNano()))).
		Headless(true)
	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("launch chromium: %w", err)
	}

	browser := rod.New().ControlURL(u).Context(ctx)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("connect to local chromium: %w", err)
	}
	defer browser.Close()

	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("open render page: %w", err)
	}
	page = page.Context(ctx)
	if err := page.SetDocumentContent(html); err != nil {
		return nil, fmt.Errorf("load html: %w", err)
	}
	stream, err := page.PDF(&proto.PagePrintToPDF{})
	if err != nil {
		return nil, fmt.Errorf("print to pdf: %w", err)
	}
	defer stream.Close()

	pdf, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("read pdf output: %w", err)
	}
	return pdf, nil
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

// errWebSearchNotConfigured is the invocation-time error for a workspace with
// no usable search provider entries
// (fix-tool-config-error-degrades-to-tool-result design.md D1/D4): explicit
// and actionable, raised through the lazy resolver before any network attempt,
// with no credential-free fallback. web.search always builds; the error
// surfaces as an error result on the tool call while the run completes.
var errWebSearchNotConfigured = errors.New("web.search is not configured — add a provider in Settings → Tools")

// webSearchWindow is the positional failover window: the first N entries in
// list order serve; entries below it never do (design.md D2).
const webSearchWindow = 3

// searchProviderFor builds the per-execution web.search backend from the
// workspace's resolved tool config: each entry's provider is constructed with
// a per-attempt HTTP client bounded by request_timeout_seconds (design.md D7)
// and the first webSearchWindow entries are chained as the failover window
// (design.md D2). Config arrives decrypted with instance env already merged
// by the settings service; with no entries there is no fallback — the
// resolver returns the explicit not-configured error, which surfaces at first
// invocation as an error result (design.md D4).
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

// Filter returns a ToolFilter bound to this registry that intersects an
// effective tool set with the registered names.
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
// registry and the turn's effective tool set (allowlist-shaped: the resolved
// denylist output, or a per-turn override). An empty set selects no tools;
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
