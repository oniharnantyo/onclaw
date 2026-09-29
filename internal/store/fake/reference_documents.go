package fake

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// -------------------------------------------------------------------------
// ReferenceDocumentStore + DocumentSectionStore implementations
// (add-reference-documents). FK parity mirrors the postgres adapter: Create
// requires the workspace and uploader to exist, and every attached agent or
// channel to exist in the SAME workspace (a foreign-workspace id is
// NotFound, never silently attached). GetByStorageKey is deliberately
// global — the capability key is the bearer token for serving. Search is a
// deterministic naive scan standing in for Postgres FTS: every
// whitespace-split term must appear in the body (case-insensitive), with a
// ±60-char snippet around the first term occurrence.
// -------------------------------------------------------------------------

func cloneReferenceDocument(d *domain.ReferenceDocument) *domain.ReferenceDocument {
	if d == nil {
		return nil
	}
	cp := *d
	cp.AgentIDs = cloneStringSlice(d.AgentIDs)
	cp.ChannelIDs = cloneStringSlice(d.ChannelIDs)
	return &cp
}

func cloneDocumentSections(sections []domain.DocumentSection) []domain.DocumentSection {
	if sections == nil {
		return nil
	}
	cp := make([]domain.DocumentSection, len(sections))
	copy(cp, sections)
	return cp
}

type referenceDocumentStore struct {
	s *fakeStore
}

// referenceDocumentJoinsLocked validates FK parity for the attach lists and
// returns normalized (deduped, sorted, non-empty) copies. Caller holds the
// lock; workspaceID scopes the existence checks (tenancy parity with the
// postgres adapter's count checks).
func referenceDocumentJoinsLocked(s *fakeStore, workspaceID string, ids []string, kind string) ([]string, error) {
	normalized := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		switch kind {
		case "agent":
			a, exists := s.agents[id]
			if !exists || a.WorkspaceID != workspaceID {
				return nil, fmt.Errorf("%w: agent %q not found in workspace", domain.ErrNotFound, id)
			}
		case "channel":
			c, exists := s.channels.channels[id]
			if !exists || c.WorkspaceID != workspaceID {
				return nil, fmt.Errorf("%w: channel %q not found in workspace", domain.ErrNotFound, id)
			}
		}
		normalized = append(normalized, id)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func (rs *referenceDocumentStore) Create(ctx context.Context, doc *domain.ReferenceDocument) error {
	if doc == nil || doc.WorkspaceID == "" || doc.StorageKey == "" || doc.UploadedBy == "" {
		return domain.ErrInvalid
	}
	// Defaults before validation (mirrors the postgres adapter).
	if doc.Scope == "" {
		doc.Scope = domain.RefDocScopeAttached
	}
	if doc.IndexStatus == "" {
		doc.IndexStatus = domain.RefDocIndexProcessing
	}
	if err := domain.ValidateReferenceDocument(doc); err != nil {
		return err
	}

	rs.s.mu.Lock()
	defer rs.s.mu.Unlock()

	if _, exists := rs.s.workspaces[doc.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if _, exists := rs.s.users[doc.UploadedBy]; !exists {
		return fmt.Errorf("%w: user not found", domain.ErrNotFound)
	}

	agentIDs, err := referenceDocumentJoinsLocked(rs.s, doc.WorkspaceID, doc.AgentIDs, "agent")
	if err != nil {
		return err
	}
	channelIDs, err := referenceDocumentJoinsLocked(rs.s, doc.WorkspaceID, doc.ChannelIDs, "channel")
	if err != nil {
		return err
	}

	if _, exists := rs.s.referenceDocumentsByStorageKey[doc.StorageKey]; exists {
		return fmt.Errorf("%w: reference document with storage key %q already exists", domain.ErrConflict, doc.StorageKey)
	}

	if doc.ID != "" {
		if _, exists := rs.s.referenceDocuments[doc.ID]; exists {
			return fmt.Errorf("%w: reference document with id %q already exists", domain.ErrConflict, doc.ID)
		}
	} else {
		doc.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = now
	}
	if doc.UpdatedAt.IsZero() {
		doc.UpdatedAt = now
	}
	if doc.AgentIDs == nil {
		doc.AgentIDs = []string{}
	}
	if doc.ChannelIDs == nil {
		doc.ChannelIDs = []string{}
	}
	doc.AgentIDs = agentIDs
	doc.ChannelIDs = channelIDs

	rs.s.referenceDocuments[doc.ID] = cloneReferenceDocument(doc)
	rs.s.referenceDocumentsByStorageKey[doc.StorageKey] = doc.ID
	return nil
}

// referenceDocumentLocked resolves the workspace-scoped document: a foreign
// id is indistinguishable from an unknown one (domain.ErrNotFound).
func referenceDocumentLocked(s *fakeStore, workspaceID, id string) (*domain.ReferenceDocument, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}
	doc, exists := s.referenceDocuments[id]
	if !exists || doc.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return doc, nil
}

func (rs *referenceDocumentStore) Get(ctx context.Context, workspaceID, id string) (*domain.ReferenceDocument, error) {
	rs.s.mu.RLock()
	defer rs.s.mu.RUnlock()

	doc, err := referenceDocumentLocked(rs.s, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return cloneReferenceDocument(doc), nil
}

func (rs *referenceDocumentStore) GetByStorageKey(ctx context.Context, storageKey string) (*domain.ReferenceDocument, error) {
	if storageKey == "" {
		return nil, domain.ErrNotFound
	}

	rs.s.mu.RLock()
	defer rs.s.mu.RUnlock()

	id, exists := rs.s.referenceDocumentsByStorageKey[storageKey]
	if !exists {
		return nil, domain.ErrNotFound
	}
	doc, exists := rs.s.referenceDocuments[id]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneReferenceDocument(doc), nil
}

func (rs *referenceDocumentStore) listLocked(workspaceID string, filter func(*domain.ReferenceDocument) bool) []domain.ReferenceDocument {
	docs := make([]domain.ReferenceDocument, 0)
	for _, doc := range rs.s.referenceDocuments {
		if doc.WorkspaceID != workspaceID {
			continue
		}
		if filter != nil && !filter(doc) {
			continue
		}
		docs = append(docs, *cloneReferenceDocument(doc))
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].CreatedAt.Equal(docs[j].CreatedAt) {
			return docs[i].ID > docs[j].ID
		}
		return docs[i].CreatedAt.After(docs[j].CreatedAt)
	})
	return docs
}

