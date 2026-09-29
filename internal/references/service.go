package references

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/attachments"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ErrNoTextLayer classifies the scanned-PDF outcome: a PDF whose conversion
// yields no extractable text. It is never returned from Upload — the upload
// SUCCEEDS with the domain.RefDocIndexNoTextLayer index status and an empty
// index (spec: "the upload succeeds with a no-text-layer status naming the
// limitation") — it is the typed marker the pipeline attaches to the
// outcome's Err field so callers can branch on it with errors.Is.
var ErrNoTextLayer = errors.New("pdf has no extractable text layer (scanned or image-only)")

// MountDirName is the run-scoped mount directory the visible reference
// documents are written under (add-reference-documents design D8: mounted
// beside the agent tree, read-only and jailed).
const MountDirName = "references"

// workspaceStorage is the narrow storage port the as-is blobs ride (D1 of
// route-reference-documents-through-workspace-storage): the workspace storage
// resolver (internal/storage/resolver.WorkspaceStorage). ForWorkspace resolves
// the driver NEW blobs land on (the workspace's configured backend, the
// instance default when unconfigured), ForBackend the driver holding an
// EXISTING blob's bytes by the backend recorded on its row, and DriverName
// names the backend new rows record — so serving can return to the recorded
// backend after a workspace switches. The narrow interface keeps the
// constructor testable with a scripted resolver and makes the wrong wiring (a
// plain instance driver) a compile-time mismatch.
type workspaceStorage interface {
	ForWorkspace(ctx context.Context, workspaceID string) (storage.Storage, error)
	ForBackend(ctx context.Context, workspaceID, backend string) (storage.Storage, error)
	DriverName(ctx context.Context, workspaceID string) (string, error)
}

// Service implements the workspace reference-document lifecycle over the
// registry and section stores: upload (classify → store blob as-is →
// convert → section → index, D1/D2), replace and delete, attach editing,
// promotion, the run-visibility lens, workspace-scoped section search, and
// the run-scoped mount writer (D8).
type Service struct {
	// st is the documented transaction seam exception (AGENTS.md): Upload,
	// Replace, and RebuildIndex span ReferenceDocuments and DocumentSections
	// atomically and therefore keep the whole store.Store aggregate.
	st store.Store
	// stor is the workspace storage resolver the as-is blobs ride (D1):
	// writes resolve ForWorkspace and record DriverName, every read of an
	// existing blob dispatches ForBackend on the row's recorded backend (D3).
	stor workspaceStorage
}

// NewService builds the references service. stor is the workspace storage
// resolver port the as-is blobs ride (see workspaceStorage — the concrete
// resolver.WorkspaceStorage satisfies it; per-workspace backend resolution
// happens inside it, keyed on the row's recorded backend for reads).
func NewService(st store.Store, stor workspaceStorage) *Service {
	return &Service{st: st, stor: stor}
}

// UploadInput is one upload or replace request.
type UploadInput struct {
	// Filename is the original file name — its extension drives the
	// converter choice and the capability mount's file name.
	Filename string
	// Name is the display name; it defaults to the filename base when empty.
	Name string
	// Description feeds the compose-time manifest (D6).
	Description string
	// Data is the original file content, stored as-is (D1).
	Data []byte
	// AgentIDs / ChannelIDs are the attach lists (the upload default scope is
	// attached). Replace ignores them — attach edits ride SetAgents /
	// SetChannels.
	AgentIDs   []string
	ChannelIDs []string
}

