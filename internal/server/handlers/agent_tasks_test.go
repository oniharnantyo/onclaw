package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	adk "github.com/cloudwego/eino/adk"
	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// fakeAgentTaskSurface models the runner's background-task surface
// (handlers.AgentTaskSurface) for the session tasks endpoint tests: the live
// run existence probe and task snapshot behind the merged list, the live
// trajectory read behind the output endpoint, the progress subscription
// behind the SSE stream, and the running-only cancel.
type fakeAgentTaskSurface struct {
	mu sync.Mutex

	spaceExists bool // TaskSpace reports a live run holding the session
	live        []agents.LiveTask

	records      []agents.TaskProgressFrame
	recordsOK    bool
	recordsErr   error
	recordsAfter int // the last requested afterSequence cursor

	cancelEntry agents.TaskListEntry
	cancelErr   error
	cancelCalls [][4]string // (workspaceID, agentID, sessionID, taskID) per call

	framesCh    chan agents.TaskProgressFrame
	subscribeOK bool
	subscribed  chan struct{} // signaled once the handler attaches (when non-nil)
	subscribes  [][3]string
	unsubs      []struct {
		key   [3]string
		subID uint64
	}
}

func (f *fakeAgentTaskSurface) TaskSpace(workspaceID, agentID, sessionID string) (*agents.BackgroundTaskSpace, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return nil, f.spaceExists
}

func (f *fakeAgentTaskSurface) LiveTasks(workspaceID, agentID, sessionID string) []agents.LiveTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agents.LiveTask(nil), f.live...)
}

func (f *fakeAgentTaskSurface) TaskProgressRecords(ctx context.Context, workspaceID, agentID, sessionID, taskID string, afterSequence int) ([]agents.TaskProgressFrame, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordsAfter = afterSequence
	if f.recordsErr != nil {
		return nil, false, f.recordsErr
	}
	return append([]agents.TaskProgressFrame(nil), f.records...), f.recordsOK, nil
}

func (f *fakeAgentTaskSurface) SubscribeTaskProgress(workspaceID, agentID, sessionID string) (uint64, <-chan agents.TaskProgressFrame, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribes = append(f.subscribes, [3]string{workspaceID, agentID, sessionID})
	if !f.subscribeOK {
		return 0, nil, false
	}
	if f.subscribed != nil {
		select {
		case f.subscribed <- struct{}{}:
		default:
		}
	}
	return 1, f.framesCh, true
}

func (f *fakeAgentTaskSurface) UnsubscribeTaskProgress(workspaceID, agentID, sessionID string, subID uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unsubs = append(f.unsubs, struct {
		key   [3]string
		subID uint64
	}{key: [3]string{workspaceID, agentID, sessionID}, subID: subID})
}

func (f *fakeAgentTaskSurface) CancelTask(ctx context.Context, workspaceID, agentID, sessionID, taskID string) (agents.TaskListEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelCalls = append(f.cancelCalls, [4]string{workspaceID, agentID, sessionID, taskID})
	return f.cancelEntry, f.cancelErr
}

func (f *fakeAgentTaskSurface) unsubscribeCalls() []struct {
	key   [3]string
	subID uint64
} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]struct {
		key   [3]string
		subID uint64
	}(nil), f.unsubs...)
}

// agentTaskTestEnv is the session tasks test rig: one workspace with an
// owner (agents.write) and a plain member, one agent, a seeded session
// (sess-1) whose transcript carries one non-task event, the four task routes
// mounted the way the router does, and the acting user switchable per test.
type agentTaskTestEnv struct {
	r            *gin.Engine
	st           store.Store
	ws           *domain.Workspace
	agent        *domain.Agent
	owner        *domain.User
	member       *domain.User
	surface      *fakeAgentTaskSurface
	transcript   *fakeStreamingRunner
	workspaceDir string
	currentUser  **domain.User
}

