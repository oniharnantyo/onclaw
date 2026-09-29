package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

type stubTool struct{}

func (s *stubTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "stub"}, nil
}

func (s *stubTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "ok", nil
}

func fakeToolCtor(ToolContext) (tool.BaseTool, error) { return &stubTool{}, nil }

// fakeToolCtorErr simulates a constructor failure.
func fakeToolCtorErr(ToolContext) (tool.BaseTool, error) { return nil, errors.New("boom") }

func TestToolRegistry_RegisterLookupList(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register("web.search", fakeToolCtor)
	reg.Register("fs.read", fakeToolCtor)

	if len(reg.Names()) != 2 {
		t.Errorf("expected 2 names, got %d: %v", len(reg.Names()), reg.Names())
	}

	ctor, ok := reg.Lookup("web.search")
	if !ok {
		t.Fatal("expected web.search to be registered")
	}
	if ctor == nil {
		t.Fatal("expected non-nil ctor")
	}

	if _, ok := reg.Lookup("nonexistent"); ok {
		t.Error("expected lookup miss for nonexistent")
	}
}

func TestToolRegistry_FilterAllowlist(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register("web.search", fakeToolCtor)
	reg.Register("fs.read", fakeToolCtor)
	reg.Register("fs.write", fakeToolCtor)

	// Empty allowlist → none available.
	avail := reg.Filter().Available(nil)
	if len(avail) != 0 {
		t.Errorf("expected 0 available with empty allowlist, got %d", len(avail))
	}

	// Allow two → exactly those, sorted.
	avail = reg.Filter().Available([]string{"fs.write", "web.search"})
	if len(avail) != 2 || avail[0] != "fs.write" || avail[1] != "web.search" {
		t.Errorf("expected [fs.write web.search], got %v", avail)
	}

	// Unknown allowed name → inert.
	avail = reg.Filter().Available([]string{"web.search", "nonexistent.tool"})
	if len(avail) != 1 || avail[0] != "web.search" {
		t.Errorf("unknown allowed name should be inert; expected [web.search], got %v", avail)
	}

	// A removed generative-UI echo tool name is inert like any unknown name
	// (markdown-card-elements 3.6): stored agent configs still carrying the
	// deleted keys resolve nothing for them.
	avail = reg.Filter().Available([]string{"web.search", "ui.chart"})
	if len(avail) != 1 || avail[0] != "web.search" {
		t.Errorf("stale ui.chart allowlist key should be inert; expected [web.search], got %v", avail)
	}
}

// TestNewDefaultToolRegistry_RemovedEchoToolsInert pins the echo-tool removal
// end to end (markdown-card-elements D1, task 3.6): ui.chart/ui.timeline/
// ui.preview are no longer registered in the default registry, and a stored
// allowlist naming them resolves only the surviving tools — silently, never
// an error — through create/update/execute.
func TestNewDefaultToolRegistry_RemovedEchoToolsInert(t *testing.T) {
	st := fake.New()
	reg := NewDefaultToolRegistry(st.Memories())

	for _, name := range []string{"ui.chart", "ui.timeline", "ui.preview"} {
		if _, ok := reg.Lookup(name); ok {
			t.Fatalf("removed echo tool %s must not be registered", name)
		}
	}

	tctx := ToolContext{WorkspaceSlug: "acme", AgentSlug: "atlas", AgentDir: t.TempDir(), SessionID: "s1"}
	names, built, err := ResolvedTools(tctx, reg, []string{tools.NameWebFetch, "ui.chart", "ui.timeline", "ui.preview"})
	if err != nil {
		t.Fatalf("ResolvedTools with stale echo keys must not error: %v", err)
	}
	if len(names) != 1 || names[0] != tools.NameWebFetch || len(built) != 1 {
		t.Fatalf("stale echo keys must silently drop, got names=%v built=%d", names, len(built))
	}
}

func TestResolvedTools_BuildsAndFilters(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register("web.search", fakeToolCtor)
	reg.Register("fs.read", fakeToolCtor)

	tctx := ToolContext{WorkspaceSlug: "acme", AgentSlug: "atlas", AgentDir: "/tmp/atlas", SessionID: "s1"}

	// Empty allowlist → no tools.
	names, built, err := ResolvedTools(tctx, reg, nil)
	if err != nil {
		t.Fatalf("ResolvedTools: %v", err)
	}
	if len(names) != 0 || len(built) != 0 {
		t.Errorf("expected no tools for empty allowlist, got names=%v built=%d", names, len(built))
	}

	names, built, err = ResolvedTools(tctx, reg, []string{"fs.read"})
	if err != nil {
		t.Fatalf("ResolvedTools: %v", err)
	}
	if len(names) != 1 || names[0] != "fs.read" {
		t.Errorf("expected [fs.read], got %v", names)
	}
	if len(built) != 1 {
		t.Errorf("expected 1 built tool, got %d", len(built))
	}
}

