package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// memorySettingsWire is the settings GET/PUT response shape.
type memorySettingsWire struct {
	Settings struct {
		VisibilityPosture   string `json:"visibility_posture"`
		IngestionEnabled    bool   `json:"ingestion_enabled"`
		RawEmbeddingEnabled bool   `json:"raw_embedding_enabled"`
		GateBudgetMs        int    `json:"gate_budget_ms"`
		Embedding           *struct {
			ProviderID string `json:"provider_id"`
			Model      string `json:"model"`
			Dimension  int    `json:"dimension"`
		} `json:"embedding"`
	} `json:"settings"`
}

func decodeMemorySettings(t *testing.T, body string) memorySettingsWire {
	t.Helper()
	var res memorySettingsWire
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("decode settings %q: %v", body, err)
	}
	return res
}

// stubConsolidator is the MemoryConsolidator test double: it records the
// workspace it was called with and returns a canned report.
type stubConsolidator struct {
	workspaceID string
	report      memory.MorningReport
	err         error
}

func (s *stubConsolidator) RunNow(_ context.Context, workspaceID string) (memory.MorningReport, error) {
	s.workspaceID = workspaceID
	return s.report, s.err
}

// newMemoryNotesTestEnv builds a gin engine with the memory-notes handlers
// and a seeded workspace, member ("member"), second member ("admin"), and a
// fixture agent (the episodic gists' producer). The caller identity is
// selected per request through the X-Test-User header.
func newMemoryNotesTestEnv(t *testing.T) (*gin.Engine, store.Store, *domain.Workspace, *stubConsolidator) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	ws := &domain.Workspace{Name: "Acme", Slug: "acme", Timezone: "UTC"}
	if err := st.Workspaces().Create(nil, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	users := map[string]*domain.User{
		"member": {Email: "member@example.com", Name: "Member"},
		"admin":  {Email: "admin@example.com", Name: "Admin"},
	}
	for _, u := range users {
		if err := st.Users().Create(nil, u); err != nil {
			t.Fatalf("seed user %s: %v", u.Email, err)
		}
	}

	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai-compatible", Name: "Fixture Provider", Enabled: true}
	if err := st.Providers().Create(nil, prov); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	agent := &domain.Agent{
		WorkspaceID: ws.ID,
		Name:        "Atlas Agent",
		Slug:        "atlas",
		Role:        "Advisor",
		Description: "Memory fixture agent",
		Brief:       "A memory test fixture",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
	}
	if err := st.Agents().Create(nil, agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	consolidator := &stubConsolidator{
		report: memory.MorningReport{
			GeneratedAt:        time.Now().UTC(),
			Conflicts:          []memory.ConflictFlag{},
			Merges:             []memory.MergeRecord{},
			ExtractionFailures: 2,
		},
	}
	h := handlers.NewMemoryNoteHandlers(
		st.MemoryNotes(),
		st.MemoryEvents(),
		newTestMemorySearcher(st),
		st.MemoryReports(),
		st.ToolSettings(),
		st.Providers(),
		consolidator,
		[]byte("0123456789abcdef0123456789abcdef"),
		providers.NewRegistry(),
	)

	r := gin.New()
	group := r.Group("/api/v1/workspaces/:ws")
	group.Use(func(c *gin.Context) {
		resolved, err := st.Workspaces().BySlug(nil, c.Param("ws"))
		if err != nil {
			handlers.AbortNotFound(c, "workspace not found")
			return
		}
		c.Set(handlers.WorkspaceContextKey, resolved)
		c.Set(handlers.UserContextKey, users[c.GetHeader("X-Test-User")])
		c.Next()
	})
	group.GET("/memory/notes", h.ListNotes)
	group.GET("/memory/notes/:id", h.GetNote)
	group.POST("/memory/notes/:id/promote", h.PromoteNote)
	group.DELETE("/memory/notes/:id", h.DeleteNote)
	group.GET("/memory/events", h.ListEvents)
	group.POST("/memory/consolidate", h.ConsolidateNow)
	group.GET("/memory/report", h.GetReport)
	group.GET("/memory/settings", h.GetSettings)
	group.PUT("/memory/settings", h.PutSettings)
	group.POST("/memory/settings/test", h.TestMemorySettings)
	return r, st, ws, consolidator
}

func doMemoryNotesRequest(r *gin.Engine, method, path, user string, body any) *httptest.ResponseRecorder {
	return doMemoryRequest(r, method, path, user, body)
}

// seedNote inserts a fully-provenanced note directly through the store.
func seedNote(t *testing.T, st store.Store, ws *domain.Workspace, visibility domain.MemoryVisibility, ownerUserID, ownerAgentID, content, topic string) domain.MemoryNote {
	t.Helper()
	note := &domain.MemoryNote{
		WorkspaceID:   ws.ID,
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Now().UTC().Add(-time.Hour),
		LearnedAt:     time.Now().UTC(),
		SourceEventID: "ev-1",
		Content:       content,
		Importance:    5,
	}
	if topic != "" {
		note.Topic = &topic
	}
	switch visibility {
	case domain.MemoryVisibilityUser:
		id := ownerUserID
		note.UserID = &id
	case domain.MemoryVisibilityAgent:
		id := ownerAgentID
		note.AgentID = &id
	}
	if err := st.MemoryNotes().InsertNote(nil, note, domain.MemoryVisibilityShared); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	return *note
}

// seedEvent inserts one episodic gist directly through the store.
func seedEvent(t *testing.T, st store.Store, ws *domain.Workspace, agentID string, visibility domain.MemoryVisibility, ownerUserID string, sessionID, description string) domain.MemoryEvent {
	t.Helper()
	ev := &domain.MemoryEvent{
		WorkspaceID:   ws.ID,
		AgentID:       agentID,
		SessionID:     sessionID,
		TurnID:        "turn-1",
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Now().UTC().Add(-time.Hour),
		LearnedAt:     time.Now().UTC(),
		SourceEventID: "ev-1",
		Description:   description,
		Participants:  []domain.MemoryParticipant{},
	}
	if ownerUserID != "" {
		id := ownerUserID
		ev.UserID = &id
	}
	if err := st.MemoryEvents().InsertEvent(nil, ev); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return *ev
}

func TestMemoryNotes_EmptyList(t *testing.T) {
	r, _, _, _ := newMemoryNotesTestEnv(t)

	w := doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/notes", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Notes  []domain.MemoryNote `json:"notes"`
		Counts map[string]int      `json:"counts"`
		Viewer string              `json:"viewer_user_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Notes) != 0 {
		t.Errorf("expected empty notes, got %d", len(res.Notes))
	}
	if res.Counts["shared"] != 0 || res.Counts["user"] != 0 || res.Counts["agent"] != 0 {
		t.Errorf("expected zero counts, got %v", res.Counts)
	}
}

func TestMemoryNotes_ScopeFilteringAndCounts(t *testing.T) {
	r, st, ws, _ := newMemoryNotesTestEnv(t)
	memberID := userIDByEmail(t, st, "member@example.com")
	adminID := userIDByEmail(t, st, "admin@example.com")

	memberNote := seedNote(t, st, ws, domain.MemoryVisibilityUser, memberID, "", "Member prefers Go", "")
	sharedNote := seedNote(t, st, ws, domain.MemoryVisibilityShared, "", "", "Postgres for everything", "")
	seedNote(t, st, ws, domain.MemoryVisibilityUser, adminID, "", "Admin prefers Rust", "")

	// The member sees shared rows plus their own user rows — never the
	// admin's (structural cross-member exclusion, D4/D8).
	w := doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/notes", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Notes  []domain.MemoryNote `json:"notes"`
		Counts map[string]int      `json:"counts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Notes) != 2 {
		t.Fatalf("expected 2 visible notes, got %d: %s", len(res.Notes), w.Body.String())
	}
	seen := map[string]bool{}
	for _, n := range res.Notes {
		seen[n.ID] = true
	}
	if !seen[memberNote.ID] || !seen[sharedNote.ID] {
		t.Errorf("expected member's own + shared notes, got %v", res.Notes)
	}
	// Counts are the workspace-wide per-tier aggregates (never content).
	if res.Counts["user"] != 2 || res.Counts["shared"] != 1 {
		t.Errorf("expected counts user=2 shared=1, got %v", res.Counts)
	}

	// The visibility filter narrows the listing.
	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/notes?visibility=shared", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Notes) != 1 || res.Notes[0].ID != sharedNote.ID {
		t.Errorf("expected only the shared note, got %v", res.Notes)
	}

	// Unknown visibility is invalid input.
	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/notes?visibility=world", "member", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown visibility, got %d", w.Code)
	}
}

