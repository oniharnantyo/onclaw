package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/attachments"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// multipartOverheadMargin pads the body cap for multipart boundaries and
// form metadata around the largest accepted payload (drop lane, 50 MB).
const multipartOverheadMargin = 1 << 20

// sniffHeadBytes is the classification head the handler reads ahead of the
// stream: ≥512 bytes for the magic-byte sniff (design D4), a larger window
// so the best-effort PDF page count sees the object headers.
const sniffHeadBytes = 512 << 10

// attachmentsHandlers handles workspace-scoped chat attachment uploads
// (attachments design D1): bytes cross the wire once, on their own endpoint,
// and the returned capability URL is the attachment's wire token.
type attachmentsHandlers struct {
	attachments store.AttachmentStore
	wsStorage   *resolver.WorkspaceStorage
}

// NewAttachmentsHandlers creates a new attachmentsHandlers instance with
// injected dependencies.
func NewAttachmentsHandlers(attachments store.AttachmentStore, wsStorage *resolver.WorkspaceStorage) *attachmentsHandlers {
	return &attachmentsHandlers{
		attachments: attachments,
		wsStorage:   wsStorage,
	}
}

// Upload accepts a multipart `file` field, classifies it into its lane, and
// stores the blob through the workspace's configured backend under a fresh
// capability key (task 2.2 / 3.5).
func (h *attachmentsHandlers) Upload(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, attachments.MaxDropBytes+multipartOverheadMargin)

	file, fh, err := c.Request.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) || strings.Contains(err.Error(), "request body too large") {
			RespondError(c, &attachments.ErrTooLarge{Type: "upload", Cap: attachments.MaxDropBytes})
			return
		}
		RespondError(c, fmt.Errorf("%w: multipart field %q is required", domain.ErrInvalid, "file"))
		return
	}
	defer file.Close()

	// Read the classification head, then re-attach it ahead of the remaining
	// stream so the blob write sees the complete file.
	head := make([]byte, sniffHeadBytes)
	n, _ := io.ReadFull(file, head)
	head = head[:n]

	mime, lane, err := attachments.Classify(fh.Filename, fh.Size, head)
	if err != nil {
		RespondError(c, err)
		return
	}

	st, err := h.wsStorage.ForWorkspace(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	backend, err := h.wsStorage.DriverName(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	key, err := storage.NewKey()
	if err != nil {
		RespondError(c, err)
		return
	}

	if err := st.Put(c.Request.Context(), key, io.MultiReader(bytes.NewReader(head), file), fh.Size, mime); err != nil {
		// Storage failures (S3 outage, disk full) surface as the standard
		// 5xx envelope — the generic internal message keeps driver details
		// out of the client response (spec: upload fails with a storage error).
		RespondError(c, err)
		return
	}

	att := &domain.Attachment{
		WorkspaceID: ws.ID,
		StorageKey:  key,
		Backend:     backend,
		Name:        fh.Filename,
		MimeType:    mime,
		Size:        fh.Size,
		Lane:        lane,
		CreatedBy:   user.ID,
	}
	if err := h.attachments.Create(c.Request.Context(), att); err != nil {
		RespondError(c, err)
		return
	}

	RespondCreated(c, gin.H{
		"id":   att.ID,
		"name": att.Name,
		"mime": mime,
		"size": att.Size,
		"url":  st.URL(key),
	})
}
