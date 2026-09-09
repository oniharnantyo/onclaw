package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/openresponses"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/services"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// v1StubModel answers every call with one assistant text message carrying
// provider usage.
type v1StubModel struct{}

func (v1StubModel) Generate(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	return v1StubMessage(), nil
}

func (v1StubModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(v1StubMessage(), nil)
	sw.Close()
	return sr, nil
}

func v1StubMessage() *schema.AgenticMessage {
	msg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "hello from atlas"}),
		},
	}
	msg.ResponseMeta = &schema.AgenticResponseMeta{
		TokenUsage: &schema.TokenUsage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
	}
	return msg
}

type v1StubComposer struct{}

func (v1StubComposer) Compose(_ context.Context, _ agents.ComposeParams) (string, error) {
	return "stub instruction", nil
}

// v1StallModel holds the model call open until its release channel closes —
// the silent stretch an SSE keepalive must bridge.
type v1StallModel struct{ release chan struct{} }

func (m v1StallModel) Generate(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	<-m.release
	return v1StubMessage(), nil
}

func (m v1StallModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	go func() {
		<-m.release
		_ = sw.Send(v1StubMessage(), nil)
		sw.Close()
	}()
	return sr, nil
}

func setupV1Env(t *testing.T) (store.Store, *gin.Engine, string) {
	t.Helper()
	return setupV1EnvOpts(t, nil, 0)
}

// setupV1EnvOpts builds the same environment with an optional stalling model
// (release non-nil) and a custom /v1 SSE keepalive cadence for the streaming
// tests. keepAlive 0 uses the handler default.
func setupV1EnvOpts(t *testing.T, release chan struct{}, keepAlive time.Duration) (store.Store, *gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	stor := storagefake.New()
	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "test-secret-key-that-is-at-least-32-chars-long!",
		TTL:    24 * time.Hour,
	})

	onClawDir := t.TempDir()
	runner := agents.NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(),
		[]byte("test-key-32-bytes-long-12345678"),
		onClawDir,
		agents.WithAgenticModelFactory(func(_ context.Context, _ string, _ providers.Credential, _ string) (model.BaseModel[*schema.AgenticMessage], error) {
			if release != nil {
				return v1StallModel{release: release}, nil
			}
			return v1StubModel{}, nil
		}),
		agents.WithInstructionComposer(v1StubComposer{}),
		// Mirror the composition root's wiring: tool resolution reads the
		// workspace's tool settings (web.search provider entries) through the
		// settings service, not the allow-all default gate.
		agents.WithToolPolicy(agents.NewToolSettingsService(st.ToolSettings(), []byte("test-key-32-bytes-long-12345678"))),
	)

	r := server.NewRouter(server.RouterOptions{
		Store:             st,
		Storage:           stor,
		Issuer:            issuer,
		EncryptionKey:     []byte("test-key-32-bytes-long-12345678"),
		Runner:            runner,
		V1StreamKeepAlive: keepAlive,
	})

	ctx := context.Background()
	ws := &domain.Workspace{Slug: "v1ws", Name: "V1 WS", Timezone: "UTC"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
		Temperature: 1.0,
		Autonomy:    domain.AutonomyApproval,
		Tools:       []string{"web.search"},
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// web.search fails construction when a workspace has no provider entries
	// (fail-fast, no credential-free default), which would fail every turn at
	// tool resolution. Seed one valid entries-shaped provider so the agent
	// composes; the stub model never invokes the tool.
	if err := st.ToolSettings().Upsert(ctx, &domain.WorkspaceToolSetting{
		WorkspaceID: ws.ID,
		ToolKey:     "web.search",
		Enabled:     true,
		Config: map[string]any{
			"entries": []any{
				map[string]any{"name": "Tavily fixture", "provider": "tavily", "api_key": "tvly-v1-fixture-key"},
			},
		},
	}); err != nil {
		t.Fatalf("seed web.search tool settings: %v", err)
	}

	// Seed the agent's on-disk workspace directory (the jail root).
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(onClawDir), ws.Slug, ag.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}

	user := &domain.User{Email: "v1@example.com", Name: "V1"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner"}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	plaintext, _, err := services.NewAPIKeyService(st.APIKeys()).Create(ctx, ws.ID, "test", user.ID)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	return st, r, plaintext
}

