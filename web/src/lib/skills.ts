import { useAuthStore } from '../store/auth';
import { useStore } from '../store';
import type { ApiWorkspaceSkill, ApiSkillDependencyStatus } from './api';

// skills.write holders: Owner/Admin/Superadmin via the built-in catalog grants.
// Members hold skills.read only and get the read-only inventory everywhere.
export function canWriteSkills(memberships: any[], tenant: any): boolean {
  if (!memberships.length) return true; // offline / mock mode — affordances stay visible
  const tenantId = tenant?.sub || tenant?.id;
  const mem = memberships.find(
    (m) =>
      m.workspace_id === tenantId ||
      m.workspace_slug === tenantId ||
      m.workspace_id === tenant?.id ||
      m.workspace_slug === tenant?.sub
  );
  if (!mem) return false;
  const role = mem.role;
  const roleName = (mem.role_name || role?.name || '').toLowerCase();
  const perms: string[] = role?.permissions || [];
  return (
    role?.is_owner === true ||
    roleName === 'superadmin' ||
    roleName === 'owner' ||
    roleName === 'admin' ||
    perms.some((p) => p === '*' || p === 'skills.write' || p === 'skills.*' || p === 'workspace.*')
  );
}

export function useCanWriteSkills(tenant: any): boolean {
  const memberships = useAuthStore((s) => s.memberships);
  const pos = useStore((s: any) => s.pos);
  if (!tenant) return canWriteSkills(memberships, { id: pos.tenantId, sub: pos.tenantId });
  return canWriteSkills(memberships, tenant);
}

// Unmet dependencies drive the persistent warning chip on rows and locked chips.
export function unmetDependencies(skill: ApiWorkspaceSkill): ApiSkillDependencyStatus[] {
  return (skill.dependency_status || []).filter((d) => d.status !== 'met');
}

export function unmetToolDependencies(skill: ApiWorkspaceSkill): string[] {
  return unmetDependencies(skill)
    .filter((d) => d.kind === 'tools')
    .map((d) => d.name);
}

// Skills attached to every agent in the workspace: system tier (locked,
// always-on) and enabled workspace tier. Disabled workspace skills are absent.
export function attachedSkills(skills: ApiWorkspaceSkill[]): ApiWorkspaceSkill[] {
  return skills.filter((s) => s.tier === 'system' || (s.tier === 'workspace' && s.enabled !== false));
}

export const SOURCE_LABELS: Record<string, string> = {
  authored: 'authored',
  upload: 'upload',
  git: 'git',
  fork: 'fork',
  system: 'system',
};