func TestMemoryNotes_SearchAndTopicFilters(t *testing.T) {
	r, st, ws, _ := newMemoryNotesTestEnv(t)
	memberID := userIDByEmail(t, st, "member@example.com")

	seedNote(t, st, ws, domain.MemoryVisibilityUser, memberID, "", "Member deploys on Fridays", "deploy")
	seedNote(t, st, ws, domain.MemoryVisibilityShared, "", "", "Billing runs on Stripe", "billing")

	var res struct {
		Notes []domain.MemoryNote `json:"notes"`
	}
	w := doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/notes?q=Stripe", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Notes) != 1 || res.Notes[0].Topic == nil || *res.Notes[0].Topic != "billing" {
		t.Errorf("expected the Stripe note, got %v", res.Notes)
	}

	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/notes?topic=deploy", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Notes) != 1 {
		t.Errorf("expected the deploy-topic note, got %v", res.Notes)
	}
}

func TestMemoryNotes_DetailWithEvidenceAndPromoteAudit(t *testing.T) {
	r, st, ws, _ := newMemoryNotesTestEnv(t)
	memberID := userIDByEmail(t, st, "member@example.com")
	adminID := userIDByEmail(t, st, "admin@example.com")
	note := seedNote(t, st, ws, domain.MemoryVisibilityUser, memberID, "", "Member ships on Fridays", "")

	// Detail carries the provenance birth tuple and the evidence links. The
	// consolidation multi-evidence link is seeded through the store's additive
	// link writer (D12) — fresh notes carry their birth pointer in
	// source_event_id instead.
	if err := st.MemoryNotes().AddNoteEvidence(nil, ws.ID, note.ID, []string{"ev-2", "ev-3"}); err != nil {
		t.Fatalf("add evidence: %v", err)
	}
	w := doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/notes/"+note.ID, "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var detail struct {
		Note     domain.MemoryNote           `json:"note"`
		Evidence []domain.MemoryNoteEvidence `json:"evidence"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.Note.ID != note.ID || detail.Note.Origin != domain.MemoryOriginDialogue || detail.Note.SourceEventID != "ev-1" {
		t.Errorf("expected full provenance on the detail, got %+v", detail.Note)
	}
	if len(detail.Evidence) != 2 || detail.Evidence[0].SourceEventID != "ev-2" || detail.Evidence[1].SourceEventID != "ev-3" {
		t.Errorf("expected the seeded evidence links oldest-first, got %v", detail.Evidence)
	}

	// Promotion widens and audits (D4): promoted_by = the promoting user.
	w = doMemoryNotesRequest(r, http.MethodPost, "/api/v1/workspaces/acme/memory/notes/"+note.ID+"/promote", "admin", map[string]any{"note": "whole team needs this"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var promoted struct {
		Note domain.MemoryNote `json:"note"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &promoted); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if promoted.Note.Visibility != domain.MemoryVisibilityShared {
		t.Errorf("expected shared after promotion, got %q", promoted.Note.Visibility)
	}
	if promoted.Note.PromotedBy == nil || *promoted.Note.PromotedBy != adminID || promoted.Note.PromotedAt == nil {
		t.Errorf("expected the promotion audit record, got %+v", promoted.Note)
	}

	// An already-shared note refuses re-promotion (409)...
	w = doMemoryNotesRequest(r, http.MethodPost, "/api/v1/workspaces/acme/memory/notes/"+note.ID+"/promote", "admin", nil)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for double promotion, got %d: %s", w.Code, w.Body.String())
	}
	// ...and an unknown id is a 404.
	w = doMemoryNotesRequest(r, http.MethodPost, "/api/v1/workspaces/acme/memory/notes/nope/promote", "admin", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown note promotion, got %d", w.Code)
	}
}

