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

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// setupTestSchemaPublic is setupTestSchema with `public` appended to the
// session search_path. The trigram indexes in migration 000054 need the
// pg_trgm operator class; on databases where pg_trgm is ALREADY installed in
// the public schema (any migrate-up against the bare dev database installs it
// there), `CREATE EXTENSION IF NOT EXISTS` is a no-op and a bare
// `search_path=<schema>` cannot resolve gin_trgm_ops. Appending public
// resolves it in both that case and the fresh-database case. The shared
// helper in postgres_test.go keeps its original shape — aligning it is the
// owner's call.
func setupTestSchemaPublic(t *testing.T) (store.Store, string, context.Context) {
	t.Helper()
	ctx := context.Background()
	baseDSN := getTestBaseDSN(t)

	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		t.Skipf("skipping integration test: database connection failed: %v", err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	schemaName := fmt.Sprintf("test_schema_%s", hex.EncodeToString(b))

	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName))
	if err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}

	separator := "?"
	if strings.Contains(baseDSN, "?") {
		separator = "&"
	}
	schemaDSN := fmt.Sprintf("%s%ssearch_path=%s,public", baseDSN, separator, schemaName)

	migrator := postgres.NewMigrator(schemaDSN)
	if err := migrator.Up(); err != nil {
		_, _ = conn.Exec(ctx, fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
		t.Fatalf("failed to run migrations: %v", err)
	}

	s, err := postgres.New(ctx, schemaDSN)
	if err != nil {
		_, _ = conn.Exec(ctx, fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
		t.Fatalf("failed to initialize postgres store: %v", err)
	}

	t.Cleanup(func() {
		_ = s.Close()
		cleanupConn, err := pgx.Connect(context.Background(), baseDSN)
		if err == nil {
			_, _ = cleanupConn.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
			_ = cleanupConn.Close(context.Background())
		}
	})

	return s, schemaDSN, ctx
}

// refDocSeedPG builds a workspace, an uploading user, two agents, and two
// channels, returning the ids the reference-document integration tests
// attach against.
func refDocSeedPG(t *testing.T, ctx context.Context, s store.Store, slug string) (wsID, userID, atlasID, beaconID, incidentsID, opsID string) {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	user := &domain.User{Email: slug + "-uploader@example.com", Name: "Uploader"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	atlas := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas"}
	if err := s.Agents().Create(ctx, atlas); err != nil {
		t.Fatalf("create agent atlas: %v", err)
	}
	beacon := &domain.Agent{WorkspaceID: ws.ID, Slug: "beacon", Name: "Beacon"}
	if err := s.Agents().Create(ctx, beacon); err != nil {
		t.Fatalf("create agent beacon: %v", err)
	}
	incidents := &domain.Channel{WorkspaceID: ws.ID, Slug: "incidents", Name: "#incidents"}
	if err := s.Channels().CreateChannel(ctx, incidents); err != nil {
		t.Fatalf("create channel incidents: %v", err)
	}
	ops := &domain.Channel{WorkspaceID: ws.ID, Slug: "ops", Name: "#ops"}
	if err := s.Channels().CreateChannel(ctx, ops); err != nil {
		t.Fatalf("create channel ops: %v", err)
	}
	return ws.ID, user.ID, atlas.ID, beacon.ID, incidents.ID, ops.ID
}

// refDocFixturePG returns a document pointing at the given workspace, ready
// for Create.
func refDocFixturePG(workspaceID, uploadedBy, storageKey string) *domain.ReferenceDocument {
	return &domain.ReferenceDocument{
		WorkspaceID: workspaceID,
		Name:        "Twilio API Reference",
		Description: "The full API manual",
		MimeType:    "application/pdf",
		SizeBytes:   1234567,
		StorageKey:  storageKey,
		Backend:     "local",
		PageCount:   120,
		Scope:       domain.RefDocScopeAttached,
		IndexStatus: domain.RefDocIndexReady,
		UploadedBy:  uploadedBy,
	}
}

// TestIntegration_ReferenceDocumentStore_CreateAndGet covers the upload-path
// round trip: every domain field survives, the id and timestamps are
// store-managed, the join lists hydrate, and the row is reachable through its
// capability key (the global serving lookup).
func TestIntegration_ReferenceDocumentStore_CreateAndGet(t *testing.T) {
	s, _, ctx := setupTestSchemaPublic(t)
	wsID, userID, atlasID, _, incidentsID, _ := refDocSeedPG(t, ctx, s, "refdoc-roundtrip")

	doc := refDocFixturePG(wsID, userID, "ref/roundtrip-key")
	doc.AgentIDs = []string{atlasID}
	doc.ChannelIDs = []string{incidentsID}
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create: %v", err)
	}
	if doc.ID == "" {
		t.Fatal("expected id to be assigned by the store")
	}
	if doc.CreatedAt.IsZero() || doc.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set by the store")
	}

	got, err := s.ReferenceDocuments().Get(ctx, wsID, doc.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != doc.ID ||
		got.WorkspaceID != wsID ||
		got.Name != "Twilio API Reference" ||
		got.Description != "The full API manual" ||
		got.MimeType != "application/pdf" ||
		got.SizeBytes != 1234567 ||
		got.StorageKey != "ref/roundtrip-key" ||
		got.Backend != "local" ||
		got.PageCount != 120 ||
		got.Scope != domain.RefDocScopeAttached ||
		got.IndexStatus != domain.RefDocIndexReady ||
		got.UploadedBy != userID ||
		len(got.AgentIDs) != 1 || got.AgentIDs[0] != atlasID ||
		len(got.ChannelIDs) != 1 || got.ChannelIDs[0] != incidentsID {
		t.Errorf("round-trip mismatch: got %+v", got)
	}

	byKey, err := s.ReferenceDocuments().GetByStorageKey(ctx, "ref/roundtrip-key")
	if err != nil {
		t.Fatalf("by storage key: %v", err)
	}
	if byKey.ID != doc.ID || len(byKey.AgentIDs) != 1 {
		t.Errorf("by-storage-key row mismatch: got %+v", byKey)
	}
}

// TestIntegration_ReferenceDocumentStore_Tenancy pins the tenancy
// requirement: a foreign document id is indistinguishable from an unknown id
// (domain.ErrNotFound — no existence leak), FK violations surface as
// ErrNotFound, and attaching a foreign-workspace agent or channel is
// rejected rather than silently attached.
func TestIntegration_ReferenceDocumentStore_Tenancy(t *testing.T) {
	s, _, ctx := setupTestSchemaPublic(t)
	wsID, userID, _, _, _, _ := refDocSeedPG(t, ctx, s, "refdoc-tenancy")

	other := &domain.Workspace{Slug: "refdoc-tenancy-other", Name: "Other WS"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	foreignAgent := &domain.Agent{WorkspaceID: other.ID, Slug: "atlas", Name: "Foreign Atlas"}
	if err := s.Agents().Create(ctx, foreignAgent); err != nil {
		t.Fatalf("create foreign agent: %v", err)
	}

	doc := refDocFixturePG(wsID, userID, "ref/tenancy-key")
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.ReferenceDocuments().Get(ctx, other.ID, doc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign-workspace lookup: expected ErrNotFound, got %v", err)
	}
	if _, err := s.ReferenceDocuments().Get(ctx, wsID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown-id lookup: expected ErrNotFound, got %v", err)
	}
	if err := s.ReferenceDocuments().SetScope(ctx, other.ID, doc.ID, domain.RefDocScopeWorkspace); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign-workspace promote: expected ErrNotFound, got %v", err)
	}
	if err := s.ReferenceDocuments().Delete(ctx, other.ID, doc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign-workspace delete: expected ErrNotFound, got %v", err)
	}

	// FK parity through the store's error translation.
	if err := s.ReferenceDocuments().Create(ctx, refDocFixturePG("00000000-0000-0000-0000-000000000000", userID, "ref/bad-ws")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown workspace: expected ErrNotFound, got %v", err)
	}
	if err := s.ReferenceDocuments().Create(ctx, refDocFixturePG(wsID, "00000000-0000-0000-0000-000000000000", "ref/bad-user")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown uploader: expected ErrNotFound, got %v", err)
	}
	badAgent := refDocFixturePG(wsID, userID, "ref/bad-agent")
	badAgent.AgentIDs = []string{"00000000-0000-0000-0000-000000000000"}
	if err := s.ReferenceDocuments().Create(ctx, badAgent); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown agent: expected ErrNotFound, got %v", err)
	}
	// A foreign-workspace agent exists — but not in THIS workspace.
	foreign := refDocFixturePG(wsID, userID, "ref/foreign-agent")
	foreign.AgentIDs = []string{foreignAgent.ID}
	if err := s.ReferenceDocuments().Create(ctx, foreign); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign-workspace agent: expected ErrNotFound, got %v", err)
	}
	if err := s.ReferenceDocuments().SetChannels(ctx, wsID, doc.ID, []string{"00000000-0000-0000-0000-000000000000"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown channel on SetChannels: expected ErrNotFound, got %v", err)
	}

	// The failed attempts must not have disturbed the row.
	got, err := s.ReferenceDocuments().Get(ctx, wsID, doc.ID)
	if err != nil || got.ID != doc.ID || len(got.AgentIDs) != 0 {
		t.Errorf("owning-workspace lookup should still succeed with no joins, got %+v, err %v", got, err)
	}
}

