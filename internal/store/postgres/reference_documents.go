package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// ReferenceDocuments returns the ReferenceDocumentStore sub-port.
func (s *store) ReferenceDocuments() storeport.ReferenceDocumentStore {
	return NewReferenceDocumentStore(s.db)
}

// DocumentSections returns the DocumentSectionStore sub-port.
func (s *store) DocumentSections() storeport.DocumentSectionStore {
	return NewDocumentSectionStore(s.db)
}

// referenceDocumentStore implements storeport.ReferenceDocumentStore for
// PostgreSQL over reference_documents and its two join tables
// (add-reference-documents D7). Get is workspace-scoped so a foreign
// document is indistinguishable from an unknown one; GetByStorageKey is
// deliberately global — capability keys are random bearer tokens and are the
// wire token for serving. Every attach-list write asserts that the target
// agents/channels belong to the SAME workspace (an FK alone would let a
// foreign-workspace id attach silently).
type referenceDocumentStore struct {
	db Executor
}

// NewReferenceDocumentStore creates a new ReferenceDocumentStore with the given database executor.
func NewReferenceDocumentStore(db Executor) storeport.ReferenceDocumentStore {
	return &referenceDocumentStore{db: db}
}

// documentSectionStore implements storeport.DocumentSectionStore for
// PostgreSQL over document_sections (add-reference-documents D2/D3). The
// FTS path rides the STORED generated body_tsv column with
// websearch_to_tsquery — the decided query parser behind the tool's single
// query path.
type documentSectionStore struct {
	db Executor
}

// NewDocumentSectionStore creates a new DocumentSectionStore with the given database executor.
func NewDocumentSectionStore(db Executor) storeport.DocumentSectionStore {
	return &documentSectionStore{db: db}
}

const referenceDocumentColumns = `
	id, workspace_id, name, description, mime, size_bytes, storage_key, backend,
	page_count, scope, index_status, uploaded_by, created_at, updated_at
`

const referenceDocumentOrderBy = ` ORDER BY created_at DESC, id DESC`

