//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// sessionSeed builds a workspace, provider, agent, and two users (both
// members) through the store and returns the ids the agent-session tests
// need.
func sessionSeed(t *testing.T, ctx context.Context, s store.Store, slug string) (workspaceID, agentID, userAID, userBID string) {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := s.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	userA := &domain.User{Email: slug + "-alice@example.com", Name: "Alice"}
	if err := s.Users().Create(ctx, userA); err != nil {
		t.Fatalf("create user A: %v", err)
	}
	userB := &domain.User{Email: slug + "-bob@example.com", Name: "Bob"}
	if err := s.Users().Create(ctx, userB); err != nil {
		t.Fatalf("create user B: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "member-" + slug}
	if err := s.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := s.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: userA.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("add member A: %v", err)
	}
	if err := s.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: userB.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("add member B: %v", err)
	}
	agent := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas-" + slug, Name: "Atlas", ProviderID: prov.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return ws.ID, agent.ID, userA.ID, userB.ID
}

// TestIntegration_AgentSessionStore_Schema covers migration 000041
// (agent-session-index design D1): the agent_sessions table exists with the
// unique (workspace, agent, session) triple, the title column defaults to the
// empty string, and the user-listing index is in place.
func TestIntegration_AgentSessionStore_Schema(t *testing.T) {
	_, schemaDSN, ctx := setupTestSchema(t)

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "agent_sessions").Scan(&exists); err != nil {
		t.Fatalf("failed to probe agent_sessions: %v", err)
	}
	if !exists {
		t.Fatal("expected agent_sessions table to exist after migrations")
	}

	var idx bool
	if err := conn.QueryRow(ctx,
		`SELECT to_regclass($1) IS NOT NULL`, "idx_agent_sessions_user_listing",
	).Scan(&idx); err != nil {
		t.Fatalf("failed to probe listing index: %v", err)
	}
	if !idx {
		t.Error("expected idx_agent_sessions_user_listing to exist")
	}

	var uniqueTriple bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conrelid = 'agent_sessions'::regclass
			  AND contype = 'u'
			  AND pg_get_constraintdef(oid) LIKE '%workspace_id%agent_id%session_id%'
		)`,
	).Scan(&uniqueTriple); err != nil {
		t.Fatalf("failed to probe unique constraint: %v", err)
	}
	if !uniqueTriple {
		t.Error("expected a UNIQUE (workspace_id, agent_id, session_id) constraint")
	}

	var titleDefault string
	if err := conn.QueryRow(ctx,
		`SELECT column_default FROM information_schema.columns
		 WHERE table_name = 'agent_sessions' AND column_name = 'title'`,
	).Scan(&titleDefault); err != nil {
		t.Fatalf("failed to read title column default: %v", err)
	}
	if titleDefault != "''::text" {
		t.Errorf("title default = %q, want ''::text", titleDefault)
	}
}

// TestIntegration_AgentSessionStore_BirthTurnInsertsTitle covers the birth
// scenario (agent-session-index spec "First turn births the index row").
func TestIntegration_AgentSessionStore_BirthTurnInsertsTitle(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID, userAID, _ := sessionSeed(t, ctx, s, "sess-birth")

	before := time.Now().UTC()
	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userAID, domain.AgentSessionUpsert{SessionID: "sess_1", Title: "Fix the login bug"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	list, err := s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userAID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 session after birth, got %d", len(list))
	}
	row := list[0]
	if row.SessionID != "sess_1" || row.Title != "Fix the login bug" {
		t.Errorf("unexpected row: %+v", row)
	}
	if row.CreatedAt.Before(before) || row.LastActiveAt.Before(before) {
		t.Errorf("birth timestamps %v/%v predate the turn %v", row.CreatedAt, row.LastActiveAt, before)
	}
	if row.DeletedAt != nil {
		t.Errorf("deleted_at = %v, want nil on birth", row.DeletedAt)
	}
	if row.ID == "" || row.WorkspaceID != wsID || row.AgentID != agentID || row.UserID != userAID {
		t.Errorf("row scope not populated: %+v", row)
	}
}

// TestIntegration_AgentSessionStore_LaterTurnBumpsWithoutRetitle covers the
// spec "Later turns bump activity without retitling": last_active_at
// advances, the birth title survives a different derived title, and the
// bumped session orders first.
func TestIntegration_AgentSessionStore_LaterTurnBumpsWithoutRetitle(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID, userAID, _ := sessionSeed(t, ctx, s, "sess-bump")

	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userAID, domain.AgentSessionUpsert{SessionID: "sess_old", Title: "Old topic"}); err != nil {
		t.Fatalf("upsert old: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userAID, domain.AgentSessionUpsert{SessionID: "sess_new", Title: "New topic"}); err != nil {
		t.Fatalf("upsert new: %v", err)
	}

	first, err := s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userAID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(first) != 2 || first[0].SessionID != "sess_new" || first[1].SessionID != "sess_old" {
		t.Fatalf("expected newest-first [sess_new sess_old], got %+v", first)
	}
	bumped := first[1]

	// A later turn on the old session passes a DIFFERENT derived title —
	// activity bumps, the title does not change.
	time.Sleep(10 * time.Millisecond)
	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userAID, domain.AgentSessionUpsert{SessionID: "sess_old", Title: "Retitle attempt"}); err != nil {
		t.Fatalf("re-upsert old: %v", err)
	}

	list, err := s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userAID)
	if err != nil {
		t.Fatalf("list after bump: %v", err)
	}
	if len(list) != 2 || list[0].SessionID != "sess_old" {
		t.Fatalf("expected bumped sess_old to order first, got %+v", list)
	}
	if !list[0].LastActiveAt.After(bumped.LastActiveAt) {
		t.Errorf("last_active_at = %v, want a bump past %v", list[0].LastActiveAt, bumped.LastActiveAt)
	}
	if list[0].Title != "Old topic" {
		t.Errorf("title = %q, want the birth title to survive a later turn", list[0].Title)
	}
}

// TestIntegration_AgentSessionStore_EmptyTitleNeverOverwrites covers the
// "Compact turn does not title" scenario against the real D2 CASE guard.
func TestIntegration_AgentSessionStore_EmptyTitleNeverOverwrites(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID, userAID, _ := sessionSeed(t, ctx, s, "sess-empty-title")

	// Birth with empty title (compaction turn is first indexed contact).
	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userAID, domain.AgentSessionUpsert{SessionID: "sess_c"}); err != nil {
		t.Fatalf("upsert empty-title birth: %v", err)
	}
	list, err := s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userAID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Title != "" {
		t.Fatalf("expected one untitled row, got %+v", list)
	}

	// A later titled turn fills the still-empty title (birth rule applies
	// while stored title is '').
	time.Sleep(10 * time.Millisecond)
	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userAID, domain.AgentSessionUpsert{SessionID: "sess_c", Title: "Derived once"}); err != nil {
		t.Fatalf("upsert titled turn: %v", err)
	}
	// Then an empty-title turn must not clear it.
	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userAID, domain.AgentSessionUpsert{SessionID: "sess_c"}); err != nil {
		t.Fatalf("upsert empty-title turn: %v", err)
	}

	list, err = s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userAID)
	if err != nil {
		t.Fatalf("relist: %v", err)
	}
	if len(list) != 1 || list[0].Title != "Derived once" {
		t.Fatalf("expected title %q to survive empty-title turns, got %+v", "Derived once", list)
	}
}

// TestIntegration_AgentSessionStore_SoftDeleteHidesThenRevives covers "Soft
// delete hides but preserves" plus design D2's revive rule (deleted_at = NULL
// on conflict).
func TestIntegration_AgentSessionStore_SoftDeleteHidesThenRevives(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID, userAID, _ := sessionSeed(t, ctx, s, "sess-softdel")

	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userAID, domain.AgentSessionUpsert{SessionID: "sess_d", Title: "Doomed"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.AgentSessions().SoftDeleteAgentSession(ctx, wsID, agentID, userAID, "sess_d"); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	list, err := s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userAID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected soft-deleted session to be hidden, got %+v", list)
	}

	// Re-chatting revives the row: deleted_at clears, the row reappears.
	time.Sleep(10 * time.Millisecond)
	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userAID, domain.AgentSessionUpsert{SessionID: "sess_d"}); err != nil {
		t.Fatalf("reviving upsert: %v", err)
	}
	list, err = s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userAID)
	if err != nil {
		t.Fatalf("list after revive: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected revived session to reappear, got %d rows", len(list))
	}
	if list[0].DeletedAt != nil {
		t.Errorf("deleted_at = %v, want nil after revive", list[0].DeletedAt)
	}
	if list[0].Title != "Doomed" {
		t.Errorf("title = %q, want the original birth title preserved across delete/revive", list[0].Title)
	}
}

// TestIntegration_AgentSessionStore_ListingIsPrivatePerUser covers "Listing
// is private per user": user B sees nothing of user A, and re-upserting A's
// session id as B does not steal ownership (the DO UPDATE never rewrites
// user_id).
func TestIntegration_AgentSessionStore_ListingIsPrivatePerUser(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID, userAID, userBID := sessionSeed(t, ctx, s, "sess-private")

	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userAID, domain.AgentSessionUpsert{SessionID: "sess_a", Title: "A's chat"}); err != nil {
		t.Fatalf("upsert A: %v", err)
	}

	bList, err := s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userBID)
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(bList) != 0 {
		t.Fatalf("user B must see nothing of user A, got %+v", bList)
	}

	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userBID, domain.AgentSessionUpsert{SessionID: "sess_a"}); err != nil {
		t.Fatalf("B upsert same session id: %v", err)
	}
	aList, err := s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userAID)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(aList) != 1 || aList[0].UserID != userAID {
		t.Fatalf("A must still own exactly their row, got %+v", aList)
	}
	bList, err = s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userBID)
	if err != nil {
		t.Fatalf("relist B: %v", err)
	}
	if len(bList) != 0 {
		t.Fatalf("B still must not see the row (ownership fixed at birth), got %+v", bList)
	}
}

// TestIntegration_AgentSessionStore_SoftDeleteForeignOrAbsentNotFound covers
// the privacy boundary: foreign-owned and absent rows are indistinguishable.
func TestIntegration_AgentSessionStore_SoftDeleteForeignOrAbsentNotFound(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID, userAID, userBID := sessionSeed(t, ctx, s, "sess-del-404")

	if err := s.AgentSessions().UpsertAgentSession(ctx, wsID, agentID, userAID, domain.AgentSessionUpsert{SessionID: "sess_a", Title: "A's chat"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := s.AgentSessions().SoftDeleteAgentSession(ctx, wsID, agentID, userBID, "sess_a"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign-user delete: expected ErrNotFound, got %v", err)
	}
	if err := s.AgentSessions().SoftDeleteAgentSession(ctx, wsID, agentID, userAID, "sess_missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("absent-session delete: expected ErrNotFound, got %v", err)
	}

	// A's row must be untouched by the failed attempts.
	list, err := s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userAID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].DeletedAt != nil {
		t.Fatalf("failed deletes must not touch the row, got %+v", list)
	}
}

// TestIntegration_AgentSessionStore_EmptyListIsEmptySlice pins the port
// contract: an empty result is an empty slice, not nil.
func TestIntegration_AgentSessionStore_EmptyListIsEmptySlice(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID, userAID, _ := sessionSeed(t, ctx, s, "sess-empty")

	list, err := s.AgentSessions().ListAgentSessions(ctx, wsID, agentID, userAID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(list) != 0 {
		t.Fatalf("expected no rows, got %+v", list)
	}
}

// TestIntegration_AgentSessionStore_UpsertUnknownReferencesNotFound pins the
// FK behavior: unknown workspace / agent / user references surface as
// domain.ErrNotFound through the store's error translation.
func TestIntegration_AgentSessionStore_UpsertUnknownReferencesNotFound(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID, userAID, _ := sessionSeed(t, ctx, s, "sess-fk")

	st := s.AgentSessions()
	if err := st.UpsertAgentSession(ctx, "00000000-0000-0000-0000-000000000000", agentID, userAID, domain.AgentSessionUpsert{SessionID: "s"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown workspace: expected ErrNotFound, got %v", err)
	}
	if err := st.UpsertAgentSession(ctx, wsID, "00000000-0000-0000-0000-000000000000", userAID, domain.AgentSessionUpsert{SessionID: "s"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown agent: expected ErrNotFound, got %v", err)
	}
	if err := st.UpsertAgentSession(ctx, wsID, agentID, "00000000-0000-0000-0000-000000000000", domain.AgentSessionUpsert{SessionID: "s"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown user: expected ErrNotFound, got %v", err)
	}
}
