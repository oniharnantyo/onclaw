package store

import "context"

// AttachmentStore manages the attachment upload registry: one row per stored
// upload, keyed by the unguessable capability key (design D5). The rows carry
// the transcript metadata; the bytes live on disk under the attachments
// directory, addressed by the same key.
type AttachmentStore interface {
	// Put stores one upload's metadata row. The key is assigned by the
	// caller (the attachments storage layer generates it) and must be
	// unique; re-putting a key replaces the row.
	Put(ctx context.Context, att Attachment) error
	// ByKey returns the upload registered under the capability key;
	// ErrNotFound when the key is unknown.
	ByKey(ctx context.Context, key string) (*Attachment, error)
}
