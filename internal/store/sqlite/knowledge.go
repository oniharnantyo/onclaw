package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type documentStore struct{ e executor }

const documentColumns = `id, filename, mime, size, scope, agent_id, agent_slug,
description, source, created_at`

func (s *documentStore) PutDocument(ctx context.Context, doc store.Document) (store.Document, error) {
	if doc.ID == "" {
		id, err := newID()
		if err != nil {
			return store.Document{}, fmt.Errorf("put document: %w", err)
		}
		doc.ID = id
	}
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = nowUTC()
	}

	// agent_id is NULL on global rows; the agent linkage (and its cascade)
	// only exists on agent-scoped ones.
	var agentID any
	if doc.Scope == "agent" {
		agentID = doc.AgentID
	}

	_, err := s.e.ExecContext(ctx, `
INSERT INTO documents (`+documentColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.Filename, doc.MIME, doc.Size, doc.Scope, agentID, doc.AgentSlug,
		doc.Description, doc.Source, formatTime(doc.CreatedAt),
	)
	if err != nil {
		// The composite UNIQUE (scope, agent_slug, filename) carries the
		// per-scope filename uniqueness.
		if isUniqueConstraintErr(err) {
			return store.Document{}, fmt.Errorf("put document %s: %w", doc.Filename, store.ErrDuplicate)
		}
		return store.Document{}, fmt.Errorf("put document %s: %w", doc.Filename, err)
	}
	return doc, nil
}

func (s *documentStore) ListDocuments(ctx context.Context) ([]store.Document, error) {
	rows, err := s.e.QueryContext(ctx, `SELECT `+documentColumns+` FROM documents ORDER BY filename`)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()

	docs := make([]store.Document, 0)
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, fmt.Errorf("list documents: %w", err)
		}
		docs = append(docs, *doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	return docs, nil
}

func (s *documentStore) GetDocument(ctx context.Context, id string) (store.Document, error) {
	row := s.e.QueryRowContext(ctx, `SELECT `+documentColumns+` FROM documents WHERE id = ?`, id)
	doc, err := scanDocument(row)
	if err != nil {
		return store.Document{}, readErr("get document", err)
	}
	return *doc, nil
}

func (s *documentStore) DocumentByName(ctx context.Context, scope, agentSlug, filename string) (store.Document, bool, error) {
	row := s.e.QueryRowContext(ctx, `
SELECT `+documentColumns+` FROM documents
WHERE scope = ? AND agent_slug = ? AND filename = ?`, scope, agentSlug, filename)
	doc, err := scanDocument(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Document{}, false, nil
		}
		return store.Document{}, false, fmt.Errorf("get document by name: %w", err)
	}
	return *doc, true, nil
}

func (s *documentStore) UpdateDocumentScope(ctx context.Context, id, scope, agentID, agentSlug string) error {
	// agent_id is NULL on global rows (see PutDocument).
	var fkAgentID any
	if scope == "agent" {
		fkAgentID = agentID
	}
	res, err := s.e.ExecContext(ctx, `
UPDATE documents SET scope = ?, agent_id = ?, agent_slug = ? WHERE id = ?`,
		scope, fkAgentID, agentSlug, id,
	)
	if err != nil {
		return fmt.Errorf("update document scope %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update document scope %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *documentStore) UpdateDocumentDescription(ctx context.Context, id, description string) error {
	res, err := s.e.ExecContext(ctx, `
UPDATE documents SET description = ? WHERE id = ?`, description, id)
	if err != nil {
		return fmt.Errorf("update document description %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update document description %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *documentStore) DeleteDocument(ctx context.Context, id string) error {
	res, err := s.e.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete document %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// scanDocument scans one documents row from a single- or multi-row result.
func scanDocument(row scanner) (*store.Document, error) {
	var doc store.Document
	var agentID sql.NullString
	var createdAt string

	if err := row.Scan(&doc.ID, &doc.Filename, &doc.MIME, &doc.Size, &doc.Scope,
		&agentID, &doc.AgentSlug, &doc.Description, &doc.Source, &createdAt); err != nil {
		return nil, err
	}
	doc.AgentID = agentID.String
	var err error
	if doc.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &doc, nil
}
