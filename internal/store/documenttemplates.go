package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DocumentTemplateStore persists the template documents of
// template-document presets (add-document-editing design D6): a DOCX or
// PPTX preset's uploaded skeleton is a binary document — it cannot live in
// the preset row's JSON config, so it is stored on disk under the app data
// directory, one file per preset at
// <dataDir>/presets/templates/<presetID><ext>. The store is
// constructor-injected beside the SQL-backed stores (it needs the data
// directory, not the database); deleting the owning preset deletes its
// template file through Delete.
//
// One template per preset: a Save replaces whatever extension was stored
// before.
type DocumentTemplateStore interface {
	// Save writes the preset's template document, deriving the file
	// extension from filename, and replaces any template previously stored
	// for the preset. It returns the stored file's absolute path.
	Save(ctx context.Context, presetID, filename string, data []byte) (string, error)
	// Path returns the stored template's absolute path; ErrNotFound when
	// the preset has no template on disk.
	Path(ctx context.Context, presetID string) (string, error)
	// Delete removes the preset's template file. Deleting a preset with no
	// template on disk is not an error — the delete path of a preset whose
	// upload never happened must still succeed.
	Delete(ctx context.Context, presetID string) error
}

// templateDirPerm and templateFilePerm match the workspaces package's
// private-to-user modes: templates are the user's uploaded documents.
const (
	templateDirPerm  os.FileMode = 0o700
	templateFilePerm os.FileMode = 0o600
)

// NewDocumentTemplateStore returns the on-disk template store rooted at
// dataDir (the config layout's root, ~/.buddies in production). Nothing is
// created until the first Save.
func NewDocumentTemplateStore(dataDir string) DocumentTemplateStore {
	return &documentTemplateStore{dir: filepath.Join(dataDir, "presets", "templates")}
}

// documentTemplateStore is the filesystem DocumentTemplateStore.
type documentTemplateStore struct {
	dir string
}

// Save implements DocumentTemplateStore.
func (s *documentTemplateStore) Save(_ context.Context, presetID, filename string, data []byte) (string, error) {
	ext, err := templateExt(presetID, filename)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.dir, templateDirPerm); err != nil {
		return "", fmt.Errorf("document template store: create %s: %w", s.dir, err)
	}
	// One template per preset: drop whatever extension is on disk before
	// the new one lands, so Path can never see two candidates.
	if err := s.removeExisting(presetID); err != nil {
		return "", err
	}
	path := filepath.Join(s.dir, presetID+ext)
	if err := os.WriteFile(path, data, templateFilePerm); err != nil {
		return "", fmt.Errorf("document template store: write %s: %w", path, err)
	}
	return path, nil
}

// Path implements DocumentTemplateStore.
func (s *documentTemplateStore) Path(_ context.Context, presetID string) (string, error) {
	if err := validTemplatePresetID(presetID); err != nil {
		return "", err
	}
	matches, err := filepath.Glob(filepath.Join(s.dir, presetID+".*"))
	if err != nil {
		return "", fmt.Errorf("document template store: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("document template store: no template for preset %s: %w", presetID, ErrNotFound)
	}
	return matches[0], nil
}

// Delete implements DocumentTemplateStore.
func (s *documentTemplateStore) Delete(_ context.Context, presetID string) error {
	if err := validTemplatePresetID(presetID); err != nil {
		return err
	}
	return s.removeExisting(presetID)
}

// removeExisting deletes every template file stored for the preset. A
// preset with no template on disk is not an error.
func (s *documentTemplateStore) removeExisting(presetID string) error {
	matches, err := filepath.Glob(filepath.Join(s.dir, presetID+".*"))
	if err != nil {
		return fmt.Errorf("document template store: %w", err)
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("document template store: remove %s: %w", m, err)
		}
	}
	return nil
}

// templateExt validates the preset ID and derives the stored file's
// extension from the uploaded filename: lowercase, dot included, empty for
// an extensionless name.
func templateExt(presetID, filename string) (string, error) {
	if err := validTemplatePresetID(presetID); err != nil {
		return "", err
	}
	if strings.TrimSpace(filename) == "" {
		return "", errors.New("document template store: a template needs a file name")
	}
	return strings.ToLower(filepath.Ext(filename)), nil
}

// validTemplatePresetID refuses IDs that could reach outside the templates
// directory: the ID is joined into a file name, so separators and traversal
// are rejected here, at the boundary.
func validTemplatePresetID(presetID string) error {
	if presetID == "" {
		return errors.New("document template store: a template needs a preset")
	}
	if strings.ContainsAny(presetID, `/\`) || strings.Contains(presetID, "..") ||
		filepath.VolumeName(presetID) != "" || strings.ContainsAny(presetID, "\x00") {
		return fmt.Errorf("document template store: invalid preset id %q", presetID)
	}
	return nil
}
