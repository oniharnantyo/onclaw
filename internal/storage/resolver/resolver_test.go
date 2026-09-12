package resolver_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/storage"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// recordingDriver pairs an in-memory storage with the config it was built
// from, so tests can pin exactly what the resolver hands the driver.
type recordingDriver struct {
	storage.Storage
	cfg storage.StorageConfig
}

var (
	recMu      sync.Mutex
	recDrivers []*recordingDriver
)

var registerOnce sync.Once

// registerRecordingS3 registers a fake driver under the real "s3" name. This
// test binary never imports internal/storage/s3, so the real driver's init()
// never runs and the name is free; the factory records every construction.
func registerRecordingS3() {
	registerOnce.Do(func() {
		storage.Register("s3", func(cfg storage.StorageConfig) (storage.Storage, error) {
			d := &recordingDriver{Storage: storagefake.New(), cfg: cfg}
			recMu.Lock()
			recDrivers = append(recDrivers, d)
			recMu.Unlock()
			return d, nil
		})
	})
}

func recordedDrivers() []*recordingDriver {
	recMu.Lock()
	defer recMu.Unlock()
	return append([]*recordingDriver(nil), recDrivers...)
}

var testKey = bytes.Repeat([]byte{0x2a}, secrets.KeySize)

// newResolver builds a resolver over a fresh fake store with an in-memory
// instance default; it returns the instance default for identity assertions.
func newResolver(t *testing.T) (*resolver.WorkspaceStorage, store.Store, storage.Storage) {
	t.Helper()
	registerRecordingS3()
	recMu.Lock()
	recDrivers = nil // counts below are per-test
	recMu.Unlock()
	st := storefake.New()
	instanceDefault := storagefake.New()
	r := resolver.New(instanceDefault, st.WorkspaceStorage(), st.Attachments(), testKey, t.TempDir())
	return r, st, instanceDefault
}

func seedWorkspace(t *testing.T, ctx context.Context, st store.Store) string {
	t.Helper()
	wsID := uuid.NewString()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{ID: wsID, Name: "Acme", Slug: "acme-" + wsID[:8]}); err != nil {
		t.Fatalf("failed to seed workspace: %v", err)
	}
	return wsID
}

func seedS3Fields(t *testing.T, ctx context.Context, st store.Store, wsID, driver, bucket string) {
	t.Helper()
	envelope, err := secrets.Encrypt(testKey, []byte(wsID), []byte("s3-secret-plaintext"))
	if err != nil {
		t.Fatalf("failed to seal test secret: %v", err)
	}
	cfg := &domain.WorkspaceStorageConfig{
		WorkspaceID:     wsID,
		Driver:          driver,
		Endpoint:        "https://s3.example.com",
		Region:          "us-east-1",
		Bucket:          bucket,
		AccessKeyID:     "AKIDEXAMPLE",
		SecretAccessKey: envelope,
		UsePathStyle:    true,
	}
	if err := st.WorkspaceStorage().Upsert(ctx, cfg); err != nil {
		t.Fatalf("failed to seed storage config: %v", err)
	}
}

func seedAttachment(t *testing.T, ctx context.Context, st store.Store, wsID, backend, storageKey string) string {
	t.Helper()
	userID := uuid.NewString()
	if err := st.Users().Create(ctx, &domain.User{ID: userID, Email: "owner-" + wsID[:8] + "@acme.test", Name: "Owner"}); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	a := &domain.Attachment{
		ID:          uuid.NewString(),
		WorkspaceID: wsID,
		StorageKey:  storageKey,
		Backend:     backend,
		Name:        "shot.png",
		MimeType:    "image/png",
		Size:        4,
		Lane:        domain.AttachmentLaneInlineImage,
		CreatedBy:   userID,
	}
	if err := st.Attachments().Create(ctx, a); err != nil {
		t.Fatalf("failed to seed attachment: %v", err)
	}
	return a.ID
}

func TestForWorkspaceUnconfiguredReturnsInstanceDefault(t *testing.T) {
	ctx := context.Background()
	r, st, instanceDefault := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)

	got, err := r.ForWorkspace(ctx, wsID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != storage.Storage(instanceDefault) {
		t.Fatal("expected the instance default for an unconfigured workspace")
	}
	if len(recordedDrivers()) != 0 {
		t.Fatal("expected no driver constructions")
	}
}

func TestForWorkspaceLocalDriverReturnsInstanceDefault(t *testing.T) {
	ctx := context.Background()
	r, st, instanceDefault := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)

	if err := st.WorkspaceStorage().Upsert(ctx, &domain.WorkspaceStorageConfig{WorkspaceID: wsID, Driver: "local"}); err != nil {
		t.Fatalf("failed to seed local config: %v", err)
	}

	got, err := r.ForWorkspace(ctx, wsID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != storage.Storage(instanceDefault) {
		t.Fatal("expected the instance default for a local-configured workspace")
	}
}

