package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_MissingFileReportsFalse(t *testing.T) {
	_, ok := Load(filepath.Join(t.TempDir(), "window.json"))
	require.False(t, ok)
}

func TestLoad_CorruptJSONReportsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"x":10,`), 0o600))

	_, ok := Load(path)
	require.False(t, ok)
}

func TestLoad_DegenerateSizeReportsFalse(t *testing.T) {
	for _, body := range []string{
		`{"x":10,"y":20,"width":0,"height":600}`,
		`{"x":10,"y":20,"width":800,"height":-1}`,
		`{}`,
	} {
		path := filepath.Join(t.TempDir(), "window.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

		_, ok := Load(path)
		require.False(t, ok, "body: %s", body)
	}
}

func TestLoad_ValidFrame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window.json")
	// Unknown fields are forward compatibility, not corruption.
	require.NoError(t, os.WriteFile(path,
		[]byte(`{"x":10,"y":20,"width":800,"height":600,"screen":"main"}`), 0o600))

	frame, ok := Load(path)
	require.True(t, ok)
	require.Equal(t, Frame{X: 10, Y: 20, Width: 800, Height: 600}, frame)
}

func TestSave_RoundTripCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "window.json")
	want := Frame{X: -1920, Y: 40, Width: 1024, Height: 768}

	require.NoError(t, Save(path, want))

	got, ok := Load(path)
	require.True(t, ok)
	require.Equal(t, want, got)
}

func TestSave_ReportsFailure(t *testing.T) {
	// A regular file where Save needs a directory makes MkdirAll fail.
	blocked := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0o600))

	err := Save(filepath.Join(blocked, "window.json"), Frame{Width: 1, Height: 1})
	require.Error(t, err)
}

func TestClamp(t *testing.T) {
	primary := Screen{Bounds: Rect{X: 0, Y: 0, Width: 1920, Height: 1080}}
	// A display to the right of the primary, as macOS arranges side-by-side
	// screens.
	external := Screen{Bounds: Rect{X: 1920, Y: 0, Width: 1920, Height: 1080}}

	tests := []struct {
		name    string
		frame   Frame
		screens []Screen
		want    Frame
	}{
		{
			name:    "empty screen list passes through",
			frame:   Frame{X: 9999, Y: -9999, Width: 800, Height: 600},
			screens: nil,
			want:    Frame{X: 9999, Y: -9999, Width: 800, Height: 600},
		},
		{
			name:    "on-screen frame passes through",
			frame:   Frame{X: 100, Y: 100, Width: 800, Height: 600},
			screens: []Screen{primary},
			want:    Frame{X: 100, Y: 100, Width: 800, Height: 600},
		},
		{
			name:    "window dragged half off the right edge is pulled back",
			frame:   Frame{X: 1900, Y: 100, Width: 800, Height: 600},
			screens: []Screen{primary},
			want:    Frame{X: 1820, Y: 100, Width: 800, Height: 600},
		},
		{
			name:    "window dragged off the bottom keeps a visible sliver",
			frame:   Frame{X: 100, Y: 1030, Width: 800, Height: 600},
			screens: []Screen{primary},
			want:    Frame{X: 100, Y: 980, Width: 800, Height: 600},
		},
		{
			name:    "fully off-screen frame lands on the first screen",
			frame:   Frame{X: 3000, Y: 100, Width: 800, Height: 600},
			screens: []Screen{primary},
			want:    Frame{X: 0, Y: 0, Width: 800, Height: 600},
		},
		{
			// A window parked on a display that is no longer attached (e.g.
			// the external monitor above, at a negative origin, unplugged).
			name:    "frame from an unplugged negative-origin display lands on the remaining screen",
			frame:   Frame{X: -1920, Y: 40, Width: 800, Height: 600},
			screens: []Screen{primary},
			want:    Frame{X: 0, Y: 0, Width: 800, Height: 600},
		},
		{
			name:    "frame larger than the display pins to the display origin",
			frame:   Frame{X: 500, Y: 500, Width: 2560, Height: 1440},
			screens: []Screen{primary},
			want:    Frame{X: 0, Y: 0, Width: 2560, Height: 1440},
		},
		{
			// Only the external display overlaps the frame, and the clamp
			// keeps the minimum visible sliver of it on that display.
			name:    "frame over the external display's right edge keeps a visible sliver there",
			frame:   Frame{X: 3800, Y: 100, Width: 800, Height: 600},
			screens: []Screen{primary, external},
			want:    Frame{X: 3740, Y: 100, Width: 800, Height: 600},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Clamp(tt.frame, tt.screens))
		})
	}
}
