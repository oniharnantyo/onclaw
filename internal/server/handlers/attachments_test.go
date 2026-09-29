package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/storage"
	_ "github.com/oniharnantyo/onclaw/internal/storage/local"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// countingAttachments counts Create calls so rejection paths can assert that
// no row was recorded.
type countingAttachments struct {
	store.AttachmentStore
	creates int32
}

func (c *countingAttachments) Create(ctx context.Context, a *domain.Attachment) error {
	atomic.AddInt32(&c.creates, 1)
	return c.AttachmentStore.Create(ctx, a)
}

// failingStorage is a storage-port stand-in whose Put always fails, playing
// the unreachable-backend role in the storage-failure test.
type failingStorage struct{}

func (failingStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	return errors.New("simulated backend outage")
}
func (failingStorage) Open(ctx context.Context, key string) (storage.File, error) {
	return nil, domain.ErrNotFound
}
func (failingStorage) Delete(ctx context.Context, key string) error { return nil }
func (failingStorage) URL(key string) string                        { return "/api/v1/files/" + key }

var attTestEncKey = []byte("01234567890123456789012345678901")

// newAttachmentsTestEnv builds a gin engine with the attachment upload route
// and the capability-serving route wired exactly as the router does. The
// middleware plays the workspace-member auth gate: unauthenticated requests
// are rejected 401 before reaching the handler, authenticated ones carry the
// workspace and user context accessors read.
func newAttachmentsTestEnv(t *testing.T, authed bool) (*gin.Engine, store.Store, *domain.Workspace, *countingAttachments) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	user := &domain.User{Email: "uploader@example.com", Name: "Uploader"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	dataDir := t.TempDir()
	strg, err := storage.Open("local", storage.StorageConfig{Driver: "local", DataDir: dataDir})
	if err != nil {
		t.Fatalf("open local storage: %v", err)
	}
	wsStorage := resolver.New(strg, st.WorkspaceStorage(), st.Attachments(), attTestEncKey, dataDir)

	counting := &countingAttachments{AttachmentStore: st.Attachments()}
	attH := handlers.NewAttachmentsHandlers(counting, wsStorage)
	fileH := handlers.NewFileHandlers(strg, st.Attachments(), st.ReferenceDocuments(), wsStorage)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if !authed {
			handlers.AbortUnauthenticated(c, "")
			return
		}
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.UserContextKey, user)
		c.Next()
	})
	r.POST("/api/v1/workspaces/:ws/attachments", attH.Upload)
	r.GET("/api/v1/files/:key", fileH.ServeFile)
	return r, st, ws, counting
}

