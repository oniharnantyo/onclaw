import { useState, useEffect } from "react";
import { Icon } from "../../components/ui/Icon";
import { Avatar } from "../../components/ui/Avatar";
import { Chip } from "../../components/ui/Chip";
import { ErrorState } from "../../components/ErrorState";
import { api, formatApiError, ApiError, type ApiMemberItem, type ApiRole } from "../../lib/api";
import { useAuthStore } from "../../store/auth";
import { useCanWriteMembers, useCanRemoveMembers } from "../../lib/writePerms";
import { InviteMemberDialog } from "../../modals/InviteMemberDialog";
import serverErrorSvg from "../../assets/server-error.svg";

export interface MembersSectionProps {
  tenant: any;
  onToast: (text: string, kind?: string) => void;
  onUpdate?: (fn: any) => void;
}

export function MembersSection({ tenant, onToast, onUpdate: _onUpdate }: MembersSectionProps) {
  const currentUser = useAuthStore((s) => s.user);
  // Mutation affordances gate per permission (web-app/settings): invite and
  // inline role changes ride members.write, removal rides members.remove.
  // Members see the full roster read-only.
  const canInvite = useCanWriteMembers(tenant);
  const canRemove = useCanRemoveMembers(tenant);
  const [members, setMembers] = useState<ApiMemberItem[]>([]);
  const [roles, setRoles] = useState<ApiRole[]>([]);
  const [loadingMembers, setLoadingMembers] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);
  const [inviteOpen, setInviteOpen] = useState(false);

  const targetWsId = tenant.sub || tenant.id;

  const loadMembersAndRoles = async () => {
    setLoadingMembers(true);
    setLoadError(null);
    try {
      const [membersRes, rolesRes] = await Promise.all([
        api.members.list(targetWsId),
        api.roles.list(targetWsId),
      ]);
      setMembers(membersRes.members || []);
      setRoles(rolesRes.roles || []);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 0) {
        // Status 0 keeps the loading/empty state (handled by ConnectionBanner)
        return;
      }
      const apiErr = err instanceof Error ? err : new Error(String(err));
      setLoadError(apiErr);
    } finally {
      setLoadingMembers(false);
    }
  };

  useEffect(() => {
    loadMembersAndRoles();
  }, [targetWsId]);

  const handleRoleChange = async (member: ApiMemberItem, newRoleId: string) => {
    const targetRole = roles.find((r) => r.id === newRoleId);
    const roleName = targetRole?.name || 'Member';
    try {
      await api.members.patch(targetWsId, member.user_id, {
        role_id: newRoleId,
      });
      setMembers((prev) =>
        prev.map((m) =>
          m.user_id === member.user_id
            ? { ...m, role_id: newRoleId, role_name: roleName, role: targetRole }
            : m
        )
      );
      onToast(member.name + ' is now ' + roleName.toLowerCase());
    } catch (err: unknown) {
      onToast(formatApiError(err, 'Failed to update member role'), 'danger');
      loadMembersAndRoles();
    }
  };

  const handleRemoveMember = async (member: ApiMemberItem) => {
    try {
      await api.members.remove(targetWsId, member.user_id);
      setMembers((prev) => prev.filter((m) => m.user_id !== member.user_id));
      onToast(member.name + ' removed from ' + tenant.name);
    } catch (err: unknown) {
      onToast(formatApiError(err, 'Failed to remove member'), 'danger');
    }
  };

  return (
    <div className="max-w-xl" data-od-id="pane-members" data-testid="pane-members">
      {canInvite && members.length > 0 && (
        <div className="mb-4 flex justify-end">
          <button
            type="button"
            data-od-id="btn-invite-open"
            data-testid="btn-invite-open"
            onClick={() => setInviteOpen(true)}
            className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
          >
            Invite member
          </button>
        </div>
      )}

      {loadingMembers ? (
        <div className="py-6 text-center text-[13px] text-muted">Loading members…</div>
      ) : loadError ? (
        <div className="py-6">
          <ErrorState
            variant="full"
            illustration={serverErrorSvg}
            title="Couldn't load members"
            description="A server error occurred while loading workspace members."
            status={loadError instanceof ApiError ? loadError.status : 500}
            detail={loadError.message}
            primaryAction={{
              label: 'Retry',
              onClick: loadMembersAndRoles,
            }}
          />
        </div>
      ) : (
        <ul className="divide-y divide-[var(--border-soft)]">
          {members.map((m: ApiMemberItem) => {
            const isMe =
              (currentUser && m.user_id === currentUser.id) ||
              (currentUser && m.email === currentUser.email) ||
              m.user_id === 'me';
            const isOwner = m.role_name === 'Owner' || m.role?.is_owner;
            const isInvited = Boolean(m.invited);

            return (
              <li
                key={m.user_id || m.email}
                className="flex items-center gap-3 py-3"
                data-od-id={'member-' + m.user_id}
                data-testid={'member-' + m.user_id}
              >
                <Avatar name={m.name} src={m.avatar_url} kind={isMe ? 'you' : 'other'} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <p className="truncate text-[14px] font-medium text-fg">
                      {m.name || m.email.split('@')[0]}
                    </p>
                    {isInvited && <Chip>Invited</Chip>}
                  </div>
                  <div className="flex items-center gap-2 font-mono text-[11px] text-muted">
                    <span className="truncate">{m.email}</span>
                    {m.joined_at && (
                      <>
                        <span>·</span>
                        <span className="font-sans shrink-0">
                          Joined {new Date(m.joined_at).toLocaleDateString()}
                        </span>
                      </>
                    )}
                  </div>
                </div>
                {isMe ? (
                  <Chip>{isOwner ? 'You are the owner' : 'You'}</Chip>
                ) : isOwner ? (
                  <Chip>Owner</Chip>
                ) : !canInvite ? (
                  <Chip>{m.role_name || 'Member'}</Chip>
                ) : (
                  <div className="flex items-center gap-2">
                    <select
                      className="h-8 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[12px] text-fg2 focus:border-accent"
                      value={m.role_id}
                      onChange={(e) => handleRoleChange(m, e.target.value)}
                      aria-label={'Role for ' + m.name}
                    >
                      {roles.length > 0 ? (
                        roles.map((r) => (
                          <option key={r.id} value={r.id}>
                            {r.name}
                          </option>
                        ))
                      ) : (
                        <>
                          <option value="Admin">Admin</option>
                          <option value="Member">Member</option>
                        </>
                      )}
                    </select>
                    {canRemove && (
                      <button
                        type="button"
                        aria-label={'Remove ' + m.name}
                        onClick={() => handleRemoveMember(m)}
                        className="flex h-7 w-7 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] hover:text-danger"
                      >
                        <Icon name="x" size={13} />
                      </button>
                    )}
                  </div>
                )}
              </li>
            );
          })}
          {members.length === 0 && (
            <li className="py-8 text-center" data-od-id="members-empty" data-testid="members-empty">
              <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
                <Icon name="users" size={20} />
              </div>
              <p className="text-[14px] font-medium text-fg">No members found</p>
              <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
                Invite team members to collaborate in this workspace.
              </p>
              {canInvite && (
                <button
                  type="button"
                  data-od-id="btn-member-empty-add"
                  data-testid="btn-member-empty-add"
                  onClick={() => setInviteOpen(true)}
                  className="mt-3 inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                >
                  <Icon name="plus" size={13} /> Invite a member
                </button>
              )}
            </li>
          )}
        </ul>
      )}

      {inviteOpen && (
        <InviteMemberDialog
          workspaceId={targetWsId}
          roles={roles}
          onClose={() => setInviteOpen(false)}
          onSuccess={loadMembersAndRoles}
          onToast={onToast}
        />
      )}
    </div>
  );
}
