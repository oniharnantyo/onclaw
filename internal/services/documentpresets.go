package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents/tools/docgen"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The document-preset CRUD methods on SettingsService (add-create-document
// design D4, spec document-generation "Document presets"): the Tools pane's
// create_document configuration dialog manages a format's presets and its
// default selection through these calls. The preset Config passes through
// opaquely — its schema is owned by the tool's adapter
// (internal/agents/tools/docgen) — except for the template-document
// metadata the service itself maintains (add-document-editing design D6):
// has_template and the upload's file name are stamped here on upload and
// surfaced on the entries, while the skeleton binary never passes through
// the preset row (it lives in the DocumentTemplateStore files).

// documentToolKey is the tool_settings key the default-preset selections
// ride under. It is the create_document tool's own key: the row's JSON
// document holds the per-format defaults
// ({"defaultPresets":{"docx":"<name>"}}) beside the v1 flat field
// {"defaultPreset":"<name>"} — the PDF default — and the tool side resolves
// this same row (internal/agents/docgen_tool.go readDefaultPresetName).
const documentToolKey = "create_document"

// storedDocumentDefault is the tool_settings document's shape for the
// create_document tool: per format, the preset NAME a preset-less call
// falls back to. DefaultPreset is the v1 flat field — the PDF default,
// kept in sync so the v1 readers keep working; DefaultPresets carries the
// per-format entries. An absent row or an empty name means no default.
type storedDocumentDefault struct {
	DefaultPreset  string            `json:"defaultPreset"`
	DefaultPresets map[string]string `json:"defaultPresets,omitempty"`
}