func TestMemoryNotes_TombstoneDelete(t *testing.T) {
	r, st, ws, _ := newMemoryNotesTestEnv(t)
	memberID := userIDByEmail(t, st, "member@example.com")
	note := seedNote(t, st, ws, domain.MemoryVisibilityUser, memberID, "", "Member is on call Mondays", "")

	w := doMemoryNotesRequest(r, http.MethodDelete, "/api/v1/workspaces/acme/memory/notes/"+note.ID, "member", nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Hidden from every read path; a second delete 404s (tombstoned and
	// absent are indistinguishable — no existence leak).
	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/notes/"+note.ID, "member", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after tombstone, got %d", w.Code)
	}
	w = doMemoryNotesRequest(r, http.MethodDelete, "/api/v1/workspaces/acme/memory/notes/"+note.ID, "member", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for double delete, got %d", w.Code)
	}
}

func TestMemoryNotes_UnknownNoteDetailIs404(t *testing.T) {
	r, _, _, _ := newMemoryNotesTestEnv(t)

	w := doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/notes/nope", "member", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestMemoryNotes_EventsScopeFiltering(t *testing.T) {
	r, st, ws, _ := newMemoryNotesTestEnv(t)
	memberID := userIDByEmail(t, st, "member@example.com")
	adminID := userIDByEmail(t, st, "admin@example.com")
	agentID := agentIDBySlug(t, st, "atlas")

	memberEvent := seedEvent(t, st, ws, agentID, domain.MemoryVisibilityUser, memberID, "sess-1", "Member asked about billing")
	seedEvent(t, st, ws, agentID, domain.MemoryVisibilityUser, adminID, "sess-2", "Admin asked about backups")

	w := doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/events", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Events []domain.MemoryEvent `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Events) != 1 || res.Events[0].ID != memberEvent.ID {
		t.Errorf("expected only the member's own event, got %v", res.Events)
	}

	// The session filter narrows to the session's events.
	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/events?session_id=sess-1", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Events) != 1 {
		t.Errorf("expected the session's event, got %v", res.Events)
	}

	// A malformed time bound is invalid input.
	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/events?from=yesterday", "member", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed from, got %d", w.Code)
	}
}

