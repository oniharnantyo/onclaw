package agents

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/agents/tools"
)

// The built-in tool catalog: the tools every agent turn may draw on — the
// two workspace-free utilities, the jailed workspace filesystem and shell,
// the todo plan pair, web access, the browser, and the scheduler (spec
// agent-tools; spec add-browser-tool; spec add-scheduler). A tool reaches
// the model only when all three gate conditions hold
// (resolveTurnTools in gate.go): it is in the catalog, it is globally enabled
// (store.ToolStates — absence of a row means enabled), and the agent opted in
// (store.Agent.Tools — an empty list means "all catalog tools"). The
// workspace-scoped tools additionally need a rootable workspace and the
// scheduler a schedule service — withheld, not a failure. Eino types stay
// inside the package (doc.go); the settings surface sees only ToolMeta.

// ToolCurrentTime reports the current time in UTC and the local zone.
const ToolCurrentTime = "current_time"

// ToolHTTPGet fetches a URL over HTTP(S) with strict size and timeout caps.
const ToolHTTPGet = "http_get"

// The filesystem middleware's tool names, and the jailed shell's execute
// tool. These are registered by the fs middleware (compose.go), so the gate
// offers or withholds them through per-tool Disable flags and the Shell
// backend rather than through ToolsConfig.
const (
	ToolLs        = "ls"
	ToolReadFile  = "read_file"
	ToolWriteFile = "write_file"
	ToolEditFile  = "edit_file"
	ToolGlob      = "glob"
	ToolGrep      = "grep"
	ToolExecute   = "execute"
)

// filesystemToolNames are the fs middleware's tools in stable order; the gate
// computes the withheld set from this list (gate.go).
var filesystemToolNames = []string{ToolLs, ToolReadFile, ToolWriteFile, ToolEditFile, ToolGlob, ToolGrep}

// The workspace-scoped delete tool, the todo plan pair, and the web tools.
// Names mirror the constructors in internal/agents/backend and
// internal/agents/tools.
const (
	ToolDeleteFile = backend.NameDeleteFile
	ToolTodoWrite  = tools.NameTodoWrite
	ToolTodoRead   = tools.NameTodoRead
	ToolWebFetch   = tools.NameWebFetch
	ToolWebSearch  = tools.NameWebSearch
	ToolBrowser    = tools.NameBrowser
)

// ToolScheduler creates, lists, updates and deletes cron schedules that run
// an agent on a prompt unattended (spec add-scheduler). Offered only when
// the runtime was built with a schedule service (WithScheduler).
const ToolScheduler = "scheduler"

// toolKind discriminates how a catalog entry reaches the model.
type toolKind int

const (
	// toolDirect tools are constructed here and passed to the agent's
	// ToolsConfig.
	toolDirect toolKind = iota
	// toolMiddleware tools are registered by the filesystem middleware; the
	// gate withholds them via per-tool Disable flags.
	toolMiddleware
	// toolShell is the filesystem middleware's execute tool; the gate offers
	// it by attaching the jailed Shell backend.
	toolShell
)

// catalogEntry is one catalog record: its display metadata plus how it is
// offered, and whether it must be rooted in the agent's workspace.
type catalogEntry struct {
	meta ToolMeta
	kind toolKind
	// workspaceScoped direct tools take the agent's workspace directory at
	// construction and cannot be offered without one.
	workspaceScoped bool
}

