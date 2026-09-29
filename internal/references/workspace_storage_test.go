package references_test

// Workspace-storage routing tests (route-reference-documents-through-
// workspace-storage tasks 2.1–3.3): blobs ride the workspace's CONFIGURED
// driver, rows record that driver's name, and every read of an existing blob
// dispatches on the row's RECORDED backend — so a mixed local/S3 library is
// the steady state, and a switch never orphans pre-switch rows.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/references"
)

// backendBytes loads stored bytes through the named scripted driver ("local"
// or "s3"); the error is the storage port's (domain.ErrNotFound when the
// blob is gone).
func backendBytes(t *testing.T, h harness, driver, key string) ([]byte, error) {
	t.Helper()
	stor := h.stor
	if driver == "s3" {
		stor = h.res.s3
	}
	f, err := stor.Open(context.Background(), key)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// TestUploadRidesConfiguredBackend (task 2.1): upload against a scripted
// two-driver resolver — the blob lands on the workspace-configured driver and
// the row records that driver's name.
func TestUploadRidesConfiguredBackend(t *testing.T) {
	h := newHarness(t, "ref-ws-upload")
	h.res.configure(h.wsID, "s3")

	doc := upload(t, h, references.UploadInput{
		Filename: "runbook.md", Data: []byte(mdRunbook), AgentIDs: []string{h.atlasID},
	})
	if doc.Backend != "s3" {
		t.Fatalf("Backend = %q, want s3 (the workspace's configured driver)", doc.Backend)
	}

	stored, err := backendBytes(t, h, "s3", doc.StorageKey)
	if err != nil {
		t.Fatalf("open stored blob on s3: %v", err)
	}
	if !bytes.Equal(stored, []byte(mdRunbook)) {
		t.Errorf("stored blob differs from the uploaded bytes")
	}
	// The instance-default local driver never saw the blob.
	if _, err := backendBytes(t, h, "local", doc.StorageKey); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("blob leaked to the local driver: %v, want not-found", err)
	}
}

// TestReplaceMovesRecordedBackend (task 2.2): a replace lands the new blob on
// the workspace's CURRENT driver, rewrites the recorded backend, removes the
// superseded blob from its old backend, and leaves no stale sections.
func TestReplaceMovesRecordedBackend(t *testing.T) {
	h := newHarness(t, "ref-ws-replace")

	v1 := upload(t, h, references.UploadInput{
		Filename: "runbook.md", Data: []byte(mdRunbook), AgentIDs: []string{h.atlasID},
	})
	if v1.Backend != "local" {
		t.Fatalf("pre-switch Backend = %q, want local", v1.Backend)
	}
	oldKey := v1.StorageKey

	// The workspace switches to s3 between the upload and the replace.
	h.res.configure(h.wsID, "s3")

	got, err := h.svc.Replace(h.ctx, h.wsID, v1.ID, references.UploadInput{Filename: "runbook.md", Data: []byte(mdRunbookV2)})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got.Backend != "s3" {
		t.Errorf("Backend after replace = %q, want s3 (the workspace's current driver)", got.Backend)
	}
	if got.StorageKey == oldKey {
		t.Errorf("storage key unchanged after replace")
	}

	// The new blob is on s3; the superseded local blob was removed.
	stored, err := backendBytes(t, h, "s3", got.StorageKey)
	if err != nil {
		t.Fatalf("open replaced blob on s3: %v", err)
	}
	if !bytes.Equal(stored, []byte(mdRunbookV2)) {
		t.Errorf("replaced blob differs from the new bytes")
	}
	if _, err := backendBytes(t, h, "local", oldKey); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("superseded local blob survived: %v, want gone", err)
	}

	// No stale sections from the previous edition; the new content is
	// searchable.
	if hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "webhooks", 0); len(hits) != 0 {
		t.Errorf("stale sections survived replace: %+v", hits)
	}
	if hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "staging", 0); len(hits) != 1 || hits[0].DocumentID != v1.ID {
		t.Fatalf("staging hits = %+v, want the rebuilt Deployment section", hits)
	}
}

// TestRebuildIndexReadsThroughRecordedBackend (task 3.1): a row recording the
// second driver has its bytes read from that driver — the blob exists ONLY
// there, so a rebuild that dispatched on anything but the recorded backend
// would fail with not-found.
func TestRebuildIndexReadsThroughRecordedBackend(t *testing.T) {
	h := newHarness(t, "ref-ws-rebuild")
	h.res.configure(h.wsID, "s3")

	doc := upload(t, h, references.UploadInput{
		Filename: "runbook.md", Data: []byte(mdRunbook), AgentIDs: []string{h.atlasID},
	})
	if doc.Backend != "s3" {
		t.Fatalf("Backend = %q, want s3", doc.Backend)
	}

	// Simulate the failure-mid-rebuild state: the section index is gone while
	// the row and its s3 blob survive.
	if err := h.st.DocumentSections().DeleteForDocument(h.ctx, h.wsID, doc.ID); err != nil {
		t.Fatalf("wipe sections: %v", err)
	}
	if err := h.svc.RebuildIndex(h.ctx, h.wsID, doc.ID); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}
	hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "webhooks", 0)
	if len(hits) != 1 || hits[0].Heading != "Webhooks" {
		t.Fatalf("post-rebuild hits = %+v, want the rebuilt Webhooks section read back from s3", hits)
	}
}

