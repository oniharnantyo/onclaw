package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// storageProbeStub is the injected probe double: tests decide whether the
// connectivity check succeeds and count the invocations (PUT local must
// never probe; each s3 save probes exactly once) and capture the config the
// handler resolved (keep-stored secret merge assertions).
type storageProbeStub struct {
	err     error
	calls   int
	lastCfg storage.StorageConfig
}

func (p *storageProbeStub) probe(ctx context.Context, cfg storage.StorageConfig) error {
	p.calls++
	p.lastCfg = cfg
	return p.err
}

// newStorageConfigEnv builds a gin engine with the storage config routes and
// an inline permission gate playing RequirePermission(domain.WorkspaceWrite)
// (the real gate is wired in router.go and covered end-to-end by smoke):
// owners pass, plain members are rejected 403.
func newStorageConfigEnv(t *testing.T, probe *storageProbeStub, member bool) (*gin.Engine, store.Store, *domain.Workspace) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	user := &domain.User{Email: "owner@example.com", Name: "Owner"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	h := handlers.NewStorageConfigHandlers(st.WorkspaceStorage(), attTestEncKey, probe.probe)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if member {
			handlers.AbortForbidden(c, "workspace settings management required")
			return
		}
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.UserContextKey, user)
		c.Next()
	})
	r.GET("/api/v1/workspaces/:ws/storage", h.GetStorage)
	r.PUT("/api/v1/workspaces/:ws/storage", h.PutStorage)
	r.POST("/api/v1/workspaces/:ws/storage/probe", h.ProbeStorage)
	return r, st, ws
}

