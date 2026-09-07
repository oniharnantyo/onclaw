package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
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
