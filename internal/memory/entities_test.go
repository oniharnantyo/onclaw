package memory

// The entity-resolution write path (wave3 tasks 4.1–4.3, D6/D7): entities
// ride the gate's and gister's existing side-calls, dedupe on normalized
// labels, inherit the narrowest endpoint's tier on edges, and skip
// malformed proposals individually.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// turnMaterial is the parsed turn the gate curates, one event per text.
func turnMaterial(t *testing.T, texts ...string) []domain.SessionEvent {
	t.Helper()
	events := make([]domain.SessionEvent, 0, len(texts))
	for i, text := range texts {
		events = append(events, chatEvent(t, "m"+string(rune('a'+i)), "turn-1", int64(i+1), time.Now().UTC().Add(-time.Hour), schema.AgenticRoleTypeUser, text))
	}
	return events
}

// TestGateLinksNoteEntities (spec: "notes arrive with entity links"): ops
// carrying entity proposals commit notes AND link them — zero additional
// model calls, the gate resolves one entity per distinct normalized label.
func TestGateLinksNoteEntities(t *testing.T) {
	s := seedWorld(t)
	ctx := context.Background()

	// Two mentions of the same entity under different spellings ("Sari",
	// then "saris" — the plural strip) must resolve to ONE row; "Beacon" is a
	// second entity; one malformed proposal rides along.
	response := `[
	 {"op":"ADD","content":"Sari owns the billing vendor contract.","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false,"entities":[{"label":"Sari","normalized_label":"sari"},{"label":"","normalized_label":""}]},
	 {"op":"ADD","content":"The rollout plan targets October for everyone.","visibility":"shared","importance":4,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false,"entities":[{"label":"saris","normalized_label":"deliberately-wrong"},{"label":"Beacon","normalized_label":"beacon"}]}
	]`
	gate := newTestGate(s, &scriptedModel{responses: []string{response}})
	job := testJob()
	job.Origin = originChannel
	job.HumanParticipants = 2 // shared ceiling: user and shared proposals both survive

	result, err := gate.Curate(ctx, job, turnMaterial(t, "Sari owns the billing vendor contract. The rollout plan targets October."), time.Now().UTC().Add(-time.Hour), "evt-end")
	if err != nil {
		t.Fatalf("Curate: %v", err)
	}
	if len(result.NoteIDs) != 2 {
		t.Fatalf("both ops must commit, got %+v", result.NoteIDs)
	}

	// Same normalized label resolves to one entity; the first spelling wins.
	all, err := s.MemoryEntities().ListEntities(ctx, testWorkspaceID, 0)
	if err != nil {
		t.Fatalf("list entities: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected exactly two entity rows (repeat label deduped), got %+v", all)
	}
	sari, err := s.MemoryEntities().GetEntityByLabel(ctx, testWorkspaceID, "sari")
	if err != nil || sari == nil {
		t.Fatalf("entity sari missing: %+v err %v", sari, err)
	}
	if sari.Label != "Sari" {
		t.Fatalf("the first spelling must win at birth, got %q", sari.Label)
	}
	if sari.Origin != domain.MemoryOriginDialogue || sari.SourceEventID != "evt-end" || sari.LearnedAt.IsZero() {
		t.Fatalf("entity provenance incomplete: %+v", sari)
	}

	// Edges: sari links both notes, atlas links the shared one. The edge
	// inherits the linked row's own tier — the narrowest endpoint (D7).
	edges, err := s.MemoryEntities().ListEdgesForEntities(ctx, testWorkspaceID, testUserID, testAgentID, []string{sari.ID})
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("expected two sari edges, got %+v", edges)
	}
	byTarget := map[string]domain.MemoryEntityEdge{}
	for _, e := range edges {
		byTarget[e.TargetID] = e
	}
	userNote, agentID := byTarget[result.Notes[0].ID], testAgentID
	if userNote.Visibility != domain.MemoryVisibilityUser {
		t.Fatalf("the edge to a user note must be user-visibility, got %+v", userNote.Visibility)
	}
	if userNote.Origin != domain.MemoryOriginDialogue || userNote.SourceEventID != "evt-end" {
		t.Fatalf("edge provenance must inherit the op's stamps, got %+v", userNote)
	}
	shared, err := s.MemoryEntities().GetEntityByLabel(ctx, testWorkspaceID, "beacon")
	if err != nil || shared == nil {
		t.Fatalf("entity beacon missing: %+v err %v", shared, err)
	}
	beaconEdges, err := s.MemoryEntities().ListEdgesForEntities(ctx, testWorkspaceID, testUserID, agentID, []string{shared.ID})
	if err != nil || len(beaconEdges) != 1 {
		t.Fatalf("expected one beacon edge, got %+v err %v", beaconEdges, err)
	}
	if beaconEdges[0].Visibility != domain.MemoryVisibilityShared || beaconEdges[0].TargetID != result.Notes[1].ID {
		t.Fatalf("the edge to a shared note must be shared-visibility, got %+v", beaconEdges[0])
	}
}

