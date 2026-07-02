package skill

import (
	"path/filepath"
)

// TargetDir returns the install target directory for a given scope.
// The global scope ("global" or "") installs into <home>/skills; any other
// value is treated as an agent name and installs into
// <home>/workspace/<agent>/.agents/skills.
//
// Runtime resolution of skill directories (which dirs an agent reads from, and
// in what precedence order) is an agent concern and lives in
// internal/agent/middlewares.ResolveDirs.
func TargetDir(home, scope string) string {
	if scope == "global" || scope == "" {
		return filepath.Join(home, "skills")
	}
	return filepath.Join(home, "workspace", scope, ".agents", "skills")
}
