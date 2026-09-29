package fake_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// refDocSeed builds a workspace, a uploader, two agents, and two channels,
// returning the ids the reference-document tests attach against.
func refDocSeed(t *testing.T, ctx context.Context, s store.Store, slug string) (wsID, userID, atlasID, beaconID, incidentsID, opsID string) {
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

// refDocFixture returns a document pointing at the given workspace, ready
// for Create.
func refDocFixture(workspaceID, uploadedBy, storageKey string) *domain.ReferenceDocument {
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

func TestReferenceDocumentStore_CreateAndGetRoundTrip(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, atlasID, _, incidentsID, _ := refDocSeed(t, ctx, s, "refdoc-roundtrip")

	doc := refDocFixture(wsID, userID, "ref/roundtrip-key")
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
}

func TestReferenceDocumentStore_CreateDefaultsAndValidation(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, _, _, _, _ := refDocSeed(t, ctx, s, "refdoc-defaults")

	// Empty scope and index status default at the store.
	doc := refDocFixture(wsID, userID, "ref/defaults-key")
	doc.Scope = ""
	doc.IndexStatus = ""
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create with defaults: %v", err)
	}
	got, _ := s.ReferenceDocuments().Get(ctx, wsID, doc.ID)
	if got.Scope != domain.RefDocScopeAttached || got.IndexStatus != domain.RefDocIndexProcessing {
		t.Errorf("expected attached/processing defaults, got %q/%q", got.Scope, got.IndexStatus)
	}

	// FK parity: unknown workspace, uploader, agents, and channels.
	if err := s.ReferenceDocuments().Create(ctx, refDocFixture("nope", userID, "ref/bad-ws")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown workspace: expected ErrNotFound, got %v", err)
	}
	if err := s.ReferenceDocuments().Create(ctx, refDocFixture(wsID, "nope", "ref/bad-user")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown uploader: expected ErrNotFound, got %v", err)
	}
	badAgent := refDocFixture(wsID, userID, "ref/bad-agent")
	badAgent.AgentIDs = []string{"nope"}
	if err := s.ReferenceDocuments().Create(ctx, badAgent); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown agent: expected ErrNotFound, got %v", err)
	}
	badChannel := refDocFixture(wsID, userID, "ref/bad-channel")
	badChannel.ChannelIDs = []string{"nope"}
	if err := s.ReferenceDocuments().Create(ctx, badChannel); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown channel: expected ErrNotFound, got %v", err)
	}

	// Duplicate storage key conflicts.
	dup := refDocFixture(wsID, userID, "ref/defaults-key")
	if err := s.ReferenceDocuments().Create(ctx, dup); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate storage key: expected ErrConflict, got %v", err)
	}

	// Disallowed mime type rejected.
	badMime := refDocFixture(wsID, userID, "ref/bad-mime")
	badMime.MimeType = "application/msword"
	if err := s.ReferenceDocuments().Create(ctx, badMime); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("legacy .doc mime: expected ErrInvalid, got %v", err)
	}
}

func TestReferenceDocumentStore_ForeignWorkspaceNotFound(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, _, _, _, _ := refDocSeed(t, ctx, s, "refdoc-foreign")

	other := &domain.Workspace{Slug: "refdoc-foreign-other", Name: "Other WS"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}

	doc := refDocFixture(wsID, userID, "ref/foreign-key")
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.ReferenceDocuments().Get(ctx, other.ID, doc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign-workspace Get: expected ErrNotFound, got %v", err)
	}
	if _, err := s.ReferenceDocuments().Get(ctx, wsID, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown id Get: expected ErrNotFound, got %v", err)
	}
	if err := s.ReferenceDocuments().Delete(ctx, other.ID, doc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign-workspace Delete: expected ErrNotFound, got %v", err)
	}

	// The failed attempts must not have disturbed the row.
	if _, err := s.ReferenceDocuments().Get(ctx, wsID, doc.ID); err != nil {
		t.Errorf("owning-workspace Get should still succeed: %v", err)
	}
}