// TestGateEdgeTierInheritanceAcrossTiers (spec scenario "edge inherits the
// narrowest endpoint"): the same entity linked to user-, agent-, and
// shared-visibility rows carries one edge per row, each stamped with that
// row's tier.
func TestGateEdgeTierInheritanceAcrossTiers(t *testing.T) {
	s := seedWorld(t)
	ctx := context.Background()

	response := `[
	 {"op":"ADD","content":"Project Atlas ships the notification service.","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false,"entities":[{"label":"Notification Service","normalized_label":"notification service"}]},
	 {"op":"ADD","content":"The agent retries failed webhook deliveries three times.","visibility":"agent","importance":4,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false,"entities":[{"label":"notification services","normalized_label":"notification service"}]},
	 {"op":"ADD","content":"The team adopted the notification service in June.","visibility":"shared","importance":4,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false,"entities":[{"label":"Notification Service","normalized_label":"notification service"}]}
	]`
	gate := newTestGate(s, &scriptedModel{responses: []string{response}})
	job := testJob()
	job.Origin = originChannel
	job.HumanParticipants = 2

	result, err := gate.Curate(ctx, job, turnMaterial(t, "material one. material two. material three."), time.Now().UTC().Add(-time.Hour), "evt-end")
	if err != nil {
		t.Fatalf("Curate: %v", err)
	}
	if len(result.NoteIDs) != 3 {
		t.Fatalf("all three ops must commit, got %+v", result.NoteIDs)
	}

	entity, err := s.MemoryEntities().GetEntityByLabel(ctx, testWorkspaceID, "notification service")
	if err != nil || entity == nil {
		t.Fatalf("entity missing: %+v err %v", entity, err)
	}
	edges, err := s.MemoryEntities().ListEdgesForEntities(ctx, testWorkspaceID, testUserID, testAgentID, []string{entity.ID})
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	if len(edges) != 3 {
		t.Fatalf("expected three edges (one per linked row), got %+v", edges)
	}
	wantTier := map[string]domain.MemoryVisibility{}
	for _, note := range result.Notes {
		wantTier[note.ID] = note.Visibility
	}
	for _, edge := range edges {
		if edge.Visibility != wantTier[edge.TargetID] {
			t.Fatalf("edge tier must equal the linked row's tier, got %+v want %+v", edge.Visibility, wantTier[edge.TargetID])
		}
	}
}

// TestGateMalformedEntitySkipsAlone (spec: "malformed proposal skips
// alone"): an empty-label proposal inside a valid op never rejects the op,
// the batch, or the valid entities beside it.
func TestGateMalformedEntitySkipsAlone(t *testing.T) {
	s := seedWorld(t)
	ctx := context.Background()

	response := `[
	 {"op":"ADD","content":"The vendor portal password rotates quarterly.","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false,"entities":[{"label":"   ","normalized_label":""}]},
	 {"op":"ADD","content":"Beacon handles the incident paging rotation.","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false,"entities":[{"label":"Beacon","normalized_label":"beacon"}]}
	]`
	gate := newTestGate(s, &scriptedModel{responses: []string{response}})
	job := testJob()

	result, err := gate.Curate(ctx, job, turnMaterial(t, "vendor portal. beacon paging."), time.Now().UTC().Add(-time.Hour), "evt-end")
	if err != nil {
		t.Fatalf("Curate must not fail on a malformed entity: %v", err)
	}
	if len(result.NoteIDs) != 2 {
		t.Fatalf("both valid ops must commit, got %+v", result.NoteIDs)
	}
	all, err := s.MemoryEntities().ListEntities(ctx, testWorkspaceID, 0)
	if err != nil || len(all) != 1 {
		t.Fatalf("only the valid entity may exist, got %+v err %v", all, err)
	}
	if all[0].NormalizedLabel != "beacon" {
		t.Fatalf("expected the beacon entity, got %+v", all[0])
	}
}

