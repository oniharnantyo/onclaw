package hooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// -------------------------------------------------------------------------
// Fakes
// -------------------------------------------------------------------------

// statusWrite records one SetHookDeliveryStatus call.
type statusWrite struct {
	Level       domain.HookLevel
	HookID      string
	Status      domain.HookStatus
	StatusError string
}

// fakeHookStore is the dispatcher's scripted store: canned per-level lists
// (list order = position order for workspace/agent tiers, mirroring the real
// stores' ORDER BY) plus recorded audit rows and status writes.
type fakeHookStore struct {
	store.HookStore // embedded; unimplemented methods panic if ever reached

	instanceHooks  []domain.InstanceHook
	workspaceHooks []domain.WorkspaceHook
	agentHooks     []domain.AgentHook

	mu         sync.Mutex
	executions []domain.HookExecution
	statuses   []statusWrite
}

func (f *fakeHookStore) ListInstanceHooks(context.Context) ([]domain.InstanceHook, error) {
	return f.instanceHooks, nil
}

func (f *fakeHookStore) ListWorkspaceHooks(_ context.Context, workspaceID string) ([]domain.WorkspaceHook, error) {
	out := make([]domain.WorkspaceHook, 0)
	for _, h := range f.workspaceHooks {
		if h.WorkspaceID == workspaceID {
			out = append(out, h)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

func (f *fakeHookStore) ListAgentHooks(_ context.Context, workspaceID, agentID string) ([]domain.AgentHook, error) {
	out := make([]domain.AgentHook, 0)
	for _, h := range f.agentHooks {
		if h.WorkspaceID == workspaceID && h.AgentID == agentID {
			out = append(out, h)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

func (f *fakeHookStore) RecordHookExecution(_ context.Context, exec *domain.HookExecution) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executions = append(f.executions, *exec)
	return nil
}

func (f *fakeHookStore) SetHookDeliveryStatus(_ context.Context, level domain.HookLevel, hookID string, status domain.HookStatus, statusError string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = append(f.statuses, statusWrite{Level: level, HookID: hookID, Status: status, StatusError: statusError})
	return nil
}

func (f *fakeHookStore) recordedExecutions() []domain.HookExecution {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.HookExecution(nil), f.executions...)
}

func (f *fakeHookStore) recordedStatuses() []statusWrite {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]statusWrite(nil), f.statuses...)
}

// recordingHookServer serves canned decision bodies and records every hit's
// path (each hook config points at its own path on one shared server).
type recordingHookServer struct {
	srv  *httptest.Server
	hits func() []string
}

func newRecordingHookServer(t *testing.T, status int, body string, delay time.Duration) *recordingHookServer {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &recordingHookServer{
		srv: srv,
		hits: func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), paths...)
		},
	}
}

// countingHookServer counts hits and serves a fixed decision body.
type countingHookServer struct {
	srv  *httptest.Server
	hits func() int
}

func newCountingHookServer(t *testing.T, status int, body string) *countingHookServer {
	t.Helper()
	var hits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &countingHookServer{srv: srv, hits: func() int { return int(hits.Load()) }}
}

// -------------------------------------------------------------------------
// Hook builders
// -------------------------------------------------------------------------

func workspaceHTTPHook(workspaceID, id, name string, position int, event domain.HookEvent, matcher string, url string) domain.WorkspaceHook {
	return domain.WorkspaceHook{
		ID:          id,
		WorkspaceID: workspaceID,
		HookBase: domain.HookBase{
			Name:        name,
			Event:       event,
			Matcher:     matcher,
			HandlerType: domain.HookHandlerHTTP,
			Config:      json.RawMessage(`{"url":"` + url + `"}`),
			TimeoutMS:   domain.DefaultHookTimeoutMS,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
			Position:    position,
		},
	}
}

func agentHTTPHook(workspaceID, agentID, id, name string, event domain.HookEvent, matcher string, url string) domain.AgentHook {
	return domain.AgentHook{
		ID:          id,
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		HookBase: domain.HookBase{
			Name:        name,
			Event:       event,
			Matcher:     matcher,
			HandlerType: domain.HookHandlerHTTP,
			Config:      json.RawMessage(`{"url":"` + url + `"}`),
			TimeoutMS:   domain.DefaultHookTimeoutMS,
			Enabled:     true,
		},
	}
}

