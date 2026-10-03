package server_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/agents/backend"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/services"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ownershipBlockingModel holds one in-flight model call open until the run's
// context is cancelled — the live-run precondition the cancel endpoint's
// happy path needs. Each instance carries its own started channel, so two
// live runs can be awaited independently.
type ownershipBlockingModel struct {
	mu      sync.Mutex
	started chan struct{}
}

func newOwnershipBlockingModel() *ownershipBlockingModel {
	return &ownershipBlockingModel{started: make(chan struct{})}
}

func (m *ownershipBlockingModel) waitStarted(t *testing.T) {
	t.Helper()
	m.mu.Lock()
	ch := m.started
	m.mu.Unlock()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("run never reached the model")
	}
}

func (m *ownershipBlockingModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	if m.started != nil {
		close(m.started)
		m.started = nil
	}
	m.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *ownershipBlockingModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	pipeR, pipeW := schema.Pipe[*schema.AgenticMessage](1)
	_ = pipeW.Send(msg, nil)
	pipeW.Close()
	return pipeR, nil
}

// seedOwnershipInterrupt appends a persisted interrupt event the approvals
// endpoint's PendingApproval read hydrates: a shell approval (ShellApprovalInfo,
// no Tool) or a service-run write escalation (ToolApprovalInfo with the write
// tier).
func seedOwnershipInterrupt(t *testing.T, st store.Store, wsID, sessionID, interruptID string, info any) {
	t.Helper()
	adapter := agents.NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), wsID)
	interrupt := &adk.SessionEvent[*schema.AgenticMessage]{
		EventID:   "evt-" + interruptID,
		TurnID:    "turn-1",
		Timestamp: time.Now().UTC(),
		Kind:      adk.SessionEventInterrupt,
		Interrupt: &adk.InterruptEvent{
			Contexts: []*adk.InterruptContext{{
				InterruptID: interruptID,
				Info:        info,
			}},
		},
	}
	if err := adapter.AppendEvents(context.Background(), sessionID, []*adk.SessionEvent[*schema.AgenticMessage]{interrupt}); err != nil {
		t.Fatalf("append interrupt event: %v", err)
	}
}

