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
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
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

// ---------------------------------------------------------------------------
// add-integration-authority, server-level escalation end-to-end (verifier
// test-adequacy fix): a webhook-shaped run interrupts on a write-tier
// connection tool and the decision flows through the REAL approvals endpoint.
// The resume request the endpoint builds carries no Origin — this suite pins
// that the denied write never dials the upstream and the approved one dials
// exactly once.
// ---------------------------------------------------------------------------

// gateE2EModel is a scripted tool-call model: while the conversation carries
// no tool result yet it calls one named tool; once the result arrives it
// finishes with text. Stateless across runs — several sessions share the
// instance through the model factory.
type gateE2EModel struct {
	toolName string
}

func (m *gateE2EModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	for _, msg := range input {
		if msg == nil {
			continue
		}
		for _, block := range msg.ContentBlocks {
			if block != nil && block.FunctionToolResult != nil {
				return &schema.AgenticMessage{
					Role: schema.AgenticRoleTypeAssistant,
					ContentBlocks: []*schema.ContentBlock{
						{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "done"}},
					},
				}, nil
			}
		}
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeFunctionToolCall, FunctionToolCall: &schema.FunctionToolCall{
				CallID:    "call-e2e-1",
				Name:      m.toolName,
				Arguments: `{}`,
			}},
		},
	}, nil
}