func scanReferenceDocument(row pgx.Row) (*domain.ReferenceDocument, error) {
	var d domain.ReferenceDocument
	err := row.Scan(
		&d.ID,
		&d.WorkspaceID,
		&d.Name,
		&d.Description,
		&d.MimeType,
		&d.SizeBytes,
		&d.StorageKey,
		&d.Backend,
		&d.PageCount,
		&d.Scope,
		&d.IndexStatus,
		&d.UploadedBy,
		&d.CreatedAt,
		&d.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &d, nil
}

// loadJoins batch-hydrates the agent/channel attach lists for the given
// documents (sorted by id for determinism). A document with no joins gets
// empty, non-nil slices — the served JSON never shows null.
func (s *referenceDocumentStore) loadJoins(ctx context.Context, workspaceID string, docs []*domain.ReferenceDocument) error {
	if len(docs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(docs))
	byID := make(map[string]*domain.ReferenceDocument, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
		byID[doc.ID] = doc
		doc.AgentIDs = []string{}
		doc.ChannelIDs = []string{}
	}

	agentRows, err := s.db.Query(ctx, `
		SELECT document_id, agent_id
		FROM reference_document_agents
		WHERE workspace_id = $1 AND document_id = ANY($2::uuid[])
		ORDER BY document_id, agent_id
	`, workspaceID, ids)
	if err != nil {
		return convertError(err)
	}
	defer agentRows.Close()
	for agentRows.Next() {
		var docID, agentID string
		if err := agentRows.Scan(&docID, &agentID); err != nil {
			return convertError(err)
		}
		if doc, ok := byID[docID]; ok {
			doc.AgentIDs = append(doc.AgentIDs, agentID)
		}
	}
	if err := agentRows.Err(); err != nil {
		return convertError(err)
	}

	channelRows, err := s.db.Query(ctx, `
		SELECT document_id, channel_id
		FROM reference_document_channels
		WHERE workspace_id = $1 AND document_id = ANY($2::uuid[])
		ORDER BY document_id, channel_id
	`, workspaceID, ids)
	if err != nil {
		return convertError(err)
	}
	defer channelRows.Close()
	for channelRows.Next() {
		var docID, channelID string
		if err := channelRows.Scan(&docID, &channelID); err != nil {
			return convertError(err)
		}
		if doc, ok := byID[docID]; ok {
			doc.ChannelIDs = append(doc.ChannelIDs, channelID)
		}
	}
	if err := channelRows.Err(); err != nil {
		return convertError(err)
	}
	return nil
}

// assertJoinsInWorkspace verifies that every id in ids names an existing row
// of table (agents | channels) inside workspaceID — the tenancy half of FK
// parity: a foreign-workspace id must be NotFound, never silently attached.
// ids must already be deduped and non-empty.
func assertJoinsInWorkspace(ctx context.Context, db Executor, table, workspaceID string, ids []string) error {
	var count int
	query := fmt.Sprintf(`SELECT count(*) FROM %s WHERE workspace_id = $1 AND id = ANY($2::uuid[])`, table)
	if err := db.QueryRow(ctx, query, workspaceID, ids).Scan(&count); err != nil {
		return convertError(err)
	}
	if count != len(ids) {
		return fmt.Errorf("%w: %s not found in workspace", domain.ErrNotFound, table)
	}
	return nil
}

// normalizeJoinIDs dedupes, drops empties, and rejects no ids.
func normalizeJoinIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// referenceDocumentExistsLocked reports whether the workspace-scoped document
// exists (ErrNotFound otherwise) — the guard every non-SELECT path runs
// first, since UPDATE/DELETE/CTE writes cannot distinguish "no such row in
// this workspace" from "rows untouched".
func (s *referenceDocumentStore) referenceDocumentExistsLocked(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}
	var exists bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM reference_documents WHERE workspace_id = $1 AND id = $2)`, workspaceID, id).Scan(&exists)
	if err != nil {
		return convertError(err)
	}
	if !exists {
		return domain.ErrNotFound
	}
	return nil
}

// insertJoins persists one join table's rows in one statement. ids must be
// deduped and workspace-checked (empty inserts nothing).
func (s *referenceDocumentStore) insertJoins(ctx context.Context, workspaceID, documentID, table, column string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	query := fmt.Sprintf(`
		INSERT INTO %s (workspace_id, document_id, %s)
		SELECT $1, $2, x FROM unnest($3::uuid[]) AS x
	`, table, column)
	if _, err := s.db.Exec(ctx, query, workspaceID, documentID, ids); err != nil {
		return convertError(err)
	}
	return nil
}

// Create inserts one document row and its agent/channel joins. The id and
// timestamps are app-managed (store invariant 4); scope and index status
// default to attached/processing when unset.
func (s *referenceDocumentStore) Create(ctx context.Context, doc *domain.ReferenceDocument) error {
	if doc == nil || doc.WorkspaceID == "" || doc.StorageKey == "" || doc.UploadedBy == "" {
		return domain.ErrInvalid
	}
	if doc.Scope == "" {
		doc.Scope = domain.RefDocScopeAttached
	}
	if doc.IndexStatus == "" {
		doc.IndexStatus = domain.RefDocIndexProcessing
	}
	if err := domain.ValidateReferenceDocument(doc); err != nil {
		return err
	}

	agentIDs := normalizeJoinIDs(doc.AgentIDs)
	channelIDs := normalizeJoinIDs(doc.ChannelIDs)
	if len(agentIDs) > 0 {
		if err := assertJoinsInWorkspace(ctx, s.db, "agents", doc.WorkspaceID, agentIDs); err != nil {
			return err
		}
	}
	if len(channelIDs) > 0 {
		if err := assertJoinsInWorkspace(ctx, s.db, "channels", doc.WorkspaceID, channelIDs); err != nil {
			return err
		}
	}

	if doc.ID == "" {
		doc.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = now
	}
	if doc.UpdatedAt.IsZero() {
		doc.UpdatedAt = now
	}

	const query = `
		INSERT INTO reference_documents (
			id, workspace_id, name, description, mime, size_bytes, storage_key, backend,
			page_count, scope, index_status, uploaded_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	if _, err := s.db.Exec(ctx, query,
		doc.ID,
		doc.WorkspaceID,
		doc.Name,
		doc.Description,
		doc.MimeType,
		doc.SizeBytes,
		doc.StorageKey,
		doc.Backend,
		doc.PageCount,
		doc.Scope,
		doc.IndexStatus,
		doc.UploadedBy,
		doc.CreatedAt,
		doc.UpdatedAt,
	); err != nil {
		return convertError(err)
	}

	if len(agentIDs) > 0 {
		if err := s.insertJoins(ctx, doc.WorkspaceID, doc.ID, "reference_document_agents", "agent_id", agentIDs); err != nil {
			return err
		}
	}
	if len(channelIDs) > 0 {
		if err := s.insertJoins(ctx, doc.WorkspaceID, doc.ID, "reference_document_channels", "channel_id", channelIDs); err != nil {
			return err
		}
	}
	if doc.AgentIDs == nil {
		doc.AgentIDs = []string{}
	}
	if doc.ChannelIDs == nil {
		doc.ChannelIDs = []string{}
	}
	return nil
}

