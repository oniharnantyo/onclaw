package memory

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---- shared test helpers ----

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// Identity coordinates the helpers reference. seedWorld fills them from the
// fake store's assigned ids; tests run sequentially (no t.Parallel), so the
// per-test reset below is race-free.
var (
	testWorkspaceID string
	testUserID      string
	testAgentID     string
	testSessionID   = "sess-1"
)

// seedWorld creates the workspace, user, and agent the fake store's write
// validation requires, returning the live store.
func seedWorld(t *testing.T) store.Store {
	t.Helper()
	s := fake.New()
	ctx := context.Background()
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	user := &domain.User{Email: "alice@example.com", Name: "Alice"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := s.Users().Create(ctx, &domain.User{Email: "bob@example.com", Name: "Bob"}); err != nil {
		t.Fatalf("seed second user: %v", err)
	}
	provider := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "Primary"}
	if err := s.Providers().Create(ctx, provider); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	agent := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  provider.ID,
		Model:       "gpt-test",
	}
	if err := s.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	testWorkspaceID = ws.ID
	testUserID = user.ID
	testAgentID = agent.ID
	return s
}

func secondUserID(t *testing.T, s store.Store) string {
	t.Helper()
	users, err := s.Users().List(context.Background())
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	for _, u := range users {
		if u.ID != testUserID {
			return u.ID
		}
	}
	t.Fatal("second user missing")
	return ""
}

// testJob is the default direct-chat job shape (one human).
func testJob() IngestJob {
	return IngestJob{
		WorkspaceID:       testWorkspaceID,
		AgentID:           testAgentID,
		UserID:            testUserID,
		SessionID:         testSessionID,
		TurnID:            "turn-1",
		Origin:            originUser,
		HumanParticipants: 1,
		Status:            "completed",
	}
}

// chatEvent builds one raw session event whose payload round-trips through
// the same serializer the ADK adapter persists with.
func chatEvent(t *testing.T, eventID, turnID string, seq int64, at time.Time, role schema.AgenticRoleType, text string) domain.SessionEvent {
	t.Helper()
	var msg *schema.AgenticMessage
	if role == schema.AgenticRoleTypeUser {
		msg = schema.UserAgenticMessage(text)
	} else {
		msg = &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{{AssistantGenText: &schema.AssistantGenText{Text: text}}}}
	}
	payload, err := (&schema.HumanReadableSerializer{}).Marshal(&adk.SessionEvent[*schema.AgenticMessage]{
		EventID:   eventID,
		TurnID:    turnID,
		Timestamp: at,
		Kind:      adk.SessionEventMessage,
		Message:   msg,
	})
	if err != nil {
		t.Fatalf("serialize session event: %v", err)
	}
	return domain.SessionEvent{
		SessionID:   testSessionID,
		EventID:     eventID,
		TurnID:      turnID,
		Seq:         seq,
		Kind:        string(adk.SessionEventMessage),
		Payload:     payload,
		OccurredAt:  at,
		WorkspaceID: testWorkspaceID,
	}
}

func appendEvents(t *testing.T, s store.Store, events ...domain.SessionEvent) {
	t.Helper()
	if err := s.SessionEvents().AppendEvents(context.Background(), testWorkspaceID, events); err != nil {
		t.Fatalf("append session events: %v", err)
	}
}

// scriptedModel answers Generates from a scripted response queue, records
// every call's input texts, and can be pointed at a permanent error.
type scriptedModel struct {
	mu        sync.Mutex
	responses []string
	err       error
	inputs    [][]string
}

func (m *scriptedModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	texts := make([]string, 0, len(input))
	for _, msg := range input {
		if msg == nil {
			continue
		}
		var sb strings.Builder
		for _, block := range msg.ContentBlocks {
			if block == nil {
				continue
			}
			if block.AssistantGenText != nil {
				sb.WriteString(block.AssistantGenText.Text)
			}
			if block.UserInputText != nil {
				sb.WriteString(block.UserInputText.Text)
			}
		}
		texts = append(texts, sb.String())
	}
	m.inputs = append(m.inputs, texts)
	if m.err != nil {
		return nil, m.err
	}
	resp := ""
	if len(m.responses) > 0 {
		resp = m.responses[0]
		m.responses = m.responses[1:]
	}
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{{AssistantGenText: &schema.AssistantGenText{Text: resp}}}}, nil
}

func (m *scriptedModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, errors.New("scriptedModel: stream not supported")
}

func (m *scriptedModel) callInputs() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]string, len(m.inputs))
	copy(out, m.inputs)
	return out
}

func staticResolver(m Model) ModelResolver {
	return func(context.Context, string, string) (Model, error) { return m, nil }
}

