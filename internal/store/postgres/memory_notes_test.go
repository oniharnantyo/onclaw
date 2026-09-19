//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// memoryStoresSchemaVersion is the migration that introduces the memory
// stores; beforeMemoryStoresVersion is the multi-bot gateways migration
// below it; consolidatorSchemaVersion is the evidence + reports wave the
// round-trip walk starts from. The shared latestSchemaVersion in
// postgres_test.go tracks the latest waves.
const (
	memoryStoresSchemaVersion = 54
	beforeMemoryStoresVersion = 53
	consolidatorSchemaVersion = 56
)

// seedMemoryScopesFixtures creates one workspace with two members and two
// agents so the visibility tiers can be exercised for cross-member and
// cross-agent exclusion.
func seedMemoryScopesFixtures(t *testing.T, s store.Store) (wsID, userA, userB, agentX, agentY string) {
	t.Helper()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: "pg-mem-scopes", Name: "PG Mem Scopes"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	uA := &domain.User{Email: "pg-member-a@example.com", Name: "Member A"}
	if err := s.Users().Create(ctx, uA); err != nil {
		t.Fatalf("create user A: %v", err)
	}
	uB := &domain.User{Email: "pg-member-b@example.com", Name: "Member B"}
	if err := s.Users().Create(ctx, uB); err != nil {
		t.Fatalf("create user B: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "P", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	aX := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, aX); err != nil {
		t.Fatalf("create agent X: %v", err)
	}
	aY := &domain.Agent{WorkspaceID: ws.ID, Slug: "beacon", Name: "Beacon", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, aY); err != nil {
		t.Fatalf("create agent Y: %v", err)
	}
	return ws.ID, uA.ID, uB.ID, aX.ID, aY.ID
}

// newPGMemoryNote builds a write-valid note owned per its tier.
func newPGMemoryNote(wsID string, visibility domain.MemoryVisibility, ownerID, content string) *domain.MemoryNote {
	n := &domain.MemoryNote{
		WorkspaceID:   wsID,
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
		LearnedAt:     time.Date(2026, 9, 15, 10, 0, 1, 0, time.UTC),
		SourceEventID: "evt_pg_1",
		Content:       content,
	}
	switch visibility {
	case domain.MemoryVisibilityUser:
		n.UserID = &ownerID
	case domain.MemoryVisibilityAgent:
		n.AgentID = &ownerID
	}
	return n
}

// newPGMemoryEvent builds a write-valid gist event owned per its tier.
func newPGMemoryEvent(wsID, agentID, sessionID, turnID string, visibility domain.MemoryVisibility, ownerID, description string) *domain.MemoryEvent {
	e := &domain.MemoryEvent{
		WorkspaceID:   wsID,
		AgentID:       agentID,
		SessionID:     sessionID,
		TurnID:        turnID,
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
		LearnedAt:     time.Date(2026, 9, 15, 10, 0, 1, 0, time.UTC),
		SourceEventID: "evt_pg_1",
		Description:   description,
	}
	if visibility == domain.MemoryVisibilityUser {
		e.UserID = &ownerID
	}
	return e
}

func TestIntegration_MemoryStores_MigrationRoundTrip(t *testing.T) {
	ctx := context.Background()
	baseDSN := getTestBaseDSN(t)

	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		t.Skipf("skipping integration test: database connection failed: %v", err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	schemaName := fmt.Sprintf("test_mem_%s", hex.EncodeToString(b))
	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}
	defer func() {
		cleanupConn, err := pgx.Connect(context.Background(), baseDSN)
		if err == nil {
			_, _ = cleanupConn.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
			_ = cleanupConn.Close(context.Background())
		}
	}()

	separator := "?"
	if strings.Contains(baseDSN, "?") {
		separator = "&"
	}
	schemaDSN := fmt.Sprintf("%s%ssearch_path=%s", baseDSN, separator, schemaName)
	mig := postgres.NewMigrator(schemaDSN)

	tableExists := func(target string) bool {
		t.Helper()
		var exists bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)`,
			schemaName, target).Scan(&exists); err != nil {
			t.Fatalf("table existence check: %v", err)
		}
		return exists
	}
	indexExists := func(target string) bool {
		t.Helper()
		var exists bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = $1 AND indexname = $2)`,
			schemaName, target).Scan(&exists); err != nil {
			t.Fatalf("index existence check: %v", err)
		}
		return exists
	}

	// Up: both tables, their lexical indexes (tsvector + trigram, D9), and
	// the session cursor index exist. Full-up lands on the latest migration
	// (000056, evidence + reports), past 000054 itself.
	if err := mig.Up(); err != nil {
		t.Fatalf("migration up: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != latestSchemaVersion || dirty {
		t.Fatalf("expected version %d clean after up, got v=%d dirty=%v err=%v", latestSchemaVersion, v, dirty, err)
	}
	if !tableExists("memory_events") || !tableExists("memory_notes") {
		t.Fatal("expected memory_events and memory_notes after up")
	}
	if tableExists("agent_daily_memories") {
		t.Fatal("expected agent_daily_memories dropped after up")
	}
	for _, idx := range []string{
		"idx_memory_events_workspace", "idx_memory_events_workspace_visibility", "idx_memory_events_session",
		"idx_memory_events_search", "idx_memory_events_trgm",
		"idx_memory_notes_workspace", "idx_memory_notes_workspace_visibility", "idx_memory_notes_workspace_topic",
		"idx_memory_notes_search", "idx_memory_notes_trgm",
	} {
		if !indexExists(idx) {
			t.Fatalf("expected index %s after up", idx)
		}
	}

	// Down past the consolidator wave: exactly the 000056 consolidator tables
	// disappear — the 000055 takeout is still applied at this depth. The step
	// count is version-relative so later migrations (000057+) don't shift the
	// walk; the following steps always land one version down.
	if err := mig.Down(latestSchemaVersion - consolidatorSchemaVersion + 1); err != nil {
		t.Fatalf("migration down to %d: %v", consolidatorSchemaVersion-1, err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != consolidatorSchemaVersion-1 || dirty {
		t.Fatalf("expected version %d clean after down, got v=%d dirty=%v err=%v", consolidatorSchemaVersion-1, v, dirty, err)
	}
	if tableExists("memory_note_evidence") || tableExists("memory_reports") {
		t.Fatal("expected the 000056 consolidator tables dropped by its down migration")
	}
	if !tableExists("memory_events") || !tableExists("memory_notes") {
		t.Fatal("expected memory tables still present after 000056 down")
	}
	if tableExists("agent_daily_memories") {
		t.Fatal("agent_daily_memories must stay dropped while 000055 is applied")
	}

	// Down a second step: the 000055 takeout reverses — the daily table is
	// recreated empty — while the 000054 memory stores stay applied.
	if err := mig.Down(1); err != nil {
		t.Fatalf("migration down 2: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != memoryStoresSchemaVersion || dirty {
		t.Fatalf("expected version %d clean after down, got v=%d dirty=%v err=%v", memoryStoresSchemaVersion, v, dirty, err)
	}
	if !tableExists("agent_daily_memories") {
		t.Fatal("expected agent_daily_memories recreated after 000055 down")
	}
	if !tableExists("memory_events") || !tableExists("memory_notes") {
		t.Fatal("expected memory tables still present after 000055 down")
	}

	// Down a third step: exactly the 000054 objects disappear.
	if err := mig.Down(1); err != nil {
		t.Fatalf("migration down 3: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != beforeMemoryStoresVersion || dirty {
		t.Fatalf("expected version %d clean after down, got v=%d dirty=%v err=%v", beforeMemoryStoresVersion, v, dirty, err)
	}
	if tableExists("memory_events") || tableExists("memory_notes") {
		t.Fatal("expected memory tables dropped after down")
	}
	if indexExists("idx_memory_notes_trgm") {
		t.Fatal("expected memory indexes dropped after down")
	}

	// Up again: the stores are recreated usable.
	if err := mig.Up(); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != latestSchemaVersion || dirty {
		t.Fatalf("expected version %d clean after re-up, got v=%d dirty=%v err=%v", latestSchemaVersion, v, dirty, err)
	}
	if !tableExists("memory_events") || !tableExists("memory_notes") {
		t.Fatal("expected memory tables recreated after re-up")
	}
}

func TestIntegration_MemoryStores_SchemaChecks(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)
	ws, userA, _, _, _ := seedMemoryScopesFixtures(t, s)

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("connect to schema: %v", err)
	}
	defer conn.Close(ctx)

	// The owner-shape CHECKs are enforced by the database, not only by the
	// domain validation in front of the store.
	if _, err := conn.Exec(ctx,
		`INSERT INTO memory_notes (workspace_id, visibility, user_id, origin, event_time, learned_at, source_event_id, content)
		 VALUES ($1, 'shared', $2, 'dialogue', now(), now(), 'evt_x', 'leak')`, ws, userA); err == nil {
		t.Fatal("expected shared-visibility note with user owner to violate chk_memory_notes_owner")
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO memory_events (workspace_id, agent_id, session_id, turn_id, visibility, user_id, origin, event_time, learned_at, source_event_id, description)
		 VALUES ($1, (SELECT id FROM agents WHERE workspace_id = $1 LIMIT 1), 's', 't', 'user', NULL, 'dialogue', now(), now(), 'evt_x', 'leak')`, ws); err == nil {
		t.Fatal("expected user-visibility event without owner to violate chk_memory_events_owner")
	}

	// Provenance columns are NOT NULL — no write without the birth tuple.
	if _, err := conn.Exec(ctx,
		`INSERT INTO memory_notes (workspace_id, visibility, origin, event_time, learned_at, source_event_id, content)
		 VALUES ($1, 'shared', 'dialogue', now(), now(), NULL, 'no evidence')`, ws); err == nil {
		t.Fatal("expected NULL source_event_id to violate NOT NULL")
	}

	// Unknown tiers and origins are rejected by the CHECK constraints.
	if _, err := conn.Exec(ctx,
		`INSERT INTO memory_notes (workspace_id, visibility, origin, event_time, learned_at, source_event_id, content)
		 VALUES ($1, 'tenant', 'dialogue', now(), now(), 'evt_x', 'bad tier')`, ws); err == nil {
		t.Fatal("expected unknown visibility to violate the visibility CHECK")
	}

	// The store reads an empty store fine (wiring sanity).
	counts, err := s.MemoryNotes().CountByVisibility(ctx, ws)
	if err != nil || len(counts) != 0 {
		t.Fatalf("expected empty counts, got (%+v, %v)", counts, err)
	}
}

func TestIntegration_MemoryStores_ScopingAndLifecycle(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws, userA, userB, agentX, agentY := seedMemoryScopesFixtures(t, s)
	notes := s.MemoryNotes()
	events := s.MemoryEvents()

	// -- Cross-member invisibility (structural, in-SQL) -------------------

	shared := newPGMemoryNote(ws, domain.MemoryVisibilityShared, "", "Deploy freeze on Fridays.")
	if err := notes.InsertNote(ctx, shared, domain.MemoryVisibilityShared); err != nil {
		t.Fatalf("insert shared note: %v", err)
	}
	noteA := newPGMemoryNote(ws, domain.MemoryVisibilityUser, userA, "Alice deploys the payments service.")
	if err := notes.InsertNote(ctx, noteA, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("insert user A note: %v", err)
	}
	noteB := newPGMemoryNote(ws, domain.MemoryVisibilityUser, userB, "Bob owns the on-call rota.")
	if err := notes.InsertNote(ctx, noteB, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("insert user B note: %v", err)
	}
	noteX := newPGMemoryNote(ws, domain.MemoryVisibilityAgent, agentX, "Atlas retries failed webhooks twice.")
	if err := notes.InsertNote(ctx, noteX, domain.MemoryVisibilityAgent); err != nil {
		t.Fatalf("insert agent X note: %v", err)
	}

	list, err := notes.ListNotesForUI(ctx, ws, userB, agentX, store.MemoryNoteFilters{})
	if err != nil || len(list) != 3 {
		t.Fatalf("expected 3 visible notes for B/X, got (%d, %v)", len(list), err)
	}
	for _, n := range list {
		if n.ID == noteA.ID {
			t.Fatal("B must not see A's user-visibility note")
		}
	}
	found, err := notes.SearchNotes(ctx, ws, userB, agentX, "payments service", store.MemoryNoteFilters{})
	if err != nil {
		t.Fatalf("search for B: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected 0 search hits for B on A's note, got %d", len(found))
	}
	got, err := notes.GetNote(ctx, ws, userA, agentX, noteB.ID)
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for foreign note, got (%+v, %v)", got, err)
	}
	list, err = notes.ListNotesForUI(ctx, ws, userA, agentY, store.MemoryNoteFilters{})
	if err != nil || len(list) != 2 {
		t.Fatalf("expected 2 visible notes for A/Y (no agent-X rows), got (%d, %v)", len(list), err)
	}

	// Events obey the same structural filter.
	evShared := newPGMemoryEvent(ws, agentX, "sess_1", "turn_1", domain.MemoryVisibilityShared, "", "Channel triaged the outage.")
	evShared.Participants = []domain.MemoryParticipant{
		{Kind: "agent", ID: agentX},
		{Kind: "user", ID: userA},
		{Kind: "user", ID: userB},
	}
	if err := events.InsertEvent(ctx, evShared); err != nil {
		t.Fatalf("insert shared event: %v", err)
	}
	evA := newPGMemoryEvent(ws, agentX, "sess_1", "turn_2", domain.MemoryVisibilityUser, userA, "Alice walked through the migration.")
	evA.EventTime = time.Date(2026, 9, 15, 10, 5, 0, 0, time.UTC)
	if err := events.InsertEvent(ctx, evA); err != nil {
		t.Fatalf("insert user event: %v", err)
	}
	evX := newPGMemoryEvent(ws, agentX, "sess_1", "turn_3", domain.MemoryVisibilityAgent, "", "Atlas finished the sweep.")
	evX.EventTime = time.Date(2026, 9, 15, 10, 10, 0, 0, time.UTC)
	if err := events.InsertEvent(ctx, evX); err != nil {
		t.Fatalf("insert agent event: %v", err)
	}

	evList, err := events.ListEventsForUI(ctx, ws, userB, agentY, store.MemoryEventFilters{})
	if err != nil || len(evList) != 1 || evList[0].ID != evShared.ID {
		t.Fatalf("expected only the shared event for B/Y, got (%d, %v)", len(evList), err)
	}
	if len(evList[0].Participants) != 3 || evList[0].Participants[0].Kind != "agent" {
		t.Fatalf("expected participants jsonb round-trip, got %+v", evList[0].Participants)
	}
	evFound, err := events.SearchEvents(ctx, ws, userB, agentY, "migration", store.MemoryEventFilters{})
	if err != nil {
		t.Fatalf("event search for B: %v", err)
	}
	if len(evFound) != 0 {
		t.Fatalf("expected 0 event hits for B on A's gist, got %d", len(evFound))
	}

	// Tenant partition: a foreign workspace sees nothing.
	ws2, u2, _, a2, _ := seedMemoryScopesFixtures2(t, s)
	if list, _ := notes.ListNotesForUI(ctx, ws2, userA, agentX, store.MemoryNoteFilters{}); len(list) != 0 {
		t.Fatalf("expected no notes across the workspace partition, got %d", len(list))
	}
	if evList, _ := events.ListEventsForUI(ctx, ws2, u2, a2, store.MemoryEventFilters{}); len(evList) != 0 {
		t.Fatalf("expected no events across the workspace partition, got %d", len(evList))
	}

	// -- Ceiling rejection (D4) -------------------------------------------

	note := newPGMemoryNote(ws, domain.MemoryVisibilityShared, "", "Share everything with everyone.")
	if err := notes.InsertNote(ctx, note, domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrMemoryVisibilityExceeded) {
		t.Fatalf("expected ErrMemoryVisibilityExceeded inserting shared under user ceiling, got %v", err)
	}
	if err := notes.SupersedeNote(ctx, ws, noteA.ID, newPGMemoryNote(ws, domain.MemoryVisibilityShared, "", "Widening correction."), domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrMemoryVisibilityExceeded) {
		t.Fatalf("expected ErrMemoryVisibilityExceeded superseding with shared under user ceiling, got %v", err)
	}

	// -- Provenance completeness (D5) --------------------------------------

	incomplete := newPGMemoryNote(ws, domain.MemoryVisibilityUser, userA, "No evidence pointer.")
	incomplete.SourceEventID = ""
	if err := notes.InsertNote(ctx, incomplete, domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Fatalf("expected ErrMemoryProvenanceIncomplete, got %v", err)
	}
	badEvent := newPGMemoryEvent(ws, agentX, "sess_p", "turn_1", domain.MemoryVisibilityUser, userA, "No learned time.")
	badEvent.LearnedAt = time.Time{}
	if err := events.InsertEvent(ctx, badEvent); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Fatalf("expected ErrMemoryProvenanceIncomplete, got %v", err)
	}

	// -- Supersede / tombstone / promote (D6) ------------------------------

	correction := newPGMemoryNote(ws, domain.MemoryVisibilityUser, userA, "The payment provider is Midtrans.")
	if err := notes.SupersedeNote(ctx, ws, noteA.ID, correction, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if correction.Supersedes == nil || *correction.Supersedes != noteA.ID {
		t.Fatalf("expected correction linked to old note, got %+v", correction)
	}
	// Default list (A, no serving agent): shared + the correction; the
	// superseded noteA is history-only.
	list, _ = notes.ListNotesForUI(ctx, ws, userA, "", store.MemoryNoteFilters{})
	if len(list) != 2 {
		t.Fatalf("expected 2 current notes for A, got %d: %+v", len(list), list)
	}
	supersededVisible := false
	for _, n := range list {
		if n.ID == noteA.ID {
			supersededVisible = true
		}
	}
	if supersededVisible {
		t.Fatal("superseded note must not appear in the default list")
	}
	history, _ := notes.ListNotesForUI(ctx, ws, userA, "", store.MemoryNoteFilters{History: true})
	if len(history) != 3 {
		t.Fatalf("expected shared + superseded + correction in the history view, got %d", len(history))
	}
	if err := notes.SupersedeNote(ctx, ws, noteA.ID, newPGMemoryNote(ws, domain.MemoryVisibilityUser, userA, "Second correction."), domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict double-superseding, got %v", err)
	}

	// CountSimilar sees only current notes: the superseded Stripe row is a
	// dead candidate, the correction matches.
	similar, err := notes.CountSimilar(ctx, ws, userA, "", "The payment provider is Midtrans, launched yesterday.", 0.3)
	if err != nil {
		t.Fatalf("count similar: %v", err)
	}
	if similar != 1 {
		t.Fatalf("expected 1 similar candidate, got %d", similar)
	}

	if err := notes.TombstoneNote(ctx, ws, correction.ID); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	if got, _ := notes.GetNote(ctx, ws, userA, "", correction.ID); got != nil {
		t.Fatalf("expected tombstoned note invisible, got %+v", got)
	}
	if err := notes.TombstoneNote(ctx, ws, correction.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound re-tombstoning, got %v", err)
	}

	if err := notes.PromoteNote(ctx, ws, noteB.ID, userB); err != nil {
		t.Fatalf("promote: %v", err)
	}
	promoted, err := notes.GetNote(ctx, ws, userA, "", noteB.ID)
	if err != nil || promoted == nil || promoted.Visibility != domain.MemoryVisibilityShared {
		t.Fatalf("expected promoted note visible to A as shared, got (%+v, %v)", promoted, err)
	}
	if promoted.PromotedBy == nil || *promoted.PromotedBy != userB || promoted.PromotedAt == nil {
		t.Fatalf("expected audited promotion, got %+v", promoted)
	}
	if err := notes.PromoteNote(ctx, ws, noteB.ID, userB); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict promoting already-shared note, got %v", err)
	}

	// -- Gister cursor and per-turn counts --------------------------------

	latest, err := events.LatestEventForSession(ctx, ws, "sess_1")
	if err != nil || latest == nil || latest.ID != evX.ID {
		t.Fatalf("expected the newest gist as cursor, got (%+v, %v)", latest, err)
	}
	counts, err := events.CountByVisibility(ctx, ws, "sess_1", "turn_2")
	if err != nil || counts[domain.MemoryVisibilityUser] != 1 {
		t.Fatalf("expected 1 user gist for turn_2, got (%+v, %v)", counts, err)
	}
	if err := events.TombstoneEvent(ctx, ws, evX.ID); err != nil {
		t.Fatalf("tombstone event: %v", err)
	}
	latest, err = events.LatestEventForSession(ctx, ws, "sess_1")
	if err != nil || latest == nil || latest.ID != evA.ID {
		t.Fatalf("expected evA as cursor after tombstoning evX, got (%+v, %v)", latest, err)
	}

	// FK discipline: writes against an unknown workspace are ErrNotFound.
	if err := notes.InsertNote(ctx, newPGMemoryNote("00000000-0000-0000-0000-000000000000", domain.MemoryVisibilityShared, "", "ghost"), domain.MemoryVisibilityShared); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}
}

// seedMemoryScopesFixtures2 creates a second workspace for partition tests.
func seedMemoryScopesFixtures2(t *testing.T, s store.Store) (wsID, userA, userB, agentX, agentY string) {
	t.Helper()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: "pg-mem-scopes-2", Name: "PG Mem Scopes 2"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace 2: %v", err)
	}
	u := &domain.User{Email: "pg-member-c@example.com", Name: "Member C"}
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatalf("create user C: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "P2", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider 2: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "cobalt", Name: "Cobalt", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return ws.ID, u.ID, u.ID, a.ID, a.ID
}
