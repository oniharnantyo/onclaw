package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// smokeChatModel implements model.BaseModel[*schema.AgenticMessage]
type smokeChatModel struct{}

func (m *smokeChatModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{
				AssistantGenText: &schema.AssistantGenText{
					Text: "Hello from the smoke agent! I am functioning properly.",
				},
			},
		},
	}, nil
}

func (m *smokeChatModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	pipeR, pipeW := schema.Pipe[*schema.AgenticMessage](2)
	go func() {
		defer pipeW.Close()
		_ = pipeW.Send(&schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{
					AssistantGenText: &schema.AssistantGenText{
						Text: "Hello from the streamed smoke agent! I am functioning properly.",
					},
				},
			},
		}, nil)
	}()
	return pipeR, nil
}

// TestSmoke_ManualEngineRunAndHistoryEndpoint executes Task 4.2:
// Starts a real HTTP listener, seeds session_events rows via one engine.Run
// against a local fake model provider, curls the new endpoint via the actual
// network stack, and validates the transcript shape.
func TestSmoke_ManualEngineRunAndHistoryEndpoint(t *testing.T) {
	ctx := context.Background()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	stor := storagefake.New()
	encKey := []byte("01234567890123456789012345678901")

	// 1. Create Workspace
	ws := &domain.Workspace{
		Slug: "smoke-ws",
		Name: "Smoke Workspace",
	}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	// 2. Create Roles
	ownerRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        "Owner",
		Permissions: domain.AllPermissions(),
	}
	if err := st.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("create owner role: %v", err)
	}

	// 3. Create User & Member
	user := &domain.User{
		Email: "smoke@example.com",
		Name:  "Smoke User",
	}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{
		WorkspaceID: ws.ID,
		UserID:      user.ID,
		RoleID:      ownerRole.ID,
	}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// 4. Create Provider
	prov := &domain.ProviderConfig{
		WorkspaceID: ws.ID,
		Type:        "fake",
		Name:        "Fake Provider",
	}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// 5. Create Agent
	agent := &domain.Agent{
		WorkspaceID: ws.ID,
		Name:        "Smoke Bot",
		Slug:        "smoke-bot",
		Role:        "tester",
		ProviderID:  prov.ID,
		Model:       "fake-model",
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// 6. Construct Runner with fake model factory
	agenticFactory := func(ctx context.Context, providerType string, cred providers.Credential, modelName string) (agents.Model, error) {
		return &smokeChatModel{}, nil
	}

	tempDir := t.TempDir()

	runner := agents.NewRunner(
		st.Workspaces(),
		st.Agents(),
		st.Users(),
		st.Members(),
		st.Roles(),
		st.Providers(),
		st.SessionEvents(),
		st.SessionCheckpoints(),
		encKey,
		tempDir,
		agents.WithAgenticModelFactory(agenticFactory),
	)

	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(tempDir), ws.Slug, agent.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}

	// 7. Seed session_events rows via one runner Run
	sessionID := "session-smoke-42"
	t.Logf("Executing runner.Run on session %s...", sessionID)
	stream, err := runner.Run(ctx, agents.ExecRequest{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		SessionID:   sessionID,
		UserID:      user.ID,
		Input:       "Hello agent!",
	})
	if err != nil {
		t.Fatalf("runner.Run failed: %v", err)
	}

	eventCount := 0
	for {
		event, err := stream.Recv()
		if err != nil {
			break
		}
		eventCount++
		t.Logf("    stream event: kind=%s turn_id=%s", event.Kind, event.TurnID)
	}
	t.Logf("Stream completed. Received %d events during execution.", eventCount)

	// 8. Start real HTTP Server on random loopback port
	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "smoke-secret-key-that-is-at-least-32-chars-long!",
		TTL:    time.Hour,
	})
	token, err := issuer.Issue(ctx, user)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	agentSvc := promptgen.NewService(st.Agents(), st.Providers(), encKey)
	r := server.NewRouter(server.RouterOptions{
		Store:         st,
		Storage:       stor,
		Issuer:        issuer,
		EncryptionKey: encKey,
		AgentService:  agentSvc,
		WorkspaceDir:  filepath.Join(tempDir, "workspaces"),
		Runner:        runner,
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serverAddr := listener.Addr().String()

	httpServer := &http.Server{Handler: r}
	go func() {
		_ = httpServer.Serve(listener)
	}()
	defer httpServer.Close()

	endpointURL := fmt.Sprintf("http://%s/api/v1/workspaces/%s/agents/%s/sessions/%s/events", serverAddr, ws.Slug, agent.Slug, sessionID)
	t.Logf("Server listening on %s", serverAddr)
	t.Logf("Executing curl against endpoint: %s", endpointURL)

	// 9. Execute real curl command
	curlCmd := exec.Command("curl", "-s", "-w", "\nHTTP_STATUS:%{http_code}\n",
		"-H", fmt.Sprintf("Authorization: Bearer %s", token),
		endpointURL,
	)
	out, err := curlCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("curl command failed: %v, out: %s", err, string(out))
	}

	outStr := string(out)
	t.Logf("Curl output:\n%s", outStr)

	// Verify HTTP 200
	if !strings.Contains(outStr, "HTTP_STATUS:200") {
		t.Fatalf("expected HTTP 200, got output:\n%s", outStr)
	}

	// Parse JSON
	idx := strings.Index(outStr, "\nHTTP_STATUS:200")
	jsonPart := outStr[:idx]
	var res struct {
		Events []agents.TranscriptEvent `json:"events"`
		Next   string                   `json:"next"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &res); err != nil {
		t.Fatalf("failed to unmarshal JSON response: %v\nJSON: %s", err, jsonPart)
	}

	t.Logf("Validated JSON response! Events count: %d", len(res.Events))
	for i, ev := range res.Events {
		t.Logf("    [%d] ID=%s Kind=%s OccurredAt=%s TurnID=%s",
			i, ev.ID, ev.Kind, ev.OccurredAt.Format(time.RFC3339), ev.TurnID)
		if ev.Message != nil {
			t.Logf("        Message: Role=%s Content=%q", ev.Message.Role, ev.Message.Content)
		}
	}

	if len(res.Events) == 0 {
		t.Fatalf("expected at least one event in history, got 0")
	}

	// Check that we have completed user and assistant messages
	hasUserMsg := false
	hasAssistantMsg := false
	for _, ev := range res.Events {
		if ev.Kind == agents.TranscriptEventMessageCompleted && ev.Message != nil {
			if ev.Message.Role == "user" {
				hasUserMsg = true
			}
			if ev.Message.Role == "assistant" {
				hasAssistantMsg = true
			}
		}
	}

	if !hasUserMsg {
		t.Fatalf("expected completed user message in transcript")
	}
	if !hasAssistantMsg {
		t.Fatalf("expected completed assistant message in transcript")
	}

	t.Log("Smoke test verified! Transcript shape confirmed via curl.")
}