func TestResolvedTools_CtorError(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register("bad", fakeToolCtorErr)

	_, _, err := ResolvedTools(ToolContext{}, reg, []string{"bad"})
	if err == nil {
		t.Fatal("expected error when ctor fails, got nil")
	}
}

func TestNewDefaultToolRegistry_MemoryAndDeleteFileRegistered(t *testing.T) {
	reg := NewDefaultToolRegistry(nil)

	for _, name := range []string{tools.NameMemory, tools.NameDeleteFile} {
		if _, ok := reg.Lookup(name); !ok {
			t.Errorf("expected %q to be registered", name)
		}
	}

	// Catalog entries mirror the registrations and stay non-configurable.
	memory, ok := ToolCatalogEntryByKey(tools.NameMemory)
	if !ok {
		t.Fatal("expected a memory catalog entry")
	}
	if memory.Group != "memory" || memory.IconKey != "memory" || memory.Configurable {
		t.Errorf("unexpected memory catalog entry: %+v", memory)
	}
	del, ok := ToolCatalogEntryByKey(tools.NameDeleteFile)
	if !ok {
		t.Fatal("expected a delete_file catalog entry")
	}
	if del.Group != "filesystem" || del.IconKey != "trash" || del.Configurable {
		t.Errorf("unexpected delete_file catalog entry: %+v", del)
	}
}

// stubDocumentPublisher is a fixed DocumentPublisher: a deterministic
// capability URL per name, verifying the source file exists.
type stubDocumentPublisher struct{}

func (stubDocumentPublisher) PublishCreatedDocument(_ context.Context, _, name, sourcePath string) (string, error) {
	if _, err := os.Stat(sourcePath); err != nil {
		return "", fmt.Errorf("created document %q not found", sourcePath)
	}
	return "/api/v1/files/doc-cap/" + name, nil
}

