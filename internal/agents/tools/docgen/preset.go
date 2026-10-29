// Package docgen adapts the document generation libraries to buddies: the
// picoloom markdown-to-PDF rendering (design D3), the DOCX and PPTX
// generation engines (add-document-editing design D6/D11), and the buddies
// preset schemas each format's create_document presets resolve through,
// mirroring how internal/agents/tools keeps third-party browser types out
// of the agents package surface.
package docgen

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

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

// Preset flavors discriminate what a DOCX or PPTX preset configures (design
// D6): a config preset parametrizes the application's built-in default
// template; a template-document preset references a user-uploaded skeleton.
const (
	PresetFlavorConfig   = "config"
	PresetFlavorTemplate = "template"
)

// DOCXPreset is the buddies DOCX preset schema (design D6, spec
// document-generation "Document presets"): one of the two flavors. A config
// preset parametrizes the built-in default template — fonts, heading style,
// page setup, footer — and that is deliberately all (not a styling engine;
// bespoke looks come from uploaded templates). A template preset carries no
// styling parameters: the preset's uploaded skeleton (the
// store.DocumentTemplateStore file) controls the look, and the config JSON
// records only the has-template metadata, never the binary.
type DOCXPreset struct {
	SchemaVersion int    `json:"schema_version"`
	Flavor        string `json:"flavor"` // PresetFlavorConfig or PresetFlavorTemplate

	// HasTemplate and TemplateName are the template-flavor metadata:
	// whether the skeleton has been uploaded and the upload's original
	// file name (display only; the file itself lives in the template
	// store).
	HasTemplate  bool   `json:"has_template,omitempty"`
	TemplateName string `json:"template_name,omitempty"`

	// The config-flavor parameters. Zero fields mean "the built-in default
	// template's value"; a template-flavor preset ignores them.
	Fonts   *DOCXFonts        `json:"fonts,omitempty"`
	Heading *DOCXHeadingStyle `json:"heading,omitempty"`
	Page    *DOCXPageSetup    `json:"page,omitempty"`
	Footer  *DOCXFooterStyle  `json:"footer,omitempty"`
}

// DOCXFonts is the config preset's font selection: the body and heading
// families and sizes, in points.
type DOCXFonts struct {
	BodyFamily    string  `json:"body_family,omitempty"`
	BodySize      float64 `json:"body_size,omitempty"` // points
	HeadingFamily string  `json:"heading_family,omitempty"`
	HeadingSize   float64 `json:"heading_size,omitempty"` // points, the Heading 1 size
}

// DOCXHeadingStyle is the config preset's heading treatment: the heading
// color and weight. A nil Bold keeps the built-in template's bold headings;
// Bold=false chooses regular-weight headings.
type DOCXHeadingStyle struct {
	Color string `json:"color,omitempty"` // "#RRGGBB"
	Bold  *bool  `json:"bold,omitempty"`
}

// DOCXPageSetup is the config preset's page geometry. Size uses the same
// vocabulary as the PDF presets ("letter", "a4", "legal"); MarginInches is
// one uniform margin for all sides.
type DOCXPageSetup struct {
	Size         string  `json:"size,omitempty"`          // "letter", "a4", "legal"
	Orientation  string  `json:"orientation,omitempty"`   // "portrait", "landscape"
	MarginInches float64 `json:"margin_inches,omitempty"` // 0 => built-in default (1 in)
}

// DOCXFooterStyle is the config preset's page footer: literal text and/or
// page numbering.
type DOCXFooterStyle struct {
	Text           string `json:"text,omitempty"`
	ShowPageNumber bool   `json:"show_page_number,omitempty"`
}

// PPTXPreset is the buddies PPTX preset schema: template-document presets
// only (design D6) — the uploaded skeleton's existing layouts and text
// placeholders control the deck; no config parameters exist.
type PPTXPreset struct {
	SchemaVersion int    `json:"schema_version"`
	Flavor        string `json:"flavor"` // always PresetFlavorTemplate

	// HasTemplate and TemplateName are the template metadata, the DOCX
	// preset's fields' counterparts.
	HasTemplate  bool   `json:"has_template,omitempty"`
	TemplateName string `json:"template_name,omitempty"`
}

// ParseDOCXPreset decodes stored DOCX preset config JSON, mirroring
// ParsePreset's contract: the result carries the effective schema version,
// unknown versions still parse (flagged in the returned warning), and known
// versions are validated here so a bad value fails at parse time with a
// readable error instead of at generation time.
func ParseDOCXPreset(data []byte) (DOCXPreset, string, error) {
	var p DOCXPreset
	if err := json.Unmarshal(data, &p); err != nil {
		return DOCXPreset{}, "", fmt.Errorf("docgen: parse DOCX preset: %w", err)
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
	if err := validateDOCXPreset(&p); err != nil {
		return DOCXPreset{}, "", err
	}
	return p, "", nil
}

// MarshalDOCXPreset encodes the preset as config JSON, stamping the current
// schema version so stored presets are self-describing.
func MarshalDOCXPreset(p DOCXPreset) ([]byte, error) {
	p.SchemaVersion = PresetSchemaVersion
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("docgen: encode DOCX preset: %w", err)
	}
	return data, nil
}

