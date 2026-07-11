package service

import (
	"context"
	"os"

	"github.com/oniharnantyo/onclaw/internal/skill"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// DiscoverSkills fetches a source and lists its candidate skills without installing them.
func (s *Service) DiscoverSkills(ctx context.Context, input DiscoverInput) (DiscoverResult, error) {
	pkgName, isPlugin, candidates, tempDir, err := s.installer.DiscoverSource(ctx, input.Source, input.Branch, false)
	if err != nil {
		return DiscoverResult{}, classify(err)
	}
	defer func() {
		if tempDir != "" {
			_ = os.RemoveAll(tempDir)
		}
	}()

	var skills []DiscoveredSkill
	for _, cand := range candidates {
		skills = append(skills, DiscoveredSkill{
			Name:        cand.Name,
			Description: cand.Description,
		})
	}

	return DiscoverResult{
		PackageName: pkgName,
		IsPlugin:    isPlugin,
		Skills:      skills,
	}, nil
}

// InstallSkills installs selected skills from a source.
func (s *Service) InstallSkills(ctx context.Context, input InstallSkillInput) ([]SkillView, error) {
	opts := skill.InstallOpts{
		Force:  input.Force,
		AsName: input.AsName,
		Branch: input.Branch,
	}

	installed, err := s.installer.Install(ctx, input.Source, input.SelectedNames, input.Scope, opts)
	if err != nil {
		return nil, classify(err)
	}

	var views []SkillView
	for _, sk := range installed {
		views = append(views, mapSkillToView(sk))
	}
	return views, nil
}

// ListSkills returns all installed skills.
func (s *Service) ListSkills(ctx context.Context) ([]SkillView, error) {
	if s.installer == nil {
		return []SkillView{}, nil
	}
	skills, err := s.installer.List(ctx)
	if err != nil {
		return nil, classify(err)
	}

	var views []SkillView
	for _, sk := range skills {
		views = append(views, mapSkillToView(sk))
	}
	return views, nil
}

// GetSkill retrieves details of a specific installed skill.
func (s *Service) GetSkill(ctx context.Context, name string, scope string) (SkillView, error) {
	sk, err := s.installer.GetSkill(ctx, name, scope)
	if err != nil {
		return SkillView{}, classify(err)
	}
	return mapSkillToView(sk), nil
}

// RemoveSkill uninstalls a skill.
func (s *Service) RemoveSkill(ctx context.Context, name string, scope string) error {
	return classify(s.installer.Remove(ctx, name, scope))
}

// UpdateSkill updates a skill from its original source.
func (s *Service) UpdateSkill(ctx context.Context, name string, scope string) (SkillView, error) {
	updated, err := s.installer.Update(ctx, name, scope)
	if err != nil {
		return SkillView{}, classify(err)
	}
	return mapSkillToView(updated), nil
}

// Helper maps store.Skill to service.SkillView
func mapSkillToView(sk *store.Skill) SkillView {
	return SkillView{
		Name:        sk.Name,
		Scope:       sk.Scope,
		SourceType:  sk.SourceType,
		Source:      sk.Source,
		SkillPath:   sk.SkillPath,
		Version:     sk.Version,
		Hash:        sk.Hash,
		Description: sk.Description,
		Enabled:     sk.Enabled == 1,
		InstalledAt: sk.InstalledAt,
		UpdatedAt:   sk.UpdatedAt,
	}
}
