package handlers_test

// /v1 Responses endpoint attachment tests (add-chat-attachments tasks 4.2–4.3):
// input parts resolve to workspace-scoped attachment references before any
// run starts, inline data URLs demote to stored attachments, compact turns
// reject attachments, and string-only inputs behave exactly as before.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/storage"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// v1aCaptureAttachments records Create calls so tests can assert exactly what
// the demotion path stored (and that ordinary turns store nothing).
type v1aCaptureAttachments struct {
	store.AttachmentStore
	mu      sync.Mutex
	created []domain.Attachment
}

func (c *v1aCaptureAttachments) Create(ctx context.Context, a *domain.Attachment) error {
	if err := c.AttachmentStore.Create(ctx, a); err != nil {
		return err
	}
	c.mu.Lock()
	c.created = append(c.created, *a)
	c.mu.Unlock()
	return nil
}

func (c *v1aCaptureAttachments) createdCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.created)
}

var v1aEncKey = []byte("test-key-32-bytes-long-12345678")

// v1aStubModel answers every call with one assistant text message carrying
// provider usage (the runner-side multimodal turn construction is task 5.x —
// these tests pin the handler's resolution behavior, not the model wire).
type v1aStubModel struct{}

func (v1aStubModel) Generate(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	return v1aStubMessage(), nil
}

func (v1aStubModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(v1aStubMessage(), nil)
	sw.Close()
	return sr, nil
}

func v1aStubMessage() *schema.AgenticMessage {
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

type v1aStubComposer struct{}

func (v1aStubComposer) Compose(_ context.Context, _ agents.ComposeParams) (string, error) {
	return "stub instruction", nil
}

type v1aEnv struct {
	st     store.Store
	router *gin.Engine
	strg   storage.Storage
	atts   *v1aCaptureAttachments
	wsID   string
	userID string
}

// newV1aEnv wires the /v1/responses route exactly as the router does — the
// real runner over the fake store, the attachments store and the workspace
// storage resolver injected positionally — with a middleware that plays the
// API-key auth gate by setting the key context directly.
func newV1aEnv(t *testing.T) v1aEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	strg := storagefake.New()
	onClawDir := t.TempDir()
	dataDir := t.TempDir()

	wsStorage := resolver.New(strg, st.WorkspaceStorage(), st.Attachments(), v1aEncKey, dataDir)

	memLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	memWorker := memory.NewWorker(
		memory.NewGister(st.MemoryEvents(), st.SessionEvents(), st.MemoryEntities(), st.Providers(), v1aEncKey, agents.DefaultAgenticModelFactory, memLog),
		memory.NewGate(st.MemoryNotes(), st.MemoryEntities(), st.Memories(), st.Providers(), v1aEncKey, agents.DefaultAgenticModelFactory, memLog),
		memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), v1aEncKey, providers.NewRegistry()),
		st.MemoryEmbeddings(),
		memLog,
	)
	ingestWorker := ingest.NewWorker(memLog, ingest.WithConsumers(memWorker))
	memSearch := newTestMemorySearcher(st)
	memGate := memory.NewIntentGate(st.Providers(), v1aEncKey, agents.DefaultAgenticModelFactory, memLog)
	runner := agents.NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), ingestWorker, memSearch, memGate,
		v1aEncKey, onClawDir,
		agents.WithAgenticModelFactory(func(_ context.Context, _ string, _ providers.Credential, _ string) (model.BaseModel[*schema.AgenticMessage], error) {
			return v1aStubModel{}, nil
		}),
		agents.WithInstructionComposer(v1aStubComposer{}),
		agents.WithToolPolicy(agents.NewToolSettingsService(st.ToolSettings(), v1aEncKey)),
		// Mirror the router's wiring: the runner resolves attachment bytes
		// through the same workspace storage resolver the handler does.
		agents.WithAttachmentBlobs(wsStorage),
	)

	ws := &domain.Workspace{Slug: "v1a", Name: "V1A WS", Timezone: "UTC"}
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
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(onClawDir), ws.Slug, ag.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}

	user := &domain.User{Email: "v1a@example.com", Name: "V1A"}
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

	atts := &v1aCaptureAttachments{AttachmentStore: st.Attachments()}
	v1H := handlers.NewV1Handlers(runner, st.Agents(), st.SessionEvents(), atts, st.AgentSessions(), wsStorage, agents.NewToolSettingsService(st.ToolSettings(), nil), 0)

	r := gin.New()
	apiKey := &domain.WorkspaceAPIKey{WorkspaceID: ws.ID, CreatedBy: user.ID}
	r.Use(func(c *gin.Context) {
		c.Set(handlers.APIKeyContextKey, apiKey)
		c.Next()
	})
	r.POST("/v1/responses", v1H.CreateResponse)

	return v1aEnv{st: st, router: r, strg: strg, atts: atts, wsID: ws.ID, userID: user.ID}
}