func storageConfigRequest(t *testing.T, r *gin.Engine, method string, payload map[string]any) (int, []byte) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(method, "/api/v1/workspaces/acme/storage", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func storageProbeRequest(t *testing.T, r *gin.Engine, payload map[string]any) (int, []byte) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/storage/probe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func s3ConfigPayload() map[string]any {
	return map[string]any{
		"driver":            "s3",
		"endpoint":          "https://s3.us-east-1.amazonaws.com",
		"region":            "us-east-1",
		"bucket":            "acme-attachments",
		"access_key_id":     "AKIAIOSFODNN7EXAMPLE",
		"secret_access_key": "wJalrXUtnFEMI-abcd",
		"use_path_style":    false,
	}
}

func TestStorageConfig_GetAbsentReturnsLocalDefault(t *testing.T) {
	r, _, _ := newStorageConfigEnv(t, &storageProbeStub{}, false)

	status, body := storageConfigRequest(t, r, http.MethodGet, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	var view map[string]any
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view["driver"] != "local" {
		t.Errorf("default driver = %v, want local", view["driver"])
	}
	// Workspace-generic default: no s3 fields, no secret material.
	for _, key := range []string{"endpoint", "region", "bucket", "access_key_id", "secret_hint"} {
		if _, present := view[key]; present {
			t.Errorf("default view must omit %q, got %v", key, view)
		}
	}
}

func TestStorageConfig_PutS3SuccessStoresSealedSecret(t *testing.T) {
	probe := &storageProbeStub{}
	r, st, ws := newStorageConfigEnv(t, probe, false)

	status, body := storageConfigRequest(t, r, http.MethodPut, s3ConfigPayload())
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	if probe.calls != 1 {
		t.Errorf("probe calls = %d, want exactly 1 on s3 save", probe.calls)
	}

	// The row holds a sealed envelope, decryptable only with the instance
	// key and the workspace id as AAD.
	cfg, err := st.WorkspaceStorage().Get(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("stored config missing: %v", err)
	}
	if !strings.HasPrefix(cfg.SecretAccessKey, secrets.Version1Prefix+":") {
		t.Fatalf("stored secret = %q, want a sealed envelope", cfg.SecretAccessKey)
	}
	plaintext, err := secrets.Decrypt(attTestEncKey, []byte(ws.ID), cfg.SecretAccessKey)
	if err != nil {
		t.Fatalf("decrypt stored envelope: %v", err)
	}
	if string(plaintext) != "wJalrXUtnFEMI-abcd" {
		t.Errorf("decrypted secret = %q, want the submitted plaintext", plaintext)
	}

	// The response view is masked: no plaintext, no envelope, last-4 hint.
	if strings.Contains(string(body), "wJalrXUtnFEMI-abcd") || strings.Contains(string(body), secrets.Version1Prefix+":") {
		t.Errorf("response leaked secret material: %s", body)
	}
	var view map[string]any
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if view["secret_hint"] != "abcd" {
		t.Errorf("secret_hint = %v, want the last-4", view["secret_hint"])
	}
	if view["driver"] != "s3" || view["bucket"] != "acme-attachments" {
		t.Errorf("view fields = %v", view)
	}
}

func TestStorageConfig_GetStoredMasksSecret(t *testing.T) {
	probe := &storageProbeStub{}
	r, _, _ := newStorageConfigEnv(t, probe, false)

	if status, body := storageConfigRequest(t, r, http.MethodPut, s3ConfigPayload()); status != http.StatusOK {
		t.Fatalf("seed s3 config: expected 200, got %d: %s", status, body)
	}

	status, body := storageConfigRequest(t, r, http.MethodGet, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	if strings.Contains(string(body), "wJalrXUtnFEMI-abcd") || strings.Contains(string(body), secrets.Version1Prefix+":") {
		t.Errorf("GET leaked secret material: %s", body)
	}
	var view map[string]any
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view["secret_hint"] != "abcd" || view["endpoint"] != "https://s3.us-east-1.amazonaws.com" {
		t.Errorf("masked view = %v", view)
	}
	if _, present := view["updated_at"]; !present {
		t.Errorf("stored view must carry updated_at: %v", view)
	}
}

func TestStorageConfig_PutS3ProbeFailureKeepsPreviousConfig(t *testing.T) {
	probe := &storageProbeStub{}
	r, st, ws := newStorageConfigEnv(t, probe, false)

	// Seed the previous configuration directly (already active s3).
	sealed, err := secrets.Encrypt(attTestEncKey, []byte(ws.ID), []byte("previous-secret-9999"))
	if err != nil {
		t.Fatalf("seal previous secret: %v", err)
	}
	previous := &domain.WorkspaceStorageConfig{
		WorkspaceID:     ws.ID,
		Driver:          "s3",
		Endpoint:        "https://old.example.com",
		Region:          "eu-west-1",
		Bucket:          "old-bucket",
		AccessKeyID:     "OLDKEY",
		SecretAccessKey: sealed,
	}
	if err := st.WorkspaceStorage().Upsert(context.Background(), previous); err != nil {
		t.Fatalf("seed previous config: %v", err)
	}

	// A save with a wrong secret: the probe fails and the save is rejected
	// 422 carrying the probe's reason (spec scenario).
	probe.err = errors.New(`bucket "acme-attachments" unreachable: The security token included in the request is invalid`)
	payload := s3ConfigPayload()
	status, body := storageConfigRequest(t, r, http.MethodPut, payload)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", status, body)
	}
	if !strings.Contains(string(body), "The security token included in the request is invalid") {
		t.Errorf("422 must carry the probe's reason verbatim: %s", body)
	}

	// The previous configuration remains active.
	cfg, err := st.WorkspaceStorage().Get(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("previous config lost: %v", err)
	}
	if cfg.Endpoint != "https://old.example.com" || cfg.Bucket != "old-bucket" || cfg.SecretAccessKey != sealed {
		t.Errorf("previous config was mutated: %+v", cfg)
	}
}

func TestStorageConfig_PutLocalSkipsProbeAndClearsS3(t *testing.T) {
	probe := &storageProbeStub{}
	r, st, ws := newStorageConfigEnv(t, probe, false)

	// Seed an active s3 config first.
	if status, body := storageConfigRequest(t, r, http.MethodPut, s3ConfigPayload()); status != http.StatusOK {
		t.Fatalf("seed s3: expected 200, got %d: %s", status, body)
	}

	status, body := storageConfigRequest(t, r, http.MethodPut, map[string]any{"driver": "local"})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	// K8: the switch to Local is probe-free and wipes the s3 config.
	if probe.calls != 1 { // only the s3 seed probed
		t.Errorf("probe calls = %d, want the local switch to probe-free", probe.calls)
	}
	cfg, err := st.WorkspaceStorage().Get(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("stored config missing: %v", err)
	}
	if cfg.Driver != "local" || cfg.Endpoint != "" || cfg.Bucket != "" || cfg.SecretAccessKey != "" {
		t.Errorf("local upsert must clear s3 fields, got %+v", cfg)
	}
	var view map[string]any
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if view["driver"] != "local" {
		t.Errorf("view = %v, want the local default shape", view)
	}
}

func TestStorageConfig_PutS3MissingFieldsIs400(t *testing.T) {
	probe := &storageProbeStub{}
	r, _, _ := newStorageConfigEnv(t, probe, false)

	payload := s3ConfigPayload()
	delete(payload, "endpoint")
	delete(payload, "bucket")
	status, body := storageConfigRequest(t, r, http.MethodPut, payload)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", status, body)
	}
	if !strings.Contains(string(body), "endpoint") || !strings.Contains(string(body), "bucket") {
		t.Errorf("400 must name the missing fields: %s", body)
	}
	if probe.calls != 0 {
		t.Errorf("validation failure must not probe, got %d calls", probe.calls)
	}
}

func TestStorageConfig_UnknownDriverIs400(t *testing.T) {
	probe := &storageProbeStub{}
	r, _, _ := newStorageConfigEnv(t, probe, false)

	status, _ := storageConfigRequest(t, r, http.MethodPut, map[string]any{"driver": "gcs"})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestStorageConfig_KeepStoredSecretEchoesHint(t *testing.T) {
	probe := &storageProbeStub{}
	r, st, ws := newStorageConfigEnv(t, probe, false)

	// Seed the original configuration.
	if status, body := storageConfigRequest(t, r, http.MethodPut, s3ConfigPayload()); status != http.StatusOK {
		t.Fatalf("seed s3: expected 200, got %d: %s", status, body)
	}
	stored, err := st.WorkspaceStorage().Get(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("stored config missing: %v", err)
	}
	originalEnvelope := stored.SecretAccessKey

	// Re-save echoing the masked hint (and a new endpoint): the stored
	// envelope survives verbatim — the secret was not changed.
	payload := s3ConfigPayload()
	payload["secret_access_key"] = "abcd" // the hint exactly
	payload["endpoint"] = "https://s3.eu-west-1.amazonaws.com"
	status, body := storageConfigRequest(t, r, http.MethodPut, payload)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	stored, err = st.WorkspaceStorage().Get(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("re-fetch stored config: %v", err)
	}
	if stored.SecretAccessKey != originalEnvelope {
		t.Error("hint-echoed save must keep the stored envelope verbatim")
	}
	plaintext, err := secrets.Decrypt(attTestEncKey, []byte(ws.ID), stored.SecretAccessKey)
	if err != nil || string(plaintext) != "wJalrXUtnFEMI-abcd" {
		t.Errorf("stored secret changed: decrypt = %q err = %v", plaintext, err)
	}
	if stored.Endpoint != "https://s3.eu-west-1.amazonaws.com" {
		t.Errorf("non-secret field update lost: %q", stored.Endpoint)
	}
	_ = body
}

func TestStorageConfig_MemberForbiddenOnGetAndPut(t *testing.T) {
	probe := &storageProbeStub{}
	r, _, _ := newStorageConfigEnv(t, probe, true)

	status, body := storageConfigRequest(t, r, http.MethodGet, nil)
	if status != http.StatusForbidden {
		t.Fatalf("member GET: expected 403, got %d: %s", status, body)
	}
	status, body = storageConfigRequest(t, r, http.MethodPut, s3ConfigPayload())
	if status != http.StatusForbidden {
		t.Fatalf("member PUT: expected 403, got %d: %s", status, body)
	}
	if probe.calls != 0 {
		t.Errorf("forbidden PUT must not probe, got %d calls", probe.calls)
	}
}

func TestStorageConfig_ProbeS3SuccessPersistsNothing(t *testing.T) {
	probe := &storageProbeStub{}
	r, st, ws := newStorageConfigEnv(t, probe, false)

	status, body := storageProbeRequest(t, r, s3ConfigPayload())
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	if probe.calls != 1 {
		t.Errorf("probe calls = %d, want exactly 1 on s3 probe", probe.calls)
	}
	var view map[string]any
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view["ok"] != true {
		t.Errorf("probe view = %v, want {ok:true}", view)
	}

	// The probe is read-only: the workspace still answers the local default.
	if _, err := st.WorkspaceStorage().Get(context.Background(), ws.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("probe must persist nothing, stored row err = %v", err)
	}
}

func TestStorageConfig_ProbeS3FailureCarriesReasonAndPersistsNothing(t *testing.T) {
	probe := &storageProbeStub{}
	r, st, ws := newStorageConfigEnv(t, probe, false)

	// Seed an active configuration: a failed probe must leave it untouched.
	if status, body := storageConfigRequest(t, r, http.MethodPut, s3ConfigPayload()); status != http.StatusOK {
		t.Fatalf("seed s3: expected 200, got %d: %s", status, body)
	}
	stored, err := st.WorkspaceStorage().Get(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("stored config missing: %v", err)
	}
	originalEnvelope := stored.SecretAccessKey

	probe.err = errors.New(`bucket "acme-attachments" unreachable: The security token included in the request is invalid`)
	probe.calls = 0
	status, body := storageProbeRequest(t, r, s3ConfigPayload())
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", status, body)
	}
	if !strings.Contains(string(body), "The security token included in the request is invalid") {
		t.Errorf("422 must carry the probe's reason verbatim: %s", body)
	}

	// The previously active configuration survives a failed probe verbatim.
	stored, err = st.WorkspaceStorage().Get(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("stored config lost: %v", err)
	}
	if stored.SecretAccessKey != originalEnvelope || stored.Endpoint != "https://s3.us-east-1.amazonaws.com" {
		t.Errorf("failed probe mutated the stored row: %+v", stored)
	}
}

func TestStorageConfig_ProbeS3MissingFieldsIs400(t *testing.T) {
	probe := &storageProbeStub{}
	r, _, _ := newStorageConfigEnv(t, probe, false)

	payload := s3ConfigPayload()
	delete(payload, "endpoint")
	delete(payload, "bucket")
	status, body := storageProbeRequest(t, r, payload)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", status, body)
	}
	if !strings.Contains(string(body), "endpoint") || !strings.Contains(string(body), "bucket") {
		t.Errorf("400 must name the missing fields: %s", body)
	}
	if probe.calls != 0 {
		t.Errorf("validation failure must not probe, got %d calls", probe.calls)
	}
}

