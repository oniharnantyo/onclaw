package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The generated-document bound surfaces (add-create-document design D7, spec
// chat-tool-cards "Generated document access"): the webview obtains a
// create_document product's bytes only through these calls, addressed by the
// plain file name the tool reported. The name is validated BEFORE any
// filesystem access and resolved solely inside the calling agent's workspace
// through the same jailed reader the workspace preview uses — the on-disk
// path never crosses the boundary. The reads resolve the plain name through
// the generations' homes in recency order: the calling session's
// sessions/<id>/artifacts/ first (the per-session layout), the legacy
// workspace-root artifacts/ second (add-turn-artifacts design D6), the
// pre-D6 workspace root last — so cards recorded before each move keep
// rendering. The Save As export (add-artifact-save-as design D1) is the
// write sibling: it copies the same jailed source to a destination the
// host's native save dialog chose.

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

// refuseGeneratedDocumentName reports whether name is not a plain basename —
// anything carrying directory components, including traversal and absolute
// paths, is refused before any filesystem call. Backslash names are refused
// too: a document name is never a windows-style path.
func refuseGeneratedDocumentName(name string) bool {
	return name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) || filepath.Base(name) != name
}

// WithSaveDialog supplies the chat service's native save dialog for the Save
// As export (add-artifact-save-as design D1). Named apart from the app-wide
// seam to make clear it scopes to ChatService, like WithChatBrowserDir.
func WithSaveDialog(sd SaveDialog) ChatOption {
	return func(s *ChatService) { s.saves = sd }
}

// GeneratedDocument reads one document the create_document tool wrote under
// the calling session's artifacts directory — sessions/<id>/artifacts/ —
// falling back to the legacy workspace-root artifacts/ (add-turn-artifacts
// design D6) and then the pre-D6 workspace root: name must be a plain
// basename with no directory components — anything else, including
// traversal and absolute paths, is refused before any filesystem call, and
// a non-conforming sessionID is refused the same way. The agent is resolved
// exactly like the workspace preview surface (WorkspaceFile): by agent id,
// so the card calls this from a session context — sessionID locates the
// generation's home, never the file itself. A well-formed name with no file
// in any location wraps store.ErrNotFound.
func (s *ChatService) GeneratedDocument(ctx context.Context, agentID, sessionID, name string) (GeneratedDocumentContent, error) {
	// Refusal before the filesystem: a plain basename joined by the jailed
	// reader can only ever land inside the agent's workspace (spec: refused
	// names touch no path). Backslash names are refused too — a document
	// name is never a windows-style path.
	if refuseGeneratedDocumentName(name) {
		return GeneratedDocumentContent{}, fmt.Errorf("generated document: refused name %q", name)
	}
	agent, err := s.workspaceAgent(ctx, agentID)
	if err != nil {
		return GeneratedDocumentContent{}, fmt.Errorf("generated document: %w", err)
	}
	content, err := s.readGeneratedDocument(agent.Slug, sessionID, name)
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

// SaveGeneratedDocumentAs exports one generated document to a destination the
// user picks in the host's native save dialog (add-artifact-save-as design
// D1): name is refused exactly like GeneratedDocument BEFORE any dialog or
// filesystem access, the agent is resolved by agent id, and the dialog is
// seeded with the artifact file name. The jailed source is read exactly like
// GeneratedDocument (the session's artifacts/ first, the legacy homes after)
// and copied byte-for-byte to the chosen destination, which the method
// returns. A user cancellation is ErrExportCancelled and no native dialog on
// this host is ErrExportUnavailable — both bare, their messages being the
// frontend contract. The workspace path never crosses the boundary.
func (s *ChatService) SaveGeneratedDocumentAs(ctx context.Context, agentID, sessionID, name string) (string, error) {
	// Refusal before the dialog or the filesystem (design D1): the same
	// predicate as GeneratedDocument, so a refused name never opens a dialog.
	if refuseGeneratedDocumentName(name) {
		return "", fmt.Errorf("generated document: refused name %q", name)
	}
	agent, err := s.workspaceAgent(ctx, agentID)
	if err != nil {
		return "", fmt.Errorf("generated document: %w", err)
	}
	dest, err := s.saves.SaveFile(name)
	if err != nil {
		return "", fmt.Errorf("generated document: dialog: %w", err)
	}
	if dest == "" {
		return "", ErrExportCancelled
	}
	content, err := s.readGeneratedDocument(agent.Slug, sessionID, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("generated document %s: %w", name, store.ErrNotFound)
		}
		return "", fmt.Errorf("generated document %s: %w", name, err)
	}
	// The string the jailed reader returned is byte-preserving, so writing
	// its bytes back is an exact copy.
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("generated document %s: write %s: %w", name, dest, err)
	}
	return dest, nil
}

// readGeneratedDocument resolves one generated document's bytes through the
// jailed workspace reader, newest home first: the calling session's
// sessions/<id>/artifacts/ (only when sessionID is a minted session id — a
// non-conforming one is refused before any read), the legacy workspace-root
// artifacts/ second, the pre-D6 workspace root last.
func (s *ChatService) readGeneratedDocument(slug, sessionID, name string) (string, error) {
	if sessionID != "" {
		if !workspaces.ValidSessionID(sessionID) {
			return "", fmt.Errorf("generated document: refused session id %q", sessionID)
		}
		content, err := s.workspaces.ReadFile(slug, path.Join(workspaces.DirSessions, sessionID, workspaces.DirArtifacts, name))
		if !errors.Is(err, fs.ErrNotExist) {
			return content, err
		}
	}
	// Legacy homes (add-turn-artifacts design D6): the workspace-root
	// artifacts/ first, the pre-D6 root second — both plain-name reads
	// inside the jailed workspace.
	content, err := s.workspaces.ReadFile(slug, path.Join(workspaces.DirArtifacts, name))
	if errors.Is(err, fs.ErrNotExist) {
		content, err = s.workspaces.ReadFile(slug, name)
	}
	return content, err
}