func newAgentTaskTestEnv(t *testing.T, surface *fakeAgentTaskSurface) *agentTaskTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	ws := &domain.Workspace{ID: "ws-tasks-test", Slug: "tasks-ws", Name: "Tasks Workspace"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: "Owner", Permissions: domain.OwnerPermissions}
	if err := st.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("create owner role: %v", err)
	}
	memberRole := &domain.Role{WorkspaceID: ws.ID, Name: "Member", Permissions: domain.MemberPermissions}
	if err := st.Roles().Create(ctx, memberRole); err != nil {
		t.Fatalf("create member role: %v", err)
	}
	owner := &domain.User{Email: "tasks-owner@example.com", Name: "Tasks Owner"}
	if err := st.Users().Create(ctx, owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member := &domain.User{Email: "tasks-member@example.com", Name: "Tasks Member"}
	if err := st.Users().Create(ctx, member); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("add owner member: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: member.ID, RoleID: memberRole.ID}); err != nil {
		t.Fatalf("add member member: %v", err)
	}
	prov := &domain.ProviderConfig{ID: "prov-fake", WorkspaceID: ws.ID, Type: "fake", Name: "Fake"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	agent := &domain.Agent{
		ID:          "agent-atlas-1",
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		Role:        "ops",
		Brief:       "Runs things",
		ProviderID:  prov.ID,
		Model:       "fake-model",
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	// A known session whose transcript carries one non-task event: the
	// existence probe's positive case and the empty-tasks case both read it.
	if err := st.SessionEvents().AppendEvents(ctx, ws.ID, []domain.SessionEvent{{
		SessionID:   "sess-1",
		EventID:     "evt-1",
		TurnID:      "turn-0",
		Kind:        "message",
		OccurredAt:  time.Now().UTC(),
		WorkspaceID: ws.ID,
	}}); err != nil {
		t.Fatalf("append session event: %v", err)
	}

	workspaceDir := t.TempDir()
	// The transcript-side fake rides along so a test can prove the tasks
	// stream never touches it (liveRun=true would make any illegal tap
	// attach — and the assertions below require zero attaches).
	transcript := newFakeStreamingRunner(&agents.HistoryResult{Events: []agents.TranscriptEvent{}})
	transcript.liveRun = true
	h := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), st.AgentSessions(), []byte("01234567890123456789012345678901"), nil, nil, nil, workspaceDir, transcript, transcript, surface, mustTestAuthorizer(t, st))

	var currentUser *domain.User
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.UserContextKey, currentUser)
		// Resolve the acting member's role like RequireWorkspace does — the
		// ownership gate evaluates agents.write through the authorizer.
		if currentUser != nil {
			if m, err := st.Members().Get(c.Request.Context(), ws.ID, currentUser.ID); err == nil {
				if role, err := st.Roles().ByID(c.Request.Context(), m.RoleID); err == nil {
					c.Set(handlers.RoleContextKey, role)
				}
			}
		}
		c.Next()
	})
	base := "/workspaces/:ws/agents/:agent/sessions/:session"
	r.GET(base+"/tasks", h.ListSessionTasks)
	r.GET(base+"/tasks/stream", h.StreamSessionTasks)
	r.GET(base+"/tasks/:task/output", h.GetSessionTaskOutput)
	r.POST(base+"/tasks/:task/cancel", h.CancelSessionTask)

	return &agentTaskTestEnv{
		r: r, st: st, ws: ws, agent: agent, owner: owner, member: member,
		surface: surface, transcript: transcript, workspaceDir: workspaceDir, currentUser: &currentUser,
	}
}

// appendTaskCreatedEvent persists one x.eino.background_task.created session
// event through the real serializer round-trip the runner's write path uses.
func appendTaskCreatedEvent(t *testing.T, env *agentTaskTestEnv, sessionID, eventID, taskID, description string) {
	t.Helper()
	payload, err := (&schema.HumanReadableSerializer{}).Marshal(&adk.SessionEvent[*schema.AgenticMessage]{
		EventID:   eventID,
		Kind:      bgtask.SessionEventTaskCreated,
		Timestamp: time.Now().UTC(),
		Extension: &adk.SessionExtensionEvent{Data: &bgtask.TaskCreatedSessionEvent{TaskID: taskID, Description: description}},
	})
	if err != nil {
		t.Fatalf("marshal created event: %v", err)
	}
	if err := env.st.SessionEvents().AppendEvents(context.Background(), env.ws.ID, []domain.SessionEvent{{
		SessionID:   sessionID,
		EventID:     eventID,
		Kind:        string(bgtask.SessionEventTaskCreated),
		Payload:     payload,
		OccurredAt:  time.Now().UTC(),
		WorkspaceID: env.ws.ID,
	}}); err != nil {
		t.Fatalf("append created event: %v", err)
	}
}

