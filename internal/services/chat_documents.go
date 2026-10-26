package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/oniharnantyo/buddies/internal/store"
)

// The generated-document bound read (add-create-document design D7, spec
// chat-tool-cards "Generated document access"): the webview obtains a
// create_document product's bytes only through this call, addressed by the
// plain file name the tool reported. The name is validated BEFORE any
// filesystem access and resolved solely inside the calling agent's workspace
// through the same jailed reader the workspace preview uses — the on-disk
// path never crosses the boundary.

// GeneratedDocumentContent is one generated document's served content: the
// name it was addressed by, its byte size, and the base64-encoded bytes.
// The on-disk path never crosses the boundary.
type GeneratedDocumentContent struct {
	Name string `json:"name"`
	// SizeBytes is the document's size in bytes.
	SizeBytes int64 `json:"sizeBytes"`
	// Data is the base64-encoded document content.
	Data string `json:"data"`
}

// GeneratedDocument reads one document the create_document tool wrote at the
// agent's workspace root: name must be a plain basename with no directory
// components — anything else, including traversal and absolute paths, is
// refused before any filesystem call. The agent is resolved exactly like the
// workspace preview surface (WorkspaceFile): by agent id, so the card can
// call this from a session context — not by session id. A well-formed name
// with no file behind it wraps store.ErrNotFound.
func (s *ChatService) GeneratedDocument(ctx context.Context, agentID, name string) (GeneratedDocumentContent, error) {
	// Refusal before the filesystem: a plain basename joined by the jailed
	// reader can only ever land at the workspace root (spec: refused names
	// touch no path). Backslash names are refused too — a document name is
	// never a windows-style path.
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) || filepath.Base(name) != name {
		return GeneratedDocumentContent{}, fmt.Errorf("generated document: refused name %q", name)
	}
	agent, err := s.workspaceAgent(ctx, agentID)
	if err != nil {
		return GeneratedDocumentContent{}, fmt.Errorf("generated document: %w", err)
	}
	content, err := s.workspaces.ReadFile(agent.Slug, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return GeneratedDocumentContent{}, fmt.Errorf("generated document %s: %w", name, store.ErrNotFound)
		}
		return GeneratedDocumentContent{}, fmt.Errorf("generated document %s: %w", name, err)
	}
	return GeneratedDocumentContent{
		Name:      name,
		SizeBytes: int64(len(content)),
		Data:      base64.StdEncoding.EncodeToString([]byte(content)),
	}, nil
}
