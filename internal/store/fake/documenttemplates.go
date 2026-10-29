package fake

import (
	"context"
	"fmt"
	"os"

	"github.com/oniharnantyo/buddies/internal/store"
)

// The fake's DocumentTemplateStore: the template store is filesystem-backed
// (it needs a data directory, not the database), so the fake materializes one
// over a temporary directory created on first use and delegates to the real
// implementation — Path must return a readable path exactly like production,
// and Save/Path/Delete then carry the production semantics for free.
type documentTemplateStore struct {
	st *Store
}

// DocumentTemplates returns the fake's template store over the aggregate.
func (s *Store) DocumentTemplates() store.DocumentTemplateStore {
	return &documentTemplateStore{st: s}
}

// underlying lazily creates the backing directory and returns the real store
// rooted at it.
func (d *documentTemplateStore) underlying() (store.DocumentTemplateStore, error) {
	d.st.mu.Lock()
	defer d.st.mu.Unlock()
	if d.st.templateDir == "" {
		dir, err := os.MkdirTemp("", "buddies-fake-templates")
		if err != nil {
			return nil, fmt.Errorf("fake: create template directory: %w", err)
		}
		d.st.templateDir = dir
	}
	return store.NewDocumentTemplateStore(d.st.templateDir), nil
}

// Save implements store.DocumentTemplateStore.
func (d *documentTemplateStore) Save(ctx context.Context, presetID, filename string, data []byte) (string, error) {
	underlying, err := d.underlying()
	if err != nil {
		return "", err
	}
	return underlying.Save(ctx, presetID, filename, data)
}

// Path implements store.DocumentTemplateStore.
func (d *documentTemplateStore) Path(ctx context.Context, presetID string) (string, error) {
	underlying, err := d.underlying()
	if err != nil {
		return "", err
	}
	return underlying.Path(ctx, presetID)
}

// Delete implements store.DocumentTemplateStore.
func (d *documentTemplateStore) Delete(ctx context.Context, presetID string) error {
	underlying, err := d.underlying()
	if err != nil {
		return err
	}
	return underlying.Delete(ctx, presetID)
}