func v1aPost(t *testing.T, env v1aEnv, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

// v1aEventCount counts the persisted events of a session in the env's
// workspace (0 when the session does not exist).
func v1aEventCount(t *testing.T, env v1aEnv, sess string) int {
	t.Helper()
	rows, err := env.st.SessionEvents().LoadEvents(context.Background(), store.LoadSessionEventsParams{
		WorkspaceID: env.wsID,
		SessionID:   sess,
	})
	if err != nil {
		return 0
	}
	return len(rows)
}

// v1aSeedTurn runs one ordinary metadata-bound turn so a session holds
// history (the compact tests compare event counts around the rejection).
func v1aSeedTurn(t *testing.T, env v1aEnv, sess, input string) {
	t.Helper()
	rec := v1aPost(t, env, map[string]any{
		"model":    "atlas",
		"input":    input,
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("seed turn: status = %d body = %s", rec.Code, rec.Body.String())
	}
}

var v1aPNG = []byte("\x89PNG\x0D\x0A\x1A\x0A" + strings.Repeat("fakepngdata", 40))

// v1aSeedAttachment inserts an attachment row (the upload endpoint's
// equivalent state) under the given capability key and stores its blob in the
// instance storage — the runner opens the bytes for inline lanes at run time.
func v1aSeedAttachment(t *testing.T, env v1aEnv, workspaceID, key, name, mime, lane string, content []byte) {
	t.Helper()
	att := &domain.Attachment{
		WorkspaceID: workspaceID,
		StorageKey:  key,
		Backend:     "local",
		Name:        name,
		MimeType:    mime,
		Size:        int64(len(content)),
		Lane:        lane,
		CreatedBy:   env.userID,
	}
	if err := env.st.Attachments().Create(context.Background(), att); err != nil {
		t.Fatalf("seed attachment: %v", err)
	}
	if err := env.strg.Put(context.Background(), key, bytes.NewReader(content), int64(len(content)), mime); err != nil {
		t.Fatalf("seed attachment blob: %v", err)
	}
}

func v1aImageContent(url string) []map[string]any {
	return []map[string]any{
		{"type": "input_text", "text": "what is in this shot?"},
		{"type": "input_image", "image_url": url},
	}
}

// TestV1Responses_CapabilityImageTurnExecutes (spec: turn with image and
// text): a capability-URL input_image resolves against the key's workspace
// and the turn executes exactly as a text-only one does.
func TestV1Responses_CapabilityImageTurnExecutes(t *testing.T) {
	env := newV1aEnv(t)

	const sess = "sess-v1a-cap-image"
	v1aSeedAttachment(t, env, env.wsID, "v1a-cap-key-1", "shot.png", "image/png", domain.AttachmentLaneInlineImage, v1aPNG)

	rec := v1aPost(t, env, map[string]any{
		"model":    "atlas",
		"input":    []map[string]any{{"type": "message", "role": "user", "content": v1aImageContent("/api/v1/files/v1a-cap-key-1")}},
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("status = %s, want completed", res.Status)
	}
	if v1aEventCount(t, env, sess) == 0 {
		t.Fatal("turn with a resolved attachment persisted no session events")
	}
	// Resolution is read-only on the referenced row.
	if env.atts.createdCount() != 0 {
		t.Errorf("capability resolution created %d rows; want 0", env.atts.createdCount())
	}
}

// TestV1Responses_UnknownCapabilityKeyRejected (spec: unknown attachment id
// in URL path): 400 invalid_param before any run starts.
func TestV1Responses_UnknownCapabilityKeyRejected(t *testing.T) {
	env := newV1aEnv(t)

	const sess = "sess-v1a-unknown-key"
	rec := v1aPost(t, env, map[string]any{
		"model":    "atlas",
		"input":    []map[string]any{{"type": "message", "role": "user", "content": v1aImageContent("/api/v1/files/no-such-key")}},
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_request_error") || !strings.Contains(rec.Body.String(), `"input"`) {
		t.Errorf("envelope = %s, want invalid_request_error naming input", rec.Body.String())
	}
	if v1aEventCount(t, env, sess) != 0 {
		t.Error("a failed resolution must not run the turn")
	}
	if env.atts.createdCount() != 0 {
		t.Errorf("failed resolution created %d rows; want 0", env.atts.createdCount())
	}
}

// TestV1Responses_ForeignCapabilityKeyRejected (tenancy): a capability key
// whose row belongs to another workspace is indistinguishable from unknown.
func TestV1Responses_ForeignCapabilityKeyRejected(t *testing.T) {
	env := newV1aEnv(t)
	ctx := context.Background()

	foreign := &domain.Workspace{Slug: "v1a-foreign", Name: "Foreign WS", Timezone: "UTC"}
	if err := env.st.Workspaces().Create(ctx, foreign); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	v1aSeedAttachment(t, env, foreign.ID, "v1a-foreign-key", "secret.png", "image/png", domain.AttachmentLaneInlineImage, v1aPNG)

	const sess = "sess-v1a-foreign-key"
	rec := v1aPost(t, env, map[string]any{
		"model":    "atlas",
		"input":    []map[string]any{{"type": "message", "role": "user", "content": v1aImageContent("/api/v1/files/v1a-foreign-key")}},
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_request_error") {
		t.Errorf("envelope = %s, want invalid_request_error", rec.Body.String())
	}
	if v1aEventCount(t, env, sess) != 0 {
		t.Error("a foreign attachment reference must not run the turn")
	}
}

// TestV1Responses_CompactRejectsAttachments (spec: compact with attachment
// rejected): the rejection happens before attachment resolution and before
// session binding — the session is untouched.
func TestV1Responses_CompactRejectsAttachments(t *testing.T) {
	env := newV1aEnv(t)

	const sess = "sess-v1a-compact-att"
	v1aSeedTurn(t, env, sess, "remember the deployment runbook")
	before := v1aEventCount(t, env, sess)
	if before == 0 {
		t.Fatal("seed turn persisted no events")
	}

	// A VALID capability key: the rejection must come from the compact gate,
	// not from resolution.
	v1aSeedAttachment(t, env, env.wsID, "v1a-compact-key", "shot.png", "image/png", domain.AttachmentLaneInlineImage, v1aPNG)
	rec := v1aPost(t, env, map[string]any{
		"model": "atlas",
		"input": []map[string]any{{"type": "message", "role": "user", "content": v1aImageContent("/api/v1/files/v1a-compact-key")}},
		"metadata": map[string]string{
			"onclaw_session": sess,
			"onclaw_command": "compact",
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "compaction accepts text only") {
		t.Errorf("envelope = %s, want the compaction text-only reason", rec.Body.String())
	}
	if got := v1aEventCount(t, env, sess); got != before {
		t.Errorf("session events = %d, want untouched %d", got, before)
	}
}

// TestV1Responses_DataURLDemotedToStorage (spec: inline data URL demoted to
// storage): the decoded bytes land in the workspace backend under a fresh
// capability key with an attachments row, and the turn proceeds.
func TestV1Responses_DataURLDemotedToStorage(t *testing.T) {
	env := newV1aEnv(t)

	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(v1aPNG)
	const sess = "sess-v1a-demote"
	rec := v1aPost(t, env, map[string]any{
		"model":    "atlas",
		"input":    []map[string]any{{"type": "message", "role": "user", "content": v1aImageContent(dataURL)}},
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	if env.atts.createdCount() != 1 {
		t.Fatalf("demotion created %d rows; want exactly 1", env.atts.createdCount())
	}
	row := env.atts.created[0]
	if row.WorkspaceID != env.wsID || row.Backend != "local" || row.Lane != domain.AttachmentLaneInlineImage {
		t.Errorf("row = ws %q backend %q lane %q", row.WorkspaceID, row.Backend, row.Lane)
	}
	if row.MimeType != "image/png" || row.Size != int64(len(v1aPNG)) || row.StorageKey == "" {
		t.Errorf("row = mime %q size %d key %q", row.MimeType, row.Size, row.StorageKey)
	}
	if row.Name != "attachment.png" {
		t.Errorf("default name = %q, want the mime-derived attachment.png", row.Name)
	}
	if row.CreatedBy != env.userID {
		t.Errorf("created_by = %q, want the API key creator", row.CreatedBy)
	}

	// The blob is stored under the recorded capability key and serves through
	// the same global lookup the wire token uses.
	f, err := env.strg.Open(context.Background(), row.StorageKey)
	if err != nil {
		t.Fatalf("open demoted blob: %v", err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read demoted blob: %v", err)
	}
	if !bytes.Equal(got, v1aPNG) {
		t.Errorf("stored blob = %d bytes, want the decoded %d", len(got), len(v1aPNG))
	}
	if _, err := env.st.Attachments().ByStorageKey(context.Background(), row.StorageKey); err != nil {
		t.Errorf("demoted row not resolvable by storage key: %v", err)
	}

	if v1aEventCount(t, env, sess) == 0 {
		t.Error("demoted turn persisted no session events")
	}
}

// TestV1Responses_DataURLOversizeRejected: an over-cap inline image rides the
// standard payload-too-large mapping before any run, storing nothing.
func TestV1Responses_DataURLOversizeRejected(t *testing.T) {
	env := newV1aEnv(t)

	huge := append(append([]byte{}, v1aPNG...), bytes.Repeat([]byte{0xAA}, 6<<20)...)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(huge)
	const sess = "sess-v1a-demote-huge"
	rec := v1aPost(t, env, map[string]any{
		"model":    "atlas",
		"input":    []map[string]any{{"type": "message", "role": "user", "content": v1aImageContent(dataURL)}},
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "5242880") {
		t.Errorf("rejection must name the image cap: %s", rec.Body.String())
	}
	if env.atts.createdCount() != 0 {
		t.Errorf("oversize demotion created %d rows; want 0", env.atts.createdCount())
	}
	if v1aEventCount(t, env, sess) != 0 {
		t.Error("oversize rejection must not run the turn")
	}
}

// TestV1Responses_StringInputStoresNoAttachments: string-only and text-only
// inputs run exactly as before — no attachment rows, turn completes.
func TestV1Responses_StringInputStoresNoAttachments(t *testing.T) {
	env := newV1aEnv(t)

	const sess = "sess-v1a-plain"
	rec := v1aPost(t, env, map[string]any{
		"model":    "atlas",
		"input":    "hi there",
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("string turn: status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = v1aPost(t, env, map[string]any{
		"model": "atlas",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": []map[string]any{
				{"type": "input_text", "text": "plain"},
				{"type": "input_text", "text": "text only"},
			}},
		},
		"metadata": map[string]string{"onclaw_session": sess},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("text-array turn: status = %d body = %s", rec.Code, rec.Body.String())
	}

	if env.atts.createdCount() != 0 {
		t.Errorf("text-only turns created %d attachment rows; want 0", env.atts.createdCount())
	}
}
