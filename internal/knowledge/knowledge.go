// Package knowledge implements the durable document corpus (add-knowledge-
// corpus design D2/D5/D6/D11): raw document bytes on disk under the layout's
// knowledge directory — global documents at <root>/global/, agent-scoped at
// <root>/agents/<slug>/ — cataloged through the store's documents table. The
// package owns the single ingestion gate (formats and size caps, D5), the
// atomic scope moves (D6), and the per-scope INDEX.md regeneration (D11).
package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Scopes and ingestion sources — the canonical Document.Scope / Document.
// Source vocabularies. The store layer persists the same raw strings.
const (
	ScopeGlobal = "global"
	ScopeAgent  = "agent"
	SourceUser  = "user"
	SourceAgent = "agent"
)

// Ingestion caps (design D5): text documents at 1 MiB, PDFs and OOXML office
// documents at 20 MiB — the existing multimodal-read ceiling, so a larger PDF
// could be stored but never read and is rejected at every ingestion path
// instead. The OOXML cap matches the document engines' input cap
// (add-document-editing design D9).
const (
	MaxTextBytes  = 1 << 20
	MaxPDFBytes   = 20 << 20
	MaxOOXMLBytes = 20 << 20
)

// IndexFileName is the store-maintained per-scope index file (design D11).
// It is derived state, never a document: not cataloged, not listed, and
// reserved as a filename.
const IndexFileName = "INDEX.md"

// PDFMime is the PDF document kind (design D5).
const PDFMime = "application/pdf"

// The OOXML office-document kinds (add-document-editing task 5.4): the modern
// Word, Excel, and PowerPoint formats, accepted alongside text and PDF.
const (
	DocxMime = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	XlsxMime = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	PptxMime = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
)

// isOOXMLMime reports whether kind is one of the OOXML office-document kinds.
func isOOXMLMime(kind string) bool {
	return kind == DocxMime || kind == XlsxMime || kind == PptxMime
}

// filePerm and dirPerm mirror the app layout's private-to-user files
// (internal/config, attachments).
const (
	filePerm os.FileMode = 0600
	dirPerm  os.FileMode = 0700
)

// ErrRejected wraps every ingestion rejection: unsupported kind, oversize, or
// an unusable filename. Callers surface the wrapped reason.
var ErrRejected = errors.New("document rejected")

// scopeDirNames pins the on-disk scope subtree names under the knowledge root
// scopeDirNames pins the on-disk scope subtree names under the knowledge root
// (design D2): the global tree at global/, agent directories under agents/.
const (
	globalDir = "global"
	agentsDir = "agents"
)

// Roots are the two scope directories the corpus composes from: the global
// tree and the per-agent tree root (agent directories live directly under
// Agents, named by slug).
type Roots struct {
	Global string
	Agents string
}

// SaveInput is one ingestion request. Scope must be ScopeGlobal or
// ScopeAgent; agent-scope saves require AgentID and AgentSlug (the slug names
// the directory, the id the catalog FK).
type SaveInput struct {
	Filename    string
	MIME        string
	Data        []byte
	Scope       string
	AgentID     string
	AgentSlug   string
	Description string
	Source      string
}

// Store manages the knowledge directory and its catalog rows. It is safe for
// concurrent use: every method touches distinct scope/filename-derived paths
// and delegates row truth to the store.
type Store struct {
	root string
	rows store.DocumentStore
}

// NewStore returns a knowledge store rooting documents at root (the layout's
// knowledge directory) and registering catalog rows through rows. Scope
// directories are created lazily on first write.
func NewStore(root string, rows store.DocumentStore) *Store {
	return &Store{root: root, rows: rows}
}

// Roots returns the global and per-agent scope directories.
func (s *Store) Roots() Roots {
	return Roots{
		Global: filepath.Join(s.root, globalDir),
		Agents: filepath.Join(s.root, agentsDir),
	}
}

// AgentDir returns one agent's scope directory (named by slug).
func (s *Store) AgentDir(slug string) string {
	return filepath.Join(s.root, agentsDir, slug)
}

