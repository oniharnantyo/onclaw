package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// attachmentStore implements storeport.AttachmentStore for PostgreSQL
// (add-chat-attachments D3). Rows carry the capability key and the backend
// holding the blob bytes; the bytes themselves never live in the database.
// ByID is workspace-scoped so a foreign attachment is indistinguishable from
// an unknown one; ByStorageKey is deliberately global — capability keys are
// 128-bit random bearer tokens and are the wire token for serving.
type attachmentStore struct {
	db Executor
}

// NewAttachmentStore creates a new AttachmentStore with the given database executor.
func NewAttachmentStore(db Executor) storeport.AttachmentStore {
	return &attachmentStore{db: db}
}

const attachmentColumns = `
	id, workspace_id, storage_key, backend, name, mime, size, lane,
	created_by, created_at
`

func scanAttachment(row pgx.Row) (*domain.Attachment, error) {
	var a domain.Attachment
	err := row.Scan(
		&a.ID,
		&a.WorkspaceID,
		&a.StorageKey,
		&a.Backend,
		&a.Name,
		&a.MimeType,
		&a.Size,
		&a.Lane,
		&a.CreatedBy,
		&a.CreatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &a, nil
}

// Create inserts one attachment row. The id and CreatedAt are app-managed
// (store invariant 4): the caller may supply an id, otherwise one is
// generated; CreatedAt is stamped by the store and returned on the struct.
func (a *attachmentStore) Create(ctx context.Context, att *domain.Attachment) error {
	if att == nil || att.WorkspaceID == "" || att.StorageKey == "" {
		return domain.ErrInvalid
	}

	if att.ID == "" {
		att.ID = uuid.NewString()
	}
	if att.CreatedAt.IsZero() {
		att.CreatedAt = time.Now().UTC()
	}

	query := `
		INSERT INTO attachments (id, workspace_id, storage_key, backend, name, mime, size, lane, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := a.db.Exec(ctx, query,
		att.ID,
		att.WorkspaceID,
		att.StorageKey,
		att.Backend,
		att.Name,
		att.MimeType,
		att.Size,
		att.Lane,
		att.CreatedBy,
		att.CreatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

// ByID resolves an attachment scoped to its workspace. The workspace
// predicate is the tenancy boundary: an id belonging to another workspace
// matches nothing, so foreign and unknown are indistinguishable
// (domain.ErrNotFound — no existence leak).
func (a *attachmentStore) ByID(ctx context.Context, workspaceID, id string) (*domain.Attachment, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT ` + attachmentColumns + `
		FROM attachments
		WHERE workspace_id = $1 AND id = $2
	`
	return scanAttachment(a.db.QueryRow(ctx, query, workspaceID, id))
}

// ByStorageKey resolves an attachment by its capability key. This lookup is
// deliberately GLOBAL and unauthenticated: capability keys are 128-bit random
// bearer tokens (same posture as avatar files) and are the wire token for
// serving. Unknown keys return domain.ErrNotFound.
func (a *attachmentStore) ByStorageKey(ctx context.Context, key string) (*domain.Attachment, error) {
	if key == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT ` + attachmentColumns + `
		FROM attachments
		WHERE storage_key = $1
	`
	return scanAttachment(a.db.QueryRow(ctx, query, key))
}