// Upload validates, sniffs, and stores one reference document: classify →
// store the original bytes as-is under a fresh capability key (D1) → convert
// in-process → sectionize → index the sections → drop the text. The response
// is synchronous: the row's index status is ready, or no_text_layer when a
// PDF yields no extractable text (upload still succeeds; the index stays
// empty). The extracted text is never stored anywhere but the section rows.
func (s *Service) Upload(ctx context.Context, workspaceID, uploadedBy string, in UploadInput) (domain.ReferenceDocument, error) {
	docType, mime, err := classifyUpload(in.Filename, in.Data)
	if err != nil {
		return domain.ReferenceDocument{}, err
	}
	name := displayName(in.Filename, in.Name)
	if err := domain.ValidateReferenceDocumentName(name); err != nil {
		return domain.ReferenceDocument{}, err
	}
	if err := domain.ValidateReferenceDocumentDescription(in.Description); err != nil {
		return domain.ReferenceDocument{}, err
	}

	backend, err := s.backendName(ctx, workspaceID)
	if err != nil {
		return domain.ReferenceDocument{}, err
	}
	// D2: the blob rides the workspace's configured driver — the resolver's
	// ForWorkspace — not the instance default opened at boot.
	drv, err := s.stor.ForWorkspace(ctx, workspaceID)
	if err != nil {
		return domain.ReferenceDocument{}, err
	}
	key, err := storage.NewKey()
	if err != nil {
		return domain.ReferenceDocument{}, fmt.Errorf("capability key: %w", err)
	}
	if err := drv.Put(ctx, key, bytes.NewReader(in.Data), int64(len(in.Data)), mime); err != nil {
		return domain.ReferenceDocument{}, fmt.Errorf("store blob for %q: %w", name, err)
	}

	idx, err := indexDocument(docType, name, in.Data)
	if err != nil {
		return domain.ReferenceDocument{}, fmt.Errorf("index %q: %w", in.Filename, err)
	}

	var doc domain.ReferenceDocument
	err = s.st.WithTx(ctx, func(tx store.Store) error {
		doc = domain.ReferenceDocument{
			WorkspaceID: workspaceID,
			Name:        name,
			Description: in.Description,
			MimeType:    mime,
			SizeBytes:   int64(len(in.Data)),
			StorageKey:  key,
			Backend:     backend,
			PageCount:   idx.Pages,
			Scope:       domain.RefDocScopeAttached,
			IndexStatus: idx.Status,
			AgentIDs:    in.AgentIDs,
			ChannelIDs:  in.ChannelIDs,
			UploadedBy:  uploadedBy,
		}
		if err := tx.ReferenceDocuments().Create(ctx, &doc); err != nil {
			return err
		}
		// A scanned PDF yields no sections — ReplaceForDocument with an
		// empty set is the no_text_layer shape (spec: index contains at most
		// page-level sections).
		return tx.DocumentSections().ReplaceForDocument(ctx, workspaceID, doc.ID, idx.Sections)
	})
	if err != nil {
		// The committed-row failure leaves the freshly stored blob orphaned
		// (the attachment blob precedent — harmless, never a broken row).
		return domain.ReferenceDocument{}, err
	}
	return doc, nil
}

// Replace re-uploads one document's bytes: the blob is swapped under a fresh
// capability key and the derived state is rebuilt from the new bytes in one
// transaction — ReplaceBlob row rewrite, then the section set replaced
// (delete-then-insert; no stale sections survive). Only Filename and Data are
// consumed: name/description edits ride UpdateMeta, attach edits ride
// SetAgents/SetChannels, and the scope tier is left as stored. The old blob
// is removed after commit; a failed removal leaves an orphan blob, never a
// broken row.
func (s *Service) Replace(ctx context.Context, workspaceID, id string, in UploadInput) (domain.ReferenceDocument, error) {
	docType, mime, err := classifyUpload(in.Filename, in.Data)
	if err != nil {
		return domain.ReferenceDocument{}, err
	}
	currentPtr, err := s.st.ReferenceDocuments().Get(ctx, workspaceID, id)
	if err != nil {
		return domain.ReferenceDocument{}, err
	}
	current := *currentPtr

	backend, err := s.backendName(ctx, workspaceID)
	if err != nil {
		return domain.ReferenceDocument{}, err
	}
	// D2: the replacement blob lands on the workspace's CURRENT configured
	// driver; the row's backend is rewritten to it (D3 keeps the old blob
	// readable through its recorded backend until the cleanup below).
	drv, err := s.stor.ForWorkspace(ctx, workspaceID)
	if err != nil {
		return domain.ReferenceDocument{}, err
	}
	key, err := storage.NewKey()
	if err != nil {
		return domain.ReferenceDocument{}, fmt.Errorf("capability key: %w", err)
	}
	if err := drv.Put(ctx, key, bytes.NewReader(in.Data), int64(len(in.Data)), mime); err != nil {
		return domain.ReferenceDocument{}, fmt.Errorf("store blob for %q: %w", current.Name, err)
	}

	idx, err := indexDocument(docType, current.Name, in.Data)
	if err != nil {
		return domain.ReferenceDocument{}, fmt.Errorf("index %q: %w", in.Filename, err)
	}

	err = s.st.WithTx(ctx, func(tx store.Store) error {
		if err := tx.ReferenceDocuments().ReplaceBlob(ctx, workspaceID, id, mime, int64(len(in.Data)), key, backend, idx.Pages, idx.Status); err != nil {
			return err
		}
		return tx.DocumentSections().ReplaceForDocument(ctx, workspaceID, id, idx.Sections)
	})
	if err != nil {
		return domain.ReferenceDocument{}, err
	}

	// Post-commit cleanup of the superseded blob, dispatched through the
	// backend the row recorded (D3 — the old blob may live on a driver the
	// workspace has since switched away from). context.WithoutCancel so a
	// completed replace never strands the old blob on a torn-down request.
	_ = s.deleteBlob(context.WithoutCancel(ctx), current)

	return s.getDocument(ctx, workspaceID, id)
}