func TestReferenceDocumentStore_GetByStorageKey(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, _, _, _, _ := refDocSeed(t, ctx, s, "refdoc-key")

	doc := refDocFixture(wsID, userID, "ref/known-key")
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create: %v", err)
	}

	byKey, err := s.ReferenceDocuments().GetByStorageKey(ctx, "ref/known-key")
	if err != nil {
		t.Fatalf("by storage key: %v", err)
	}
	if byKey.ID != doc.ID {
		t.Errorf("by-storage-key row mismatch: got %+v", byKey)
	}
	if _, err := s.ReferenceDocuments().GetByStorageKey(ctx, "ref/never-stored"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown storage key: expected ErrNotFound, got %v", err)
	}

	// Delete removes the document and its capability-key entry.
	if err := s.ReferenceDocuments().Delete(ctx, wsID, doc.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.ReferenceDocuments().GetByStorageKey(ctx, "ref/known-key"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("by storage key after delete: expected ErrNotFound, got %v", err)
	}
}

func TestReferenceDocumentStore_ListLenses(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, atlasID, beaconID, incidentsID, _ := refDocSeed(t, ctx, s, "refdoc-lenses")

	manual := refDocFixture(wsID, userID, "ref/manual")
	manual.AgentIDs = []string{atlasID}
	runbook := refDocFixture(wsID, userID, "ref/runbook")
	runbook.ChannelIDs = []string{incidentsID}
	promoted := refDocFixture(wsID, userID, "ref/promoted")
	promoted.Scope = domain.RefDocScopeWorkspace
	for _, doc := range []*domain.ReferenceDocument{manual, runbook, promoted} {
		if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
			t.Fatalf("create %s: %v", doc.StorageKey, err)
		}
	}

	all, err := s.ReferenceDocuments().List(ctx, wsID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 documents in workspace, got %d", len(all))
	}

	// Each lens is join-matched PLUS promoted: the promoted document (no
	// joins) appears in both lenses; an attached-only document appears only
	// in its own lens.
	byAgent, err := s.ReferenceDocuments().ListByAgent(ctx, wsID, atlasID)
	if err != nil {
		t.Fatalf("list by agent: %v", err)
	}
	if len(byAgent) != 2 {
		t.Fatalf("agent lens = %d documents, want manual + promoted", len(byAgent))
	}
	agentKeys := map[string]bool{}
	for _, doc := range byAgent {
		agentKeys[doc.StorageKey] = true
	}
	if !agentKeys["ref/manual"] || !agentKeys["ref/promoted"] || agentKeys["ref/runbook"] {
		t.Errorf("agent lens mismatch: %v (want manual + promoted, no runbook)", agentKeys)
	}

	byChannel, err := s.ReferenceDocuments().ListByChannel(ctx, wsID, incidentsID)
	if err != nil {
		t.Fatalf("list by channel: %v", err)
	}
	if len(byChannel) != 2 {
		t.Fatalf("channel lens = %d documents, want runbook + promoted", len(byChannel))
	}
	channelKeys := map[string]bool{}
	for _, doc := range byChannel {
		channelKeys[doc.StorageKey] = true
	}
	if !channelKeys["ref/runbook"] || !channelKeys["ref/promoted"] || channelKeys["ref/manual"] {
		t.Errorf("channel lens mismatch: %v (want runbook + promoted, no manual)", channelKeys)
	}

	// A foreign agent's lens carries only the promoted document — the
	// atlas-attached manual never leaks into it.
	byBeacon, err := s.ReferenceDocuments().ListByAgent(ctx, wsID, beaconID)
	if err != nil {
		t.Fatalf("list by other agent: %v", err)
	}
	if len(byBeacon) != 1 || byBeacon[0].StorageKey != "ref/promoted" {
		t.Errorf("other-agent lens = %+v, want only the promoted document", byBeacon)
	}

	// The lenses are workspace-scoped: another workspace sees nothing.
	empty, err := s.ReferenceDocuments().List(ctx, "nope")
	if err != nil {
		t.Fatalf("list foreign workspace: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected empty list for foreign workspace, got %d", len(empty))
	}
}

