package sqlite

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type documentPresetStore struct{ e executor }

const documentPresetColumns = `id, name, format, config, created_at, updated_at`

func (s *documentPresetStore) Create(ctx context.Context, p *store.DocumentPreset) error {
	if p.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("create document preset: %w", err)
		}
		p.ID = id
	}
	// The v1 default is stamped before the insert: the format is half of the
	// (format, name) uniqueness key the unique index enforces.
	if p.Format == "" {
		p.Format = store.DocumentPresetFormatPDF
	}
	stampCreated(&p.CreatedAt, &p.UpdatedAt)

	_, err := s.e.ExecContext(ctx, `
INSERT INTO document_presets (`+documentPresetColumns+`)
VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Format, p.Config, formatTime(p.CreatedAt), formatTime(p.UpdatedAt),
	)
	if err != nil {
		// The unique index on (format, name) rides the name column's NOCASE
		// collation, so the violation fires on case-insensitive duplicates
		// within the format too.
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("create document preset: %w", store.ErrDuplicate)
		}
		return fmt.Errorf("create document preset: %w", err)
	}
	return nil
}

func (s *documentPresetStore) ByID(ctx context.Context, id string) (*store.DocumentPreset, error) {
	row := s.e.QueryRowContext(ctx, `SELECT `+documentPresetColumns+` FROM document_presets WHERE id = ?`, id)
	p, err := scanDocumentPreset(row)
	if err != nil {
		return nil, readErr("get document preset", err)
	}
	return p, nil
}

// ByName matches within the format; the name column carries COLLATE NOCASE,
// so the comparison is case-insensitive.
func (s *documentPresetStore) ByName(ctx context.Context, format, name string) (*store.DocumentPreset, error) {
	row := s.e.QueryRowContext(ctx, `
SELECT `+documentPresetColumns+` FROM document_presets
WHERE format = ? AND name = ?`, format, name)
	p, err := scanDocumentPreset(row)
	if err != nil {
		return nil, readErr("get document preset", err)
	}
	return p, nil
}

func (s *documentPresetStore) List(ctx context.Context, format string) ([]store.DocumentPreset, error) {
	// name carries COLLATE NOCASE, so the sort is case-insensitive.
	rows, err := s.e.QueryContext(ctx, `
SELECT `+documentPresetColumns+` FROM document_presets
WHERE format = ? ORDER BY name`, format)
	if err != nil {
		return nil, fmt.Errorf("list document presets: %w", err)
	}
	defer rows.Close()

	presets := make([]store.DocumentPreset, 0)
	for rows.Next() {
		p, err := scanDocumentPreset(rows)
		if err != nil {
			return nil, fmt.Errorf("list document presets: %w", err)
		}
		presets = append(presets, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list document presets: %w", err)
	}
	return presets, nil
}

func (s *documentPresetStore) Update(ctx context.Context, p *store.DocumentPreset) error {
	p.UpdatedAt = nowUTC()
	res, err := s.e.ExecContext(ctx, `
UPDATE document_presets SET name = ?, format = ?, config = ?, updated_at = ?
WHERE id = ?`,
		p.Name, p.Format, p.Config, formatTime(p.UpdatedAt), p.ID,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("update document preset %s: %w", p.ID, store.ErrDuplicate)
		}
		return fmt.Errorf("update document preset %s: %w", p.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update document preset %s: %w", p.ID, store.ErrNotFound)
	}
	return nil
}

func (s *documentPresetStore) Delete(ctx context.Context, id string) error {
	res, err := s.e.ExecContext(ctx, `DELETE FROM document_presets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete document preset: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete document preset %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// scanDocumentPreset scans one document_presets row from a single- or
// multi-row result.
func scanDocumentPreset(row scanner) (*store.DocumentPreset, error) {
	var p store.DocumentPreset
	var createdAt, updatedAt string

	if err := row.Scan(&p.ID, &p.Name, &p.Format, &p.Config, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	var err error
	if p.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if p.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}