func instanceHTTPHook(id, name string, event domain.HookEvent, matcher string, url string) domain.InstanceHook {
	return domain.InstanceHook{
		ID: id,
		HookBase: domain.HookBase{
			Name:        name,
			Event:       event,
			Matcher:     matcher,
			HandlerType: domain.HookHandlerHTTP,
			Config:      json.RawMessage(`{"url":"` + url + `"}`),
			TimeoutMS:   domain.DefaultHookTimeoutMS,
			Enabled:     true,
		},
	}
}

func matchAll() string { return "*" }

func matchOnly(values ...string) string {
	return strings.Join(values, "|")
}

func dispatcherEvent() Event {
	return Event{
		Origin:    "user",
		Workspace: EventRef{ID: "ws-1", Name: "Acme"},
		Agent:     EventRef{ID: "ag-1", Name: "Atlas"},
		SessionID: "sess-1",
	}
}

func dispatcherRegistry(srv *httptest.Server) *Registry {
	return NewRegistry(WithHTTPClient(srv.Client()), WithHTTPAllowPrivate(true))
}

func dispatcherTool() EventTool {
	return EventTool{Name: "shell.run", CallID: "call-1", Args: `{"cmd":"ls"}`}
}

// -------------------------------------------------------------------------
// Tests
// -------------------------------------------------------------------------

// TestDispatcher_TierAndListOrder pins D14: instance → workspace → agent
// tiers, list position within a tier, audit rows in dispatch order.
func TestDispatcher_TierAndListOrder(t *testing.T) {
	srv := newRecordingHookServer(t, http.StatusOK, "", 0)
	hs := &fakeHookStore{
		instanceHooks: []domain.InstanceHook{
			instanceHTTPHook("inst-1", "Instance Gate", domain.HookEventPreToolUse, matchAll(), srv.srv.URL+"/inst"),
		},
		workspaceHooks: []domain.WorkspaceHook{
			workspaceHTTPHook("ws-1", "ws-2", "Second", 1, domain.HookEventPreToolUse, matchAll(), srv.srv.URL+"/ws-second"),
			workspaceHTTPHook("ws-1", "ws-1", "First", 0, domain.HookEventPreToolUse, matchAll(), srv.srv.URL+"/ws-first"),
		},
		agentHooks: []domain.AgentHook{
			agentHTTPHook("ws-1", "ag-1", "agent-1", "Agent Gate", domain.HookEventPreToolUse, matchAll(), srv.srv.URL+"/agent"),
		},
	}
	d := NewDispatcher(hs, dispatcherRegistry(srv.srv))
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	blocked, resultJSON := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
	if blocked || resultJSON != "" {
		t.Fatalf("blocked = %v, json = %q, want clean allow", blocked, resultJSON)
	}

	// List order within the workspace tier is position order (First pos 0
	// before Second pos 1 despite insertion order).
	want := []string{"/inst", "/ws-first", "/ws-second", "/agent"}
	got := srv.hits()
	if len(got) != len(want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hit %d = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}

	// One audit row per executed hook, in dispatch order.
	execs := hs.recordedExecutions()
	if len(execs) != 4 {
		t.Fatalf("audit rows = %d, want 4", len(execs))
	}
	for i, wantName := range []string{"Instance Gate", "First", "Second", "Agent Gate"} {
		if execs[i].HookName != wantName || execs[i].Decision != "allow" {
			t.Errorf("audit[%d] = (%s, %s), want (%s, allow)", i, execs[i].HookName, execs[i].Decision, wantName)
		}
		if execs[i].Event != domain.HookEventPreToolUse || execs[i].WorkspaceID != "ws-1" {
			t.Errorf("audit[%d] = event %s workspace %s, want pre_tool_use/ws-1", i, execs[i].Event, execs[i].WorkspaceID)
		}
	}
}