func (rs *referenceDocumentStore) List(ctx context.Context, workspaceID string) ([]domain.ReferenceDocument, error) {
	if workspaceID == "" {
		return []domain.ReferenceDocument{}, nil
	}

	rs.s.mu.RLock()
	defer rs.s.mu.RUnlock()

	return rs.listLocked(workspaceID, nil), nil
}

func (rs *referenceDocumentStore) ListByAgent(ctx context.Context, workspaceID, agentID string) ([]domain.ReferenceDocument, error) {
	if workspaceID == "" || agentID == "" {
		return []domain.ReferenceDocument{}, nil
	}

	rs.s.mu.RLock()
	defer rs.s.mu.RUnlock()

	// The agent lens is attached + promoted: every agent-joined document plus
	// every promoted (workspace-scope) document. listLocked visits each
	// document once, so the union is naturally deduped and stable-ordered.
	return rs.listLocked(workspaceID, func(doc *domain.ReferenceDocument) bool {
		if doc.Scope == domain.RefDocScopeWorkspace {
			return true
		}
		for _, id := range doc.AgentIDs {
			if id == agentID {
				return true
			}
		}
		return false
	}), nil
}

func (rs *referenceDocumentStore) ListByChannel(ctx context.Context, workspaceID, channelID string) ([]domain.ReferenceDocument, error) {
	if workspaceID == "" || channelID == "" {
		return []domain.ReferenceDocument{}, nil
	}

	rs.s.mu.RLock()
	defer rs.s.mu.RUnlock()

	return rs.listLocked(workspaceID, func(doc *domain.ReferenceDocument) bool {
		if doc.Scope == domain.RefDocScopeWorkspace {
			return true
		}
		for _, id := range doc.ChannelIDs {
			if id == channelID {
				return true
			}
		}
		return false
	}), nil
}

