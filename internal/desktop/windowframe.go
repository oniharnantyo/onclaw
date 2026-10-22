// Package desktop holds desktop-shell window-frame persistence. The frame
// math must compile without Wails — the server build links every internal
// package — so Wails types are mirrored here and converted at the call site
// (spec: polish-native-feel D5).
package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Frame is a window frame in global screen coordinates.
type Frame struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Rect is a rectangle in global screen coordinates (mirrors Wails' Rect).
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Screen is a display as far as the clamp cares: its bounds.
type Screen struct {
	Bounds Rect
}

// minVisible is the smallest sliver (in points) of the window Clamp keeps
// reachable on each axis.
const minVisible = 100

// Load reads the window frame at path. A missing or unreadable file,
// corrupt JSON, or a frame without positive size reports false — the caller
// falls back to the window defaults.
func Load(path string) (Frame, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Frame{}, false
	}
	var f Frame
	if err := json.Unmarshal(data, &f); err != nil {
		return Frame{}, false
	}
	if f.Width <= 0 || f.Height <= 0 {
		return Frame{}, false
	}
	return f, true
}

// Save writes the frame, creating the parent directory when needed. It is
// best-effort by contract: callers log the error and keep running.
func Save(path string, f Frame) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create frame directory: %w", err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode window frame: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write window frame: %w", err)
	}
	return nil
}

// Clamp pulls the frame back on-screen: it keeps the screen the frame
// overlaps most and holds at least minVisible points of the window visible
// there. A frame off every screen lands on the first screen's origin, a
// window at least as large as that screen pins to the screen's origin, and
// an empty screen list (screens unavailable before the app runs) passes the
// frame through unchanged.
func Clamp(f Frame, screens []Screen) Frame {
	if len(screens) == 0 {
		return f
	}

	bounds := Rect{X: f.X, Y: f.Y, Width: f.Width, Height: f.Height}
	best := -1
	bestOverlap := 0
	for i, s := range screens {
		if o := overlapArea(bounds, s.Bounds); o > bestOverlap {
			bestOverlap, best = o, i
		}
	}
	if best == -1 {
		// Entirely off-screen (display unplugged, geometry changed): park
		// the frame on the first screen's origin, size intact.
		return Frame{
			X:      screens[0].Bounds.X,
			Y:      screens[0].Bounds.Y,
			Width:  f.Width,
			Height: f.Height,
		}
	}

	b := screens[best].Bounds
	return Frame{
		X:      keepVisible(f.X, f.Width, b.X, b.Width),
		Y:      keepVisible(f.Y, f.Height, b.Y, b.Height),
		Width:  f.Width,
		Height: f.Height,
	}
}

// overlapArea returns the intersecting area of two rectangles.
func overlapArea(a, b Rect) int {
	w := min(a.X+a.Width, b.X+b.Width) - max(a.X, b.X)
	h := min(a.Y+a.Height, b.Y+b.Height) - max(a.Y, b.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// keepVisible clamps one axis so at least minVisible points of a window of
// the given size stay on the screen; a window at least as large as the
// screen pins to the screen's origin.
func keepVisible(pos, size, screenPos, screenSize int) int {
	if size >= screenSize {
		return screenPos
	}
	return min(max(pos, screenPos-size+minVisible), screenPos+screenSize-minVisible)
}