// TestWriteMountMixedBackendsByteIdentical (task 3.2): a library holding a
// pre-switch local row and an s3 row mounts both files byte-identical — each
// document materializes through its recorded backend.
func TestWriteMountMixedBackendsByteIdentical(t *testing.T) {
	h := newHarness(t, "ref-ws-mount")

	localDoc := upload(t, h, references.UploadInput{
		Filename: "local.md", Data: []byte("# Local\nlocal content"), AgentIDs: []string{h.atlasID},
	})
	// The workspace switches to s3; the next upload lands there while the
	// earlier row keeps its recorded local backend.
	h.res.configure(h.wsID, "s3")
	s3Doc := upload(t, h, references.UploadInput{
		Filename: "cloud.md", Data: []byte("# Cloud\ncloud content"), AgentIDs: []string{h.atlasID},
	})
	if localDoc.Backend != "local" || s3Doc.Backend != "s3" {
		t.Fatalf("backends = %q/%q, want local/s3", localDoc.Backend, s3Doc.Backend)
	}

	dir := t.TempDir()
	if err := h.svc.WriteMount(h.ctx, dir, []domain.ReferenceDocument{localDoc, s3Doc}); err != nil {
		t.Fatalf("WriteMount: %v", err)
	}
	gotLocal, err := os.ReadFile(filepath.Join(dir, "local.md"))
	if err != nil {
		t.Fatalf("read mounted local.md: %v", err)
	}
	gotCloud, err := os.ReadFile(filepath.Join(dir, "cloud.md"))
	if err != nil {
		t.Fatalf("read mounted cloud.md: %v", err)
	}
	if !bytes.Equal(gotLocal, []byte("# Local\nlocal content")) {
		t.Errorf("mounted local.md differs from the stored blob")
	}
	if !bytes.Equal(gotCloud, []byte("# Cloud\ncloud content")) {
		t.Errorf("mounted cloud.md differs from the stored blob")
	}
}

// TestDeleteRemovesBlobFromRecordedBackend (task 3.3): deleting an
// s3-recorded row removes the blob from the second driver — even after the
// workspace has since switched back to local — and leaves the local driver's
// blobs untouched.
func TestDeleteRemovesBlobFromRecordedBackend(t *testing.T) {
	h := newHarness(t, "ref-ws-delete")

	localDoc := upload(t, h, references.UploadInput{
		Filename: "local.md", Data: []byte(mdRunbook), AgentIDs: []string{h.atlasID},
	})
	h.res.configure(h.wsID, "s3")
	s3Doc := upload(t, h, references.UploadInput{
		Filename: "cloud.md", Data: []byte(mdRunbookV2), AgentIDs: []string{h.atlasID},
	})
	// The workspace has since switched back to local: the delete must still
	// dispatch on the RECORDED backend, not the current configuration.
	h.res.configure(h.wsID, "local")

	if err := h.svc.Delete(h.ctx, h.wsID, s3Doc.ID); err != nil {
		t.Fatalf("delete s3-recorded row: %v", err)
	}
	if _, err := backendBytes(t, h, "s3", s3Doc.StorageKey); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("s3 blob survived delete: %v, want gone", err)
	}
	if _, err := backendBytes(t, h, "local", localDoc.StorageKey); err != nil {
		t.Errorf("local driver disturbed by the s3 delete: %v", err)
	}
}

// TestUnconfiguredWorkspaceKeepsLocalBackendEndToEnd (task 4.1): the
// existing-rows regression — an unconfigured workspace (resolver falls back
// to the instance default) keeps the backend = local behavior end to end:
// upload, download, search, mount, delete.
func TestUnconfiguredWorkspaceKeepsLocalBackendEndToEnd(t *testing.T) {
	h := newHarness(t, "ref-ws-unconfigured")

	// No scripted configuration at all: the resolver falls back to the
	// instance default and records "local" — the pre-change shape.
	doc := upload(t, h, references.UploadInput{
		Filename: "runbook.md", Data: []byte(mdRunbook), AgentIDs: []string{h.atlasID},
	})
	if doc.Backend != "local" {
		t.Fatalf("Backend = %q, want local for an unconfigured workspace", doc.Backend)
	}
	if got := h.svc.URL(doc); got != "/api/v1/files/"+doc.StorageKey {
		t.Errorf("URL = %q, want the local capability shape", got)
	}

	// Download: byte-identical through the recorded backend.
	stored, err := backendBytes(t, h, "local", doc.StorageKey)
	if err != nil {
		t.Fatalf("open stored blob: %v", err)
	}
	if !bytes.Equal(stored, []byte(mdRunbook)) {
		t.Errorf("stored blob differs from the uploaded bytes")
	}

	// Search rides the index built from the blob.
	if hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "signing", 0); len(hits) != 1 || hits[0].DocumentID != doc.ID {
		t.Fatalf("signing hits = %+v, want the runbook", hits)
	}

	// Mount materialization.
	dir := t.TempDir()
	if err := h.svc.WriteMount(h.ctx, dir, []domain.ReferenceDocument{doc}); err != nil {
		t.Fatalf("WriteMount: %v", err)
	}
	mounted, err := os.ReadFile(filepath.Join(dir, "runbook.md"))
	if err != nil {
		t.Fatalf("read mount file: %v", err)
	}
	if !bytes.Equal(mounted, []byte(mdRunbook)) {
		t.Errorf("mounted bytes differ from the uploaded bytes")
	}

	// Delete clears the row and the local blob.
	if err := h.svc.Delete(h.ctx, h.wsID, doc.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := backendBytes(t, h, "local", doc.StorageKey); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("blob survived delete: %v, want gone", err)
	}
}