// TestIntegration_ReferenceDocumentStore_LensesMutations covers the list
// lenses, the set-complete attach writes, the scope flip, blob replacement,
// and delete cascading the joins.
func TestIntegration_ReferenceDocumentStore_LensesMutations(t *testing.T) {
	s, _, ctx := setupTestSchemaPublic(t)
	wsID, userID, atlasID, beaconID, incidentsID, opsID := refDocSeedPG(t, ctx, s, "refdoc-lenses")

	manual := refDocFixturePG(wsID, userID, "ref/manual")
	manual.AgentIDs = []string{atlasID}
	runbook := refDocFixturePG(wsID, userID, "ref/runbook")
	runbook.ChannelIDs = []string{incidentsID}
	for _, doc := range []*domain.ReferenceDocument{manual, runbook} {
		if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
			t.Fatalf("create %s: %v", doc.StorageKey, err)
		}
	}

	all, err := s.ReferenceDocuments().List(ctx, wsID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(all))
	}
	byAgent, err := s.ReferenceDocuments().ListByAgent(ctx, wsID, atlasID)
	if err != nil {
		t.Fatalf("list by agent: %v", err)
	}
	if len(byAgent) != 1 || byAgent[0].StorageKey != "ref/manual" {
		t.Errorf("agent lens mismatch: %+v", byAgent)
	}
	byChannel, err := s.ReferenceDocuments().ListByChannel(ctx, wsID, incidentsID)
	if err != nil {
		t.Fatalf("list by channel: %v", err)
	}
	if len(byChannel) != 1 || byChannel[0].StorageKey != "ref/runbook" {
		t.Errorf("channel lens mismatch: %+v", byChannel)
	}

	// Set-complete replacement on both join tables.
	if err := s.ReferenceDocuments().SetAgents(ctx, wsID, manual.ID, []string{beaconID}); err != nil {
		t.Fatalf("set agents: %v", err)
	}
	got, _ := s.ReferenceDocuments().Get(ctx, wsID, manual.ID)
	if len(got.AgentIDs) != 1 || got.AgentIDs[0] != beaconID {
		t.Errorf("expected agent set replaced to [beacon], got %v", got.AgentIDs)
	}
	if err := s.ReferenceDocuments().SetChannels(ctx, wsID, manual.ID, []string{incidentsID, opsID}); err != nil {
		t.Fatalf("set channels: %v", err)
	}
	got, _ = s.ReferenceDocuments().Get(ctx, wsID, manual.ID)
	if len(got.ChannelIDs) != 2 {
		t.Errorf("expected two channels, got %v", got.ChannelIDs)
	}

	// Promote / demote.
	if err := s.ReferenceDocuments().SetScope(ctx, wsID, manual.ID, domain.RefDocScopeWorkspace); err != nil {
		t.Fatalf("promote: %v", err)
	}
	got, _ = s.ReferenceDocuments().Get(ctx, wsID, manual.ID)
	if got.Scope != domain.RefDocScopeWorkspace {
		t.Errorf("expected workspace scope after promote, got %q", got.Scope)
	}
	if err := s.ReferenceDocuments().SetScope(ctx, wsID, manual.ID, domain.RefDocScopeAttached); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if err := s.ReferenceDocuments().SetScope(ctx, wsID, manual.ID, "promoted"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("invalid scope: expected ErrInvalid, got %v", err)
	}

	// Metadata rewrite leaves blob identity alone.
	if err := s.ReferenceDocuments().UpdateMeta(ctx, wsID, manual.ID, "Renamed Manual", "New description"); err != nil {
		t.Fatalf("update meta: %v", err)
	}
	got, _ = s.ReferenceDocuments().Get(ctx, wsID, manual.ID)
	if got.Name != "Renamed Manual" || got.StorageKey != "ref/manual" {
		t.Errorf("meta update mismatch: %+v", got)
	}

	// Replace swaps the blob identity and keeps scope + attach lists.
	if err := s.ReferenceDocuments().ReplaceBlob(ctx, wsID, manual.ID, "application/pdf", 999999, "ref/manual-v2", "s3", 240, domain.RefDocIndexProcessing); err != nil {
		t.Fatalf("replace blob: %v", err)
	}
	got, _ = s.ReferenceDocuments().Get(ctx, wsID, manual.ID)
	if got.StorageKey != "ref/manual-v2" || got.Backend != "s3" || got.SizeBytes != 999999 || got.PageCount != 240 || got.IndexStatus != domain.RefDocIndexProcessing {
		t.Errorf("blob identity not swapped: %+v", got)
	}
	if len(got.ChannelIDs) != 2 {
		t.Errorf("attach lists must survive re-upload: %+v", got)
	}
	if _, err := s.ReferenceDocuments().GetByStorageKey(ctx, "ref/manual"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old storage key: expected ErrNotFound, got %v", err)
	}

	// Delete removes the row and cascades both join tables: the lenses go
	// empty and the capability key stops resolving.
	if err := s.ReferenceDocuments().Delete(ctx, wsID, manual.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	byAgent, err = s.ReferenceDocuments().ListByAgent(ctx, wsID, beaconID)
	if err != nil {
		t.Fatalf("list by agent after delete: %v", err)
	}
	if len(byAgent) != 0 {
		t.Errorf("expected agent joins cascaded, got %+v", byAgent)
	}
	byChannel, err = s.ReferenceDocuments().ListByChannel(ctx, wsID, opsID)
	if err != nil {
		t.Fatalf("list by channel after delete: %v", err)
	}
	if len(byChannel) != 0 {
		t.Errorf("expected channel joins cascaded, got %+v", byChannel)
	}
	if _, err := s.ReferenceDocuments().GetByStorageKey(ctx, "ref/manual-v2"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("storage key after delete: expected ErrNotFound, got %v", err)
	}
	if err := s.ReferenceDocuments().Delete(ctx, wsID, manual.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("double delete: expected ErrNotFound, got %v", err)
	}
}

