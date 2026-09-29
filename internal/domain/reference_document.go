package domain

import (
	"fmt"
	"strings"
	"time"
)

// Reference document scopes stored on reference_documents.scope
// (add-reference-documents D7). attached is the default tier: the document is
// visible only to the agents and channels it is attached to. workspace is the
// admin-promoted tier: visible to every agent in the workspace.
const (
	RefDocScopeAttached  = "attached"
	RefDocScopeWorkspace = "workspace" // promoted
)

// Reference document index statuses stored on reference_documents.index_status.
// processing is the upload-time default; the deterministic indexer flips it to
// ready, or to no_text_layer for scanned/image-only PDFs whose page-level
// sections carry no extractable text.
const (
	RefDocIndexReady       = "ready"
	RefDocIndexNoTextLayer = "no_text_layer"
	RefDocIndexProcessing  = "processing"
)

// Document section locator kinds (LocatorKind*) live in reference_locator.go
// (added by the sectioning wave); this file owns the document-level
// scope/index-status vocabularies.

// MaxReferenceDocumentNameChars bounds the display name (1..200).
const MaxReferenceDocumentNameChars = 200

// MaxReferenceDocumentDescriptionChars bounds the one-line description that
// feeds the compose-time manifest (D6).
const MaxReferenceDocumentDescriptionChars = 2000

// DocumentRunScope is the run-side half of the D7 visibility predicate: the
// agent serving the run, and — when the run executes inside a team room —
// that channel. Scheduled runs pass IsChannelSession=false with an empty
// ChannelID: the delivery target never widens visibility.
type DocumentRunScope struct {
	AgentID          string
	ChannelID        string
	IsChannelSession bool
}

