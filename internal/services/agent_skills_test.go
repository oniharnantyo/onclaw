package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// workspaceSkillsDir creates and returns the slug's workspace skills
// directory (SkillsDir creates it on demand).
func (f *agentsFixture) workspaceSkillsDir(t *testing.T, slug string) string {
	t.Helper()
	dir, err := f.ws.SkillsDir(slug)
	require.NoError(t, err)
	return dir
}

// writeSkill creates a skill directory holding a SKILL.md under dir and
// returns the skill directory path.
func writeSkill(t *testing.T, dir, name, skillMD string) string {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(skillDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0600))
	return skillDir
}

// dirNames returns the entry names of dir in os.ReadDir (sorted) order, as a
// before/after snapshot of a tier directory.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// TestAgentsService_ListWorkspaceSkills pins the listing contract (spec
// agent-management "Personal skills are listed"): one entry per workspace
// subdirectory carrying a SKILL.md, sorted by name, description parsed from
// the SKILL.md; non-skill entries are skipped and the global library tier is
// never read.
func TestAgentsService_ListWorkspaceSkills(t *testing.T) {
	f := newAgentsFixture(t)
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)
	wsSkills := f.workspaceSkillsDir(t, "scout")
	writeSkill(t, wsSkills, "deploy-runbook", "---\nname: deploy-runbook\ndescription: Run the deploy pipeline safely.\n---\n\n# Deploy runbook\n")
	writeSkill(t, wsSkills, "zeta-tool", "---\ndescription: Zeta does zeta things.\n---\n")
	// Entries that are not skills: a bare directory and a plain file.
	require.NoError(t, os.MkdirAll(filepath.Join(wsSkills, "not-a-skill"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(wsSkills, "notes.txt"), []byte("x"), 0600))
	// A library-tier skill must not leak into the agent's own listing.
	writeSkill(t, f.skills, "library-only", "description: lives in the library")

	skills, err := f.svc.ListWorkspaceSkills(context.Background(), agent.ID)
	require.NoError(t, err)
	require.Len(t, skills, 2)
	require.Equal(t, "deploy-runbook", skills[0].Name)
	require.Equal(t, "Run the deploy pipeline safely.", skills[0].Description)
	require.Equal(t, "zeta-tool", skills[1].Name)
	require.Equal(t, "Zeta does zeta things.", skills[1].Description)
}

func TestAgentsService_ListWorkspaceSkills_NoSkillsYet(t *testing.T) {
	f := newAgentsFixture(t)
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)

	skills, err := f.svc.ListWorkspaceSkills(context.Background(), agent.ID)
	require.NoError(t, err)
	require.NotNil(t, skills)
	require.Empty(t, skills)
}

func TestAgentsService_PromoteWorkspaceSkill_CopiesAndRegisters(t *testing.T) {
	f := newAgentsFixture(t)
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)
	wsSkills := f.workspaceSkillsDir(t, "scout")
	const skillMD = "---\nname: deploy-runbook\ndescription: Run the deploy pipeline safely.\n---\n\n# Deploy runbook\n"
	skillDir := writeSkill(t, wsSkills, "deploy-runbook", skillMD)
	const scriptBody = "#!/bin/sh\necho deploying\n"
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "scripts"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte(scriptBody), 0700))

	require.NoError(t, f.svc.PromoteWorkspaceSkill(context.Background(), agent.ID, "deploy-runbook"))

	// The bytes landed in the library, subdirectories included.
	promoted, err := os.ReadFile(filepath.Join(f.skills, "deploy-runbook", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, skillMD, string(promoted))
	script, err := os.ReadFile(filepath.Join(f.skills, "deploy-runbook", "scripts", "run.sh"))
	require.NoError(t, err)
	require.Equal(t, scriptBody, string(script))

	// The registry row is created enabled with the parsed description.
	row, err := f.st.Skills().GetByName(context.Background(), "deploy-runbook")
	require.NoError(t, err)
	require.Equal(t, "deploy-runbook", row.Name)
	require.Equal(t, "Run the deploy pipeline safely.", row.Description)
	require.True(t, row.Enabled)

	// The workspace copy stays (design D2: promote is a copy).
	wsSkill, err := os.ReadFile(filepath.Join(wsSkills, "deploy-runbook", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, skillMD, string(wsSkill))
}

func TestAgentsService_PromoteWorkspaceSkill_CollisionRejected(t *testing.T) {
	f := newAgentsFixture(t)
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)
	wsSkills := f.workspaceSkillsDir(t, "scout")
	writeSkill(t, wsSkills, "deploy-runbook", "---\ndescription: agent version\n---\nagent body")

	ctx := context.Background()
	require.NoError(t, f.st.Skills().Upsert(ctx, &store.Skill{Name: "deploy-runbook", Description: "library description", Enabled: false}))
	existing, err := f.st.Skills().GetByName(ctx, "deploy-runbook")
	require.NoError(t, err)
	writeSkill(t, f.skills, "deploy-runbook", "library original bytes")

	err = f.svc.PromoteWorkspaceSkill(ctx, agent.ID, "deploy-runbook")
	require.Error(t, err)
	require.Contains(t, err.Error(), "deploy-runbook")
	require.Contains(t, err.Error(), "already exists")

	// The library skill is byte-identical and the registry row untouched.
	lib, err := os.ReadFile(filepath.Join(f.skills, "deploy-runbook", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "library original bytes", string(lib))
	row, err := f.st.Skills().GetByName(ctx, "deploy-runbook")
	require.NoError(t, err)
	require.Equal(t, existing, row)
}

func TestAgentsService_PromoteWorkspaceSkill_RejectsInvalidNames(t *testing.T) {
	f := newAgentsFixture(t)
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)

	for _, name := range []string{"../escape", "Foo", ""} {
		err := f.svc.PromoteWorkspaceSkill(context.Background(), agent.ID, name)
		require.Error(t, err, "skill name %q must be rejected", name)
		require.Contains(t, err.Error(), "invalid skill name")
	}

	entries, err := os.ReadDir(f.skills)
	require.NoError(t, err)
	require.Empty(t, entries, "nothing may reach the library")
	rows, err := f.st.Skills().List(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows, "nothing may reach the registry")
}