// Get resolves a document scoped to its workspace, joins hydrated. The
// workspace predicate is the tenancy boundary: an id belonging to another
// workspace matches nothing, so foreign and unknown are indistinguishable
// (domain.ErrNotFound — no existence leak).
func (s *referenceDocumentStore) Get(ctx context.Context, workspaceID, id string) (*domain.ReferenceDocument, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT ` + referenceDocumentColumns + `
		FROM reference_documents
		WHERE workspace_id = $1 AND id = $2
	`
	doc, err := scanReferenceDocument(s.db.QueryRow(ctx, query, workspaceID, id))
	if err != nil {
		return nil, err
	}
	if err := s.loadJoins(ctx, workspaceID, []*domain.ReferenceDocument{doc}); err != nil {
		return nil, err
	}
	return doc, nil
}

// GetByStorageKey resolves a document by its capability key. This lookup is
// deliberately GLOBAL and unauthenticated: capability keys are 128-bit random
// bearer tokens (the AttachmentStore.ByStorageKey posture) and are the wire
// token for serving. Unknown keys return domain.ErrNotFound.
func (s *referenceDocumentStore) GetByStorageKey(ctx context.Context, storageKey string) (*domain.ReferenceDocument, error) {
	if storageKey == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT ` + referenceDocumentColumns + `
		FROM reference_documents
		WHERE storage_key = $1
	`
	doc, err := scanReferenceDocument(s.db.QueryRow(ctx, query, storageKey))
	if err != nil {
		return nil, err
	}
	if err := s.loadJoins(ctx, doc.WorkspaceID, []*domain.ReferenceDocument{doc}); err != nil {
		return nil, err
	}
	return doc, nil
}

func (s *referenceDocumentStore) list(ctx context.Context, workspaceID, extraWhere string, args ...any) ([]domain.ReferenceDocument, error) {
	if workspaceID == "" {
		return []domain.ReferenceDocument{}, nil
	}
	query := `
		SELECT ` + referenceDocumentColumns + `
		FROM reference_documents
		WHERE workspace_id = $1` + extraWhere + referenceDocumentOrderBy
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	docs := make([]domain.ReferenceDocument, 0)
	for rows.Next() {
		doc, err := scanReferenceDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, *doc)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	// Pointers are taken only after the loop completes: a pointer taken inside
	// the loop aliases the slice's backing array, and any append reallocation
	// leaves earlier pointers mutating throwaway copies during join hydration.
	pointers := make([]*domain.ReferenceDocument, len(docs))
	for i := range docs {
		pointers[i] = &docs[i]
	}
	if err := s.loadJoins(ctx, workspaceID, pointers); err != nil {
		return nil, err
	}
	return docs, nil
}

// List returns the workspace's documents, newest first.
func (s *referenceDocumentStore) List(ctx context.Context, workspaceID string) ([]domain.ReferenceDocument, error) {
	return s.list(ctx, workspaceID, "", workspaceID)
}