func (rs *referenceDocumentStore) UpdateMeta(ctx context.Context, workspaceID, id, name, description string) error {
	if err := domain.ValidateReferenceDocumentName(name); err != nil {
		return err
	}
	if err := domain.ValidateReferenceDocumentDescription(description); err != nil {
		return err
	}

	rs.s.mu.Lock()
	defer rs.s.mu.Unlock()

	doc, err := referenceDocumentLocked(rs.s, workspaceID, id)
	if err != nil {
		return err
	}
	doc.Name = name
	doc.Description = description
	doc.UpdatedAt = time.Now().UTC()
	return nil
}

func (rs *referenceDocumentStore) SetScope(ctx context.Context, workspaceID, id, scope string) error {
	if err := domain.ValidateRefDocScope(scope); err != nil {
		return err
	}

	rs.s.mu.Lock()
	defer rs.s.mu.Unlock()

	doc, err := referenceDocumentLocked(rs.s, workspaceID, id)
	if err != nil {
		return err
	}
	doc.Scope = scope
	doc.UpdatedAt = time.Now().UTC()
	return nil
}

func (rs *referenceDocumentStore) SetAgents(ctx context.Context, workspaceID, id string, agentIDs []string) error {
	rs.s.mu.Lock()
	defer rs.s.mu.Unlock()

	doc, err := referenceDocumentLocked(rs.s, workspaceID, id)
	if err != nil {
		return err
	}
	normalized, err := referenceDocumentJoinsLocked(rs.s, workspaceID, agentIDs, "agent")
	if err != nil {
		return err
	}
	doc.AgentIDs = normalized
	doc.UpdatedAt = time.Now().UTC()
	return nil
}

func (rs *referenceDocumentStore) SetChannels(ctx context.Context, workspaceID, id string, channelIDs []string) error {
	rs.s.mu.Lock()
	defer rs.s.mu.Unlock()

	doc, err := referenceDocumentLocked(rs.s, workspaceID, id)
	if err != nil {
		return err
	}
	normalized, err := referenceDocumentJoinsLocked(rs.s, workspaceID, channelIDs, "channel")
	if err != nil {
		return err
	}
	doc.ChannelIDs = normalized
	doc.UpdatedAt = time.Now().UTC()
	return nil
}

func (rs *referenceDocumentStore) ReplaceBlob(ctx context.Context, workspaceID, id string, mimeType string, sizeBytes int64, storageKey, backend string, pageCount int, indexStatus string) error {
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

	rs.s.mu.Lock()
	defer rs.s.mu.Unlock()

	doc, err := referenceDocumentLocked(rs.s, workspaceID, id)
	if err != nil {
		return err
	}
	if other, exists := rs.s.referenceDocumentsByStorageKey[storageKey]; exists && other != id {
		return fmt.Errorf("%w: reference document with storage key %q already exists", domain.ErrConflict, storageKey)
	}

	delete(rs.s.referenceDocumentsByStorageKey, doc.StorageKey)
	doc.MimeType = mimeType
	doc.SizeBytes = sizeBytes
	doc.StorageKey = storageKey
	doc.Backend = backend
	doc.PageCount = pageCount
	doc.IndexStatus = indexStatus
	doc.UpdatedAt = time.Now().UTC()
	rs.s.referenceDocumentsByStorageKey[storageKey] = doc.ID
	return nil
}

func (rs *referenceDocumentStore) Delete(ctx context.Context, workspaceID, id string) error {
	rs.s.mu.Lock()
	defer rs.s.mu.Unlock()

	doc, err := referenceDocumentLocked(rs.s, workspaceID, id)
	if err != nil {
		return err
	}
	delete(rs.s.referenceDocuments, doc.ID)
	delete(rs.s.referenceDocumentsByStorageKey, doc.StorageKey)
	delete(rs.s.referenceDocumentSections, doc.ID)
	return nil
}

type documentSectionStore struct {
	s *fakeStore
}

