//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// seedMemoryFixtures creates a workspace, user, and agent for memory tests,
// returning their IDs.
func seedMemoryFixtures(t *testing.T, s store.Store, wsSlug, userEmail, agentSlug string) (wsID, userID, agentID string) {
	t.Helper()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: wsSlug, Name: "Mem WS " + wsSlug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace: %v", err)
	}
	u := &domain.User{Email: userEmail, Name: "Mem User"}
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatalf("unexpected create user: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "P", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: agentSlug, Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("unexpected create agent: %v", err)
	}
	return ws.ID, u.ID, a.ID
}

func TestIntegration_MemoryStore_UserMemory(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	ws1, u1, _ := seedMemoryFixtures(t, s, "pg-mem-user-ws1", "pg-mem-u1@example.com", "atlas")
	ws2, u2, _ := seedMemoryFixtures(t, s, "pg-mem-user-ws2", "pg-mem-u2@example.com", "atlas")
	mem := s.Memories()

	// Get-absent returns (nil, nil), not a sentinel.
	got, err := mem.UserMemory(ctx, ws1, u1)
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for absent user memory, got (%v, %v)", got, err)
	}

	// Upsert stores content.
	if err := mem.UpsertUserMemory(ctx, ws1, u1, "User works on backend microservices."); err != nil {
		t.Fatalf("unexpected UpsertUserMemory error: %v", err)
	}
	got, err = mem.UserMemory(ctx, ws1, u1)
	if err != nil || got == nil || got.Content != "User works on backend microservices." {
		t.Fatalf("expected stored content, got (%v, %v)", got, err)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}

	// Upsert REPLACES content (HTTP PUT semantics).
	if err := mem.UpsertUserMemory(ctx, ws1, u1, "User works on fullstack features."); err != nil {
		t.Fatalf("unexpected second UpsertUserMemory error: %v", err)
	}
	got, _ = mem.UserMemory(ctx, ws1, u1)
	if got.Content != "User works on fullstack features." {
		t.Fatalf("expected replaced content, got %q", got.Content)
	}

	// Scope isolation: another user and another workspace see nothing.
	got, _ = mem.UserMemory(ctx, ws1, u2)
	if got != nil {
		t.Fatalf("expected nil memory for other user, got %q", got.Content)
	}
	got, _ = mem.UserMemory(ctx, ws2, u1)
	if got != nil {
		t.Fatalf("expected nil memory for other workspace, got %q", got.Content)
	}

	// Append to a nonexistent row creates the document with the fragment.
	if err := mem.AppendUserMemory(ctx, ws2, u2, "Seeded fragment."); err != nil {
		t.Fatalf("unexpected AppendUserMemory on absent row: %v", err)
	}

	// Two appends land in order in one document.
	if err := mem.AppendUserMemory(ctx, ws1, u1, " Prefers Go."); err != nil {
		t.Fatalf("unexpected AppendUserMemory error: %v", err)
	}
	if err := mem.AppendUserMemory(ctx, ws1, u1, " Uses UTC timestamps."); err != nil {
		t.Fatalf("unexpected second AppendUserMemory error: %v", err)
	}
	got, _ = mem.UserMemory(ctx, ws1, u1)
	if got.Content != "User works on fullstack features. Prefers Go. Uses UTC timestamps." {
		t.Fatalf("expected ordered concatenation, got %q", got.Content)
	}

	// Cap rejection on Upsert (sentinel error).
	over := strings.Repeat("a", domain.MaxMemoryContentChars+1)
	if err := mem.UpsertUserMemory(ctx, ws1, u1, over); !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded on over-cap upsert, got %v", err)
	}

	// Cap rejection on Append: fragment under cap, resulting doc over cap
	// (in-statement guard).
	if err := mem.UpsertUserMemory(ctx, ws2, u2, strings.Repeat("b", domain.MaxMemoryContentChars-5)); err != nil {
		t.Fatalf("unexpected fill upsert error: %v", err)
	}
	if err := mem.AppendUserMemory(ctx, ws2, u2, strings.Repeat("c", 10)); !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded on over-cap append, got %v", err)
	}

	// Stored memory is unchanged after rejected writes.
	got, _ = mem.UserMemory(ctx, ws2, u2)
	if len(got.Content) != domain.MaxMemoryContentChars-5 {
		t.Fatalf("expected content unchanged after rejected writes, got %d chars", len(got.Content))
	}
}

