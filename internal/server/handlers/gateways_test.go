package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/gateways"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

type stubBotVerifier struct {
	username string
	err      error
}

func (s *stubBotVerifier) VerifyBotToken(_ context.Context, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.username, nil
}

type stubWARuntime struct{}

func (s *stubWARuntime) CloudHealth(_ context.Context, _ string) (string, error) {
	return "ok", nil
}
func (s *stubWARuntime) ProbeCredentials(_ context.Context, _ string) (string, error) {
	return "WA Bot", nil
}
func (s *stubWARuntime) PairingState(_ context.Context, _ string) (handlers.WhatsAppPairingSnapshot, bool) {
	return handlers.WhatsAppPairingSnapshot{State: "connected", Connection: "connected"}, true
}
func (s *stubWARuntime) StartPairing(_ context.Context, _, _ string) error {
	return nil
}
func (s *stubWARuntime) RegeneratePairing(_ context.Context, _ string) error {
	return nil
}
func (s *stubWARuntime) LogoutDevice(_ context.Context, _ string) error {
	return nil
}

type stubWAAuth struct{}

func (s *stubWAAuth) VerifyWebhookSignature(_ context.Context, _ string, _ []byte, _ string) bool {
	return true
}
func (s *stubWAAuth) VerifyChallengeToken(_ context.Context, _, _ string) bool {
	return true
}

type testGatewayTokenDecryptor struct {
	encKey []byte
}

func (d testGatewayTokenDecryptor) DecryptGatewayToken(_ context.Context, workspaceID, envelope string) (string, error) {
	pt, err := secrets.Decrypt(d.encKey, []byte(workspaceID), envelope)
	return string(pt), err
}

type stubAdapter struct{}

func (stubAdapter) Start(ctx context.Context) error { return nil }
func (stubAdapter) Stop(ctx context.Context) error  { return nil }
func (stubAdapter) Capabilities() gateways.AdapterCapabilities {
	return gateways.AdapterCapabilities{CanEdit: true, CanButton: true}
}
func (stubAdapter) SendMessage(ctx context.Context, chatID, body, flavor string, opts gateways.SendOptions) (string, error) {
	return "msg-1", nil
}
func (stubAdapter) EditMessage(ctx context.Context, chatID, messageID, body, flavor string) error {
	return nil
}
func (stubAdapter) SendTyping(ctx context.Context, chatID string) error { return nil }
func (stubAdapter) SendApprovalCard(ctx context.Context, chatID string, interrupt agents.ApprovalPayload) (string, error) {
	return "msg-card", nil
}
func (stubAdapter) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	return []byte("file"), nil
}

func setupGatewayTestRouter(t *testing.T, st store.Store, encKey []byte, verifier handlers.GatewayBotVerifier) (*gin.Engine, *domain.Workspace, *domain.Agent) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	ws := &domain.Workspace{
		ID:   "ws-gw-test-1",
		Slug: "gw-ws",
		Name: "Gateway Test Workspace",
	}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}

	provider := &domain.ProviderConfig{
		ID:          "p-test-1",
		WorkspaceID: ws.ID,
		Type:        "openai",
		Name:        "OpenAI",
	}
	if err := st.Providers().Create(ctx, provider); err != nil {
		t.Fatalf("failed to create test provider: %v", err)
	}

	agent := &domain.Agent{
		ID:          "agent-gw-test-1",
		WorkspaceID: ws.ID,
		Name:        "Atlas",
		Slug:        "atlas",
		ProviderID:  provider.ID,
		Model:       "gpt-4",
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("failed to create test agent: %v", err)
	}

	pairing := gateways.NewPairingService(st.GatewayLinks())
	bridge := gateways.NewApprovalBridge(nil, nil, st.GatewayLinks())
	gwRouter := gateways.NewRouter(st.Gateways(), st.GatewayBindings(), st.GatewayLinks(), st.Agents(), st.Members(), st.Users(), pairing, nil)
	outboxSvc := gateways.NewOutbox(st.GatewayOutbox(), func(string) (gateways.MessageSender, bool) { return nil, false })
	svc := gateways.NewService(gwRouter, nil, bridge, st.Gateways(), outboxSvc)
	factory := func(spec gateways.AdapterSpec) (gateways.PlatformAdapter, error) {
		return stubAdapter{}, nil
	}
	manager := gateways.NewManager(st.Gateways(), testGatewayTokenDecryptor{encKey: encKey}, factory, svc)

	gh := handlers.NewGatewayHandlers(
		st.Gateways(),
		st.GatewayBindings(),
		st.GatewayLinks(),
		st.GatewayOutbox(),
		st.Agents(),
		st.Users(),
		pairing,
		manager,
		svc,
		verifier,
		encKey,
		&stubWARuntime{},
		&stubWAAuth{},
	)

	r := gin.New()
	wsGroup := r.Group("/api/v1/workspaces/:ws")
	wsGroup.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Next()
	})
	telegramGroup := wsGroup.Group("/gateways/telegram")
	{
		telegramGroup.GET("", gh.ListTelegramGateways)
		telegramGroup.POST("", gh.CreateTelegramGateway)
		telegramGroup.GET("/:gatewayId", gh.GetTelegramGateway)
		telegramGroup.PUT("/:gatewayId", gh.UpdateTelegramGateway)
		telegramGroup.POST("/:gatewayId/enable", gh.EnableTelegramGateway)
		telegramGroup.POST("/:gatewayId/disable", gh.DisableTelegramGateway)
		telegramGroup.POST("/:gatewayId/test", gh.TestTelegramGateway)
		telegramGroup.DELETE("/:gatewayId", gh.DeleteTelegramGateway)
	}

	return r, ws, agent
}