func failingResolver(string) ModelResolver {
	return func(context.Context, string, string) (Model, error) { return nil, errors.New("model provider down") }
}

func newTestGister(s store.Store, m Model) *Gister {
	return NewGister(s.MemoryEvents(), s.SessionEvents(), nil, nil, nil, WithModelResolver(staticResolver(m)))
}

func newTestGate(s store.Store, m Model) *Gate {
	return NewGate(s.MemoryNotes(), s.Memories(), nil, nil, nil, testLogger, WithModelResolver(staticResolver(m)))
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

// ---- worker tests ----

// TestWorkerChipPayloadShape pins the chip contract: the empty payload must
// marshal to exactly the agreed wire shape (counts + ids, never content).
func TestWorkerChipPayloadShape(t *testing.T) {
	payload := MemoryIngestedPayload{NoteIDs: []string{}, EventIDs: []string{}}
	got, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal chip payload: %v", err)
	}
	want := `{"note_ids":[],"event_ids":[],"counts":{"shared":0,"user":0,"agent":0}}`
	if string(got) != want {
		t.Fatalf("chip payload shape drifted:\n got: %s\nwant: %s", got, want)
	}
}

// TestWorkerProcessesBothStatuses (D2): a completed and a failed run both
// ingest — the job's status is attribution, never a filter. The failed run's
// turn is enqueued after the first finishes so its material is genuinely new
// window content.
func TestWorkerProcessesBothStatuses(t *testing.T) {
	s := seedWorld(t)
	base := time.Now().UTC().Add(-time.Hour)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "Reminder: the deploy window is Tuesdays."),
		chatEvent(t, "e2", "turn-1", 2, base.Add(time.Minute), schema.AgenticRoleTypeAssistant, "Noted."),
	)

	gistModel := &scriptedModel{responses: []string{
		`{"description":"Deploy window discussed","outcome":"Tuesday deploy window confirmed"}`,
		`[]`,
		`{"description":"Vendor contact named","outcome":"Vendor contact is Budi"}`,
		`[]`,
	}}
	gister := newTestGister(s, gistModel)
	gate := newTestGate(s, gistModel)
	w := NewWorker(gister, gate, testLogger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	done := testJob()
	done.Status = "completed"
	w.Enqueue(done)
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 1 }, "worker did not process the completed job")

	appendEvents(t, s,
		chatEvent(t, "e3", "turn-2", 3, base.Add(2*time.Minute), schema.AgenticRoleTypeUser, "Follow-up: the vendor contact is Budi."),
		chatEvent(t, "e4", "turn-2", 4, base.Add(3*time.Minute), schema.AgenticRoleTypeAssistant, "Recorded."),
	)

	failed := testJob()
	failed.TurnID = "turn-2"
	failed.Status = "failed"
	w.Enqueue(failed)
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 2 }, "worker did not process the failed job")

	stats := w.Stats()
	if stats.Succeeded != 2 || stats.Failed != 0 {
		t.Fatalf("expected 2 succeeded / 0 failed, got %+v", stats)
	}
	events, err := s.MemoryEvents().ListEventsForUI(ctx, testWorkspaceID, testUserID, testAgentID, store.MemoryEventFilters{SessionID: testSessionID})
	if err != nil {
		t.Fatalf("list gists: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 gist events (both run statuses ingest), got %d", len(events))
	}
}

// TestWorkerModelDownFailsSoft (D10): a dead side-call model fails both
// stages, the job is counted failed, nothing is written, and nothing escapes.
func TestWorkerModelDownFailsSoft(t *testing.T) {
	s := seedWorld(t)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, time.Now().UTC().Add(-time.Hour), schema.AgenticRoleTypeUser, "Remember that the staging database resets nightly."),
	)

	gister := NewGister(s.MemoryEvents(), s.SessionEvents(), nil, nil, nil, WithModelResolver(failingResolver("down")))
	gate := NewGate(s.MemoryNotes(), s.Memories(), nil, nil, nil, testLogger, WithModelResolver(failingResolver("down")))
	w := NewWorker(gister, gate, testLogger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(testJob())
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 1 }, "worker did not process the job")

	stats := w.Stats()
	if stats.Failed != 1 || stats.Succeeded != 0 {
		t.Fatalf("expected the job to fail soft, got %+v", stats)
	}
	notes, err := s.MemoryNotes().ListNotesForUI(ctx, testWorkspaceID, testUserID, testAgentID, store.MemoryNoteFilters{})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("no notes must be written when the model is down, got %d", len(notes))
	}
	events, err := s.MemoryEvents().ListEventsForUI(ctx, testWorkspaceID, testUserID, testAgentID, store.MemoryEventFilters{SessionID: testSessionID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("no gists must be written when the model is down, got %d", len(events))
	}
}

