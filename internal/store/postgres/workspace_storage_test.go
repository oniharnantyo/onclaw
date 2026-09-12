//go:build integration

package postgres_test

import (
	"crypto/rand"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
)

// sealTestSecret seals plaintext with a fresh random key (the same mechanism
// the handlers use at the boundary) and returns the envelope plus the key so
// the test can unseal what the store returns.
func sealTestSecret(t *testing.T, workspaceID, plaintext string) (envelope string, key []byte) {
	t.Helper()
	key = make([]byte, secrets.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate random key: %v", err)
	}
	envelope, err := secrets.Encrypt(key, []byte(workspaceID), []byte(plaintext))
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}
	return envelope, key
}

// TestIntegration_WorkspaceStorageStore_GetAbsentNotFound covers the
// instance-default contract: absence of a row is domain.ErrNotFound and the
// caller applies the local default.
func TestIntegration_WorkspaceStorageStore_GetAbsentNotFound(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := &domain.Workspace{Slug: "wsstorage-absent", Name: "Absent"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	if _, err := s.WorkspaceStorage().Get(ctx, ws.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("absent config: expected ErrNotFound, got %v", err)
	}
}

// TestIntegration_WorkspaceStorageStore_UpsertGetRoundTrip covers the write →
// read round-trip: every field including use_path_style survives, the sealed
// envelope comes back byte-for-byte and unseals to the original secret, and
// updated_at is store-managed.
func TestIntegration_WorkspaceStorageStore_UpsertGetRoundTrip(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := &domain.Workspace{Slug: "wsstorage-roundtrip", Name: "RoundTrip"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	envelope, key := sealTestSecret(t, ws.ID, "s3-secret-plaintext")

	cfg := &domain.WorkspaceStorageConfig{
		WorkspaceID:     ws.ID,
		Driver:          "s3",
		Endpoint:        "https://s3.example.com",
		Region:          "us-east-1",
		Bucket:          "onclaw-blobs",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: envelope,
		UsePathStyle:    true,
	}
	if err := s.WorkspaceStorage().Upsert(ctx, cfg); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if cfg.UpdatedAt.IsZero() {
		t.Fatal("expected updated_at to be set by the store")
	}

	got, err := s.WorkspaceStorage().Get(ctx, ws.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.WorkspaceID != ws.ID ||
		got.Driver != "s3" ||
		got.Endpoint != "https://s3.example.com" ||
		got.Region != "us-east-1" ||
		got.Bucket != "onclaw-blobs" ||
		got.AccessKeyID != "AKIAEXAMPLE" ||
		got.SecretAccessKey != envelope ||
		!got.UsePathStyle {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
	if !got.UpdatedAt.Equal(cfg.UpdatedAt) {
		t.Errorf("updated_at = %v, want the stored %v", got.UpdatedAt, cfg.UpdatedAt)
	}

	// The envelope must unseal (with the same key and AAD) to the original
	// plaintext — the store carried it opaquely without touching it.
	plaintext, err := secrets.Decrypt(key, []byte(ws.ID), got.SecretAccessKey)
	if err != nil {
		t.Fatalf("unseal returned envelope: %v", err)
	}
	if string(plaintext) != "s3-secret-plaintext" {
		t.Errorf("unsealed secret = %q, want the original plaintext", plaintext)
	}
}

// TestIntegration_WorkspaceStorageStore_UpsertReplaces covers PUT semantics:
// a second upsert rewrites every column, leaves exactly one row, and the
// changed values are what a later Get returns.
func TestIntegration_WorkspaceStorageStore_UpsertReplaces(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)
	ws := &domain.Workspace{Slug: "wsstorage-replace", Name: "Replace"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	envelope, _ := sealTestSecret(t, ws.ID, "first-secret")

	first := &domain.WorkspaceStorageConfig{
		WorkspaceID:     ws.ID,
		Driver:          "s3",
		Endpoint:        "https://old.example.com",
		Region:          "eu-west-1",
		Bucket:          "old-bucket",
		AccessKeyID:     "OLDKEY",
		SecretAccessKey: envelope,
		UsePathStyle:    true,
	}
	if err := s.WorkspaceStorage().Upsert(ctx, first); err != nil {
		t.Fatalf("upsert first: %v", err)
	}

	second := &domain.WorkspaceStorageConfig{
		WorkspaceID:     ws.ID,
		Driver:          "local",
		Endpoint:        "",
		Region:          "",
		Bucket:          "",
		AccessKeyID:     "",
		SecretAccessKey: "",
		UsePathStyle:    false,
	}
	if err := s.WorkspaceStorage().Upsert(ctx, second); err != nil {
		t.Fatalf("upsert second: %v", err)
	}

	got, err := s.WorkspaceStorage().Get(ctx, ws.ID)
	if err != nil {
		t.Fatalf("get after replace: %v", err)
	}
	if got.Driver != "local" ||
		got.Endpoint != "" ||
		got.Region != "" ||
		got.Bucket != "" ||
		got.AccessKeyID != "" ||
		got.SecretAccessKey != "" ||
		got.UsePathStyle {
		t.Errorf("expected wholesale replacement to local/empty, got %+v", got)
	}

	// Exactly one row for the workspace.
	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("connect for row count: %v", err)
	}
	defer conn.Close(ctx)
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM workspace_storage WHERE workspace_id = $1`, ws.ID).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after re-upsert, got %d", count)
	}
}

// TestIntegration_WorkspaceStorageStore_SecretNotPlaintextAtRest covers the
// at-rest posture: the stored column holds the sealed envelope, never the
// plaintext the caller protected, and the round-trip returns the exact
// envelope (encryption is the caller's job — the store is a passive carrier).
func TestIntegration_WorkspaceStorageStore_SecretNotPlaintextAtRest(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)
	ws := &domain.Workspace{Slug: "wsstorage-atrest", Name: "AtRest"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	const plaintext = "super-secret-access-key"
	envelope, key := sealTestSecret(t, ws.ID, plaintext)

	cfg := &domain.WorkspaceStorageConfig{
		WorkspaceID:     ws.ID,
		Driver:          "s3",
		SecretAccessKey: envelope,
	}
	if err := s.WorkspaceStorage().Upsert(ctx, cfg); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("connect for at-rest probe: %v", err)
	}
	defer conn.Close(ctx)

	var atRest string
	if err := conn.QueryRow(ctx, `SELECT secret_access_key FROM workspace_storage WHERE workspace_id = $1`, ws.ID).Scan(&atRest); err != nil {
		t.Fatalf("read secret at rest: %v", err)
	}
	if atRest == plaintext {
		t.Error("secret at rest is the plaintext — the caller-sealed envelope must be stored, never the raw secret")
	}
	if atRest != envelope {
		t.Errorf("secret at rest = %q, want the exact sealed envelope %q", atRest, envelope)
	}

	got, err := s.WorkspaceStorage().Get(ctx, ws.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SecretAccessKey != envelope {
		t.Errorf("round-trip secret = %q, want the exact sealed envelope", got.SecretAccessKey)
	}
	unsealed, err := secrets.Decrypt(key, []byte(ws.ID), got.SecretAccessKey)
	if err != nil || string(unsealed) != plaintext {
		t.Errorf("unsealed round-trip = %q (err %v), want %q", unsealed, err, plaintext)
	}
}
