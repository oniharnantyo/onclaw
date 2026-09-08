package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// fakeMemoryStore is a minimal MemoryStore for tool tests: maps keyed by the
// composite identity, Get returning (nil, nil) when nothing is stored.
type fakeMemoryStore struct {
	mu        sync.Mutex
	user      map[string]*domain.Memory // key: workspaceID + "|" + userID
	workspace map[string]*domain.Memory // key: workspaceID
	daily     map[string]*domain.Memory // key: workspaceID + "|" + agentID + "|" + 2006-01-02
	appendErr error                     // simulated store-side append failure
	appends   []string                  // resolved targets of append calls
}

func newFakeMemoryStore() *fakeMemoryStore {
	return &fakeMemoryStore{
		user:      make(map[string]*domain.Memory),
		workspace: make(map[string]*domain.Memory),
		daily:     make(map[string]*domain.Memory),
	}
}

func dailyKey(workspaceID, agentID string, date time.Time) string {
	return workspaceID + "|" + agentID + "|" + date.UTC().Format("2006-01-02")
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

func (f *fakeMemoryStore) AgentDailyMemory(_ context.Context, workspaceID, agentID string, date time.Time) (*domain.Memory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.daily[dailyKey(workspaceID, agentID, date)], nil
}

func (f *fakeMemoryStore) UpsertAgentDailyMemory(_ context.Context, workspaceID, agentID string, date time.Time, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.daily[dailyKey(workspaceID, agentID, date)] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (f *fakeMemoryStore) AppendAgentDailyMemory(_ context.Context, workspaceID, agentID string, date time.Time, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appendErr != nil {
		return f.appendErr
	}
	f.appends = append(f.appends, "daily:"+dailyKey(workspaceID, agentID, date))
	key := dailyKey(workspaceID, agentID, date)
	mem, ok := f.daily[key]
	if !ok {
		f.daily[key] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
		return nil
	}
	mem.Content += content
	return nil
}

// newMemoryToolForTest constructs the memory tool bound to an explicit
// identity, returning it through the package's invokable test seam.
func newMemoryToolForTest(t *testing.T, memories *fakeMemoryStore, workspaceID, agentID, userID string, tz *time.Location) invokable {
	t.Helper()
	tl, err := NewMemory(memories, workspaceID, agentID, userID, tz)
	if err != nil {
		t.Fatalf("NewMemory: %v", err)
	}
	return tl.(invokable)
}

func TestResolveMemoryPath_AcceptedForms(t *testing.T) {
	tz := time.UTC

	ref, err := resolveMemoryPath("USER.md", tz)
	if err != nil {
		t.Fatalf("USER.md: %v", err)
	}
	if ref.scope != memoryScopeUser || ref.document != "USER.md" {
		t.Errorf("USER.md: got scope=%q document=%q", ref.scope, ref.document)
	}

	ref, err = resolveMemoryPath("WORKSPACE.md", tz)
	if err != nil {
		t.Fatalf("WORKSPACE.md: %v", err)
	}
	if ref.scope != memoryScopeWorkspace || ref.document != "WORKSPACE.md" {
		t.Errorf("WORKSPACE.md: got scope=%q document=%q", ref.scope, ref.document)
	}

	ref, err = resolveMemoryPath("MEMORY-08-09-2026.md", tz)
	if err != nil {
		t.Fatalf("MEMORY-08-09-2026.md: %v", err)
	}
	if ref.scope != memoryScopeDaily {
		t.Errorf("dated path: got scope %q", ref.scope)
	}
	if got := ref.date.UTC().Format("2006-01-02"); got != "2026-09-08" {
		t.Errorf("dated path: expected 2026-09-08, got %s", got)
	}
	if ref.document != "MEMORY-08-09-2026.md" {
		t.Errorf("dated path: expected canonical document, got %q", ref.document)
	}
}

func TestResolveMemoryPath_TodayResolvesInWorkspaceTZ(t *testing.T) {
	// A zone whose local date differs from UTC for most of the day.
	tz, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	ref, err := resolveMemoryPath("MEMORY-TODAY.md", tz)
	if err != nil {
		t.Fatalf("MEMORY-TODAY.md: %v", err)
	}
	now := time.Now().In(tz)
	want := "MEMORY-" + now.Format("02-01-2006") + ".md"
	if ref.document != want {
		t.Errorf("expected TODAY to resolve to %q in workspace TZ, got %q", want, ref.document)
	}
	// The stored date must be the workspace-local calendar date: its UTC
	// y/m/d equal the local y/m/d (the store keys daily rows by UTC date).
	if ref.date.UTC().Format("2006-01-02") != now.Format("2006-01-02") {
		t.Errorf("daily date %s does not match workspace-local date %s",
			ref.date.UTC().Format("2006-01-02"), now.Format("2006-01-02"))
	}
}

func TestResolveMemoryPath_RejectedFormsListAcceptedPaths(t *testing.T) {
	rejected := []string{
		"MEMORY-2026/09/08.md", // wrong order / separators
		"user.md",              // case-sensitive exact names
		"MEMORY.md",
		"NOTES.md",
		"MEMORY-8-9-2026.md",   // single digits: strict dd-mm-yyyy only
		"MEMORY-32-13-2026.md", // impossible calendar date
		"MEMORY-08-09-26.md",   // two-digit year
		"USER.md.bak",
		"",
	}
	for _, path := range rejected {
		_, err := resolveMemoryPath(path, time.UTC)
		if err == nil {
			t.Errorf("%q: expected rejection, got nil", path)
			continue
		}
		for _, form := range []string{"USER.md", "WORKSPACE.md", "MEMORY-TODAY.md", "MEMORY-DD-MM-YYYY.md"} {
			if !strings.Contains(err.Error(), form) {
				t.Errorf("%q: error must list accepted forms (missing %s): %v", path, form, err)
			}
		}
	}
}

func TestMemoryTool_ReadEmptyReturnsMarker(t *testing.T) {
	store := newFakeMemoryStore()
	tool := newMemoryToolForTest(t, store, "ws-1", "agent-1", "user-1", time.UTC)
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
	tool := newMemoryToolForTest(t, store, "ws-1", "agent-1", "user-1", time.UTC)
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
	tool := newMemoryToolForTest(t, store, "ws-1", "agent-1", "user-1", time.UTC)

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
	tool := newMemoryToolForTest(t, newFakeMemoryStore(), "ws-1", "agent-1", "user-1", time.UTC)
	_, err := tool.InvokableRun(context.Background(), `{"path":"USER.md","action":"write","content":"x"}`)
	if err == nil || !strings.Contains(err.Error(), `action must be "read" or "append"`) {
		t.Errorf("expected action error, got %v", err)
	}
}

func TestMemoryTool_AppendConfirmsResolvedDailyDocument(t *testing.T) {
	store := newFakeMemoryStore()
	tool := newMemoryToolForTest(t, store, "ws-1", "agent-1", "user-1", time.UTC)

	out, err := tool.InvokableRun(context.Background(), `{"path":"MEMORY-TODAY.md","action":"append","content":"Shipped v2."}`)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	want := "Appended to MEMORY-" + time.Now().UTC().Format("02-01-2006") + ".md"
	if !strings.Contains(out, want) {
		t.Errorf("expected confirmation naming %q, got %q", want, out)
	}

	// Two appends on the same day land in the single daily document.
	if _, err := tool.InvokableRun(context.Background(), `{"path":"MEMORY-TODAY.md","action":"append","content":" Again."}`); err != nil {
		t.Fatalf("second append: %v", err)
	}
	mem, err := store.AgentDailyMemory(context.Background(), "ws-1", "agent-1",
		time.Date(time.Now().UTC().Year(), time.Now().UTC().Month(), time.Now().UTC().Day(), 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("daily read: %v", err)
	}
	if mem == nil || mem.Content != "Shipped v2. Again." {
		t.Errorf("expected both appends in one daily document, got %+v", mem)
	}
}

func TestMemoryTool_AppendOverCapErrorsAndLeavesMemoryUnchanged(t *testing.T) {
	store := newFakeMemoryStore()
	filler := strings.Repeat("a", domain.MaxMemoryContentChars)
	if err := store.UpsertWorkspaceMemory(context.Background(), "ws-1", filler); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := newMemoryToolForTest(t, store, "ws-1", "agent-1", "user-1", time.UTC)

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
	tool := newMemoryToolForTest(t, store, "ws-1", "agent-1", "user-1", time.UTC)
	_, err := tool.InvokableRun(context.Background(), `{"path":"USER.md","action":"append","content":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("expected store error to surface, got %v", err)
	}
}

func TestMemoryTool_ScopingIsStructural(t *testing.T) {
	store := newFakeMemoryStore()
	alice := newMemoryToolForTest(t, store, "ws-1", "agent-1", "alice", time.UTC)
	bob := newMemoryToolForTest(t, store, "ws-1", "agent-1", "bob", time.UTC)

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
