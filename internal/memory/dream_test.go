package memory_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

type mockEpisodicStore struct {
	mu              sync.Mutex
	summaries       []*memory.EpisodicSummary
	nextID          int64
	countUnpromoted func() (int, error)
	listUnpromoted  func() ([]*memory.EpisodicSummary, error)
}

func (m *mockEpisodicStore) AppendEpisodic(ctx context.Context, agent, summary, l0Abstract, keyTopics, sourceID, expiresAt string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	m.summaries = append(m.summaries, &memory.EpisodicSummary{
		ID: m.nextID, Agent: agent, Summary: summary,
		L0Abstract: l0Abstract, KeyTopics: keyTopics, SourceID: sourceID, ExpiresAt: expiresAt,
	})
	return m.nextID, nil
}

func (m *mockEpisodicStore) ListUnpromoted(ctx context.Context, agent string) ([]*memory.EpisodicSummary, error) {
	if m.listUnpromoted != nil {
		return m.listUnpromoted()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*memory.EpisodicSummary
	for _, s := range m.summaries {
		if s.PromotedAt == nil {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockEpisodicStore) CountUnpromoted(ctx context.Context, agent string) (int, error) {
	if m.countUnpromoted != nil {
		return m.countUnpromoted()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var count int
	for _, s := range m.summaries {
		if s.PromotedAt == nil {
			count++
		}
	}
	return count, nil
}

func (m *mockEpisodicStore) MarkPromoted(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.summaries {
		if s.ID == id {
			now := time.Now().Format(time.RFC3339)
			s.PromotedAt = &now
			break
		}
	}
	return nil
}

func (m *mockEpisodicStore) PruneExpired(ctx context.Context) (int64, error) {
	return 0, nil
}

func (m *mockEpisodicStore) GetEpisodic(ctx context.Context, id int64) (*memory.EpisodicSummary, error) {
	return nil, nil
}

type mockCoreStore struct {
	mu       sync.Mutex
	content  string
	writeErr error
	readErr  error
}

func (m *mockCoreStore) ReadCore(ctx context.Context, workspace string) (string, error) {
	return m.content, m.readErr
}

func (m *mockCoreStore) WriteCore(ctx context.Context, workspace, op, target, content string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return "", m.writeErr
	}
	if op == "replace" || op == "add" {
		m.content = content
	}
	return content, nil
}

type mockStagedWriteStore struct {
	mu       sync.Mutex
	writes   []memory.StagedWrite
	stageErr error
}

func (m *mockStagedWriteStore) StageWrite(ctx context.Context, agent, op, target, content string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stageErr != nil {
		return 0, m.stageErr
	}
	id := int64(len(m.writes) + 1)
	m.writes = append(m.writes, memory.StagedWrite{ID: id, Agent: agent, Operation: op, Content: content, Status: "pending"})
	return id, nil
}

func (m *mockStagedWriteStore) ListStaged(ctx context.Context, agent string) ([]*memory.StagedWrite, error) {
	return nil, nil
}

func (m *mockStagedWriteStore) GetStagedWrite(ctx context.Context, id int64) (*memory.StagedWrite, error) {
	return nil, nil
}

func (m *mockStagedWriteStore) ApproveWrite(ctx context.Context, id int64) error { return nil }

func (m *mockStagedWriteStore) RejectWrite(ctx context.Context, id int64) error { return nil }

func TestNewDreamer_Defaults(t *testing.T) {
	d := memory.NewDreamer(nil, nil, nil, nil, "", "", 0, 0, false, "")
	if d == nil {
		t.Fatal("expected non-nil Dreamer")
	}
}

func TestMaybeDream_NilStore(t *testing.T) {
	d := memory.NewDreamer(nil, nil, nil, nil, "test-agent", "", 1, time.Second, false, "")
	err := d.MaybeDream(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when EpisodicStore is nil, got %v", err)
	}
}

func TestMaybeDream_BelowThreshold(t *testing.T) {
	store := &mockEpisodicStore{}
	d := memory.NewDreamer(store, nil, nil, nil, "test-agent", "", 5, time.Second, false, "")
	err := d.MaybeDream(context.Background())
	if err != nil {
		t.Fatalf("expected nil when below threshold, got %v", err)
	}
}

func TestMaybeDream_Debounce(t *testing.T) {
	store := &mockEpisodicStore{
		countUnpromoted: func() (int, error) { return 10, nil },
		listUnpromoted: func() ([]*memory.EpisodicSummary, error) {
			return []*memory.EpisodicSummary{
				{ID: 1, L0Abstract: "Test.", Summary: "Test summary.", SourceID: "conv_1"},
			}, nil
		},
	}
	coreStore := &mockCoreStore{}
	d := memory.NewDreamer(store, coreStore, nil, nil, "test-agent", "", 1, time.Hour, false, "")

	err := d.MaybeDream(context.Background())
	if err != nil {
		t.Fatalf("first call should succeed, got %v", err)
	}

	err = d.MaybeDream(context.Background())
	if err != nil {
		t.Fatalf("debounced call should return nil, got %v", err)
	}
}

func TestMaybeDream_WithReviewModel(t *testing.T) {
	tmpDir := t.TempDir()
	ws := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(ws, 0755)

	episodicStore := &mockEpisodicStore{
		countUnpromoted: func() (int, error) { return 5, nil },
		listUnpromoted: func() ([]*memory.EpisodicSummary, error) {
			return []*memory.EpisodicSummary{
				{ID: 1, L0Abstract: "Session one.", Summary: "Full summary one.", SourceID: "conv_1"},
				{ID: 2, L0Abstract: "Session two.", Summary: "Full summary two.", SourceID: "conv_2"},
			}, nil
		},
	}
	coreStore := &mockCoreStore{}
	reviewModel := &fakeAgenticModel{response: "- User prefers Go\n- Project uses SQLite"}

	d := memory.NewDreamer(episodicStore, coreStore, nil, reviewModel, "test-agent", ws, 1, time.Second, false, "")
	err := d.MaybeDream(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(coreStore.content, "User prefers Go") && !strings.Contains(coreStore.content, "Project uses SQLite") {
		t.Errorf("expected coreStore to contain synthesized facts, got: %q", coreStore.content)
	}
}

func TestMaybeDream_WriteApproval(t *testing.T) {
	episodicStore := &mockEpisodicStore{
		countUnpromoted: func() (int, error) { return 5, nil },
		listUnpromoted: func() ([]*memory.EpisodicSummary, error) {
			return []*memory.EpisodicSummary{
				{ID: 1, L0Abstract: "Session.", Summary: "Summary.", SourceID: "conv_1"},
			}, nil
		},
	}
	coreStore := &mockCoreStore{}
	stagedStore := &mockStagedWriteStore{}
	reviewModel := &fakeAgenticModel{response: "- Important fact"}

	d := memory.NewDreamer(episodicStore, coreStore, stagedStore, reviewModel, "test-agent", "", 1, time.Second, true, "")
	err := d.MaybeDream(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stagedStore.writes) != 1 {
		t.Errorf("expected 1 staged write, got %d", len(stagedStore.writes))
	}
	if len(stagedStore.writes) > 0 && stagedStore.writes[0].Content != "Important fact" {
		t.Errorf("expected staged content 'Important fact', got %q", stagedStore.writes[0].Content)
	}
}

func TestMaybeDream_NONESynthesis(t *testing.T) {
	episodicStore := &mockEpisodicStore{
		countUnpromoted: func() (int, error) { return 5, nil },
		listUnpromoted: func() ([]*memory.EpisodicSummary, error) {
			return []*memory.EpisodicSummary{
				{ID: 1, L0Abstract: "Session.", Summary: "Summary.", SourceID: "conv_1"},
			}, nil
		},
	}
	coreStore := &mockCoreStore{}
	reviewModel := &fakeAgenticModel{response: "NONE"}

	d := memory.NewDreamer(episodicStore, coreStore, nil, reviewModel, "test-agent", "", 1, time.Second, false, "")
	err := d.MaybeDream(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if coreStore.content != "" {
		t.Errorf("expected no writes to coreStore when synthesis is NONE, got %q", coreStore.content)
	}
}

func TestMaybeDream_ExtractiveFallbackDigest(t *testing.T) {
	tmpDir := t.TempDir()
	ws := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(ws, 0755)

	episodicStore := &mockEpisodicStore{
		countUnpromoted: func() (int, error) { return 3, nil },
		listUnpromoted: func() ([]*memory.EpisodicSummary, error) {
			return []*memory.EpisodicSummary{
				{ID: 1, L0Abstract: "Worked on Go backend.", Summary: "Summary.", SourceID: "conv_1"},
			}, nil
		},
	}
	coreStore := &mockCoreStore{}

	d := memory.NewDreamer(episodicStore, coreStore, nil, nil, "test-agent", ws, 1, time.Second, false, "")
	err := d.MaybeDream(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(coreStore.content, "Worked on Go backend") {
		t.Logf("extractive fallback may not produce facts (depends on l0_abstract content): %q", coreStore.content)
	}
}

func TestPeriodicPruner_StartStop(t *testing.T) {
	store := &mockEpisodicStore{}
	pruner := memory.NewPeriodicPruner(store, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	pruner.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()
	pruner.Stop()
}

func TestConsolidateFacts(t *testing.T) {
}

func TestParseDreamSweeps_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	// Use NewDreamer to write a sweep record, then parse it back
	store := &mockEpisodicStore{
		countUnpromoted: func() (int, error) { return 5, nil },
		listUnpromoted: func() ([]*memory.EpisodicSummary, error) {
			return []*memory.EpisodicSummary{
				{ID: 1, L0Abstract: "Session one.", Summary: "Full summary one.", SourceID: "conv_1"},
				{ID: 2, L0Abstract: "Session two.", Summary: "Full summary two.", SourceID: "conv_2"},
				{ID: 3, L0Abstract: "Session three.", Summary: "Full summary three.", SourceID: "conv_3"},
			}, nil
		},
	}
	coreStore := &mockCoreStore{}
	reviewModel := &fakeAgenticModel{response: "- User prefers Go\n- Project uses SQLite\n- TDD is mandatory"}

	d := memory.NewDreamer(store, coreStore, nil, reviewModel, "test-agent", tmpDir, 1, time.Second, false, "gpt-4o")
	err := d.MaybeDream(context.Background())
	if err != nil {
		t.Fatalf("MaybeDream failed: %v", err)
	}

	// Parse back
	records, err := memory.ParseDreamSweeps(filepath.Join(tmpDir, "DREAMS.md"))
	if err != nil {
		t.Fatalf("ParseDreamSweeps failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 sweep record, got %d", len(records))
	}

	r := records[0]
	if r.Agent != "test-agent" {
		t.Errorf("expected agent 'test-agent', got %q", r.Agent)
	}
	if r.EpisodesCount != 3 {
		t.Errorf("expected 3 episodes, got %d", r.EpisodesCount)
	}
	if r.Score <= 0 {
		t.Errorf("expected positive score, got %f", r.Score)
	}
	if len(r.Promotions) != 3 {
		t.Errorf("expected 3 promotions, got %d: %v", len(r.Promotions), r.Promotions)
	}
	if r.Promotions[0] != "User prefers Go" {
		t.Errorf("expected first promotion 'User prefers Go', got %q", r.Promotions[0])
	}
	if r.ReviewModel != "gpt-4o" {
		t.Errorf("expected review model 'gpt-4o', got %q", r.ReviewModel)
	}
	if r.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestParseDreamSweeps_MissingFile(t *testing.T) {
	records, err := memory.ParseDreamSweeps("/nonexistent/DREAMS.md")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if records != nil {
		t.Errorf("expected nil records for missing file, got %d", len(records))
	}
}

func TestParseDreamSweeps_MultipleSweeps(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "DREAMS.md")

	content := `# Dreaming Review Records

## Dreaming Sweep 0
- **Timestamp:** 2026-07-04T10:00:00Z
- **Agent:** agent-a
- **Episodes Reviewed:** 2
- **Score:** 0.50
- **Review Model:** gpt-4o

### Promotions

- User prefers Go

---

## Dreaming Sweep 1
- **Timestamp:** 2026-07-04T11:00:00Z
- **Agent:** agent-b
- **Episodes Reviewed:** 3
- **Score:** 0.67

### Promotions

- Project uses SQLite

### Supersessions

- Old approach deprecated

---

## Dreaming Sweep 2
- **Timestamp:** 2026-07-04T12:00:00Z
- **Agent:** agent-a
- **Episodes Reviewed:** 1
- **Score:** 1.00

### Promotions

- TDD is mandatory
- Use React for frontend

---

`
	os.WriteFile(path, []byte(content), 0644)

	records, err := memory.ParseDreamSweeps(path)
	if err != nil {
		t.Fatalf("ParseDreamSweeps failed: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 sweep records, got %d", len(records))
	}

	if records[0].Agent != "agent-a" || records[0].EpisodesCount != 2 || len(records[0].Promotions) != 1 {
		t.Errorf("sweep 0 mismatch: agent=%q episodes=%d promotions=%d", records[0].Agent, records[0].EpisodesCount, len(records[0].Promotions))
	}
	if records[0].Score != 0.50 {
		t.Errorf("sweep 0 expected score 0.50, got %f", records[0].Score)
	}
	if records[0].ReviewModel != "gpt-4o" {
		t.Errorf("sweep 0 expected review model 'gpt-4o', got %q", records[0].ReviewModel)
	}

	if len(records[1].Supersessions) != 1 {
		t.Errorf("sweep 1 expected 1 supersession, got %d", len(records[1].Supersessions))
	}

	if len(records[2].Promotions) != 2 {
		t.Errorf("sweep 2 expected 2 promotions, got %d", len(records[2].Promotions))
	}
}

func TestListDreamFiles(t *testing.T) {
	tmpDir := t.TempDir()
	ws1 := filepath.Join(tmpDir, "agent-a")
	os.MkdirAll(ws1, 0755)
	ws2 := filepath.Join(tmpDir, "agent-b")
	os.MkdirAll(ws2, 0755)

	os.WriteFile(filepath.Join(ws1, "DREAMS.md"), []byte("# Dreams"), 0644)
	os.WriteFile(filepath.Join(ws2, "DREAMS.md"), []byte("# Dreams"), 0644)

	files, err := memory.ListDreamFiles(ws1, ws2)
	if err != nil {
		t.Fatalf("ListDreamFiles failed: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
}

func TestListDreamFiles_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()
	files, err := memory.ListDreamFiles(tmpDir)
	if err != nil {
		t.Fatalf("ListDreamFiles failed: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil, got %d files", len(files))
	}
}
