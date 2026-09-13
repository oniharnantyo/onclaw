import { useState, useEffect, useMemo, useCallback } from 'react';
import { Modal } from '../../components/ui/Modal';
import { TimezoneSelect } from '../../components/ui/TimezoneSelect';
import { UserPicker } from '../../components/ui/UserPicker';
import { Avatar } from '../../components/ui/Avatar';
import { Icon } from '../../components/ui/Icon';
import { inputCls, labelCls } from '../../components/ui/constants';
import { cx } from '../../lib/helpers';
import {
  api,
  formatApiError,
  type ApiAdminWorkspaceItem,
  type ApiMemberItem,
  type ApiUser,
} from '../../lib/api';

export interface EditTenantModalProps {
  workspace: ApiAdminWorkspaceItem;
  onClose: () => void;
  onUpdateSuccess?: (updated: ApiAdminWorkspaceItem) => void;
  onToast: (text: string, kind?: string) => void;
}

export function EditTenantModal({
  workspace,
  onClose,
  onUpdateSuccess,
  onToast,
}: EditTenantModalProps) {
  const isMaster = workspace.is_master || workspace.slug === 'master';

  // Section 1: General settings
  const [name, setName] = useState(workspace.name || '');
  const [timezone, setTimezone] = useState(workspace.timezone || 'UTC');
  const [savingGeneral, setSavingGeneral] = useState(false);
  const [generalError, setGeneralError] = useState<string | null>(null);

  // Section 2 & 3: Members & Owners
  const [members, setMembers] = useState<ApiMemberItem[]>([]);
  const [loadingMembers, setLoadingMembers] = useState(true);
  const [membersError, setMembersError] = useState<string | null>(null);

  // Transfer owner state
  const [transferUserId, setTransferUserId] = useState('');
  const [transferUser, setTransferUser] = useState<ApiUser | null>(null);
  const [confirmTransfer, setConfirmTransfer] = useState(false);
  const [transferring, setTransferring] = useState(false);

  // Add member state
  const [addMemberUserId, setAddMemberUserId] = useState('');
  const [addMemberRole, setAddMemberRole] = useState<'Admin' | 'Member'>('Member');
  const [addingMember, setAddingMember] = useState(false);

  const fetchMembers = useCallback(async () => {
    setLoadingMembers(true);
    setMembersError(null);
    try {
      const res = await api.admin.workspaces.listMembers(workspace.slug || workspace.id);
      setMembers(res.members || []);
    } catch (err: unknown) {
      const msg = formatApiError(err, 'Failed to load workspace members');
      setMembersError(msg);
    } finally {
      setLoadingMembers(false);
    }
  }, [workspace.id, workspace.slug]);

  useEffect(() => {
    fetchMembers();
  }, [fetchMembers]);

  const currentOwners = useMemo(() => {
    return members.filter(
      (m) =>
        m.role?.is_owner ||
        m.role_name?.toLowerCase() === 'owner' ||
        m.role?.name?.toLowerCase() === 'owner'
    );
  }, [members]);

  const existingMemberUserIds = useMemo(() => {
    return members.map((m) => m.user_id).filter(Boolean);
  }, [members]);

  const currentOwnerUserIds = useMemo(() => {
    return currentOwners.map((m) => m.user_id).filter(Boolean);
  }, [currentOwners]);

  // Handle save general settings (rename + timezone)
  const handleSaveGeneral = async (e: React.FormEvent) => {
    e.preventDefault();

    const trimmedName = name.trim();
    if (!trimmedName) {
      setGeneralError('Workspace name cannot be empty');
      return;
    }

    setSavingGeneral(true);
    setGeneralError(null);

    try {
      const res = await api.admin.workspaces.patch(workspace.slug || workspace.id, {
        name: trimmedName,
        timezone,
      });

      const updatedWs: ApiAdminWorkspaceItem = {
        ...workspace,
        name: res.workspace.name,
        timezone: res.workspace.timezone,
      };

      onToast(`Updated tenant "${res.workspace.name}"`);
      if (onUpdateSuccess) {
        onUpdateSuccess(updatedWs);
      }
    } catch (err: unknown) {
      const msg = formatApiError(err, 'Failed to update workspace');
      setGeneralError(msg);
      onToast(msg, 'danger');
    } finally {
      setSavingGeneral(false);
    }
  };

  // Handle owner transfer
  const handleTransferOwner = async () => {
    if (isMaster || !transferUserId) return;

    setTransferring(true);
    try {
      const res = await api.admin.workspaces.transferOwner(workspace.slug || workspace.id, {
        user_id: transferUserId,
      });

      const newOwnerName = res.owner?.name || res.owner?.email || transferUser?.name || transferUser?.email || 'new owner';
      const demotedCount = currentOwners.filter((o) => o.user_id !== transferUserId).length;

      let msg = `Transferred ownership of "${workspace.name}" to ${newOwnerName}`;
      if (demotedCount > 0) {
        msg += ` (${demotedCount} previous owner${demotedCount > 1 ? 's' : ''} demoted to Admin)`;
      }
      onToast(msg);

      setTransferUserId('');
      setTransferUser(null);
      setConfirmTransfer(false);

      await fetchMembers();
    } catch (err: unknown) {
      const msg = formatApiError(err, 'Failed to transfer ownership');
      onToast(msg, 'danger');
    } finally {
      setTransferring(false);
    }
  };

  // Handle add member
  const handleAddMember = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!addMemberUserId) return;

    setAddingMember(true);
    try {
      const res = await api.admin.workspaces.addMember(workspace.slug || workspace.id, {
        user_id: addMemberUserId,
        role_name: addMemberRole,
      });

      const addedUserName = res.user?.name || res.user?.email || 'User';
      onToast(`Added "${addedUserName}" as ${res.role?.name || addMemberRole}`);

      setAddMemberUserId('');
      await fetchMembers();

      if (onUpdateSuccess) {
        onUpdateSuccess({
          ...workspace,
          member_count: (workspace.member_count || members.length) + 1,
        });
      }
    } catch (err: unknown) {
      const msg = formatApiError(err, 'Failed to add member');
      onToast(msg, 'danger');
    } finally {
      setAddingMember(false);
    }
  };

  const hasGeneralChanges =
    name.trim() !== workspace.name || timezone !== workspace.timezone;

  return (
    <Modal
      title={`Edit tenant: ${workspace.name}`}
      onClose={onClose}
      odId="modal-edit-tenant"
      footer={
        <div className="flex w-full items-center justify-end">
          <button
            type="button"
            onClick={onClose}
            data-od-id="btn-admin-close-edit-tenant"
            data-testid="btn-admin-close-edit-tenant"
            className="flex h-9 items-center rounded-md border border-line px-4 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Done
          </button>
        </div>
      }
    >
      <div className="space-y-6 p-5">
        {/* Section 1: General Settings */}
        <section className="space-y-4 rounded-lg border border-line bg-surface p-4">
          <div className="flex items-center justify-between border-b border-linesoft pb-2.5">
            <div>
              <h3 className="text-[14px] font-semibold text-fg">General settings</h3>
              <p className="text-[12px] text-muted">
                Update workspace display name and primary timezone.
              </p>
            </div>
            {isMaster && (
              <span className="rounded bg-[color-mix(in_oklab,var(--accent)_16%,transparent)] px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-accenttext">
                Master
              </span>
            )}
          </div>

          {generalError && (
            <div className="rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] p-2.5 text-[12px] text-danger">
              {generalError}
            </div>
          )}

          <form onSubmit={handleSaveGeneral} className="space-y-3">
            <div>
              <label className={labelCls} htmlFor="edit-ws-name">
                Workspace name
              </label>
              <input
                id="edit-ws-name"
                className={inputCls}
                data-od-id="input-edit-ws-name"
                data-testid="input-edit-ws-name"
                value={name}
                disabled={savingGeneral}
                onChange={(e) => {
                  setName(e.target.value);
                  setGeneralError(null);
                }}
                placeholder="Workspace name"
              />
            </div>

            <div>
              <label className={labelCls} htmlFor="edit-ws-slug">
                Slug (immutable)
              </label>
              <input
                id="edit-ws-slug"
                className={cx(inputCls, 'opacity-60 bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] cursor-not-allowed')}
                data-od-id="input-edit-ws-slug"
                data-testid="input-edit-ws-slug"
                value={workspace.slug}
                disabled
              />
            </div>

            <div>
              <label className={labelCls} htmlFor="edit-ws-tz">
                Timezone
              </label>
              <TimezoneSelect
                id="edit-ws-tz"
                data-od-id="select-edit-ws-tz"
                data-testid="select-edit-ws-tz"
                value={timezone}
                disabled={savingGeneral}
                onChange={(tz) => {
                  setTimezone(tz);
                  setGeneralError(null);
                }}
              />
            </div>

            <div className="flex justify-end pt-1">
              <button
                type="submit"
                disabled={savingGeneral || !hasGeneralChanges || !name.trim()}
                data-od-id="btn-save-tenant"
                data-testid="btn-save-tenant"
                className="flex h-8 items-center rounded-md bg-accent px-3 text-[12.5px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-50"
              >
                {savingGeneral ? 'Saving…' : 'Save changes'}
              </button>
            </div>
          </form>
        </section>

        {/* Section 2: Owner Transfer */}
        {!isMaster && (
          <section className="space-y-4 rounded-lg border border-line bg-surface p-4">
            <div className="border-b border-linesoft pb-2.5">
              <h3 className="text-[14px] font-semibold text-fg">Ownership transfer</h3>
              <p className="text-[12px] text-muted">
                Transfer workspace ownership to another user. Current owners will be demoted to Admin role.
              </p>
            </div>

            {/* Current Owner(s) Display */}
            <div>
              <span className="block text-[11px] font-semibold uppercase tracking-wider text-muted mb-2">
                Current Owner{currentOwners.length > 1 ? 's' : ''}
              </span>
              {loadingMembers ? (
                <div className="flex items-center gap-2 text-[12px] text-muted py-1">
                  <div className="h-4 w-4 animate-spin rounded-full border-2 border-line border-t-accent" />
                  Loading owners…
                </div>
              ) : currentOwners.length === 0 ? (
                <p className="text-[12px] text-muted py-1">No owners currently assigned.</p>
              ) : (
                <div className="space-y-1.5" data-testid="current-owners-list">
                  {currentOwners.map((owner) => (
                    <div
                      key={owner.user_id}
                      className="flex items-center justify-between rounded-md border border-linesoft bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] px-3 py-2"
                    >
                      <div className="flex items-center gap-2.5 min-w-0">
                        <Avatar
                          name={owner.name || owner.email}
                          src={owner.avatar_url}
                          size={22}
                          kind="other"
                        />
                        <div className="min-w-0 truncate">
                          <span className="text-[13px] font-medium text-fg">{owner.name || owner.email}</span>
                          {owner.name && owner.email && (
                            <span className="ml-1.5 text-[11px] text-muted truncate">({owner.email})</span>
                          )}
                        </div>
                      </div>
                      <span className="rounded bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] px-2 py-0.5 font-mono text-[10px] font-semibold text-accenttext">
                        Owner
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Transfer Controls */}
            <div className="space-y-2 pt-1">
              <label className={labelCls} htmlFor="select-transfer-owner">
                Select new owner
              </label>
              <UserPicker
                id="select-transfer-owner"
                data-od-id="select-transfer-owner"
                data-testid="select-transfer-owner"
                value={transferUserId}
                onChange={(val, user) => {
                  setTransferUserId(val);
                  setTransferUser(user || null);
                  setConfirmTransfer(false);
                }}
                disabledUserIds={currentOwnerUserIds}
                placeholder="Select user to become owner…"
              />

              {confirmTransfer ? (
                <div className="rounded-md border border-[color-mix(in_oklab,var(--accent)_30%,transparent)] bg-[color-mix(in_oklab,var(--accent)_8%,transparent)] p-3 space-y-2.5">
                  <div className="flex items-start gap-2">
                    <Icon name="alert-triangle" size={16} className="mt-0.5 shrink-0 text-accent" />
                    <div className="text-[12px] text-fg">
                      <p className="font-semibold">Confirm ownership transfer</p>
                      <p className="mt-0.5 text-fg2">
                        Transfer ownership to <span className="font-semibold text-fg">{transferUser?.name || transferUser?.email || transferUserId}</span>? All other current owners will be demoted to Admin.
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center justify-end gap-2 pt-1">
                    <button
                      type="button"
                      onClick={() => setConfirmTransfer(false)}
                      disabled={transferring}
                      data-od-id="btn-cancel-transfer-owner"
                      data-testid="btn-cancel-transfer-owner"
                      className="flex h-7 items-center rounded border border-line px-2.5 text-[11.5px] font-medium text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]"
                    >
                      Cancel
                    </button>
                    <button
                      type="button"
                      onClick={handleTransferOwner}
                      disabled={transferring}
                      data-od-id="btn-confirm-transfer-owner"
                      data-testid="btn-confirm-transfer-owner"
                      className="flex h-7 items-center rounded bg-accent px-3 text-[11.5px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] disabled:opacity-50"
                    >
                      {transferring ? 'Transferring…' : 'Confirm transfer'}
                    </button>
                  </div>
                </div>
              ) : (
                <div className="flex justify-end pt-1">
                  <button
                    type="button"
                    onClick={() => setConfirmTransfer(true)}
                    disabled={!transferUserId || transferring}
                    data-od-id="btn-transfer-owner"
                    data-testid="btn-transfer-owner"
                    className="flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12.5px] font-medium text-fg2 transition-colors hover:border-muted hover:text-fg disabled:opacity-50"
                  >
                    Transfer ownership
                  </button>
                </div>
              )}
            </div>
          </section>
        )}

        {/* Section 3: Members List & Add Member */}
        <section className="space-y-4 rounded-lg border border-line bg-surface p-4">
          <div className="flex items-center justify-between border-b border-linesoft pb-2.5">
            <div>
              <h3 className="text-[14px] font-semibold text-fg">Tenant members</h3>
              <p className="text-[12px] text-muted">
                Manage users belonging to this workspace.
              </p>
            </div>
            <span className="rounded bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] px-2 py-0.5 font-mono text-[11px] font-medium text-muted">
              {members.length} {members.length === 1 ? 'member' : 'members'}
            </span>
          </div>

          {/* Add Member Subsection */}
          <form onSubmit={handleAddMember} className="space-y-2 rounded-md border border-linesoft bg-[color-mix(in_oklab,var(--fg)_2%,transparent)] p-3">
            <span className="block text-[11px] font-semibold uppercase tracking-wider text-muted">
              Add member
            </span>
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
              <div className="flex-1">
                <UserPicker
                  id="select-add-member-user"
                  data-od-id="select-add-member-user"
                  data-testid="select-add-member-user"
                  value={addMemberUserId}
                  onChange={(val) => setAddMemberUserId(val)}
                  excludeUserIds={existingMemberUserIds}
                  placeholder="Select user to add…"
                />
              </div>
              <div className="w-32 shrink-0">
                <select
                  id="select-add-member-role"
                  data-od-id="select-add-member-role"
                  data-testid="select-add-member-role"
                  className={inputCls}
                  value={addMemberRole}
                  onChange={(e) => setAddMemberRole(e.target.value as 'Admin' | 'Member')}
                >
                  <option value="Admin">Admin</option>
                  <option value="Member">Member</option>
                </select>
              </div>
              <button
                type="submit"
                disabled={addingMember || !addMemberUserId}
                data-od-id="btn-add-member"
                data-testid="btn-add-member"
                className="flex h-9 items-center justify-center gap-1.5 rounded-md bg-accent px-3.5 text-[12.5px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] disabled:opacity-50 shrink-0"
              >
                <Icon name="plus" size={13} sw={2.2} />
                {addingMember ? 'Adding…' : 'Add member'}
              </button>
            </div>
          </form>

          {/* Members Table */}
          <div>
            {loadingMembers ? (
              <div className="flex h-28 items-center justify-center text-[12px] text-muted">
                <div className="flex items-center gap-2">
                  <div className="h-4 w-4 animate-spin rounded-full border-2 border-line border-t-accent" />
                  Loading members…
                </div>
              </div>
            ) : membersError ? (
              <div className="p-4 text-center text-[12px] text-danger">
                {membersError}
              </div>
            ) : members.length === 0 ? (
              <p className="py-4 text-center text-[12px] text-muted">
                No members found in this workspace.
              </p>
            ) : (
              <div
                className="divide-y divide-linesoft overflow-hidden rounded-md border border-linesoft"
                data-od-id="tenant-members-list"
                data-testid="tenant-members-list"
              >
                {members.map((m) => {
                  const isOwner =
                    m.role?.is_owner ||
                    m.role_name?.toLowerCase() === 'owner' ||
                    m.role?.name?.toLowerCase() === 'owner';
                  const isAdminRole =
                    m.role_name?.toLowerCase() === 'admin' ||
                    m.role?.name?.toLowerCase() === 'admin';

                  return (
                    <div
                      key={m.user_id}
                      data-od-id={'tenant-member-row-' + m.user_id}
                      data-testid={'tenant-member-row-' + m.user_id}
                      className="flex items-center justify-between p-2.5 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_3%,transparent)]"
                    >
                      <div className="flex items-center gap-2.5 min-w-0">
                        <Avatar
                          name={m.name || m.email}
                          src={m.avatar_url}
                          size={24}
                          kind="other"
                        />
                        <div className="min-w-0">
                          <div className="truncate text-[13px] font-medium text-fg">
                            {m.name || m.email}
                          </div>
                          {m.name && m.email && (
                            <div className="truncate text-[11px] text-muted">
                              {m.email}
                            </div>
                          )}
                        </div>
                      </div>

                      <div className="flex items-center gap-2 shrink-0">
                        {isOwner ? (
                          <span className="rounded bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] px-2 py-0.5 font-mono text-[10px] font-semibold text-accenttext">
                            Owner
                          </span>
                        ) : isAdminRole ? (
                          <span className="rounded bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] px-2 py-0.5 font-mono text-[10px] font-semibold text-fg2">
                            Admin
                          </span>
                        ) : (
                          <span className="rounded bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] px-2 py-0.5 font-mono text-[10px] text-muted">
                            Member
                          </span>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </section>
      </div>
    </Modal>
  );
}