// TestNewDefaultToolRegistry_DocumentTools pins the document.* family's
// registration contract (add-document-read-tool / add-document-create-tool):
// both tools register in the default registry, document.read builds against a
// real workspace directory with the run's read-only roots, and
// document.create binds the workspace blob store's publisher plus the
// run's read-only roots for chat-delivered templates.
func TestNewDefaultToolRegistry_DocumentTools(t *testing.T) {
	reg := NewDefaultToolRegistry(nil)

	for _, name := range []string{tools.NameDocumentRead, tools.NameDocumentCreate} {
		if _, ok := reg.Lookup(name); !ok {
			t.Errorf("expected %q to be registered", name)
		}
	}

	agentDir := t.TempDir()

	// document.read builds against the jail root plus read-only roots.
	ctor, ok := reg.Lookup(tools.NameDocumentRead)
	if !ok {
		t.Fatal("expected document.read registration")
	}
	dropRoot := t.TempDir()
	tl, err := ctor(ToolContext{AgentDir: agentDir, ReadOnlyRoots: []string{dropRoot}})
	if err != nil {
		t.Fatalf("document.read constructor: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatalf("document.read info: %v", err)
	}
	if info.Name != tools.NameDocumentRead {
		t.Errorf("tool name = %q, want %q", info.Name, tools.NameDocumentRead)
	}
	// A missing workspace directory self-heals at construction (design.md D5):
	// MkdirAll creates the jail root; only a non-directory or unwritable path
	// still fails.
	missing := filepath.Join(agentDir, "missing")
	if _, err := ctor(ToolContext{AgentDir: missing}); err != nil {
		t.Errorf("document.read must self-heal a missing agent dir: %v", err)
	}
	if info, err := os.Stat(missing); err != nil || !info.IsDir() {
		t.Errorf("expected the missing agent dir to be created, got %+v (err=%v)", info, err)
	}

	// document.create builds against the jail root; the publisher and the
	// rod HTML→PDF renderer are bound per construction. An unwired publisher
	// still constructs — the create tool's documented contract omits the
	// capability URL from results when delivery is unwired.
	ctor, ok = reg.Lookup(tools.NameDocumentCreate)
	if !ok {
		t.Fatal("expected document.create registration")
	}
	tl, err = ctor(ToolContext{AgentDir: agentDir})
	if err != nil {
		t.Fatalf("document.create constructor without publisher: %v", err)
	}
	tl, err = ctor(ToolContext{AgentDir: agentDir, WorkspaceID: "ws", DocumentPublisher: stubDocumentPublisher{}})
	if err != nil {
		t.Fatalf("document.create constructor: %v", err)
	}
	info, err = tl.Info(context.Background())
	if err != nil {
		t.Fatalf("document.create info: %v", err)
	}
	if info.Name != tools.NameDocumentCreate {
		t.Errorf("tool name = %q, want %q", info.Name, tools.NameDocumentCreate)
	}

	// Catalog cards: group document, per-verb icons, non-configurable.
	read, ok := ToolCatalogEntryByKey(tools.NameDocumentRead)
	if !ok {
		t.Fatal("expected a document.read catalog entry")
	}
	if read.Group != "document" || read.IconKey != "file-text" || read.DisplayName != "Read Document" || read.Configurable {
		t.Errorf("unexpected document.read catalog entry: %+v", read)
	}
	create, ok := ToolCatalogEntryByKey(tools.NameDocumentCreate)
	if !ok {
		t.Fatal("expected a document.create catalog entry")
	}
	if create.Group != "document" || create.IconKey != "file-plus" || create.DisplayName != "Create Document" || create.Configurable {
		t.Errorf("unexpected document.create catalog entry: %+v", create)
	}
}

// TestNewDefaultToolRegistry_ScheduleOptional pins the schedule tool's
// registration contract (integrate-scheduler 6.1): WithSchedulerTools registers
// it and its catalog entry exists; without the option it is simply absent —
// never a broken registration.
func TestNewDefaultToolRegistry_ScheduleOptional(t *testing.T) {
	bare := NewDefaultToolRegistry(nil)
	if _, ok := bare.Lookup(tools.NameSchedule); ok {
		t.Fatal("the schedule tool must not register without WithSchedulerTools")
	}

	st := fake.New()
	reg := NewDefaultToolRegistry(st.Memories(), WithSchedulerTools(st.Schedulers(), st.Channels()))
	ctor, ok := reg.Lookup(tools.NameSchedule)
	if !ok {
		t.Fatal("WithSchedulerTools must register the schedule tool")
	}

	// The constructor builds against the wired stores and binds the run's
	// identity (a construction failure fails tool resolution).
	tl, err := ctor(ToolContext{WorkspaceID: "ws", AgentID: "ag", UserID: "user", WorkspaceTZ: time.UTC})
	if err != nil {
		t.Fatalf("schedule constructor: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}
	if info.Name != tools.NameSchedule {
		t.Errorf("tool name = %q, want %q", info.Name, tools.NameSchedule)
	}

	// Catalog card: non-configurable, calendar icon (the web Icon map's key).
	entry, ok := ToolCatalogEntryByKey(tools.NameSchedule)
	if !ok {
		t.Fatal("expected a schedule catalog entry")
	}
	if entry.DisplayName != "Schedule" || entry.IconKey != "calendar" || entry.Configurable {
		t.Errorf("unexpected schedule catalog entry: %+v", entry)
	}
}

// fakeDocumentTools is a scripted DocumentTools recording document.search's
// construction-bound identity and scope at call time.
type fakeDocumentTools struct {
	calls []fakeDocumentSearchCall
	hits  []domain.DocumentSectionHit
}

type fakeDocumentSearchCall struct {
	workspaceID string
	scope       domain.DocumentRunScope
	query       string
	limit       int
}

func (f *fakeDocumentTools) SearchDocuments(_ context.Context, workspaceID string, scope domain.DocumentRunScope, query string, limit int) ([]domain.DocumentSectionHit, error) {
	f.calls = append(f.calls, fakeDocumentSearchCall{workspaceID: workspaceID, scope: scope, query: query, limit: limit})
	return f.hits, nil
}

func (f *fakeDocumentTools) VisibleDocuments(context.Context, string, domain.DocumentRunScope) ([]domain.ReferenceDocument, error) {
	return nil, nil
}

// TestNewDefaultToolRegistry_DocumentSearchOptional pins document.search's
// registration contract (add-reference-documents 4.1): the references
// service is optional — WithDocumentTools registers the tool and nothing
// registers without it (the memory-search precedent) — while document.read
// stays registered either way (reading a jailed file never requires the
// library).
func TestNewDefaultToolRegistry_DocumentSearchOptional(t *testing.T) {
	bare := NewDefaultToolRegistry(nil)
	if _, ok := bare.Lookup(tools.NameDocumentSearch); ok {
		t.Fatal("document.search must not register without WithDocumentTools")
	}
	if _, ok := bare.Lookup(tools.NameDocumentRead); !ok {
		t.Fatal("document.read must register even without WithDocumentTools")
	}
	if slices.Contains(bare.Names(), tools.NameDocumentSearch) {
		t.Error("unwired registry must not offer document.search to denylist resolution")
	}

	reg := NewDefaultToolRegistry(nil, WithDocumentTools(&fakeDocumentTools{}))
	ctor, ok := reg.Lookup(tools.NameDocumentSearch)
	if !ok {
		t.Fatal("WithDocumentTools must register document.search")
	}

	// The constructor builds and binds the run's identity and visibility
	// scope; the query arguments carry no identity fields.
	tl, err := ctor(ToolContext{
		WorkspaceID:      "ws-1",
		AgentID:          "ag-1",
		ChannelID:        "ch-9",
		IsChannelSession: true,
	})
	if err != nil {
		t.Fatalf("document.search constructor: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}
	if info.Name != tools.NameDocumentSearch {
		t.Errorf("tool name = %q, want %q", info.Name, tools.NameDocumentSearch)
	}

	fake := &fakeDocumentTools{}
	reg2 := NewDefaultToolRegistry(nil, WithDocumentTools(fake))
	ctor2, ok := reg2.Lookup(tools.NameDocumentSearch)
	if !ok {
		t.Fatal("expected document.search registration")
	}
	tl2, err := ctor2(ToolContext{WorkspaceID: "ws-2", AgentID: "ag-2"})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	if _, err := tl2.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":" rate limits "}`); err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("searcher calls = %d, want 1", len(fake.calls))
	}
	call := fake.calls[0]
	if call.workspaceID != "ws-2" || call.query != "rate limits" || call.limit != 8 {
		t.Errorf("call = %+v, want ws-2 / trimmed query / limit 8", call)
	}
	// A direct (non-channel) run: no channel, not a channel session.
	if call.scope.AgentID != "ag-2" || call.scope.ChannelID != "" || call.scope.IsChannelSession {
		t.Errorf("scope = %+v, want the direct-run scope", call.scope)
	}
}

// TestNewDefaultToolRegistry_DocumentSearchChannelScope pins the channel half
// of the visibility binding: a channel session run binds ChannelID and
// IsChannelSession onto the searcher's DocumentRunScope, while the scheduled
// shape (channel known, IsChannelSession false) resolves by agent bindings
// only — the delivery target never widens the searchable set.
func TestNewDefaultToolRegistry_DocumentSearchChannelScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		tctx ToolContext
		want domain.DocumentRunScope
	}{
		{
			name: "channel session",
			tctx: ToolContext{WorkspaceID: "ws", AgentID: "ag", ChannelID: "ch-1", IsChannelSession: true},
			want: domain.DocumentRunScope{AgentID: "ag", ChannelID: "ch-1", IsChannelSession: true},
		},
		{
			name: "scheduled run with delivery channel",
			tctx: ToolContext{WorkspaceID: "ws", AgentID: "ag", ChannelID: "ch-1"},
			want: domain.DocumentRunScope{AgentID: "ag", ChannelID: "ch-1", IsChannelSession: false},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeDocumentTools{}
			reg := NewDefaultToolRegistry(nil, WithDocumentTools(fake))
			ctor, ok := reg.Lookup(tools.NameDocumentSearch)
			if !ok {
				t.Fatal("expected document.search registration")
			}
			tl, err := ctor(tc.tctx)
			if err != nil {
				t.Fatalf("constructor: %v", err)
			}
			if _, err := tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":"x"}`); err != nil {
				t.Fatalf("InvokableRun: %v", err)
			}
			if fake.calls[0].scope != tc.want {
				t.Errorf("scope = %+v, want %+v", fake.calls[0].scope, tc.want)
			}
		})
	}
}