// catalogEntries is the tool catalog in stable display order — the only
// source of membership (spec agent-tools "Tool gate"). DisplayName and
// IconKey are display metadata for the settings surfaces; IconKey names a
// kit glyph (the frontend Icon map falls back to a neutral plug).
var catalogEntries = []catalogEntry{
	{meta: ToolMeta{Name: ToolCurrentTime, DisplayName: "Current Time", IconKey: "clock", Description: "Get the current date and time (UTC and local timezone)."}},
	{meta: ToolMeta{Name: ToolHTTPGet, DisplayName: "HTTP Get", IconKey: "external-link", Description: "Fetch a URL over HTTP(S) and return the response body as text, with strict size and timeout limits."}},
	{meta: ToolMeta{Name: ToolLs, DisplayName: "List Files", IconKey: "folder", Description: "List the files and directories under a path in your workspace."}, kind: toolMiddleware, workspaceScoped: true},
	{meta: ToolMeta{Name: ToolReadFile, DisplayName: "Read File", IconKey: "file", Description: "Read a file's content from your workspace, with line-based offset and limit."}, kind: toolMiddleware, workspaceScoped: true},
	{meta: ToolMeta{Name: ToolWriteFile, DisplayName: "Write File", IconKey: "file-plus", Description: "Create or overwrite a file in your workspace."}, kind: toolMiddleware, workspaceScoped: true},
	{meta: ToolMeta{Name: ToolEditFile, DisplayName: "Edit File", IconKey: "edit", Description: "Replace an exact string in an existing workspace file."}, kind: toolMiddleware, workspaceScoped: true},
	{meta: ToolMeta{Name: ToolGlob, DisplayName: "Glob", IconKey: "scan", Description: "Find files in your workspace matching a glob pattern."}, kind: toolMiddleware, workspaceScoped: true},
	{meta: ToolMeta{Name: ToolGrep, DisplayName: "Grep", IconKey: "compass", Description: "Search file contents with a regular expression across your workspace."}, kind: toolMiddleware, workspaceScoped: true},
	{meta: ToolMeta{Name: ToolExecute, DisplayName: "Shell", IconKey: "terminal", Description: "Run a shell command inside your workspace with a bounded timeout and output size."}, kind: toolShell, workspaceScoped: true},
	{meta: ToolMeta{Name: ToolDeleteFile, DisplayName: "Delete File", IconKey: "trash", Description: "Permanently delete a file inside your workspace."}, workspaceScoped: true},
	{meta: ToolMeta{Name: ToolTodoWrite, DisplayName: "Write Todos", IconKey: "list-checks", Description: "Replace your persisted task plan; the plan survives across turns in your workspace."}, workspaceScoped: true},
	{meta: ToolMeta{Name: ToolTodoRead, DisplayName: "Read Todos", IconKey: "list", Description: "Read your persisted task plan."}, workspaceScoped: true},
	{meta: ToolMeta{Name: ToolWebFetch, DisplayName: "Web Fetch", IconKey: "link", Description: "Fetch a URL over HTTP(S) and return its readable text content."}},
	{meta: ToolMeta{Name: ToolWebSearch, DisplayName: "Web Search", IconKey: "search", Description: "Search the web through the configured provider chain (configured in Tools settings)."}},
	{meta: ToolMeta{Name: ToolBrowser, DisplayName: "Browser", IconKey: "globe", Description: "Drive a real Chromium browser with persistent named profiles: navigate, snapshot, click, type, screenshot, manage cookies. Logins persist across sessions."}},
	{meta: ToolMeta{Name: ToolScheduler, DisplayName: "Scheduler", IconKey: "calendar-clock", Description: "Create, list, update and delete cron schedules that run an agent on a prompt unattended; times are local."}},
}

// ToolMeta is the plain-Go catalog record crossing the package boundary for
// settings surfaces (the Tools pane and agent tool opt-ins).
type ToolMeta struct {
	Name string
	// DisplayName is the Title Case label settings surfaces render; the
	// raw Name stays the gate/allowlist key.
	DisplayName string
	// IconKey names a frontend kit glyph; unknown keys fall back to a
	// neutral plug on the client.
	IconKey     string
	Description string
}

// CatalogTools returns the built-in tool catalog in stable display order.
// The result is never nil.
func CatalogTools() []ToolMeta {
	metas := make([]ToolMeta, 0, len(catalogEntries))
	for _, entry := range catalogEntries {
		metas = append(metas, entry.meta)
	}
	return metas
}

// CatalogToolByName returns the catalog entry for a tool name.
func CatalogToolByName(name string) (ToolMeta, bool) {
	for _, entry := range catalogEntries {
		if entry.meta.Name == name {
			return entry.meta, true
		}
	}
	return ToolMeta{}, false
}

// toolScope is what a direct tool may need from the turn it is constructed
// for (add-scheduler design D8): the calling agent and the workspace
// directory. Workspace-free tools ignore it.
type toolScope struct {
	// AgentID is the id of the agent whose turn is being composed — the
	// scheduler tool's `create` defaults its target agent to it.
	AgentID string
	// Workspace is the agent's workspace directory; "" for a turn without a
	// rootable workspace (workspace-free tools never read it).
	Workspace string
}