// TestIntegration_ReferenceDocumentStore_LensesIncludePromoted pins the lens
// semantics the management/chat surfaces require: the agent and channel
// lenses each return their join-matched documents PLUS every promoted
// (scope='workspace') document — a promoted document with no joins appears
// in both lenses, an attached-only document appears only in its own lens,
// and a foreign agent's lens carries only the promoted document.
func TestIntegration_ReferenceDocumentStore_LensesIncludePromoted(t *testing.T) {
	s, _, ctx := setupTestSchemaPublic(t)
	wsID, userID, atlasID, beaconID, incidentsID, opsID := refDocSeedPG(t, ctx, s, "refdoc-lens-promoted")

	manual := refDocFixturePG(wsID, userID, "ref/lens-manual")
	manual.AgentIDs = []string{atlasID}
	runbook := refDocFixturePG(wsID, userID, "ref/lens-runbook")
	runbook.ChannelIDs = []string{incidentsID}
	promoted := refDocFixturePG(wsID, userID, "ref/lens-promoted")
	promoted.Scope = domain.RefDocScopeWorkspace
	for _, doc := range []*domain.ReferenceDocument{manual, runbook, promoted} {
		if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
			t.Fatalf("create %s: %v", doc.StorageKey, err)
		}
	}

	byAgent, err := s.ReferenceDocuments().ListByAgent(ctx, wsID, atlasID)
	if err != nil {
		t.Fatalf("list by agent: %v", err)
	}
	agentKeys := map[string]bool{}
	for _, doc := range byAgent {
		agentKeys[doc.StorageKey] = true
		if len(doc.AgentIDs) == 0 && len(doc.ChannelIDs) == 0 && doc.Scope != domain.RefDocScopeWorkspace {
			t.Errorf("agent lens returned unattached, unpromoted document: %+v", doc)
		}
	}
	if !agentKeys["ref/lens-manual"] || !agentKeys["ref/lens-promoted"] || agentKeys["ref/lens-runbook"] {
		t.Errorf("agent lens mismatch: %v (want manual + promoted, no runbook)", agentKeys)
	}

	byChannel, err := s.ReferenceDocuments().ListByChannel(ctx, wsID, incidentsID)
	if err != nil {
		t.Fatalf("list by channel: %v", err)
	}
	channelKeys := map[string]bool{}
	for _, doc := range byChannel {
		channelKeys[doc.StorageKey] = true
	}
	if !channelKeys["ref/lens-runbook"] || !channelKeys["ref/lens-promoted"] || channelKeys["ref/lens-manual"] {
		t.Errorf("channel lens mismatch: %v (want runbook + promoted, no manual)", channelKeys)
	}

	// A foreign agent's lens carries only the promoted document.
	byBeacon, err := s.ReferenceDocuments().ListByAgent(ctx, wsID, beaconID)
	if err != nil {
		t.Fatalf("list by other agent: %v", err)
	}
	if len(byBeacon) != 1 || byBeacon[0].StorageKey != "ref/lens-promoted" {
		t.Errorf("other-agent lens = %+v, want only the promoted document", byBeacon)
	}
	// Same for a foreign channel.
	byOps, err := s.ReferenceDocuments().ListByChannel(ctx, wsID, opsID)
	if err != nil {
		t.Fatalf("list by other channel: %v", err)
	}
	if len(byOps) != 1 || byOps[0].StorageKey != "ref/lens-promoted" {
		t.Errorf("other-channel lens = %+v, want only the promoted document", byOps)
	}
}

