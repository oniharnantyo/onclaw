package docgen

import (
	"testing"

	picoloom "github.com/alnah/picoloom/v2"
	"github.com/stretchr/testify/require"
)

func TestMapPreset_NilAndEmptyFallBackToLibraryDefaults(t *testing.T) {
	// Nothing set means picoloom's Input stays zero: the library applies its
	// own defaults at render time and receives no empty overrides to clobber
	// them.
	require.Equal(t, picoloom.Input{}, mapPreset(nil))
	require.Equal(t, picoloom.Input{}, mapPreset(&Preset{}))
}

func TestMapPreset_FullPreset(t *testing.T) {
	p := &Preset{
		Theme:     "technical",
		Page:      &PageSetup{Size: "legal", Orientation: "landscape", MarginInches: 1},
		Cover:     &Cover{Title: "Report", Subtitle: "Q3", Author: "A", AuthorTitle: "T", Organization: "O", Date: "2026-10-26", Version: "v1"},
		Footer:    &FooterStyle{Position: "right", ShowPageNumber: true, Text: "buddies"},
		Watermark: &Watermark{Text: "DRAFT", Color: "#123456", Opacity: 0.25, Angle: -30},
		TOC:       &TOCSettings{Title: "Contents", MinDepth: 2, MaxDepth: 4},
	}
	in := mapPreset(p)
	require.Equal(t, &picoloom.PageSettings{Size: "legal", Orientation: "landscape", Margin: 1}, in.Page)
	require.Equal(t, &picoloom.Cover{
		Title:        "Report",
		Subtitle:     "Q3",
		Author:       "A",
		AuthorTitle:  "T",
		Organization: "O",
		Date:         "2026-10-26",
		Version:      "v1",
	}, in.Cover)
	require.Equal(t, &picoloom.Footer{Position: "right", ShowPageNumber: true, Text: "buddies"}, in.Footer)
	require.Equal(t, &picoloom.Watermark{Text: "DRAFT", Color: "#123456", Opacity: 0.25, Angle: -30}, in.Watermark)
	require.Equal(t, &picoloom.TOC{Title: "Contents", MinDepth: 2, MaxDepth: 4}, in.TOC)
}

func TestMapPreset_SectionToggles(t *testing.T) {
	cases := []struct {
		name     string
		preset   Preset
		wantNil  func(in picoloom.Input) any
		wantSent bool
	}{
		{
			name:     "empty page setup still maps (presence is the toggle)",
			preset:   Preset{Page: &PageSetup{}},
			wantNil:  func(in picoloom.Input) any { return in.Page },
			wantSent: true,
		},
		{
			name:     "empty cover still maps (presence is the toggle)",
			preset:   Preset{Cover: &Cover{}},
			wantNil:  func(in picoloom.Input) any { return in.Cover },
			wantSent: true,
		},
		{
			name:     "empty toc still maps (presence is the toggle)",
			preset:   Preset{TOC: &TOCSettings{}},
			wantNil:  func(in picoloom.Input) any { return in.TOC },
			wantSent: true,
		},
		{
			name:     "empty footer maps to nil (nothing to render)",
			preset:   Preset{Footer: &FooterStyle{}},
			wantNil:  func(in picoloom.Input) any { return in.Footer },
			wantSent: false,
		},
		{
			name:     "empty watermark maps to nil (picoloom no-ops on it anyway)",
			preset:   Preset{Watermark: &Watermark{}},
			wantNil:  func(in picoloom.Input) any { return in.Watermark },
			wantSent: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := mapPreset(&tc.preset)
			got := tc.wantNil(in)
			if tc.wantSent {
				require.NotNil(t, got)
			} else {
				require.Nil(t, got)
			}
		})
	}
}

func TestMapPreset_FooterContentRules(t *testing.T) {
	pageNumberOnly := mapPreset(&Preset{Footer: &FooterStyle{Position: "left", ShowPageNumber: true}})
	require.Equal(t, &picoloom.Footer{Position: "left", ShowPageNumber: true}, pageNumberOnly.Footer)

	textOnly := mapPreset(&Preset{Footer: &FooterStyle{Text: "Internal"}})
	require.Equal(t, &picoloom.Footer{Text: "Internal"}, textOnly.Footer)

	positionOnly := mapPreset(&Preset{Footer: &FooterStyle{Position: "center"}})
	require.Nil(t, positionOnly.Footer, "a position with nothing to show only widens the bottom margin")
}

func TestMapPreset_WatermarkZeroFieldsTakeLibraryDefaults(t *testing.T) {
	// picoloom renders watermark color/opacity/angle verbatim, so docgen
	// substitutes the library's own defaults for zero fields instead of
	// sending empties that would break the CSS.
	in := mapPreset(&Preset{Watermark: &Watermark{Text: "CONFIDENTIAL"}})
	require.Equal(t, &picoloom.Watermark{
		Text:    "CONFIDENTIAL",
		Color:   picoloom.DefaultWatermarkColor,
		Opacity: picoloom.DefaultWatermarkOpacity,
		Angle:   picoloom.DefaultWatermarkAngle,
	}, in.Watermark)
}

func TestMapPreset_ThemeNeverRidesTheInput(t *testing.T) {
	// The theme is converter-level in picoloom (WithStyle at construction);
	// the Converter swaps engines for it, so it must never appear here.
	in := mapPreset(&Preset{Theme: "technical"})
	require.Equal(t, picoloom.Input{}, in)
}