func TestStorageConfig_ProbeLocalSkipsProbe(t *testing.T) {
	probe := &storageProbeStub{}
	r, _, _ := newStorageConfigEnv(t, probe, false)

	status, body := storageProbeRequest(t, r, map[string]any{"driver": "local"})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	if probe.calls != 0 {
		t.Errorf("local probe must be probe-free, got %d calls", probe.calls)
	}
	var view map[string]any
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view["ok"] != true {
		t.Errorf("probe view = %v, want {ok:true}", view)
	}
}

func TestStorageConfig_ProbeS3ResolvesKeepStoredSecret(t *testing.T) {
	probe := &storageProbeStub{}
	r, _, _ := newStorageConfigEnv(t, probe, false)

	// Seed the original configuration so the probe has a stored secret to
	// merge against.
	if status, body := storageConfigRequest(t, r, http.MethodPut, s3ConfigPayload()); status != http.StatusOK {
		t.Fatalf("seed s3: expected 200, got %d: %s", status, body)
	}

	// Untouched secret field (empty string): the probe must run against the
	// stored plaintext, not the empty submit.
	payload := s3ConfigPayload()
	payload["secret_access_key"] = ""
	payload["endpoint"] = "https://s3.eu-west-1.amazonaws.com"
	probe.calls = 0
	if status, body := storageProbeRequest(t, r, payload); status != http.StatusOK {
		t.Fatalf("probe with empty secret: expected 200, got %d: %s", status, body)
	}
	if probe.lastCfg.SecretKey != "wJalrXUtnFEMI-abcd" {
		t.Errorf("probed secret = %q, want the stored plaintext", probe.lastCfg.SecretKey)
	}
	if probe.lastCfg.Endpoint != "https://s3.eu-west-1.amazonaws.com" {
		t.Errorf("probed endpoint = %q, want the submitted endpoint", probe.lastCfg.Endpoint)
	}

	// Hint echo keeps the stored secret for the probe too.
	payload["secret_access_key"] = "abcd"
	if status, body := storageProbeRequest(t, r, payload); status != http.StatusOK {
		t.Fatalf("probe with hint echo: expected 200, got %d: %s", status, body)
	}
	if probe.lastCfg.SecretKey != "wJalrXUtnFEMI-abcd" {
		t.Errorf("hint-echo probed secret = %q, want the stored plaintext", probe.lastCfg.SecretKey)
	}
}

func TestStorageConfig_ProbeMemberForbidden(t *testing.T) {
	probe := &storageProbeStub{}
	r, _, _ := newStorageConfigEnv(t, probe, true)

	status, body := storageProbeRequest(t, r, s3ConfigPayload())
	if status != http.StatusForbidden {
		t.Fatalf("member probe: expected 403, got %d: %s", status, body)
	}
	if probe.calls != 0 {
		t.Errorf("forbidden probe must not run, got %d calls", probe.calls)
	}
}