// sameIDSet compares two id lists as sets (order-insensitive).
func sameIDSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(want))
	for _, id := range want {
		seen[id] = true
	}
	for _, id := range got {
		if !seen[id] {
			return false
		}
	}
	return true
}

// TestIntegration_ReferenceDocumentStore_ListHydratesEveryJoin is the
// aliasing regression: list()'s scan loop used to take element pointers
// before finishing, so append reallocation left earlier pointers mutating
// throwaway copies and the first rows came back with empty joins (rows 0-3
// of a 6-document workspace served "agents":[],"channels":[]). Eight
// documents force multiple backing-array reallocations; EVERY listed row
// must carry the joins it was created with.
func TestIntegration_ReferenceDocumentStore_ListHydratesEveryJoin(t *testing.T) {
	s, _, ctx := setupTestSchemaPublic(t)
	wsID, userID, atlasID, beaconID, incidentsID, opsID := refDocSeedPG(t, ctx, s, "refdoc-hydrate")

	const count = 8
	type expected struct {
		agents   []string
		channels []string
	}
	want := make(map[string]expected, count)
	for i := 0; i < count; i++ {
		doc := refDocFixturePG(wsID, userID, fmt.Sprintf("ref/hydrate-%d", i))
		doc.Name = fmt.Sprintf("Hydrate Doc %d", i)
		// Distinct alternating join combos: every row carries non-empty
		// agent and channel lists whose contents identify the row.
		if i%2 == 0 {
			doc.AgentIDs = []string{atlasID}
		} else {
			doc.AgentIDs = []string{beaconID}
		}
		if i%3 == 0 {
			doc.ChannelIDs = []string{incidentsID, opsID}
		} else {
			doc.ChannelIDs = []string{incidentsID}
		}
		if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
			t.Fatalf("create %s: %v", doc.StorageKey, err)
		}
		want[doc.StorageKey] = expected{agents: doc.AgentIDs, channels: doc.ChannelIDs}
	}

	all, err := s.ReferenceDocuments().List(ctx, wsID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != count {
		t.Fatalf("expected %d documents, got %d", count, len(all))
	}
	hydrated := 0
	for _, doc := range all {
		exp, ok := want[doc.StorageKey]
		if !ok {
			t.Errorf("unexpected document in list: %+v", doc)
			continue
		}
		// loadJoins orders joins by id, not by attach order — compare as sets.
		if !sameIDSet(doc.AgentIDs, exp.agents) || !sameIDSet(doc.ChannelIDs, exp.channels) {
			t.Errorf("document %s joins = agents:%v channels:%v, want agents:%v channels:%v",
				doc.StorageKey, doc.AgentIDs, doc.ChannelIDs, exp.agents, exp.channels)
			continue
		}
		hydrated++
	}
	if hydrated != count {
		t.Fatalf("only %d of %d rows hydrated their joins", hydrated, count)
	}

	// The agent lens rides the same list() path.
	byAgent, err := s.ReferenceDocuments().ListByAgent(ctx, wsID, beaconID)
	if err != nil {
		t.Fatalf("list by agent: %v", err)
	}
	if len(byAgent) != count/2 {
		t.Fatalf("expected %d beacon-attached documents, got %d", count/2, len(byAgent))
	}
	for _, doc := range byAgent {
		if len(doc.ChannelIDs) == 0 {
			t.Errorf("lens row %s lost its channel joins (agents:%v channels:%v)", doc.StorageKey, doc.AgentIDs, doc.ChannelIDs)
		}
	}
}