func TestIntegration_MemoryStore_WorkspaceMemory(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	ws1, _, _ := seedMemoryFixtures(t, s, "pg-mem-ws-ws1", "pg-mem-ws1@example.com", "atlas")
	ws2, _, _ := seedMemoryFixtures(t, s, "pg-mem-ws-ws2", "pg-mem-ws2@example.com", "atlas")
	mem := s.Memories()

	// A fresh workspace has the empty default column value, not absence.
	got, err := mem.WorkspaceMemory(ctx, ws1)
	if err != nil || got == nil || got.Content != "" {
		t.Fatalf("expected empty workspace memory, got (%v, %v)", got, err)
	}

	// Upsert stores content.
	if err := mem.UpsertWorkspaceMemory(ctx, ws1, "Deploy freeze on Fridays."); err != nil {
		t.Fatalf("unexpected UpsertWorkspaceMemory error: %v", err)
	}
	got, err = mem.WorkspaceMemory(ctx, ws1)
	if err != nil || got == nil || got.Content != "Deploy freeze on Fridays." {
		t.Fatalf("expected stored content, got (%v, %v)", got, err)
	}

	// Scope isolation between workspaces.
	got, _ = mem.WorkspaceMemory(ctx, ws2)
	if got == nil || got.Content != "" {
		t.Fatalf("expected empty memory for other workspace, got %v", got)
	}

	// Two appends land in order in one document (column starts from '').
	if err := mem.AppendWorkspaceMemory(ctx, ws1, " On-call rota rotated weekly."); err != nil {
		t.Fatalf("unexpected AppendWorkspaceMemory error: %v", err)
	}
	if err := mem.AppendWorkspaceMemory(ctx, ws1, " Staging redeployed nightly."); err != nil {
		t.Fatalf("unexpected second AppendWorkspaceMemory error: %v", err)
	}
	got, _ = mem.WorkspaceMemory(ctx, ws1)
	if got.Content != "Deploy freeze on Fridays. On-call rota rotated weekly. Staging redeployed nightly." {
		t.Fatalf("expected ordered concatenation, got %q", got.Content)
	}

	// Cap rejection on Upsert and Append (sentinel error).
	over := strings.Repeat("a", domain.MaxMemoryContentChars+1)
	if err := mem.UpsertWorkspaceMemory(ctx, ws1, over); !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded on over-cap upsert, got %v", err)
	}
	if err := mem.AppendWorkspaceMemory(ctx, ws1, over); !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded on over-cap append, got %v", err)
	}

	// Workspace memory does not clobber other workspace fields.
	w, err := s.Workspaces().ByID(ctx, ws1)
	if err != nil {
		t.Fatalf("unexpected workspace read error: %v", err)
	}
	if w.Name != "Mem WS pg-mem-ws-ws1" || w.Slug != "pg-mem-ws-ws1" {
		t.Fatalf("expected workspace fields untouched, got name=%q slug=%q", w.Name, w.Slug)
	}

	// A settings-style update via the workspace store leaves memory intact.
	w.Description = "Updated by settings save"
	if err := s.Workspaces().Update(ctx, w); err != nil {
		t.Fatalf("unexpected workspace update error: %v", err)
	}
	got, _ = mem.WorkspaceMemory(ctx, ws1)
	if got.Content != "Deploy freeze on Fridays. On-call rota rotated weekly. Staging redeployed nightly." {
		t.Fatalf("expected memory intact after settings save, got %q", got.Content)
	}

	// Unknown workspace returns ErrNotFound.
	if err := mem.UpsertWorkspaceMemory(ctx, "00000000-0000-0000-0000-000000000000", "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}
}

