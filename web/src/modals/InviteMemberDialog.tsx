import { useState } from "react";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { api, formatApiError, type ApiRole } from "../lib/api";

export interface InviteMemberDialogProps {
  workspaceId: string;
  roles: ApiRole[];
  onClose: () => void;
  onSuccess: () => void;
  onToast: (text: string, kind?: string) => void;
}

export function InviteMemberDialog({
  workspaceId,
  roles,
  onClose,
  onSuccess,
  onToast,
}: InviteMemberDialogProps) {
  const defaultRoleId =
    roles.find((r) => r.name.toLowerCase() === 'member')?.id || roles[0]?.id || '';
  const [email, setEmail] = useState('');
  const [roleId, setRoleId] = useState(defaultRoleId);
  const [submitting, setSubmitting] = useState(false);

  const isValid = email.includes('@') && email.trim().length > 0;

  const handleSubmit = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!isValid || submitting) return;

    setSubmitting(true);
    try {
      await api.members.add(workspaceId, {
        email: email.trim(),
        role_id: roleId || roles[0]?.id || '',
      });
      onToast('Invite sent to ' + email.trim());
      onSuccess();
      onClose();
    } catch (err: unknown) {
      onToast(formatApiError(err, 'Failed to invite member'), 'danger');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title="Invite member"
      onClose={onClose}
      odId="modal-invite-member"
      data-testid="modal-invite-member"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-od-id="btn-invite-cancel"
            data-testid="btn-invite-cancel"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="submit"
            form="invite-member-form"
            data-od-id="btn-invite"
            data-testid="btn-invite"
            disabled={!isValid || submitting}
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent"
          >
            {submitting ? 'Inviting…' : 'Invite'}
          </button>
        </>
      }
    >
      <form id="invite-member-form" onSubmit={handleSubmit} className="space-y-4 p-5">
        <div>
          <label className={labelCls} htmlFor="invite-email">
            Email address
          </label>
          <input
            id="invite-email"
            type="email"
            className={inputCls}
            placeholder="teammate@example.com"
            value={email}
            aria-label="Invite email"
            data-od-id="input-invite-email"
            data-testid="input-invite-email"
            onChange={(e) => setEmail(e.target.value)}
            autoFocus
          />
        </div>

        <div>
          <label className={labelCls} htmlFor="invite-role">
            Role
          </label>
          <select
            id="invite-role"
            className={inputCls}
            value={roleId}
            onChange={(e) => setRoleId(e.target.value)}
            aria-label="Role for invite"
            data-od-id="select-invite-role"
            data-testid="select-invite-role"
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
        </div>
      </form>
    </Modal>
  );
}