func (m *gateE2EModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

// gateE2ELister answers the runner's connection listing with canned refs.
type gateE2ELister struct{ refs []agents.HTTPConnectionRef }

func (l *gateE2ELister) AttachedHTTPConnections(context.Context, string, string) ([]agents.HTTPConnectionRef, error) {
	return l.refs, nil
}

// gateE2ECreds resolves every connection's credential.
type gateE2ECreds struct{}

func (gateE2ECreds) CredentialForConnection(context.Context, string, string) (string, error) {
	return "e2e-token", nil
}

// gateE2EUpstream counts the provider requests that actually arrived.
type gateE2EUpstream struct {
	*httptest.Server
	mu    chan struct{}
	hits  int
	calls int
}

func newGateE2EUpstream(t *testing.T) *gateE2EUpstream {
	t.Helper()
	u := &gateE2EUpstream{mu: make(chan struct{}, 1)}
	u.mu <- struct{}{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-u.mu
		u.hits++
		u.mu <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *gateE2EUpstream) hitCount() int {
	<-u.mu
	defer func() { u.mu <- struct{}{} }()
	return u.hits
}

// gateE2ERecipeSeq mints unique recipe ids (the domain registry panics on
// duplicates — recipes are global process state).
var gateE2ERecipeSeq int

func registerGateE2ERecipe(t *testing.T, baseURL string) *domain.Recipe {
	t.Helper()
	gateE2ERecipeSeq++
	id := fmt.Sprintf("gate-e2e-%d", gateE2ERecipeSeq)
	r := domain.Recipe{
		ID:           id,
		Service:      "Gate E2E " + id,
		Icon:         "plug",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Kind:         domain.RecipeKindHTTP,
		BaseURL:      baseURL,
		TokenHeader:  "X-Test-Token",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Create a token"}},
		Probe:        domain.RecipeProbe{Method: "GET", Path: "/ping"},
		Verbs: []domain.RecipeVerb{
			{Tool: id + ".create_thing", Method: "POST", Path: "/v1/things", Tier: domain.RecipeToolTierWrite},
		},
	}
	domain.RegisterRecipe(r) // panics on an invalid recipe — the test fails loudly
	return domain.RecipeByID(id)
}

// runToInterrupt drives one webhook-shaped run to its write-escalation
// interrupt, draining the stream to EOF first — the interrupt stream closes
// only after the run's checkpoint finalization has landed, which is the
// earliest point at which a real client's resume can succeed.
func runToInterrupt(ctx context.Context, t *testing.T, runner *agents.Runner, req agents.ExecRequest) *agents.ApprovalPayload {
	t.Helper()
	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("webhook run: %v", err)
	}
	var approval *agents.ApprovalPayload
	lastErr := ""
	for {
		ev, err := stream.Recv()
		if err != nil {
			if approval == nil {
				t.Fatalf("run stream ended without an approval interrupt (lastError=%q)", lastErr)
			}
			return approval
		}
		if ev.Error != "" {
			lastErr = ev.Error
		}
		if ev.Kind == agents.TranscriptEventApprovalRequired && ev.Approval != nil && ev.Approval.Tool != nil {
			approval = ev.Approval
		}
	}
}

// waitFor polls until the predicate holds or the deadline passes.
func waitFor(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// historyKinds dumps the persisted transcript kinds for debugging waits.
func historyKinds(ctx context.Context, runner *agents.Runner, wsID, agentID, sessionID string) string {
	hist, err := runner.History(ctx, agents.HistoryRequest{WorkspaceID: wsID, AgentID: agentID, SessionID: sessionID})
	if err != nil {
		return "history error: " + err.Error()
	}
	kinds := make([]string, 0, len(hist.Events))
	for _, ev := range hist.Events {
		k := string(ev.Kind)
		if ev.ToolResult != nil {
			k += fmt.Sprintf("(%s=%q)", ev.ToolResult.Name, ev.ToolResult.Result)
		}
		if ev.Error != "" {
			k += fmt.Sprintf("(err=%q)", ev.Error)
		}
		kinds = append(kinds, k)
	}
	return strings.Join(kinds, ", ")
}

// TestConnectionGateApprovalEndpoint_WebhookEscalation drives the full
// service-run escalation through the HTTP surface: webhook-shaped run → write
// escalation interrupt → POST /approvals with approved=false → the denied
// tool NEVER dials the upstream and the transcript carries the canonical
// block; approved=true (fresh run) → exactly one dial.
func TestConnectionGateApprovalEndpoint_WebhookEscalation(t *testing.T) {
	ctx := context.Background()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	stor := storagefake.New()
	encKey := []byte("01234567890123456789012345678901")

	upstream := newGateE2EUpstream(t)
	recipe := registerGateE2ERecipe(t, upstream.URL)
	writeName := recipe.ID + ".create_thing"
	connID := "conn-" + recipe.ID

	ws := &domain.Workspace{Slug: "gate-e2e-ws", Name: "Gate E2E Workspace"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: "Owner", Permissions: domain.OwnerPermissions}
	if err := st.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("create owner role: %v", err)
	}
	owner := &domain.User{Email: "gate-owner@example.com", Name: "Owner"}
	if err := st.Users().Create(ctx, owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("add owner member: %v", err)
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

	gateModel := &gateE2EModel{toolName: writeName}
	runner := agents.NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), memWorker, memSearch, memGate,
		encKey, tempDir,
		agents.WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (agents.Model, error) {
			return gateModel, nil
		}),
		agents.WithConnectionToolSource(agents.NewConnectionToolSource(
			&gateE2ELister{refs: []agents.HTTPConnectionRef{{
				ConnectionID: connID,
				Service:      recipe.ID,
				Status:       domain.ConnectionStatusConnected,
				Recipe:       recipe,
			}}},
			gateE2ECreds{},
		)),
	)
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(tempDir), ws.Slug, agent.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}

	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "smoke-secret-key-that-is-at-least-32-chars-long!",
		TTL:    time.Hour,
	})
	ownerToken, err := issuer.Issue(ctx, owner)
	if err != nil {
		t.Fatalf("issue owner token: %v", err)
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

	// webhookRun is the exact ExecRequest the webhook run submitter builds
	// (internal/server/webhooks_runtime.go): OriginService plus the
	// connection/event attribution under a mechanical acting identity.
	webhookRun := func(sessionID string) agents.ExecRequest {
		return agents.ExecRequest{
			WorkspaceID:       ws.ID,
			AgentID:           agent.ID,
			SessionID:         sessionID,
			UserID:            owner.ID, // the acting identity the submitter resolves
			Input:             "handle the thing.created event",
			Origin:            agents.OriginService,
			ConnectionID:      connID,
			ConnectionService: recipe.ID,
			Event:             "thing.created",
		}
	}
	approvalURL := func(sessionID string) string {
		return fmt.Sprintf("%s/api/v1/workspaces/%s/agents/%s/sessions/%s/approvals",
			ts.URL, ws.Slug, agent.Slug, sessionID)
	}
	decide := func(sessionID string, interruptID string, approved bool) int {
		t.Helper()
		body := fmt.Sprintf(`{"approved":%t}`, approved)
		// A real client can POST between the interrupt event persisting and
		// the run's checkpoint finalization landing; the resume then conflicts
		// and the client retries. Retry the same way, surfacing bodies.
		deadline := time.Now().Add(5 * time.Second)
		for {
			req, err := http.NewRequestWithContext(ctx, "POST", approvalURL(sessionID)+"/"+interruptID, strings.NewReader(body))
			if err != nil {
				t.Fatalf("build approval request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+ownerToken)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST approvals: %v", err)
			}
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return resp.StatusCode
			}
			if time.Now().After(deadline) {
				t.Fatalf("POST approvals kept failing (%d): %s", resp.StatusCode, respBody)
			}
			t.Logf("POST approvals %d (retrying): %s", resp.StatusCode, respBody)
			time.Sleep(50 * time.Millisecond)
		}
	}
	historyToolResult := func(sessionID string) string {
		hist, err := runner.History(ctx, agents.HistoryRequest{
			WorkspaceID: ws.ID,
			AgentID:     agent.ID,
			SessionID:   sessionID,
		})
		if err != nil {
			return ""
		}
		result := ""
		for _, ev := range hist.Events {
			if ev.Kind == agents.TranscriptEventToolCallFinished && ev.ToolResult != nil && ev.ToolResult.Name == writeName {
				result = ev.ToolResult.Result
			}
		}
		return result
	}

	// --- Denial: the interrupted run's origin must survive the endpoint's
	// origin-less resume request — the denied write NEVER dials. ---
	denySession := "sess-webhook-deny"
	denyApproval := runToInterrupt(ctx, t, runner, webhookRun(denySession))
	if got := decide(denySession, denyApproval.InterruptID, false); got != http.StatusOK {
		t.Fatalf("denial POST = %d, want 200", got)
	}
	// The resumed turn's persistence is the outcome marker (History
	// synthesizes turn_completed only at a LATER turn boundary, so the final
	// resumed turn never shows one): the denied block lands as the tool
	// result.
	waitFor(t, "the denied block to land in the transcript: "+historyKinds(ctx, runner, ws.ID, agent.ID, denySession),
		func() bool { return historyToolResult(denySession) == agents.GateBlockResult(writeName) })
	if hits := upstream.hitCount(); hits != 0 {
		t.Fatalf("DENIED write dialed the upstream %d time(s) — fail-open on the resume lane", hits)
	}
	if got := historyToolResult(denySession); got != agents.GateBlockResult(writeName) {
		t.Fatalf("denied tool result in transcript = %q, want the canonical block", got)
	}

	// --- Approval: the resumed turn executes the write exactly once. ---
	approveSession := "sess-webhook-approve"
	approveApproval := runToInterrupt(ctx, t, runner, webhookRun(approveSession))
	if got := decide(approveSession, approveApproval.InterruptID, true); got != http.StatusOK {
		t.Fatalf("approval POST = %d, want 200", got)
	}
	waitFor(t, "the approved body to land in the transcript",
		func() bool { return historyToolResult(approveSession) == `{"ok":true}` })
	if hits := upstream.hitCount(); hits != 1 {
		t.Fatalf("approved write dialed the upstream %d time(s), want exactly 1", hits)
	}
	if got := historyToolResult(approveSession); got != `{"ok":true}` {
		t.Fatalf("approved tool result in transcript = %q, want the upstream body", got)
	}

	// The resolved escalation leaves nothing pending (the hydrated state the
	// card renders is gone once decided).
	pending, err := runner.PendingApproval(ctx, ws.ID, approveSession)
	if err != nil {
		t.Fatalf("PendingApproval: %v", err)
	}
	if pending != nil {
		t.Fatalf("resolved escalation still pending: %+v", pending)
	}
}