// appendTaskCompletedEvent persists one x.task_completed session event.
func appendTaskCompletedEvent(t *testing.T, env *agentTaskTestEnv, sessionID, eventID string, payload agents.TaskCompletedPayload) {
	t.Helper()
	serialized, err := (&schema.HumanReadableSerializer{}).Marshal(&adk.SessionEvent[*schema.AgenticMessage]{
		EventID:   eventID,
		Kind:      adk.SessionEventKind("x.task_completed"), // agents.sessionEventKindTaskCompleted is unexported; the runner persists this literal
		Timestamp: time.Now().UTC(),
		Extension: &adk.SessionExtensionEvent{Data: payload},
	})
	if err != nil {
		t.Fatalf("marshal completed event: %v", err)
	}
	if err := env.st.SessionEvents().AppendEvents(context.Background(), env.ws.ID, []domain.SessionEvent{{
		SessionID:   sessionID,
		EventID:     eventID,
		Kind:        "x.task_completed",
		Payload:     serialized,
		OccurredAt:  time.Now().UTC(),
		WorkspaceID: env.ws.ID,
	}}); err != nil {
		t.Fatalf("append completed event: %v", err)
	}
}

// taskOutputWire mirrors the output endpoint's pinned wire shape for decoding
// in tests (the handler's response type is internal).
type taskOutputWire struct {
	TaskID  string `json:"task_id"`
	Records []struct {
		agents.TaskProgressRecord
		Sequence int `json:"sequence"`
	} `json:"records"`
	Terminal *agents.TaskCompletedPayload `json:"terminal"`
}

func getTasks(t *testing.T, env *agentTaskTestEnv, sessionID string) (*httptest.ResponseRecorder, []agents.TaskListEntry) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/"+sessionID+"/tasks", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		return w, nil
	}
	var res struct {
		Tasks []agents.TaskListEntry `json:"tasks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal tasks: %v (%s)", err, w.Body.String())
	}
	return w, res.Tasks
}

// TestAgentTasks_ListMergesLiveAndPersisted: one running live delegation, one
// completed shell task from an earlier turn, and one canceled delegation from
// a previous run all appear with their merged statuses, kinds, and output
// locations — sorted by task id.
func TestAgentTasks_ListMergesLiveAndPersisted(t *testing.T) {
	surface := &fakeAgentTaskSurface{
		spaceExists: true,
		live: []agents.LiveTask{{
			TaskID: "task-live-2", Kind: "delegation", Status: "running",
			Description: "live delegation", OutputPath: ".tasks/task-live-2.output",
		}},
	}
	env := newAgentTaskTestEnv(t, surface)
	appendTaskCreatedEvent(t, env, "sess-1", "evt-created-hist", "task-hist-1", "historical delegation")
	appendTaskCompletedEvent(t, env, "sess-1", "evt-done-shell", agents.TaskCompletedPayload{
		TaskID: "task-done-0", Kind: "shell", Outcome: "completed",
		OutputPath: ".tasks/task-done-0.output", Summary: "shell summary",
	})
	appendTaskCompletedEvent(t, env, "sess-1", "evt-done-hist", agents.TaskCompletedPayload{
		TaskID: "task-hist-1", Kind: "delegation", Outcome: "canceled", Summary: "historical delegation",
	})

	w, tasks := getTasks(t, env, "sess-1")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 merged tasks, got %d: %s", len(tasks), w.Body.String())
	}
	// Deterministic task-id sort.
	ids := []string{tasks[0].TaskID, tasks[1].TaskID, tasks[2].TaskID}
	if ids[0] != "task-done-0" || ids[1] != "task-hist-1" || ids[2] != "task-live-2" {
		t.Fatalf("unexpected merged order %v", ids)
	}
	shell, hist, live := tasks[0], tasks[1], tasks[2]
	if shell.Kind != "shell" || shell.Status != "completed" || shell.Summary != "shell summary" || shell.OutputPath != ".tasks/task-done-0.output" {
		t.Fatalf("unexpected shell entry: %+v", shell)
	}
	// The persisted terminal outcome beats the created-only base: canceled,
	// with the created event's description kept.
	if hist.Kind != "delegation" || hist.Status != "canceled" || hist.Description != "historical delegation" {
		t.Fatalf("unexpected historical entry: %+v", hist)
	}
	if live.Kind != "delegation" || live.Status != "running" || live.Description != "live delegation" {
		t.Fatalf("unexpected live entry: %+v", live)
	}

	// The pinned wire shape of one entry.
	var raw struct {
		Tasks []map[string]json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, key := range []string{"task_id", "kind", "status", "description"} {
		if _, ok := raw.Tasks[2][key]; !ok {
			t.Fatalf("entry missing pinned key %q: %s", key, raw.Tasks[2])
		}
	}

	// A read surface never cancels or subscribes.
	if calls := surface.cancelCallsRecorded(); len(calls) != 0 {
		t.Fatalf("a list must not cancel, got %v", calls)
	}
}

// TestAgentTasks_ListUnknownSession404: no live run and no persisted events —
// the session is indistinguishable from an unknown one and reads not-found.
func TestAgentTasks_ListUnknownSession404(t *testing.T) {
	env := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})

	w, _ := getTasks(t, env, "nope")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAgentTasks_ListKnownSessionEmptyTasks: a known session with no task
// evidence lists an empty array — not an error, not null.
func TestAgentTasks_ListKnownSessionEmptyTasks(t *testing.T) {
	env := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})

	w, tasks := getTasks(t, env, "sess-1")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if tasks != nil && len(tasks) != 0 {
		t.Fatalf("expected empty task list, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"tasks":[]`) {
		t.Fatalf("empty list must render [], got %s", w.Body.String())
	}
}