// RebuildIndex re-derives the section index from the stored blob alone — the
// failure-mid-rebuild recovery path (design D1: the index is rebuildable from
// the blob; nothing else is retained). The row's index status is rewritten to
// what the blob now yields.
func (s *Service) RebuildIndex(ctx context.Context, workspaceID, id string) error {
	docPtr, err := s.st.ReferenceDocuments().Get(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	doc := *docPtr
	docType := mimeDocType(doc.MimeType)
	if docType == "" {
		return fmt.Errorf("%w: reference document %q has unsupported type %q", domain.ErrInvalid, doc.Name, doc.MimeType)
	}
	data, err := s.readBlob(ctx, doc)
	if err != nil {
		return err
	}
	idx, err := indexDocument(docType, doc.Name, data)
	if err != nil {
		return fmt.Errorf("rebuild index for %q: %w", doc.Name, err)
	}
	return s.st.WithTx(ctx, func(tx store.Store) error {
		if err := tx.ReferenceDocuments().ReplaceBlob(ctx, workspaceID, id, doc.MimeType, doc.SizeBytes, doc.StorageKey, doc.Backend, idx.Pages, idx.Status); err != nil {
			return err
		}
		return tx.DocumentSections().ReplaceForDocument(ctx, workspaceID, id, idx.Sections)
	})
}

// Delete removes a document and every piece of derived state: the row (its
// join rows die with it), the section index, and the stored blob. Row first —
// a blob removal that then fails leaves an orphan blob (harmless precedent),
// never a row pointing at missing bytes.
func (s *Service) Delete(ctx context.Context, workspaceID, id string) error {
	docPtr, err := s.st.ReferenceDocuments().Get(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	doc := *docPtr
	if err := s.st.ReferenceDocuments().Delete(ctx, workspaceID, id); err != nil {
		return err
	}
	// Belt and braces for adapters without a cascade: absent sections are a
	// no-op.
	if err := s.st.DocumentSections().DeleteForDocument(ctx, workspaceID, id); err != nil {
		return err
	}
	if err := s.deleteBlob(context.WithoutCancel(ctx), doc); err != nil {
		return fmt.Errorf("delete stored blob for %q: %w", doc.Name, err)
	}
	return nil
}

// UpdateMeta rewrites the editable bibliographic fields (name, description).
func (s *Service) UpdateMeta(ctx context.Context, workspaceID, id, name, description string) (domain.ReferenceDocument, error) {
	if err := s.st.ReferenceDocuments().UpdateMeta(ctx, workspaceID, id, name, description); err != nil {
		return domain.ReferenceDocument{}, err
	}
	return s.getDocument(ctx, workspaceID, id)
}

// SetAgents replaces the document's attached-agent set. Every id must exist
// in the workspace — unknown or foreign ids return domain.ErrNotFound (the
// store enforces FK parity).
func (s *Service) SetAgents(ctx context.Context, workspaceID, id string, agentIDs []string) (domain.ReferenceDocument, error) {
	if err := s.st.ReferenceDocuments().SetAgents(ctx, workspaceID, id, agentIDs); err != nil {
		return domain.ReferenceDocument{}, err
	}
	return s.getDocument(ctx, workspaceID, id)
}

// SetChannels replaces the document's attached-channel set, with the same
// workspace FK parity as SetAgents.
func (s *Service) SetChannels(ctx context.Context, workspaceID, id string, channelIDs []string) (domain.ReferenceDocument, error) {
	if err := s.st.ReferenceDocuments().SetChannels(ctx, workspaceID, id, channelIDs); err != nil {
		return domain.ReferenceDocument{}, err
	}
	return s.getDocument(ctx, workspaceID, id)
}

// Promote flips the visibility tier to workspace — visible to every agent in
// the workspace. Admin-gating rides the permission catalog at the API layer
// (design D7); the service is the tier flip itself.
func (s *Service) Promote(ctx context.Context, workspaceID, id string) (domain.ReferenceDocument, error) {
	return s.setScope(ctx, workspaceID, id, domain.RefDocScopeWorkspace)
}

// Demote flips the visibility tier back to attached.
func (s *Service) Demote(ctx context.Context, workspaceID, id string) (domain.ReferenceDocument, error) {
	return s.setScope(ctx, workspaceID, id, domain.RefDocScopeAttached)
}

func (s *Service) setScope(ctx context.Context, workspaceID, id, scope string) (domain.ReferenceDocument, error) {
	if err := s.st.ReferenceDocuments().SetScope(ctx, workspaceID, id, scope); err != nil {
		return domain.ReferenceDocument{}, err
	}
	return s.getDocument(ctx, workspaceID, id)
}

// getDocument loads one row and dereferences it — the store port returns a
// non-nil pointer on a nil error, so the point of use assumes non-nil.
func (s *Service) getDocument(ctx context.Context, workspaceID, id string) (domain.ReferenceDocument, error) {
	doc, err := s.st.ReferenceDocuments().Get(ctx, workspaceID, id)
	if err != nil {
		return domain.ReferenceDocument{}, err
	}
	return *doc, nil
}

// Get resolves one document within its workspace. A foreign id is
// indistinguishable from an unknown one (domain.ErrNotFound — tenancy).
func (s *Service) Get(ctx context.Context, workspaceID, id string) (domain.ReferenceDocument, error) {
	doc, err := s.st.ReferenceDocuments().Get(ctx, workspaceID, id)
	if err != nil {
		return domain.ReferenceDocument{}, err
	}
	return *doc, nil
}

// VisibleDocuments is the compose-side visibility lens (D7): the workspace's
// documents filtered through the one visibility predicate, evaluated
// identically at manifest and query time.
func (s *Service) VisibleDocuments(ctx context.Context, workspaceID string, scope domain.DocumentRunScope) ([]domain.ReferenceDocument, error) {
	docs, err := s.st.ReferenceDocuments().List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	visible := make([]domain.ReferenceDocument, 0, len(docs))
	for _, doc := range docs {
		if doc.VisibleTo(scope) {
			visible = append(visible, doc)
		}
	}
	return visible, nil
}

// SearchDocuments runs the full-text section query over exactly the
// documents the run can see (D7: visibility enforced at query time, never by
// post-filtering). An empty visible set short-circuits to an empty result —
// it must never degrade to an unfiltered query.
func (s *Service) SearchDocuments(ctx context.Context, workspaceID string, scope domain.DocumentRunScope, query string, limit int) ([]domain.DocumentSectionHit, error) {
	visible, err := s.VisibleDocuments(ctx, workspaceID, scope)
	if err != nil {
		return nil, err
	}
	if len(visible) == 0 {
		return []domain.DocumentSectionHit{}, nil
	}
	ids := make([]string, 0, len(visible))
	for _, doc := range visible {
		ids = append(ids, doc.ID)
	}
	return s.st.DocumentSections().Search(ctx, workspaceID, ids, query, limit)
}

// WriteMount materializes the run's visible documents as files under dir —
// the references mount's write half (D8). Docs are written as
// <dir>/<doc.Name>, sorted by name (id as the determinism tiebreak); name
// collisions get the deterministic suffix <stem>-2<ext>, -3, ... Display
// names are sanitized to a single path element, so a name can never escape
// the mount directory.
func (s *Service) WriteMount(ctx context.Context, dir string, docs []domain.ReferenceDocument) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("%w: mount directory is required", domain.ErrInvalid)
	}

	sorted := make([]domain.ReferenceDocument, len(docs))
	copy(sorted, docs)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name == sorted[j].Name {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].Name < sorted[j].Name
	})

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create mount directory: %w", err)
	}

	used := make(map[string]struct{}, len(sorted))
	for _, doc := range sorted {
		fileName := mountFileName(doc.Name, used)
		used[fileName] = struct{}{}
		data, err := s.readBlob(ctx, doc)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, fileName), data, 0o644); err != nil {
			return fmt.Errorf("write mount file %q: %w", fileName, err)
		}
	}
	return nil
}