// TestDispatcher_FirstBlockWinsShortCircuits pins D14's first block wins and
// the canonical block JSON (D3).
func TestDispatcher_FirstBlockWinsShortCircuits(t *testing.T) {
	blocker := newCountingHookServer(t, http.StatusOK, `{"decision":"block","reason":"deployments frozen"}`)
	second := newCountingHookServer(t, http.StatusOK, "")
	hs := &fakeHookStore{
		workspaceHooks: []domain.WorkspaceHook{
			workspaceHTTPHook("ws-1", "ws-1", "Blocker", 0, domain.HookEventPreToolUse, matchAll(), blocker.srv.URL),
			workspaceHTTPHook("ws-1", "ws-2", "Second", 1, domain.HookEventPreToolUse, matchAll(), second.srv.URL),
		},
	}
	d := NewDispatcher(hs, dispatcherRegistry(blocker.srv))
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	blocked, resultJSON := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
	if !blocked {
		t.Fatal("expected block")
	}

	var payload struct {
		BlockedByHook bool   `json:"blocked_by_hook"`
		Hook          string `json:"hook"`
		Reason        string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &payload); err != nil {
		t.Fatalf("block JSON: %v (%q)", err, resultJSON)
	}
	if !payload.BlockedByHook || payload.Hook != "Blocker" || payload.Reason != "deployments frozen" {
		t.Errorf("block payload = %+v, want blocked_by_hook/hook/reason", payload)
	}
	if second.hits() != 0 {
		t.Fatalf("second hook executed after a block (%d hits), want short-circuit", second.hits())
	}
	execs := hs.recordedExecutions()
	if len(execs) != 1 || execs[0].Decision != "block" || execs[0].HookName != "Blocker" {
		t.Fatalf("audit = %+v, want one block row for Blocker", execs)
	}

	// The exported builder matches the wire shape byte-for-byte.
	if again := BlockToolResult("Blocker", "deployments frozen"); again != resultJSON {
		t.Errorf("BlockToolResult = %q, dispatch produced %q", again, resultJSON)
	}
}

// TestDispatcher_MatcherSkipWritesNoAudit pins D19 (matchers gate before any
// dispatch): a non-matching hook is skipped — no handler execution, no audit
// row.
func TestDispatcher_MatcherSkipWritesNoAudit(t *testing.T) {
	srv := newCountingHookServer(t, http.StatusOK, "")
	hs := &fakeHookStore{
		workspaceHooks: []domain.WorkspaceHook{
			workspaceHTTPHook("ws-1", "ws-1", "Web Only", 0, domain.HookEventPreToolUse, matchOnly("web.fetch"), srv.srv.URL),
		},
	}
	d := NewDispatcher(hs, dispatcherRegistry(srv.srv))
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
	if blocked {
		t.Fatal("unexpected block")
	}
	if srv.hits() != 0 {
		t.Fatalf("handler executed %d times on non-matching hook, want 0", srv.hits())
	}
	if rows := hs.recordedExecutions(); len(rows) != 0 {
		t.Fatalf("audit rows = %d, want 0 on matcher skip", len(rows))
	}

	// The matching tool name executes and audits (fresh CallID — the first
	// call's allow outcome is cached per D4).
	tool := dispatcherTool()
	tool.Name = "web.fetch"
	tool.CallID = "call-web-1"
	blocked, _ = resolved.PreToolUse(context.Background(), dispatcherEvent(), tool)
	if blocked || srv.hits() != 1 || len(hs.recordedExecutions()) != 1 {
		t.Fatalf("matching occurrence: blocked=%v hits=%d rows=%d, want false/1/1",
			blocked, srv.hits(), len(hs.recordedExecutions()))
	}
}

// TestDispatcher_IfGateSkipsNonMatchingInputs pins D20: the if condition
// narrows a tool-event hook — a matching input proceeds to the handler, a
// non-matching one skips the hook entirely (no execution, no audit row), and
// the gate never blocks by itself. The pattern is matched against the
// serialized tool-input JSON the event carries.
func TestDispatcher_IfGateSkipsNonMatchingInputs(t *testing.T) {
	srv := newCountingHookServer(t, http.StatusOK, "")
	hs := &fakeHookStore{
		workspaceHooks: []domain.WorkspaceHook{
			workspaceHTTPHook("ws-1", "ws-1", "Secret Reader Gate", 0, domain.HookEventPreToolUse, matchOnly("read_file"), srv.srv.URL),
		},
	}
	hs.workspaceHooks[0].If = "read_file(secret|credential)"

	d := NewDispatcher(hs, dispatcherRegistry(srv.srv))
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Tool and family match, but the input does not hit the pattern: the
	// hook is skipped with no handler call and no audit row.
	plain := EventTool{Name: "read_file", CallID: "call-plain", Args: `{"path":"/tmp/notes.txt"}`}
	if blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), plain); blocked {
		t.Fatal("non-matching if input must not block")
	}
	if srv.hits() != 0 || len(hs.recordedExecutions()) != 0 {
		t.Fatalf("non-matching if executed hooks (hits=%d rows=%d), want none",
			srv.hits(), len(hs.recordedExecutions()))
	}

	// A matching input (pattern hits the args JSON) proceeds to the handler
	// and audits normally.
	secret := EventTool{Name: "read_file", CallID: "call-secret", Args: `{"path":"/etc/credential"}`}
	if blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), secret); blocked {
		t.Fatal("matching if input must not block an allow-decision handler")
	}
	if srv.hits() != 1 || len(hs.recordedExecutions()) != 1 {
		t.Fatalf("matching if executed hooks (hits=%d rows=%d), want 1/1",
			srv.hits(), len(hs.recordedExecutions()))
	}
}

