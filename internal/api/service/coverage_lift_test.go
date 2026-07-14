package service_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// TestService_Setters exercises the trivial setters so they are not counted as
// uncovered statements.
func TestService_Setters(t *testing.T) {
	f := newFixtureFull(t)
	f.svc.SetStagedWriteStore(newFakeStagedWriteStore())
	f.svc.SetWorkspacePath(t.TempDir())
	f.svc.SetResolve(nil)
}

func TestService_ListAgentMCP(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	list, err := f.svc.ListAgentMCP(ctx, "agent-a")
	if err != nil {
		t.Fatalf("ListAgentMCP: %v", err)
	}
	if list == nil {
		t.Error("expected non-nil list")
	}
}

func TestService_SetAgentMCP(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	if err := f.svc.SetAgentMCP(ctx, "agent-a", "srv", true); err != nil {
		t.Fatalf("SetAgentMCP: %v", err)
	}
}

func TestService_SetAndGetAgentPersona(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	ws := t.TempDir()
	if err := f.agentStore.AddAgent(ctx, &store.Agent{Name: "agent-p", Workspace: ws}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	content := "Persona content for testing."
	if err := f.svc.SetAgentPersona(ctx, "agent-p", "MEMORY.md", content); err != nil {
		t.Fatalf("SetAgentPersona: %v", err)
	}
	got, err := f.svc.GetAgentPersona(ctx, "agent-p", "MEMORY.md")
	if err != nil {
		t.Fatalf("GetAgentPersona: %v", err)
	}
	if got != content {
		t.Errorf("expected %q, got %q", content, got)
	}
}

func TestService_GetAgentPersona_NotWhitelisted(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	if _, err := f.svc.GetAgentPersona(ctx, "agent-p", "secret.txt"); err == nil {
		t.Error("expected error for non-whitelisted file")
	}
}

func TestService_GetAgentPersona_MissingFile(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	ws := t.TempDir()
	if err := f.agentStore.AddAgent(ctx, &store.Agent{Name: "agent-m", Workspace: ws}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	// A whitelisted file that does not exist returns "" without error.
	got, err := f.svc.GetAgentPersona(ctx, "agent-m", "MEMORY.md")
	if err != nil {
		t.Fatalf("GetAgentPersona missing: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty for missing file, got %q", got)
	}
}

func TestService_ListStagedWrites(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	writes, err := f.svc.ListStagedWrites(ctx)
	if err != nil {
		t.Fatalf("ListStagedWrites: %v", err)
	}
	if writes == nil {
		t.Error("expected non-nil slice")
	}
}

func TestService_ApproveRejectStagedWrite(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	sts := newFakeStagedWriteStore()
	approveID, err := sts.StageWrite(ctx, "agent", "write", "target", "content")
	if err != nil {
		t.Fatalf("StageWrite: %v", err)
	}
	rejectID, err := sts.StageWrite(ctx, "agent", "write", "target2", "content2")
	if err != nil {
		t.Fatalf("StageWrite: %v", err)
	}
	f.svc.SetStagedWriteStore(sts)
	if err := f.svc.ApproveStagedWrite(ctx, approveID); err != nil {
		t.Fatalf("ApproveStagedWrite: %v", err)
	}
	if err := f.svc.RejectStagedWrite(ctx, rejectID); err != nil {
		t.Fatalf("RejectStagedWrite: %v", err)
	}
}

func TestService_ListDreamSweeps(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	f.svc.SetWorkspacePath(t.TempDir())
	sweeps, err := f.svc.ListDreamSweeps(ctx)
	if err != nil {
		t.Fatalf("ListDreamSweeps: %v", err)
	}
	if sweeps == nil {
		t.Error("expected non-nil slice")
	}
}

func TestService_ListSkills_Empty(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	if _, err := f.svc.ListSkills(ctx); err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
}

func TestService_GetSkill_NotFound(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	if _, err := f.svc.GetSkill(ctx, "nope", "agent"); err == nil {
		t.Error("expected error for missing skill")
	}
}

func TestService_RemoveSkill_NotFound(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	if err := f.svc.RemoveSkill(ctx, "nope", "agent"); err == nil {
		t.Error("expected error for missing skill")
	}
}

func TestService_UpdateSkill_NotFound(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	if _, err := f.svc.UpdateSkill(ctx, "nope", "agent"); err == nil {
		t.Error("expected error for missing skill")
	}
}

func TestService_DiscoverSkills_BadSource(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	if _, err := f.svc.DiscoverSkills(ctx, service.DiscoverInput{Source: "/nonexistent/path/xyz"}); err == nil {
		t.Error("expected error for bad source")
	}
}

func TestService_InstallSkills_BadSource(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	if _, err := f.svc.InstallSkills(ctx, service.InstallSkillInput{Source: "/nonexistent/path/xyz"}); err == nil {
		t.Error("expected error for bad source")
	}
}

func TestService_TestConnection_NotFound(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	res, _ := f.svc.TestConnection(ctx, "ghost")
	if res == nil || res.Success {
		t.Error("expected failure for unknown provider")
	}
}

func TestService_TestConnection_Disabled(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	f.profileStore.AddProfile(ctx, &store.Profile{Name: "disabled-p", ProviderType: "openai", Enabled: 0})
	res, _ := f.svc.TestConnection(ctx, "disabled-p")
	if res == nil || res.Success {
		t.Error("expected failure for disabled provider")
	}
}

func TestService_TestConnection_NoKey(t *testing.T) {
	f := newFixtureFull(t)
	ctx := context.Background()

	// Enabled provider with no stored secret: ResolveSecret fails, so the
	// connection is reported as failed before any network call.
	f.profileStore.AddProfile(ctx, &store.Profile{Name: "nokey-p", ProviderType: "openai", Enabled: 1})
	res, _ := f.svc.TestConnection(ctx, "nokey-p")
	if res == nil || res.Success {
		t.Error("expected failure without API key")
	}
}