// TableOfContents projects one document's top-level headings for the
// compose-time manifest (D6): the distinct Level<=1 headings in ordinal
// order, capped at topN entries (topN <= 0 means no cap — the store search
// convention). An unresolvable document returns domain.ErrNotFound; a
// document with no indexable headings returns an empty slice.
func (s *Service) TableOfContents(ctx context.Context, workspaceID, docID string, topN int) ([]string, error) {
	sections, err := s.st.DocumentSections().ListForDocument(ctx, workspaceID, docID)
	if err != nil {
		return nil, err
	}
	toc := make([]string, 0, len(sections))
	seen := make(map[string]struct{}, len(sections))
	for _, sec := range sections {
		if sec.Level > 1 {
			continue
		}
		heading := strings.TrimSpace(sec.Heading)
		if heading == "" {
			continue
		}
		if _, dup := seen[heading]; dup {
			continue
		}
		seen[heading] = struct{}{}
		toc = append(toc, heading)
		if topN > 0 && len(toc) >= topN {
			break
		}
	}
	return toc, nil
}

// URL renders the document's capability URL exactly the way the attachment
// upload handler builds its response URL: the recorded backend's URL over
// the row's capability key (D3 — per-blob dispatch, never current config, so
// the URL always names the driver the bytes actually live on). An
// unresolvable recorded backend renders no URL — the same condition the
// capability serving path fails on.
func (s *Service) URL(doc domain.ReferenceDocument) string {
	backend, err := s.stor.ForBackend(context.Background(), doc.WorkspaceID, doc.Backend)
	if err != nil {
		return ""
	}
	return backend.URL(doc.StorageKey)
}