// ParsePPTXPreset decodes stored PPTX preset config JSON, mirroring
// ParseDOCXPreset's contract.
func ParsePPTXPreset(data []byte) (PPTXPreset, string, error) {
	var p PPTXPreset
	if err := json.Unmarshal(data, &p); err != nil {
		return PPTXPreset{}, "", fmt.Errorf("docgen: parse PPTX preset: %w", err)
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
	if err := validatePPTXPreset(&p); err != nil {
		return PPTXPreset{}, "", err
	}
	return p, "", nil
}

// MarshalPPTXPreset encodes the preset as config JSON, stamping the current
// schema version so stored presets are self-describing.
func MarshalPPTXPreset(p PPTXPreset) ([]byte, error) {
	p.SchemaVersion = PresetSchemaVersion
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("docgen: encode PPTX preset: %w", err)
	}
	return data, nil
}

// validateDOCXPreset checks a known-version DOCX preset against the schema's
// enums and bounds. The page vocabulary is the PDF presets' (the same words
// mean the same geometry in both formats).
func validateDOCXPreset(p *DOCXPreset) error {
	switch p.Flavor {
	case PresetFlavorConfig, PresetFlavorTemplate:
		// Template presets may carry leftover config parameters; the engine
		// ignores them.
	default:
		return fmt.Errorf("docgen: invalid flavor %q (must be %q or %q)", p.Flavor, PresetFlavorConfig, PresetFlavorTemplate)
	}
	if p.Fonts != nil {
		for _, size := range []struct {
			name  string
			value float64
		}{{"body size", p.Fonts.BodySize}, {"heading size", p.Fonts.HeadingSize}} {
			if size.value != 0 && (size.value < 6 || size.value > 72) {
				return fmt.Errorf("docgen: fonts: %s %.1f must be between 6 and 72 points", size.name, size.value)
			}
		}
	}
	if p.Heading != nil && p.Heading.Color != "" && !isHexColor(p.Heading.Color) {
		return fmt.Errorf("docgen: heading: color %q must be #RRGGBB", p.Heading.Color)
	}
	if p.Page != nil {
		if p.Page.Size != "" && p.Page.Size != "letter" && p.Page.Size != "a4" && p.Page.Size != "legal" {
			return fmt.Errorf("docgen: page setup: size %q must be letter, a4, or legal", p.Page.Size)
		}
		if p.Page.Orientation != "" && p.Page.Orientation != "portrait" && p.Page.Orientation != "landscape" {
			return fmt.Errorf("docgen: page setup: orientation %q must be portrait or landscape", p.Page.Orientation)
		}
		if p.Page.MarginInches < 0 || p.Page.MarginInches > 4 {
			return fmt.Errorf("docgen: page setup: margin %.2f must be between 0 and 4 inches", p.Page.MarginInches)
		}
	}
	return nil
}

// validatePPTXPreset checks a known-version PPTX preset: template flavor
// only — PPTX generation fills an uploaded skeleton's existing layouts and
// has no built-in default template to parametrize.
func validatePPTXPreset(p *PPTXPreset) error {
	if p.Flavor != PresetFlavorTemplate {
		return fmt.Errorf("docgen: invalid flavor %q (PPTX presets are %q presets)", p.Flavor, PresetFlavorTemplate)
	}
	return nil
}

// isHexColor reports whether color is a #RRGGBB (or RRGGBB) hex color.
func isHexColor(color string) bool {
	color = strings.TrimPrefix(color, "#")
	if len(color) != 6 {
		return false
	}
	_, err := hex.DecodeString(color)
	return err == nil
}

// TemplateMeta is the has-template metadata a DOCX or PPTX preset config
// records for its uploaded skeleton.
type TemplateMeta struct {
	HasTemplate  bool
	TemplateName string
}

// ParseTemplateMeta peeks the has-template metadata out of a stored preset
// config. It is deliberately lenient — the caller lists presets, so a
// config that no longer parses must not break the listing — and reads as
// "no template" instead.
func ParseTemplateMeta(data []byte) TemplateMeta {
	var raw struct {
		HasTemplate  bool   `json:"has_template"`
		TemplateName string `json:"template_name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return TemplateMeta{}
	}
	return TemplateMeta{HasTemplate: raw.HasTemplate, TemplateName: raw.TemplateName}
}

// ApplyTemplateMeta stamps the has-template metadata onto a stored preset
// config, preserving the rest of the JSON document verbatim: the config
// schema is the tool's, and the preset service only records that the
// skeleton upload happened.
func ApplyTemplateMeta(configJSON []byte, hasTemplate bool, templateName string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(configJSON, &doc); err != nil {
		return nil, fmt.Errorf("docgen: stamp template metadata: %w", err)
	}
	doc["has_template"] = hasTemplate
	if hasTemplate {
		doc["template_name"] = templateName
	} else {
		delete(doc, "template_name")
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("docgen: stamp template metadata: %w", err)
	}
	return data, nil
}
