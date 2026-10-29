package docgen

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// rawPreset builds config JSON from a literal so table cases stay readable.
func rawPreset(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}

func TestParsePreset_Valid(t *testing.T) {
	data := rawPreset(t, Preset{
		SchemaVersion: 1,
		Theme:         "technical",
		Page:          &PageSetup{Size: "a4", Orientation: "landscape", MarginInches: 0.75},
		Cover:         &Cover{Title: "Q3 Report", Author: "Buddies"},
		Footer:        &FooterStyle{Position: "center", ShowPageNumber: true, Text: "Internal"},
		Watermark:     &Watermark{Text: "DRAFT", Color: "#ff0000", Opacity: 0.2, Angle: -30},
		TOC:           &TOCSettings{Title: "Contents", MinDepth: 2, MaxDepth: 4},
	})
	p, warning, err := ParsePreset(data)
	require.NoError(t, err)
	require.Empty(t, warning)
	require.Equal(t, PresetSchemaVersion, p.SchemaVersion)
	require.Equal(t, "technical", p.Theme)
	require.Equal(t, &PageSetup{Size: "a4", Orientation: "landscape", MarginInches: 0.75}, p.Page)
	require.Equal(t, &Cover{Title: "Q3 Report", Author: "Buddies"}, p.Cover)
	require.Equal(t, &FooterStyle{Position: "center", ShowPageNumber: true, Text: "Internal"}, p.Footer)
	require.Equal(t, &Watermark{Text: "DRAFT", Color: "#ff0000", Opacity: 0.2, Angle: -30}, p.Watermark)
	require.Equal(t, &TOCSettings{Title: "Contents", MinDepth: 2, MaxDepth: 4}, p.TOC)
}

func TestParsePreset_EmptyConfig(t *testing.T) {
	p, warning, err := ParsePreset([]byte(`{}`))
	require.NoError(t, err)
	require.Empty(t, warning, "an empty config is valid: it means library defaults")
	require.Equal(t, Preset{SchemaVersion: PresetSchemaVersion}, p, "version is stamped, everything else stays zero")
}

func TestParsePreset_MissingVersionReadsAsCurrent(t *testing.T) {
	p, warning, err := ParsePreset([]byte(`{"theme":"legal"}`))
	require.NoError(t, err)
	require.Empty(t, warning)
	require.Equal(t, PresetSchemaVersion, p.SchemaVersion, "missing version must be stamped")
	require.Equal(t, "legal", p.Theme)
}

func TestParsePreset_UnknownVersionReadsButFlags(t *testing.T) {
	cases := []struct {
		name    string
		version int
	}{
		{"newer major version", 2},
		{"negative version", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := rawPreset(t, Preset{
				SchemaVersion: tc.version,
				Theme:         "not-a-known-theme-but-unknown-versions-skip-validation",
			})
			p, warning, err := ParsePreset(data)
			require.NoError(t, err, "unknown versions read; the design flags them in the editor")
			require.Contains(t, warning, "not supported")
			require.Contains(t, warning, "schema version")
			require.Equal(t, tc.version, p.SchemaVersion)
		})
	}
}