func TestReferenceDocumentStore_UpdateMetaAndScope(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, _, _, _, _ := refDocSeed(t, ctx, s, "refdoc-meta")

	doc := refDocFixture(wsID, userID, "ref/meta")
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := s.ReferenceDocuments().UpdateMeta(ctx, wsID, doc.ID, "Renamed Manual", "New description"); err != nil {
		t.Fatalf("update meta: %v", err)
	}
	got, _ := s.ReferenceDocuments().Get(ctx, wsID, doc.ID)
	if got.Name != "Renamed Manual" || got.Description != "New description" {
		t.Errorf("meta mismatch: %+v", got)
	}
	if got.StorageKey != "ref/meta" || got.MimeType != "application/pdf" {
		t.Errorf("meta update must not touch blob identity: %+v", got)
	}

	// Promote, then demote.
	if err := s.ReferenceDocuments().SetScope(ctx, wsID, doc.ID, domain.RefDocScopeWorkspace); err != nil {
		t.Fatalf("promote: %v", err)
	}
	got, _ = s.ReferenceDocuments().Get(ctx, wsID, doc.ID)
	if got.Scope != domain.RefDocScopeWorkspace {
		t.Errorf("expected workspace scope after promote, got %q", got.Scope)
	}
	if err := s.ReferenceDocuments().SetScope(ctx, wsID, doc.ID, domain.RefDocScopeAttached); err != nil {
		t.Fatalf("demote: %v", err)
	}

	// Invalid inputs.
	if err := s.ReferenceDocuments().SetScope(ctx, wsID, doc.ID, "promoted"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("invalid scope: expected ErrInvalid, got %v", err)
	}
	if err := s.ReferenceDocuments().UpdateMeta(ctx, wsID, doc.ID, "", "x"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("empty name: expected ErrInvalid, got %v", err)
	}
	if err := s.ReferenceDocuments().UpdateMeta(ctx, "nope", doc.ID, "x", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign workspace update: expected ErrNotFound, got %v", err)
	}
	if err := s.ReferenceDocuments().SetScope(ctx, "nope", doc.ID, domain.RefDocScopeWorkspace); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign workspace promote: expected ErrNotFound, got %v", err)
	}
}

