package fake

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/oniharnantyo/buddies/internal/store"
)

type documentPresetStore struct{ s *Store }

func (f documentPresetStore) Create(_ context.Context, p *store.DocumentPreset) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if p.ID == "" {
		p.ID = nextID()
	}
	// The format is normalized before the conflict check: it is half of the
	// (format, name) uniqueness key.
	if p.Format == "" {
		p.Format = store.DocumentPresetFormatPDF
	}
	if err := f.nameConflict("create document preset", p.ID, p.Format, p.Name); err != nil {
		return err
	}
	stamp(&p.CreatedAt, &p.UpdatedAt)
	f.s.documentPresets[p.ID] = *p
	return nil
}

func (f documentPresetStore) ByID(_ context.Context, id string) (*store.DocumentPreset, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	p, ok := f.s.documentPresets[id]
	if !ok {
		return nil, fmt.Errorf("document preset %s: %w", id, store.ErrNotFound)
	}
	return &p, nil
}

// ByName matches within the format and case-insensitively on the name,
// mirroring the sqlite adapter's unique index on (format, name) over the
// NOCASE name column.
func (f documentPresetStore) ByName(_ context.Context, format, name string) (*store.DocumentPreset, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	for _, p := range f.s.documentPresets {
		if p.Format == format && strings.EqualFold(p.Name, name) {
			out := p
			return &out, nil
		}
	}
	return nil, fmt.Errorf("document preset %s/%s: %w", format, name, store.ErrNotFound)
}

func (f documentPresetStore) List(_ context.Context, format string) ([]store.DocumentPreset, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.DocumentPreset, 0, len(f.s.documentPresets))
	for _, p := range f.s.documentPresets {
		if p.Format == format {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if strings.ToLower(out[i].Name) != strings.ToLower(out[j].Name) {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f documentPresetStore) Update(_ context.Context, p *store.DocumentPreset) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	stored, ok := f.s.documentPresets[p.ID]
	if !ok {
		return fmt.Errorf("document preset %s: %w", p.ID, store.ErrNotFound)
	}
	if err := f.nameConflict("update document preset "+p.ID, p.ID, p.Format, p.Name); err != nil {
		return err
	}
	p.CreatedAt = stored.CreatedAt
	p.UpdatedAt = nowUTC()
	f.s.documentPresets[p.ID] = *p
	return nil
}

func (f documentPresetStore) Delete(_ context.Context, id string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.documentPresets[id]; !ok {
		return fmt.Errorf("document preset %s: %w", id, store.ErrNotFound)
	}
	delete(f.s.documentPresets, id)
	return nil
}

// nameConflict rejects a name already held by another preset of the same
// format case-insensitively, mirroring the sqlite adapter's unique index on
// (format, name) over the NOCASE name column. op prefixes the ErrDuplicate
// wrap. The caller must hold f.s.mu (the store mutex is not reentrant).
func (f documentPresetStore) nameConflict(op, id, format, name string) error {
	for otherID, other := range f.s.documentPresets {
		if otherID != id && other.Format == format && strings.EqualFold(other.Name, name) {
			return fmt.Errorf("%s: %w", op, store.ErrDuplicate)
		}
	}
	return nil
}