func TestParsePreset_KnownVersionRejectsInvalidFields(t *testing.T) {
	cases := []struct {
		name    string
		preset  Preset
		wantErr string
	}{
		{
			name:    "unknown theme",
			preset:  Preset{Theme: "steamdeck"},
			wantErr: `invalid theme "steamdeck"`,
		},
		{
			name:    "bad page size",
			preset:  Preset{Page: &PageSetup{Size: "a5"}},
			wantErr: "page setup",
		},
		{
			name:    "bad orientation",
			preset:  Preset{Page: &PageSetup{Orientation: "diagonal"}},
			wantErr: "page setup",
		},
		{
			name:    "margin out of bounds",
			preset:  Preset{Page: &PageSetup{MarginInches: 5}},
			wantErr: "page setup",
		},
		{
			name:    "bad footer position",
			preset:  Preset{Footer: &FooterStyle{Position: "top", Text: "x"}},
			wantErr: "footer",
		},
		{
			name:    "bad watermark color",
			preset:  Preset{Watermark: &Watermark{Text: "D", Color: "red"}},
			wantErr: "watermark",
		},
		{
			name:    "watermark opacity out of range",
			preset:  Preset{Watermark: &Watermark{Text: "D", Opacity: 1.5}},
			wantErr: "watermark",
		},
		{
			name:    "watermark angle out of range",
			preset:  Preset{Watermark: &Watermark{Text: "D", Angle: 91}},
			wantErr: "watermark",
		},
		{
			name:    "toc depth out of range",
			preset:  Preset{TOC: &TOCSettings{MaxDepth: 7}},
			wantErr: "table of contents",
		},
		{
			name:    "toc min above max",
			preset:  Preset{TOC: &TOCSettings{MinDepth: 4, MaxDepth: 2}},
			wantErr: "table of contents",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, warning, err := ParsePreset(rawPreset(t, tc.preset))
			require.ErrorContains(t, err, tc.wantErr)
			require.Empty(t, warning)
		})
	}
}

func TestParsePreset_MalformedJSON(t *testing.T) {
	_, _, err := ParsePreset([]byte(`{"theme":`))
	require.ErrorContains(t, err, "docgen: parse preset")
}

func TestMarshalPreset_StampsSchemaVersion(t *testing.T) {
	data, err := MarshalPreset(Preset{Theme: "technical"})
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	require.Equal(t, float64(PresetSchemaVersion), raw["schema_version"])

	p, warning, err := ParsePreset(data)
	require.NoError(t, err)
	require.Empty(t, warning)
	require.Equal(t, Preset{SchemaVersion: PresetSchemaVersion, Theme: "technical"}, p)
}

// TestParseDOCXPreset_RoundTripAndValidation covers the DOCX preset schema
// (add-document-editing design D6): config and template flavors round-trip
// verbatim, invalid fields fail at parse time with readable errors, and the
// flavor enum is enforced.
func TestParseDOCXPreset_RoundTripAndValidation(t *testing.T) {
	t.Run("config flavor round-trips", func(t *testing.T) {
		data := rawPreset(t, DOCXPreset{
			SchemaVersion: 1,
			Flavor:        PresetFlavorConfig,
			Fonts:         &DOCXFonts{BodyFamily: "Georgia", BodySize: 12, HeadingFamily: "Helvetica", HeadingSize: 18},
			Heading:       &DOCXHeadingStyle{Color: "#AA11BB", Bold: boolPtr(false)},
			Page:          &DOCXPageSetup{Size: "a4", Orientation: "landscape", MarginInches: 1.25},
			Footer:        &DOCXFooterStyle{Text: "ACME", ShowPageNumber: true},
		})
		p, warning, err := ParseDOCXPreset(data)
		require.NoError(t, err)
		require.Empty(t, warning)
		require.Equal(t, PresetSchemaVersion, p.SchemaVersion)
		require.Equal(t, PresetFlavorConfig, p.Flavor)
		require.Equal(t, "Georgia", p.Fonts.BodyFamily)
		require.Equal(t, 12.0, p.Fonts.BodySize)
		require.True(t, p.Footer.ShowPageNumber)
	})

	t.Run("template flavor carries only metadata", func(t *testing.T) {
		p, warning, err := ParseDOCXPreset(rawPreset(t, DOCXPreset{
			Flavor: PresetFlavorTemplate, HasTemplate: true, TemplateName: "Branded.docx",
		}))
		require.NoError(t, err)
		require.Empty(t, warning)
		require.True(t, p.HasTemplate)
		require.Equal(t, "Branded.docx", p.TemplateName)
	})

	t.Run("invalid fields are readable parse errors", func(t *testing.T) {
		cases := []struct {
			name   string
			preset DOCXPreset
			want   string
		}{
			{"unknown flavor", DOCXPreset{Flavor: "hybrid"}, "invalid flavor"},
			{"tiny font", DOCXPreset{Flavor: PresetFlavorConfig, Fonts: &DOCXFonts{BodySize: 3}}, "between 6 and 72"},
			{"huge font", DOCXPreset{Flavor: PresetFlavorConfig, Fonts: &DOCXFonts{HeadingSize: 99}}, "between 6 and 72"},
			{"bad color", DOCXPreset{Flavor: PresetFlavorConfig, Heading: &DOCXHeadingStyle{Color: "red"}}, "#RRGGBB"},
			{"bad page size", DOCXPreset{Flavor: PresetFlavorConfig, Page: &DOCXPageSetup{Size: "a3"}}, "letter, a4, or legal"},
			{"bad orientation", DOCXPreset{Flavor: PresetFlavorConfig, Page: &DOCXPageSetup{Orientation: "diagonal"}}, "portrait or landscape"},
			{"bad margin", DOCXPreset{Flavor: PresetFlavorConfig, Page: &DOCXPageSetup{MarginInches: 9}}, "between 0 and 4 inches"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, _, err := ParseDOCXPreset(rawPreset(t, tc.preset))
				require.ErrorContains(t, err, tc.want)
			})
		}
	})

	t.Run("unknown version still parses but is flagged", func(t *testing.T) {
		p, warning, err := ParseDOCXPreset([]byte(`{"schema_version":9,"flavor":"config"}`))
		require.NoError(t, err)
		require.Contains(t, warning, "not supported")
		require.Equal(t, 9, p.SchemaVersion)
	})

	t.Run("missing version reads as current", func(t *testing.T) {
		p, warning, err := ParseDOCXPreset([]byte(`{"flavor":"template"}`))
		require.NoError(t, err)
		require.Empty(t, warning)
		require.Equal(t, PresetSchemaVersion, p.SchemaVersion)
	})
}