func TestReferenceDocumentStore_SetAgentsSetChannelsComplete(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, atlasID, beaconID, incidentsID, opsID := refDocSeed(t, ctx, s, "refdoc-attach")

	doc := refDocFixture(wsID, userID, "ref/attach")
	doc.AgentIDs = []string{atlasID}
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Set-complete: the stored set becomes exactly the given ids.
	if err := s.ReferenceDocuments().SetAgents(ctx, wsID, doc.ID, []string{beaconID}); err != nil {
		t.Fatalf("set agents: %v", err)
	}
	got, _ := s.ReferenceDocuments().Get(ctx, wsID, doc.ID)
	if len(got.AgentIDs) != 1 || got.AgentIDs[0] != beaconID {
		t.Errorf("expected agent set replaced to [beacon], got %v", got.AgentIDs)
	}

	if err := s.ReferenceDocuments().SetChannels(ctx, wsID, doc.ID, []string{incidentsID, opsID}); err != nil {
		t.Fatalf("set channels: %v", err)
	}
	got, _ = s.ReferenceDocuments().Get(ctx, wsID, doc.ID)
	if len(got.ChannelIDs) != 2 {
		t.Errorf("expected two channels, got %v", got.ChannelIDs)
	}

	// Clearing with an empty list is legitimate.
	if err := s.ReferenceDocuments().SetAgents(ctx, wsID, doc.ID, nil); err != nil {
		t.Fatalf("clear agents: %v", err)
	}
	got, _ = s.ReferenceDocuments().Get(ctx, wsID, doc.ID)
	if len(got.AgentIDs) != 0 {
		t.Errorf("expected empty agent set, got %v", got.AgentIDs)
	}

	// Foreign and unknown ids are NotFound — never silently attached.
	other := &domain.Workspace{Slug: "refdoc-attach-other", Name: "Other"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	foreignAgent := &domain.Agent{WorkspaceID: other.ID, Slug: "atlas", Name: "Foreign Atlas"}
	if err := s.Agents().Create(ctx, foreignAgent); err != nil {
		t.Fatalf("create foreign agent: %v", err)
	}
	if err := s.ReferenceDocuments().SetAgents(ctx, wsID, doc.ID, []string{foreignAgent.ID}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign-workspace agent: expected ErrNotFound, got %v", err)
	}
	if err := s.ReferenceDocuments().SetChannels(ctx, wsID, doc.ID, []string{"nope"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown channel: expected ErrNotFound, got %v", err)
	}
}

func TestReferenceDocumentStore_ReplaceBlob(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, atlasID, _, _, _ := refDocSeed(t, ctx, s, "refdoc-replace")

	doc := refDocFixture(wsID, userID, "ref/old-edition")
	doc.AgentIDs = []string{atlasID}
	doc.Scope = domain.RefDocScopeWorkspace
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := s.ReferenceDocuments().ReplaceBlob(ctx, wsID, doc.ID, "application/pdf", 999999, "ref/new-edition", "s3", 240, domain.RefDocIndexReady); err != nil {
		t.Fatalf("replace blob: %v", err)
	}
	got, _ := s.ReferenceDocuments().Get(ctx, wsID, doc.ID)
	if got.StorageKey != "ref/new-edition" || got.Backend != "s3" || got.SizeBytes != 999999 || got.PageCount != 240 || got.IndexStatus != domain.RefDocIndexReady {
		t.Errorf("blob identity not swapped: %+v", got)
	}
	// Scope and attach lists survive the re-upload.
	if got.Scope != domain.RefDocScopeWorkspace || len(got.AgentIDs) != 1 {
		t.Errorf("scope/attach lists must survive re-upload: %+v", got)
	}
	// The old capability key is gone, the new one resolves.
	if _, err := s.ReferenceDocuments().GetByStorageKey(ctx, "ref/old-edition"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old storage key: expected ErrNotFound, got %v", err)
	}

	// Colliding key on another document conflicts.
	other := refDocFixture(wsID, userID, "ref/other")
	if err := s.ReferenceDocuments().Create(ctx, other); err != nil {
		t.Fatalf("create other: %v", err)
	}
	if err := s.ReferenceDocuments().ReplaceBlob(ctx, wsID, other.ID, "application/pdf", 1, "ref/new-edition", "local", 1, domain.RefDocIndexReady); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("colliding storage key: expected ErrConflict, got %v", err)
	}
	if err := s.ReferenceDocuments().ReplaceBlob(ctx, "nope", doc.ID, "application/pdf", 1, "ref/x", "local", 1, domain.RefDocIndexReady); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign workspace replace: expected ErrNotFound, got %v", err)
	}
}