// TestDispatcher_IfGateNameRules pins the if-name matching rules (D20): the
// name part follows the matcher entry rules — exact names and trailing-".*"
// families — and a name miss skips the hook without touching the pattern.
func TestDispatcher_IfGateNameRules(t *testing.T) {
	srv := newCountingHookServer(t, http.StatusOK, "")
	hs := &fakeHookStore{
		workspaceHooks: []domain.WorkspaceHook{
			workspaceHTTPHook("ws-1", "ws-1", "Read Gate", 0, domain.HookEventPreToolUse, matchAll(), srv.srv.URL),
		},
	}
	hs.workspaceHooks[0].If = "read_file(url|href)"

	d := NewDispatcher(hs, dispatcherRegistry(srv.srv))
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Name miss: the tool is not read_file, so the hook skips even though
	// the pattern would hit its args JSON.
	if blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(),
		EventTool{Name: "web.fetch", CallID: "c-1", Args: `{"url":"https://x"}`}); blocked {
		t.Fatal("name miss must not block")
	}
	if srv.hits() != 0 || len(hs.recordedExecutions()) != 0 {
		t.Fatalf("name-miss executed hooks (hits=%d rows=%d), want none", srv.hits(), len(hs.recordedExecutions()))
	}

	// Name hit with a pattern hit: proceeds to the handler.
	if blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(),
		EventTool{Name: "read_file", CallID: "c-2", Args: `{"path":"x","href":"y"}`}); blocked {
		t.Fatal("name and pattern hit must not block an allow-decision handler")
	}
	if srv.hits() != 1 {
		t.Fatalf("name+pattern hit executed %d times, want 1", srv.hits())
	}
}

// TestDispatcher_IfGateFamilyName pins the family form of the if name: an
// entry like read_file.* selects the root and every dotted descendant.
func TestDispatcher_IfGateFamilyName(t *testing.T) {
	srv := newCountingHookServer(t, http.StatusOK, "")
	hs := &fakeHookStore{
		workspaceHooks: []domain.WorkspaceHook{
			workspaceHTTPHook("ws-1", "ws-1", "Family Gate", 0, domain.HookEventPreToolUse, matchAll(), srv.srv.URL),
		},
	}
	hs.workspaceHooks[0].If = "read_file.*(secret)"

	d := NewDispatcher(hs, dispatcherRegistry(srv.srv))
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Family member with a matching pattern: proceeds.
	if blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(),
		EventTool{Name: "read_file.raw", CallID: "c-f1", Args: `{"path":"secret.pem"}`}); blocked {
		t.Fatal("family-member hit with pattern hit must not block an allow handler")
	}
	if srv.hits() != 1 {
		t.Fatalf("family-member hit executed %d times, want 1", srv.hits())
	}

	// Same family, pattern miss: skipped, no second audit row.
	if blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(),
		EventTool{Name: "read_file.raw", CallID: "c-f2", Args: `{"path":"notes.txt"}`}); blocked {
		t.Fatal("pattern miss must not block")
	}
	if srv.hits() != 1 || len(hs.recordedExecutions()) != 1 {
		t.Fatalf("pattern miss executed hooks (hits=%d rows=%d), want 1/1",
			srv.hits(), len(hs.recordedExecutions()))
	}
}

// TestDispatcher_IfIgnoredOnNonToolEvents pins that a non-tool event never
// evaluates an if condition (domain rejects the combination at save; a row
// that slipped through anyway must still fire normally).
func TestDispatcher_IfIgnoredOnNonToolEvents(t *testing.T) {
	srv := newCountingHookServer(t, http.StatusOK, "")
	hs := &fakeHookStore{
		workspaceHooks: []domain.WorkspaceHook{
			workspaceHTTPHook("ws-1", "ws-1", "Finish Pager", 0, domain.HookEventRunFinished, matchOnly("failed"), srv.srv.URL),
		},
	}
	hs.workspaceHooks[0].If = "read_file(secret)" // save-invalid for run_finished

	d := NewDispatcher(hs, dispatcherRegistry(srv.srv))
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	resolved.RunFinished(context.Background(), dispatcherEvent(), "failed")
	deadline := time.Now().Add(3 * time.Second)
	for len(hs.recordedExecutions()) < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if rows := len(hs.recordedExecutions()); rows != 1 {
		t.Fatalf("run_finished audit rows = %d, want 1 (if never evaluated on non-tool events)", rows)
	}
	if srv.hits() != 1 {
		t.Fatalf("run_finished handler hits = %d, want 1", srv.hits())
	}
}

