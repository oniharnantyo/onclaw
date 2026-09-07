package server

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/agents/backend"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/skills"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// workspaceSkillStoreAdapter bridges the persistence layer
// (store.WorkspaceSkillStore over domain.WorkspaceSkill rows) to the install
// pipeline's local port (skills.Store over skills.Skill). The two shapes are
// field-isomorphic apart from the row ID (skills.Skill is keyed by
// workspaceID+name) and the dependency set: the domain row stores dependency
// names, the pipeline's set carries per-dependency statuses, which are
// flattened to names on write and re-materialized only on install/recheck
// responses from the pipeline's in-memory rows.
type workspaceSkillStoreAdapter struct {
	store store.WorkspaceSkillStore
}

// newWorkspaceSkillStoreAdapter wraps a WorkspaceSkillStore as a skills.Store.
func newWorkspaceSkillStoreAdapter(wsStore store.WorkspaceSkillStore) *workspaceSkillStoreAdapter {
	return &workspaceSkillStoreAdapter{store: wsStore}
}

// WorkspaceSkillReader returns the runtime's read-only view of the registry:
// the names of enabled workspace skills for a workspace slug. It satisfies
// backend.EnabledSkillReader; the composition root passes it to the runner
// via agents.WithEnabledSkillReader (design D2/D3).
func WorkspaceSkillReader(wsStore store.WorkspaceSkillStore) backend.EnabledSkillReader {
	return newWorkspaceSkillStoreAdapter(wsStore)
}

// WorkspaceSkillsInstallStore returns the persistence-side skills.Store the
// install pipeline reads and writes through: the same adapter as the runtime
// reader, bound to the full WorkspaceSkillStore surface.
func WorkspaceSkillsInstallStore(wsStore store.WorkspaceSkillStore) skills.Store {
	return newWorkspaceSkillStoreAdapter(wsStore)
}

// EnabledSkillNames implements backend.EnabledSkillReader.
func (a *workspaceSkillStoreAdapter) EnabledSkillNames(ctx context.Context, workspaceSlug string) ([]string, error) {
	return a.store.ListEnabled(ctx, workspaceSlug)
}

func (a *workspaceSkillStoreAdapter) Create(ctx context.Context, skill *skills.Skill) error {
	if skill == nil {
		return domain.ErrInvalid
	}
	return a.store.Create(ctx, skillToDomain(skill))
}

func (a *workspaceSkillStoreAdapter) Get(ctx context.Context, workspaceID, name string) (*skills.Skill, error) {
	row, err := a.store.GetByName(ctx, workspaceID, name)
	if err != nil {
		return nil, err
	}
	return skillFromDomain(row), nil
}

func (a *workspaceSkillStoreAdapter) List(ctx context.Context, workspaceID string) ([]skills.Skill, error) {
	rows, err := a.store.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]skills.Skill, 0, len(rows))
	for i := range rows {
		out = append(out, *skillFromDomain(&rows[i]))
	}
	return out, nil
}

func (a *workspaceSkillStoreAdapter) ListEnabled(ctx context.Context, workspaceID string) ([]skills.Skill, error) {
	rows, err := a.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]skills.Skill, 0, len(rows))
	for _, row := range rows {
		if row.Enabled {
			out = append(out, row)
		}
	}
	return out, nil
}

func (a *workspaceSkillStoreAdapter) SetEnabled(ctx context.Context, workspaceID, name string, enabled bool) error {
	row, err := a.store.GetByName(ctx, workspaceID, name)
	if err != nil {
		return err
	}
	return a.store.SetEnabled(ctx, workspaceID, row.ID, enabled)
}

func (a *workspaceSkillStoreAdapter) Update(ctx context.Context, skill *skills.Skill) error {
	if skill == nil {
		return domain.ErrInvalid
	}
	// The store updates by row ID; preserve identity and timestamps the
	// pipeline's name-keyed entity does not carry.
	existing, err := a.store.GetByName(ctx, skill.WorkspaceID, skill.Name)
	if err != nil {
		return err
	}
	row := skillToDomain(skill)
	row.ID = existing.ID
	row.CreatedAt = existing.CreatedAt
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = existing.UpdatedAt
	}
	return a.store.Update(ctx, row)
}