// backendName names the backend new blobs are recorded against — the
// workspace's configured driver per the resolver (D2); an unconfigured
// workspace records the instance default's name, "local".
func (s *Service) backendName(ctx context.Context, workspaceID string) (string, error) {
	backend, err := s.stor.DriverName(ctx, workspaceID)
	if err != nil {
		return "", fmt.Errorf("resolve storage backend: %w", err)
	}
	return backend, nil
}

// deleteBlob removes one document's stored bytes through the backend the row
// records (D3 — per-blob dispatch, never the workspace's current config), so
// pre-switch blobs stay removable after a workspace moves to another driver.
func (s *Service) deleteBlob(ctx context.Context, doc domain.ReferenceDocument) error {
	drv, err := s.stor.ForBackend(ctx, doc.WorkspaceID, doc.Backend)
	if err != nil {
		return err
	}
	return drv.Delete(ctx, doc.StorageKey)
}

// readBlob loads one document's stored bytes through the backend the row
// records (D3 — indexing, re-index, and mount materialization inherit the
// dispatch), never through the workspace's current configuration.
func (s *Service) readBlob(ctx context.Context, doc domain.ReferenceDocument) ([]byte, error) {
	drv, err := s.stor.ForBackend(ctx, doc.WorkspaceID, doc.Backend)
	if err != nil {
		return nil, fmt.Errorf("resolve stored blob backend for %q: %w", doc.Name, err)
	}
	f, err := drv.Open(ctx, doc.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("open stored blob for %q: %w", doc.Name, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read stored blob for %q: %w", doc.Name, err)
	}
	return data, nil
}

// indexedDocument is the ingest pipeline's outcome (tasks 3.3): the row's
// index status, the section rows to store (nil for scanned PDFs), and the
// PDF page count. Err carries the classification sentinel for non-fatal
// outcomes — ErrNoTextLayer for scanned PDFs, which succeed as uploads;
// fatal pipeline failures return from indexDocument instead.
type indexedDocument struct {
	Status   string
	Sections []domain.DocumentSection
	Pages    int
	Err      error
}

// indexDocument converts one document in-process, sections the markdown, and
// maps the slices to index rows. Text formats (md, txt) pass through
// unconverted (D2); everything else rides tools.ConvertDocument's registry by
// canonical type — never by the display name, which is user-editable and may
// carry no extension. The extracted text lives nowhere but the returned
// section rows (D1).
func indexDocument(docType, docName string, data []byte) (indexedDocument, error) {
	pages := 0
	if docType == "pdf" {
		pages = attachments.PDFPageCount(data)
	}

	markdown, err := convertForIndex(docType, data)
	if err != nil {
		return indexedDocument{}, err
	}

	// A scanned or image-only PDF converts to nothing: upload succeeds with
	// the no_text_layer status and an empty index (spec scenario).
	if docType == "pdf" && strings.TrimSpace(markdown) == "" {
		return indexedDocument{
			Status: domain.RefDocIndexNoTextLayer,
			Pages:  pages,
			Err:    ErrNoTextLayer,
		}, nil
	}

	sections := make([]domain.DocumentSection, 0, 8)
	for _, sec := range Sectionize(docName, docType, markdown) {
		sections = append(sections, domain.DocumentSection{
			Heading:     sec.Heading,
			Locator:     sec.Locator,
			LocatorKind: sec.LocatorKind,
			Level:       sec.Level,
			Ordinal:     sec.Ordinal,
			Body:        sec.Body,
		})
	}
	return indexedDocument{Status: domain.RefDocIndexReady, Sections: sections, Pages: pages}, nil
}

// convertForIndex converts one in-memory document to markdown. md/txt bypass
// conversion entirely (D2); the rest resolve through the converter registry
// under a synthetic name carrying the canonical extension — conversion is a
// function of the sniffed type, never of the user-editable display name. A
// sniffed container that fails conversion (a zip that is not a real docx) is
// invalid input.
func convertForIndex(docType string, data []byte) (string, error) {
	switch docType {
	case "md", "txt":
		return string(data), nil
	}
	markdown, err := tools.ConvertDocument("index."+docType, data)
	if err != nil {
		return "", fmt.Errorf("%w: converting %s document: %v", domain.ErrInvalid, docType, err)
	}
	return markdown, nil
}

// displayName resolves the display name: the caller's choice, defaulting to
// the filename base. The extension is kept in the default so the capability
// mount writes converter-addressable file names (document.read resolves by
// extension).
func displayName(filename, name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return filepath.Base(strings.TrimSpace(filename))
}

// mountNameSanitizer reduces a display name to a single path element:
// separators and dot-dot runs become underscores, so the mount stays jailed
// (D8) regardless of what the display name contains.
var mountNameSanitizer = strings.NewReplacer("/", "_", "\\", "_", "\x00", "_", "..", "_")

// sanitizeMountName maps a display name to a safe single path element.
func sanitizeMountName(name string) string {
	clean := filepath.Clean(mountNameSanitizer.Replace(strings.TrimSpace(name)))
	if clean == "." || clean == ".." || clean == string(filepath.Separator) {
		return "document"
	}
	return clean
}

// mountFileName resolves one document's mount file name, suffixing
// deterministically on collision: <stem>-2<ext>, <stem>-3<ext>, ...
func mountFileName(name string, used map[string]struct{}) string {
	candidate := sanitizeMountName(name)
	if _, taken := used[candidate]; !taken {
		return candidate
	}
	ext := filepath.Ext(candidate)
	stem := strings.TrimSuffix(candidate, ext)
	for n := 2; ; n++ {
		next := stem + "-" + strconv.Itoa(n) + ext
		if _, taken := used[next]; !taken {
			return next
		}
	}
}
