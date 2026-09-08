package agents

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/tools"
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

func TestSearchProviderFor_MissingCredentialFailsConstruction(t *testing.T) {
	// Cannot pass settings validation, but the resolver must still fail
	// loudly instead of building a dead provider.
	tctx := ToolContext{ToolConfigs: webSearchToolConfig(
		map[string]any{"id": "a1b2c3d4", "name": "Tavily 1", "provider": "tavily"},
	)}
	_, err := searchProviderFor(tctx)
	if err == nil || !strings.Contains(err.Error(), "requires an API key") {
		t.Fatalf("expected missing-credential construction error, got %v", err)
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