// TestAgentTasks_ListLiveRunProvesExistence: a live run with no persisted
// events is a known session — the list serves its (empty) task space instead
// of 404 (the CancelRun first-turn rule).
func TestAgentTasks_ListLiveRunProvesExistence(t *testing.T) {
	env := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{spaceExists: true})

	w, tasks := getTasks(t, env, "sess-uncommitted")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for a live run with no committed events, got %d: %s", w.Code, w.Body.String())
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks, got %s", w.Body.String())
	}
}

// TestAgentTasks_ListUnknownAgent404: agent resolution mirrors the other
// agent routes and the task surface is never consulted.
func TestAgentTasks_ListUnknownAgent404(t *testing.T) {
	env := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/ghost/sessions/sess-1/tasks", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown agent, got %d: %s", w.Code, w.Body.String())
	}
	if env.surface.subscribesRecorded() != 0 || env.surface.recordsRequested() {
		t.Fatalf("the task surface must not be consulted for an unknown agent")
	}
}

// TestAgentTasks_OutputLiveTask: a running task's trajectory reads from the
// live space past the cursor, with the terminal field null while the task
// runs.
func TestAgentTasks_OutputLiveTask(t *testing.T) {
	surface := &fakeAgentTaskSurface{
		recordsOK: true,
		records: []agents.TaskProgressFrame{
			{TaskID: "task-live-2", Kind: "delegation", Sequence: 1, Record: &agents.TaskProgressRecord{Type: agents.TaskProgressRecordText, Text: "working on it"}},
			{TaskID: "task-live-2", Kind: "delegation", Sequence: 2, Record: &agents.TaskProgressRecord{Type: agents.TaskProgressRecordToolCall, Name: "grep", Args: `{"pattern":"flaky"}`}},
		},
	}
	env := newAgentTaskTestEnv(t, surface)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/task-live-2/output?after=0", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res taskOutputWire
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal output: %v (%s)", err, w.Body.String())
	}
	if res.TaskID != "task-live-2" || len(res.Records) != 2 {
		t.Fatalf("unexpected output payload: %+v", res)
	}
	if res.Records[0].Sequence != 1 || res.Records[0].Type != agents.TaskProgressRecordText || res.Records[0].Text != "working on it" {
		t.Fatalf("unexpected first record: %+v", res.Records[0])
	}
	if res.Records[1].Sequence != 2 || res.Records[1].Type != agents.TaskProgressRecordToolCall || res.Records[1].Name != "grep" {
		t.Fatalf("unexpected second record: %+v", res.Records[1])
	}
	if res.Terminal != nil {
		t.Fatalf("a running task's terminal must be null, got %+v", res.Terminal)
	}
	// The cursor rides through to the live read.
	if got := surface.lastRecordsAfter(); got != 0 {
		t.Fatalf("expected after=0 to reach the live read, got %d", got)
	}

	// A client-supplied cursor is forwarded verbatim.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/task-live-2/output?after=3", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for cursor read, got %d: %s", w.Code, w.Body.String())
	}
	if got := surface.lastRecordsAfter(); got != 3 {
		t.Fatalf("expected after=3 to reach the live read, got %d", got)
	}

	// A negative cursor is invalid_request.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/task-live-2/output?after=-1", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a negative cursor, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAgentTasks_OutputEndedReplaysFile: an ended task replays its persisted
