package cli

import (
	"testing"
)

func TestResolveContextWindow(t *testing.T) {
	// Case 1: Agent override present
	t.Run("agent override", func(t *testing.T) {
		res := resolveContextWindow(12000, 8000, `{"context_window":4000}`)
		if res != 12000 {
			t.Errorf("expected 12000, got %d", res)
		}
	})

	// Case 2: Global config present (no agent override)
	t.Run("global config wins over model default", func(t *testing.T) {
		res := resolveContextWindow(0, 8000, `{"context_window":4000}`)
		if res != 8000 {
			t.Errorf("expected 8000, got %d", res)
		}
	})

	// Case 3: Model metadata default wins (no agent override, no global config)
	t.Run("model default wins", func(t *testing.T) {
		res := resolveContextWindow(0, 0, `{"context_window":4000}`)
		if res != 4000 {
			t.Errorf("expected 4000, got %d", res)
		}
	})

	// Case 4: Fallback to 64000
	t.Run("fallback value when none set", func(t *testing.T) {
		res := resolveContextWindow(0, 0, "")
		if res != 64000 {
			t.Errorf("expected 64000, got %d", res)
		}
	})
}