// TestDispatcher_FailurePolicy pins D9: a handler failure resolves under the
// hook's on_failure policy — allow continues, block fails closed with a
// reason naming the hook and the failure; the audit row records "failure"
// and the hook's health flips to error.
func TestDispatcher_FailurePolicy(t *testing.T) {
	t.Run("on_failure allow continues", func(t *testing.T) {
		dead := newCountingHookServer(t, http.StatusInternalServerError, "boom")
		healthy := newCountingHookServer(t, http.StatusOK, "")
		hs := &fakeHookStore{
			workspaceHooks: []domain.WorkspaceHook{
				workspaceHTTPHook("ws-1", "ws-1", "Dead", 0, domain.HookEventPreToolUse, matchAll(), dead.srv.URL),
				workspaceHTTPHook("ws-1", "ws-2", "Healthy", 1, domain.HookEventPreToolUse, matchAll(), healthy.srv.URL),
			},
		}
		d := NewDispatcher(hs, dispatcherRegistry(dead.srv))
		resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if blocked {
			t.Fatal("on_failure=allow must not block")
		}
		if healthy.hits() != 1 {
			t.Fatalf("chain stopped after fail-open hook (%d hits), want continue", healthy.hits())
		}
		execs := hs.recordedExecutions()
		if len(execs) != 2 || execs[0].Decision != "failure" || execs[1].Decision != "allow" {
			t.Fatalf("audit = %+v, want failure then allow", execs)
		}
		statuses := hs.recordedStatuses()
		if len(statuses) != 2 || statuses[0].Status != domain.HookStatusError || statuses[1].Status != domain.HookStatusOK {
			t.Fatalf("statuses = %+v, want error then ok", statuses)
		}
	})

	t.Run("on_failure block fails closed", func(t *testing.T) {
		dead := newCountingHookServer(t, http.StatusInternalServerError, "boom")
		hs := &fakeHookStore{
			workspaceHooks: []domain.WorkspaceHook{
				workspaceHTTPHook("ws-1", "ws-1", "Safety Gate", 0, domain.HookEventPreToolUse, matchAll(), dead.srv.URL),
			},
		}
		hs.workspaceHooks[0].OnFailure = domain.HookFailureBlock
		d := NewDispatcher(hs, dispatcherRegistry(dead.srv))
		resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		blocked, resultJSON := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if !blocked {
			t.Fatal("on_failure=block must block on handler failure")
		}
		if want := `"blocked_by_hook":true`; !contains(resultJSON, want) {
			t.Errorf("block JSON = %q, want it to contain %s", resultJSON, want)
		}
		execs := hs.recordedExecutions()
		if len(execs) != 1 || execs[0].Decision != "failure" || execs[0].Detail == "" {
			t.Fatalf("audit = %+v, want one failure row with detail", execs)
		}
	})

	t.Run("timeout is a failure under the hook's budget", func(t *testing.T) {
		slow := newRecordingHookServer(t, http.StatusOK, "", 300*time.Millisecond)
		hs := &fakeHookStore{
			workspaceHooks: []domain.WorkspaceHook{
				workspaceHTTPHook("ws-1", "ws-1", "Slow Gate", 0, domain.HookEventPreToolUse, matchAll(), slow.srv.URL),
			},
		}
		hs.workspaceHooks[0].TimeoutMS = 50
		hs.workspaceHooks[0].OnFailure = domain.HookFailureBlock
		d := NewDispatcher(hs, dispatcherRegistry(slow.srv))
		resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		start := time.Now()
		blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if !blocked {
			t.Fatal("budget expiry with on_failure=block must block")
		}
		if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
			t.Fatalf("dispatch waited %v, want the 50ms hook budget to bound it", elapsed)
		}
	})
}

