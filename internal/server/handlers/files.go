package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// fileHandlers handles capability file serving: avatar files from the
// instance storage and, for capability keys of stored rows, blobs streamed
// from the backend recorded on the row (attachments design D15/D16 and the
// reference-document branch, add-reference-documents 3.5 — capability URLs
// are onclaw-proxied and driver-invariant, and downloads are byte-identical).
type fileHandlers struct {
	storage     storage.Storage
	attachments store.AttachmentStore
	documents   store.ReferenceDocumentStore
	wsStorage   *resolver.WorkspaceStorage
}

// NewFileHandlers creates a new fileHandlers instance with injected dependencies.
func NewFileHandlers(strg storage.Storage, attachments store.AttachmentStore, documents store.ReferenceDocumentStore, wsStorage *resolver.WorkspaceStorage) *fileHandlers {
	return &fileHandlers{
		storage:     strg,
		attachments: attachments,
		documents:   documents,
		wsStorage:   wsStorage,
	}
}

// ServeFile serves stored capability files with immutable caching headers.
func (h *fileHandlers) ServeFile(c *gin.Context) {
	if h == nil || h.storage == nil {
		AbortNotFound(c, "file not found")
		return
	}

	key := c.Param("key")
	if key == "" {
		key = c.Param("name")
	}
	if key == "" {
		AbortNotFound(c, "file not found")
		return
	}

	// Attachment branch: capability keys are bearer tokens, so the lookup by
	// storage key is deliberately global and unauthenticated (AttachmentStore
	// contract); the blob streams from the backend recorded on the row.
	if att, err := h.attachments.ByStorageKey(c.Request.Context(), key); err == nil {
		h.serve(c, key, func() (storage.File, error) {
			st, err := h.wsStorage.ForBackend(c.Request.Context(), att.WorkspaceID, att.Backend)
			if err != nil {
				return nil, err
			}
			return st.Open(c.Request.Context(), key)
		})
		return
	}

	// Reference-document branch (add-reference-documents 3.5): the same
	// global bearer-token lookup over the reference registry, streaming from
	// the backend recorded on the document row so the download is the
	// uploaded file, byte-identical.
	if doc, err := h.documents.GetByStorageKey(c.Request.Context(), key); err == nil {
		h.serve(c, key, func() (storage.File, error) {
			st, err := h.wsStorage.ForBackend(c.Request.Context(), doc.WorkspaceID, doc.Backend)
			if err != nil {
				return nil, err
			}
			return st.Open(c.Request.Context(), key)
		})
		return
	}

	h.serve(c, key, func() (storage.File, error) {
		return h.storage.Open(c.Request.Context(), key)
	})
}

// serve resolves the file through open, then streams it with the shared
// capability-serving headers.
func (h *fileHandlers) serve(c *gin.Context, key string, open func() (storage.File, error)) {
	file, err := open()
	if err != nil {
		AbortNotFound(c, "file not found")
		return
	}
	defer file.Close()

	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("Content-Type", file.ContentType())

	http.ServeContent(c.Writer, c.Request, key, time.Time{}, file)
}
