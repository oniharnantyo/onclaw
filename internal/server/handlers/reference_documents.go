package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/references"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// referenceDocumentsHandlers handles the workspace reference-document library
// surface (add-reference-documents tasks 6.1–6.4): multipart upload with
// magic-byte classification, the list lenses, metadata edits, set-complete
// attach editing, admin-gated promotion, content replacement, and delete. The
// service owns classify/store/index; the registry store backs the read lenses;
// the handler owns the wire shapes and the error envelopes the attachment
// upload contract pins (the reject body carries the classification guidance,
// oversize rides the 413 payload mapping).
type referenceDocumentsHandlers struct {
	references *references.Service
	documents  store.ReferenceDocumentStore
}

// NewReferenceDocumentsHandlers creates a new referenceDocumentsHandlers
// instance with the injected references service and registry store.
func NewReferenceDocumentsHandlers(references *references.Service, documents store.ReferenceDocumentStore) *referenceDocumentsHandlers {
	return &referenceDocumentsHandlers{references: references, documents: documents}
}

// documentView is the pinned wire shape of one reference document
// (add-reference-documents 6.1): camelCase, the capability URL as the wire
// token, and the attach lists as id arrays. The domain struct's own tags stay
// snake_case (the row shape) — the view, not the row, is the API contract.
type documentView struct {
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
	CreatedAt   time.Time `json:"createdAt"`
}

// documentViewOf projects one row onto the wire shape. The attach lists are
// never null on the wire — an empty attach set is an empty array.
func (h *referenceDocumentsHandlers) documentViewOf(doc domain.ReferenceDocument) documentView {
	agents := doc.AgentIDs
	if agents == nil {
		agents = []string{}
	}
	channels := doc.ChannelIDs
	if channels == nil {
		channels = []string{}
	}
	return documentView{
		ID:          doc.ID,
		Name:        doc.Name,
		Description: doc.Description,
		Mime:        doc.MimeType,
		Size:        doc.SizeBytes,
		URL:         h.references.URL(doc),
		IndexStatus: doc.IndexStatus,
		Scope:       doc.Scope,
		PageCount:   doc.PageCount,
		Agents:      agents,
		Channels:    channels,
		CreatedAt:   doc.CreatedAt,
	}
}

// Upload accepts a multipart `file` field plus the optional name, description,
// and repeated agentIds / channelIds attach lists, classifies the bytes by
// magic number, and stores the document through the references service. The
// response is 201 with the pinned document shape; classification rejections
// carry their guidance text in the standard invalid envelope and oversize
// rides the 413 payload-too-large mapping.
func (h *referenceDocumentsHandlers) Upload(c *gin.Context) {
	in, ok := h.readUpload(c)
	if !ok {
		return
	}
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	doc, err := h.references.Upload(c.Request.Context(), ws.ID, user.ID, *in)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondCreated(c, h.documentViewOf(doc))
}

// PutContent replaces one document's bytes: the blob is swapped and the
// section index rebuilt from the new file in one service step. Only the
// multipart `file` field is consumed — metadata and attach edits ride their
// own endpoints — and the scope tier is left as stored.
func (h *referenceDocumentsHandlers) PutContent(c *gin.Context) {
	in, ok := h.readUpload(c)
	if !ok {
		return
	}
	ws := MustCurrentWorkspace(c)

	doc, err := h.references.Replace(c.Request.Context(), ws.ID, c.Param("id"), references.UploadInput{
		Filename: in.Filename,
		Data:     in.Data,
	})
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, h.documentViewOf(doc))
}

// readUpload parses the shared multipart shape of Upload and PutContent: the
// required `file` field (body-capped like the attachment upload) with its
// sniffable bytes, plus the optional metadata and attach-list fields the
// replace flow ignores. The ok return is false only when the error response
// has already been written.
func (h *referenceDocumentsHandlers) readUpload(c *gin.Context) (*references.UploadInput, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, references.MaxReferenceFileBytes+multipartOverheadMargin)

	file, fh, err := c.Request.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			RespondError(c, fmt.Errorf("%w: reference document upload exceeds the %d byte body cap",
				domain.ErrPayloadTooLarge, references.MaxReferenceFileBytes))
			return nil, false
		}
		RespondError(c, fmt.Errorf("%w: multipart field %q is required", domain.ErrInvalid, "file"))
		return nil, false
	}
	defer file.Close()

	// The body cap bounds this read: the service classifies, stores, and
	// indexes the bytes in memory (deterministic CPU ingest, no stream state).
	data, err := io.ReadAll(file)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			RespondError(c, fmt.Errorf("%w: reference document upload exceeds the %d byte body cap",
				domain.ErrPayloadTooLarge, references.MaxReferenceFileBytes))
			return nil, false
		}
		RespondError(c, err)
		return nil, false
	}

	in := &references.UploadInput{
		Filename:    fh.Filename,
		Name:        c.Request.FormValue("name"),
		Description: c.Request.FormValue("description"),
		Data:        data,
	}
	// The attach lists are repeated fields (agentIds=…&agentIds=…) — the wire
	// shape the documents pane sends. Absent lists stay nil and the upload
	// defaults to the attached scope with empty joins.
	if c.Request.MultipartForm != nil {
		in.AgentIDs = c.Request.MultipartForm.Value["agentIds"]
		in.ChannelIDs = c.Request.MultipartForm.Value["channelIds"]
	}
	return in, true
}