// TestDispatcher_PreToolUseDedup pins D4: the same CallID evaluates once for
// the lifetime of the Resolved; the cached outcome (including its block JSON)
// is returned verbatim; a different CallID evaluates fresh.
func TestDispatcher_PreToolUseDedup(t *testing.T) {
	srv := newCountingHookServer(t, http.StatusOK, `{"decision":"block","reason":"nope"}`)
	hs := &fakeHookStore{
		workspaceHooks: []domain.WorkspaceHook{
			workspaceHTTPHook("ws-1", "ws-1", "Gate", 0, domain.HookEventPreToolUse, matchAll(), srv.srv.URL),
		},
	}
	d := NewDispatcher(hs, dispatcherRegistry(srv.srv))
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	tool := dispatcherTool()
	blocked1, json1 := resolved.PreToolUse(context.Background(), dispatcherEvent(), tool)
	blocked2, json2 := resolved.PreToolUse(context.Background(), dispatcherEvent(), tool)
	if !blocked1 || !blocked2 || json1 != json2 {
		t.Fatalf("dedup outcomes differ: (%v,%q) vs (%v,%q)", blocked1, json1, blocked2, json2)
	}
	if hits := srv.hits(); hits != 1 {
		t.Fatalf("handler executed %d times for one CallID, want exactly 1 (D4)", hits)
	}
	if rows := len(hs.recordedExecutions()); rows != 1 {
		t.Fatalf("audit rows = %d after dedup, want 1", rows)
	}

	// A different CallID is a new logical call.
	tool.CallID = "call-2"
	if _, _ = resolved.PreToolUse(context.Background(), dispatcherEvent(), tool); srv.hits() != 2 {
		t.Fatalf("new CallID evaluated %d total hits, want 2", srv.hits())
	}
}

// TestDispatcher_DetachedObserverSurvivesCancel pins D5: observational
// deliveries run on a cancellation-proof context with their own budget and
// still land when the caller cancels immediately after firing.
func TestDispatcher_DetachedObserverSurvivesCancel(t *testing.T) {
	delivered := make(chan string, 3)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond) // outlive the caller's cancel
		delivered <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	hs := &fakeHookStore{
		instanceHooks: []domain.InstanceHook{
			instanceHTTPHook("i-run", "Run Observer", domain.HookEventRunStarted, matchAll(), srv.URL+"/run"),
			instanceHTTPHook("i-post", "Post Observer", domain.HookEventPostToolUse, matchAll(), srv.URL+"/post"),
			instanceHTTPHook("i-fin", "Finish Observer", domain.HookEventRunFinished, matchOnly("failed"), srv.URL+"/finish"),
		},
	}
	d := NewDispatcher(hs, dispatcherRegistry(srv))
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	resolved.ObserveRunStarted(ctx, dispatcherEvent())
	resolved.PostToolUse(ctx, dispatcherEvent(), dispatcherTool())
	resolved.RunFinished(ctx, dispatcherEvent(), "failed")
	cancel() // the run tears down immediately; deliveries must survive

	// All three deliveries must land (in any order — each runs on its own
	// detached goroutine).
	want := map[string]bool{"/run": false, "/post": false, "/finish": false}
	for range want {
		select {
		case got := <-delivered:
			if _, seen := want[got]; !seen {
				t.Fatalf("unexpected delivery %q", got)
			}
			want[got] = true
		case <-time.After(3 * time.Second):
			t.Fatalf("observer deliveries incomplete after caller cancel: %v", want)
		}
	}
	for path, landed := range want {
		if !landed {
			t.Fatalf("observer delivery %q never landed after caller cancel", path)
		}
	}

	// Wait for the audit rows to settle, then assert all three recorded.
	deadline := time.Now().Add(3 * time.Second)
	for len(hs.recordedExecutions()) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	execs := hs.recordedExecutions()
	if len(execs) != 3 {
		t.Fatalf("audit rows = %d, want 3", len(execs))
	}
	for _, row := range execs {
		if row.Decision != "allow" {
			t.Errorf("decision = %q, want allow", row.Decision)
		}
	}
}

