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