func TestForWorkspaceS3BuildsDriverAndUnsealsSecret(t *testing.T) {
	ctx := context.Background()
	r, st, instanceDefault := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)
	seedS3Fields(t, ctx, st, wsID, "s3", "acme-blobs")

	got, err := r.ForWorkspace(ctx, wsID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == storage.Storage(instanceDefault) || got == nil {
		t.Fatal("expected a constructed s3 driver, not the instance default")
	}

	drivers := recordedDrivers()
	if len(drivers) != 1 {
		t.Fatalf("expected exactly one driver construction, got %d", len(drivers))
	}
	cfg := drivers[0].cfg
	if cfg.Driver != "s3" || cfg.Endpoint != "https://s3.example.com" || cfg.Region != "us-east-1" ||
		cfg.Bucket != "acme-blobs" || cfg.AccessKey != "AKIDEXAMPLE" || !cfg.UsePathStyle {
		t.Fatalf("unexpected driver config: %+v", cfg)
	}
	if cfg.SecretKey != "s3-secret-plaintext" {
		t.Fatalf("expected the unsealed secret, got %q", cfg.SecretKey)
	}
	if cfg.BaseURL != "/api/v1/files/" {
		t.Fatalf("expected the proxied capability base URL, got %q", cfg.BaseURL)
	}
}

func TestForWorkspaceS3CacheHitReturnsSameInstance(t *testing.T) {
	ctx := context.Background()
	r, st, _ := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)
	seedS3Fields(t, ctx, st, wsID, "s3", "acme-blobs")

	first, err := r.ForWorkspace(ctx, wsID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := r.ForWorkspace(ctx, wsID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first != second {
		t.Fatal("expected the cached instance across calls")
	}
	if len(recordedDrivers()) != 1 {
		t.Fatalf("expected one construction (cache hit must not rebuild), got %d", len(recordedDrivers()))
	}
}

func TestForWorkspaceConfigChangeBuildsNewInstance(t *testing.T) {
	ctx := context.Background()
	r, st, _ := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)
	seedS3Fields(t, ctx, st, wsID, "s3", "acme-blobs")

	first, err := r.ForWorkspace(ctx, wsID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A configuration change (different bucket) invalidates the entry.
	seedS3Fields(t, ctx, st, wsID, "s3", "acme-blobs-v2")
	second, err := r.ForWorkspace(ctx, wsID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first == second {
		t.Fatal("expected a new instance after a config change")
	}
	if len(recordedDrivers()) != 2 {
		t.Fatalf("expected two constructions after the config change, got %d", len(recordedDrivers()))
	}
	if got := second.(*recordingDriver).cfg.Bucket; got != "acme-blobs-v2" {
		t.Fatalf("expected the new bucket, got %q", got)
	}
}

func TestForWorkspaceUnknownDriverInvalid(t *testing.T) {
	ctx := context.Background()
	r, st, _ := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)

	if err := st.WorkspaceStorage().Upsert(ctx, &domain.WorkspaceStorageConfig{WorkspaceID: wsID, Driver: "gcs"}); err != nil {
		t.Fatalf("failed to seed config: %v", err)
	}
	if _, err := r.ForWorkspace(ctx, wsID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for an unknown driver, got %v", err)
	}
}

// TestOpenAttachmentThroughRecordedBackend pins the D16 read path: the
// attachment row records backend "s3" while the workspace's current config is
// local — writes stay on the instance default, and the read builds the
// recorded backend from the row's stored s3 fields (backend column only, no
// config snapshot).
func TestOpenAttachmentThroughRecordedBackend(t *testing.T) {
	ctx := context.Background()
	r, st, instanceDefault := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)
	seedS3Fields(t, ctx, st, wsID, "local", "acme-blobs")

	// Prime the recorded-backend driver and store its blob bytes.
	backend, err := r.ForBackend(ctx, wsID, "s3")
	if err != nil {
		t.Fatalf("unexpected ForBackend error: %v", err)
	}
	content := []byte("\x89PNG blob bytes")
	if err := backend.Put(ctx, "att/blob-key", bytes.NewReader(content), int64(len(content)), "image/png"); err != nil {
		t.Fatalf("failed to store blob through the s3 backend: %v", err)
	}

	// Writes still go through the instance default (workspace driver local).
	writes, err := r.ForWorkspace(ctx, wsID)
	if err != nil {
		t.Fatalf("unexpected ForWorkspace error: %v", err)
	}
	if writes != storage.Storage(instanceDefault) {
		t.Fatal("expected writes on the local workspace to use the instance default")
	}

	attID := seedAttachment(t, ctx, st, wsID, "s3", "att/blob-key")
	got, err := r.OpenAttachment(ctx, wsID, attID)
	if err != nil {
		t.Fatalf("unexpected OpenAttachment error: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("expected the stored blob bytes through the recorded backend")
	}
}

func TestOpenAttachmentForeignWorkspaceNotFound(t *testing.T) {
	ctx := context.Background()
	r, st, _ := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)
	otherWsID := seedWorkspace(t, ctx, st)
	seedS3Fields(t, ctx, st, wsID, "s3", "acme-blobs")
	if err := storagefake.New().Put(ctx, "att/k", bytes.NewReader([]byte("x")), 1, "text/plain"); err != nil {
		t.Fatalf("failed to store blob: %v", err)
	}
	attID := seedAttachment(t, ctx, st, wsID, "s3", "att/k")

	// A foreign workspace gets ErrNotFound — foreign and unknown are
	// indistinguishable (tenancy).
	if _, err := r.OpenAttachment(ctx, otherWsID, attID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a foreign workspace, got %v", err)
	}
}