func TestAgentsService_PromoteWorkspaceSkill_MissingSkill(t *testing.T) {
	f := newAgentsFixture(t)
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)
	f.workspaceSkillsDir(t, "scout")

	err := f.svc.PromoteWorkspaceSkill(context.Background(), agent.ID, "ghost")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
	require.Contains(t, err.Error(), "not found")
}

func TestAgentsService_DeleteWorkspaceSkill(t *testing.T) {
	f := newAgentsFixture(t)
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)
	wsSkills := f.workspaceSkillsDir(t, "scout")
	writeSkill(t, wsSkills, "deploy-runbook", "---\ndescription: personal copy\n---\n")
	writeSkill(t, f.skills, "library-skill", "description: stays put")

	require.NoError(t, f.svc.DeleteWorkspaceSkill(context.Background(), agent.ID, "deploy-runbook"))

	// Only the workspace copy is gone; the library is untouched.
	requireDirAbsent(t, filepath.Join(wsSkills, "deploy-runbook"))
	kept, err := os.ReadFile(filepath.Join(f.skills, "library-skill", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "description: stays put", string(kept))
}

func TestAgentsService_DeleteWorkspaceSkill_UnknownSkill(t *testing.T) {
	f := newAgentsFixture(t)
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)
	f.workspaceSkillsDir(t, "scout")

	err := f.svc.DeleteWorkspaceSkill(context.Background(), agent.ID, "ghost")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
	require.Contains(t, err.Error(), "not found")
}

// TestAgentsService_DeleteWorkspaceSkill_RejectsInvalidNames pins the
// DNS-label gate on the delete path (same validation as promote): a rejected
// name must leave both the workspace and the library untouched.
func TestAgentsService_DeleteWorkspaceSkill_RejectsInvalidNames(t *testing.T) {
	f := newAgentsFixture(t)
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)
	wsSkills := f.workspaceSkillsDir(t, "scout")
	writeSkill(t, wsSkills, "deploy-runbook", "---\ndescription: personal copy\n---\n")
	writeSkill(t, f.skills, "library-skill", "description: stays put")
	wsBefore := dirNames(t, wsSkills)
	libBefore := dirNames(t, f.skills)

	for _, name := range []string{"../escape", "Foo", ""} {
		err := f.svc.DeleteWorkspaceSkill(context.Background(), agent.ID, name)
		require.Error(t, err, "skill name %q must be rejected", name)
		require.Contains(t, err.Error(), "invalid skill name")
	}

	require.Equal(t, wsBefore, dirNames(t, wsSkills), "the workspace must be untouched")
	require.Equal(t, libBefore, dirNames(t, f.skills), "the library must be untouched")
}

func TestAgentsService_WorkspaceSkills_UnknownAgent(t *testing.T) {
	f := newAgentsFixture(t)

	_, err := f.svc.ListWorkspaceSkills(context.Background(), "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
	err = f.svc.PromoteWorkspaceSkill(context.Background(), "missing", "deploy-runbook")
	require.ErrorIs(t, err, store.ErrNotFound)
	err = f.svc.DeleteWorkspaceSkill(context.Background(), "missing", "deploy-runbook")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestAgentsService_WorkspaceSkills_CapabilityOff pins the capability-off
// posture: without a skills directory the promote and delete bindings reject
// while the workspace-only listing still works.
func TestAgentsService_WorkspaceSkills_CapabilityOff(t *testing.T) {
	f := newAgentsFixture(t)
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)
	svc := NewAgentsService(f.st, f.gen, f.ws, f.know, "")

	err := svc.PromoteWorkspaceSkill(context.Background(), agent.ID, "deploy-runbook")
	require.ErrorContains(t, err, "skills capability is not configured")
	err = svc.DeleteWorkspaceSkill(context.Background(), agent.ID, "deploy-runbook")
	require.ErrorContains(t, err, "skills capability is not configured")

	skills, err := svc.ListWorkspaceSkills(context.Background(), agent.ID)
	require.NoError(t, err)
	require.NotNil(t, skills)
	require.Empty(t, skills)
}