// webSearchToolConfig builds a decrypted, env-merged web.search runtime
// config the way the settings service hands it to the registry.
func webSearchToolConfig(entries ...map[string]any) map[string]map[string]any {
	list := make([]any, 0, len(entries))
	for _, e := range entries {
		list = append(list, e)
	}
	return map[string]map[string]any{
		tools.Name: {"entries": list},
	}
}

func TestSearchProviderFor_NotConfigured(t *testing.T) {
	want := "web.search is not configured — add a provider in Settings → Tools"

	// No config at all.
	_, err := searchProviderFor(ToolContext{})
	if err == nil || err.Error() != want {
		t.Fatalf("expected %q, got %v", want, err)
	}

	// Explicitly empty stack.
	tctx := ToolContext{ToolConfigs: map[string]map[string]any{
		tools.Name: {"entries": []any{}, "request_timeout_seconds": 10},
	}}
	_, err = searchProviderFor(tctx)
	if err == nil || err.Error() != want {
		t.Fatalf("expected %q, got %v", want, err)
	}
}

func TestSearchProviderFor_BuildsChainFromEntries(t *testing.T) {
	tctx := ToolContext{ToolConfigs: webSearchToolConfig(
		map[string]any{"id": "a1b2c3d4", "name": "Tavily 1", "provider": "tavily", "api_key": "k1"},
		map[string]any{"id": "b2c3d4e5", "name": "SearXNG", "provider": "searxng", "base_url": "http://searxng:8080"},
		map[string]any{"id": "c3d4e5f6", "name": "Exa 1", "provider": "exa", "api_key": "k3"},
	)}
	provider, err := searchProviderFor(tctx)
	if err != nil {
		t.Fatalf("chain construction: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil chain provider")
	}
}