// output file — records decoded from the file's lines with 1-based sequences,
// and the persisted terminal notice as the response's terminal.
func TestAgentTasks_OutputEndedReplaysFile(t *testing.T) {
	env := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})
	appendTaskCompletedEvent(t, env, "sess-1", "evt-done-shell", agents.TaskCompletedPayload{
		TaskID: "task-done-0", Kind: "shell", Outcome: "completed",
		OutputPath: ".tasks/task-done-0.output", Summary: "shell summary",
	})
	agentDir := domain.AgentWorkspaceDir(env.workspaceDir, env.ws.Slug, env.agent.Slug)
	if err := os.MkdirAll(filepath.Join(agentDir, ".tasks"), 0o755); err != nil {
		t.Fatalf("seed tasks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, ".tasks", "task-done-0.output"), []byte("alpha line\nbeta line\n"), 0o644); err != nil {
		t.Fatalf("seed output file: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/task-done-0/output", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res taskOutputWire
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal output: %v (%s)", err, w.Body.String())
	}
	if res.TaskID != "task-done-0" || len(res.Records) != 2 {
		t.Fatalf("unexpected replay payload: %+v", res)
	}
	if res.Records[0].Sequence != 1 || res.Records[0].Text != "alpha line" || res.Records[0].Type != agents.TaskProgressRecordText {
		t.Fatalf("unexpected first replay record: %+v", res.Records[0])
	}
	if res.Records[1].Sequence != 2 || res.Records[1].Text != "beta line" {
		t.Fatalf("unexpected second replay record: %+v", res.Records[1])
	}
	if res.Terminal == nil || res.Terminal.TaskID != "task-done-0" || res.Terminal.Outcome != "completed" {
		t.Fatalf("replay must carry the persisted terminal, got %+v", res.Terminal)
	}
}

// TestAgentTasks_OutputPrunedFile404: the completion event persists but the
// output file is gone — a clear not-found naming the task, never an empty
// trajectory.
func TestAgentTasks_OutputPrunedFile404(t *testing.T) {
	env := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})
	appendTaskCompletedEvent(t, env, "sess-1", "evt-done-shell", agents.TaskCompletedPayload{
		TaskID: "task-pruned-9", Kind: "shell", Outcome: "completed",
		OutputPath: ".tasks/task-pruned-9.output", Summary: "gone",
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/task-pruned-9/output", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a pruned output file, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "task-pruned-9") {
		t.Fatalf("not-found must name the task, got %s", w.Body.String())
	}
}

// TestAgentTasks_OutputCreatedOnly404: a created event without a completion
// (the restart-lost shape) has no output location to replay — honest
// not-found.
func TestAgentTasks_OutputCreatedOnly404(t *testing.T) {
	env := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})
	appendTaskCreatedEvent(t, env, "sess-1", "evt-created-lost", "task-lost-5", "lost delegation")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/task-lost-5/output", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a created-only task, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "task-lost-5") {
		t.Fatalf("not-found must name the task, got %s", w.Body.String())
	}
}