// DocumentPresetEntry is one saved document preset as the Tools pane's
// configuration dialog renders and edits it. Config is the preset's opaque
// JSON document — the dialog owns its schema; the service stores it
// verbatim. HasTemplate and TemplateName mirror the template-document
// metadata (add-document-editing design D6): whether the preset's uploaded
// skeleton is in place and what it was uploaded as.
type DocumentPresetEntry struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Format       string    `json:"format"`
	Config       string    `json:"config"`
	HasTemplate  bool      `json:"hasTemplate"`
	TemplateName string    `json:"templateName,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
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
// config included — and, for a template-document preset, the uploaded
// skeleton with it, so the copy generates like the source (the template
// files are keyed by preset ID, which the copy changes). Unknown id is
// store.ErrNotFound; a colliding newName is store.ErrDuplicate. A source
// whose skeleton file has gone missing fails the copy rather than produce a
// preset claiming a template it cannot render with.
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
	if err := s.copyPresetTemplate(ctx, row, dup); err != nil {
		return DocumentPresetEntry{}, fmt.Errorf("duplicate document preset: %w", err)
	}
	return documentPresetEntry(dup), nil
}

// copyPresetTemplate carries the source's uploaded skeleton to the copy.
// A nil template store (feature not wired) and a preset without an uploaded
// skeleton copy nothing.
func (s *SettingsService) copyPresetTemplate(ctx context.Context, source, dup *store.DocumentPreset) error {
	if s.templates == nil || !docgen.ParseTemplateMeta([]byte(source.Config)).HasTemplate {
		return nil
	}
	path, err := s.templates.Path(ctx, source.ID)
	if err != nil {
		return fmt.Errorf("find the source's template: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read the source's template: %w", err)
	}
	if _, err := s.templates.Save(ctx, dup.ID, filepath.Base(path), data); err != nil {
		return fmt.Errorf("store the copy's template: %w", err)
	}
	return nil
}

// SetDocumentPresetTemplate stores the preset's uploaded skeleton document
// (add-document-editing design D6): the base64 document lands in the
// template store under the preset's ID, and the preset row records the
// upload (has_template, the file's name) in its config. The file name's
// extension must match the preset's format — .docx for a DOCX preset,
// .pptx for a PPTX preset; PDF presets are config-only and take no
// template. Unknown id is store.ErrNotFound.
func (s *SettingsService) SetDocumentPresetTemplate(ctx context.Context, presetID, filename, base64Data string) error {
	if s.templates == nil {
		return fmt.Errorf("set document preset template: template storage is not configured")
	}
	if strings.TrimSpace(filename) == "" {
		return fmt.Errorf("set document preset template: a template needs a file name")
	}
	row, err := s.store.DocumentPresets().ByID(ctx, presetID)
	if err != nil {
		return fmt.Errorf("set document preset template: %w", err)
	}
	extension := strings.ToLower(filepath.Ext(filename))
	if extension != docTemplateExtension(row.Format) {
		return fmt.Errorf("set document preset template: a %s preset takes a %s skeleton, not %q",
			strings.ToUpper(row.Format), docTemplateExtension(row.Format), filename)
	}
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return fmt.Errorf("set document preset template: decode the template document: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("set document preset template: the template document is empty")
	}
	if _, err := s.templates.Save(ctx, row.ID, filename, data); err != nil {
		return fmt.Errorf("set document preset template: %w", err)
	}
	config, err := docgen.ApplyTemplateMeta([]byte(row.Config), true, filename)
	if err != nil {
		return fmt.Errorf("set document preset template: %w", err)
	}
	row.Config = string(config)
	if err := s.store.DocumentPresets().Update(ctx, row); err != nil {
		return fmt.Errorf("set document preset template: %w", err)
	}
	return nil
}

// docTemplateExtension is the skeleton file extension a format's template
// upload must carry; the empty result means the format takes no template.
func docTemplateExtension(format string) string {
	switch format {
	case store.DocumentPresetFormatDOCX:
		return ".docx"
	case store.DocumentPresetFormatPPTX:
		return ".pptx"
	default:
		return ""
	}
}

// DeleteDocumentPreset removes the preset's row and its uploaded skeleton —
// a template preset's file is the preset's payload, so it goes with it
// (add-document-editing design D6). Generated documents are workspace files
// and are never touched. Unknown id is store.ErrNotFound. Deleting the
// preset named as a format's default intentionally leaves that stored
// default dangling: agents resolve presets by name and the create_document
// tool treats an unresolvable default as "no default" (spec: deleting the
// default resets the selection to none), so the service does not clear the
// tool_settings row transactionally.
func (s *SettingsService) DeleteDocumentPreset(ctx context.Context, id string) error {
	if err := s.store.DocumentPresets().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete document preset: %w", err)
	}
	if s.templates != nil {
		if err := s.templates.Delete(ctx, id); err != nil {
			return fmt.Errorf("delete document preset: remove its template: %w", err)
		}
	}
	return nil
}

// DefaultDocumentPreset returns the format's stored default preset NAME —
// empty when none was ever set. The name is returned verbatim from storage:
// a default naming a since-deleted preset is left dangling and still reads
// back (the tool resolves a dangling default to "no default"; see
// DeleteDocumentPreset). The pdf format falls back to the v1 flat field
// when the per-format map has no entry, so rows written before the
// per-format map keep working.
func (s *SettingsService) DefaultDocumentPreset(ctx context.Context, format string) (string, error) {
	stored, err := readDocumentDefaults(ctx, s.store)
	if err != nil {
		return "", fmt.Errorf("get document default: %w", err)
	}
	if name := stored.DefaultPresets[format]; name != "" {
		return name, nil
	}
	if format == store.DocumentPresetFormatPDF {
		return stored.DefaultPreset, nil
	}
	return "", nil
}

// SetDefaultDocumentPreset stores the format's default preset NAME; an
// empty name clears the format's default. A non-empty name must resolve to
// a preset of the format — store.ErrNotFound otherwise; the canonical
// spelling of the matched preset is stored. The pdf format's selection also
// rides the v1 flat field, so the stored document keeps every reader
// working; the other formats' entries live only in the per-format map.
func (s *SettingsService) SetDefaultDocumentPreset(ctx context.Context, format, name string) error {
	stored, err := readDocumentDefaults(ctx, s.store)
	if err != nil {
		return fmt.Errorf("set document default: %w", err)
	}
	canonical := ""
	if name != "" {
		row, err := s.store.DocumentPresets().ByName(ctx, format, name)
		if err != nil {
			return fmt.Errorf("set document default: %w", err)
		}
		canonical = row.Name
	}
	if stored.DefaultPresets == nil {
		stored.DefaultPresets = make(map[string]string)
	}
	if canonical == "" {
		delete(stored.DefaultPresets, format)
	} else {
		stored.DefaultPresets[format] = canonical
	}
	if format == store.DocumentPresetFormatPDF {
		// The v1 flat field stays the PDF default, in sync.
		stored.DefaultPreset = canonical
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

// readDocumentDefaults reads the create_document tool-settings row's
// stored defaults; an absent row reads as none.
func readDocumentDefaults(ctx context.Context, st store.Store) (storedDocumentDefault, error) {
	raw, ok, err := st.ToolSettings().Get(ctx, documentToolKey)
	if err != nil || !ok || raw == "" {
		return storedDocumentDefault{}, err
	}
	var stored storedDocumentDefault
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return storedDocumentDefault{}, fmt.Errorf("decode document default: %w", err)
	}
	return stored, nil
}

// documentPresetEntry maps a store row onto the bound entry.
func documentPresetEntry(p *store.DocumentPreset) DocumentPresetEntry {
	meta := docgen.ParseTemplateMeta([]byte(p.Config))
	return DocumentPresetEntry{
		ID:           p.ID,
		Name:         p.Name,
		Format:       p.Format,
		Config:       p.Config,
		HasTemplate:  meta.HasTemplate,
		TemplateName: meta.TemplateName,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
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