func TestDocumentSectionStore_ReplaceAndSearch(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, atlasID, beaconID, _, _ := refDocSeed(t, ctx, s, "refdoc-sections")

	doc := refDocFixture(wsID, userID, "ref/searchable")
	doc.AgentIDs = []string{atlasID}
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create: %v", err)
	}
	hidden := refDocFixture(wsID, userID, "ref/hidden")
	hidden.AgentIDs = []string{beaconID}
	if err := s.ReferenceDocuments().Create(ctx, hidden); err != nil {
		t.Fatalf("create hidden: %v", err)
	}

	sections := []domain.DocumentSection{
		{Heading: "Webhooks", Locator: "p. 30", LocatorKind: domain.LocatorKindPage, Level: 2, Ordinal: 0, Body: "Signing keys rotate monthly in the sandbox."},
		{Heading: "Rate limits", Locator: "p. 44", LocatorKind: domain.LocatorKindPage, Level: 2, Ordinal: 1, Body: "The sandbox enforces a strict rate limit of 100 requests per second."},
	}
	if err := s.DocumentSections().ReplaceForDocument(ctx, wsID, doc.ID, sections); err != nil {
		t.Fatalf("replace sections: %v", err)
	}
	if err := s.DocumentSections().ReplaceForDocument(ctx, wsID, hidden.ID, []domain.DocumentSection{
		{Heading: "Hidden rate limits", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 0, Body: "Secret internal rate limit documentation."},
	}); err != nil {
		t.Fatalf("replace hidden sections: %v", err)
	}

	// All-terms match, case-insensitive: only the section containing ALL of
	// sandbox/rate/limit hits.
	hits, err := s.DocumentSections().Search(ctx, wsID, []string{doc.ID, hidden.ID}, "SANDBOX rate limit", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].DocumentID != doc.ID || hits[0].Heading != "Rate limits" {
		t.Errorf("expected the visible document's Rate limits hit, got %+v", hits[0])
	}
	if hits[0].DocumentName == "" {
		t.Errorf("expected document name joined onto hits: %+v", hits[0])
	}
	if !strings.Contains(strings.ToLower(hits[0].Snippet), "rate") {
		t.Errorf("expected snippet around the first term: %+v", hits[0].Snippet)
	}

	// A query two bodies share returns both documents' sections.
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{doc.ID, hidden.ID}, "rate limit", 10)
	if err != nil {
		t.Fatalf("search two docs: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits across documents, got %d: %+v", len(hits), hits)
	}

	// A term that only one body contains filters the other out.
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{doc.ID, hidden.ID}, "rotate monthly", 10)
	if err != nil {
		t.Fatalf("search phrase-ish: %v", err)
	}
	if len(hits) != 1 || hits[0].Heading != "Webhooks" {
		t.Errorf("expected only the Webhooks section, got %+v", hits)
	}

	// Visibility: empty document list never degrades to unfiltered.
	hits, err = s.DocumentSections().Search(ctx, wsID, nil, "rate limit", 10)
	if err != nil {
		t.Fatalf("search empty visibility: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected empty result for empty visibility filter, got %d", len(hits))
	}
	// Visibility: foreign/unknown doc ids contribute nothing.
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{"nope"}, "rate limit", 10)
	if err != nil {
		t.Fatalf("search unknown visibility: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected empty result for unknown visibility filter, got %d", len(hits))
	}

	// Limit bounds the result.
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{doc.ID, hidden.ID}, "rate limit", 1)
	if err != nil {
		t.Fatalf("search limited: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("expected limit to bound hits to 1, got %d", len(hits))
	}

	// Empty query is invalid input.
	if _, err := s.DocumentSections().Search(ctx, wsID, []string{doc.ID}, "   ", 10); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("empty query: expected ErrInvalid, got %v", err)
	}

	// Replace is set-complete: old sections disappear.
	if err := s.DocumentSections().ReplaceForDocument(ctx, wsID, doc.ID, []domain.DocumentSection{
		{Heading: "Only chapter", LocatorKind: domain.LocatorKindHeading, Level: 1, Ordinal: 0, Body: "Something entirely different about webhooks."},
	}); err != nil {
		t.Fatalf("re-replace: %v", err)
	}
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{doc.ID}, "sandbox", 10)
	if err != nil {
		t.Fatalf("search after replace: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected old sections to be gone after replace, got %+v", hits)
	}

	// Sections of an unknown document are ErrNotFound; deleting for an
	// already-gone document is not.
	if err := s.DocumentSections().ReplaceForDocument(ctx, wsID, "nope", sections); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("replace for unknown doc: expected ErrNotFound, got %v", err)
	}
	if err := s.DocumentSections().DeleteForDocument(ctx, wsID, "nope"); err != nil {
		t.Errorf("delete for unknown doc must be idempotent: %v", err)
	}

	// Deleting the document removes its sections (fake parity with cascade).
	if err := s.ReferenceDocuments().Delete(ctx, wsID, hidden.ID); err != nil {
		t.Fatalf("delete hidden: %v", err)
	}
	hits, err = s.DocumentSections().Search(ctx, wsID, []string{hidden.ID}, "secret internal", 10)
	if err != nil {
		t.Fatalf("search after doc delete: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected sections to die with the document, got %+v", hits)
	}
}