// buildDirectTool constructs one direct catalog tool for the turn described
// by scope. Workspace-scoped tools are rooted at scope.Workspace (empty only
// for the workspace-free ones, which ignore it); the scheduler tool is
// offered only when the runtime was built with a schedule service.
func (m *manager) buildDirectTool(name string, scope toolScope) (tool.BaseTool, error) {
	switch name {
	case ToolCurrentTime:
		return newCurrentTimeTool()
	case ToolHTTPGet:
		return newHTTPGetTool()
	case ToolDeleteFile:
		return backend.NewDeleteFile(scope.Workspace)
	case ToolTodoWrite:
		return tools.NewTodoWrite(scope.Workspace)
	case ToolTodoRead:
		return tools.NewTodoRead(scope.Workspace)
	case ToolWebFetch:
		// The guarded client: every hop's resolved IPs are validated, so
		// redirects cannot smuggle the fetch to a private target.
		return tools.NewWebFetch(tools.WithFetchHTTPClient(tools.NewFetchHTTPClient(false)))
	case ToolWebSearch:
		// The persisted provider chain resolves lazily on first invocation
		// (design D8); the tool_settings-backed adapter supplies it.
		return tools.NewWebSearch(tools.WithSearchConfigSource(&storeSearchConfigSource{settings: m.store.ToolSettings()}))
	case ToolBrowser:
		// The persisted browser config resolves lazily per launch (spec
		// add-browser-tool D9) through the tool_settings-backed adapter;
		// profiles and screenshots root at the layout's browser directory
		// (D10).
		return tools.NewBrowser(
			tools.WithBrowserConfigSource(&storeBrowserConfigSource{settings: m.store.ToolSettings()}),
			tools.WithBrowserDir(m.browserDir))
	case ToolScheduler:
		// A thin client over the runtime's schedule service (add-scheduler
		// design D1), defaulted to the calling agent (D8).
		return newSchedulerTool(m.scheduler, m.store.Agents(), scope.AgentID)
	default:
		return nil, fmt.Errorf("tool %s is not directly constructible", name)
	}
}

// newCurrentTimeTool builds the current_time invokable tool.
func newCurrentTimeTool() (tool.InvokableTool, error) {
	type input struct{}
	type output struct {
		UTC      string `json:"utc"`
		Local    string `json:"local"`
		Timezone string `json:"timezone"`
	}
	return utils.InferTool(ToolCurrentTime,
		"Get the current date and time (UTC and local timezone).",
		func(_ context.Context, _ input) (output, error) {
			now := time.Now()
			zone, _ := now.Zone()
			return output{
				UTC:      now.UTC().Format(time.RFC3339Nano),
				Local:    now.Format(time.RFC3339Nano),
				Timezone: zone,
			}, nil
		})
}

// httpGet limits: bounded responses never hang a turn or exhaust memory.
const (
	httpGetTimeout      = 10 * time.Second
	httpGetMaxSize      = 256 << 10 // 256 KiB of body text
	httpGetMaxRedirects = 3
)

// newHTTPGetTool builds the http_get invokable tool.
func newHTTPGetTool() (tool.InvokableTool, error) {
	type input struct {
		URL string `json:"url"`
	}
	type output struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
		// Truncated reports whether the body was cut at the size cap.
		Truncated bool `json:"truncated"`
	}
	return utils.InferTool(ToolHTTPGet,
		"Fetch a URL over HTTP(S) and return the response body as text, with strict size and timeout limits.",
		func(ctx context.Context, in input) (output, error) {
			if !strings.HasPrefix(in.URL, "http://") && !strings.HasPrefix(in.URL, "https://") {
				return output{}, fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", in.URL)
			}

			ctx, cancel := context.WithTimeout(ctx, httpGetTimeout)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
			if err != nil {
				return output{}, fmt.Errorf("build request: %w", err)
			}
			client := &http.Client{
				Timeout: httpGetTimeout,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					if len(via) >= httpGetMaxRedirects {
						return fmt.Errorf("stopped after %d redirects", httpGetMaxRedirects)
					}
					return nil
				},
			}

			resp, err := client.Do(req)
			if err != nil {
				return output{}, fmt.Errorf("get %s: %w", in.URL, err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, httpGetMaxSize+1))
			if err != nil {
				return output{}, fmt.Errorf("read body of %s: %w", in.URL, err)
			}
			truncated := false
			if len(body) > httpGetMaxSize {
				body = body[:httpGetMaxSize]
				truncated = true
			}
			return output{Status: resp.StatusCode, Body: string(body), Truncated: truncated}, nil
		})
}
