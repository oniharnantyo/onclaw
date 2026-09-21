package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// fakeMemoryStore is a minimal MemoryStore for tool tests: maps keyed by the
// composite identity, Get returning (nil, nil) when nothing is stored. The
// two-document tool never needs anything beyond the port's six methods.
type fakeMemoryStore struct {
	mu        sync.Mutex
	user      map[string]*domain.Memory // key: workspaceID + "|" + userID
	workspace map[string]*domain.Memory // key: workspaceID
	appendErr error                     // simulated store-side append failure
	appends   []string                  // resolved targets of append calls
}

func newFakeMemoryStore() *fakeMemoryStore {
	return &fakeMemoryStore{
		user:      make(map[string]*domain.Memory),
		workspace: make(map[string]*domain.Memory),
	}
}

func (f *fakeMemoryStore) UserMemory(_ context.Context, workspaceID, userID string) (*domain.Memory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.user[workspaceID+"|"+userID], nil
}

func (f *fakeMemoryStore) UpsertUserMemory(_ context.Context, workspaceID, userID, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.user[workspaceID+"|"+userID] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (f *fakeMemoryStore) AppendUserMemory(ctx context.Context, workspaceID, userID, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appendErr != nil {
		return f.appendErr
	}
	f.appends = append(f.appends, "user:"+workspaceID+"|"+userID)
	key := workspaceID + "|" + userID
	mem, ok := f.user[key]
	if !ok {
		f.user[key] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
		return nil
	}
	mem.Content += content
	return nil
}

func (f *fakeMemoryStore) WorkspaceMemory(_ context.Context, workspaceID string) (*domain.Memory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.workspace[workspaceID], nil
}

func (f *fakeMemoryStore) UpsertWorkspaceMemory(_ context.Context, workspaceID, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workspace[workspaceID] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (f *fakeMemoryStore) AppendWorkspaceMemory(_ context.Context, workspaceID, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appendErr != nil {
		return f.appendErr
	}
	f.appends = append(f.appends, "workspace:"+workspaceID)
	mem, ok := f.workspace[workspaceID]
	if !ok {
		f.workspace[workspaceID] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
		return nil
	}
	mem.Content += content
	return nil
}

// newMemoryToolForTest constructs the memory tool bound to an explicit
// identity, returning the concrete type so tests can reach both Info and
// InvokableRun.
func newMemoryToolForTest(t *testing.T, memories *fakeMemoryStore, workspaceID, userID string) *memoryTool {
	t.Helper()
	tl, err := NewMemory(memories, workspaceID, userID)
	if err != nil {
		t.Fatalf("NewMemory: %v", err)
	}
	return tl.(*memoryTool)
}

func TestResolveMemoryPath_AcceptedForms(t *testing.T) {
	ref, err := resolveMemoryPath("USER.md")
	if err != nil {
		t.Fatalf("USER.md: %v", err)
	}
	if ref.scope != memoryScopeUser || ref.document != "USER.md" {
		t.Errorf("USER.md: got scope=%q document=%q", ref.scope, ref.document)
	}

	ref, err = resolveMemoryPath("WORKSPACE.md")
	if err != nil {
		t.Fatalf("WORKSPACE.md: %v", err)
	}
	if ref.scope != memoryScopeWorkspace || ref.document != "WORKSPACE.md" {
		t.Errorf("WORKSPACE.md: got scope=%q document=%q", ref.scope, ref.document)
	}
}

// TestResolveMemoryPath_DailyPathsAreGone pins the takeout: the retired daily
// grammar (TODAY alias and strictly parsed dates) must be rejected like any
// other unknown path.
func TestResolveMemoryPath_DailyPathsAreGone(t *testing.T) {
	daily := []string{
		"MEMORY-TODAY.md",
		"MEMORY-08-09-2026.md",
		"MEMORY-8-9-2026.md",   // single digits: never accepted again
		"MEMORY-32-13-2026.md", // impossible calendar date
		"MEMORY-08-09-26.md",   // two-digit year
	}
	for _, path := range daily {
		_, err := resolveMemoryPath(path)
		if err == nil {
			t.Errorf("%q: expected rejection, got nil", path)
		}
	}
}

func TestResolveMemoryPath_RejectedFormsListAcceptedForms(t *testing.T) {
	rejected := []string{
		"MEMORY-TODAY.md",      // retired daily alias
		"MEMORY-08-09-2026.md", // retired daily date
		"MEMORY-2026/09/08.md", // wrong order / separators
		"user.md",              // case-sensitive exact names
		"MEMORY.md",
		"NOTES.md",
		"USER.md.bak",
		"",
	}
	for _, path := range rejected {
		_, err := resolveMemoryPath(path)
		if err == nil {
			t.Errorf("%q: expected rejection, got nil", path)
			continue
		}
		for _, form := range []string{"USER.md", "WORKSPACE.md"} {
			if !strings.Contains(err.Error(), form) {
				t.Errorf("%q: error must name the two accepted forms (missing %s): %v", path, form, err)
			}
		}
		if strings.Contains(err.Error(), "dd-mm-yyyy") {
			t.Errorf("%q: error must not present the retired daily grammar as accepted: %v", path, err)
		}
	}
}

func TestMemoryTool_InfoDescribesTwoDocuments(t *testing.T) {
	tool := newMemoryToolForTest(t, newFakeMemoryStore(), "ws-1", "user-1")
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	for _, form := range []string{"USER.md", "WORKSPACE.md"} {
		if !strings.Contains(info.Desc, form) {
			t.Errorf("description must cover %s: %q", form, info.Desc)
		}
	}
	if strings.Contains(info.Desc, "MEMORY-") {
		t.Errorf("description must not mention daily paths: %q", info.Desc)
	}
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("ToJSONSchema: %v", err)
	}
	pathParam, ok := js.Properties.Get("path")
	if !ok {
		t.Fatal("path parameter missing")
	}
	if pathParam.Description != "The memory document: USER.md or WORKSPACE.md." {
		t.Errorf("path parameter must list only the two accepted forms, got %q", pathParam.Description)
	}
	actionParam, ok := js.Properties.Get("action")
	if !ok {
		t.Fatal("action parameter missing")
	}
	if !strings.Contains(actionParam.Description, "read") || !strings.Contains(actionParam.Description, "append") {
		t.Errorf("action parameter must allow exactly read and append, got %q", actionParam.Description)
	}
}

func TestMemoryTool_DailyPathsAreGone(t *testing.T) {
	store := newFakeMemoryStore()
	tool := newMemoryToolForTest(t, store, "ws-1", "user-1")

	for _, args := range []string{
		`{"path":"MEMORY-TODAY.md","action":"read"}`,
		`{"path":"MEMORY-TODAY.md","action":"append","content":"Shipped v2."}`,
		`{"path":"MEMORY-08-09-2026.md","action":"append","content":"Shipped v2."}`,
	} {
		_, err := tool.InvokableRun(context.Background(), args)
		if err == nil {
			t.Fatalf("%s: expected structured error, got nil", args)
		}
		for _, form := range []string{"USER.md", "WORKSPACE.md"} {
			if !strings.Contains(err.Error(), form) {
				t.Errorf("%s: error must name the two accepted forms (missing %s): %v", args, form, err)
			}
		}
	}
	// Nothing may be stored by the rejected attempts.
	if len(store.user) != 0 || len(store.workspace) != 0 {
		t.Errorf("rejected daily paths must store nothing, got user=%v workspace=%v", store.user, store.workspace)
	}
	if len(store.appends) != 0 {
		t.Errorf("rejected daily paths must not append, got %v", store.appends)
	}
}

func TestMemoryTool_ReadEmptyReturnsMarker(t *testing.T) {
	store := newFakeMemoryStore()
	tool := newMemoryToolForTest(t, store, "ws-1", "user-1")
	out, err := tool.InvokableRun(context.Background(), `{"path":"USER.md","action":"read"}`)
	if err != nil {
		t.Fatalf("read USER.md: %v", err)
	}
	if !strings.Contains(out, memoryEmptyMarker) {
		t.Errorf("expected empty marker in %q", out)
	}

	out, err = tool.InvokableRun(context.Background(), `{"path":"WORKSPACE.md","action":"read"}`)
	if err != nil {
		t.Fatalf("read WORKSPACE.md: %v", err)
	}
	if !strings.Contains(out, memoryEmptyMarker) {
		t.Errorf("expected empty marker in %q", out)
	}
}

func TestMemoryTool_ReadReturnsStoredContent(t *testing.T) {
	store := newFakeMemoryStore()
	if err := store.UpsertUserMemory(context.Background(), "ws-1", "user-1", "Prefers concise answers."); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := newMemoryToolForTest(t, store, "ws-1", "user-1")
	out, err := tool.InvokableRun(context.Background(), `{"path":"USER.md","action":"read"}`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "Prefers concise answers.") {
		t.Errorf("expected stored content in %q", out)
	}
}

func TestMemoryTool_AppendWithoutContentFails(t *testing.T) {
	store := newFakeMemoryStore()
	tool := newMemoryToolForTest(t, store, "ws-1", "user-1")

	for _, args := range []string{
		`{"path":"USER.md","action":"append"}`,
		`{"path":"USER.md","action":"append","content":"   "}`,
	} {
		_, err := tool.InvokableRun(context.Background(), args)
		if err == nil || !strings.Contains(err.Error(), "content is required") {
			t.Errorf("%s: expected content-required error, got %v", args, err)
		}
	}
	if got := store.user["ws-1|user-1"]; got != nil {
		t.Errorf("nothing should be stored, got %q", got.Content)
	}
}

func TestMemoryTool_UnknownActionFails(t *testing.T) {
	tool := newMemoryToolForTest(t, newFakeMemoryStore(), "ws-1", "user-1")
	_, err := tool.InvokableRun(context.Background(), `{"path":"USER.md","action":"write","content":"x"}`)
	if err == nil || !strings.Contains(err.Error(), `action must be "read" or "append"`) {
		t.Errorf("expected action error, got %v", err)
	}
}

// TestMemoryTool_AppendConfirmsResolvedDocument covers the "Append
// confirmation names the date" scenario: the result confirms the concrete
// resolved document by its accepted name.
func TestMemoryTool_AppendConfirmsResolvedDocument(t *testing.T) {
	store := newFakeMemoryStore()
	tool := newMemoryToolForTest(t, store, "ws-1", "user-1")

	out, err := tool.InvokableRun(context.Background(), `{"path":"USER.md","action":"append","content":"Prefers concise answers."}`)
	if err != nil {
		t.Fatalf("append USER.md: %v", err)
	}
	if !strings.Contains(out, "Appended to USER.md") {
		t.Errorf("expected confirmation naming USER.md, got %q", out)
	}
	mem, _ := store.UserMemory(context.Background(), "ws-1", "user-1")
	if mem == nil || mem.Content != "Prefers concise answers." {
		t.Errorf("expected USER.md content stored, got %+v", mem)
	}

	out, err = tool.InvokableRun(context.Background(), `{"path":"WORKSPACE.md","action":"append","content":"Deploy window is Tuesday."}`)
	if err != nil {
		t.Fatalf("append WORKSPACE.md: %v", err)
	}
	if !strings.Contains(out, "Appended to WORKSPACE.md") {
		t.Errorf("expected confirmation naming WORKSPACE.md, got %q", out)
	}
}

func TestMemoryTool_AppendOverCapErrorsAndLeavesMemoryUnchanged(t *testing.T) {
	store := newFakeMemoryStore()
	filler := strings.Repeat("a", domain.MaxMemoryContentChars)
	if err := store.UpsertWorkspaceMemory(context.Background(), "ws-1", filler); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := newMemoryToolForTest(t, store, "ws-1", "user-1")

	_, err := tool.InvokableRun(context.Background(), `{"path":"WORKSPACE.md","action":"append","content":"x"}`)
	if err == nil || !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected cap-exceeded error, got %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", domain.MaxMemoryContentChars)) {
		t.Errorf("error must name the cap: %v", err)
	}
	mem, _ := store.WorkspaceMemory(context.Background(), "ws-1")
	if mem == nil || mem.Content != filler {
		t.Errorf("stored memory must be unchanged, got len %d", len(mem.Content))
	}
}