// TestWorkerQueueOverflowNeverBlocks: a full queue drops the job and counts
// it — Enqueue must return immediately even when nothing drains.
func TestWorkerQueueOverflowNeverBlocks(t *testing.T) {
	s := seedWorld(t)
	gister := newTestGister(s, &scriptedModel{})
	gate := newTestGate(s, &scriptedModel{})
	w := NewWorker(gister, gate, testLogger, WithQueueSize(1)) // never started — nothing drains

	start := time.Now()
	for i := 0; i < 4; i++ {
		w.Enqueue(testJob())
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Enqueue blocked on a full queue for %v", elapsed)
	}
	stats := w.Stats()
	if stats.Enqueued != 1 {
		t.Fatalf("expected exactly 1 enqueued, got %+v", stats)
	}
	if stats.QueueDropped != 3 {
		t.Fatalf("expected 3 queue drops, got %+v", stats)
	}
}

// TestWorkerEnqueueAfterStopDrops: a stopped worker counts late enqueues as
// dropped instead of parking them in the queue.
func TestWorkerEnqueueAfterStopDrops(t *testing.T) {
	s := seedWorld(t)
	w := NewWorker(newTestGister(s, &scriptedModel{}), newTestGate(s, &scriptedModel{}), testLogger, WithQueueSize(4))
	w.Start(context.Background())
	w.Stop()

	w.Enqueue(testJob())
	stats := w.Stats()
	if stats.Enqueued != 0 || stats.QueueDropped != 1 {
		t.Fatalf("expected the post-stop enqueue to drop, got %+v", stats)
	}
}

// TestWorkerEmitsChipAfterCommit (D11): the chip carries committed ids and
// the visibility breakdown only, and a turn that stores nothing emits none.
func TestWorkerEmitsChipAfterCommit(t *testing.T) {
	s := seedWorld(t)
	base := time.Now().UTC().Add(-time.Hour)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "Team update: the launch date moved to October."),
		chatEvent(t, "e2", "turn-2", 2, base.Add(time.Minute), schema.AgenticRoleTypeUser, "That is all for today."),
	)

	gistModel := &scriptedModel{responses: []string{
		`{"description":"Launch date moved","outcome":"Launch is in October"}`,
		`[{"op":"ADD","content":"The launch date moved to October","visibility":"shared","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false}]`,
		`{"description":"Nothing new","outcome":"Turn closed"}`,
		`[]`,
	}}
	gister := newTestGister(s, gistModel)
	gate := newTestGate(s, gistModel)

	var mu sync.Mutex
	var chips []MemoryIngestedPayload
	var chipJobs []IngestJob
	w := NewWorker(gister, gate, testLogger, WithChipSink(func(_ context.Context, job IngestJob, payload MemoryIngestedPayload) {
		mu.Lock()
		defer mu.Unlock()
		chips = append(chips, payload)
		chipJobs = append(chipJobs, job)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	channel := testJob()
	channel.Origin = originChannel
	channel.HumanParticipants = 2
	w.Enqueue(channel)
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 1 }, "worker did not process the first job")

	// The second turn's window is already consumed by the first gist — a
	// quiet success, so no second chip is emitted.
	second := testJob()
	second.TurnID = "turn-2"
	w.Enqueue(second)
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 2 }, "worker did not process the second job")

	mu.Lock()
	defer mu.Unlock()
	if len(chips) != 1 {
		t.Fatalf("expected exactly one chip (a turn that stores nothing emits none), got %d", len(chips))
	}
	if chipJobs[0].TurnID != "turn-1" {
		t.Fatalf("chip attributed to the wrong turn: %+v", chipJobs[0])
	}
	chip := chips[0]
	if len(chip.EventIDs) != 1 || len(chip.NoteIDs) != 1 {
		t.Fatalf("expected one gist id and one note id, got %+v", chip)
	}
	// The breakdown counts every stored row: one shared gist + one shared note.
	if chip.Counts.Shared != 2 || chip.Counts.User != 0 || chip.Counts.Agent != 0 {
		t.Fatalf("expected the shared count to carry the breakdown, got %+v", chip.Counts)
	}
	raw, err := json.Marshal(chip)
	if err != nil {
		t.Fatalf("marshal chip: %v", err)
	}
	if !strings.Contains(string(raw), `"counts":{"shared":2,"user":0,"agent":0}`) {
		t.Fatalf("chip breakdown missing from payload: %s", raw)
	}
	if strings.Contains(string(raw), "launch") {
		t.Fatalf("chip payload must never carry content: %s", raw)
	}
}
