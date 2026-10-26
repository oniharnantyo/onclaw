// Package docgen adapts the picoloom markdown-to-PDF library to buddies. It
// defines the buddies v1 document-preset schema (design D5) and the
// manager-held converter lifecycle (design D3), mirroring how
// internal/agents/tools keeps third-party browser types out of the agents
// package surface.
package docgen

import (
	"encoding/json"
	"fmt"

	picoloom "github.com/alnah/picoloom/v2"
)

// PresetSchemaVersion is the current schema version of the preset config
// JSON stored in document_presets. It is an int so future versions compare
// numerically: 1 is the only defined version; a missing field reads as 1
// (the schema shipped with it from day one).
const PresetSchemaVersion = 1

// Themes are the styling presets buddies v1 exposes, one per style shipped
// embedded in picoloom. Adding a picoloom style requires extending this list
// so the preset editor's form stays honest.
var Themes = []string{
	"academic",
	"corporate",
	"creative",
	"default",
	"invoice",
	"legal",
	"manuscript",
	"technical",
}

// Preset is the buddies v1 preset schema: the styling reference the
// create_document tool resolves by name (design D2/D5). It deliberately has
// no custom CSS and no signature block — the spec forbids both in v1 — and
// no logo field (presets never carry filesystem paths). The zero Preset maps
// onto picoloom's built-in defaults.
//
// Section presence follows picoloom's semantics: a non-nil Page, Cover, or
// TOC turns that feature on (an all-empty struct uses the library defaults
// for it), while Footer and Watermark only map when they carry content,
// because picoloom renders nothing useful for an empty one.
type Preset struct {
	SchemaVersion int          `json:"schema_version"`
	Theme         string       `json:"theme,omitempty"`
	Page          *PageSetup   `json:"page,omitempty"`
	Cover         *Cover       `json:"cover,omitempty"`
	Footer        *FooterStyle `json:"footer,omitempty"`
	Watermark     *Watermark   `json:"watermark,omitempty"`
	TOC           *TOCSettings `json:"toc,omitempty"`
}

// PageSetup is the page geometry. Size and Orientation are picoloom's enums;
// MarginInches is one uniform margin for all sides (picoloom only supports a
// uniform margin). Zero fields mean "use the library default".
type PageSetup struct {
	Size         string  `json:"size,omitempty"`          // "letter", "a4", "legal"
	Orientation  string  `json:"orientation,omitempty"`   // "portrait", "landscape"
	MarginInches float64 `json:"margin_inches,omitempty"` // 0 => library default (0.5 in)
}

// Cover holds the cover-page fields exposed in v1.
type Cover struct {
	Title        string `json:"title,omitempty"`
	Subtitle     string `json:"subtitle,omitempty"`
	Author       string `json:"author,omitempty"`
	AuthorTitle  string `json:"author_title,omitempty"`
	Organization string `json:"organization,omitempty"`
	Date         string `json:"date,omitempty"`
	Version      string `json:"version,omitempty"`
}

// FooterStyle is the page-footer configuration. It maps only when
// ShowPageNumber is set or Text is non-empty; Position without content is
// dropped, since an empty footer strip only adds bottom margin.
type FooterStyle struct {
	Position       string `json:"position,omitempty"` // "left", "center", "right"
	ShowPageNumber bool   `json:"show_page_number,omitempty"`
	Text           string `json:"text,omitempty"`
}

// Watermark is the diagonal background text. Zero Color, Opacity, and Angle
// fall back to picoloom's defaults (see mapWatermark): a watermark is only
// sent when Text is non-empty.
type Watermark struct {
	Text    string  `json:"text,omitempty"`
	Color   string  `json:"color,omitempty"`
	Opacity float64 `json:"opacity,omitempty"`
	Angle   float64 `json:"angle,omitempty"`
}

// TOCSettings configures the table of contents. A non-nil TOCSettings turns
// the TOC on; zero depths use the library defaults (min 2, max 3).
type TOCSettings struct {
	Title    string `json:"title,omitempty"`
	MinDepth int    `json:"min_depth,omitempty"`
	MaxDepth int    `json:"max_depth,omitempty"`
}

// ParsePreset decodes stored preset config JSON. The returned Preset always
// carries the effective schema version (a missing field reads as
// PresetSchemaVersion). Unknown versions still parse — the design reads them
// but flags them in the editor — surfaced as the returned warning string
// (empty when there is nothing to flag); their fields skip validation
// because their meaning is undefined. Known versions are validated against
// the schema's enums and bounds with picoloom's own validators, so a bad
// value fails here with a readable error instead of at conversion time.
func ParsePreset(data []byte) (Preset, string, error) {
	var p Preset
	if err := json.Unmarshal(data, &p); err != nil {
		return Preset{}, "", fmt.Errorf("docgen: parse preset: %w", err)
	}
	if p.SchemaVersion == 0 {
		p.SchemaVersion = PresetSchemaVersion
	}
	if p.SchemaVersion != PresetSchemaVersion {
		return p, fmt.Sprintf(
			"preset schema version %d is not supported (supported: %d); fields may be misread",
			p.SchemaVersion, PresetSchemaVersion,
		), nil
	}
	if err := validatePreset(&p); err != nil {
		return Preset{}, "", err
	}
	return p, "", nil
}

// MarshalPreset encodes the preset as config JSON, stamping the current
// schema version so stored presets are self-describing.
func MarshalPreset(p Preset) ([]byte, error) {
	p.SchemaVersion = PresetSchemaVersion
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("docgen: encode preset: %w", err)
	}
	return data, nil
}

// validatePreset checks a known-version preset against the schema's enums
// and bounds. Field-level validation reuses picoloom's validators so the
// schema and the library can never disagree about what is valid.
func validatePreset(p *Preset) error {
	if !isKnownTheme(p.Theme) {
		return fmt.Errorf("docgen: invalid theme %q (must be one of %v)", p.Theme, Themes)
	}
	if p.Page != nil {
		page := picoloom.PageSettings{
			Size:        p.Page.Size,
			Orientation: p.Page.Orientation,
			Margin:      p.Page.MarginInches,
		}
		if err := page.Validate(); err != nil {
			return fmt.Errorf("docgen: page setup: %w", err)
		}
	}
	if p.Footer != nil {
		footer := picoloom.Footer{Position: p.Footer.Position}
		if err := footer.Validate(); err != nil {
			return fmt.Errorf("docgen: footer: %w", err)
		}
	}
	if p.Watermark != nil {
		watermark := picoloom.Watermark{
			Text:    p.Watermark.Text,
			Color:   p.Watermark.Color,
			Opacity: p.Watermark.Opacity,
			Angle:   p.Watermark.Angle,
		}
		if err := watermark.Validate(); err != nil {
			return fmt.Errorf("docgen: watermark: %w", err)
		}
	}
	if p.TOC != nil {
		toc := picoloom.TOC{Title: p.TOC.Title, MinDepth: p.TOC.MinDepth, MaxDepth: p.TOC.MaxDepth}
		if err := toc.Validate(); err != nil {
			return fmt.Errorf("docgen: table of contents: %w", err)
		}
	}
	return nil
}

// isKnownTheme reports whether theme is part of the v1 theme list; the empty
// theme is the library default and always allowed.
func isKnownTheme(theme string) bool {
	if theme == "" {
		return true
	}
	for _, t := range Themes {
		if t == theme {
			return true
		}
	}
	return false
}
