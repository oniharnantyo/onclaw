package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// errSkillsCapabilityOff rejects the workspace-skill write bindings of a
// runtime built without a skills capability (AgentsService.skillsDir empty):
// there is no global library to promote into and the skills tiers are not
// mounted at all — the same posture resolveSkills takes on an empty
// skillsDir.
var errSkillsCapabilityOff = errors.New("skills capability is not configured")

// WorkspaceSkill is one workspace-tier skill as bound to the frontend: the
// skill's directory name plus the description parsed from its SKILL.md.
type WorkspaceSkill struct {
	Name        string
	Description string
}

// ListWorkspaceSkills lists the skills in the agent's own workspace skills
// directory (spec agent-management "Agent workspace skills management"): one
// entry per subdirectory carrying a SKILL.md, sorted by name, each with the
// description parsed exactly the way the skill backend parses it. The result
// is never nil; a workspace without skills (or an agent row without a
// workspace) lists empty. Unlike promote and delete this only reads the
// workspace tier, so it needs no skills capability.
func (s *AgentsService) ListWorkspaceSkills(ctx context.Context, agentID string) ([]WorkspaceSkill, error) {
	agent, err := s.store.Agents().ByID(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list workspace skills: %w", err)
	}
	if agent.Slug == "" {
		// The row never had a workspace (pre-backfill): nothing to list.
		return []WorkspaceSkill{}, nil
	}
	skillsDir, err := s.workspaces.SkillsDir(agent.Slug)
	if err != nil {
		return nil, fmt.Errorf("list workspace skills for agent %q: %w", agent.Slug, err)
	}
	return listSkillDirs(skillsDir)
}

// PromoteWorkspaceSkill copies one workspace-tier skill into the global
// skills library and registers it enabled (spec agent-skills "Skill
// promotion", design D2): the workspace copy is never modified and keeps
// shadowing the promoted skill for its creator. A name that already exists
// in the library is rejected with an error naming the collision and the
// library stays untouched (design D3). The runtime must have a skills
// capability; the skill name must be a DNS label.
func (s *AgentsService) PromoteWorkspaceSkill(ctx context.Context, agentID, name string) error {
	if s.skillsDir == "" {
		return fmt.Errorf("promote workspace skill: %w", errSkillsCapabilityOff)
	}
	if err := validateSkillName(name); err != nil {
		return fmt.Errorf("promote workspace skill: %w", err)
	}
	agent, wsSkillsDir, err := s.resolveWorkspaceSkillsDir(ctx, "promote workspace skill", agentID)
	if err != nil {
		return err
	}
	srcDir := filepath.Join(wsSkillsDir, name)
	info, err := os.Stat(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("promote workspace skill: skill %q not found in agent %q workspace", name, agent.Slug)
		}
		return fmt.Errorf("promote workspace skill: stat skill %q: %w", name, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("promote workspace skill: skill %q is not a directory", name)
	}
	content, err := os.ReadFile(filepath.Join(srcDir, backend.SkillMDFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("promote workspace skill: skill %q has no %s", name, backend.SkillMDFileName)
		}
		return fmt.Errorf("promote workspace skill: read %s of skill %q: %w", backend.SkillMDFileName, name, err)
	}

	dstDir := filepath.Join(s.skillsDir, name)
	if _, err := os.Stat(dstDir); err == nil {
		// Design D3: the curated tier is never silently overwritten — the
		// error names the collision and the library stays untouched.
		return fmt.Errorf("promote workspace skill: skill %q already exists in the skills library", name)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("promote workspace skill: stat library skill %q: %w", name, err)
	}
	if err := os.MkdirAll(dstDir, info.Mode()); err != nil {
		return fmt.Errorf("promote workspace skill: create library skill %q: %w", name, err)
	}
	if err := os.CopyFS(dstDir, os.DirFS(srcDir)); err != nil {
		_ = os.RemoveAll(dstDir) // a failed copy leaves the library untouched
		return fmt.Errorf("promote workspace skill: copy skill %q: %w", name, err)
	}
	// The workspace copy stays exactly as it was (design D2): only the
	// library and its registry row are written.
	if err := s.store.Skills().Upsert(ctx, &store.Skill{
		Name:        name,
		Description: backend.ParseSkillDescription(string(content)),
		Enabled:     true,
	}); err != nil {
		return fmt.Errorf("promote workspace skill: register skill %q: %w", name, err)
	}
	return nil
}