// ListByAgent returns the workspace's documents whose agent join contains
// agentID PLUS the promoted (scope='workspace') documents — the agent lens
// the agent config modal and composer render (attached + promoted).
func (s *referenceDocumentStore) ListByAgent(ctx context.Context, workspaceID, agentID string) ([]domain.ReferenceDocument, error) {
	if workspaceID == "" || agentID == "" {
		return []domain.ReferenceDocument{}, nil
	}
	return s.list(ctx, workspaceID, `
		AND (
			scope = $3
			OR EXISTS (
				SELECT 1 FROM reference_document_agents j
				WHERE j.document_id = reference_documents.id AND j.agent_id = $2::uuid
			)
		)`, workspaceID, agentID, domain.RefDocScopeWorkspace)
}

// ListByChannel returns the workspace's documents whose channel join contains
// channelID PLUS the promoted (scope='workspace') documents — the channel
// lens the channel settings and composer render (attached + promoted).
func (s *referenceDocumentStore) ListByChannel(ctx context.Context, workspaceID, channelID string) ([]domain.ReferenceDocument, error) {
	if workspaceID == "" || channelID == "" {
		return []domain.ReferenceDocument{}, nil
	}
	return s.list(ctx, workspaceID, `
		AND (
			scope = $3
			OR EXISTS (
				SELECT 1 FROM reference_document_channels j
				WHERE j.document_id = reference_documents.id AND j.channel_id = $2::uuid
			)
		)`, workspaceID, channelID, domain.RefDocScopeWorkspace)
}

