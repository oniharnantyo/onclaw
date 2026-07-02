package browser

import (
	sysbrowser "github.com/oniharnantyo/onclaw/internal/browser"
)

var (
	// Mgr is the shared browser manager singleton.
	Mgr = sysbrowser.NewManager()
)
