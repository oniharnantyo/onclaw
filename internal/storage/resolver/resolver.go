// Package resolver maps a workspace's stored storage configuration to a
// driver instance (attachments design D15/D16): the workspace's configured
// backend when present, the instance-default local storage when not, and —
// for reads — the backend recorded on the attachment row, so blobs written
// under a previous configuration stay readable after a switch.
package resolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// WorkspaceStorage resolves driver instances for workspace-scoped blob
// storage. Write paths resolve through ForWorkspace, read paths for existing
// blobs through ForBackend using the backend recorded on the attachment row.
type WorkspaceStorage struct {
	instanceDefault storage.Storage
	configs         store.WorkspaceStorageStore
	attachments     store.AttachmentStore
	encKey          []byte
	// dataDir is the instance data root behind the default local driver; the
	// default itself is injected, so this only records where local data lives.
	dataDir string

	mu    sync.Mutex
	cache map[string]storage.Storage // key: workspaceID + fingerprint(config incl. sealed envelope)
}

// The runner consumes the resolver through the narrow internal/agents
// AttachmentBlobs interface (attachments design D5/D17); asserted
// structurally so a signature drift fails this build, not the runner's
// wiring.
var _ interface {
	OpenAttachment(ctx context.Context, workspaceID, attachmentID string) ([]byte, error)
	AttachmentURL(ctx context.Context, workspaceID, attachmentID string) (string, error)
	// PublishCreatedDocument backs the document.create tool's delivery port
	// (add-document-create-tool D6) through the agents.DocumentPublisher seam.
	PublishCreatedDocument(ctx context.Context, workspaceID, name, sourcePath string) (capabilityURL string, err error)
} = (*WorkspaceStorage)(nil)

// New creates a new WorkspaceStorage resolver. The instance default is the
// driver the composition root opened from server configuration (local in the
// stock assembly); encKey is the instance AES-256 key unsealing stored
// workspace secrets with; dataDir is the data root behind the local default.
func New(instanceDefault storage.Storage, configs store.WorkspaceStorageStore, attachments store.AttachmentStore, encKey []byte, dataDir string) *WorkspaceStorage {
	return &WorkspaceStorage{
		instanceDefault: instanceDefault,
		configs:         configs,
		attachments:     attachments,
		encKey:          encKey,
		dataDir:         dataDir,
		cache:           make(map[string]storage.Storage),
	}
}

// ForWorkspace resolves the driver new writes go through: the workspace's
// configured backend when present, the instance default when not (design D15).
func (w *WorkspaceStorage) ForWorkspace(ctx context.Context, workspaceID string) (storage.Storage, error) {
	cfg, err := w.configs.Get(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// No configuration: the instance default applies (D16).
			return w.instanceDefault, nil
		}
		return nil, err
	}

	switch cfg.Driver {
	case "local":
		// Local is zero-config — the instance default already is it (D15).
		return w.instanceDefault, nil
	case "s3":
		return w.s3Storage(workspaceID, cfg)
	default:
		return nil, fmt.Errorf("%w: workspace %s configures unknown storage driver %q", domain.ErrInvalid, workspaceID, cfg.Driver)
	}
}

// DriverName names the backend ForWorkspace resolves for NEW writes: the
// value recorded on the attachment row alongside the capability key (design
// D16), so reads can later return to it through ForBackend even after the
// workspace switches backends.
func (w *WorkspaceStorage) DriverName(ctx context.Context, workspaceID string) (string, error) {
	cfg, err := w.configs.Get(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "local", nil
		}
		return "", err
	}
	switch cfg.Driver {
	case "local", "s3":
		return cfg.Driver, nil
	default:
		return "", fmt.Errorf("%w: workspace %s configures unknown storage driver %q", domain.ErrInvalid, workspaceID, cfg.Driver)
	}
}

// ForBackend resolves the driver holding an existing attachment's bytes: the
// backend recorded on the attachment row, not the workspace's current setting
// (design D16). The row carries the backend only — no config snapshot — so
// "s3" builds from the workspace's current stored s3 fields.
func (w *WorkspaceStorage) ForBackend(ctx context.Context, workspaceID, backend string) (storage.Storage, error) {
	switch backend {
	case "local":
		return w.instanceDefault, nil
	case "s3":
		cfg, err := w.configs.Get(ctx, workspaceID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("%w: workspace %s has no storage configuration for recorded backend %q", domain.ErrNotFound, workspaceID, backend)
			}
			return nil, err
		}
		return w.s3Storage(workspaceID, cfg)
	default:
		return nil, fmt.Errorf("%w: unknown storage backend %q", domain.ErrInvalid, backend)
	}
}

