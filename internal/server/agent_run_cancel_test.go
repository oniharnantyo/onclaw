package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/services"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// cancelBlockingModel holds the in-flight model call open until the run's
// context is cancelled — modelling a long tool/model execution that the
// cancel endpoint interrupts at a safe point. Like the runner-suite gate
// model, the cancelled Stream call fails with the context error so the run
// unwinds through the safe-point path.
type cancelBlockingModel struct {
	mu      sync.Mutex
	started chan struct{}
}

func newCancelBlockingModel() *cancelBlockingModel {
	return &cancelBlockingModel{started: make(chan struct{})}
}

// startedChan snapshots the started channel under the model's lock so
// waitStarted never races with Generate's nil-ing of the field.
func (m *cancelBlockingModel) startedChan() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func (m *cancelBlockingModel) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-m.startedChan():
	case <-time.After(5 * time.Second):
		t.Fatal("run never reached the model")
	}
}

func (m *cancelBlockingModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	first := m.started != nil
	if first {
		close(m.started)
		m.started = nil
	}
	m.mu.Unlock()
	// Hold the "in-flight call" open until the run is cancelled.
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *cancelBlockingModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	pipeR, pipeW := schema.Pipe[*schema.AgenticMessage](1)
	_ = pipeW.Send(msg, nil)
	pipeW.Close()
	return pipeR, nil
}

// TestAgentRunCancelEndpoint exercises the run-cancel route end to end: it is
// registered on the workspace group behind agents.write (403 without), maps
// unknown agents/sessions to 404, conflicts (409) when no run is live, and
// cancels a live run (200) whose turn then unwinds with a cancel marker in
// the session history.
func TestAgentRunCancelEndpoint(t *testing.T) {
	ctx := context.Background()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	stor := storagefake.New()
	encKey := []byte("01234567890123456789012345678901")

	ws := &domain.Workspace{Slug: "cancel-ws", Name: "Cancel Workspace"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: "Owner", Permissions: domain.AllPermissions()}
	if err := st.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("create owner role: %v", err)
	}
	memberRole := &domain.Role{WorkspaceID: ws.ID, Name: "Member", Permissions: domain.MemberPermissions}
	if err := st.Roles().Create(ctx, memberRole); err != nil {
		t.Fatalf("create member role: %v", err)
	}
	owner := &domain.User{Email: "owner@example.com", Name: "Owner"}
	if err := st.Users().Create(ctx, owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member := &domain.User{Email: "member@example.com", Name: "Member"}
	if err := st.Users().Create(ctx, member); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("add owner member: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: member.ID, RoleID: memberRole.ID}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "fake", Name: "Fake"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	agent := &domain.Agent{
		WorkspaceID: ws.ID,
		Name:        "Atlas",
		Slug:        "atlas",
		ProviderID:  prov.ID,
		Model:       "fake-model",
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	tempDir := t.TempDir()
	blockingModel := newCancelBlockingModel()
	runner := agents.NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(),
		encKey, tempDir,
		agents.WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (agents.Model, error) {
			return blockingModel, nil
		}),
	)
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(tempDir), ws.Slug, agent.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}

	// Seed a persisted session (sess-1) so the session-existence check finds
	// it; sess-unknown stays absent.
	if err := st.SessionEvents().AppendEvents(ctx, ws.ID, []domain.SessionEvent{{
		SessionID:   "sess-1",
		EventID:     "evt-1",
		TurnID:      "turn-0",
		Kind:        "message",
		Payload:     []byte(`{}`),
		OccurredAt:  time.Now().UTC(),
		WorkspaceID: ws.ID,
	}}); err != nil {
		t.Fatalf("append session event: %v", err)
	}

	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "smoke-secret-key-that-is-at-least-32-chars-long!",
		TTL:    time.Hour,
	})
	ownerToken, err := issuer.Issue(ctx, owner)
	if err != nil {
		t.Fatalf("issue owner token: %v", err)
	}
	memberToken, err := issuer.Issue(ctx, member)
	if err != nil {
		t.Fatalf("issue member token: %v", err)
	}

	r := server.NewRouter(server.RouterOptions{
		Store:         st,
		Storage:       stor,
		Issuer:        issuer,
		EncryptionKey: encKey,
		AgentService:  promptgen.NewService(st.Agents(), st.Providers(), encKey),
		WorkspaceDir:  filepath.Join(tempDir, "workspaces"),
		Runner:        runner,
	})
	ts := httptest.NewServer(r)
	defer ts.Close()

	cancelURL := func(agentSlug, session, turn string) string {
		return fmt.Sprintf("%s/api/v1/workspaces/%s/agents/%s/sessions/%s/runs/%s/cancel",
			ts.URL, ws.Slug, agentSlug, session, turn)
	}
	do := func(token, url string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return http.DefaultClient.Do(req)
	}

	// Wrong permission (member lacks agents.write) → 403.
	resp, err := do(memberToken, cancelURL(agent.Slug, "sess-1", "turn-1"))
	if err != nil {
		t.Fatalf("POST as member: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("member cancel: expected 403, got %d", resp.StatusCode)
	}

	// Unknown agent → 404.
	resp, err = do(ownerToken, cancelURL("ghost", "sess-1", "turn-1"))
	if err != nil {
		t.Fatalf("POST unknown agent: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown agent: expected 404, got %d", resp.StatusCode)
	}

	// Unknown session → 404.
	resp, err = do(ownerToken, cancelURL(agent.Slug, "sess-unknown", "turn-1"))
	if err != nil {
		t.Fatalf("POST unknown session: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown session: expected 404, got %d", resp.StatusCode)
	}

	// Known session, no live run → 409.
	resp, err = do(ownerToken, cancelURL(agent.Slug, "sess-1", "turn-1"))
	if err != nil {
		t.Fatalf("POST no live run: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("no live run: expected 409, got %d", resp.StatusCode)
	}

	// Live run → 200, and the run unwinds with a cancelled event on its
	// stream and a cancel marker in history.
	stream, err := runner.Run(ctx, agents.ExecRequest{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		SessionID:   "sess-1",
		UserID:      owner.ID,
		Input:       "long-running task",
	})
	if err != nil {
		t.Fatalf("runner.Run: %v", err)
	}
	blockingModel.waitStarted(t)

	resp, err = do(ownerToken, cancelURL(agent.Slug, "sess-1", "turn-1"))
	if err != nil {
		t.Fatalf("POST live-run cancel: %v", err)
	}
	var body struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !body.Cancelled {
		t.Errorf("live-run cancel: expected 200 {cancelled:true}, got %d cancelled=%v", resp.StatusCode, body.Cancelled)
	}

	// The cancelled run reaches a terminal state, reporting cancellation.
	streamCancelled := false
	var seenKinds []agents.TranscriptEventKind
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			ev, err := stream.Recv()
			if err != nil {
				return
			}
			seenKinds = append(seenKinds, ev.Kind)
			if ev.Kind == agents.TranscriptEventCancelled {
				streamCancelled = true
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled run never reached a terminal state")
	}
	if !streamCancelled {
		t.Errorf("expected a cancelled event on the cancelled run's stream, got %v", seenKinds)
	}
	// Deregistration lags full unwind by one manager goroutine hop; poll until
	// the run is no longer live rather than asserting immediately.
	live := true
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if !runner.CancelRun(ws.ID, agent.ID, "sess-1") {
			live = false
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if live {
		t.Error("cancelled run must no longer be live")
	}
	// The cancel marker's persistence through the safe-point path is covered
	// by the runner-side suite (internal/agents, tasks 4.2/4.3).
}