func (a *workspaceSkillStoreAdapter) Delete(ctx context.Context, workspaceID, name string) error {
	row, err := a.store.GetByName(ctx, workspaceID, name)
	if err != nil {
		return err
	}
	return a.store.Delete(ctx, workspaceID, row.ID)
}

// workspaceSkillsTxProvider binds the install service's transaction seam to
// store.WithTx: the registry row write, the tool-gate write, and the bulk
// agent-allowlist update commit or roll back together (design D5/D8 — the
// documented exception to the one-sub-interface rule).
func workspaceSkillsTxProvider(st store.Store) skills.TxProvider {
	return func(ctx context.Context, fn func(ctx context.Context, tx skills.TxStores) error) error {
		return st.WithTx(ctx, func(tx store.Store) error {
			return fn(ctx, skills.TxStores{
				Skills:       newWorkspaceSkillStoreAdapter(tx.WorkspaceSkills()),
				Agents:       tx.Agents(),
				ToolSettings: tx.ToolSettings(),
			})
		})
	}
}

func skillToDomain(skill *skills.Skill) *domain.WorkspaceSkill {
	return &domain.WorkspaceSkill{
		WorkspaceID:  skill.WorkspaceID,
		Name:         skill.Name,
		Description:  skill.Description,
		Version:      skill.Version,
		Source:       domain.SkillSource(skill.Source),
		Enabled:      skill.Enabled,
		Dependencies: depSetToDomain(skill.Dependencies),
		CreatedAt:    skill.InstalledAt,
		UpdatedAt:    skill.UpdatedAt,
	}
}

func skillFromDomain(row *domain.WorkspaceSkill) *skills.Skill {
	return &skills.Skill{
		WorkspaceID:  row.WorkspaceID,
		Name:         row.Name,
		Description:  row.Description,
		Version:      row.Version,
		Source:       skills.Source(row.Source),
		Enabled:      row.Enabled,
		Dependencies: depSetFromDomain(row.Dependencies),
		InstalledAt:  row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

// depSetToDomain flattens the pipeline's resolved statuses to the dependency
// names the registry row persists (design D5: the resolved set is stored so
// reports re-render without re-parsing).
func depSetToDomain(set skills.DependencySet) domain.SkillDependencies {
	deps := domain.SkillDependencies{
		Tools:    make([]string, 0, len(set.Tools)),
		Binaries: make([]string, 0, len(set.Binaries)),
		Python:   make([]string, 0, len(set.Python)),
	}
	for _, dep := range set.Tools {
		deps.Tools = append(deps.Tools, dep.Name)
	}
	for _, dep := range set.Binaries {
		deps.Binaries = append(deps.Binaries, dep.Name)
	}
	for _, dep := range set.Python {
		deps.Python = append(deps.Python, dep.Requirement)
	}
	return deps
}

// depSetFromDomain rehydrates stored dependency names. Statuses are not
// persisted per-entry on the row; live statuses come from the install and
// re-check pipeline responses, so entries carry no status here.
func depSetFromDomain(deps domain.SkillDependencies) skills.DependencySet {
	set := skills.DependencySet{
		Tools:    make([]skills.ToolDependency, 0, len(deps.Tools)),
		Binaries: make([]skills.BinaryDependency, 0, len(deps.Binaries)),
		Python:   make([]skills.PythonDependency, 0, len(deps.Python)),
	}
	for _, name := range deps.Tools {
		set.Tools = append(set.Tools, skills.ToolDependency{Name: name})
	}
	for _, name := range deps.Binaries {
		set.Binaries = append(set.Binaries, skills.BinaryDependency{Name: name})
	}
	for _, req := range deps.Python {
		set.Python = append(set.Python, skills.PythonDependency{Requirement: req})
	}
	return set
}
