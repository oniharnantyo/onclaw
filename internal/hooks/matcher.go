package hooks

import (
	"regexp"
)

// MatchToolName checks if the hook matcher pattern matches the event's tool name.
// An empty pattern matches everything.
func MatchToolName(pattern string, toolName string) bool {
	if pattern == "" {
		return true
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(toolName)
}