// Save ingests one document: the gate (D5) and the per-scope uniqueness
// precheck (D6) run first — a rejection leaves no row and no file — then the
// bytes land in the scope directory and the catalog row is registered; a row
// failure removes the file so a cataloged document always has its bytes.
func (s *Store) Save(ctx context.Context, in SaveInput) (store.Document, error) {
	filename, err := cleanFilename(in.Filename)
	if err != nil {
		return store.Document{}, err
	}
	if err := ValidateIncoming(filename, in.MIME, int64(len(in.Data))); err != nil {
		return store.Document{}, err
	}
	scope, agentID, agentSlug, err := normalizeScope(in.Scope, in.AgentID, in.AgentSlug)
	if err != nil {
		return store.Document{}, err
	}
	source := in.Source
	if source != SourceUser && source != SourceAgent {
		return store.Document{}, fmt.Errorf("%w: unknown source %q", ErrRejected, in.Source)
	}

	// Uniqueness (D6): a duplicate within one scope is rejected, the existing
	// document unchanged. The catalog's composite UNIQUE is the backstop.
	if _, found, err := s.rows.DocumentByName(ctx, scope, agentSlug, filename); err != nil {
		return store.Document{}, fmt.Errorf("save document: %w", err)
	} else if found {
		return store.Document{}, fmt.Errorf("save document %s: %w: scope %q already holds a document of that name",
			filename, store.ErrDuplicate, scope)
	}

	dir := s.scopeDir(scope, agentSlug)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return store.Document{}, fmt.Errorf("save document: %w", err)
	}

	// The file lands first; a catalog failure removes it (the attachments
	// store's save order).
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, in.Data, filePerm); err != nil {
		return store.Document{}, fmt.Errorf("save document: %w", err)
	}
	doc := store.Document{
		Filename:    filename,
		MIME:        resolveMime(filename, in.MIME),
		Size:        int64(len(in.Data)),
		Scope:       scope,
		AgentID:     agentID,
		AgentSlug:   agentSlug,
		Description: in.Description,
		Source:      source,
	}
	saved, err := s.rows.PutDocument(ctx, doc)
	if err != nil {
		_ = os.Remove(path)
		return store.Document{}, fmt.Errorf("save document: %w", err)
	}
	s.regenerateIndex(ctx, scope, agentSlug)
	return saved, nil
}

// List returns every cataloged document. The result is never nil and never
// contains the scope indexes (they are not cataloged).
func (s *Store) List(ctx context.Context) ([]store.Document, error) {
	docs, err := s.rows.ListDocuments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	return docs, nil
}

// Get returns one document's catalog row; an unknown id wraps
// store.ErrNotFound.
func (s *Store) Get(ctx context.Context, id string) (store.Document, error) {
	doc, err := s.rows.GetDocument(ctx, id)
	if err != nil {
		return store.Document{}, fmt.Errorf("get document: %w", err)
	}
	return doc, nil
}

// Content reads one document's stored bytes. The row locates the scope
// directory; an unknown id wraps store.ErrNotFound.
func (s *Store) Content(ctx context.Context, id string) ([]byte, error) {
	doc, err := s.rows.GetDocument(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read document: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(s.scopeDir(doc.Scope, doc.AgentSlug), doc.Filename))
	if err != nil {
		return nil, fmt.Errorf("read document %s: %w", doc.Filename, err)
	}
	return data, nil
}

// SetScope moves one document between scopes (D6): the target scope is
// prechecked for filename collisions, the file is renamed to the target
// directory, and the row is flipped; a row failure renames the file back, so
// a cataloged document always has its file at its scope's location. Both
// scopes' indexes are regenerated.
func (s *Store) SetScope(ctx context.Context, id, scope, agentID, agentSlug string) (store.Document, error) {
	doc, err := s.rows.GetDocument(ctx, id)
	if err != nil {
		return store.Document{}, fmt.Errorf("set document scope: %w", err)
	}
	scope, agentID, agentSlug, err = normalizeScope(scope, agentID, agentSlug)
	if err != nil {
		return store.Document{}, fmt.Errorf("set document scope: %w", err)
	}

	// Uniqueness in the target scope; the document's own row is not a
	// collision (a same-scope re-set is a no-op move).
	if existing, found, err := s.rows.DocumentByName(ctx, scope, agentSlug, doc.Filename); err != nil {
		return store.Document{}, fmt.Errorf("set document scope: %w", err)
	} else if found && existing.ID != doc.ID {
		return store.Document{}, fmt.Errorf("set document scope: %s: %w: scope %q already holds a document of that name",
			doc.Filename, store.ErrDuplicate, scope)
	}

	from := filepath.Join(s.scopeDir(doc.Scope, doc.AgentSlug), doc.Filename)
	to := filepath.Join(s.scopeDir(scope, agentSlug), doc.Filename)
	if err := os.MkdirAll(filepath.Dir(to), dirPerm); err != nil {
		return store.Document{}, fmt.Errorf("set document scope: %w", err)
	}
	if err := os.Rename(from, to); err != nil {
		return store.Document{}, fmt.Errorf("set document scope: %w", err)
	}
	if err := s.rows.UpdateDocumentScope(ctx, id, scope, agentID, agentSlug); err != nil {
		// Compensating move-back: the row kept the old scope, so the file
		// must too.
		if backErr := os.Rename(to, from); backErr != nil {
			err = errors.Join(err, fmt.Errorf("move file back: %w", backErr))
		}
		return store.Document{}, fmt.Errorf("set document scope: %w", err)
	}
	s.regenerateIndex(ctx, doc.Scope, doc.AgentSlug)
	s.regenerateIndex(ctx, scope, agentSlug)
	doc.Scope, doc.AgentID, doc.AgentSlug = scope, agentID, agentSlug
	return doc, nil
}