// TestIntegration_DocumentSectionStore_ReplaceAndFTSSearch covers the
// section index: replace-all semantics, ranking by relevance, the
// websearch_to_tsquery phrase syntax, headline snippets, the visibility
// filter's empty-list behavior, and delete cascading sections.
func TestIntegration_DocumentSectionStore_ReplaceAndFTSSearch(t *testing.T) {
	s, _, ctx := setupTestSchemaPublic(t)
	wsID, userID, atlasID, beaconID, _, _ := refDocSeedPG(t, ctx, s, "refdoc-fts")

	runbook := refDocFixturePG(wsID, userID, "ref/fts-runbook")
	runbook.Name = "Incident Runbook"
	runbook.AgentIDs = []string{atlasID}
	if err := s.ReferenceDocuments().Create(ctx, runbook); err != nil {
		t.Fatalf("create runbook: %v", err)
	}
	secret := refDocFixturePG(wsID, userID, "ref/fts-secret")
	secret.Name = "Secret Notes"
	secret.AgentIDs = []string{beaconID}
	if err := s.ReferenceDocuments().Create(ctx, secret); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	if err := s.DocumentSections().ReplaceForDocument(ctx, wsID, runbook.ID, []domain.DocumentSection{
		{Heading: "Webhooks", Locator: "p. 30", LocatorKind: domain.LocatorKindPage, Level: 2, Ordinal: 0,
			Body: "Webhook signing keys rotate monthly. The sandbox rate limit is 100 requests per second per account."},
		{Heading: "Retries", Locator: "p. 31", LocatorKind: domain.LocatorKindPage, Level: 2, Ordinal: 1,
			Body: "Failed deliveries retry with exponential backoff. Rate limit headers expose the retry budget."},
		{Heading: "Overview", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 2,
			Body: "This runbook covers integration sandbox configuration and on-call procedures."},
	}); err != nil {
		t.Fatalf("replace runbook sections: %v", err)
	}
	if err := s.DocumentSections().ReplaceForDocument(ctx, wsID, secret.ID, []domain.DocumentSection{
		{Heading: "Secret rate limit notes", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 0,
			Body: "Internal rate limit notes: the sandbox rate limit can be raised by support ticket."},
	}); err != nil {
		t.Fatalf("replace secret sections: %v", err)
	}

	// Mixed-locator hits, ranked by ts_rank: websearch_to_tsquery ANDs bare
	// terms (Google-style), so only sections carrying ALL of
	// sandbox/rate/limit match — and the secret note, which repeats the
	// terms, outranks the runbook's Webhooks section. Relevance, not
	// ordinal, decides the order.
	hits, err := s.DocumentSections().Search(ctx, wsID, []string{runbook.ID, secret.ID}, "sandbox rate limit", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d: %+v", len(hits), hits)
	}
	if hits[0].DocumentID != secret.ID {
		t.Errorf("expected the term-dense secret note first, got %+v", hits[0])
	}
	if hits[1].Heading != "Webhooks" || hits[1].Locator != "p. 30" || hits[1].LocatorKind != domain.LocatorKindPage {
		t.Errorf("expected the all-terms Webhooks hit second, got %+v", hits[1])
	}
	for _, hit := range hits {
		if hit.DocumentName == "" {
			t.Errorf("expected document name joined onto every hit: %+v", hit)
		}
		if hit.Snippet == "" {
			t.Errorf("expected a ts_headline snippet: %+v", hit)
		}
	}

	// websearch phrase syntax: quoted phrases match contiguous text only.
	// The phrase "rate limit is 100" exists only in the runbook's Webhooks
	// section — not in Retries, not in the secret note.
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{runbook.ID, secret.ID}, `"rate limit is 100"`, 10)
	if err != nil {
		t.Fatalf("phrase search: %v", err)
	}
	if len(hits) != 1 || hits[0].DocumentID != runbook.ID || hits[0].Heading != "Webhooks" {
		t.Errorf("expected exactly the runbook's Webhooks phrase hit, got %+v", hits)
	}
	// The mirrored phrase exists only in the secret note.
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{runbook.ID, secret.ID}, `"can be raised"`, 10)
	if err != nil {
		t.Fatalf("phrase search 2: %v", err)
	}
	if len(hits) != 1 || hits[0].DocumentID != secret.ID {
		t.Errorf("expected exactly the secret note's phrase hit, got %+v", hits)
	}

	// OR semantics of websearch_to_tsquery: any term finds the runbook.
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{runbook.ID}, "backoff", 10)
	if err != nil {
		t.Fatalf("single-term search: %v", err)
	}
	if len(hits) != 1 || hits[0].Heading != "Retries" {
		t.Errorf("expected the Retries section, got %+v", hits)
	}

	// Visibility: an empty filter returns empty — never unfiltered.
	hits, err = s.DocumentSections().Search(ctx, wsID, nil, "sandbox", 10)
	if err != nil {
		t.Fatalf("empty visibility search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected empty result for empty visibility filter, got %+v", hits)
	}
	// Visibility: foreign/unknown ids contribute nothing, never an error.
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{"00000000-0000-0000-0000-000000000000"}, "sandbox", 10)
	if err != nil {
		t.Fatalf("unknown visibility search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected empty result for unknown visibility filter, got %+v", hits)
	}

	// Limit bounds the result set.
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{runbook.ID, secret.ID}, "rate limit", 1)
	if err != nil {
		t.Fatalf("limited search: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("expected limit to bound hits to 1, got %d", len(hits))
	}

	// Empty query is invalid input.
	if _, err := s.DocumentSections().Search(ctx, wsID, []string{runbook.ID}, "  ", 10); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("empty query: expected ErrInvalid, got %v", err)
	}

	// Replace is set-complete: old sections disappear.
	if err := s.DocumentSections().ReplaceForDocument(ctx, wsID, runbook.ID, []domain.DocumentSection{
		{Heading: "Only chapter", LocatorKind: domain.LocatorKindHeading, Level: 1, Ordinal: 0,
			Body: "Something entirely different about on-call handoffs."},
	}); err != nil {
		t.Fatalf("re-replace: %v", err)
	}
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{runbook.ID}, "sandbox", 10)
	if err != nil {
		t.Fatalf("search after replace: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected old sections to be gone after replace, got %+v", hits)
	}

	// Sections of an unknown document are ErrNotFound.
	if err := s.DocumentSections().ReplaceForDocument(ctx, wsID, "00000000-0000-0000-0000-000000000000", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("replace for unknown doc: expected ErrNotFound, got %v", err)
	}
	// Deleting for an already-gone document is a no-op, not an error.
	if err := s.DocumentSections().DeleteForDocument(ctx, wsID, "00000000-0000-0000-0000-000000000000"); err != nil {
		t.Errorf("delete for unknown doc must be idempotent: %v", err)
	}

	// Deleting the document cascades its sections (ON DELETE CASCADE).
	if err := s.ReferenceDocuments().Delete(ctx, wsID, secret.ID); err != nil {
		t.Fatalf("delete secret: %v", err)
	}
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{secret.ID}, "support ticket", 10)
	if err != nil {
		t.Fatalf("search after doc delete: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected sections to die with the document, got %+v", hits)
	}
}