func TestGateway_MultiBot_TelegramCRUD(t *testing.T) {
	encKey := []byte("01234567890123456789012345678901")
	st := storefake.New()
	verifier := &stubBotVerifier{username: "test_bot"}
	r, ws, agent := setupGatewayTestRouter(t, st, encKey, verifier)

	// 1. Create a telegram gateway account
	reqBody, _ := json.Marshal(map[string]any{
		"token":     "123456789:ABCdefGHIjklMNOpqrSTUvwxYZ1234567890",
		"agent_id":  agent.ID,
		"transport": "long_polling",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+ws.Slug+"/gateways/telegram", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 on creation, got %d: %s", w.Code, w.Body.String())
	}
	var created handlers.ApiGatewayConfig
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode created response: %v", err)
	}
	if created.ID == "" || created.AgentID != agent.ID || created.TokenHint != "7890" || created.Transport != "long_polling" {
		t.Fatalf("unexpected created config: %+v", created)
	}

	// 2. List telegram gateways
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+ws.Slug+"/gateways/telegram", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on list, got %d: %s", w.Code, w.Body.String())
	}
	var list []handlers.ApiGatewayConfig
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("expected 1 gateway in list, got %+v", list)
	}

	// 3. Get single gateway
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+ws.Slug+"/gateways/telegram/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on get, got %d: %s", w.Code, w.Body.String())
	}
	var got handlers.ApiGatewayConfig
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode get response: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("unexpected got gateway: %+v", got)
	}

	// 4. Update transport to webhook without URL -> 422 naming webhook_url
	reqBody, _ = json.Marshal(map[string]any{
		"transport": "webhook",
	})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/"+ws.Slug+"/gateways/telegram/"+created.ID, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for webhook without URL, got %d: %s", w.Code, w.Body.String())
	}

	// 5. Update transport to webhook with https URL
	reqBody, _ = json.Marshal(map[string]any{
		"transport":   "webhook",
		"webhook_url": "https://example.com/api/v1/webhooks/telegram/" + created.ID,
	})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/"+ws.Slug+"/gateways/telegram/"+created.ID, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid update, got %d: %s", w.Code, w.Body.String())
	}
	var updated handlers.ApiGatewayConfig
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to decode updated response: %v", err)
	}
	if updated.Transport != "webhook" || updated.WebhookURL != "https://example.com/api/v1/webhooks/telegram/"+created.ID {
		t.Fatalf("unexpected updated config: %+v", updated)
	}

	// 6. Disable gateway
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+ws.Slug+"/gateways/telegram/"+created.ID+"/disable", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on disable, got %d: %s", w.Code, w.Body.String())
	}

	// 7. Enable gateway
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+ws.Slug+"/gateways/telegram/"+created.ID+"/enable", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on enable, got %d: %s", w.Code, w.Body.String())
	}

	// 8. Test gateway
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+ws.Slug+"/gateways/telegram/"+created.ID+"/test", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on test, got %d: %s", w.Code, w.Body.String())
	}
	var testResp struct {
		OK          bool   `json:"ok"`
		BotUsername string `json:"bot_username"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("failed to decode test response: %v", err)
	}
	if !testResp.OK || testResp.BotUsername != "test_bot" {
		t.Fatalf("unexpected test response: %+v", testResp)
	}

	// 9. Delete gateway
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/"+ws.Slug+"/gateways/telegram/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete, got %d: %s", w.Code, w.Body.String())
	}
}
