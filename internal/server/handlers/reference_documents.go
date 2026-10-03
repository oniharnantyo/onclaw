package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/authz"
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
	// authz is the permission authorizer (fix-role-permission-audit D6):
	// document mutations are ownership-scoped — the uploader mutates their
	// own documents, other members' documents need workspace.write — and the
	// listing lens resolves through the same evaluation point as the
	// middleware.
	authz authz.Authorizer
}

// NewReferenceDocumentsHandlers creates a new referenceDocumentsHandlers
// instance with the injected references service, registry store, and
// authorizer.
func NewReferenceDocumentsHandlers(references *references.Service, documents store.ReferenceDocumentStore, authz authz.Authorizer) *referenceDocumentsHandlers {
	return &referenceDocumentsHandlers{references: references, documents: documents, authz: authz}
}

// callerHolds reports whether the request's resolved role holds the given
// workspace permission through the authorizer port (fix-role-permission-audit
// D1: in-handler checks ride the same evaluation point as the middleware).
func (h *referenceDocumentsHandlers) callerHolds(c *gin.Context, permission string) (bool, error) {
	ws := MustCurrentWorkspace(c)
	role := MustCurrentRole(c)
	return h.authz.Enforce(c.Request.Context(), role.ID, ws.ID, permission)
}

// canMutateDocument evaluates the ownership-scoped mutation rule
// (fix-role-permission-audit D6): the document's uploader may always mutate
// it; anyone else needs workspace.write. The caller has already resolved the
// document.
func (h *referenceDocumentsHandlers) canMutateDocument(c *gin.Context, doc domain.ReferenceDocument) (bool, error) {
	user := MustCurrentUser(c)
	if doc.UploadedBy == user.ID {
		return true, nil
	}
	return h.callerHolds(c, domain.WorkspaceWrite)
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
// own endpoints — and the scope tier is left as stored. Ownership-scoped
// (fix-role-permission-audit D6): the uploader or a workspace.write holder.
func (h *referenceDocumentsHandlers) PutContent(c *gin.Context) {
	in, ok := h.readUpload(c)
	if !ok {
		return
	}
	ws := MustCurrentWorkspace(c)

	current, err := h.references.Get(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	allowed, err := h.canMutateDocument(c, current)
	if err != nil {
		RespondError(c, err)
		return
	}
	if !allowed {
		RespondError(c, fmt.Errorf("%w: replacing another member's document requires workspace.write", domain.ErrForbidden))
		return
	}

	doc, err := h.references.Replace(c.Request.Context(), ws.ID, current.ID, references.UploadInput{
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
// other member-readable collections, filtered through the tiered-visibility
// lens (add-reference-documents D7 + fix-role-permission-audit D6):
// reference_documents.promote holders see every workspace document; everyone
// else sees their own uploads plus documents attached to agents or channels
// they can configure.
func (h *referenceDocumentsHandlers) List(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	role := MustCurrentRole(c)
	ctx := c.Request.Context()

	seeAll, err := h.authz.Enforce(ctx, role.ID, ws.ID, domain.PermissionReferenceDocumentsPromote)
	if err != nil {
		RespondError(c, err)
		return
	}
	canConfigureAgents, err := h.authz.Enforce(ctx, role.ID, ws.ID, domain.AgentsWrite)
	if err != nil {
		RespondError(c, err)
		return
	}
	canConfigureChannels, err := h.authz.Enforce(ctx, role.ID, ws.ID, domain.ChannelsWrite)
	if err != nil {
		RespondError(c, err)
		return
	}

	var (
		docs []domain.ReferenceDocument
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
		if !seeAll && !h.docVisibleTo(doc, user.ID, canConfigureAgents, canConfigureChannels) {
			continue
		}
		views = append(views, h.documentViewOf(doc))
	}
	RespondOK(c, gin.H{"documents": views})
}

// docVisibleTo is the member listing-lens predicate: own uploads always
// pass; otherwise the document passes when the viewer can configure a
// surface it is attached to (agents.write for the attached agents,
// channels.write for the attached channels).
func (h *referenceDocumentsHandlers) docVisibleTo(doc domain.ReferenceDocument, userID string, canConfigureAgents, canConfigureChannels bool) bool {
	if doc.UploadedBy == userID {
		return true
	}
	if canConfigureAgents && len(doc.AgentIDs) > 0 {
		return true
	}
	return canConfigureChannels && len(doc.ChannelIDs) > 0
}

// patchDocumentRequest is the metadata-edit payload; the pointer fields
// distinguish "leave as stored" from an explicit clear/blank.
type patchDocumentRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// Patch rewrites the editable bibliographic fields (name, description).
// Absent fields stay as stored; a submitted blank name is invalid.
// Ownership-scoped (fix-role-permission-audit D6): the uploader or a
// workspace.write holder.
func (h *referenceDocumentsHandlers) Patch(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	ctx := c.Request.Context()

	current, err := h.references.Get(ctx, ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}

	allowed, err := h.canMutateDocument(c, current)
	if err != nil {
		RespondError(c, err)
		return
	}
	if !allowed {
		RespondError(c, fmt.Errorf("%w: editing another member's document requires workspace.write", domain.ErrForbidden))
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
// domain.ErrNotFound, indistinguishable (tenancy). Gated to agents.write
// (fix-role-permission-audit D6): attaching a document to agents is agent
// configuration, reserved for agents.write holders.
func (h *referenceDocumentsHandlers) PutAgents(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	allowed, err := h.callerHolds(c, domain.AgentsWrite)
	if err != nil {
		RespondError(c, err)
		return
	}
	if !allowed {
		RespondError(c, fmt.Errorf("%w: attaching documents to agents requires agents.write", domain.ErrForbidden))
		return
	}

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
// workspace FK parity as PutAgents. Gated to channels.write
// (fix-role-permission-audit D6): attaching a document to channels is channel
// configuration.
func (h *referenceDocumentsHandlers) PutChannels(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	allowed, err := h.callerHolds(c, domain.ChannelsWrite)
	if err != nil {
		RespondError(c, err)
		return
	}
	if !allowed {
		RespondError(c, fmt.Errorf("%w: attaching documents to channels requires channels.write", domain.ErrForbidden))
		return
	}

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
// Ownership-scoped (fix-role-permission-audit D6): the uploader or a
// workspace.write holder.
func (h *referenceDocumentsHandlers) Delete(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	current, err := h.references.Get(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	allowed, err := h.canMutateDocument(c, current)
	if err != nil {
		RespondError(c, err)
		return
	}
	if !allowed {
		RespondError(c, fmt.Errorf("%w: deleting another member's document requires workspace.write", domain.ErrForbidden))
		return
	}

	if err := h.references.Delete(c.Request.Context(), ws.ID, current.ID); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}