func TestMemoryTool_AppendStoreErrorSurfacesAsToolError(t *testing.T) {
	store := newFakeMemoryStore()
	store.appendErr = errors.New("disk full")
	tool := newMemoryToolForTest(t, store, "ws-1", "user-1")
	_, err := tool.InvokableRun(context.Background(), `{"path":"USER.md","action":"append","content":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("expected store error to surface, got %v", err)
	}
}

func TestMemoryTool_ScopingIsStructural(t *testing.T) {
	store := newFakeMemoryStore()
	alice := newMemoryToolForTest(t, store, "ws-1", "alice")
	bob := newMemoryToolForTest(t, store, "ws-1", "bob")

	if _, err := alice.InvokableRun(context.Background(), `{"path":"USER.md","action":"append","content":"Alice's note."}`); err != nil {
		t.Fatalf("append: %v", err)
	}

	out, err := bob.InvokableRun(context.Background(), `{"path":"USER.md","action":"read"}`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(out, "Alice's note.") {
		t.Errorf("bob must not see alice's memory: %q", out)
	}
	if !strings.Contains(out, memoryEmptyMarker) {
		t.Errorf("expected bob's empty marker, got %q", out)
	}
}

// ---------------------------------------------------------------------------
// memory.search (integrate-agent-zero-memory 4.3/4.4)
// ---------------------------------------------------------------------------

// seedSearchWorld builds the central in-memory store with a workspace, two
// members (Budi the caller, Sari the other member), and two agents, returning
// the ids the search-tool constructions need.
type searchWorld struct {
	store     store.Store
	workspace string
	budi      string
	sari      string
	atlas     string
	beacon    string
}

func seedSearchWorld(t *testing.T) *searchWorld {
	t.Helper()
	ctx := context.Background()
	s := fake.New()
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	budi := &domain.User{Email: "budi@example.com", Name: "Budi"}
	if err := s.Users().Create(ctx, budi); err != nil {
		t.Fatalf("seed budi: %v", err)
	}
	sari := &domain.User{Email: "sari@example.com", Name: "Sari"}
	if err := s.Users().Create(ctx, sari); err != nil {
		t.Fatalf("seed sari: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "Primary"}
	if err := s.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	atlas := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: prov.ID, Model: "gpt-test"}
	if err := s.Agents().Create(ctx, atlas); err != nil {
		t.Fatalf("seed atlas: %v", err)
	}
	beacon := &domain.Agent{WorkspaceID: ws.ID, Slug: "beacon", Name: "Beacon", ProviderID: prov.ID, Model: "gpt-test"}
	if err := s.Agents().Create(ctx, beacon); err != nil {
		t.Fatalf("seed beacon: %v", err)
	}
	return &searchWorld{store: s, workspace: ws.ID, budi: budi.ID, sari: sari.ID, atlas: atlas.ID, beacon: beacon.ID}
}

// seedNote inserts one complete provenance note with the given visibility
// owner fields.
func (w *searchWorld) seedNote(t *testing.T, visibility domain.MemoryVisibility, ownerID, content, topic string) domain.MemoryNote {
	t.Helper()
	now := time.Now().UTC().Add(-time.Hour)
	note := &domain.MemoryNote{
		WorkspaceID:   w.workspace,
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     now,
		LearnedAt:     now,
		SourceEventID: "ev-" + content[:8],
		Content:       content,
		Importance:    5,
	}
	if topic != "" {
		note.Topic = &topic
	}
	switch visibility {
	case domain.MemoryVisibilityUser:
		id := ownerID
		note.UserID = &id
	case domain.MemoryVisibilityAgent:
		id := ownerID
		note.AgentID = &id
	}
	if err := w.store.MemoryNotes().InsertNote(context.Background(), note, domain.MemoryVisibilityShared); err != nil {
		t.Fatalf("seed note %q: %v", content, err)
	}
	return *note
}

// seedEvent inserts one complete provenance gist row.
func (w *searchWorld) seedEvent(t *testing.T, visibility domain.MemoryVisibility, ownerID, description string) domain.MemoryEvent {
	t.Helper()
	now := time.Now().UTC().Add(-time.Hour)
	event := &domain.MemoryEvent{
		WorkspaceID:   w.workspace,
		AgentID:       w.atlas,
		SessionID:     "sess-1",
		TurnID:        "turn-1",
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     now,
		LearnedAt:     now,
		SourceEventID: "ev-" + description[:8],
		Description:   description,
		Outcome:       "resolved",
	}
	switch visibility {
	case domain.MemoryVisibilityUser:
		id := ownerID
		event.UserID = &id
	case domain.MemoryVisibilityAgent:
		// MemoryEvent.AgentID is the plain producing-agent id and doubles as
		// the agent-tier owner.
		event.AgentID = w.atlas
	}
	if err := w.store.MemoryEvents().InsertEvent(context.Background(), event); err != nil {
		t.Fatalf("seed event %q: %v", description, err)
	}
	return *event
}

func newSearchToolForTest(t *testing.T, w *searchWorld, userID, agentID string) *memorySearchTool {
	t.Helper()
	searcher := memory.NewSearcher(w.store.MemoryNotes(), w.store.MemoryEvents(), w.store.MemoryEmbeddings(), w.store.MemoryEntities(), w.store.SessionEvents(),
		memory.NewProviderEmbedder(w.store.Providers(), w.store.ToolSettings(), []byte("test-key-32-bytes-long-12345678"), providers.NewRegistry()))
	impl, err := NewMemorySearch(searcher, w.workspace, userID, agentID)
	if err != nil {
		t.Fatalf("NewMemorySearch: %v", err)
	}
	return impl.(*memorySearchTool)
}

// TestMemorySearch_ReadOnlyRoundTrip (task 4.3): the tool returns structured
// results carrying content and the provenance tuple, and writes nothing.
func TestMemorySearch_ReadOnlyRoundTrip(t *testing.T) {
	w := seedSearchWorld(t)
	ctx := context.Background()
	note := w.seedNote(t, domain.MemoryVisibilityShared, "", "The deploy window is Tuesday morning.", "")
	event := w.seedEvent(t, domain.MemoryVisibilityUser, w.budi, "Discussed the vendor renewal timeline.")

	notesBefore, _ := w.store.MemoryNotes().ListNotesForUI(ctx, w.workspace, w.budi, w.atlas, store.MemoryNoteFilters{History: true})
	eventsBefore, _ := w.store.MemoryEvents().ListEventsForUI(ctx, w.workspace, w.budi, w.atlas, store.MemoryEventFilters{})

	impl := newSearchToolForTest(t, w, w.budi, w.atlas)
	out, err := impl.InvokableRun(ctx, `{"query":"deploy window"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var result memorySearchResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode result %q: %v", out, err)
	}
	if len(result.Notes) != 1 || result.Notes[0].Content != note.Content {
		t.Fatalf("expected the shared note, got %+v", result.Notes)
	}
	if result.Notes[0].SourceEventID == "" || result.Notes[0].Origin != string(domain.MemoryOriginDialogue) {
		t.Fatalf("note result must carry provenance, got %+v", result.Notes[0])
	}
	if len(result.Events) != 0 {
		t.Fatalf("the vendor event must not match the deploy query, got %+v", result.Events)
	}

	// An identity-matched query also reaches the caller's own user rows.
	out, err = impl.InvokableRun(ctx, `{"query":"vendor renewal"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].ID != event.ID {
		t.Fatalf("expected the caller's own gist, got %+v", result.Events)
	}
	if result.Events[0].SourceEventID == "" || result.Events[0].Visibility != string(domain.MemoryVisibilityUser) {
		t.Fatalf("event result must carry provenance, got %+v", result.Events[0])
	}

	// Read-only: zero writes, zero model calls — the stores are unchanged.
	notesAfter, _ := w.store.MemoryNotes().ListNotesForUI(ctx, w.workspace, w.budi, w.atlas, store.MemoryNoteFilters{History: true})
	eventsAfter, _ := w.store.MemoryEvents().ListEventsForUI(ctx, w.workspace, w.budi, w.atlas, store.MemoryEventFilters{})
	if len(notesAfter) != len(notesBefore) || len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("search must not write: notes %d→%d events %d→%d",
			len(notesBefore), len(notesAfter), len(eventsBefore), len(eventsAfter))
	}
}

// TestMemorySearch_CannotCrossIdentity (task 4.4, spec: search cannot cross
// identity): another member's user rows and another agent's agent rows are
// excluded regardless of query content — the caller identity is structural,
// carried by the construction, never by the query.
func TestMemorySearch_CannotCrossIdentity(t *testing.T) {
	w := seedSearchWorld(t)
	w.seedNote(t, domain.MemoryVisibilityUser, w.sari, "Sari's private salary benchmark is 90000.", "")
	w.seedNote(t, domain.MemoryVisibilityAgent, w.beacon, "Beacon's escalation phrase is page the on-call now.", "")
	w.seedNote(t, domain.MemoryVisibilityShared, "", "The team deploy window is Tuesday morning.", "")
	w.seedNote(t, domain.MemoryVisibilityUser, w.budi, "Budi prefers concise summaries.", "")
	w.seedEvent(t, domain.MemoryVisibilityAgent, w.atlas, "Atlas rehearsed the failover drill.")

	impl := newSearchToolForTest(t, w, w.budi, w.atlas)

	for _, query := range []string{"salary benchmark is 90000", "escalation phrase is page the on-call", "Sari", "Beacon"} {
		out, err := impl.InvokableRun(context.Background(), `{"query":"`+query+`"}`)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		if strings.Contains(out, "90000") || strings.Contains(out, "page the on-call") || strings.Contains(out, "failover drill") {
			t.Fatalf("query %q leaked cross-identity rows: %s", query, out)
		}
	}

	// The caller's own visible set still answers.
	out, err := impl.InvokableRun(context.Background(), `{"query":"deploy window is Tuesday"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "deploy window is Tuesday") {
		t.Fatalf("shared rows must stay visible: %s", out)
	}
	out, err = impl.InvokableRun(context.Background(), `{"query":"concise summaries"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "Budi prefers concise") {
		t.Fatalf("own user rows must stay visible: %s", out)
	}
}

// TestMemorySearch_EmptyResultHints pins the abstention contract: zero hits
// are a structured result whose hint reads as "nothing is recorded".
func TestMemorySearch_EmptyResultHints(t *testing.T) {
	w := seedSearchWorld(t)
	impl := newSearchToolForTest(t, w, w.budi, w.atlas)

	out, err := impl.InvokableRun(context.Background(), `{"query":"quantum flux capacitor schedule"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var result memorySearchResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Notes) != 0 || len(result.Events) != 0 {
		t.Fatalf("expected a structured zero result, got %+v", result)
	}
	if result.Hint != memorySearchEmptyHint {
		t.Fatalf("expected the abstention hint, got %q", result.Hint)
	}
}

// TestMemorySearch_FiltersNarrow covers the agent-settable filter surface:
// visibility bucket, topic, and the time window.
func TestMemorySearch_FiltersNarrow(t *testing.T) {
	w := seedSearchWorld(t)
	w.seedNote(t, domain.MemoryVisibilityShared, "", "The launch date moved to October.", "launch")
	w.seedNote(t, domain.MemoryVisibilityShared, "", "The staging database resets nightly.", "infra")
	w.seedEvent(t, domain.MemoryVisibilityShared, "", "The billing provider outage was resolved.")

	impl := newSearchToolForTest(t, w, w.budi, w.atlas)

	out, err := impl.InvokableRun(context.Background(), `{"query":"staging database","visibility":"user"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if strings.Contains(out, "staging database") {
		t.Fatalf("a shared row filtered to the user tier must disappear: %s", out)
	}

	out, err = impl.InvokableRun(context.Background(), `{"query":"launch date moved","topic":"infra"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if strings.Contains(out, "launch date") {
		t.Fatalf("the topic filter must exclude other topics: %s", out)
	}

	out, err = impl.InvokableRun(context.Background(), `{"query":"billing provider outage","from":"2030-01-01T00:00:00Z"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if strings.Contains(out, "billing") {
		t.Fatalf("the time window must exclude rows before it: %s", out)
	}
}

// TestMemorySearch_StructuredErrors covers the argument contract: missing
// query, unknown visibility bucket, and malformed time bounds all fail with
// errors naming the expectation.
func TestMemorySearch_StructuredErrors(t *testing.T) {
	w := seedSearchWorld(t)
	impl := newSearchToolForTest(t, w, w.budi, w.atlas)

	for _, tc := range []struct {
		args    string
		message string
	}{
		{`{"query":"  "}`, "query is required"},
		{`{}`, "query is required"},
		{`{"query":"x","visibility":"world"}`, `visibility must be "shared", "user", or "agent"`},
		{`{"query":"x","from":"tuesday"}`, "from must be an RFC3339 timestamp"},
		{`{"query":"x","until":"2020-13-45T99:00:00Z"}`, "until must be an RFC3339 timestamp"},
	} {
		_, err := impl.InvokableRun(context.Background(), tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Errorf("%s: expected error containing %q, got %v", tc.args, tc.message, err)
		}
	}
}

// TestMemorySearch_EntityFilterTraverses (wave3 task 5.2, spec: entity
// filter traverses without leaking): the optional entity argument returns the
// entity's linked rows fused with the query results, visible-set only, and a
// pure entity call (no query) runs pure traversal; both empty is a
// structured error.
func TestMemorySearch_EntityFilterTraverses(t *testing.T) {
	w := seedSearchWorld(t)
	ctx := context.Background()

	entity := &domain.MemoryEntity{
		WorkspaceID:     w.workspace,
		Label:           "ProjectX",
		NormalizedLabel: domain.NormalizeEntityLabel("ProjectX"),
		Origin:          domain.MemoryOriginDialogue,
		SourceEventID:   "ev-entity",
		LearnedAt:       time.Now().UTC().Add(-time.Hour),
	}
	if err := w.store.MemoryEntities().ResolveEntity(ctx, entity); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	linked := w.seedNote(t, domain.MemoryVisibilityShared, "", "unlinked phrasing zebra", "")
	saris := w.seedNote(t, domain.MemoryVisibilityUser, w.sari, "ProjectX private zebra of Sari", "")
	lexical := w.seedNote(t, domain.MemoryVisibilityShared, "", "the zebra lexicon", "")
	for _, edge := range []domain.MemoryEntityEdge{
		{EntityID: entity.ID, TargetType: domain.MemoryTargetNote, TargetID: linked.ID, Visibility: domain.MemoryVisibilityShared, Origin: domain.MemoryOriginDialogue, SourceEventID: "ev-edge"},
		{EntityID: entity.ID, TargetType: domain.MemoryTargetNote, TargetID: saris.ID, Visibility: domain.MemoryVisibilityUser, Origin: domain.MemoryOriginDialogue, SourceEventID: "ev-edge"},
	} {
		if _, err := w.store.MemoryEntities().AddEdges(ctx, w.workspace, []domain.MemoryEntityEdge{edge}); err != nil {
			t.Fatalf("seed edge: %v", err)
		}
	}

	impl := newSearchToolForTest(t, w, w.budi, w.atlas)

	// Entity + query: the traversed row and the lexical row both surface,
	// Sari's private row never does.
	out, err := impl.InvokableRun(ctx, `{"query":"zebra","entity":"ProjectX"}`)
	if err != nil {
		t.Fatalf("entity+query search: %v", err)
	}
	var result memorySearchResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode result %q: %v", out, err)
	}
	seen := map[string]bool{}
	for _, n := range result.Notes {
		seen[n.ID] = true
	}
	if !seen[linked.ID] || !seen[lexical.ID] {
		t.Fatalf("entity+query must surface the traversed and lexical rows, got %+v", result.Notes)
	}
	if seen[saris.ID] {
		t.Fatalf("traversal leaked Sari's user-visibility row: %+v", result.Notes)
	}

	// Entity only: pure traversal, no query needed.
	out, err = impl.InvokableRun(ctx, `{"entity":"projectx"}`)
	if err != nil {
		t.Fatalf("entity-only search: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Notes) != 1 || result.Notes[0].ID != linked.ID {
		t.Fatalf("entity-only must traverse to the visible linked row, got %+v", result.Notes)
	}
	if result.Notes[0].SourceEventID == "" || result.Notes[0].Visibility != string(domain.MemoryVisibilityShared) {
		t.Fatalf("traversed rows carry the same provenance tuple, got %+v", result.Notes[0])
	}

	// Neither query nor entity: the structured argument error.
	if _, err := impl.InvokableRun(ctx, `{}`); err == nil || !strings.Contains(err.Error(), "query is required unless entity is provided") {
		t.Fatalf("expected the both-empty argument error, got %v", err)
	}
}