// SetDescription rewrites one document's description (D6: the primary
// discovery handle for PDFs) and regenerates the scope index, which surfaces
// descriptions.
func (s *Store) SetDescription(ctx context.Context, id, description string) (store.Document, error) {
	if err := s.rows.UpdateDocumentDescription(ctx, id, description); err != nil {
		return store.Document{}, fmt.Errorf("set document description: %w", err)
	}
	doc, err := s.rows.GetDocument(ctx, id)
	if err != nil {
		return store.Document{}, fmt.Errorf("set document description: %w", err)
	}
	s.regenerateIndex(ctx, doc.Scope, doc.AgentSlug)
	return doc, nil
}

// Delete removes one document: the catalog row first (the row is truth), then
// the file, then the scope index regenerates. An unknown id wraps
// store.ErrNotFound.
func (s *Store) Delete(ctx context.Context, id string) error {
	doc, err := s.rows.GetDocument(ctx, id)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	if err := s.rows.DeleteDocument(ctx, id); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	if err := os.Remove(filepath.Join(s.scopeDir(doc.Scope, doc.AgentSlug), doc.Filename)); err != nil {
		return fmt.Errorf("delete document %s: %w", doc.Filename, err)
	}
	s.regenerateIndex(ctx, doc.Scope, doc.AgentSlug)
	return nil
}

// RemoveAgentScope removes one agent's scope directory after the agent row's
// deletion cascaded its documents (the FK on documents.agent_id). Best-effort
// by contract: callers log a failure, they do not fail the agent delete.
func (s *Store) RemoveAgentScope(_ context.Context, slug string) error {
	if err := os.RemoveAll(s.AgentDir(slug)); err != nil {
		return fmt.Errorf("remove agent knowledge scope %s: %w", slug, err)
	}
	return nil
}

// scopeDir resolves one scope's directory: global at <root>/global, agent
// scope at <root>/agents/<slug>.
func (s *Store) scopeDir(scope, agentSlug string) string {
	if scope == ScopeAgent {
		return s.AgentDir(agentSlug)
	}
	return filepath.Join(s.root, globalDir)
}

// regenerateIndex rewrites one scope's INDEX.md from the catalog rows (D11).
// The index is derived state, so a write failure is deliberately swallowed —
// the next mutation repairs it.
func (s *Store) regenerateIndex(ctx context.Context, scope, agentSlug string) {
	docs, err := s.rows.ListDocuments(ctx)
	if err != nil {
		return
	}
	dir := s.scopeDir(scope, agentSlug)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return
	}

	var b strings.Builder
	b.WriteString("# Knowledge index\n")
	for _, doc := range scopeDocs(docs, scope, agentSlug) {
		line := "- " + doc.Filename
		if doc.Description != "" {
			line += " — " + doc.Description
		}
		line += fmt.Sprintf(" (%d bytes)", doc.Size)
		b.WriteString(line + "\n")
	}
	_ = os.WriteFile(filepath.Join(dir, IndexFileName), []byte(b.String()), filePerm)
}