// TestParsePPTXPreset_TemplateOnly covers the PPTX preset schema: template
// flavor is the only valid one — PPTX generation has no built-in default
// template to parametrize.
func TestParsePPTXPreset_TemplateOnly(t *testing.T) {
	p, warning, err := ParsePPTXPreset(rawPreset(t, PPTXPreset{
		Flavor: PresetFlavorTemplate, HasTemplate: true, TemplateName: "Deck.pptx",
	}))
	require.NoError(t, err)
	require.Empty(t, warning)
	require.True(t, p.HasTemplate)
	require.Equal(t, "Deck.pptx", p.TemplateName)

	_, _, err = ParsePPTXPreset(rawPreset(t, PPTXPreset{Flavor: PresetFlavorConfig}))
	require.ErrorContains(t, err, "PPTX presets are")

	// Marshal stamps the schema version.
	data, err := MarshalPPTXPreset(PPTXPreset{Flavor: PresetFlavorTemplate})
	require.NoError(t, err)
	require.JSONEq(t, `{"schema_version":1,"flavor":"template"}`, string(data))
}

// TestTemplateMeta_AndApply cover the metadata helpers the preset service
// rides: the peek is lenient (an unparseable config reads as no template),
// and the stamp preserves the rest of the document.
func TestTemplateMeta_AndApply(t *testing.T) {
	require.False(t, ParseTemplateMeta([]byte("{not json")).HasTemplate, "a corrupt config reads as no template")
	meta := ParseTemplateMeta([]byte(`{"flavor":"template","has_template":true,"template_name":"D.pptx"}`))
	require.True(t, meta.HasTemplate)
	require.Equal(t, "D.pptx", meta.TemplateName)

	stamped, err := ApplyTemplateMeta([]byte(`{"flavor":"template","theme":"x"}`), true, "D.pptx")
	require.NoError(t, err)
	require.JSONEq(t, `{"flavor":"template","theme":"x","has_template":true,"template_name":"D.pptx"}`, string(stamped))

	cleared, err := ApplyTemplateMeta(stamped, false, "")
	require.NoError(t, err)
	require.JSONEq(t, `{"flavor":"template","theme":"x","has_template":false}`, string(cleared))
}

// boolPtr is the table cases' shorthand for the heading weight override.
func boolPtr(b bool) *bool { return &b }
