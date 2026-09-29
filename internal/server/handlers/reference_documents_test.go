package handlers_test

// Reference-document HTTP surface tests (add-reference-documents 6.1–6.4):
// the pinned upload/list/patch/attach/promote/delete contract over the fake
// store, the classification error envelopes mirroring the attachment upload,
// capability serving byte-identical to the upload, tenancy, and the
// document-mention chip's path through the /v1 turn payload into
// ExecRequest.Attachments.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/openresponses"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/references"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/storage"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"

	_ "github.com/oniharnantyo/onclaw/internal/storage/local"
)

// ---------------------------------------------------------------------------
// Shared environment: real workspace + permission middleware over the fake
// store, the documents routes registered exactly as the router does.
// ---------------------------------------------------------------------------

type refDocEnv struct {
	st     store.Store
	strg   storage.Storage
	router *gin.Engine
	ws     *domain.Workspace
	owner  *domain.User
	member *domain.User
}

func newRefDocEnv(t *testing.T) refDocEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	dataDir := t.TempDir()
	strg, err := storage.Open("local", storage.StorageConfig{Driver: "local", DataDir: dataDir})
	if err != nil {
		t.Fatalf("open local storage: %v", err)
	}
	encKey := []byte("01234567890123456789012345678901")
	wsStorage := resolver.New(strg, st.WorkspaceStorage(), st.Attachments(), encKey, dataDir)

	docH := handlers.NewReferenceDocumentsHandlers(references.NewService(st, wsStorage), st.ReferenceDocuments())
	fileH := handlers.NewFileHandlers(strg, st.Attachments(), st.ReferenceDocuments(), wsStorage)

	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	owner := &domain.User{Email: "owner@example.com", Name: "Owner"}
	member := &domain.User{Email: "member@example.com", Name: "Member"}
	for _, u := range []*domain.User{owner, member} {
		if err := st.Users().Create(ctx, u); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleOwner, IsOwner: true, Permissions: domain.OwnerPermissions, BuiltIn: true}
	memberRole := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleMember, Permissions: domain.MemberPermissions, BuiltIn: true}
	for _, role := range []*domain.Role{ownerRole, memberRole} {
		if err := st.Roles().Create(ctx, role); err != nil {
			t.Fatalf("seed role: %v", err)
		}
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("seed owner membership: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: member.ID, RoleID: memberRole.ID}); err != nil {
		t.Fatalf("seed member membership: %v", err)
	}

	// FK targets for the attach lists.
	agentA := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas"}
	agentB := &domain.Agent{WorkspaceID: ws.ID, Slug: "beacon", Name: "Beacon"}
	for _, a := range []*domain.Agent{agentA, agentB} {
		if err := st.Agents().Create(ctx, a); err != nil {
			t.Fatalf("seed agent: %v", err)
		}
	}
	channel := &domain.Channel{WorkspaceID: ws.ID, Slug: "incidents", Name: "#incidents"}
	if err := st.Channels().CreateChannel(ctx, channel); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	// The real middleware chain, scoped exactly like the router: an auth
	// stub plays AuthRequired (tests pick the caller per request via a
	// header), the real workspace membership gate wraps the tenant-scoped
	// group, and the real permission gate sits ONLY on promote/demote. The
	// capability file route carries no workspace middleware.
	mw := server.NewMiddlewares(st.Users(), st.Workspaces(), st.Members(), st.Roles(), nil)
	r := gin.New()

	wsGroup := r.Group("/api/v1/workspaces/:ws")
	wsGroup.Use(func(c *gin.Context) {
		if u := c.GetHeader("X-Test-User"); u != "" {
			user := owner
			if u == "member" {
				user = member
			}
			c.Set(handlers.UserContextKey, user)
		}
		c.Next()
	})
	wsGroup.Use(mw.RequireWorkspace("ws"))
	{
		wsGroup.GET("/documents", docH.List)
		wsGroup.POST("/documents", docH.Upload)
		wsGroup.PATCH("/documents/:id", docH.Patch)
		wsGroup.DELETE("/documents/:id", docH.Delete)
		wsGroup.PUT("/documents/:id/agents", docH.PutAgents)
		wsGroup.PUT("/documents/:id/channels", docH.PutChannels)
		wsGroup.PUT("/documents/:id/content", docH.PutContent)
		wsGroup.POST("/documents/:id/promote", mw.RequirePermission(domain.PermissionReferenceDocumentsPromote), docH.Promote)
		wsGroup.POST("/documents/:id/demote", mw.RequirePermission(domain.PermissionReferenceDocumentsPromote), docH.Demote)
	}
	r.GET("/api/v1/files/:key", fileH.ServeFile)

	return refDocEnv{st: st, strg: strg, router: r, ws: ws, owner: owner, member: member}
}

// ---------------------------------------------------------------------------
// Request helpers.
// ---------------------------------------------------------------------------