// DeleteWorkspaceSkill removes one workspace-tier skill directory (spec
// agent-skills "Delete removes only the workspace copy"): the global library
// is untouched. A nonexistent skill name is a not-found error. The runtime
// must have a skills capability; the skill name must be a DNS label.
func (s *AgentsService) DeleteWorkspaceSkill(ctx context.Context, agentID, name string) error {
	if s.skillsDir == "" {
		return fmt.Errorf("delete workspace skill: %w", errSkillsCapabilityOff)
	}
	if err := validateSkillName(name); err != nil {
		return fmt.Errorf("delete workspace skill: %w", err)
	}
	agent, wsSkillsDir, err := s.resolveWorkspaceSkillsDir(ctx, "delete workspace skill", agentID)
	if err != nil {
		return err
	}
	dir := filepath.Join(wsSkillsDir, name)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("delete workspace skill: skill %q not found in agent %q workspace", name, agent.Slug)
		}
		return fmt.Errorf("delete workspace skill: stat skill %q: %w", name, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete workspace skill: remove skill %q: %w", name, err)
	}
	return nil
}

// resolveWorkspaceSkillsDir fetches the agent row and its workspace skills
// directory — created on demand, the same seam resolveSkills reads. op names
// the calling binding for error context.
func (s *AgentsService) resolveWorkspaceSkillsDir(ctx context.Context, op, agentID string) (store.Agent, string, error) {
	agent, err := s.store.Agents().ByID(ctx, agentID)
	if err != nil {
		return store.Agent{}, "", fmt.Errorf("%s: %w", op, err)
	}
	if agent.Slug == "" {
		return store.Agent{}, "", fmt.Errorf("%s: agent %q has no workspace", op, agent.Name)
	}
	skillsDir, err := s.workspaces.SkillsDir(agent.Slug)
	if err != nil {
		return store.Agent{}, "", fmt.Errorf("%s: resolve agent %q workspace: %w", op, agent.Slug, err)
	}
	return *agent, skillsDir, nil
}

// validateSkillName rejects a skill-directory name that is not a DNS label
// (workspaces.ValidSlug) before any path is built from it: a skill name
// becomes a directory name under both tiers, so traversal must never reach
// the filesystem.
func validateSkillName(name string) error {
	if !workspaces.ValidSlug(name) {
		return fmt.Errorf("invalid skill name %q (want a DNS label: lowercase letters, digits, single interior dashes, at most 63 characters)", name)
	}
	return nil
}

// listSkillDirs lists the skill directories under dir the way the skill
// backend's collectTier discovers them: subdirectories containing a
// SKILL.md, each with the description parsed from that file. The result is
// sorted by name (os.ReadDir order) and never nil; a missing directory is
// the normal empty state.
func listSkillDirs(dir string) ([]WorkspaceSkill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []WorkspaceSkill{}, nil
		}
		return nil, fmt.Errorf("read skills directory %s: %w", dir, err)
	}
	skills := make([]WorkspaceSkill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name(), backend.SkillMDFileName))
		if err != nil {
			if os.IsNotExist(err) {
				continue // not a valid skill directory
			}
			return nil, fmt.Errorf("read skill %s: %w", entry.Name(), err)
		}
		skills = append(skills, WorkspaceSkill{
			Name:        entry.Name(),
			Description: backend.ParseSkillDescription(string(content)),
		})
	}
	return skills, nil
}