// ReferenceDocument is one workspace library row (add-reference-documents):
// the as-is uploaded blob's capability key and backend, its bibliographic
// metadata, the tiered visibility scope, and the derived index status. The
// bytes themselves live in a storage-port driver — never in the database
// (D1: the blob is the single source of truth; the section index is
// rebuildable from it).
type ReferenceDocument struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mime"`
	SizeBytes   int64  `json:"size_bytes"`
	// StorageKey is the capability key; never serialized (same posture as
	// attachment storage keys).
	StorageKey string `json:"-"`
	// Backend is the storage driver holding the blob ("local" | "s3").
	Backend   string `json:"backend"`
	PageCount int    `json:"page_count"`
	// Scope is RefDocScopeAttached or RefDocScopeWorkspace (promoted).
	Scope       string `json:"scope"`
	IndexStatus string `json:"index_status"`
	// AgentIDs / ChannelIDs are the attached agents/channels — the join
	// tables hydrated onto reads. Empty on scope=workspace rows is allowed
	// but the predicate ignores them (promoted already returns true).
	AgentIDs   []string  `json:"agent_ids"`
	ChannelIDs []string  `json:"channel_ids"`
	UploadedBy string    `json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// VisibleTo is the D7 visibility predicate, evaluated identically at compose
// (manifest) and query time (search): the document is visible to a run iff it
// is promoted to the workspace, OR the run's agent is in the attached agents,
// OR the run is a channel session whose channel is in the attached channels.
// The scheduler rule falls out of the shape: scheduled runs pass
// IsChannelSession=false with an empty ChannelID, so a channel-tied document
// is never visible to them — delivery target does not widen scope.
func (d ReferenceDocument) VisibleTo(s DocumentRunScope) bool {
	if d.Scope == RefDocScopeWorkspace {
		return true
	}
	if s.AgentID != "" {
		for _, id := range d.AgentIDs {
			if id == s.AgentID {
				return true
			}
		}
	}
	if s.IsChannelSession && s.ChannelID != "" {
		for _, id := range d.ChannelIDs {
			if id == s.ChannelID {
				return true
			}
		}
	}
	return false
}

// DocumentSection is one indexed slice of a reference document (D2): a
// format-agnostic split of the converted markdown, carrying the type
// appropriate citation locator (D3). Level is the ATX heading level (1-3);
// 0 marks a synthetic preamble/whole-doc section. Workspace scoping rides on
// the store method arguments, not the struct.
type DocumentSection struct {
	ID          string `json:"id"`
	DocumentID  string `json:"document_id"`
	Heading     string `json:"heading"`
	Locator     string `json:"locator"`
	LocatorKind string `json:"locator_kind"`
	Level       int    `json:"level"`
	Ordinal     int    `json:"ordinal"`
	Body        string `json:"body"`
}

// DocumentSectionHit is one document.search result row (D4): the section that
// matched, joined to its document for the citation chip's name, plus a
// bounded snippet rendered around the match.
type DocumentSectionHit struct {
	DocumentID   string `json:"document_id"`
	DocumentName string `json:"document_name"`
	Heading      string `json:"heading"`
	Locator      string `json:"locator"`
	LocatorKind  string `json:"locator_kind"`
	Snippet      string `json:"snippet"`
}

// referenceDocumentAllowedMimes is the closed upload-type set: pdf, docx,
// pptx, xlsx, md, txt, html, csv. Legacy binary formats (doc, ppt) and
// everything else are rejected at upload with conversion guidance.
var referenceDocumentAllowedMimes = map[string]struct{}{
	"application/pdf": {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   {},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {},
	"text/markdown": {},
	"text/plain":    {},
	"text/html":     {},
	"text/csv":      {},
}

// ValidReferenceDocMime reports whether mime is in the allowed upload set.
func ValidReferenceDocMime(mime string) bool {
	_, ok := referenceDocumentAllowedMimes[mime]
	return ok
}

// ValidateReferenceDocumentName enforces the 1..200 display-name rule. The
// name may not be blank (whitespace-only counts as blank).
func ValidateReferenceDocumentName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: reference document name is required", ErrInvalid)
	}
	if len(name) > MaxReferenceDocumentNameChars {
		return fmt.Errorf("%w: reference document name exceeds maximum length of %d characters", ErrInvalid, MaxReferenceDocumentNameChars)
	}
	return nil
}

// ValidateReferenceDocumentDescription enforces the manifest-description cap.
func ValidateReferenceDocumentDescription(description string) error {
	if len(description) > MaxReferenceDocumentDescriptionChars {
		return fmt.Errorf("%w: reference document description exceeds maximum length of %d characters", ErrInvalid, MaxReferenceDocumentDescriptionChars)
	}
	return nil
}

// ValidRefDocScope reports whether scope is one of the two tiers.
func ValidRefDocScope(scope string) bool {
	switch scope {
	case RefDocScopeAttached, RefDocScopeWorkspace:
		return true
	default:
		return false
	}
}

// ValidateRefDocScope enforces a non-empty, known scope (the store applies
// the attached default before validating writes).
func ValidateRefDocScope(scope string) error {
	if !ValidRefDocScope(scope) {
		return fmt.Errorf("%w: unknown reference document scope %q, must be %q or %q", ErrInvalid, scope, RefDocScopeAttached, RefDocScopeWorkspace)
	}
	return nil
}

// ValidRefDocIndexStatus reports whether status is one of the three index states.
func ValidRefDocIndexStatus(status string) bool {
	switch status {
	case RefDocIndexReady, RefDocIndexNoTextLayer, RefDocIndexProcessing:
		return true
	default:
		return false
	}
}

// ValidateRefDocIndexStatus enforces a non-empty, known index status (the
// store applies the processing default before validating writes).
func ValidateRefDocIndexStatus(status string) error {
	if !ValidRefDocIndexStatus(status) {
		return fmt.Errorf("%w: unknown reference document index status %q", ErrInvalid, status)
	}
	return nil
}

// ValidDocumentLocatorKind reports whether kind is one of the five locator forms.
func ValidDocumentLocatorKind(kind string) bool {
	switch kind {
	case LocatorKindPage, LocatorKindSlide, LocatorKindSheet, LocatorKindHeading, LocatorKindNone:
		return true
	default:
		return false
	}
}

// ValidateDocumentLocatorKind enforces a known locator kind; empty is invalid
// (the sectioner emits none for locator-less formats, never "").
func ValidateDocumentLocatorKind(kind string) error {
	if !ValidDocumentLocatorKind(kind) {
		return fmt.Errorf("%w: unknown document section locator kind %q", ErrInvalid, kind)
	}
	return nil
}

// ValidateReferenceDocument enforces the full write-shape of a
// reference_documents row: identity fields present, name/description within
// bounds, an allowed mime, and known scope/index-status values. Stores apply
// the scope/index-status defaults before calling this.
func ValidateReferenceDocument(doc *ReferenceDocument) error {
	if doc == nil {
		return fmt.Errorf("%w: reference document is required", ErrInvalid)
	}
	if doc.WorkspaceID == "" {
		return fmt.Errorf("%w: reference document workspace is required", ErrInvalid)
	}
	if doc.StorageKey == "" {
		return fmt.Errorf("%w: reference document storage key is required", ErrInvalid)
	}
	if doc.Backend == "" {
		return fmt.Errorf("%w: reference document storage backend is required", ErrInvalid)
	}
	if doc.UploadedBy == "" {
		return fmt.Errorf("%w: reference document uploader is required", ErrInvalid)
	}
	if err := ValidateReferenceDocumentName(doc.Name); err != nil {
		return err
	}
	if err := ValidateReferenceDocumentDescription(doc.Description); err != nil {
		return err
	}
	if !ValidReferenceDocMime(doc.MimeType) {
		return fmt.Errorf("%w: unsupported reference document type %q", ErrInvalid, doc.MimeType)
	}
	if err := ValidateRefDocScope(doc.Scope); err != nil {
		return err
	}
	if err := ValidateRefDocIndexStatus(doc.IndexStatus); err != nil {
		return err
	}
	if doc.SizeBytes < 0 {
		return fmt.Errorf("%w: reference document size must not be negative", ErrInvalid)
	}
	if doc.PageCount < 0 {
		return fmt.Errorf("%w: reference document page count must not be negative", ErrInvalid)
	}
	return nil
}

// ValidateDocumentSection enforces one document_sections row: parent document
// named, a known locator kind, an ATX level in 0..3 (0 = synthetic section),
// and a non-negative ordinal. Heading/locator/body content is free-form.
func ValidateDocumentSection(section *DocumentSection) error {
	if section == nil {
		return fmt.Errorf("%w: document section is required", ErrInvalid)
	}
	if section.DocumentID == "" {
		return fmt.Errorf("%w: document section document is required", ErrInvalid)
	}
	if err := ValidateDocumentLocatorKind(section.LocatorKind); err != nil {
		return err
	}
	if section.Level < 0 || section.Level > 3 {
		return fmt.Errorf("%w: document section level must be between 0 and 3", ErrInvalid)
	}
	if section.Ordinal < 0 {
		return fmt.Errorf("%w: document section ordinal must not be negative", ErrInvalid)
	}
	return nil
}