// TestSessionOwnershipRule pins the member own-session grants (fix-role-
// permission-audit design D3, tasks 3.2): cancelling a run, resolving an
// approval, and deleting a session are permitted for the session's owning
// member or an agents.write holder — the routes carry no permission gate
// anymore, the handlers enforce the rule.
func TestSessionOwnershipRule(t *testing.T) {
	ctx := context.Background()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	stor := storagefake.New()
	encKey := []byte("01234567890123456789012345678901")

	ws := &domain.Workspace{Slug: "own-ws", Name: "Ownership Workspace"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: "Owner", Permissions: domain.OwnerPermissions}
	if err := st.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("create owner role: %v", err)
	}
	adminRole := &domain.Role{WorkspaceID: ws.ID, Name: "Admin", Permissions: domain.AdminPermissions}
	if err := st.Roles().Create(ctx, adminRole); err != nil {
		t.Fatalf("create admin role: %v", err)
	}
	memberRole := &domain.Role{WorkspaceID: ws.ID, Name: "Member", Permissions: domain.MemberPermissions}
	if err := st.Roles().Create(ctx, memberRole); err != nil {
		t.Fatalf("create member role: %v", err)
	}
	owner := &domain.User{Email: "owner@example.com", Name: "Owner"}
	member := &domain.User{Email: "member@example.com", Name: "Member"}
	admin := &domain.User{Email: "admin@example.com", Name: "Admin"}
	for _, u := range []*domain.User{owner, member, admin} {
		if err := st.Users().Create(ctx, u); err != nil {
			t.Fatalf("create user %s: %v", u.Email, err)
		}
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("add owner member: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: admin.ID, RoleID: adminRole.ID}); err != nil {
		t.Fatalf("add admin member: %v", err)
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

	// One indexed session per member: the ownership fact the rule reads.
	if err := st.AgentSessions().UpsertAgentSession(ctx, ws.ID, agent.ID, member.ID, domain.AgentSessionUpsert{SessionID: "sess-member", Title: "Member topic"}); err != nil {
		t.Fatalf("seed member session: %v", err)
	}
	if err := st.AgentSessions().UpsertAgentSession(ctx, ws.ID, agent.ID, owner.ID, domain.AgentSessionUpsert{SessionID: "sess-owner", Title: "Owner topic"}); err != nil {
		t.Fatalf("seed owner session: %v", err)
	}
	// A third member-owned session hosts the approval cases, isolated from
	// the live-run session (a cancel marker after an interrupt reads as
	// resolved — PendingApproval's rule).
	if err := st.AgentSessions().UpsertAgentSession(ctx, ws.ID, agent.ID, member.ID, domain.AgentSessionUpsert{SessionID: "sess-approve", Title: "Approvals"}); err != nil {
		t.Fatalf("seed approval session: %v", err)
	}

	// A pending shell approval on the member's own session — the 200 happy
	// path (a denial decision: nothing executes).
	seedOwnershipInterrupt(t, st, ws.ID, "sess-approve", "interrupt-shell-1", backend.ShellApprovalInfo{Command: "ls /tmp"})

	tempDir := t.TempDir()
	memLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	memWorker := memory.NewWorker(
		memory.NewGister(st.MemoryEvents(), st.SessionEvents(), st.MemoryEntities(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog),
		memory.NewGate(st.MemoryNotes(), st.MemoryEntities(), st.Memories(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog),
		memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), encKey, providers.NewRegistry()),
		st.MemoryEmbeddings(),
		memLog,
	)
	ingestWorker := ingest.NewWorker(memLog, ingest.WithConsumers(memWorker))
	memSearch := newTestMemorySearcher(st)
	memGate := memory.NewIntentGate(st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog)

	// The model factory hands each live run its own blocking model (armed
	// one-shot by startLiveRun); every other run — e.g. an approval resume —
	// resolves the deterministic smoke model.
	var modelMu sync.Mutex
	var armedModel *ownershipBlockingModel
	runner := agents.NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), ingestWorker, memSearch, memGate,
		encKey, tempDir,
		agents.WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (agents.Model, error) {
			modelMu.Lock()
			defer modelMu.Unlock()
			if armedModel != nil {
				m := armedModel
				armedModel = nil
				return m, nil
			}
			return &smokeChatModel{}, nil
		}),
	)
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(tempDir), ws.Slug, agent.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}

	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "smoke-secret-key-that-is-at-least-32-chars-long!",
		TTL:    time.Hour,
	})
	tokens := make(map[string]string, 3)
	for _, u := range []*domain.User{owner, member, admin} {
		token, err := issuer.Issue(ctx, u)
		if err != nil {
			t.Fatalf("issue token for %s: %v", u.Email, err)
		}
		tokens[u.Email] = token
	}
	memberToken, adminToken := tokens[member.Email], tokens[admin.Email]

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

	do := func(t *testing.T, token, method, path, body string) int {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, ts.URL+path, reader)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	cancelURL := func(agentSlug, session, turn string) string {
		return fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/runs/%s/cancel", ws.Slug, agentSlug, session, turn)
	}
	approvalURL := func(agentSlug, session, interruptID string) string {
		return fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/approvals/%s", ws.Slug, agentSlug, session, interruptID)
	}
	sessionURL := func(agentSlug, session string) string {
		return fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s", ws.Slug, agentSlug, session)
	}
	startLiveRun := func(t *testing.T, sessionID, userID string) *agents.EventStream {
		t.Helper()
		blocking := newOwnershipBlockingModel()
		modelMu.Lock()
		armedModel = blocking
		modelMu.Unlock()
		stream, err := runner.Run(ctx, agents.ExecRequest{
			WorkspaceID: ws.ID,
			AgentID:     agent.ID,
			SessionID:   sessionID,
			UserID:      userID,
			Input:       "long-running task",
		})
		if err != nil {
			t.Fatalf("runner.Run: %v", err)
		}
		blocking.waitStarted(t)
		return stream
	}
	awaitTerminal := func(t *testing.T, stream *agents.EventStream) {
		t.Helper()
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				if _, err := stream.Recv(); err != nil {
					return
				}
			}
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled run never reached a terminal state")
		}
	}

	t.Run("owning member cancels their own live run (200)", func(t *testing.T) {
		stream := startLiveRun(t, "sess-member", member.ID)
		if got := do(t, memberToken, http.MethodPost, cancelURL(agent.Slug, "sess-member", "turn-2"), ""); got != http.StatusOK {
			t.Errorf("owner-member cancel: expected 200, got %d", got)
		}
		awaitTerminal(t, stream)
	})

	t.Run("owning member resolves their own shell approval (200)", func(t *testing.T) {
		if got := do(t, memberToken, http.MethodPost, approvalURL(agent.Slug, "sess-approve", "interrupt-shell-1"), `{"approved":false}`); got != http.StatusOK {
			t.Errorf("owner-member shell denial: expected 200, got %d", got)
		}
	})

	t.Run("owning member deletes their own session (204)", func(t *testing.T) {
		if got := do(t, memberToken, http.MethodDelete, sessionURL(agent.Slug, "sess-member"), ""); got != http.StatusNoContent {
			t.Errorf("owner-member delete: expected 204, got %d", got)
		}
	})

	t.Run("non-owner member is 403 on all three foreign-session mutations", func(t *testing.T) {
		if got := do(t, memberToken, http.MethodPost, cancelURL(agent.Slug, "sess-owner", "turn-1"), ""); got != http.StatusForbidden {
			t.Errorf("non-owner cancel: expected 403, got %d", got)
		}
		if got := do(t, memberToken, http.MethodPost, approvalURL(agent.Slug, "sess-owner", "no-such-interrupt"), `{"approved":true}`); got != http.StatusForbidden {
			t.Errorf("non-owner approval: expected 403, got %d", got)
		}
		if got := do(t, memberToken, http.MethodDelete, sessionURL(agent.Slug, "sess-owner"), ""); got != http.StatusForbidden {
			t.Errorf("non-owner delete: expected 403, got %d", got)
		}
	})

	t.Run("admin with agents.write cancels a foreign live run (200) and deletes a foreign session (204)", func(t *testing.T) {
		stream := startLiveRun(t, "sess-owner", owner.ID)
		if got := do(t, adminToken, http.MethodPost, cancelURL(agent.Slug, "sess-owner", "turn-2"), ""); got != http.StatusOK {
			t.Errorf("admin foreign cancel: expected 200, got %d", got)
		}
		awaitTerminal(t, stream)

		// The owner's row is foreign to the admin — the agents.write holder
		// deletes it scoped to the fetched row's owner.
		if got := do(t, adminToken, http.MethodDelete, sessionURL(agent.Slug, "sess-owner"), ""); got != http.StatusNoContent {
			t.Errorf("admin foreign delete: expected 204, got %d", got)
		}
	})

	t.Run("owning member without integrations.write cannot decide a connection-write approval (403)", func(t *testing.T) {
		seedOwnershipInterrupt(t, st, ws.ID, "sess-approve", "interrupt-tool-2", agents.ToolApprovalInfo{
			Name:         "github.merge_pull_request",
			Service:      "github",
			ServiceName:  "GitHub",
			ConnectionID: "conn-1",
			Tier:         domain.RecipeToolTierWrite,
		})
		if got := do(t, memberToken, http.MethodPost, approvalURL(agent.Slug, "sess-approve", "interrupt-tool-2"), `{"approved":true}`); got != http.StatusForbidden {
			t.Errorf("owner-member connection-write approval: expected 403, got %d", got)
		}
	})

	t.Run("absent sessions keep their shapes", func(t *testing.T) {
		// A member probing an unknown session id gets the not-found shape.
		if got := do(t, memberToken, http.MethodDelete, sessionURL(agent.Slug, "sess-ghost"), ""); got != http.StatusNotFound {
			t.Errorf("member absent delete: expected 404, got %d", got)
		}
		// An agents.write holder cancelling an absent session: no live run,
		// no events — still not-found.
		if got := do(t, adminToken, http.MethodPost, cancelURL(agent.Slug, "sess-ghost", "turn-1"), ""); got != http.StatusNotFound {
			t.Errorf("admin absent cancel: expected 404, got %d", got)
		}
	})
}

