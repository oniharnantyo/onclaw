package store

import (
	"context"
	"time"
)

// The DocumentPresetFormat* vocabulary is the document format a preset
// applies to. PDF is the v1 format (add-create-document design D4) and the
// format column's default; DOCX and PPTX join it with the OOXML generation
// engines (add-document-editing design D6). The column existed from day one,
// so the new formats are rows, not schema changes.
const (
	DocumentPresetFormatPDF  = "pdf"
	DocumentPresetFormatDOCX = "docx"
	DocumentPresetFormatPPTX = "pptx"
)

// DocumentPreset is one saved presentation configuration for the
// create_document tool (add-create-document design D4/D5). Config is the
// preset's opaque JSON document — a buddies-defined v1 schema owned by the
// tool's adapter; the store persists it verbatim and never interprets it.
// Format is the document format the preset applies to; names are unique per
// format.
type DocumentPreset struct {
	ID   string
	Name string
	// Format is the document format the preset applies to (the
	// DocumentPresetFormat* vocabulary).
	Format string
	// Config is the preset's presentation configuration as opaque JSON,
	// persisted verbatim (the Hook.Config pattern).
	Config    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DocumentPresetStore persists document presets (add-create-document design
// D4): one row per saved presentation configuration, names unique per
// format. The default-preset selection does not live here — it rides the
// ToolSettingStore under the create_document tool key.
type DocumentPresetStore interface {
	// Create stores a new preset, assigning ID and stamping the timestamps
	// (UTC) when empty; an empty Format becomes "pdf" (the v1 default).
	// Names are unique per format case-insensitively: a duplicate returns
	// ErrDuplicate.
	Create(ctx context.Context, p *DocumentPreset) error
	// ByID returns the preset with the given ID; ErrNotFound when absent.
	ByID(ctx context.Context, id string) (*DocumentPreset, error)
	// ByName returns the format's preset with the given name, matched
	// case-insensitively within the format; ErrNotFound when absent.
	ByName(ctx context.Context, format, name string) (*DocumentPreset, error)
	// List returns the format's presets ordered by name, case-insensitive.
	// The result is never nil.
	List(ctx context.Context, format string) ([]DocumentPreset, error)
	// Update rewrites an existing preset's definition (name, format,
	// config) and stamps UpdatedAt (UTC); ErrNotFound when absent,
	// ErrDuplicate when the name collides with another preset's
	// case-insensitively within the format.
	Update(ctx context.Context, p *DocumentPreset) error
	// Delete removes a preset's row only — generated documents are files in
	// agent workspaces and are never touched; ErrNotFound when absent.
	Delete(ctx context.Context, id string) error
}