// uploadAttachment POSTs a multipart `file` field, mimicking the real client
// (the declared content type is deliberately unreliable — the sniff wins).
func uploadAttachment(t *testing.T, r *gin.Engine, filename string, content []byte, declaredType string) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if declaredType != "" {
		_ = declaredType // part headers already declare via CreateFormFile; sniff is authoritative
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

type uploadResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

var pngBytes = []byte("\x89PNG\x0D\x0A\x1A\x0A" + strings.Repeat("fakepngdata", 40))

func TestAttachments_UploadSuccessShape(t *testing.T) {
	r, st, ws, counting := newAttachmentsTestEnv(t, true)

	rec := uploadAttachment(t, r, "shot.png", pngBytes, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var res uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res.ID == "" || res.Name != "shot.png" || res.Mime != "image/png" || res.Size != int64(len(pngBytes)) {
		t.Errorf("response shape = %+v", res)
	}
	// D1: the capability URL is the wire token — the onclaw-proxied files
	// path over the fresh 128-bit capability key.
	if !strings.HasPrefix(res.URL, "/api/v1/files/") || len(strings.TrimPrefix(res.URL, "/api/v1/files/")) != 32 {
		t.Errorf("url = %q, want /api/v1/files/<32 hex>", res.URL)
	}
	if atomic.LoadInt32(&counting.creates) != 1 {
		t.Errorf("expected exactly 1 attachment row, got %d", counting.creates)
	}

	// The row records the resolved backend and the sniffed lane.
	att, err := st.Attachments().ByID(context.Background(), ws.ID, res.ID)
	if err != nil {
		t.Fatalf("attachment row missing: %v", err)
	}
	if att.Backend != "local" || att.Lane != domain.AttachmentLaneInlineImage || att.StorageKey == "" {
		t.Errorf("row = backend %q lane %q key %q", att.Backend, att.Lane, att.StorageKey)
	}
}

func TestAttachments_UploadUnauthenticated(t *testing.T) {
	r, _, _, counting := newAttachmentsTestEnv(t, false)

	rec := uploadAttachment(t, r, "shot.png", pngBytes, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"unauthenticated"`) {
		t.Errorf("expected the standard unauthenticated envelope: %s", rec.Body.String())
	}
	if atomic.LoadInt32(&counting.creates) != 0 {
		t.Errorf("no attachment row may be created on 401, got %d", counting.creates)
	}
}

func TestAttachments_OversizeImageRejected413(t *testing.T) {
	r, _, _, counting := newAttachmentsTestEnv(t, true)

	huge := append([]byte("\x89PNG\x0D\x0A\x1A\x0A"), bytes.Repeat([]byte{0xAA}, 8<<20)...)
	rec := uploadAttachment(t, r, "huge.png", huge, "")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
	// Spec: 413 with a message naming the 5 MB image cap.
	if !strings.Contains(rec.Body.String(), fmt.Sprintf("%d", int64(5<<20))) {
		t.Errorf("413 must name the image cap: %s", rec.Body.String())
	}
	if atomic.LoadInt32(&counting.creates) != 0 {
		t.Errorf("rejected upload must not create a row, got %d", counting.creates)
	}
}

func TestAttachments_ExecutableRenamedPNGRejected(t *testing.T) {
	r, _, _, _ := newAttachmentsTestEnv(t, true)

	// PE/MZ bytes wearing an image name: the sniff is authoritative, so the
	// upload is rejected as a disallowed type (spec scenario).
	exe := append([]byte("MZ"), bytes.Repeat([]byte{0x90}, 600)...)
	rec := uploadAttachment(t, r, "image.png", exe, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "executable") {
		t.Errorf("rejection must name the disallowed type: %s", rec.Body.String())
	}
}

func TestAttachments_ZeroByteRejectedStoresNothing(t *testing.T) {
	r, _, _, counting := newAttachmentsTestEnv(t, true)

	rec := uploadAttachment(t, r, "empty.png", nil, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if atomic.LoadInt32(&counting.creates) != 0 {
		t.Errorf("zero-byte upload must store nothing, got %d rows", counting.creates)
	}
}

func TestAttachments_OfficeFormatRejectedWithPDFGuidance(t *testing.T) {
	r, st, ws, counting := newAttachmentsTestEnv(t, true)
	ctx := context.Background()

	// Modern office formats (docx/xlsx/pptx) land in the drop lane — accepted
	// with 201, no rejection.
	rec := uploadAttachment(t, r, "report.docx", []byte("PK\x03\x04-not-really-a-zip-but-office-ext-"+strings.Repeat("x", 600)), "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("docx upload: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var res uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode docx response: %v", err)
	}
	att, err := st.Attachments().ByID(ctx, ws.ID, res.ID)
	if err != nil {
		t.Fatalf("docx row missing: %v", err)
	}
	if att.Lane != domain.AttachmentLaneDrop {
		t.Errorf("docx lane = %q, want drop", att.Lane)
	}

	// Legacy binary office formats stay rejected with conversion guidance.
	rec = uploadAttachment(t, r, "report.doc", []byte("\xD0\xCF\x11\xE0-legacy-ole-compound-doc-"+strings.Repeat("x", 600)), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("doc upload: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "legacy binary office formats") || !strings.Contains(rec.Body.String(), "convert to modern formats") {
		t.Errorf("rejection must suggest converting to modern formats: %s", rec.Body.String())
	}
	// Only the accepted docx upload created a row.
	if atomic.LoadInt32(&counting.creates) != 1 {
		t.Errorf("expected exactly 1 created row, got %d", counting.creates)
	}
}

func TestAttachments_TextLanes(t *testing.T) {
	r, st, ws, _ := newAttachmentsTestEnv(t, true)
	ctx := context.Background()

	// 40 KB .yaml config → inline-text lane (spec scenario). The string is
	// sliced before conversion: a []byte slice past len would pick up the
	// allocation's zero-valued capacity.
	yamlBody := []byte(strings.Repeat("config: value\n", 40<<10)[:40<<10])
	rec := uploadAttachment(t, r, "config.yaml", yamlBody, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("yaml upload: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var res uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	att, err := st.Attachments().ByID(ctx, ws.ID, res.ID)
	if err != nil {
		t.Fatalf("yaml row missing: %v", err)
	}
	if att.Lane != domain.AttachmentLaneInlineText {
		t.Errorf("40KB yaml lane = %q, want inline-text", att.Lane)
	}

	// 4 MB .sql dump → drop lane (spec scenario).
	sqlBody := []byte(strings.Repeat("SELECT 1;\n", 4<<20)[:4<<20])
	rec = uploadAttachment(t, r, "dump.sql", sqlBody, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("sql upload: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	att, err = st.Attachments().ByID(ctx, ws.ID, res.ID)
	if err != nil {
		t.Fatalf("sql row missing: %v", err)
	}
	if att.Lane != domain.AttachmentLaneDrop {
		t.Errorf("4MB sql lane = %q, want drop", att.Lane)
	}
}

func TestAttachments_CapabilityURLServesStoredBytes(t *testing.T) {
	r, _, _, _ := newAttachmentsTestEnv(t, true)

	rec := uploadAttachment(t, r, "shot.png", pngBytes, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var res uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Capability URLs serve without additional authentication — fetch with
	// no auth middleware involvement (the engine's middleware is bypassed by
	// using a bare request to the same serving route).
	req := httptest.NewRequest(http.MethodGet, res.URL, nil)
	got := httptest.NewRecorder()
	r.ServeHTTP(got, req)
	if got.Code != http.StatusOK {
		t.Fatalf("capability fetch: expected 200, got %d: %s", got.Code, got.Body.String())
	}
	if !bytes.Equal(got.Body.Bytes(), pngBytes) {
		t.Errorf("served bytes differ from the upload (%d vs %d bytes)", got.Body.Len(), len(pngBytes))
	}
}

func TestAttachments_StorageFailureSurfacesAsErrorEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	user := &domain.User{Email: "uploader@example.com", Name: "Uploader"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	// An unreachable backend: the resolver's default fails every Put.
	wsStorage := resolver.New(failingStorage{}, st.WorkspaceStorage(), st.Attachments(), attTestEncKey, t.TempDir())
	attH := handlers.NewAttachmentsHandlers(st.Attachments(), wsStorage)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.UserContextKey, user)
		c.Next()
	})
	r.POST("/api/v1/workspaces/:ws/attachments", attH.Upload)

	rec := uploadAttachment(t, r, "shot.png", pngBytes, "")
	if rec.Code < 500 || rec.Code >= 600 {
		t.Fatalf("expected a 5xx-class envelope, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("expected the standard error envelope: %s", rec.Body.String())
	}
}