// TestAgentTasks_OutputUnknownTaskAndSession404: an unknown task in a known
// session and an unknown session both read not-found (the latter via the
// CancelRun existence rule).
func TestAgentTasks_OutputUnknownTaskAndSession404(t *testing.T) {
	env := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/task-unknown/output", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown task, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/nope/tasks/task-unknown/output", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "session not found") {
		t.Fatalf("unknown session must read as session-not-found, got %s", w.Body.String())
	}
}

// TestAgentTasks_CancelRunningTask: the cancel acceptance mirrors CancelRun's
// wire shape and addresses the task space by the resolved coordinates.
func TestAgentTasks_CancelRunningTask(t *testing.T) {
	surface := &fakeAgentTaskSurface{
		cancelEntry: agents.TaskListEntry{TaskID: "task-live-2", Kind: "delegation", Status: "canceled"},
	}
	env := newAgentTaskTestEnv(t, surface)
	*env.currentUser = env.owner

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/task-live-2/cancel", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on running-task cancel, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !res.Cancelled {
		t.Fatalf("expected cancelled=true, got %s", w.Body.String())
	}
	calls := surface.cancelCallsRecorded()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one CancelTask call, got %d", len(calls))
	}
	if call := calls[0]; call != [4]string{"ws-tasks-test", "agent-atlas-1", "sess-1", "task-live-2"} {
		t.Fatalf("CancelTask addressed the wrong task, got %v", call)
	}
}

// TestAgentTasks_CancelNotRunning409: unknown id, ended run, and terminal
// task all collapse into the typed conflict.
func TestAgentTasks_CancelNotRunning409(t *testing.T) {
	surface := &fakeAgentTaskSurface{cancelErr: agents.ErrTaskNotRunning}
	env := newAgentTaskTestEnv(t, surface)
	*env.currentUser = env.owner

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/task-ended-1/cancel", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a not-running task, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"conflict"`) {
		t.Fatalf("expected the conflict envelope, got %s", w.Body.String())
	}
}

// TestAgentTasks_CancelOwnershipRule mirrors fix-role-permission-audit D3:
// the session's owning member or an agents.write holder; everyone else 403,
// checked before the task-space probe.
func TestAgentTasks_CancelOwnershipRule(t *testing.T) {
	// A plain member with no index row: 403 before the surface is touched.
	memberEnv := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})
	*memberEnv.currentUser = memberEnv.member
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/task-live-2/cancel", nil)
	memberEnv.r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a foreign member's cancel, got %d: %s", w.Code, w.Body.String())
	}
	if calls := memberEnv.surface.cancelCallsRecorded(); len(calls) != 0 {
		t.Fatalf("the ownership gate must precede the task-space probe, got %v", calls)
	}

	// The session's owning member passes without agents.write.
	ownedEnv := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})
	if err := ownedEnv.st.AgentSessions().UpsertAgentSession(context.Background(), ownedEnv.ws.ID, ownedEnv.agent.ID, ownedEnv.member.ID, domain.AgentSessionUpsert{SessionID: "sess-owned", Title: "Member topic"}); err != nil {
		t.Fatalf("seed owned session: %v", err)
	}
	*ownedEnv.currentUser = ownedEnv.member
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/workspaces/tasks-ws/agents/atlas/sessions/sess-owned/tasks/task-live-2/cancel", nil)
	ownedEnv.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for the owning member's cancel, got %d: %s", w.Code, w.Body.String())
	}

	// An agents.write holder may cancel a foreign session's task.
	adminEnv := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})
	*adminEnv.currentUser = adminEnv.owner
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/workspaces/tasks-ws/agents/atlas/sessions/sess-owned-elsewhere/tasks/task-live-2/cancel", nil)
	adminEnv.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for an agents.write holder's cancel, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAgentTasks_CancelUnknownAgent404: agent resolution precedes everything.
