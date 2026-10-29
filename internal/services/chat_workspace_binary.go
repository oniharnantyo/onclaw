package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
)

// The workspace binary bound read (add-turn-artifacts design D8, spec
// chat-panels "Preview sources"): the workspace file preview's binary
// sibling of WorkspaceFile (chat.go) — a jailed read addressed by a
// workspace-relative path, so a `.pdf` workspace file can render as a
// document in the panel instead of as garbage text. The same jail as the
// text read applies: traversal, absolute paths, and symlink escapes are
// refused before the open, and nothing outside the agent's workspace is ever
// served. The on-disk path never crosses the boundary — the bytes travel
// base64-encoded.

// WorkspaceFileBinaryContent is one workspace file's served binary content:
// its mime (picked by extension), its byte size, and the base64-encoded
// bytes. The on-disk path never crosses the boundary.
type WorkspaceFileBinaryContent struct {
	// Mime is the content type the preview renders by; only `.pdf` is
	// distinguished — everything else is application/octet-stream.
	Mime string `json:"mime"`
	// SizeBytes is the file's size in bytes.
	SizeBytes int64 `json:"sizeBytes"`
	// Data is the base64-encoded file content.
	Data string `json:"data"`
}

// WorkspaceFileBinary reads one file from the agent's workspace as binary
// (add-turn-artifacts design D8): relPath is workspace-relative and jailed
// exactly like WorkspaceFile — traversal, absolute paths, and symlink
// escapes are refused before the open. The mime is picked by extension:
// `.pdf` (case-insensitive) is application/pdf, everything else
// application/octet-stream.
func (s *ChatService) WorkspaceFileBinary(ctx context.Context, agentID, relPath string) (WorkspaceFileBinaryContent, error) {
	agent, err := s.workspaceAgent(ctx, agentID)
	if err != nil {
		return WorkspaceFileBinaryContent{}, err
	}
	content, err := s.workspaces.ReadFile(agent.Slug, relPath)
	if err != nil {
		return WorkspaceFileBinaryContent{}, fmt.Errorf("workspace file binary: %w", err)
	}
	mime := "application/octet-stream"
	if strings.EqualFold(filepath.Ext(relPath), ".pdf") {
		mime = "application/pdf"
	}
	return WorkspaceFileBinaryContent{
		Mime:      mime,
		SizeBytes: int64(len(content)),
		Data:      base64.StdEncoding.EncodeToString([]byte(content)),
	}, nil
}