func TestSearchProviderFor_MissingCredentialFailsResolution(t *testing.T) {
	// Cannot pass settings validation, but the resolver must still fail
	// loudly instead of building a dead provider.
	tctx := ToolContext{ToolConfigs: webSearchToolConfig(
		map[string]any{"id": "a1b2c3d4", "name": "Tavily 1", "provider": "tavily"},
	)}
	_, err := searchProviderFor(tctx)
	if err == nil || !strings.Contains(err.Error(), "requires an API key") {
		t.Fatalf("expected missing-credential resolution error, got %v", err)
	}
}

// TestWebSearchResolution_DegradesToInvocationError pins the lazy web.search
// resolution end to end (fix-tool-config-error-degrades-to-tool-result 1.3):
// an unconfigured workspace resolves the tool successfully through the
// registry, and the not-configured error surfaces on first invocation as the
// middleware-contract error result naming web.search; a configured workspace
// still resolves its provider chain at construction.
func TestWebSearchResolution_DegradesToInvocationError(t *testing.T) {
	reg := NewDefaultToolRegistry(nil)
	ctor, ok := reg.Lookup(tools.Name)
	if !ok {
		t.Fatal("expected web.search registration")
	}

	// Unconfigured workspace: construction succeeds; the not-configured error
	// surfaces on first invocation (the runtime's error-result middleware
	// renders it as the JSON the model reads).
	tl, err := ctor(ToolContext{})
	if err != nil {
		t.Fatalf("unconfigured workspace must still build web.search: %v", err)
	}
	it, ok := tl.(tool.InvokableTool)
	if !ok {
		t.Fatal("webSearchTool must implement tool.InvokableTool")
	}
	_, err = it.InvokableRun(context.Background(), `{"query":"test"}`)
	if want := "web.search is not configured — add a provider in Settings → Tools"; err == nil || err.Error() != want {
		t.Fatalf("expected not-configured invocation error %q, got %v", want, err)
	}
	payload := errorResultPayload(err, tools.Name)
	if !strings.Contains(payload, `"error"`) || !strings.Contains(payload, `"tool":"web.search"`) {
		t.Errorf("expected middleware-contract error result naming web.search, got %s", payload)
	}

	// Configured workspace: the chain resolves at construction. Not invoked —
	// that would attempt a network call.
	configured, err := ctor(ToolContext{ToolConfigs: webSearchToolConfig(
		map[string]any{"id": "a1b2c3d4", "name": "Tavily 1", "provider": "tavily", "api_key": "k1"},
	)})
	if err != nil {
		t.Fatalf("configured workspace must build web.search through its chain: %v", err)
	}
	if configured == nil {
		t.Fatal("expected non-nil tool for configured workspace")
	}
}

func TestWebSearchAttemptTimeout(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]any
		want   time.Duration
	}{
		{"absent", nil, 10 * time.Second},
		{"empty value", map[string]any{"request_timeout_seconds": nil}, 10 * time.Second},
		{"configured", map[string]any{"request_timeout_seconds": 5}, 5 * time.Second},
		{"zero falls back to default", map[string]any{"request_timeout_seconds": 0}, 10 * time.Second},
		{"negative falls back to default", map[string]any{"request_timeout_seconds": -3}, 10 * time.Second},
		{"over max clamps", map[string]any{"request_timeout_seconds": 120}, 60 * time.Second},
		{"non-numeric falls back to default", map[string]any{"request_timeout_seconds": "soon"}, 10 * time.Second},
	}
	for _, tc := range cases {
		if got := webSearchAttemptTimeout(tc.config); got != tc.want {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}