func TestAgentTasks_CancelUnknownAgent404(t *testing.T) {
	env := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})
	*env.currentUser = env.owner

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workspaces/tasks-ws/agents/ghost/sessions/sess-1/tasks/task-live-2/cancel", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown agent, got %d: %s", w.Code, w.Body.String())
	}
	if calls := env.surface.cancelCallsRecorded(); len(calls) != 0 {
		t.Fatalf("the surface must not be consulted for an unknown agent, got %v", calls)
	}
}

// TestAgentTasks_StreamFramesToRunEnd: the SSE endpoint streams the task
// frames it is fed — progress then terminal, each addressed by task id — and
// ends with [DONE] when the run closes the channel, releasing the
// subscription.
func TestAgentTasks_StreamFramesToRunEnd(t *testing.T) {
	surface := &fakeAgentTaskSurface{
		subscribeOK: true,
		framesCh:    make(chan agents.TaskProgressFrame, 8),
		subscribed:  make(chan struct{}, 1),
	}
	env := newAgentTaskTestEnv(t, surface)

	done := make(chan struct{})
	var w *httptest.ResponseRecorder
	go func() {
		defer close(done)
		w = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/stream", nil)
		env.r.ServeHTTP(w, req)
	}()

	<-surface.subscribed
	surface.framesCh <- agents.TaskProgressFrame{
		TaskID: "task-live-2", Kind: "delegation", Sequence: 1,
		Record: &agents.TaskProgressRecord{Type: agents.TaskProgressRecordText, Text: "child text"},
	}
	surface.framesCh <- agents.TaskProgressFrame{
		TaskID: "task-live-2", Kind: "delegation",
		Terminal: &agents.TaskCompletedPayload{TaskID: "task-live-2", Kind: "delegation", Outcome: "completed", OutputPath: ".tasks/task-live-2.output", Summary: "done"},
	}
	close(surface.framesCh)
	<-done

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertSSEHeaders(t, w)
	frames := sseFrames(t, w.Body.String())
	assertDoneLast(t, frames)
	if len(frames) != 3 {
		t.Fatalf("expected 2 task frames + [DONE], got %v", frames)
	}
	var progress agents.TaskProgressFrame
	if err := json.Unmarshal([]byte(frames[0]), &progress); err != nil {
		t.Fatalf("frame 0 is not a task frame: %v (%q)", err, frames[0])
	}
	if progress.TaskID != "task-live-2" || progress.Sequence != 1 || progress.Record == nil || progress.Record.Text != "child text" {
		t.Fatalf("unexpected progress frame: %+v", progress)
	}
	var terminal agents.TaskProgressFrame
	if err := json.Unmarshal([]byte(frames[1]), &terminal); err != nil {
		t.Fatalf("frame 1 is not a task frame: %v (%q)", err, frames[1])
	}
	if terminal.TaskID != "task-live-2" || terminal.Terminal == nil || terminal.Terminal.Outcome != "completed" {
		t.Fatalf("unexpected terminal frame: %+v", terminal)
	}

	// Disconnect hygiene: the subscription is released with the exact key it
	// attached under.
	unsubs := surface.unsubscribeCalls()
	if len(unsubs) != 1 {
		t.Fatalf("expected exactly one unsubscribe, got %v", unsubs)
	}
	if unsubs[0].key != [3]string{"ws-tasks-test", "agent-atlas-1", "sess-1"} || unsubs[0].subID != 1 {
		t.Fatalf("unsubscribe addressed the wrong subscription, got %+v", unsubs[0])
	}
}

// TestAgentTasks_StreamUnknownSession404: no live run and no persisted
// events — the not-found envelope is written before the stream switches to
// SSE.
func TestAgentTasks_StreamUnknownSession404(t *testing.T) {
	env := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/nope/tasks/stream", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got == "text/event-stream" {
		t.Fatalf("error responses must stay JSON, got SSE headers")
	}
	if strings.Contains(w.Body.String(), "data:") {
		t.Fatalf("no SSE frames may precede the error envelope, got:\n%s", w.Body.String())
	}
	if unsubs := env.surface.unsubscribeCalls(); len(unsubs) != 0 {
		t.Fatalf("no subscription may be released for a refused stream, got %v", unsubs)
	}
}