func refDocUpload(t *testing.T, r *gin.Engine, user, method, path string, fields map[string][]string, filename string, content []byte) *httptest.ResponseRecorder {
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
	for _, key := range []string{"name", "description", "agentIds", "channelIds"} {
		for _, v := range fields[key] {
			if err := mw.WriteField(key, v); err != nil {
				t.Fatalf("write field %s: %v", key, err)
			}
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func refDocJSON(t *testing.T, r *gin.Engine, user, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// documentResponse is the pinned 201/200 document shape.
type documentResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Mime        string    `json:"mime"`
	Size        int64     `json:"size"`
	URL         string    `json:"url"`
	IndexStatus string    `json:"indexStatus"`
	Scope       string    `json:"scope"`
	PageCount   int       `json:"pageCount"`
	Agents      []string  `json:"agents"`
	Channels    []string  `json:"channels"`
	CreatedAt   timeValue `json:"createdAt"`
}

// timeValue decodes the createdAt timestamp, recording whether a non-zero
// value arrived at all (the pinned shape carries one).
type timeValue struct {
	Valid bool
	Raw   string
}

func (t *timeValue) UnmarshalJSON(b []byte) error {
	t.Raw = string(b)
	t.Valid = len(b) > 0 && string(b) != "null" && string(b) != `""` && string(b) != "0"
	return nil
}

func decodeDocument(t *testing.T, rec *httptest.ResponseRecorder) documentResponse {
	t.Helper()
	var res documentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode document response: %v (%s)", err, rec.Body.String())
	}
	return res
}

const refDocMD = "# Webhooks\n\nSigning requires the shared secret.\n\n# Rate limits\n\nSandbox allows 10 req/s.\n"

// uploadDoc seeds one document through the API and returns the response view.
func uploadDoc(t *testing.T, env refDocEnv, name string, agentIDs ...string) documentResponse {
	t.Helper()
	fields := map[string][]string{"name": {name}, "description": {"the " + name + " manual"}}
	for _, id := range agentIDs {
		fields["agentIds"] = append(fields["agentIds"], id)
	}
	rec := refDocUpload(t, env.router, "owner", http.MethodPost, "/api/v1/workspaces/acme/documents", fields, name, []byte(refDocMD))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload %s: expected 201, got %d: %s", name, rec.Code, rec.Body.String())
	}
	return decodeDocument(t, rec)
}

// ---------------------------------------------------------------------------
// Upload: happy shape, rejections, oversize.
// ---------------------------------------------------------------------------

func TestReferenceDocuments_UploadHappyShape(t *testing.T) {
	env := newRefDocEnv(t)
	ctx := context.Background()

	agentsRows, err := env.st.Agents().ListForWorkspace(ctx, env.ws.ID)
	if err != nil || len(agentsRows) == 0 {
		t.Fatalf("seeded agents missing: %v", err)
	}

	rec := refDocUpload(t, env.router, "owner", http.MethodPost, "/api/v1/workspaces/acme/documents", map[string][]string{
		"name":        {"Twilio API"},
		"description": {"the Twilio manual"},
		"agentIds":    {agentsRows[0].ID},
	}, "twilio-api.md", []byte(refDocMD))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	res := decodeDocument(t, rec)

	// Every pinned field (add-reference-documents 6.1).
	if res.ID == "" {
		t.Error("id must be set")
	}
	if res.Name != "Twilio API" {
		t.Errorf("name = %q, want the submitted display name", res.Name)
	}
	if res.Description != "the Twilio manual" {
		t.Errorf("description = %q", res.Description)
	}
	if res.Mime != "text/markdown" {
		t.Errorf("mime = %q, want the sniffed type", res.Mime)
	}
	if res.Size != int64(len(refDocMD)) {
		t.Errorf("size = %d, want the byte count %d", res.Size, len(refDocMD))
	}
	if !strings.HasPrefix(res.URL, "/api/v1/files/") || len(strings.TrimPrefix(res.URL, "/api/v1/files/")) != 32 {
		t.Errorf("url = %q, want /api/v1/files/<32 hex>", res.URL)
	}
	if res.IndexStatus != domain.RefDocIndexReady {
		t.Errorf("indexStatus = %q, want ready", res.IndexStatus)
	}
	if res.Scope != domain.RefDocScopeAttached {
		t.Errorf("scope = %q, want the attached default", res.Scope)
	}
	if len(res.Agents) != 1 || res.Agents[0] != agentsRows[0].ID {
		t.Errorf("agents = %v, want [%s]", res.Agents, agentsRows[0].ID)
	}
	if res.Channels == nil || len(res.Channels) != 0 {
		t.Errorf("channels = %v, want an empty (non-null) array", res.Channels)
	}
	if !res.CreatedAt.Valid {
		t.Errorf("createdAt = %s, want a timestamp", res.CreatedAt.Raw)
	}

	// The row is workspace-scoped and queryable.
	if _, err := env.st.ReferenceDocuments().Get(ctx, env.ws.ID, res.ID); err != nil {
		t.Fatalf("row missing: %v", err)
	}
}

func TestReferenceDocuments_UploadExecutableRenamedPDFRejected(t *testing.T) {
	env := newRefDocEnv(t)

	// PE/MZ bytes wearing a .pdf name: magic-byte sniffing is authoritative
	// (spec scenario: claimed type overridden by sniffing).
	exe := append([]byte("MZ"), bytes.Repeat([]byte{0x90}, 600)...)
	rec := refDocUpload(t, env.router, "owner", http.MethodPost, "/api/v1/workspaces/acme/documents", nil, "manual.pdf", exe)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"invalid_request"`) {
		t.Errorf("rejection must ride the standard invalid envelope: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "allowed: pdf, docx, pptx, xlsx, md, txt, html, csv") {
		t.Errorf("rejection must carry the guidance list: %s", rec.Body.String())
	}
}

func TestReferenceDocuments_UploadLegacyDocGuidance(t *testing.T) {
	env := newRefDocEnv(t)

	legacy := append([]byte("\xD0\xCF\x11\xE0-ole-compound-"), bytes.Repeat([]byte{0x00}, 600)...)
	rec := refDocUpload(t, env.router, "owner", http.MethodPost, "/api/v1/workspaces/acme/documents", nil, "manual.doc", legacy)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	// Spec: rejected with a message suggesting conversion to a modern format
	// or PDF.
	if !strings.Contains(rec.Body.String(), "legacy .doc files are not supported") || !strings.Contains(rec.Body.String(), "convert to .docx or PDF") {
		t.Errorf("rejection must carry the conversion guidance: %s", rec.Body.String())
	}
}

// referencesMaxPDFBytes mirrors the PDF upload cap (references.
// MaxReferencePDFBytes) for the oversize test.
const referencesMaxPDFBytes = 20 << 20

func TestReferenceDocuments_UploadOversize413(t *testing.T) {
	env := newRefDocEnv(t)

	// A PDF past the 20 MB cap: the typed oversize rejection maps to 413
	// payload_too_large naming the cap (the attachment contract's shape).
	pdf := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte{0x41}, referencesMaxPDFBytes+1)...)
	rec := refDocUpload(t, env.router, "owner", http.MethodPost, "/api/v1/workspaces/acme/documents", nil, "huge.pdf", pdf)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"payload_too_large"`) {
		t.Errorf("413 must ride the payload_too_large envelope: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), fmt.Sprintf("%d", referencesMaxPDFBytes)) {
		t.Errorf("413 must name the pdf cap: %s", rec.Body.String())
	}
}

func TestReferenceDocuments_UploadMissingFileField(t *testing.T) {
	env := newRefDocEnv(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", "no file here")
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/documents", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Test-User", "owner")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "file") {
		t.Errorf("rejection must name the missing field: %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// List + lenses.
// ---------------------------------------------------------------------------

func TestReferenceDocuments_ListAndLenses(t *testing.T) {
	env := newRefDocEnv(t)
	ctx := context.Background()

	agentsRows, err := env.st.Agents().ListForWorkspace(ctx, env.ws.ID)
	if err != nil || len(agentsRows) < 2 {
		t.Fatalf("seeded agents missing: %v", err)
	}
	channels, err := env.st.Channels().ListChannels(ctx, env.ws.ID)
	if err != nil || len(channels) == 0 {
		t.Fatalf("seeded channels missing: %v", err)
	}

	docA := uploadDoc(t, env, "agent-doc.md", agentsRows[0].ID)
	docC := uploadDoc(t, env, "channel-doc.md")
	if err := env.st.ReferenceDocuments().SetChannels(ctx, env.ws.ID, docC.ID, []string{channels[0].ID}); err != nil {
		t.Fatalf("attach channel: %v", err)
	}
	docP := uploadDoc(t, env, "promoted-doc.md")
	if err := env.st.ReferenceDocuments().SetScope(ctx, env.ws.ID, docP.ID, domain.RefDocScopeWorkspace); err != nil {
		t.Fatalf("promote: %v", err)
	}

	var list struct {
		Documents []documentResponse `json:"documents"`
	}

	rec := refDocJSON(t, env.router, "owner", http.MethodGet, "/api/v1/workspaces/acme/documents", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Documents) != 3 {
		t.Fatalf("documents = %d, want 3", len(list.Documents))
	}

	rec = refDocJSON(t, env.router, "owner", http.MethodGet, "/api/v1/workspaces/acme/documents?agent="+agentsRows[0].ID, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode agent lens: %v", err)
	}
	// The lens is attached + promoted: the promoted document the agent is
	// NOT attached to must appear alongside the agent-attached one, while
	// the channel-attached document stays out.
	if len(list.Documents) != 2 {
		t.Fatalf("agent lens = %d documents, want the attached doc plus the promoted doc", len(list.Documents))
	}
	agentIDs := map[string]bool{}
	for _, d := range list.Documents {
		agentIDs[d.ID] = true
	}
	if !agentIDs[docA.ID] || !agentIDs[docP.ID] || agentIDs[docC.ID] {
		t.Fatalf("agent lens = %+v, want %s + promoted %s, not the channel doc %s", list.Documents, docA.ID, docP.ID, docC.ID)
	}

	rec = refDocJSON(t, env.router, "owner", http.MethodGet, "/api/v1/workspaces/acme/documents?channel="+channels[0].ID, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode channel lens: %v", err)
	}
	if len(list.Documents) != 2 {
		t.Fatalf("channel lens = %d documents, want the channel-attached doc plus the promoted doc", len(list.Documents))
	}
	channelIDs := map[string]bool{}
	for _, d := range list.Documents {
		channelIDs[d.ID] = true
	}
	if !channelIDs[docC.ID] || !channelIDs[docP.ID] || channelIDs[docA.ID] {
		t.Fatalf("channel lens = %+v, want %s + promoted %s, not the agent doc %s", list.Documents, docC.ID, docP.ID, docA.ID)
	}
}

// ---------------------------------------------------------------------------
// Patch, attach, promote/demote, delete, replace, tenancy.
// ---------------------------------------------------------------------------

func TestReferenceDocuments_Patch(t *testing.T) {
	env := newRefDocEnv(t)
	doc := uploadDoc(t, env, "original.md")

	rec := refDocJSON(t, env.router, "owner", http.MethodPatch, "/api/v1/workspaces/acme/documents/"+doc.ID,
		map[string]any{"name": "Renamed", "description": "new description"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	res := decodeDocument(t, rec)
	if res.Name != "Renamed" || res.Description != "new description" {
		t.Errorf("patched view = %+v", res)
	}

	// A blank name is invalid; the stored row is untouched.
	rec = refDocJSON(t, env.router, "owner", http.MethodPatch, "/api/v1/workspaces/acme/documents/"+doc.ID,
		map[string]any{"name": "   "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank name: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	row, err := env.st.ReferenceDocuments().Get(context.Background(), env.ws.ID, doc.ID)
	if err != nil || row.Name != "Renamed" {
		t.Fatalf("blank-name patch must not apply: %v (%+v)", err, row)
	}
}

func TestReferenceDocuments_AttachSetComplete(t *testing.T) {
	env := newRefDocEnv(t)
	ctx := context.Background()

	agentsRows, err := env.st.Agents().ListForWorkspace(ctx, env.ws.ID)
	if err != nil || len(agentsRows) < 2 {
		t.Fatalf("seeded agents missing: %v", err)
	}
	channels, err := env.st.Channels().ListChannels(ctx, env.ws.ID)
	if err != nil || len(channels) == 0 {
		t.Fatalf("seeded channels missing: %v", err)
	}

	doc := uploadDoc(t, env, "attach-doc.md")

	// Agents: set-complete — the stored set becomes exactly the submission.
	rec := refDocJSON(t, env.router, "owner", http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/agents",
		map[string]any{"agentIds": []string{agentsRows[0].ID, agentsRows[1].ID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("attach agents: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	res := decodeDocument(t, rec)
	if len(res.Agents) != 2 {
		t.Fatalf("agents = %v, want both", res.Agents)
	}
	rec = refDocJSON(t, env.router, "owner", http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/agents",
		map[string]any{"agentIds": []string{agentsRows[1].ID}})
	res = decodeDocument(t, rec)
	if len(res.Agents) != 1 || res.Agents[0] != agentsRows[1].ID {
		t.Errorf("set-complete agents = %v, want exactly [%s]", res.Agents, agentsRows[1].ID)
	}

	// Channels: the same set-complete contract.
	rec = refDocJSON(t, env.router, "owner", http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/channels",
		map[string]any{"channelIds": []string{channels[0].ID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("attach channels: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	res = decodeDocument(t, rec)
	if len(res.Channels) != 1 || res.Channels[0] != channels[0].ID {
		t.Errorf("channels = %v, want [%s]", res.Channels, channels[0].ID)
	}

	// Unknown and foreign targets are indistinguishable not-found (tenancy).
	rec = refDocJSON(t, env.router, "owner", http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/agents",
		map[string]any{"agentIds": []string{"no-such-agent"}})
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Errorf("unknown agent attach = %d %s, want 404 not_found", rec.Code, rec.Body.String())
	}
}

func TestReferenceDocuments_PromoteDemoteAndMemberForbidden(t *testing.T) {
	env := newRefDocEnv(t)
	doc := uploadDoc(t, env, "promote-doc.md")

	// Owner (holds reference_documents.promote) promotes; demote returns the
	// attached tier.
	rec := refDocJSON(t, env.router, "owner", http.MethodPost, "/api/v1/workspaces/acme/documents/"+doc.ID+"/promote", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("promote: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if res := decodeDocument(t, rec); res.Scope != domain.RefDocScopeWorkspace {
		t.Errorf("promoted scope = %q, want workspace", res.Scope)
	}

	rec = refDocJSON(t, env.router, "owner", http.MethodPost, "/api/v1/workspaces/acme/documents/"+doc.ID+"/demote", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("demote: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if res := decodeDocument(t, rec); res.Scope != domain.RefDocScopeAttached {
		t.Errorf("demoted scope = %q, want attached", res.Scope)
	}

	// A member holds no reference_documents.promote: the house 403 forbidden
	// envelope, and the tier is untouched.
	rec = refDocJSON(t, env.router, "member", http.MethodPost, "/api/v1/workspaces/acme/documents/"+doc.ID+"/promote", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member promote: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"forbidden"`) {
		t.Errorf("member promote must ride the forbidden envelope: %s", rec.Body.String())
	}
	row, err := env.st.ReferenceDocuments().Get(context.Background(), env.ws.ID, doc.ID)
	if err != nil || row.Scope != domain.RefDocScopeAttached {
		t.Fatalf("member promote must not flip the tier: %v (%+v)", err, row)
	}
}

func TestReferenceDocuments_Delete(t *testing.T) {
	env := newRefDocEnv(t)
	doc := uploadDoc(t, env, "doomed.md")

	rec := refDocJSON(t, env.router, "owner", http.MethodDelete, "/api/v1/workspaces/acme/documents/"+doc.ID, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// The row and its blob die together: the capability URL stops resolving
	// (blob deleted, not orphaned).
	serveReq := httptest.NewRequest(http.MethodGet, doc.URL, nil)
	serveRec := httptest.NewRecorder()
	env.router.ServeHTTP(serveRec, serveReq)
	if serveRec.Code != http.StatusNotFound {
		t.Errorf("capability URL after delete = %d, want 404", serveRec.Code)
	}
}

func TestReferenceDocuments_ReplaceContent(t *testing.T) {
	env := newRefDocEnv(t)
	doc := uploadDoc(t, env, "replace-me.md")

	newBody := "# Replaced\n\nEntirely new content for the index.\n"
	rec := refDocUpload(t, env.router, "owner", http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/content", nil, "replace-me.md", []byte(newBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	res := decodeDocument(t, rec)
	if res.Size != int64(len(newBody)) {
		t.Errorf("replaced size = %d, want %d", res.Size, len(newBody))
	}
	if res.Name != "replace-me.md" {
		t.Errorf("replace must not rename: name = %q", res.Name)
	}
	if res.URL == doc.URL {
		t.Errorf("replace must mint a fresh capability key, got %q", res.URL)
	}

	// The NEW capability URL serves the NEW bytes; the superseded key stops
	// resolving (the old blob was removed post-commit).
	serveReq := httptest.NewRequest(http.MethodGet, res.URL, nil)
	serveRec := httptest.NewRecorder()
	env.router.ServeHTTP(serveRec, serveReq)
	if serveRec.Code != http.StatusOK || !bytes.Equal(serveRec.Body.Bytes(), []byte(newBody)) {
		t.Errorf("new capability URL = %d %q, want the new bytes", serveRec.Code, serveRec.Body.String())
	}
	serveReq = httptest.NewRequest(http.MethodGet, doc.URL, nil)
	serveRec = httptest.NewRecorder()
	env.router.ServeHTTP(serveRec, serveReq)
	if serveRec.Code != http.StatusNotFound {
		t.Errorf("superseded capability URL = %d, want 404", serveRec.Code)
	}
}

func TestReferenceDocuments_CrossWorkspace404(t *testing.T) {
	env := newRefDocEnv(t)
	doc := uploadDoc(t, env, "mine.md")

	// A second workspace the same owner belongs to; the foreign id resolves
	// not-found, indistinguishable from an unknown one (tenancy).
	foreign := &domain.Workspace{Name: "Other", Slug: "other"}
	if err := env.st.Workspaces().Create(context.Background(), foreign); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	role := &domain.Role{WorkspaceID: foreign.ID, Name: domain.RoleOwner, IsOwner: true, Permissions: domain.OwnerPermissions, BuiltIn: true}
	if err := env.st.Roles().Create(context.Background(), role); err != nil {
		t.Fatalf("seed foreign role: %v", err)
	}
	if err := env.st.Members().Add(context.Background(), &domain.Member{WorkspaceID: foreign.ID, UserID: env.owner.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("seed foreign membership: %v", err)
	}

	foreignCases := []struct {
		name   string
		method string
		path   string
	}{
		{"patch", http.MethodPatch, "/api/v1/workspaces/other/documents/" + doc.ID},
		{"promote", http.MethodPost, "/api/v1/workspaces/other/documents/" + doc.ID + "/promote"},
		{"delete", http.MethodDelete, "/api/v1/workspaces/other/documents/" + doc.ID},
		{"attach", http.MethodPut, "/api/v1/workspaces/other/documents/" + doc.ID + "/agents"},
		{"unknown id", http.MethodPatch, "/api/v1/workspaces/other/documents/no-such-document"},
	}
	for _, tc := range foreignCases {
		rec := refDocJSON(t, env.router, "owner", tc.method, tc.path, map[string]any{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d: %s", tc.name, rec.Code, rec.Body.String())
			continue
		}
		if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
			t.Errorf("%s: expected the not_found envelope: %s", tc.name, rec.Body.String())
		}
	}

	// The document itself is untouched.
	if _, err := env.st.ReferenceDocuments().Get(context.Background(), env.ws.ID, doc.ID); err != nil {
		t.Fatalf("foreign-id probes must not touch the row: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Capability serving: byte-identical downloads (spec scenario).
// ---------------------------------------------------------------------------

func TestReferenceDocuments_CapabilityURLServesUploadedBytes(t *testing.T) {
	env := newRefDocEnv(t)

	// A body the markdown pass-through keeps verbatim, so byte-identity is a
	// strong check.
	body := "# Runbook\n\n" + strings.Repeat("Step. ", 500)
	rec := refDocUpload(t, env.router, "owner", http.MethodPost, "/api/v1/workspaces/acme/documents", map[string][]string{"name": {"runbook"}}, "runbook.md", []byte(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	res := decodeDocument(t, rec)

	// Capability URLs serve without additional authentication — the files
	// route resolves the reference-document row by storage key and streams
	// the recorded backend's bytes.
	serveReq := httptest.NewRequest(http.MethodGet, res.URL, nil)
	serveRec := httptest.NewRecorder()
	env.router.ServeHTTP(serveRec, serveReq)
	if serveRec.Code != http.StatusOK {
		t.Fatalf("capability fetch: expected 200, got %d: %s", serveRec.Code, serveRec.Body.String())
	}
	if !bytes.Equal(serveRec.Body.Bytes(), []byte(body)) {
		t.Errorf("served bytes differ from the upload (%d vs %d bytes)", serveRec.Body.Len(), len(body))
	}
	if got := serveRec.Header().Get("Content-Type"); got != "text/markdown" {
		t.Errorf("content type = %q, want the stored mime", got)
	}
}

// ---------------------------------------------------------------------------
// Mixed-backend capability serving (route-reference-documents-through-
// workspace-storage task 4.2): a library holding pre-switch local rows and
// post-switch s3 rows serves both capability URLs byte-identical — the files
// route dispatches on the backend each row records, never on the workspace's
// current configuration.
// ---------------------------------------------------------------------------

// refDocS3StubBucket is the path-style bucket the stub and the seeded
// workspace storage config agree on.
const refDocS3StubBucket = "onclaw-refdocs-test"

// refDocS3Stub is a minimal path-style S3 stand-in answering the operations
// this flow issues (HeadBucket, PutObject, GetObject, DeleteObject) against an
// in-memory map. Loopback httptest only — no real S3, no external network
// (the same stand-in shape the s3 driver's own tests run against).
type refDocS3Stub struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
}

func newRefDocS3Stub() *refDocS3Stub {
	return &refDocS3Stub{objects: map[string][]byte{}, types: map[string]string{}}
}

func (s *refDocS3Stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Path-style addressing: /{bucket}/{key...}
	prefix := "/" + refDocS3StubBucket + "/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, prefix)

	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if refDocIsAWSChunked(r.Header) {
			body = refDocDecodeAWSChunked(body)
		}
		s.mu.Lock()
		s.objects[key] = body
		s.types[key] = r.Header.Get("Content-Type")
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)

	case http.MethodGet:
		s.mu.Lock()
		data, ok := s.objects[key]
		ct := s.types[key]
		s.mu.Unlock()
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`))
			return
		}
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)

	case http.MethodDelete:
		s.mu.Lock()
		delete(s.objects, key)
		delete(s.types, key)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func refDocIsAWSChunked(h http.Header) bool {
	return strings.HasPrefix(h.Get("x-amz-content-sha256"), "STREAMING") ||
		strings.Contains(h.Get("Content-Encoding"), "aws-chunked")
}

// refDocDecodeAWSChunked strips the aws-chunked framing the SDK applies when
// streaming uploads carry trailing checksums: hex size lines (optionally
// ;chunk-signature=...) followed by the data and a CRLF; the zero chunk ends
// the payload.
func refDocDecodeAWSChunked(body []byte) []byte {
	var out []byte
	for {
		nl := bytes.IndexByte(body, '\n')
		if nl < 0 {
			return body // framing ended without a zero chunk — plain body
		}
		line := strings.TrimSpace(string(body[:nl]))
		sizeHex := line
		if i := strings.IndexByte(sizeHex, ';'); i >= 0 {
			sizeHex = sizeHex[:i]
		}
		size, err := strconv.ParseInt(sizeHex, 16, 64)
		if err != nil {
			return body // not a chunk size line — plain body
		}
		body = body[nl+1:]
		if size == 0 {
			return out
		}
		if int64(len(body)) < size {
			return append(out, body...) // truncated — best effort
		}
		out = append(out, body[:size]...)
		body = body[size:]
		body = bytes.TrimPrefix(body, []byte("\r\n"))
	}
}

// seedRefDocS3Config plays the Storage settings pane: it upserts the
// workspace's storage row pointing at the stub endpoint, with the access
// secret sealed under the same instance key the resolver unseals with.
func seedRefDocS3Config(t *testing.T, env refDocEnv, endpoint string) {
	t.Helper()
	envelope, err := secrets.Encrypt([]byte("01234567890123456789012345678901"), []byte(env.ws.ID), []byte("stub-secret"))
	if err != nil {
		t.Fatalf("seal storage secret: %v", err)
	}
	if err := env.st.WorkspaceStorage().Upsert(context.Background(), &domain.WorkspaceStorageConfig{
		WorkspaceID:     env.ws.ID,
		Driver:          "s3",
		Endpoint:        endpoint,
		Region:          "us-east-1",
		Bucket:          refDocS3StubBucket,
		AccessKeyID:     "test-access-key",
		SecretAccessKey: envelope,
		UsePathStyle:    true,
	}); err != nil {
		t.Fatalf("upsert workspace storage config: %v", err)
	}
}

// TestReferenceDocuments_CapabilityURLMixedBackends pins the serving branch of
// the mixed-backend steady state: a pre-switch row recorded "local" and a
// post-switch row recorded "s3" both serve byte-identical through the backend
// each row records, while the workspace's current configuration is s3.
func TestReferenceDocuments_CapabilityURLMixedBackends(t *testing.T) {
	env := newRefDocEnv(t)

	// Pre-switch: the workspace is unconfigured, so the upload records the
	// instance default and the blob lands in the local data dir.
	localBody := "# Local\n\n" + strings.Repeat("Local step. ", 200)
	rec := refDocUpload(t, env.router, "owner", http.MethodPost, "/api/v1/workspaces/acme/documents", map[string][]string{"name": {"local-runbook"}}, "local-runbook.md", []byte(localBody))
	if rec.Code != http.StatusCreated {
		t.Fatalf("pre-switch upload: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	localDoc := decodeDocument(t, rec)

	// The pane switches the workspace's storage to the stub-backed s3 driver.
	stub := newRefDocS3Stub()
	srv := httptest.NewServer(stub)
	defer srv.Close()
	seedRefDocS3Config(t, env, srv.URL)

	// Post-switch upload: the blob lands in the bucket and the row records s3.
	cloudBody := "# Cloud\n\n" + strings.Repeat("Cloud step. ", 200)
	rec = refDocUpload(t, env.router, "owner", http.MethodPost, "/api/v1/workspaces/acme/documents", map[string][]string{"name": {"cloud-runbook"}}, "cloud-runbook.md", []byte(cloudBody))
	if rec.Code != http.StatusCreated {
		t.Fatalf("post-switch upload: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	cloudDoc := decodeDocument(t, rec)

	// The rows record the backends the blobs were written to.
	localRow, err := env.st.ReferenceDocuments().Get(context.Background(), env.ws.ID, localDoc.ID)
	if err != nil {
		t.Fatalf("load pre-switch row: %v", err)
	}
	cloudRow, err := env.st.ReferenceDocuments().Get(context.Background(), env.ws.ID, cloudDoc.ID)
	if err != nil {
		t.Fatalf("load post-switch row: %v", err)
	}
	if localRow.Backend != "local" || cloudRow.Backend != "s3" {
		t.Fatalf("recorded backends = %q/%q, want local/s3", localRow.Backend, cloudRow.Backend)
	}

	// The bucket holds the cloud blob byte-identical; the local blob never
	// leaked into it.
	stub.mu.Lock()
	stored, inBucket := stub.objects[cloudRow.StorageKey]
	leaked := stub.objects[localRow.StorageKey]
	stub.mu.Unlock()
	if !inBucket || !bytes.Equal(stored, []byte(cloudBody)) {
		t.Errorf("bucket blob for the s3 row: present %t byte-identical %t", inBucket, inBucket && bytes.Equal(stored, []byte(cloudBody)))
	}
	if leaked != nil {
		t.Errorf("local blob leaked into the s3 bucket (%d bytes)", len(leaked))
	}

	// Both capability URLs serve byte-identical — each through the backend its
	// row records, never the workspace's current (s3) configuration.
	for _, tc := range []struct {
		name string
		url  string
		want []byte
	}{
		{"pre-switch local row", localDoc.URL, []byte(localBody)},
		{"post-switch s3 row", cloudDoc.URL, []byte(cloudBody)},
	} {
		serveReq := httptest.NewRequest(http.MethodGet, tc.url, nil)
		serveRec := httptest.NewRecorder()
		env.router.ServeHTTP(serveRec, serveReq)
		if serveRec.Code != http.StatusOK {
			t.Errorf("%s: capability fetch expected 200, got %d: %s", tc.name, serveRec.Code, serveRec.Body.String())
			continue
		}
		if !bytes.Equal(serveRec.Body.Bytes(), tc.want) {
			t.Errorf("%s: served bytes differ from the upload (%d vs %d bytes)", tc.name, serveRec.Body.Len(), len(tc.want))
		}
	}
}

// ---------------------------------------------------------------------------
// Chat-turn document chips (add-reference-documents 10.4): the /v1 payload's
// chip objects must land on ExecRequest.Attachments under the document lane.
// ---------------------------------------------------------------------------

// TestFlattenInputParts_DocumentChips pins the wire parsing directly: typed
// input_document parts and the chip object verbatim both parse to document
// candidates with identity preserved; the visible text joins only input_text
// parts; a chip without its identity is malformed input.
func TestFlattenInputParts_DocumentChips(t *testing.T) {
	raw := []byte(`[
		{"type": "message", "role": "user", "content": [
			{"type": "input_text", "text": "compare this"},
			{"type": "input_document", "kind": "document", "documentId": "doc-1", "name": "twilio-api.pdf", "path": "references/twilio-api.pdf"},
			{"kind": "document", "documentId": "doc-2", "name": "notes.md", "path": "references/notes.md"}
		]}
	]`)
	text, atts, err := openresponses.FlattenInputParts(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if text != "compare this" {
		t.Errorf("text = %q, want only the input_text part", text)
	}
	if len(atts) != 2 {
		t.Fatalf("candidates = %d, want 2", len(atts))
	}
	if atts[0].Kind != "document" || atts[0].DocumentID != "doc-1" || atts[0].Filename != "twilio-api.pdf" || atts[0].Path != "references/twilio-api.pdf" {
		t.Errorf("typed chip = %+v", atts[0])
	}
	if atts[1].Kind != "document" || atts[1].DocumentID != "doc-2" || atts[1].Filename != "notes.md" || atts[1].Path != "references/notes.md" {
		t.Errorf("verbatim chip = %+v", atts[1])
	}

	for _, bad := range []string{
		`[{"type":"message","role":"user","content":[{"type":"input_document","documentId":"","name":"x.md"}]}]`,
		`[{"type":"message","role":"user","content":[{"kind":"document","documentId":"doc-1","name":""}]}]`,
	} {
		if _, _, err := openresponses.FlattenInputParts([]byte(bad)); err == nil {
			t.Errorf("malformed chip must error: %s", bad)
		}
	}
}

// capturingChatModel records every model input so the handler test can assert
// the document chip's pointer note reached the run.
type capturingChatModel struct {
	mu       sync.Mutex
	turns    int
	messages [][]*schema.AgenticMessage
}

func (m *capturingChatModel) Generate(_ context.Context, msgs []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.record(msgs)
	return stubAssistantMessage(), nil
}

func (m *capturingChatModel) Stream(_ context.Context, msgs []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	m.record(msgs)
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(stubAssistantMessage(), nil)
	sw.Close()
	return sr, nil
}

func (m *capturingChatModel) record(msgs []*schema.AgenticMessage) {
	m.mu.Lock()
	m.turns++
	m.messages = append(m.messages, msgs)
	m.mu.Unlock()
}

func (m *capturingChatModel) lastUserBlocks(t *testing.T) []*schema.ContentBlock {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		t.Fatal("the model never saw a turn")
	}
	msgs := m.messages[len(m.messages)-1]
	var blocks []*schema.ContentBlock
	for _, msg := range msgs {
		if msg != nil && msg.Role == schema.AgenticRoleTypeUser {
			blocks = append(blocks, msg.ContentBlocks...)
		}
	}
	return blocks
}

func stubAssistantMessage() *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "ok"})},
	}
}

// chipInstructionComposer is the stub compose seam the chip env's runner needs.
type chipInstructionComposer struct{}

func (chipInstructionComposer) Compose(_ context.Context, _ agents.ComposeParams) (string, error) {
	return "stub instruction", nil
}

type chipEnv struct {
	router  *gin.Engine
	capture *capturingChatModel
}

// newChipEnv wires POST /v1/responses exactly as the router does — the real
// runner over the fake store with a capturing model — and plays the API-key
// auth gate with a middleware.
func newChipEnv(t *testing.T) chipEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	strg := storagefake.New()
	onClawDir := t.TempDir()
	dataDir := t.TempDir()
	encKey := []byte("test-key-32-bytes-long-12345678")
	wsStorage := resolver.New(strg, st.WorkspaceStorage(), st.Attachments(), encKey, dataDir)

	memLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	embedder := memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), encKey, providers.NewRegistry())
	memWorker := memory.NewWorker(
		memory.NewGister(st.MemoryEvents(), st.SessionEvents(), st.MemoryEntities(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog),
		memory.NewGate(st.MemoryNotes(), st.MemoryEntities(), st.Memories(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog),
		embedder,
		st.MemoryEmbeddings(),
		memLog,
	)
	memSearch := memory.NewSearcher(st.MemoryNotes(), st.MemoryEvents(), st.MemoryEmbeddings(), st.MemoryEntities(), st.SessionEvents(), embedder)
	memGate := memory.NewIntentGate(st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog)

	capture := &capturingChatModel{}
	runner := agents.NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), memWorker, memSearch, memGate,
		encKey, onClawDir,
		agents.WithAgenticModelFactory(func(_ context.Context, _ string, _ providers.Credential, _ string) (model.BaseModel[*schema.AgenticMessage], error) {
			return capture, nil
		}),
		agents.WithInstructionComposer(chipInstructionComposer{}),
		agents.WithToolPolicy(agents.NewToolSettingsService(st.ToolSettings(), encKey)),
		// Mirror the router's wiring: attachment bytes resolve through the
		// same workspace storage resolver the handler uses, and the
		// references capability rides the same service shape.
		agents.WithAttachmentBlobs(wsStorage),
		agents.WithReferences(references.NewService(st, wsStorage)),
	)

	ws := &domain.Workspace{Slug: "chip", Name: "Chip WS", Timezone: "UTC"}
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

	user := &domain.User{Email: "chip@example.com", Name: "Chip"}
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

	v1H := handlers.NewV1Handlers(runner, st.Agents(), st.SessionEvents(), st.Attachments(), wsStorage, agents.NewToolSettingsService(st.ToolSettings(), nil), 0)
	r := gin.New()
	apiKey := &domain.WorkspaceAPIKey{WorkspaceID: ws.ID, CreatedBy: user.ID}
	r.Use(func(c *gin.Context) {
		c.Set(handlers.APIKeyContextKey, apiKey)
		c.Next()
	})
	r.POST("/v1/responses", v1H.CreateResponse)
	return chipEnv{router: r, capture: capture}
}

func chipTurn(t *testing.T, env chipEnv, content []map[string]any, sess string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{
		"model": "atlas",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": content},
		},
		"metadata": map[string]string{"onclaw_session": sess},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

func TestV1Responses_DocumentChipReachesAttachments(t *testing.T) {
	env := newChipEnv(t)

	rec := chipTurn(t, env, []map[string]any{
		{"type": "input_text", "text": "compare this with the official limits"},
		// The typed part form.
		{"type": "input_document", "kind": "document", "documentId": "doc-1", "name": "twilio-api.pdf", "path": "references/twilio-api.pdf"},
		// The chip object verbatim, as the composer holds it locally.
		{"kind": "document", "documentId": "doc-2", "name": "integration-notes.md", "path": "references/integration-notes.md"},
	}, "sess-chip-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var notes []string
	for _, block := range env.capture.lastUserBlocks(t) {
		if block != nil && block.UserInputText != nil {
			notes = append(notes, block.UserInputText.Text)
		}
	}
	joined := strings.Join(notes, "\n")
	// One pointer note per chip: identity only, teaching the document tools.
	if !strings.Contains(joined, `User referenced document "twilio-api.pdf"`) || !strings.Contains(joined, "references/twilio-api.pdf") {
		t.Errorf("pointer note for doc-1 missing: %q", joined)
	}
	if !strings.Contains(joined, `User referenced document "integration-notes.md"`) {
		t.Errorf("pointer note for doc-2 missing: %q", joined)
	}
	if !strings.Contains(joined, "document.search") {
		t.Errorf("pointer notes must teach the document tools: %q", joined)
	}
}

func TestV1Responses_MalformedDocumentChipRejected(t *testing.T) {
	env := newChipEnv(t)

	rec := chipTurn(t, env, []map[string]any{
		{"type": "input_text", "text": "no identity"},
		{"type": "input_document", "kind": "document", "documentId": "", "name": "twilio-api.pdf"},
	}, "sess-chip-bad")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_request_error") {
		t.Errorf("envelope = %s, want invalid_request_error", rec.Body.String())
	}
	if env.capture.turns != 0 {
		t.Errorf("a rejected chip must not run the turn, saw %d", env.capture.turns)
	}
}