// TestDispatcher_GracefulSkip pins D7: an uninterpretable hook gets status
// error at resolve time, does not run, and its on_failure policy applies as
// a failed execution — allow continues, block fails closed.
func TestDispatcher_GracefulSkip(t *testing.T) {
	t.Run("unknown handler type", func(t *testing.T) {
		healthy := newCountingHookServer(t, http.StatusOK, "")
		hs := &fakeHookStore{
			instanceHooks: []domain.InstanceHook{
				instanceHTTPHook("i-broken", "Future Hook", domain.HookEventPreToolUse, matchAll(), healthy.srv.URL),
			},
			workspaceHooks: []domain.WorkspaceHook{
				workspaceHTTPHook("ws-1", "ws-1", "Healthy", 0, domain.HookEventPreToolUse, matchAll(), healthy.srv.URL),
			},
		}
		hs.instanceHooks[0].HandlerType = domain.HookHandlerType("carrier-pigeon")
		d := NewDispatcher(hs, dispatcherRegistry(healthy.srv))
		resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		// Resolve persisted the error status.
		statuses := hs.recordedStatuses()
		if len(statuses) != 1 || statuses[0].HookID != "i-broken" || statuses[0].Status != domain.HookStatusError {
			t.Fatalf("statuses = %+v, want one error write for i-broken", statuses)
		}

		// on_failure=allow default: skip does not block, chain continues.
		blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if blocked {
			t.Fatal("skipped hook with on_failure=allow must not block")
		}
		if healthy.hits() != 1 {
			t.Fatalf("chain stopped after skipped hook (%d hits), want continue", healthy.hits())
		}
		execs := hs.recordedExecutions()
		if len(execs) != 2 || execs[0].Decision != "failure" || execs[0].HookName != "Future Hook" {
			t.Fatalf("audit = %+v, want failure row for the skipped hook then allow", execs)
		}
	})

	t.Run("skipped blocking gate fails closed", func(t *testing.T) {
		hs := &fakeHookStore{
			instanceHooks: []domain.InstanceHook{
				instanceHTTPHook("i-gate", "Old Gate", domain.HookEventPreToolUse, matchAll(), "http://unused.invalid"),
			},
		}
		hs.instanceHooks[0].HandlerType = domain.HookHandlerType("quantum")
		hs.instanceHooks[0].OnFailure = domain.HookFailureBlock
		d := NewDispatcher(hs, dispatcherRegistry(newCountingHookServer(t, http.StatusOK, "").srv))
		resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		blocked, resultJSON := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if !blocked {
			t.Fatal("skipped gate with on_failure=block must fail closed")
		}
		if want := `"hook":"Old Gate"`; !contains(resultJSON, want) {
			t.Errorf("block JSON = %q, want it to name the skipped hook", resultJSON)
		}
	})

	t.Run("unknown event, uncompilable matcher, and uncompilable if are marked and never dispatched", func(t *testing.T) {
		srv := newCountingHookServer(t, http.StatusOK, "")
		hs := &fakeHookStore{
			instanceHooks: []domain.InstanceHook{
				instanceHTTPHook("i-event", "Wrong Event", domain.HookEvent("wormhole_opened"), matchAll(), srv.srv.URL),
				instanceHTTPHook("i-matcher", "Bad Matcher", domain.HookEventPreToolUse, "[", srv.srv.URL),
				instanceHTTPHook("i-if", "Bad If", domain.HookEventPreToolUse, matchAll(), srv.srv.URL),
			},
		}
		hs.instanceHooks[2].If = "read_file([)"
		d := NewDispatcher(hs, dispatcherRegistry(srv.srv))
		if _, err := d.Resolve(context.Background(), "ws-1", "ag-1"); err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		statuses := hs.recordedStatuses()
		if len(statuses) != 3 {
			t.Fatalf("statuses = %+v, want error writes for all three broken hooks", statuses)
		}
		marked := map[string]bool{}
		for _, s := range statuses {
			if s.Status != domain.HookStatusError {
				t.Errorf("status for %s = %q, want error", s.HookID, s.Status)
			}
			marked[s.HookID] = true
		}
		if !marked["i-event"] || !marked["i-matcher"] || !marked["i-if"] {
			t.Fatalf("marked = %v, want all three ids", marked)
		}

		// The unknown-event hook never fires on any known event; the
		// bad-matcher and bad-if hooks fail per on_failure (allow) without a
		// handler call.
		resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if blocked {
			t.Fatal("unexpected block from broken hooks with default on_failure")
		}
		if srv.hits() != 0 {
			t.Fatalf("handler executed %d times for broken hooks, want 0", srv.hits())
		}
	})
}

