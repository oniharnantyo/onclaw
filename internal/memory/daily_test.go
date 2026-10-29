package memory

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDailyPath covers the daily-note filename: the given day renders as
// YYYY-MM-DD.md inside the memory directory.
func TestDailyPath(t *testing.T) {
	layout := NewLayout("/ws/agent")

	day := time.Date(2026, 10, 27, 14, 30, 0, 0, time.UTC)
	require.Equal(t, filepath.Join("/ws/agent", DirName, "2026-10-27.md"), layout.DailyPath(day))

	// Zero-padded months and days, and no time-of-day component.
	early := time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC)
	require.Equal(t, filepath.Join("/ws/agent", DirName, "2026-02-05.md"), layout.DailyPath(early))
}