// UpdateMeta rewrites the editable bibliographic fields. Absent or foreign
// documents return domain.ErrNotFound.
func (s *referenceDocumentStore) UpdateMeta(ctx context.Context, workspaceID, id, name, description string) error {
	if err := domain.ValidateReferenceDocumentName(name); err != nil {
		return err
	}
	if err := domain.ValidateReferenceDocumentDescription(description); err != nil {
		return err
	}

	tag, err := s.db.Exec(ctx, `
		UPDATE reference_documents
		SET name = $3, description = $4, updated_at = now()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id, name, description)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetScope flips the visibility tier (promote to workspace / demote to
// attached); attach lists are left as stored. Absent or foreign documents
// return domain.ErrNotFound.
func (s *referenceDocumentStore) SetScope(ctx context.Context, workspaceID, id, scope string) error {
	if err := domain.ValidateRefDocScope(scope); err != nil {
		return err
	}

	tag, err := s.db.Exec(ctx, `
		UPDATE reference_documents
		SET scope = $3, updated_at = now()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id, scope)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetAgents replaces the agent join set in one atomic statement
// (delete-then-insert CTE). Every agent must exist in the workspace —
// unknown or foreign-workspace agents return domain.ErrNotFound.
func (s *referenceDocumentStore) SetAgents(ctx context.Context, workspaceID, id string, agentIDs []string) error {
	return s.setJoins(ctx, workspaceID, id, normalizeJoinIDs(agentIDs),
		"reference_document_agents", "agent_id", "agents", "agent")
}

// SetChannels replaces the channel join set in one atomic statement. Every
// channel must exist in the workspace — unknown or foreign-workspace channels
// return domain.ErrNotFound.
func (s *referenceDocumentStore) SetChannels(ctx context.Context, workspaceID, id string, channelIDs []string) error {
	return s.setJoins(ctx, workspaceID, id, normalizeJoinIDs(channelIDs),
		"reference_document_channels", "channel_id", "channels", "channel")
}

// setJoins is the shared set-complete join replace: verify the document and
// every target id belong to the workspace, then delete-then-insert in one
// statement so a failure never leaves a half-swapped attach list.
func (s *referenceDocumentStore) setJoins(ctx context.Context, workspaceID, id string, ids []string, joinTable, column, entityTable, entityName string) error {
	if err := s.referenceDocumentExistsLocked(ctx, workspaceID, id); err != nil {
		return err
	}
	if len(ids) > 0 {
		if err := assertJoinsInWorkspace(ctx, s.db, entityTable, workspaceID, ids); err != nil {
			return err
		}
	}
	query := fmt.Sprintf(`
		WITH del AS (
			DELETE FROM %s WHERE workspace_id = $1 AND document_id = $2
		)
		INSERT INTO %s (workspace_id, document_id, %s)
		SELECT $1, $2, x FROM unnest($3::uuid[]) AS x
	`, joinTable, joinTable, column)
	if _, err := s.db.Exec(ctx, query, workspaceID, id, ids); err != nil {
		return convertError(err)
	}
	return nil
}

// ReplaceBlob swaps the blob identity after a re-upload: mime, size, storage
// key, backend, page count, and derived index status are rewritten; scope and
// the attach lists are left as stored. The caller rebuilds the section index
// separately (ReplaceForDocument). A colliding storage key surfaces as
// domain.ErrConflict; absent or foreign documents as domain.ErrNotFound.
func (s *referenceDocumentStore) ReplaceBlob(ctx context.Context, workspaceID, id string, mimeType string, sizeBytes int64, storageKey, backend string, pageCount int, indexStatus string) error {
	if storageKey == "" || backend == "" {
		return fmt.Errorf("%w: replacement blob needs a storage key and backend", domain.ErrInvalid)
	}
	if !domain.ValidReferenceDocMime(mimeType) {
		return fmt.Errorf("%w: unsupported reference document type %q", domain.ErrInvalid, mimeType)
	}
	if err := domain.ValidateRefDocIndexStatus(indexStatus); err != nil {
		return err
	}
	if sizeBytes < 0 || pageCount < 0 {
		return fmt.Errorf("%w: replacement blob size and page count must not be negative", domain.ErrInvalid)
	}

	tag, err := s.db.Exec(ctx, `
		UPDATE reference_documents
		SET mime = $3, size_bytes = $4, storage_key = $5, backend = $6,
		    page_count = $7, index_status = $8, updated_at = now()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id, mimeType, sizeBytes, storageKey, backend, pageCount, indexStatus)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Delete removes the document; its join rows and sections die with it (ON
// DELETE CASCADE). The caller deletes the blob bytes. Absent or foreign
// documents return domain.ErrNotFound.
func (s *referenceDocumentStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	tag, err := s.db.Exec(ctx, `
		DELETE FROM reference_documents
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ReplaceForDocument replaces the document's entire section set in one
// atomic statement (delete-then-insert CTE): the stored rows become exactly
// sections. IDs are assigned when empty. An absent or foreign document
// returns domain.ErrNotFound (the FK alone cannot — a CTE delete of zero rows
// is silent).
func (ds *documentSectionStore) ReplaceForDocument(ctx context.Context, workspaceID, documentID string, sections []domain.DocumentSection) error {
	if workspaceID == "" {
		return domain.ErrInvalid
	}

	var exists bool
	err := ds.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM reference_documents WHERE workspace_id = $1 AND id = $2)`, workspaceID, documentID).Scan(&exists)
	if err != nil {
		return convertError(err)
	}
	if !exists {
		return domain.ErrNotFound
	}

	ids := make([]string, 0, len(sections))
	headings := make([]string, 0, len(sections))
	locators := make([]string, 0, len(sections))
	kinds := make([]string, 0, len(sections))
	levels := make([]int32, 0, len(sections))
	ordinals := make([]int32, 0, len(sections))
	bodies := make([]string, 0, len(sections))
	for i := range sections {
		section := sections[i]
		section.DocumentID = documentID
		if section.ID == "" {
			section.ID = uuid.NewString()
		}
		if err := domain.ValidateDocumentSection(&section); err != nil {
			return err
		}
		ids = append(ids, section.ID)
		headings = append(headings, section.Heading)
		locators = append(locators, section.Locator)
		kinds = append(kinds, section.LocatorKind)
		levels = append(levels, int32(section.Level))
		ordinals = append(ordinals, int32(section.Ordinal))
		bodies = append(bodies, section.Body)
	}

	const query = `
		WITH del AS (
			DELETE FROM document_sections WHERE workspace_id = $1 AND document_id = $2
		)
		INSERT INTO document_sections (id, workspace_id, document_id, heading, locator, locator_kind, level, ordinal, body)
		SELECT u.id, $1, $2, u.heading, u.locator, u.locator_kind, u.level, u.ordinal, u.body
		FROM unnest($3::uuid[], $4::text[], $5::text[], $6::text[], $7::int[], $8::int[], $9::text[])
		     AS u(id, heading, locator, locator_kind, level, ordinal, body)
	`
	if _, err := ds.db.Exec(ctx, query,
		workspaceID, documentID,
		ids, headings, locators, kinds, levels, ordinals, bodies,
	); err != nil {
		return convertError(err)
	}
	return nil
}

// DeleteForDocument drops the document's sections. Absent documents leave
// nothing to drop — no error (the caller typically just deleted the document,
// whose rows already cascaded).
func (ds *documentSectionStore) DeleteForDocument(ctx context.Context, workspaceID, documentID string) error {
	if workspaceID == "" || documentID == "" {
		return nil
	}
	_, err := ds.db.Exec(ctx, `
		DELETE FROM document_sections
		WHERE workspace_id = $1 AND document_id = $2
	`, workspaceID, documentID)
	if err != nil {
		return convertError(err)
	}
	return nil
}

// ListForDocument returns the document's sections in ordinal order — the
// compose-side read the manifest's table-of-contents projection (D6) is
// built from. Workspace-scoped: an absent or foreign document returns
// domain.ErrNotFound, indistinguishably.
func (ds *documentSectionStore) ListForDocument(ctx context.Context, workspaceID, documentID string) ([]domain.DocumentSection, error) {
	if workspaceID == "" || documentID == "" {
		return nil, domain.ErrNotFound
	}

	const query = `
		SELECT id, document_id, heading, locator, locator_kind, level, ordinal, body
		FROM document_sections
		WHERE workspace_id = $1 AND document_id = $2
		ORDER BY ordinal ASC, id ASC
	`
	rows, err := ds.db.Query(ctx, query, workspaceID, documentID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	sections := make([]domain.DocumentSection, 0)
	for rows.Next() {
		var sec domain.DocumentSection
		if err := rows.Scan(&sec.ID, &sec.DocumentID, &sec.Heading, &sec.Locator, &sec.LocatorKind, &sec.Level, &sec.Ordinal, &sec.Body); err != nil {
			return nil, convertError(err)
		}
		sections = append(sections, sec)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	// Tenancy parity with Get: zero rows for an id that is absent or belongs
	// to another workspace is NotFound, never an empty success — callers
	// cannot otherwise distinguish an empty index from an unresolvable
	// document.
	var exists bool
	if err := ds.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM reference_documents WHERE workspace_id = $1 AND id = $2)`, workspaceID, documentID).Scan(&exists); err != nil {
		return nil, convertError(err)
	}
	if !exists {
		return nil, domain.ErrNotFound
	}
	return sections, nil
}

// Search runs the full-text query over the workspace's sections of the given
// documents, best match first (ts_rank, then ordinal). documentIDs is the
// run's visibility filter — an empty slice returns an empty result and never
// degrades to an unfiltered query (D7). Snippets come from ts_headline.
func (ds *documentSectionStore) Search(ctx context.Context, workspaceID string, documentIDs []string, query string, limit int) ([]domain.DocumentSectionHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: search query is empty", domain.ErrInvalid)
	}
	// The visibility filter never degrades to an unfiltered query (D7).
	visible := normalizeJoinIDs(documentIDs)
	if workspaceID == "" || len(visible) == 0 {
		return []domain.DocumentSectionHit{}, nil
	}

	sql := `
		SELECT s.document_id, d.name, s.heading, s.locator, s.locator_kind,
			ts_headline('english', s.body, q.tsq, 'MaxFragments=1,MaxWords=24,MinWords=10') AS snippet
		FROM document_sections s
		JOIN reference_documents d ON d.id = s.document_id
		CROSS JOIN (SELECT websearch_to_tsquery('english', $2) AS tsq) q
		WHERE s.workspace_id = $1
		  AND s.document_id = ANY($3::uuid[])
		  AND s.body_tsv @@ q.tsq
		ORDER BY ts_rank(s.body_tsv, q.tsq) DESC, s.ordinal ASC
	`
	args := []any{workspaceID, query, visible}
	if limit > 0 {
		args = append(args, limit)
		sql += fmt.Sprintf(` LIMIT $%d`, len(args))
	}

	rows, err := ds.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	hits := make([]domain.DocumentSectionHit, 0)
	for rows.Next() {
		var hit domain.DocumentSectionHit
		if err := rows.Scan(&hit.DocumentID, &hit.DocumentName, &hit.Heading, &hit.Locator, &hit.LocatorKind, &hit.Snippet); err != nil {
			return nil, convertError(err)
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return hits, nil
}