// TestWorkspaceCreationSuperadminOnly pins the D4 gate on POST /workspaces
// (fix-role-permission-audit 4.1): master-workspace membership with
// admin.workspaces.write — a superadmin creates (201), an ordinary workspace
// member is 403, an unauthenticated caller is 401. The birth transaction is
// unchanged and stays covered by TestWorkspaces_AtomicBirth.
func TestWorkspaceCreationSuperadminOnly(t *testing.T) {
	env := setupTestEnv(t)
	// One superadmin seed per env: SeedSuperadmin skips when a master
	// superadmin already exists, so subtests share this one.
	_, superToken := seedTestSuperadmin(t, env, "gate-root@onclaw.local", "Gate Root", "supersecret123")

	validBirth := func(slug string) map[string]any {
		return map[string]any{
			"name":     "Gated WS " + slug,
			"slug":     slug,
			"timezone": "UTC",
		}
	}

	t.Run("unauthenticated is 401", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", "", validBirth("gate-anon"))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("ordinary master-tenant member is 403", func(t *testing.T) {
		// Seeding the superadmin ensures the master workspace and its built-in
		// roles; a plain master Member holds no admin.workspaces.write.
		user, userToken := createTestUser(t, env, "gate-plain@example.com", "Gate Plain", "password123")
		master, err := env.store.Workspaces().BySlug(context.Background(), domain.MasterWorkspaceSlug)
		if err != nil {
			t.Fatalf("resolve master workspace: %v", err)
		}
		memberRole, err := env.store.Roles().FindByName(context.Background(), master.ID, domain.RoleMember)
		if err != nil {
			t.Fatalf("resolve master member role: %v", err)
		}
		addMember(t, env, master.ID, user.ID, memberRole.ID)

		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", userToken, validBirth("gate-member"))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("superadmin creates a workspace (201)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", superToken, validBirth("gate-super"))
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})
}