func v1Post(t *testing.T, router *gin.Engine, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestV1Models(t *testing.T) {
	_, router, key := setupV1Env(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var res struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Data) != 1 || res.Data[0].ID != "atlas" {
		t.Fatalf("models = %+v, want [atlas]", res.Data)
	}
}

func TestV1Responses_NonStreamingTurn(t *testing.T) {
	st, router, key := setupV1Env(t)

	rec := v1Post(t, router, key, map[string]any{
		"model": "atlas",
		"input": "hi there",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var res struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Model  string `json:"model"`
		Output []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Status  string `json:"status"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if res.Status != "completed" || res.Model != "atlas" {
		t.Fatalf("status/model = %s/%s, want completed/atlas", res.Status, res.Model)
	}
	if len(res.Output) == 0 || res.Output[0].Type != "message" {
		t.Fatalf("output = %+v, want a message item", res.Output)
	}
	if len(res.Output[0].Content) == 0 || res.Output[0].Content[0].Text != "hello from atlas" {
		t.Fatalf("content = %+v", res.Output[0].Content)
	}
	if res.Usage == nil || res.Usage.InputTokens != 11 || res.Usage.OutputTokens != 7 || res.Usage.TotalTokens != 18 {
		t.Fatalf("usage = %+v, want 11/7/18", res.Usage)
	}
	if !strings.HasPrefix(res.ID, "resp_") {
		t.Fatalf("response id = %q, want resp_ prefix", res.ID)
	}

	// Ephemeral turn: nothing persisted for the encoded session.
	session, _, err := openresponses.DecodeResponseID(res.ID)
	if err != nil {
		t.Fatalf("decode id: %v", err)
	}
	rows, err := st.SessionEvents().LoadEvents(context.Background(), store.LoadSessionEventsParams{
		WorkspaceID: mustV1WorkspaceID(t, st),
		SessionID:   session,
	})
	if err == nil && len(rows) > 0 {
		t.Fatalf("ephemeral session persisted %d events; want none", len(rows))
	}
}

// mustV1WorkspaceID resolves the seeded workspace ID of setupV1Env.
func mustV1WorkspaceID(t *testing.T, st store.Store) string {
	t.Helper()
	ws, err := st.Workspaces().BySlug(context.Background(), "v1ws")
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	return ws.ID
}

func TestV1Responses_RequestValidation(t *testing.T) {
	_, router, key := setupV1Env(t)

	// Missing model → 400 invalid_request_error naming the parameter.
	rec := v1Post(t, router, key, map[string]any{"input": "hi"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing model: status = %d", rec.Code)
	}

	// Unsupported content part → 400 naming the part type.
	rec = v1Post(t, router, key, map[string]any{
		"model": "atlas",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_image", "image_url": "x"}}},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported part: status = %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("input_image")) {
		t.Fatalf("error should name the part type: %s", rec.Body.String())
	}

	// Unknown model slug → 404 model_not_found.
	rec = v1Post(t, router, key, map[string]any{"model": "ghost", "input": "hi"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown model: status = %d", rec.Code)
	}

	// Malformed previous_response_id → 400.
	rec = v1Post(t, router, key, map[string]any{"model": "atlas", "input": "hi", "previous_response_id": "garbage"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed response id: status = %d", rec.Code)
	}
}

// TestV1Responses_SessionBirthOnFirstUse covers the metadata birth path: an
// unknown metadata.onclaw_session runs on the persistent adapter under the
// client-chosen ID — the birth turn's events persist to that session in the
// key's workspace (regression: adapter selection must key off the binding
// decision, not an ID-shape heuristic), and a second metadata-bound turn
// appends to the same history.
func TestV1Responses_SessionBirthOnFirstUse(t *testing.T) {
	st, router, key := setupV1Env(t)
	wsID := mustV1WorkspaceID(t, st)

	const sess = "sess_birth_first_use"
	rec := v1Post(t, router, key, map[string]any{
		"model":    "atlas",
		"input":    "hello",
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("birth turn: status = %d body = %s", rec.Code, rec.Body.String())
	}
	var birth struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &birth); err != nil {
		t.Fatalf("decode birth response: %v", err)
	}
	if birth.Status != "completed" {
		t.Fatalf("birth turn status = %s", birth.Status)
	}

	// The birth turn persisted under the client-chosen ID in this workspace.
	rows, err := st.SessionEvents().LoadEvents(context.Background(), store.LoadSessionEventsParams{
		WorkspaceID: wsID,
		SessionID:   sess,
	})
	if err != nil {
		t.Fatalf("load birth events: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("birth turn persisted no events under %q", sess)
	}
	firstCount := len(rows)

	// Second metadata-bound turn appends to the same session.
	rec = v1Post(t, router, key, map[string]any{
		"model":    "atlas",
		"input":    "again",
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("append turn: status = %d body = %s", rec.Code, rec.Body.String())
	}
	rows, err = st.SessionEvents().LoadEvents(context.Background(), store.LoadSessionEventsParams{
		WorkspaceID: wsID,
		SessionID:   sess,
	})
	if err != nil {
		t.Fatalf("load appended events: %v", err)
	}
	if len(rows) <= firstCount {
		t.Fatalf("second bind did not append: %d events after, %d before", len(rows), firstCount)
	}
}

// TestV1Responses_ChainingCannotBootstrap: previous_response_id is strictly
// bind-only — a well-formed ID resolving to a session with no persisted
// events fails not-found and creates no session.
func TestV1Responses_ChainingCannotBootstrap(t *testing.T) {
	st, router, key := setupV1Env(t)
	wsID := mustV1WorkspaceID(t, st)

	const sess = "sess_chain_ghost"
	rec := v1Post(t, router, key, map[string]any{
		"model":                "atlas",
		"input":                "hi",
		"previous_response_id": openresponses.MintResponseID(sess, "00000000-0000-0000-0000-000000000000"),
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("chained bind to unknown session: status = %d body = %s", rec.Code, rec.Body.String())
	}

	rows, err := st.SessionEvents().LoadEvents(context.Background(), store.LoadSessionEventsParams{
		WorkspaceID: wsID,
		SessionID:   sess,
	})
	if err == nil && len(rows) > 0 {
		t.Fatalf("chained bootstrap created session with %d events; want none", len(rows))
	}
}

// TestV1Responses_CrossWorkspaceIsolation: a session ID already in use by
// another workspace births an independent local session under the same name;
// the foreign workspace's events remain untouched and unreadable through the
// local key.
func TestV1Responses_CrossWorkspaceIsolation(t *testing.T) {
	st, router, key := setupV1Env(t)
	ctx := context.Background()
	localWS := mustV1WorkspaceID(t, st)

	// A second workspace with one persisted event under the colliding ID.
	foreign := &domain.Workspace{Slug: "v1foreign", Name: "Foreign WS", Timezone: "UTC"}
	if err := st.Workspaces().Create(ctx, foreign); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	const sess = "sess_shared_name"
	adapter := agents.NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), foreign.ID)
	if err := adapter.AppendEvents(ctx, sess, []*adk.SessionEvent[*schema.AgenticMessage]{
		{EventID: "foreign-1", TurnID: "foreign-turn", Timestamp: time.Now().UTC(), Message: schema.UserAgenticMessage("foreign secret")},
	}); err != nil {
		t.Fatalf("seed foreign session: %v", err)
	}

	// The local key births a local session under the same name.
	rec := v1Post(t, router, key, map[string]any{
		"model":    "atlas",
		"input":    "hi",
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("local birth turn: status = %d body = %s", rec.Code, rec.Body.String())
	}

	localRows, err := st.SessionEvents().LoadEvents(ctx, store.LoadSessionEventsParams{
		WorkspaceID: localWS,
		SessionID:   sess,
	})
	if err != nil {
		t.Fatalf("load local rows: %v", err)
	}
	if len(localRows) == 0 {
		t.Fatalf("local session was not born under %q", sess)
	}

	// The foreign workspace's history is untouched: exactly the seeded event.
	foreignRows, err := st.SessionEvents().LoadEvents(ctx, store.LoadSessionEventsParams{
		WorkspaceID: foreign.ID,
		SessionID:   sess,
	})
	if err != nil {
		t.Fatalf("load foreign rows: %v", err)
	}
	if len(foreignRows) != 1 || foreignRows[0].EventID != "foreign-1" {
		t.Fatalf("foreign session mutated: %+v", foreignRows)
	}

	// Chaining off the local birth stays in the local workspace and never
	// reads the foreign history.
	var first struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode birth response: %v", err)
	}
	rec = v1Post(t, router, key, map[string]any{
		"model":                "atlas",
		"input":                "chain",
		"previous_response_id": first.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("local chained turn: status = %d body = %s", rec.Code, rec.Body.String())
	}
	foreignRows, err = st.SessionEvents().LoadEvents(ctx, store.LoadSessionEventsParams{
		WorkspaceID: foreign.ID,
		SessionID:   sess,
	})
	if err != nil {
		t.Fatalf("reload foreign rows: %v", err)
	}
	if len(foreignRows) != 1 {
		t.Fatalf("chained turn leaked into foreign workspace: %d rows", len(foreignRows))
	}
}

func TestV1Responses_StreamingTurn(t *testing.T) {
	_, router, key := setupV1Env(t)

	b, _ := json.Marshal(map[string]any{"model": "atlas", "input": "hi", "stream": true})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}

	body := rec.Body.String()
	for _, want := range []string{
		`"type":"response.created"`,
		`"type":"response.in_progress"`,
		`"type":"response.output_item.added"`,
		`"type":"response.output_text.delta"`,
		`"delta":"hello from atlas"`,
		`"type":"response.output_text.done"`,
		`"type":"response.output_item.done"`,
		`"type":"response.completed"`,
		`"status":"completed"`,
		"data: [DONE]",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %q", want)
		}
	}

	// Sequence numbers strictly increasing.
	var seqs []int
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimPrefix(line, "data: ")
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if s, ok := ev["sequence_number"].(float64); ok {
			seqs = append(seqs, int(s))
		}
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Fatalf("sequence numbers not strictly increasing: %v", seqs)
		}
	}
}

// TestV1Responses_StreamingKeepAlive bridges long silent stretches (a slow
// model call or a long browser tool) by periodically re-emitting the in-progress
// response snapshot. Proxies don't reap the idle connection and sequence numbers
// remain ordered.
func TestV1Responses_StreamingKeepAlive(t *testing.T) {
	release := make(chan struct{})
	// Fast keepalive so the test stays quick.
	_, router, key := setupV1EnvOpts(t, release, 25*time.Millisecond)

	b, _ := json.Marshal(map[string]any{"model": "atlas", "input": "hi", "stream": true})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()

	// Release the stalling model after ~80ms (giving the 25ms keepalive time
	// to fire at least twice during the silent stretch).
	go func() {
		time.Sleep(80 * time.Millisecond)
		close(release)
	}()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if xab := rec.Header().Get("X-Accel-Buffering"); xab != "no" {
		t.Errorf("X-Accel-Buffering = %q, want 'no'", xab)
	}

	body := rec.Body.String()
	// Count how many times response.in_progress appeared: 1 initial + >= 2 keepalives.
	inProgressCount := strings.Count(body, `"type":"response.in_progress"`)
	if inProgressCount < 2 {
		t.Errorf("got %d response.in_progress frames, want >= 2 (keepalive did not fire)", inProgressCount)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("missing [DONE] terminal frame")
	}

	// Sequence numbers across all frames (including keepalives) must be strictly increasing.
	var seqs []int
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimPrefix(line, "data: ")
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if s, ok := ev["sequence_number"].(float64); ok {
			seqs = append(seqs, int(s))
		}
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Fatalf("sequence numbers not strictly increasing: %v", seqs)
		}
	}
}

func TestV1Responses_BoundAndChainedTurns(t *testing.T) {
	_, router, key := setupV1Env(t)

	// Metadata binding births the session on first use: the first bound turn
	// persists the session's history under the client-chosen ID.
	const sess = "sess-v1-bound"
	rec := v1Post(t, router, key, map[string]any{
		"model":    "atlas",
		"input":    "hi",
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("bound turn: status = %d body = %s", rec.Code, rec.Body.String())
	}
	var first struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if first.Status != "completed" {
		t.Fatalf("bound turn status = %s", first.Status)
	}

	// The minted response ID encodes the bound session: chaining via
	// previous_response_id resolves to the same session.
	decodedSession, _, err := openresponses.DecodeResponseID(first.ID)
	if err != nil {
		t.Fatalf("decode minted id: %v", err)
	}
	if decodedSession != sess {
		t.Fatalf("chained session = %q, want %q", decodedSession, sess)
	}

	rec = v1Post(t, router, key, map[string]any{
		"model":                "atlas",
		"input":                "again",
		"previous_response_id": first.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("chained turn: status = %d body = %s", rec.Code, rec.Body.String())
	}
}