// List returns the workspace's documents, or one lens when the request names
// an agent or channel (?agent= / ?channel= — the visibility lenses the agent
// config modal and channel settings render). Reads ride membership like the
// other member-readable collections.
func (h *referenceDocumentsHandlers) List(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	ctx := c.Request.Context()

	var (
		docs []domain.ReferenceDocument
		err  error
	)
	switch {
	case c.Query("agent") != "":
		docs, err = h.documents.ListByAgent(ctx, ws.ID, c.Query("agent"))
	case c.Query("channel") != "":
		docs, err = h.documents.ListByChannel(ctx, ws.ID, c.Query("channel"))
	default:
		docs, err = h.documents.List(ctx, ws.ID)
	}
	if err != nil {
		RespondError(c, err)
		return
	}

	views := make([]documentView, 0, len(docs))
	for _, doc := range docs {
		views = append(views, h.documentViewOf(doc))
	}
	RespondOK(c, gin.H{"documents": views})
}

// patchDocumentRequest is the metadata-edit payload; the pointer fields
// distinguish "leave as stored" from an explicit clear/blank.
type patchDocumentRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// Patch rewrites the editable bibliographic fields (name, description).
// Absent fields stay as stored; a submitted blank name is invalid.
func (h *referenceDocumentsHandlers) Patch(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	ctx := c.Request.Context()

	current, err := h.references.Get(ctx, ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}

	var req patchDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	name, description := current.Name, current.Description
	if req.Name != nil {
		name = *req.Name
	}
	if req.Description != nil {
		description = *req.Description
	}

	doc, err := h.references.UpdateMeta(ctx, ws.ID, current.ID, name, description)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, h.documentViewOf(doc))
}

// setDocumentAgentsRequest is the agent attach payload.
type setDocumentAgentsRequest struct {
	AgentIDs []string `json:"agentIds"`
}

// PutAgents replaces the document's attached-agent set (set-complete). Every
// id must exist in the workspace — unknown or foreign ids ride the store's
// domain.ErrNotFound, indistinguishable (tenancy).
func (h *referenceDocumentsHandlers) PutAgents(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req setDocumentAgentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	doc, err := h.references.SetAgents(c.Request.Context(), ws.ID, c.Param("id"), req.AgentIDs)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, h.documentViewOf(doc))
}

// setDocumentChannelsRequest is the channel attach payload.
type setDocumentChannelsRequest struct {
	ChannelIDs []string `json:"channelIds"`
}

// PutChannels replaces the document's attached-channel set, with the same
// workspace FK parity as PutAgents.
func (h *referenceDocumentsHandlers) PutChannels(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req setDocumentChannelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	doc, err := h.references.SetChannels(c.Request.Context(), ws.ID, c.Param("id"), req.ChannelIDs)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, h.documentViewOf(doc))
}

// Promote flips the document to the workspace tier (visible to every agent).
// The route is gated behind reference_documents.promote — the permission
// catalog's admin-only entry — at registration; the handler is the flip.
func (h *referenceDocumentsHandlers) Promote(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	doc, err := h.references.Promote(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, h.documentViewOf(doc))
}

// Demote flips the document back to the attached tier, behind the same
// permission gate as Promote.
func (h *referenceDocumentsHandlers) Demote(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	doc, err := h.references.Demote(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, h.documentViewOf(doc))
}

// Delete removes the document, its section index, and the stored blob. The
// response mirrors the connections disconnect shape: 204, no body.
func (h *referenceDocumentsHandlers) Delete(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	if err := h.references.Delete(c.Request.Context(), ws.ID, c.Param("id")); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}
