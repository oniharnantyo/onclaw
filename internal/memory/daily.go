package memory

import (
	"path/filepath"
	"time"
)

// dailyFormat is the daily-note filename layout (YYYY-MM-DD).
const dailyFormat = "2006-01-02"

// DailyPath returns the daily-note path of the given day
// (<memory>/YYYY-MM-DD.md): the working notes an agent appends observations
// to during its turns; the sweep is what promotes their durable lines.
func (l Layout) DailyPath(day time.Time) string {
	return filepath.Join(l.Dir, day.Format(dailyFormat)+".md")
}