// TestAgentTasks_StreamKnownSessionNoLiveRunImmediateEnd: a known session
// with no live run gets a clean empty stream (reconnect-friendly) — SSE
// headers and an immediate [DONE].
func TestAgentTasks_StreamKnownSessionNoLiveRunImmediateEnd(t *testing.T) {
	env := newAgentTaskTestEnv(t, &fakeAgentTaskSurface{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/stream", nil)
	env.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertSSEHeaders(t, w)
	if w.Body.String() != "data: [DONE]\n\n" {
		t.Fatalf("expected an immediate clean end, got %q", w.Body.String())
	}
}

// TestAgentTasks_StreamNeverTouchesTranscript: the tasks stream reads only
// the task-progress channel — even with a live run the transcript tap is
// never attached, so a non-subscribed session's transcript events are
// untouched.
func TestAgentTasks_StreamNeverTouchesTranscript(t *testing.T) {
	surface := &fakeAgentTaskSurface{
		subscribeOK: true,
		framesCh:    make(chan agents.TaskProgressFrame, 4),
		subscribed:  make(chan struct{}, 1),
	}
	env := newAgentTaskTestEnv(t, surface)

	done := make(chan struct{})
	var w *httptest.ResponseRecorder
	go func() {
		defer close(done)
		w = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/stream", nil)
		env.r.ServeHTTP(w, req)
	}()

	<-surface.subscribed
	surface.framesCh <- agents.TaskProgressFrame{
		TaskID: "task-live-2", Kind: "delegation", Sequence: 1,
		Record: &agents.TaskProgressRecord{Type: agents.TaskProgressRecordText, Text: "child text"},
	}
	close(surface.framesCh)
	<-done

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// The transcript fake is live-run capable: any transcript-side tap would
	// have attached. Zero attaches proves the tasks stream never touches it.
	if env.transcript.subscriberCount() != 0 {
		t.Fatalf("the tasks stream must never attach a transcript tap")
	}
	if calls := env.transcript.unsubscribeCalls(); len(calls) != 0 {
		t.Fatalf("the tasks stream must never touch the transcript taps, got %v", calls)
	}
	// The streamed frames are task frames, not transcript events.
	if strings.Contains(w.Body.String(), `"kind":"turn_started"`) || strings.Contains(w.Body.String(), `"text_delta"`) {
		t.Fatalf("transcript event shapes must never ride the tasks stream, got:\n%s", w.Body.String())
	}
}

// TestAgentTasks_StreamClientDisconnect: a client going away mid-stream ends
// the handler and releases the subscription instead of leaking it.
func TestAgentTasks_StreamClientDisconnect(t *testing.T) {
	surface := &fakeAgentTaskSurface{
		subscribeOK: true,
		framesCh:    make(chan agents.TaskProgressFrame, 4),
		subscribed:  make(chan struct{}, 1),
	}
	env := newAgentTaskTestEnv(t, surface)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var w *httptest.ResponseRecorder
	go func() {
		defer close(done)
		w = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/workspaces/tasks-ws/agents/atlas/sessions/sess-1/tasks/stream", nil).WithContext(ctx)
		env.r.ServeHTTP(w, req)
	}()

	<-surface.subscribed
	cancel()
	<-done

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertSSEHeaders(t, w)
	assertDoneLast(t, sseFrames(t, w.Body.String()))

	unsubs := surface.unsubscribeCalls()
	if len(unsubs) != 1 {
		t.Fatalf("expected the abandoned connection's subscription to be released, got %v", unsubs)
	}
	if unsubs[0].subID != 1 {
		t.Fatalf("expected unsubscribe of sub 1, got %d", unsubs[0].subID)
	}
}

// Call-record accessors keeping the race detector honest.
func (f *fakeAgentTaskSurface) cancelCallsRecorded() [][4]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][4]string(nil), f.cancelCalls...)
}

func (f *fakeAgentTaskSurface) subscribesRecorded() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subscribes)
}

func (f *fakeAgentTaskSurface) recordsRequested() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recordsAfter != 0 || f.recordsOK || f.recordsErr != nil
}

func (f *fakeAgentTaskSurface) lastRecordsAfter() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recordsAfter
}
