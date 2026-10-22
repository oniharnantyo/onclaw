package agents

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// PromptDocs supplies an agent's persona documents, read fresh from disk per
// turn (design D11: no cache — external edits apply to the next turn).
type PromptDocs interface {
	// Documents returns the agent's identity, soul and bootstrap contents for
	// the given slug. Missing documents come back as empty strings; an error
	// is returned only for real IO failures (spec: missing documents degrade
	// cleanly, agents without documents still chat).
	Documents(ctx context.Context, slug string) (identity, soul, bootstrap string, err error)
}

// workspacePromptDocs is the workspaces-backed PromptDocs source.
type workspacePromptDocs struct {
	workspaces *workspaces.Manager
}

// NewWorkspacePromptDocs adapts the workspaces manager into the runtime's
// PromptDocs source: it reads IDENTITY.md, SOUL.md and BOOTSTRAP.md from the
// slug's workspace directory fresh on every call.
func NewWorkspacePromptDocs(m *workspaces.Manager) PromptDocs {
	return &workspacePromptDocs{workspaces: m}
}

// Documents implements PromptDocs. A slug-less agent owns no workspace at
// all (the migration-window shape), so its documents degrade to absent.
func (w *workspacePromptDocs) Documents(_ context.Context, slug string) (string, string, string, error) {
	if slug == "" {
		return "", "", "", nil
	}
	dir, err := w.workspaces.Dir(slug)
	if err != nil {
		return "", "", "", fmt.Errorf("prompt docs: %w", err)
	}
	identity, err := w.read(dir, workspaces.DocIdentity)
	if err != nil {
		return "", "", "", err
	}
	soul, err := w.read(dir, workspaces.DocSoul)
	if err != nil {
		return "", "", "", err
	}
	bootstrap, err := w.read(dir, workspaces.DocBootstrap)
	if err != nil {
		return "", "", "", err
	}
	return identity, soul, bootstrap, nil
}

// read reads one document; a missing file is the normal absent state ("").
func (w *workspacePromptDocs) read(dir, name string) (string, error) {
	content, _, err := w.workspaces.ReadDocument(dir, name)
	if err != nil {
		return "", fmt.Errorf("prompt docs: %w", err)
	}
	return content, nil
}
