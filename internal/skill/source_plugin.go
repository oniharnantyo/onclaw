package skill

import (
	"os"
	"path/filepath"
)

// detectPlugin checks if a directory contains a Claude plugin manifest at .claude-plugin/plugin.json.
func detectPlugin(dir string) bool {
	pluginPath := filepath.Join(dir, ".claude-plugin", "plugin.json")
	fi, err := os.Stat(pluginPath)
	return err == nil && !fi.IsDir()
}
