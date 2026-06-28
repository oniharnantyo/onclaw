package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

// TranscriptEntry represents a single turn or event recorded in the transcript.
type TranscriptEntry struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`                // "user", "assistant", "tool_call", "tool_result", "interrupted", "error"
	Name      string `json:"name,omitempty"`      // for tool_call/tool_result
	Content   string `json:"content,omitempty"`   // for user, assistant
	Arguments string `json:"arguments,omitempty"` // for tool_call
	Result    string `json:"result,omitempty"`    // for tool_result
	Error     string `json:"error,omitempty"`     // for error
}

// AppendToTranscript writes an entry to the append-only transcript file, redacting any secrets, and performs fsync.
func AppendToTranscript(filePath string, entry *TranscriptEntry) error {
	// Ensure parent directory exists
	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, 0700); err != nil {
		return fmt.Errorf("failed to create transcript parent directory: %w", err)
	}

	// Redact secrets in all fields
	entry.Content = tools.Redact(entry.Content)
	entry.Arguments = tools.Redact(entry.Arguments)
	entry.Result = tools.Redact(entry.Result)
	entry.Error = tools.Redact(entry.Error)

	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal transcript entry: %w", err)
	}

	// Open in append mode, create if not exists
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to open transcript file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write to transcript file: %w", err)
	}

	// fsync per turn
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync transcript file: %w", err)
	}

	return nil
}