func TestReferenceDocumentStore_AgentDeleteCleansJoins(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, atlasID, _, _, _ := refDocSeed(t, ctx, s, "refdoc-cascade")

	doc := refDocFixture(wsID, userID, "ref/cascade")
	doc.AgentIDs = []string{atlasID}
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	if err := s.Agents().Delete(ctx, wsID, atlasID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	got, err := s.ReferenceDocuments().Get(ctx, wsID, doc.ID)
	if err != nil {
		t.Fatalf("get after agent delete: %v", err)
	}
	if len(got.AgentIDs) != 0 {
		t.Errorf("expected agent join cleaned by cascade, got %v", got.AgentIDs)
	}
}

func TestDocumentSectionStore_ListForDocument(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, atlasID, _, _, _ := refDocSeed(t, ctx, s, "refdoc-list-sections")

	doc := refDocFixture(wsID, userID, "ref/toc")
	doc.AgentIDs = []string{atlasID}
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Stored out of ordinal order; the read must sort.
	sections := []domain.DocumentSection{
		{Heading: "Second", LocatorKind: domain.LocatorKindHeading, Level: 1, Ordinal: 1, Body: "second body"},
		{Heading: "First", LocatorKind: domain.LocatorKindHeading, Level: 1, Ordinal: 0, Body: "first body"},
		{Heading: "Deep", LocatorKind: domain.LocatorKindHeading, Level: 3, Ordinal: 2, Body: "deep body"},
	}
	if err := s.DocumentSections().ReplaceForDocument(ctx, wsID, doc.ID, sections); err != nil {
		t.Fatalf("replace sections: %v", err)
	}

	got, err := s.DocumentSections().ListForDocument(ctx, wsID, doc.ID)
	if err != nil {
		t.Fatalf("list for document: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("listed sections = %d, want 3", len(got))
	}
	for i, want := range []string{"First", "Second", "Deep"} {
		if got[i].Heading != want {
			t.Errorf("section %d heading = %q, want %q (ordinal order)", i, got[i].Heading, want)
		}
	}

	// A document with no sections lists empty, not an error.
	if err := s.DocumentSections().DeleteForDocument(ctx, wsID, doc.ID); err != nil {
		t.Fatalf("delete sections: %v", err)
	}
	got, err = s.DocumentSections().ListForDocument(ctx, wsID, doc.ID)
	if err != nil {
		t.Fatalf("list after wipe: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("listed sections after wipe = %d, want 0", len(got))
	}

	// Unknown documents are ErrNotFound...
	if _, err := s.DocumentSections().ListForDocument(ctx, wsID, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown doc = %v, want ErrNotFound", err)
	}
	// ...and an empty workspace id never resolves anything (tenancy).
	if _, err := s.DocumentSections().ListForDocument(ctx, "", doc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("empty workspace = %v, want ErrNotFound", err)
	}
}

func TestReferenceDocumentStore_ChannelDeleteCleansJoins(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID, _, _, incidentsID, opsID := refDocSeed(t, ctx, s, "refdoc-channel-cascade")

	doc := refDocFixture(wsID, userID, "ref/channel-cascade")
	doc.ChannelIDs = []string{incidentsID, opsID}
	if err := s.ReferenceDocuments().Create(ctx, doc); err != nil {
		t.Fatalf("create doc: %v", err)
	}

	// Deleting one attached channel mirrors the postgres FK cascade: the
	// channel's join rows die, the other attachment and the row survive.
	if err := s.Channels().DeleteChannel(ctx, wsID, incidentsID); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	got, err := s.ReferenceDocuments().Get(ctx, wsID, doc.ID)
	if err != nil {
		t.Fatalf("get after channel delete: %v", err)
	}
	if len(got.ChannelIDs) != 1 || got.ChannelIDs[0] != opsID {
		t.Errorf("expected only [%s] left in channel joins, got %v", opsID, got.ChannelIDs)
	}

	// The agent joins are untouched by a channel delete.
	if _, err := s.Agents().ListForWorkspace(ctx, wsID); err != nil {
		t.Fatalf("list agents: %v", err)
	}
}
