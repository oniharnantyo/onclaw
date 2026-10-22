package fake

import "time"

// nowUTC stamps timestamps app-side, mirroring the app-managed timestamp
// invariant of the real adapters.
func nowUTC() time.Time { return time.Now().UTC() }