func TestIntegration_DocumentSectionStore_ListForDocument(t *testing.T) {
	s, _, ctx := setupTestSchemaPublic(t)
	wsID, userID, atlasID, _, _, _ := refDocSeedPG(t, ctx, s, "refdoc-toc")

	doc := refDocFixturePG(wsID, userID, "ref/toc-doc")
	doc.AgentIDs = []string{atlasID}
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create doc: %v", err)
	}

	// Ordinals inserted out of order; the read must return ordinal order.
	if err := s.DocumentSections().ReplaceForDocument(ctx, wsID, doc.ID, []domain.DocumentSection{
		{Heading: "Second", LocatorKind: domain.LocatorKindHeading, Level: 1, Ordinal: 1, Body: "second body"},
		{Heading: "First", LocatorKind: domain.LocatorKindHeading, Level: 1, Ordinal: 0, Body: "first body"},
	}); err != nil {
		t.Fatalf("replace sections: %v", err)
	}

	got, err := s.DocumentSections().ListForDocument(ctx, wsID, doc.ID)
	if err != nil {
		t.Fatalf("list for document: %v", err)
	}
	if len(got) != 2 || got[0].Heading != "First" || got[1].Heading != "Second" {
		t.Fatalf("listed sections = %+v, want First then Second in ordinal order", got)
	}

	// Unknown and foreign documents are ErrNotFound, indistinguishably.
	if _, err := s.DocumentSections().ListForDocument(ctx, wsID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown doc = %v, want ErrNotFound", err)
	}
	if _, err := s.DocumentSections().ListForDocument(ctx, "00000000-0000-0000-0000-000000000000", doc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign workspace doc = %v, want ErrNotFound", err)
	}
}
