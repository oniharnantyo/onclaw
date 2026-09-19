package domain

import (
	"errors"
	"fmt"
	"time"
)

// MaxMemoryContentChars is the shared cap across both memory documents
// (USER.md and WORKSPACE.md; ~8k tokens at ≈4 chars/token). It is enforced
// identically on every write path: the memory tool, the HTTP edit endpoints,
// and the store layer.
const MaxMemoryContentChars = 32000

// ErrMemoryCapExceeded is returned (wrapped, with sizes) when a memory write
// would push a document past MaxMemoryContentChars. Callers surface the trim
// escape hatch: a human edits the memory down via the UI.
var ErrMemoryCapExceeded = errors.New("memory content too large")

// Memory carries scope-free memory content; the scope (user/workspace)
// is the store call's arguments, never part of the value.
type Memory struct {
	Content   string
	UpdatedAt time.Time
}

// ValidateMemoryContent checks a whole memory document against the shared cap.
// The error names the current size, the cap, and the human-trim escape hatch.
func ValidateMemoryContent(content string) error {
	if n := len(content); n > MaxMemoryContentChars {
		return fmt.Errorf("%w: %d chars exceeds the %d char cap; trim it via the memory editor in the UI", ErrMemoryCapExceeded, n, MaxMemoryContentChars)
	}
	return nil
}

// ValidateMemoryAppend pre-checks an append against the resulting document
// size (current + addition), not just the appended fragment.
func ValidateMemoryAppend(current, addition string) error {
	if n := len(current) + len(addition); n > MaxMemoryContentChars {
		return fmt.Errorf("%w: %d chars exceeds the %d char cap; trim it via the memory editor in the UI", ErrMemoryCapExceeded, n, MaxMemoryContentChars)
	}
	return nil
}