func TestIntegration_MemoryStore_AgentDailyMemory(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	ws1, _, a1 := seedMemoryFixtures(t, s, "pg-mem-daily-ws1", "pg-mem-d1@example.com", "atlas")
	ws2, _, a2 := seedMemoryFixtures(t, s, "pg-mem-daily-ws2", "pg-mem-d2@example.com", "beacon")
	mem := s.Memories()

	day := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	nextDay := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)

	// Get-absent returns (nil, nil).
	got, err := mem.AgentDailyMemory(ctx, ws1, a1, day)
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for absent daily memory, got (%v, %v)", got, err)
	}

	// Two appends land in order in the single document for that day.
	if err := mem.AppendAgentDailyMemory(ctx, ws1, a1, day, "Morning: triaged incidents."); err != nil {
		t.Fatalf("unexpected AppendAgentDailyMemory error: %v", err)
	}
	if err := mem.AppendAgentDailyMemory(ctx, ws1, a1, day, " Afternoon: wrote postmortem."); err != nil {
		t.Fatalf("unexpected second AppendAgentDailyMemory error: %v", err)
	}
	got, err = mem.AgentDailyMemory(ctx, ws1, a1, day)
	if err != nil || got == nil {
		t.Fatalf("expected daily memory, got (%v, %v)", got, err)
	}
	if got.Content != "Morning: triaged incidents. Afternoon: wrote postmortem." {
		t.Fatalf("expected ordered concatenation, got %q", got.Content)
	}

	// Upsert REPLACES the day's document in place (one doc per day).
	if err := mem.UpsertAgentDailyMemory(ctx, ws1, a1, day, "Rewritten summary."); err != nil {
		t.Fatalf("unexpected UpsertAgentDailyMemory error: %v", err)
	}
	got, _ = mem.AgentDailyMemory(ctx, ws1, a1, day)
	if got.Content != "Rewritten summary." {
		t.Fatalf("expected replaced daily content, got %q", got.Content)
	}

	// A different date is a separate document.
	got, _ = mem.AgentDailyMemory(ctx, ws1, a1, nextDay)
	if got != nil {
		t.Fatalf("expected nil memory for other day, got %q", got.Content)
	}

	// Scope isolation: other agent and other workspace see nothing.
	got, _ = mem.AgentDailyMemory(ctx, ws1, a2, day)
	if got != nil {
		t.Fatalf("expected nil memory for other agent, got %q", got.Content)
	}
	got, _ = mem.AgentDailyMemory(ctx, ws2, a1, day)
	if got != nil {
		t.Fatalf("expected nil memory for other workspace, got %q", got.Content)
	}

	// Data-layer hardening: a daily row addressed across workspaces (right
	// agent, wrong workspace) is unwritable via the composite FK.
	if err := mem.AppendAgentDailyMemory(ctx, ws1, a2, day, "cross-tenant"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-workspace daily append, got %v", err)
	}

	// Cap rejection on Upsert and Append (sentinel error).
	over := strings.Repeat("a", domain.MaxMemoryContentChars+1)
	if err := mem.UpsertAgentDailyMemory(ctx, ws1, a1, day, over); !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded on over-cap upsert, got %v", err)
	}
	if err := mem.UpsertAgentDailyMemory(ctx, ws2, a2, day, strings.Repeat("b", domain.MaxMemoryContentChars-5)); err != nil {
		t.Fatalf("unexpected fill upsert error: %v", err)
	}
	if err := mem.AppendAgentDailyMemory(ctx, ws2, a2, day, strings.Repeat("c", 10)); !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded on over-cap append, got %v", err)
	}

	// Daily memories die with the agent (ON DELETE CASCADE).
	if err := s.Agents().Delete(ctx, ws1, a1); err != nil {
		t.Fatalf("unexpected Delete agent error: %v", err)
	}
	got, _ = mem.AgentDailyMemory(ctx, ws1, a1, day)
	if got != nil {
		t.Fatalf("expected daily memory to cascade-delete with agent, got %q", got.Content)
	}
}