// TestAttachmentURLThroughRecordedBackend pins the URL read path the runner's
// message construction uses (attachments design D1/D5): the capability URL is
// derived from the recorded backend over the row's storage key, and a foreign
// workspace is indistinguishable from an unknown attachment.
func TestAttachmentURLThroughRecordedBackend(t *testing.T) {
	ctx := context.Background()
	r, st, instanceDefault := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)

	attID := seedAttachment(t, ctx, st, wsID, "local", "att/cap-key")
	url, err := r.AttachmentURL(ctx, wsID, attID)
	if err != nil {
		t.Fatalf("unexpected AttachmentURL error: %v", err)
	}
	if want := instanceDefault.URL("att/cap-key"); url != want {
		t.Errorf("AttachmentURL = %q, want the recorded backend's capability URL %q", url, want)
	}

	otherWsID := seedWorkspace(t, ctx, st)
	if _, err := r.AttachmentURL(ctx, otherWsID, attID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a foreign workspace, got %v", err)
	}
}

func TestOpenAttachmentUnknownBackendInvalid(t *testing.T) {
	ctx := context.Background()
	r, st, _ := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)
	attID := seedAttachment(t, ctx, st, wsID, "gcs", "att/k")

	if _, err := r.OpenAttachment(ctx, wsID, attID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for an unknown recorded backend, got %v", err)
	}
}

func TestForBackendS3WithoutConfigNotFound(t *testing.T) {
	ctx := context.Background()
	r, st, _ := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)

	if _, err := r.ForBackend(ctx, wsID, "s3"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound without a storage config row, got %v", err)
	}
}

func TestCorruptEnvelopeErrorsAndNamesWorkspaceOnly(t *testing.T) {
	ctx := context.Background()
	r, st, _ := newResolver(t)
	wsID := seedWorkspace(t, ctx, st)

	// Malformed envelope.
	if err := st.WorkspaceStorage().Upsert(ctx, &domain.WorkspaceStorageConfig{
		WorkspaceID: wsID, Driver: "s3", Region: "us-east-1", Bucket: "b", SecretAccessKey: "not-an-envelope",
	}); err != nil {
		t.Fatalf("failed to seed config: %v", err)
	}
	_, err := r.ForWorkspace(ctx, wsID)
	if !errors.Is(err, domain.ErrUndecryptable) {
		t.Fatalf("expected ErrUndecryptable for a malformed envelope, got %v", err)
	}
	if !strings.Contains(err.Error(), wsID) {
		t.Fatalf("expected the error to name the workspace, got %q", err.Error())
	}

	// Envelope sealed under a different key (authentication failure).
	wrongKey := bytes.Repeat([]byte{0x31}, secrets.KeySize)
	envelope, encErr := secrets.Encrypt(wrongKey, []byte(wsID), []byte("real-secret-plaintext"))
	if encErr != nil {
		t.Fatalf("failed to seal secret: %v", encErr)
	}
	if err := st.WorkspaceStorage().Upsert(ctx, &domain.WorkspaceStorageConfig{
		WorkspaceID: wsID, Driver: "s3", Region: "us-east-1", Bucket: "b", SecretAccessKey: envelope,
	}); err != nil {
		t.Fatalf("failed to seed config: %v", err)
	}
	_, err = r.ForWorkspace(ctx, wsID)
	if !errors.Is(err, domain.ErrUndecryptable) {
		t.Fatalf("expected ErrUndecryptable for a wrong-key envelope, got %v", err)
	}
	// The secret material must never appear in the surfaced error.
	if strings.Contains(err.Error(), "real-secret-plaintext") {
		t.Fatalf("error leaked the secret: %q", err.Error())
	}
	if !strings.Contains(err.Error(), wsID) {
		t.Fatalf("expected the error to name the workspace, got %q", err.Error())
	}
}