func (ds *documentSectionStore) ReplaceForDocument(ctx context.Context, workspaceID, documentID string, sections []domain.DocumentSection) error {
	if workspaceID == "" {
		return domain.ErrInvalid
	}

	ds.s.mu.Lock()
	defer ds.s.mu.Unlock()

	if _, err := referenceDocumentLocked(ds.s, workspaceID, documentID); err != nil {
		return err
	}

	stored := make([]domain.DocumentSection, 0, len(sections))
	for i := range sections {
		section := sections[i]
		section.DocumentID = documentID
		if section.ID == "" {
			section.ID = uuid.NewString()
		}
		if err := domain.ValidateDocumentSection(&section); err != nil {
			return err
		}
		stored = append(stored, section)
	}
	ds.s.referenceDocumentSections[documentID] = stored
	return nil
}

func (ds *documentSectionStore) DeleteForDocument(ctx context.Context, workspaceID, documentID string) error {
	if workspaceID == "" || documentID == "" {
		return nil
	}

	ds.s.mu.Lock()
	defer ds.s.mu.Unlock()

	delete(ds.s.referenceDocumentSections, documentID)
	return nil
}

func (ds *documentSectionStore) Search(ctx context.Context, workspaceID string, documentIDs []string, query string, limit int) ([]domain.DocumentSectionHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: search query is empty", domain.ErrInvalid)
	}
	if workspaceID == "" || len(documentIDs) == 0 {
		// The visibility filter never degrades to an unfiltered query (D7).
		return []domain.DocumentSectionHit{}, nil
	}

	visible := make(map[string]struct{}, len(documentIDs))
	for _, id := range documentIDs {
		if id != "" {
			visible[id] = struct{}{}
		}
	}

	terms := strings.Fields(strings.ToLower(query))
	firstTerm := terms[0]

	ds.s.mu.RLock()
	defer ds.s.mu.RUnlock()

	hits := make([]domain.DocumentSectionHit, 0)
	// Deterministic order: document id, then ordinal (the postgres adapter
	// ranks by ts_rank; the fake stands in with scan order determinism).
	docIDs := make([]string, 0, len(visible))
	for id := range visible {
		docIDs = append(docIDs, id)
	}
	sort.Strings(docIDs)

	for _, docID := range docIDs {
		doc, err := referenceDocumentLocked(ds.s, workspaceID, docID)
		if err != nil {
			continue // foreign/unknown ids are simply absent, never an error
		}
		for _, section := range ds.s.referenceDocumentSections[docID] {
			body := strings.ToLower(section.Body)
			match := true
			for _, term := range terms {
				if !strings.Contains(body, term) {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			hits = append(hits, domain.DocumentSectionHit{
				DocumentID:   docID,
				DocumentName: doc.Name,
				Heading:      section.Heading,
				Locator:      section.Locator,
				LocatorKind:  section.LocatorKind,
				Snippet:      naiveSnippet(section.Body, firstTerm),
			})
		}
	}

	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// ListForDocument returns the document's sections in ordinal order. A
// foreign or unknown document is domain.ErrNotFound, indistinguishably
// (tenancy parity with Get).
func (ds *documentSectionStore) ListForDocument(ctx context.Context, workspaceID, documentID string) ([]domain.DocumentSection, error) {
	if workspaceID == "" || documentID == "" {
		return nil, domain.ErrNotFound
	}

	ds.s.mu.RLock()
	defer ds.s.mu.RUnlock()

	if _, err := referenceDocumentLocked(ds.s, workspaceID, documentID); err != nil {
		return nil, err
	}
	stored := cloneDocumentSections(ds.s.referenceDocumentSections[documentID])
	if stored == nil {
		stored = []domain.DocumentSection{}
	}
	sort.Slice(stored, func(i, j int) bool {
		if stored[i].Ordinal == stored[j].Ordinal {
			return stored[i].ID < stored[j].ID
		}
		return stored[i].Ordinal < stored[j].Ordinal
	})
	return stored, nil
}

// naiveSnippet renders ±60 characters around the first case-insensitive
// occurrence of term, clamped to the body bounds.
func naiveSnippet(body, term string) string {
	if term == "" {
		return body
	}
	lower := strings.ToLower(body)
	idx := strings.Index(lower, strings.ToLower(term))
	if idx < 0 {
		return body
	}
	start := idx - 60
	if start < 0 {
		start = 0
	}
	end := idx + len(term) + 60
	if end > len(body) {
		end = len(body)
	}
	return body[start:end]
}
