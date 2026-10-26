package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// The document-preset CRUD methods on SettingsService (add-create-document
// design D4, spec document-generation "Document presets"): the Tools pane's
// create_document configuration dialog manages a format's presets and its
// default selection through these calls. The preset Config passes through
// opaquely — its schema is owned by the tool's adapter
// (internal/agents/tools/docgen), never imported or interpreted here.

// documentToolKey is the tool_settings key the default-preset selection
// rides under. It is the create_document tool's own key: the row's JSON
// document is exactly {"defaultPreset":"<name>"} — the preset NAME, because
// agents reference presets by name. The tool side resolves this same row.
const documentToolKey = "create_document"

// storedDocumentDefault is the tool_settings document's shape for the
// create_document tool: the preset NAME calls without an explicit preset
// fall back to. An absent row or an empty field means no default.
type storedDocumentDefault struct {
	DefaultPreset string `json:"defaultPreset"`
}

// DocumentPresetEntry is one saved document preset as the Tools pane's
// configuration dialog renders and edits it. Config is the preset's opaque
// JSON document — the dialog owns its schema; the service stores it
// verbatim.
type DocumentPresetEntry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Format    string    `json:"format"`
	Config    string    `json:"config"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ListDocumentPresets returns the format's presets, ordered by name. The
// result is never nil.
func (s *SettingsService) ListDocumentPresets(ctx context.Context, format string) ([]DocumentPresetEntry, error) {
	rows, err := s.store.DocumentPresets().List(ctx, format)
	if err != nil {
		return nil, fmt.Errorf("list document presets: %w", err)
	}
	entries := make([]DocumentPresetEntry, 0, len(rows))
	for i := range rows {
		entries = append(entries, documentPresetEntry(&rows[i]))
	}
	return entries, nil
}

// CreateDocumentPreset stores a new preset for the format (an empty format
// defaults to pdf, the v1-only format). The name must be non-empty and the
// config valid JSON; names are unique per format case-insensitively — a
// collision is store.ErrDuplicate.
func (s *SettingsService) CreateDocumentPreset(ctx context.Context, format, name, configJSON string) (DocumentPresetEntry, error) {
	if err := validateDocumentPreset("create", name, configJSON); err != nil {
		return DocumentPresetEntry{}, err
	}
	row := &store.DocumentPreset{Name: name, Format: format, Config: configJSON}
	if err := s.store.DocumentPresets().Create(ctx, row); err != nil {
		return DocumentPresetEntry{}, fmt.Errorf("create document preset: %w", err)
	}
	return documentPresetEntry(row), nil
}

// UpdateDocumentPreset rewrites the preset's name and config, preserving its
// format (an edit never moves a preset between formats). Unknown id is
// store.ErrNotFound; a name colliding with another preset of the same format
// case-insensitively is store.ErrDuplicate.
func (s *SettingsService) UpdateDocumentPreset(ctx context.Context, id, name, configJSON string) (DocumentPresetEntry, error) {
	if err := validateDocumentPreset("update", name, configJSON); err != nil {
		return DocumentPresetEntry{}, err
	}
	row, err := s.store.DocumentPresets().ByID(ctx, id)
	if err != nil {
		return DocumentPresetEntry{}, fmt.Errorf("update document preset: %w", err)
	}
	row.Name = name
	row.Config = configJSON
	if err := s.store.DocumentPresets().Update(ctx, row); err != nil {
		return DocumentPresetEntry{}, fmt.Errorf("update document preset: %w", err)
	}
	return documentPresetEntry(row), nil
}

// DuplicateDocumentPreset copies the preset to newName within its format,
// config included. Unknown id is store.ErrNotFound; a colliding newName is
// store.ErrDuplicate.
func (s *SettingsService) DuplicateDocumentPreset(ctx context.Context, id, newName string) (DocumentPresetEntry, error) {
	if newName == "" {
		return DocumentPresetEntry{}, fmt.Errorf("duplicate document preset: a preset needs a name")
	}
	row, err := s.store.DocumentPresets().ByID(ctx, id)
	if err != nil {
		return DocumentPresetEntry{}, fmt.Errorf("duplicate document preset: %w", err)
	}
	dup := &store.DocumentPreset{Name: newName, Format: row.Format, Config: row.Config}
	if err := s.store.DocumentPresets().Create(ctx, dup); err != nil {
		return DocumentPresetEntry{}, fmt.Errorf("duplicate document preset: %w", err)
	}
	return documentPresetEntry(dup), nil
}

// DeleteDocumentPreset removes the preset's row only — generated documents
// are workspace files and are never touched. Unknown id is store.ErrNotFound.
// Deleting the preset named as the default intentionally leaves that stored
// default dangling: agents resolve presets by name and the create_document
// tool treats an unresolvable default as "no default" (spec: deleting the
// default resets the selection to none), so the service does not clear the
// tool_settings row transactionally.
func (s *SettingsService) DeleteDocumentPreset(ctx context.Context, id string) error {
	if err := s.store.DocumentPresets().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete document preset: %w", err)
	}
	return nil
}

// DefaultDocumentPreset returns the stored default preset NAME — empty when
// none was ever set. The name is returned verbatim from storage: a default
// naming a since-deleted preset is left dangling and still reads back
// (the tool resolves a dangling default to "no default"; see
// DeleteDocumentPreset). The format parameter names the format the default
// applies to; v1 stores a single default per tool, and PDF is the only
// format.
func (s *SettingsService) DefaultDocumentPreset(ctx context.Context, format string) (string, error) {
	raw, ok, err := s.store.ToolSettings().Get(ctx, documentToolKey)
	if err != nil {
		return "", fmt.Errorf("get document default: %w", err)
	}
	_ = format // v1: one default per tool; the parameter keeps the per-format shape for later formats
	if !ok || raw == "" {
		return "", nil
	}
	var stored storedDocumentDefault
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return "", fmt.Errorf("decode document default: %w", err)
	}
	return stored.DefaultPreset, nil
}

// SetDefaultDocumentPreset stores the format's default preset NAME; an empty
// name clears the default (the stored field becomes empty). A non-empty name
// must resolve to a preset of the format — store.ErrNotFound otherwise; the
// canonical spelling of the matched preset is stored. The document under the
// tool key is exactly {"defaultPreset":"<name>"}.
func (s *SettingsService) SetDefaultDocumentPreset(ctx context.Context, format, name string) error {
	stored := storedDocumentDefault{}
	if name != "" {
		row, err := s.store.DocumentPresets().ByName(ctx, format, name)
		if err != nil {
			return fmt.Errorf("set document default: %w", err)
		}
		stored.DefaultPreset = row.Name
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("set document default: %w", err)
	}
	if err := s.store.ToolSettings().Set(ctx, documentToolKey, string(raw)); err != nil {
		return fmt.Errorf("set document default: %w", err)
	}
	return nil
}

// documentPresetEntry maps a store row onto the bound entry.
func documentPresetEntry(p *store.DocumentPreset) DocumentPresetEntry {
	return DocumentPresetEntry{
		ID:        p.ID,
		Name:      p.Name,
		Format:    p.Format,
		Config:    p.Config,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

// validateDocumentPreset rejects an unusable preset definition before the
// store is touched: a preset without a name can never be referenced by an
// agent, and config that is not JSON would break the tool's decoder.
func validateDocumentPreset(op, name, configJSON string) error {
	if name == "" {
		return fmt.Errorf("%s document preset: a preset needs a name", op)
	}
	if !json.Valid([]byte(configJSON)) {
		return fmt.Errorf("%s document preset: config is not valid JSON", op)
	}
	return nil
}
