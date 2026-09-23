package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/services"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// TestApprovalsEndpoint_NoPendingApprovalConflicts exercises the approval
// resolution endpoint's guard rails: without a pending approval matching the
// interrupt ID the request conflicts (409); without a decision it is invalid.
func TestApprovalsEndpoint_NoPendingApprovalConflicts(t *testing.T) {
	ctx := context.Background()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	stor := storagefake.New()
	encKey := []byte("01234567890123456789012345678901")

	ws := &domain.Workspace{Slug: "appr-ws", Name: "Approval Workspace"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: "Owner", Permissions: domain.AllPermissions()}
	if err := st.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("create role: %v", err)
	}
	user := &domain.User{Email: "approver@example.com", Name: "Approver"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: ownerRole.ID}); err != nil {
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
		Tools:       []string{"execute"},
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	tempDir := t.TempDir()
	memLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	memWorker := memory.NewWorker(
		memory.NewGister(st.MemoryEvents(), st.SessionEvents(), st.MemoryEntities(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog),
		memory.NewGate(st.MemoryNotes(), st.MemoryEntities(), st.Memories(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog),
		memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), encKey, providers.NewRegistry()),
		st.MemoryEmbeddings(),
		memLog,
	)
	memSearch := newTestMemorySearcher(st)
	memGate := memory.NewIntentGate(st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog)
	runner := agents.NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), memWorker, memSearch, memGate,
		encKey, tempDir,
		agents.WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (agents.Model, error) {
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
	token, err := issuer.Issue(ctx, user)
	if err != nil {
		t.Fatalf("issue token: %v", err)
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

	url := func(interruptID string) string {
		return fmt.Sprintf("%s/api/v1/workspaces/%s/agents/%s/sessions/sess-1/approvals/%s",
			ts.URL, ws.Slug, agent.Slug, interruptID)
	}
	do := func(method, url, body string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		return http.DefaultClient.Do(req)
	}

	// Missing decision → 400.
	resp, err := do("POST", url("some-interrupt"), `{}`)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing approved: expected 400, got %d", resp.StatusCode)
	}

	// Unknown interrupt → 409 conflict.
	resp, err = do("POST", url("some-interrupt"), `{"approved":true}`)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Errorf("unknown interrupt: expected 409, got %d (%v)", resp.StatusCode, body)
	}
}

// TestApprovalsEndpoint_ToolApprovalDeciderPermission pins the service-run
// write escalation's decider rule (add-integration-authority task 2.4): a
// pending TOOL approval requires the DECIDER to hold integrations.write — a
// member is 403, a holder resumes — while the endpoint itself is unchanged
// for shell approvals.
func TestApprovalsEndpoint_ToolApprovalDeciderPermission(t *testing.T) {
	ctx := context.Background()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	stor := storagefake.New()
	encKey := []byte("01234567890123456789012345678901")

	ws := &domain.Workspace{Slug: "tool-appr-ws", Name: "Tool Approval Workspace"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: "Owner", Permissions: domain.OwnerPermissions}
	if err := st.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("create owner role: %v", err)
	}
	workerRole := &domain.Role{WorkspaceID: ws.ID, Name: "Workers", Permissions: domain.MemberPermissions}
	if err := st.Roles().Create(ctx, workerRole); err != nil {
		t.Fatalf("create worker role: %v", err)
	}
	owner := &domain.User{Email: "owner@example.com", Name: "Owner"}
	if err := st.Users().Create(ctx, owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	worker := &domain.User{Email: "worker@example.com", Name: "Worker"}
	if err := st.Users().Create(ctx, worker); err != nil {
		t.Fatalf("create worker: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("add owner member: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: worker.ID, RoleID: workerRole.ID}); err != nil {
		t.Fatalf("add worker member: %v", err)
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
	memLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	memWorker := memory.NewWorker(
		memory.NewGister(st.MemoryEvents(), st.SessionEvents(), st.MemoryEntities(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog),
		memory.NewGate(st.MemoryNotes(), st.MemoryEntities(), st.Memories(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog),
		memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), encKey, providers.NewRegistry()),
		st.MemoryEmbeddings(),
		memLog,
	)
	memSearch := newTestMemorySearcher(st)
	memGate := memory.NewIntentGate(st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog)
	runner := agents.NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), memWorker, memSearch, memGate,
		encKey, tempDir,
		agents.WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (agents.Model, error) {
			return &smokeChatModel{}, nil
		}),
	)
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(tempDir), ws.Slug, agent.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}

	// Seed a pending service-run write escalation: a persisted interrupt
	// session event carrying the tool payload (the hydrated state the
	// endpoint's decider check reads through PendingApproval).
	const sessionID = "sess-escalation"
	adapter := agents.NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	interrupt := &adk.SessionEvent[*schema.AgenticMessage]{
		EventID:   "evt-tool-interrupt",
		TurnID:    "turn-1",
		Timestamp: time.Now().UTC(),
		Kind:      adk.SessionEventInterrupt,
		Interrupt: &adk.InterruptEvent{
			Contexts: []*adk.InterruptContext{{
				InterruptID: "interrupt-tool-1",
				Info: agents.ToolApprovalInfo{
					Name:         "github.merge_pull_request",
					Service:      "github",
					ServiceName:  "GitHub",
					ConnectionID: "conn-1",
					Tier:         domain.RecipeToolTierWrite,
				},
			}},
		},
	}
	if err := adapter.AppendEvents(ctx, sessionID, []*adk.SessionEvent[*schema.AgenticMessage]{interrupt}); err != nil {
		t.Fatalf("append interrupt event: %v", err)
	}

	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "smoke-secret-key-that-is-at-least-32-chars-long!",
		TTL:    time.Hour,
	})
	ownerToken, err := issuer.Issue(ctx, owner)
	if err != nil {
		t.Fatalf("issue owner token: %v", err)
	}
	workerToken, err := issuer.Issue(ctx, worker)
	if err != nil {
		t.Fatalf("issue worker token: %v", err)
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

	url := fmt.Sprintf("%s/api/v1/workspaces/%s/agents/%s/sessions/%s/approvals/interrupt-tool-1",
		ts.URL, ws.Slug, agent.Slug, sessionID)
	do := func(token, body string) int {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// A member (no integrations.write) may not decide the escalation.
	if got := do(workerToken, `{"approved":true}`); got != http.StatusForbidden {
		t.Errorf("member decider: expected 403, got %d", got)
	}
	if got := do(workerToken, `{"approved":false}`); got != http.StatusForbidden {
		t.Errorf("member denial: expected 403, got %d", got)
	}

	// An integrations.write holder resumes the run.
	if got := do(ownerToken, `{"approved":true}`); got != http.StatusOK {
		t.Errorf("owner decider: expected 200, got %d", got)
	}
}