// scopeDocs filters the catalog to one scope, sorted by filename (D11's index
// order).
func scopeDocs(docs []store.Document, scope, agentSlug string) []store.Document {
	out := make([]store.Document, 0)
	for _, doc := range docs {
		if doc.Scope == scope && doc.AgentSlug == agentSlug {
			out = append(out, doc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Filename < out[j].Filename })
	return out
}

// normalizeScope validates the scope vocabulary: agent scope requires the
// agent id and slug; global clears both.
func normalizeScope(scope, agentID, agentSlug string) (string, string, string, error) {
	switch scope {
	case ScopeGlobal:
		return ScopeGlobal, "", "", nil
	case ScopeAgent:
		if agentID == "" || agentSlug == "" {
			return "", "", "", fmt.Errorf("%w: agent-scoped documents require the agent's id and slug", ErrRejected)
		}
		return ScopeAgent, agentID, agentSlug, nil
	default:
		return "", "", "", fmt.Errorf("%w: unknown scope %q", ErrRejected, scope)
	}
}

// cleanFilename rejects names the corpus cannot store safely: empty names,
// anything carrying a path (the filename must be a bare base name), and the
// reserved index filename (D11: the index is never a document).
func cleanFilename(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: empty filename", ErrRejected)
	}
	if name != filepath.Base(name) || name == "." || name == ".." ||
		strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') {
		return "", fmt.Errorf("%w: filename %q must be a bare name without a path", ErrRejected, name)
	}
	if name == IndexFileName {
		return "", fmt.Errorf("%w: %s is reserved for the scope index", ErrRejected, IndexFileName)
	}
	return name, nil
}

// ValidateIncoming applies the ingestion gate (D5, widened by
// add-document-editing task 5.4): plain text (text/*, up to MaxTextBytes),
// PDF (up to MaxPDFBytes), and OOXML office documents — .docx, .xlsx, .pptx
// (up to MaxOOXMLBytes) — are accepted; anything else is rejected with a
// one-line reason naming the supported kinds. An empty declared MIME falls
// back to the filename's extension.
func ValidateIncoming(filename, mime string, size int64) error {
	kind := resolveMime(filename, mime)
	limit, ok := kindLimit(kind)
	if !ok {
		return fmt.Errorf("%w: unsupported document kind %q: only plain text (text/*, up to %s), PDF (up to %s), and OOXML office documents (.docx, .xlsx, .pptx, up to %s) are supported",
			ErrRejected, kind, humanBytes(MaxTextBytes), humanBytes(MaxPDFBytes), humanBytes(MaxOOXMLBytes))
	}
	if size > limit {
		return fmt.Errorf("%w: %s documents are limited to %s (got %s)",
			ErrRejected, kindLabel(kind), humanBytes(limit), humanBytes(size))
	}
	return nil
}

// kindLimit reports the size cap of one resolved kind and whether the kind is
// accepted at all.
func kindLimit(kind string) (int64, bool) {
	switch {
	case kind == PDFMime:
		return MaxPDFBytes, true
	case isOOXMLMime(kind):
		return MaxOOXMLBytes, true
	case strings.HasPrefix(kind, "text/"):
		return MaxTextBytes, true
	default:
		return 0, false
	}
}

// kindLabel names a kind for size reasons.
func kindLabel(kind string) string {
	switch {
	case kind == PDFMime:
		return "PDF"
	case isOOXMLMime(kind):
		return "OOXML"
	}
	return "text"
}

// resolveMime determines a document's MIME kind: the declared value when
// present, otherwise the filename's extension — .pdf is a PDF, the OOXML
// extensions resolve to their office-document kinds (add-document-editing
// task 5.4), known word-processing/media/archive extensions resolve to their
// real kind (so the rejection names them), and everything else is attempted
// as plain text.
func resolveMime(filename, declared string) string {
	if declared != "" {
		return declared
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf":
		return PDFMime
	case ".docx":
		return DocxMime
	case ".xlsx":
		return XlsxMime
	case ".pptx":
		return PptxMime
	case ".doc":
		return "application/msword"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4", ".mov", ".avi", ".mkv", ".webm":
		return "video/mp4"
	case ".mp3", ".wav", ".ogg", ".flac":
		return "audio/mpeg"
	case ".zip", ".tar", ".gz", ".rar", ".7z":
		return "application/zip"
	case ".exe", ".dll", ".bin", ".dmg", ".iso":
		return "application/octet-stream"
	default:
		// Unknown extensions are attempted as text: the declared-MIME gate
		// already rejected the kinds every common client labels.
		return "text/plain"
	}
}

// humanBytes renders a byte count for rejection reasons (the attachments
// store's format).
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