// TestGisterLinksEventEntities: the gist's entity proposals link the
// committed event — edge tier inherits the gist's tier (agent for a
// scheduler turn), origin and source stamp ride along.
func TestGisterLinksEventEntities(t *testing.T) {
	s := seedWorld(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "Acme Corp renewed the platform subscription."),
	)

	gistModel := &scriptedModel{responses: []string{
		`{"description":"Acme Corp renewed the subscription","outcome":"Renewed","entities":[{"label":"Acme Corp","normalized_label":"acme corp"}]}`,
	}}
	gister := newTestGister(s, gistModel)
	job := testJob()
	job.Origin = originScheduler
	job.HumanParticipants = 0 // scheduler turn: agent-visibility gist

	win, err := gister.Window(ctx, job)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	event, err := gister.Gist(ctx, job, win)
	if err != nil {
		t.Fatalf("Gist: %v", err)
	}

	entity, err := s.MemoryEntities().GetEntityByLabel(ctx, testWorkspaceID, "acme corp")
	if err != nil || entity == nil {
		t.Fatalf("entity missing: %+v err %v", entity, err)
	}
	if entity.Label != "Acme Corp" || entity.Origin != domain.MemoryOriginDialogue || entity.SourceEventID != win.endID {
		t.Fatalf("entity provenance must ride the gist, got %+v", entity)
	}
	edges, err := s.MemoryEntities().ListEdgesForEntities(ctx, testWorkspaceID, testUserID, testAgentID, []string{entity.ID})
	if err != nil || len(edges) != 1 {
		t.Fatalf("expected one event edge, got %+v err %v", edges, err)
	}
	edge := edges[0]
	if edge.TargetType != domain.MemoryTargetEvent || edge.TargetID != event.ID {
		t.Fatalf("the edge must link the committed event, got %+v", edge)
	}
	if edge.Visibility != domain.MemoryVisibilityAgent {
		t.Fatalf("the edge must inherit the agent-visibility gist, got %+v", edge.Visibility)
	}
	if edge.SourceEventID != win.endID || edge.Origin != domain.MemoryOriginDialogue {
		t.Fatalf("edge provenance must inherit the event's stamps, got %+v", edge)
	}
}

// TestGateEntitiesResolveAcrossJobs (spec: "same label resolves to one
// entity"): a second job mentioning the same label links the EXISTING entity
// row instead of duplicating it.
func TestGateEntitiesResolveAcrossJobs(t *testing.T) {
	s := seedWorld(t)
	ctx := context.Background()

	first := newTestGate(s, &scriptedModel{responses: []string{
		`[{"op":"ADD","content":"Sari joined the platform team.","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false,"entities":[{"label":"Sari","normalized_label":"sari"}]}]`,
	}})
	if _, err := first.Curate(ctx, testJob(), turnMaterial(t, "Sari joined."), time.Now().UTC().Add(-2*time.Hour), "evt-1"); err != nil {
		t.Fatalf("first Curate: %v", err)
	}

	second := newTestGate(s, &scriptedModel{responses: []string{
		`[{"op":"ADD","content":"Sari presented the quarterly roadmap.","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false,"entities":[{"label":"SARI","normalized_label":"sari"}]}]`,
	}})
	if _, err := second.Curate(ctx, testJob(), turnMaterial(t, "Sari presented."), time.Now().UTC().Add(-time.Hour), "evt-2"); err != nil {
		t.Fatalf("second Curate: %v", err)
	}

	all, err := s.MemoryEntities().ListEntities(ctx, testWorkspaceID, 0)
	if err != nil || len(all) != 1 {
		t.Fatalf("the repeat label must resolve, never duplicate, got %+v err %v", all, err)
	}
	entity, err := s.MemoryEntities().GetEntityByLabel(ctx, testWorkspaceID, "sari")
	if err != nil || entity == nil {
		t.Fatalf("entity missing: %v", err)
	}
	edges, err := s.MemoryEntities().ListEdgesForEntities(ctx, testWorkspaceID, testUserID, testAgentID, []string{entity.ID})
	if err != nil || len(edges) != 2 {
		t.Fatalf("both notes must link the one entity, got %+v err %v", edges, err)
	}
	// The birth spelling and provenance never rewrite on re-resolution.
	if entity.Label != "Sari" {
		t.Fatalf("birth spelling must survive re-resolution, got %q", entity.Label)
	}
	if strings.TrimSpace(entity.SourceEventID) == "" {
		t.Fatalf("provenance must survive re-resolution, got %+v", entity)
	}
}
