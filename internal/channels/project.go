package channels

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// LocalProjectSpace materializes the shared per-channel project directories
// under the workspace data root (channel-teams D5). Each directory nests
// under the workspace's on-disk root — the same `<root>/<workspace-slug>`
// layout the agent directories use — so removing a workspace takes its
// projects with it. It satisfies agents.ProjectSpace; the runner calls Ensure
// before mounting the directory read-write at /project in a member agent's
// jail.
type LocalProjectSpace struct {
	dataDir    string
	workspaces store.WorkspaceStore
}

// The local project space is the ProjectSpace implementation the composition
// root wires.
var _ agents.ProjectSpace = (*LocalProjectSpace)(nil)

// NewLocalProjectSpace creates the local project space rooted at dataDir (the
// OnClaw data directory). The workspace store resolves each Ensure/Remove
// call's workspaceID to the slug-keyed on-disk workspace root; the pinned
// agents.ProjectSpace interface carries no context, so the store lookup runs
// on context.Background — a single indexed read.
func NewLocalProjectSpace(dataDir string, workspaces store.WorkspaceStore) *LocalProjectSpace {
	return &LocalProjectSpace{dataDir: dataDir, workspaces: workspaces}
}

// Ensure creates (idempotently) the channel's project directory at
// `<dataDir>/workspaces/<workspace-slug>/projects/<channel-slug>/` and
// returns its absolute path. The channel slug is validated before it may
// become a path component, so no traversal can sneak into the layout.
func (s *LocalProjectSpace) Ensure(workspaceID, channelSlug string) (string, error) {
	dir, err := s.projectDir(workspaceID, channelSlug)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create project directory: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve project directory: %w", err)
	}
	return abs, nil
}

// Remove deletes the channel's project directory. Removing a missing
// directory is a no-op, so the channel-deletion path stays idempotent.
func (s *LocalProjectSpace) Remove(workspaceID, channelSlug string) error {
	dir, err := s.projectDir(workspaceID, channelSlug)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove project directory: %w", err)
	}
	return nil
}

// projectDir resolves the workspace root for workspaceID and joins the
// slug-validated channel component: `<root>/<workspace-slug>/projects/<slug>`.
func (s *LocalProjectSpace) projectDir(workspaceID, channelSlug string) (string, error) {
	if err := domain.ValidateChannelSlug(channelSlug); err != nil {
		return "", fmt.Errorf("invalid channel slug: %w", err)
	}
	ws, err := s.workspaces.ByID(context.Background(), workspaceID)
	if err != nil {
		return "", fmt.Errorf("load workspace: %w", err)
	}
	return filepath.Join(domain.WorkspaceRoot(s.dataDir), ws.Slug, "projects", channelSlug), nil
}