// OpenAttachment resolves an attachment record to its bytes through the
// backend recorded on the row (design D16). Foreign-workspace ids are
// indistinguishable from unknown ones (domain.ErrNotFound, tenancy).
func (w *WorkspaceStorage) OpenAttachment(ctx context.Context, workspaceID, attachmentID string) ([]byte, error) {
	att, err := w.attachments.ByID(ctx, workspaceID, attachmentID)
	if err != nil {
		return nil, err
	}

	backend, err := w.ForBackend(ctx, workspaceID, att.Backend)
	if err != nil {
		return nil, err
	}

	f, err := backend.Open(ctx, att.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("failed to open attachment %s: %w", att.ID, err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read attachment %s: %w", att.ID, err)
	}
	return data, nil
}

// AttachmentURL resolves an attachment record to its capability URL: the
// recorded backend's URL over the row's storage key (design D1 — the
// capability URL is the transcript token and the demoted reference form).
// Foreign-workspace ids are indistinguishable from unknown ones
// (domain.ErrNotFound, tenancy).
func (w *WorkspaceStorage) AttachmentURL(ctx context.Context, workspaceID, attachmentID string) (string, error) {
	att, err := w.attachments.ByID(ctx, workspaceID, attachmentID)
	if err != nil {
		return "", err
	}

	backend, err := w.ForBackend(ctx, workspaceID, att.Backend)
	if err != nil {
		return "", err
	}
	return backend.URL(att.StorageKey), nil
}

// createdDocumentMimeTypes maps the document.create output formats to their
// wire mime types (add-document-create-tool design.md D6). Anything else is
// generic octet-stream — the capability URL serves the recorded bytes either way.
var createdDocumentMimeTypes = map[string]string{
	".pdf":  "application/pdf",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

// PublishCreatedDocument copies an agent-created document into the
// workspace's configured backend under a fresh capability key and returns its
// capability URL (add-document-create-tool design.md D6). The blob is
// recorded as an attachment row so the capability serving path resolves the
// backend recorded at write time (design D16) — the same serving path chat
// attachments already use, including after a workspace switches backends.
func (w *WorkspaceStorage) PublishCreatedDocument(ctx context.Context, workspaceID, name, sourcePath string) (string, error) {
	f, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("open created document %q: %w", name, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat created document %q: %w", name, err)
	}

	st, err := w.ForWorkspace(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	backend, err := w.DriverName(ctx, workspaceID)
	if err != nil {
		return "", err
	}

	key, err := storage.NewKey()
	if err != nil {
		return "", err
	}

	mime := createdDocumentMimeTypes[strings.ToLower(filepath.Ext(name))]
	if mime == "" {
		mime = "application/octet-stream"
	}
	if err := st.Put(ctx, key, f, info.Size(), mime); err != nil {
		return "", fmt.Errorf("store created document %q: %w", name, err)
	}

	att := &domain.Attachment{
		WorkspaceID: workspaceID,
		StorageKey:  key,
		Backend:     backend,
		Name:        name,
		MimeType:    mime,
		Size:        info.Size(),
		Lane:        domain.AttachmentLaneDrop,
	}
	if err := w.attachments.Create(ctx, att); err != nil {
		return "", fmt.Errorf("record created document %q: %w", name, err)
	}
	return st.URL(key), nil
}

// s3Storage returns the cached s3 driver instance for the workspace's stored
// configuration, unsealing the secret and building the driver on a cache
// miss (design D15: driver instances are cached per workspace+config).
func (w *WorkspaceStorage) s3Storage(workspaceID string, cfg *domain.WorkspaceStorageConfig) (storage.Storage, error) {
	cacheKey := workspaceID + "\x00" + fingerprint(cfg)

	w.mu.Lock()
	defer w.mu.Unlock()

	if s, ok := w.cache[cacheKey]; ok {
		return s, nil
	}

	plaintext, err := secrets.Decrypt(w.encKey, []byte(workspaceID), cfg.SecretAccessKey)
	if err != nil {
		// Name the workspace, never the secret material.
		return nil, fmt.Errorf("workspace %s: failed to unseal storage secret: %w", workspaceID, err)
	}

	s, err := storage.Open("s3", storage.StorageConfig{
		Driver:       "s3",
		Endpoint:     cfg.Endpoint,
		Region:       cfg.Region,
		Bucket:       cfg.Bucket,
		AccessKey:    cfg.AccessKeyID,
		SecretKey:    string(plaintext),
		UsePathStyle: cfg.UsePathStyle,
		BaseURL:      "/api/v1/files/",
	})
	if err != nil {
		return nil, fmt.Errorf("workspace %s: failed to open s3 storage driver: %w", workspaceID, err)
	}
	w.cache[cacheKey] = s
	return s, nil
}

// fingerprint hashes the full stored configuration — including the sealed
// envelope — so any change (including a re-sealed secret) yields a fresh
// cache entry and stale driver instances invalidate naturally.
func fingerprint(cfg *domain.WorkspaceStorageConfig) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%t",
		cfg.Driver, cfg.Endpoint, cfg.Region, cfg.Bucket, cfg.AccessKeyID, cfg.SecretAccessKey, cfg.UsePathStyle)
	return hex.EncodeToString(h.Sum(nil))
}
