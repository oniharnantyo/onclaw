package services

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/promptgen"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// promptDocumentNames lists the three workspace documents in composition
// order; the config Identity tab's file rail mirrors it.
var promptDocumentNames = []string{workspaces.DocIdentity, workspaces.DocSoul, workspaces.DocBootstrap}

// PromptDocuments is the current content of an agent's three prompt
// documents, as bound to the frontend. The Exists flags report which files
// are present: a failed or never-generated agent legitimately holds none.
type PromptDocuments struct {
	Identity        string
	IdentityExists  bool
	Soul            string
	SoulExists      bool
	Bootstrap       string
	BootstrapExists bool
}

// GetPromptDocuments reads the agent's three workspace documents from disk;
// store.ErrNotFound when the agent is absent. Missing files come back empty
// with Exists=false — the normal state of an agent whose generation failed
// or never ran.
func (s *AgentsService) GetPromptDocuments(ctx context.Context, agentID string) (PromptDocuments, error) {
	stored, err := s.store.Agents().ByID(ctx, agentID)
	if err != nil {
		return PromptDocuments{}, fmt.Errorf("get prompt documents: %w", err)
	}
	dir, err := s.workspaces.Dir(stored.Slug)
	if err != nil {
		return PromptDocuments{}, fmt.Errorf("get prompt documents: %w", err)
	}

	var out PromptDocuments
	if out.Identity, out.IdentityExists, err = s.workspaces.ReadDocument(dir, workspaces.DocIdentity); err != nil {
		return PromptDocuments{}, fmt.Errorf("get prompt documents: %w", err)
	}
	if out.Soul, out.SoulExists, err = s.workspaces.ReadDocument(dir, workspaces.DocSoul); err != nil {
		return PromptDocuments{}, fmt.Errorf("get prompt documents: %w", err)
	}
	if out.Bootstrap, out.BootstrapExists, err = s.workspaces.ReadDocument(dir, workspaces.DocBootstrap); err != nil {
		return PromptDocuments{}, fmt.Errorf("get prompt documents: %w", err)
	}
	return out, nil
}

// SavePromptDocument rewrites one of the agent's three workspace documents
// with content through the manager's atomic write — an existing file is
// backed up to <name>.bak first. Unknown document names and absent agents
// are errors (store.ErrNotFound for the latter).
func (s *AgentsService) SavePromptDocument(ctx context.Context, agentID, doc, content string) error {
	switch doc {
	case workspaces.DocIdentity, workspaces.DocSoul, workspaces.DocBootstrap:
	default:
		return fmt.Errorf("save prompt document: unknown document %q (want one of %v)", doc, promptDocumentNames)
	}
	stored, err := s.store.Agents().ByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("save prompt document: %w", err)
	}
	dir, err := s.workspaces.Dir(stored.Slug)
	if err != nil {
		return fmt.Errorf("save prompt document: %w", err)
	}
	if err := s.workspaces.WriteDocumentWithBackup(dir, doc, content); err != nil {
		return fmt.Errorf("save prompt document: %w", err)
	}
	return nil
}

// workspaceDocStore adapts the workspaces manager to promptgen.DocStore, so
// the generation service reads and commits the three documents without
// knowing about directories.
type workspaceDocStore struct{ manager *workspaces.Manager }

// NewWorkspaceDocStore returns the promptgen.DocStore serving document files
// from the manager's per-slug workspace directories.
func NewWorkspaceDocStore(m *workspaces.Manager) promptgen.DocStore {
	return workspaceDocStore{manager: m}
}

// ReadDocuments implements promptgen.DocStore: exists is true when the
// workspace holds at least one of the three documents.
func (w workspaceDocStore) ReadDocuments(slug string) (promptgen.Documents, bool, error) {
	dir, err := w.manager.Dir(slug)
	if err != nil {
		return promptgen.Documents{}, false, err
	}

	var out promptgen.Documents
	var identityOK, soulOK, bootstrapOK bool
	if out.Identity, identityOK, err = w.manager.ReadDocument(dir, workspaces.DocIdentity); err != nil {
		return promptgen.Documents{}, false, err
	}
	if out.Soul, soulOK, err = w.manager.ReadDocument(dir, workspaces.DocSoul); err != nil {
		return promptgen.Documents{}, false, err
	}
	if out.Bootstrap, bootstrapOK, err = w.manager.ReadDocument(dir, workspaces.DocBootstrap); err != nil {
		return promptgen.Documents{}, false, err
	}
	return out, identityOK || soulOK || bootstrapOK, nil
}

// CommitDocuments implements promptgen.DocStore. It is two-phase so a backup
// failure aborts the whole commit with every existing document's content
// untouched (design D6): phase one backs up each existing document in place
// by rewriting its identical content through the manager's backup-then-write
// path, phase two commits the new content the same way. A phase-two failure
// still leaves every previous content recoverable from the .bak files.
func (w workspaceDocStore) CommitDocuments(slug string, docs promptgen.Documents) error {
	dir, err := w.manager.Create(slug)
	if err != nil {
		return err
	}
	contents := map[string]string{
		workspaces.DocIdentity:  docs.Identity,
		workspaces.DocSoul:      docs.Soul,
		workspaces.DocBootstrap: docs.Bootstrap,
	}

	// Phase one: back up every existing document, content unchanged.
	for _, name := range promptDocumentNames {
		current, exists, err := w.manager.ReadDocument(dir, name)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if err := w.manager.WriteDocumentWithBackup(dir, name, current); err != nil {
			return err
		}
	}
	// Phase two: commit the new content.
	for _, name := range promptDocumentNames {
		if err := w.manager.WriteDocumentWithBackup(dir, name, contents[name]); err != nil {
			return err
		}
	}
	return nil
}
