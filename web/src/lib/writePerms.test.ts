import { describe, it, expect } from 'vitest';
import {
  canWriteMembers,
  canRemoveMembers,
  canWriteProviders,
  canWriteWorkspace,
  canWriteHooks,
  canWriteSchedulers,
} from './writePerms';

// The decided built-in Member set (fix-role-permission-audit): its read
// permissions plus the new channels.read / channels.write — roles.write is
// removed from the catalog entirely.
const MEMBER_PERMS = [
  'workspace.read',
  'members.read',
  'roles.read',
  'providers.read',
  'agents.read',
  'skills.read',
  'scheduler.read',
  'channels.read',
  'channels.write',
];

const membership = (roleName: string, permissions: string[], extra: Record<string, unknown> = {}) => ({
  workspace_id: 'ws_acme',
  workspace_slug: 'acme',
  role_name: roleName,
  role: { name: roleName, permissions, is_owner: roleName === 'Owner', ...extra },
});

const acme = { id: 'ws_acme', sub: 'acme' };

describe('lib/writePerms', () => {
  it('keeps mock mode (no memberships) permissive like the rest of the family', () => {
    expect(canWriteMembers([], acme)).toBe(true);
    expect(canRemoveMembers([], acme)).toBe(true);
    expect(canWriteProviders([], acme)).toBe(true);
    expect(canWriteWorkspace([], acme)).toBe(true);
    expect(canWriteHooks([], acme)).toBe(true);
    expect(canWriteSchedulers([], acme)).toBe(true);
  });

  it('denies every checked permission for the built-in Member set', () => {
    const mem = [membership('Member', MEMBER_PERMS)];
    expect(canWriteMembers(mem, acme)).toBe(false);
    expect(canRemoveMembers(mem, acme)).toBe(false);
    expect(canWriteProviders(mem, acme)).toBe(false);
    expect(canWriteWorkspace(mem, acme)).toBe(false);
    expect(canWriteHooks(mem, acme)).toBe(false);
    expect(canWriteSchedulers(mem, acme)).toBe(false);
  });

  it('denies a membership whose role carries only the matching read permission', () => {
    const mem = [membership('Member', ['hooks.read', 'scheduler.read'])];
    expect(canWriteHooks(mem, acme)).toBe(false);
    expect(canWriteSchedulers(mem, acme)).toBe(false);
  });

  it('grants a role holding the exact permission strings', () => {
    const mem = [
      membership('Custom', ['members.write', 'members.remove', 'providers.write', 'workspace.write', 'hooks.write', 'scheduler.write']),
    ];
    expect(canWriteMembers(mem, acme)).toBe(true);
    expect(canRemoveMembers(mem, acme)).toBe(true);
    expect(canWriteProviders(mem, acme)).toBe(true);
    expect(canWriteWorkspace(mem, acme)).toBe(true);
    expect(canWriteHooks(mem, acme)).toBe(true);
    expect(canWriteSchedulers(mem, acme)).toBe(true);
  });

  it('grants the family wildcards and the catch-all', () => {
    const mem = [membership('Custom', ['hooks.*'])];
    expect(canWriteHooks(mem, acme)).toBe(true);
    expect(canWriteSchedulers(mem, acme)).toBe(false);
    expect(canWriteSchedulers([membership('Custom', ['*'])], acme)).toBe(true);
    expect(canWriteWorkspace([membership('Custom', ['workspace.*'])], acme)).toBe(true);
  });

  it('grants owners, superadmins, and admins by role identity regardless of listed perms', () => {
    expect(canWriteProviders([membership('Owner', [], { is_owner: true })], acme)).toBe(true);
    expect(canWriteHooks([membership('Superadmin', [])], acme)).toBe(true);
    expect(canWriteSchedulers([membership('Admin', [])], acme)).toBe(true);
  });

  it('matches the workspace by slug or id and denies non-members', () => {
    const bySlug = [membership('Member', MEMBER_PERMS)];
    expect(canWriteWorkspace(bySlug, { id: 'other', sub: 'acme' })).toBe(false);
    const foreign = [{ ...membership('Admin', []), workspace_slug: 'globex', workspace_id: 'ws_globex' }];
    expect(canWriteWorkspace(foreign, acme)).toBe(false);
  });
});
