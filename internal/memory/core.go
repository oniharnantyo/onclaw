package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sentinel errors classify expected, recoverable MEMORY.md write conditions.
// The agent tool converts these into recoverable observations instead of
// fatal tool errors; genuine I/O failures (read/write of MEMORY.md) remain
// unwrapped and stay fatal so real infrastructure breakage is not masked.
var (
	ErrTargetNotFound    = errors.New("memory target not found")
	ErrTargetNotUnique   = errors.New("memory target not unique")
	ErrTargetRequired    = errors.New("memory target required")
	ErrUnknownOp         = errors.New("unknown memory operation")
	ErrCharLimitExceeded = errors.New("memory character limit exceeded")
)

// FileCoreStore implements CoreStore, reading and writing MEMORY.md in the agent workspace.
type FileCoreStore struct {
	CharLimit int
}

// NewFileCoreStore constructs a FileCoreStore instance.
func NewFileCoreStore(charLimit int) CoreStore {
	if charLimit <= 0 {
		charLimit = 3200 // default 800 tokens * 4 chars
	}
	return &FileCoreStore{CharLimit: charLimit}
}

// ReadCore reads the curated core file from the workspace.
func (s *FileCoreStore) ReadCore(ctx context.Context, workspace string) (string, error) {
	path := filepath.Join(workspace, "MEMORY.md")
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read MEMORY.md: %w", err)
	}
	return string(content), nil
}

// WriteCore modifies the curated core file using add, replace, or remove operations.
func (s *FileCoreStore) WriteCore(ctx context.Context, workspace string, op string, target string, content string) (string, error) {
	// Security scan for new content
	if op != "remove" {
		if err := ScanContent(content); err != nil {
			return "", err
		}
	}

	path := filepath.Join(workspace, "MEMORY.md")
	existingBytes, err := os.ReadFile(path)
	var existing string
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read MEMORY.md for edit: %w", err)
		}
	} else {
		existing = string(existingBytes)
	}

	var newContent string
	switch op {
	case "add":
		if existing == "" {
			newContent = content
		} else {
			if !strings.HasSuffix(existing, "\n") {
				newContent = existing + "\n" + content
			} else {
				newContent = existing + content
			}
		}
	case "replace":
		if target == "" {
			return "", fmt.Errorf("%w: replace operation requires a target string", ErrTargetRequired)
		}
		count := strings.Count(existing, target)
		if count == 0 {
			return "", fmt.Errorf("%w: target string not found in MEMORY.md: %q", ErrTargetNotFound, target)
		}
		if count > 1 {
			return "", fmt.Errorf("%w: target string is not unique in MEMORY.md (found %d occurrences): %q", ErrTargetNotUnique, count, target)
		}
		newContent = strings.Replace(existing, target, content, 1)
	case "remove":
		if target == "" {
			return "", fmt.Errorf("%w: remove operation requires a target string", ErrTargetRequired)
		}
		count := strings.Count(existing, target)
		if count == 0 {
			return "", fmt.Errorf("%w: target string not found in MEMORY.md: %q", ErrTargetNotFound, target)
		}
		if count > 1 {
			return "", fmt.Errorf("%w: target string is not unique in MEMORY.md (found %d occurrences): %q", ErrTargetNotUnique, count, target)
		}
		newContent = strings.Replace(existing, target, "", 1)
	default:
		return "", fmt.Errorf("%w: unknown operation: %q", ErrUnknownOp, op)
	}

	// Verify character cap limit
	if len(newContent) > s.CharLimit {
		return "", fmt.Errorf("%w: write would exceed character limit of %d (result would be %d characters). Please consolidate or delete old memories first.", ErrCharLimitExceeded, s.CharLimit, len(newContent))
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("create workspace directory: %w", err)
	}

	// Write changes
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("write MEMORY.md: %w", err)
	}

	return newContent, nil
}