// TestDispatcher_PromptCap pins D12: over max_invocations_per_run the
// evaluator is never called — allow, recorded as capped.
func TestDispatcher_PromptCap(t *testing.T) {
	model := &fakeEvaluatorModel{call: decideArgs("block", "policy says no", false)}
	factory := &fakeEvaluatorFactory{model: model}
	hs := &fakeHookStore{
		workspaceHooks: []domain.WorkspaceHook{
			workspaceHTTPHook("ws-1", "ws-1", "LLM Gate", 0, domain.HookEventPreToolUse, matchOnly("shell.run"), ""),
		},
	}
	hs.workspaceHooks[0].HandlerType = domain.HookHandlerPrompt
	hs.workspaceHooks[0].TimeoutMS = domain.DefaultPromptHookTimeoutMS
	hs.workspaceHooks[0].Config = json.RawMessage(`{"provider":"openai","model":"gpt-4o","prompt":"policy","max_invocations_per_run":2}`)
	reg := dispatcherRegistry(newCountingHookServer(t, http.StatusOK, "").srv)
	reg.evaluatorFactory = factory
	d := NewDispatcher(hs, reg)
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	tool := dispatcherTool() // shell.run — matches
	for i, callID := range []string{"c1", "c2", "c3", "c4"} {
		tool.CallID = callID
		blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), tool)
		wantBlocked := i < 2 // cap 2: first two evaluate (block), rest capped-allow
		if blocked != wantBlocked {
			t.Errorf("call %d (%s): blocked = %v, want %v", i, callID, blocked, wantBlocked)
		}
	}
	if factory.gotWorkspaceID == "" {
		t.Fatal("evaluator factory never consulted")
	}

	execs := hs.recordedExecutions()
	decisions := map[string]int{}
	for _, e := range execs {
		decisions[e.Decision]++
	}
	if decisions["block"] != 2 || decisions["capped"] != 2 {
		t.Fatalf("decisions = %v, want 2 block + 2 capped", decisions)
	}
}

// TestDispatcher_EvaluatePromptSubmission pins the blocking prompt seam:
// origin matchers gate the evaluation, first block wins with the hook's
// reason, and no evaluation without a matching origin.
func TestDispatcher_EvaluatePromptSubmission(t *testing.T) {
	srv := newCountingHookServer(t, http.StatusOK, `{"decision":"block","reason":"cron prompts need review"}`)
	hs := &fakeHookStore{
		workspaceHooks: []domain.WorkspaceHook{
			workspaceHTTPHook("ws-1", "ws-1", "Cron Gate", 0, domain.HookEventUserPromptSubmit, matchOnly("cron"), srv.srv.URL),
		},
	}
	d := NewDispatcher(hs, dispatcherRegistry(srv.srv))
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	blocked, hookName, reason := resolved.EvaluatePromptSubmission(context.Background(), dispatcherEvent())
	if blocked || hookName != "" || reason != "" {
		t.Fatalf("user-origin prompt: (%v, %q, %q), want no evaluation", blocked, hookName, reason)
	}
	if srv.hits() != 0 || len(hs.recordedExecutions()) != 0 {
		t.Fatalf("non-matching origin executed hooks (hits=%d rows=%d), want none", srv.hits(), len(hs.recordedExecutions()))
	}

	ev := dispatcherEvent()
	ev.Origin = "cron"
	blocked, hookName, reason = resolved.EvaluatePromptSubmission(context.Background(), ev)
	if !blocked || hookName != "Cron Gate" || reason != "cron prompts need review" {
		t.Fatalf("cron-origin prompt: (%v, %q, %q), want block by Cron Gate", blocked, hookName, reason)
	}
}

// TestDispatcher_NoopDispatcher pins the runner default: an empty chain,
// every seam a no-op, no errors.
func TestDispatcher_NoopDispatcher(t *testing.T) {
	resolved, err := NewDispatcher(nil, nil).Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.HasHooks() {
		t.Fatal("noop dispatcher resolved hooks")
	}
	if blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool()); blocked {
		t.Fatal("noop dispatcher blocked")
	}
	if blocked, _, _ := resolved.EvaluatePromptSubmission(context.Background(), dispatcherEvent()); blocked {
		t.Fatal("noop dispatcher blocked a prompt")
	}
	resolved.ObserveRunStarted(context.Background(), dispatcherEvent())
	resolved.PostToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
	resolved.RunFinished(context.Background(), dispatcherEvent(), "completed")
}

// TestDispatcher_DisabledHooksResolveToNothing: enabled=false rows never join
// the chain.
func TestDispatcher_DisabledHooksResolveToNothing(t *testing.T) {
	srv := newCountingHookServer(t, http.StatusOK, "")
	hs := &fakeHookStore{
		workspaceHooks: []domain.WorkspaceHook{
			workspaceHTTPHook("ws-1", "ws-1", "Paused", 0, domain.HookEventPreToolUse, matchAll(), srv.srv.URL),
		},
	}
	hs.workspaceHooks[0].Enabled = false
	d := NewDispatcher(hs, dispatcherRegistry(srv.srv))
	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.HasHooks() {
		t.Fatal("disabled hook joined the chain")
	}
	if blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool()); blocked {
		t.Fatal("disabled hook blocked")
	}
	if srv.hits() != 0 {
		t.Fatalf("disabled hook executed %d times, want 0", srv.hits())
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