func TestMemoryNotes_ConsolidateNowAndReport(t *testing.T) {
	r, st, ws, consolidator := newMemoryNotesTestEnv(t)

	// No report yet: the empty shape (silence names itself).
	w := doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/report", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var reportRes struct {
		Report memory.MorningReport `json:"report"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reportRes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reportRes.Report.ExtractionFailures != 0 || reportRes.Report.Conflicts == nil || reportRes.Report.Merges == nil {
		t.Errorf("expected the empty report shape, got %+v", reportRes.Report)
	}

	// Consolidate-now drives the same RunNow the nightly ticker uses.
	w = doMemoryNotesRequest(r, http.MethodPost, "/api/v1/workspaces/acme/memory/consolidate", "admin", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if consolidator.workspaceID != ws.ID {
		t.Errorf("expected RunNow scoped to the workspace, got %q", consolidator.workspaceID)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reportRes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reportRes.Report.ExtractionFailures != 2 {
		t.Errorf("expected the consolidator's report, got %+v", reportRes.Report)
	}

	// The consolidator's saved report reads back through GET.
	raw, _ := json.Marshal(memory.MorningReport{
		GeneratedAt:        time.Now().UTC(),
		Conflicts:          []memory.ConflictFlag{{NoteID: "n1", Document: "WORKSPACE.md", Excerpt: "Stripe"}},
		Merges:             []memory.MergeRecord{{CanonicalID: "n1", MergedIDs: []string{"n2", "n3"}}},
		ExtractionFailures: 1,
	})
	if err := st.MemoryReports().Save(nil, ws.ID, raw, time.Now().UTC()); err != nil {
		t.Fatalf("save report: %v", err)
	}
	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/report", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reportRes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reportRes.Report.ExtractionFailures != 1 || len(reportRes.Report.Conflicts) != 1 || len(reportRes.Report.Merges) != 1 {
		t.Errorf("expected the saved report, got %+v", reportRes.Report)
	}
}

func TestMemoryNotes_SettingsRoundTrip(t *testing.T) {
	r, st, ws, _ := newMemoryNotesTestEnv(t)

	// The env seeds exactly one workspace provider — the embedding pin's
	// valid target.
	provList, err := st.Providers().ListForWorkspace(nil, ws.ID)
	if err != nil || len(provList) != 1 {
		t.Fatalf("expected one seeded provider, got %d err %v", len(provList), err)
	}
	fixtureProvider := provList[0].ID

	// Defaults: narrow posture, ingestion on, no embedding provider.
	w := doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/settings", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	s := decodeMemorySettings(t, w.Body.String()).Settings
	if s.VisibilityPosture != "narrow" || !s.IngestionEnabled || s.Embedding != nil {
		t.Fatalf("expected defaults, got %+v", s)
	}

	// PUT configures posture, ingestion, and the embedding provider. The
	// embedding provider IS a workspace provider (endpoint + credential live
	// on its record) — the record pins provider, model, dimension only, and
	// an unknown provider is rejected.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"visibility_posture": "org-shared",
		"ingestion_enabled":  false,
		"embedding": map[string]any{
			"provider_id": "does-not-exist",
			"model":       "text-embedding-3-small",
			"dimension":   1536,
		},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown embedding provider, got %d: %s", w.Code, w.Body.String())
	}

	// The seeded fixture provider is accepted; no endpoint or api_key field
	// exists on the embedding record anymore.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"visibility_posture": "org-shared",
		"ingestion_enabled":  false,
		"embedding": map[string]any{
			"provider_id": fixtureProvider,
			"model":       "text-embedding-3-small",
			"dimension":   1536,
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	s = decodeMemorySettings(t, w.Body.String()).Settings
	if s.VisibilityPosture != "org-shared" || s.IngestionEnabled {
		t.Errorf("expected org-shared + disabled, got %+v", s)
	}
	if s.Embedding == nil || s.Embedding.ProviderID != fixtureProvider || s.Embedding.Model != "text-embedding-3-small" || s.Embedding.Dimension != 1536 {
		t.Fatalf("expected the embedding view, got %+v", s.Embedding)
	}
	row, err := st.ToolSettings().Get(nil, ws.ID, "memory")
	if err != nil || row == nil {
		t.Fatalf("expected a stored memory settings row, got %v err %v", row, err)
	}
	// The ingestion toggle rides the structured record — the row's Enabled
	// bit stays the memory TOOL's availability (the Tools pane owns it).
	if ing, ok := row.Config["ingestion_enabled"].(bool); !ok || ing {
		t.Fatalf("expected ingestion_enabled false in the structured record, got %v", row.Config["ingestion_enabled"])
	}
	// No embedding-specific endpoint or credential is stored — the provider
	// record owns both, and legacy keys are dropped on save.
	if row.Config["embedding_endpoint"] != nil || row.Config["embedding_api_key"] != nil {
		t.Fatalf("expected no embedding endpoint/api_key at rest, got %v/%v", row.Config["embedding_endpoint"], row.Config["embedding_api_key"])
	}

	// A partial PUT keeps the stored provider and updates just the model.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"embedding": map[string]any{"model": "text-embedding-3-large"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	s = decodeMemorySettings(t, w.Body.String()).Settings
	if s.Embedding == nil || s.Embedding.Model != "text-embedding-3-large" {
		t.Errorf("expected the new model, got %+v", s.Embedding)
	}
	if s.Embedding == nil || s.Embedding.ProviderID != fixtureProvider {
		t.Errorf("expected the stored embedding provider to survive a partial PUT, got %+v", s.Embedding)
	}

	// Invalid posture is invalid input; the stored posture is untouched.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"visibility_posture": "everyone",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/settings", "member", nil)
	s = decodeMemorySettings(t, w.Body.String()).Settings
	if s.VisibilityPosture != "org-shared" {
		t.Errorf("expected the stored posture to survive the rejection, got %q", s.VisibilityPosture)
	}

	// A dimension must be positive when supplied.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"embedding": map[string]any{"dimension": 0},
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for dimension 0, got %d", w.Code)
	}
}

// TestMemoryNotes_RawEmbeddingToggle (wave3): the raw-embedding setting
// round-trips through PUT/GET; absence is the ON default and the stored
// record carries the structured key.
func TestMemoryNotes_RawEmbeddingToggle(t *testing.T) {
	r, st, ws, _ := newMemoryNotesTestEnv(t)

	// Absence is ON: no record exists yet, and the view defaults enabled.
	w := doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/settings", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if s := decodeMemorySettings(t, w.Body.String()).Settings; !s.RawEmbeddingEnabled {
		t.Fatalf("absence must read as enabled, got %+v", s)
	}

	// PUT false disables the raw channel.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"raw_embedding_enabled": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if s := decodeMemorySettings(t, w.Body.String()).Settings; s.RawEmbeddingEnabled {
		t.Fatalf("expected the response to carry the disabled toggle, got %+v", s)
	}
	row, err := st.ToolSettings().Get(nil, ws.ID, "memory")
	if err != nil || row == nil {
		t.Fatalf("expected a stored settings row, got %v err %v", row, err)
	}
	if v, ok := row.Config["raw_embedding_enabled"].(bool); !ok || v {
		t.Fatalf("expected raw_embedding_enabled false at rest, got %v", row.Config["raw_embedding_enabled"])
	}

	// A GET (not just the PUT echo) reads the stored toggle back.
	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/settings", "member", nil)
	if s := decodeMemorySettings(t, w.Body.String()).Settings; s.RawEmbeddingEnabled {
		t.Fatalf("expected the stored disabled toggle on GET, got %+v", s)
	}

	// A partial PUT without the key keeps the stored choice.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"ingestion_enabled": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/settings", "member", nil)
	if s := decodeMemorySettings(t, w.Body.String()).Settings; s.RawEmbeddingEnabled {
		t.Fatalf("the stored toggle must survive a partial PUT, got %+v", s)
	}

	// Re-enabling round-trips too.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"raw_embedding_enabled": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/settings", "member", nil)
	if s := decodeMemorySettings(t, w.Body.String()).Settings; !s.RawEmbeddingEnabled {
		t.Fatalf("expected the re-enabled toggle on GET, got %+v", s)
	}
}

func TestMemoryNotes_SettingsGateBudget(t *testing.T) {
	r, st, ws, _ := newMemoryNotesTestEnv(t)

	// Fresh workspace: the default resolves on GET even though no record
	// exists (absence-is-defaults).
	w := doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/settings", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if s := decodeMemorySettings(t, w.Body.String()).Settings; s.GateBudgetMs != memory.DefaultGateBudgetMS {
		t.Fatalf("expected default gate_budget_ms %d, got %d", memory.DefaultGateBudgetMS, s.GateBudgetMs)
	}

	// Below the lower bound is rejected, naming the range.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"gate_budget_ms": memory.MinGateBudgetMS - 1,
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for gate_budget_ms %d, got %d: %s", memory.MinGateBudgetMS-1, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), fmt.Sprintf("between %d and %d", memory.MinGateBudgetMS, memory.MaxGateBudgetMS)) {
		t.Fatalf("expected the error to name the bounds, got %s", w.Body.String())
	}

	// Above the upper bound is rejected too.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"gate_budget_ms": memory.MaxGateBudgetMS + 1,
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for gate_budget_ms %d, got %d: %s", memory.MaxGateBudgetMS+1, w.Code, w.Body.String())
	}

	// A non-integer supply fails JSON binding into the int field.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"gate_budget_ms": 1.5,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-integer gate_budget_ms, got %d: %s", w.Code, w.Body.String())
	}

	// An in-bounds value persists and reads back as the same integer.
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"gate_budget_ms": 8000,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if s := decodeMemorySettings(t, w.Body.String()).Settings; s.GateBudgetMs != 8000 {
		t.Fatalf("expected 8000 on the PUT response, got %d", s.GateBudgetMs)
	}

	// A PUT without the field leaves the stored budget untouched (absent =
	// keep stored; there is no null-clear path).
	w = doMemoryNotesRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory/settings", "admin", map[string]any{
		"ingestion_enabled": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = doMemoryNotesRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory/settings", "member", nil)
	if s := decodeMemorySettings(t, w.Body.String()).Settings; s.GateBudgetMs != 8000 {
		t.Fatalf("expected the stored 8000 to survive a PUT without the field, got %d", s.GateBudgetMs)
	}
	row, err := st.ToolSettings().Get(nil, ws.ID, "memory")
	if err != nil || row == nil {
		t.Fatalf("expected a stored memory settings row, got %v err %v", row, err)
	}
	if ms, ok := row.Config["gate_budget_ms"].(int); !ok || ms != 8000 {
		t.Fatalf("expected gate_budget_ms 8000 at rest, got %v", row.Config["gate_budget_ms"])
	}
}

func TestMemoryNotes_SettingsTestConnection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := storefake.New()
	ws := &domain.Workspace{Name: "Acme", Slug: "acme", Timezone: "UTC"}
	if err := st.Workspaces().Create(nil, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if err := st.Users().Create(nil, &domain.User{Email: "admin@example.com", Name: "Admin"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// A keyless provider for the plain probe legs and a provider carrying a
	// sealed credential — the embedding test resolves both from the record.
	keyless := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai-compatible", Name: "Keyless", BaseURL: "https://emb.example.com/v1", Enabled: true}
	if err := st.Providers().Create(nil, keyless); err != nil {
		t.Fatalf("seed keyless provider: %v", err)
	}
	sealed, err := secrets.Encrypt([]byte("0123456789abcdef0123456789abcdef"), []byte(ws.ID), []byte("sk-stored"))
	if err != nil {
		t.Fatalf("seal credential: %v", err)
	}
	credentialed := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai-compatible", Name: "Credentialed", BaseURL: "https://emb.example.com/v1", KeyCiphertext: sealed, Enabled: true}
	if err := st.Providers().Create(nil, credentialed); err != nil {
		t.Fatalf("seed credentialed provider: %v", err)
	}
	down := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai-compatible", Name: "Down", BaseURL: "https://down.example.com", Enabled: true}
	if err := st.Providers().Create(nil, down); err != nil {
		t.Fatalf("seed down provider: %v", err)
	}

	var probedEndpoint, probedKey, probedModel string
	probe := func(_ context.Context, endpoint, apiKey, model string) (int, error) {
		probedEndpoint, probedKey, probedModel = endpoint, apiKey, model
		if strings.Contains(endpoint, "down.example.com") {
			return 0, errors.New("embedding endpoint unreachable: connection refused")
		}
		return 1536, nil
	}
	h := handlers.NewMemoryNoteHandlers(
		st.MemoryNotes(), st.MemoryEvents(), newTestMemorySearcher(st), st.MemoryReports(), st.ToolSettings(),
		st.Providers(), &stubConsolidator{}, []byte("0123456789abcdef0123456789abcdef"),
		providers.NewRegistry(),
		handlers.WithEmbeddingProbe(probe),
	)
	r := gin.New()
	group := r.Group("/api/v1/workspaces/:ws")
	group.Use(func(c *gin.Context) {
		resolved, _ := st.Workspaces().BySlug(nil, c.Param("ws"))
		c.Set(handlers.WorkspaceContextKey, resolved)
		c.Set(handlers.UserContextKey, userByEmail(st, "admin@example.com"))
		c.Next()
	})
	group.POST("/memory/settings/test", h.TestMemorySettings)
	group.PUT("/memory/settings", h.PutSettings)

	// Happy path: ok + the discovered dimension, probed through the chosen
	// provider's base URL.
	w := doMemoryRequest(r, http.MethodPost, "/api/v1/workspaces/acme/memory/settings/test", "admin", map[string]any{
		"provider_id": keyless.ID, "model": "text-embedding-3-small",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		OK        bool `json:"ok"`
		Dimension int  `json:"dimension"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.OK || res.Dimension != 1536 {
		t.Errorf("expected ok/1536, got %+v", res)
	}
	if probedEndpoint != "https://emb.example.com/v1" || probedKey != "" || probedModel != "text-embedding-3-small" {
		t.Errorf("expected the probe to ride the provider's base URL keyless, got %q/%q/%q", probedEndpoint, probedKey, probedModel)
	}

	// The provider's sealed credential rides the probe, unsealed server-side.
	w = doMemoryRequest(r, http.MethodPost, "/api/v1/workspaces/acme/memory/settings/test", "admin", map[string]any{
		"provider_id": credentialed.ID, "model": "text-embedding-3-small",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if probedKey != "sk-stored" {
		t.Errorf("expected the provider credential to ride the probe, got %q", probedKey)
	}
	// No key ever echoes back.
	if strings.Contains(w.Body.String(), "sk-stored") {
		t.Errorf("response leaked the stored key: %s", w.Body.String())
	}

	// Dimension mismatch is a structured 422 naming both sides.
	w = doMemoryRequest(r, http.MethodPost, "/api/v1/workspaces/acme/memory/settings/test", "admin", map[string]any{
		"provider_id": keyless.ID, "model": "m", "dimension": 768,
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "1536") || !strings.Contains(w.Body.String(), "768") {
		t.Errorf("expected the mismatch to name both dimensions: %s", w.Body.String())
	}

	// Probe failure is a 422 carrying the endpoint's reason.
	w = doMemoryRequest(r, http.MethodPost, "/api/v1/workspaces/acme/memory/settings/test", "admin", map[string]any{
		"provider_id": down.ID, "model": "m",
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}

	// Missing required fields is a 400.
	w = doMemoryRequest(r, http.MethodPost, "/api/v1/workspaces/acme/memory/settings/test", "admin", map[string]any{
		"model": "text-embedding-3-small",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// An unknown provider is a 400, not a probe against nothing.
	w = doMemoryRequest(r, http.MethodPost, "/api/v1/workspaces/acme/memory/settings/test", "admin", map[string]any{
		"provider_id": "does-not-exist", "model": "m",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// userByEmail resolves a seeded user pointer for the test middleware.
func userByEmail(st store.Store, email string) *domain.User {
	u, err := st.Users().ByEmail(nil, email)
	if err != nil {
		return nil
	}
	return u
}

// agentIDBySlug resolves the fixture workspace agent's store-assigned ID.
func agentIDBySlug(t *testing.T, st store.Store, slug string) string {
	t.Helper()
	ws, err := st.Workspaces().BySlug(nil, "acme")
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	a, err := st.Agents().BySlug(nil, ws.ID, slug)
	if err != nil {
		t.Fatalf("load agent %s: %v", slug, err)
	}
	return a.ID
}
