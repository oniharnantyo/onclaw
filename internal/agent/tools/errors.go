package tools

import (
	"errors"
	"fmt"
	"os"
)

// Sentinel errors classify expected, recoverable filesystem failures so the
// FSErrorMiddleware can convert them into tool-result observations instead of
// fatal tool errors. The Eino agent loop treats every non-nil tool error as
// terminal, so only conditions the model can reasonably adapt to are
// classified here. Genuine infrastructure failures (unrecoverable disk I/O,
// etc.) are deliberately left unclassified and stay fatal.
var (
	ErrPathOutsideWorkspace = errors.New("path outside workspace")
	ErrFileNotFound         = errors.New("file not found")
	ErrPermissionDenied     = errors.New("permission denied")
	ErrEditNotUnique        = errors.New("edit not unique")
	ErrEditOldStringMissing = errors.New("old string missing")
	ErrEmptyPattern         = errors.New("empty pattern")
	ErrInvalidRegex         = errors.New("invalid regex")
	ErrInvalidGlob          = errors.New("invalid glob")
)

// fsExpectedSentinels is the set the middleware converts to observations.
var fsExpectedSentinels = []error{
	ErrPathOutsideWorkspace,
	ErrFileNotFound,
	ErrPermissionDenied,
	ErrEditNotUnique,
	ErrEditOldStringMissing,
	ErrEmptyPattern,
	ErrInvalidRegex,
	ErrInvalidGlob,
}

// IsExpectedFSError reports whether err matches a classified, recoverable
// filesystem condition. errors.Is is used so wrapped (%w) errors match.
func IsExpectedFSError(err error) bool {
	for _, s := range fsExpectedSentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// wrapSentinel annotates a sentinel with the offending requested path/value.
// Only the requested input is quoted — never the absolute workspace root.
func wrapSentinel(sentinel error, value string) error {
	return fmt.Errorf("%w: %q", sentinel, value)
}

// mapOSError maps OS-level errors to classified sentinels, leaving other
// (genuine infrastructure) errors untouched. The value is the requested input.
func mapOSError(err error, value string) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return wrapSentinel(ErrFileNotFound, value)
	case errors.Is(err, os.ErrPermission):
		return wrapSentinel(ErrPermissionDenied, value)
	default:
		return err
	}
}
