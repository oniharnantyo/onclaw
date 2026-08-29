import { useState } from 'react';
import { Modal } from '../../components/ui/Modal';
import { inputCls, labelCls } from '../../components/ui/constants';
import { api, formatApiError, ApiError, type ApiUser } from '../../lib/api';

interface CreateUserModalProps {
  onClose: () => void;
  onCreateSuccess: (user: ApiUser) => void;
  onToast: (text: string, kind?: string) => void;
}

export function CreateUserModal({ onClose, onCreateSuccess, onToast }: CreateUserModalProps) {
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [generalError, setGeneralError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFieldErrors({});
    setGeneralError(null);

    const trimmedEmail = email.trim();
    const trimmedName = name.trim();
    const trimmedPassword = password.trim();

    const errors: Record<string, string> = {};
    if (!trimmedEmail) {
      errors.email = 'Email is required';
    } else if (!trimmedEmail.includes('@') || !trimmedEmail.includes('.')) {
      errors.email = 'Valid email is required';
    }

    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);
      return;
    }

    setSubmitting(true);
    try {
      const res = await api.admin.users.create({
        email: trimmedEmail,
        name: trimmedName || undefined,
        password: trimmedPassword || undefined,
      });

      onToast(`Created user "${res.user.email}"`);
      onCreateSuccess(res.user);
      onClose();
    } catch (err: unknown) {
      if (err instanceof ApiError) {
        if (err.details && err.details.length > 0) {
          const detailMap: Record<string, string> = {};
          err.details.forEach((d) => {
            if (d.field) {
              detailMap[d.field] = d.message;
            }
          });
          setFieldErrors(detailMap);
        }
        const msg = formatApiError(err, 'Failed to create user');
        setGeneralError(msg);
        onToast(msg, 'danger');
      } else {
        const msg = formatApiError(err, 'Failed to create user');
        setGeneralError(msg);
        onToast(msg, 'danger');
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title="Create user"
      onClose={onClose}
      odId="modal-create-user"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-od-id="btn-admin-cancel-user"
            data-testid="btn-admin-cancel-user"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="submit"
            form="create-user-form"
            disabled={submitting || !email.trim()}
            data-od-id="btn-admin-submit-user"
            data-testid="btn-admin-submit-user"
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-50"
          >
            {submitting ? 'Creating…' : 'Create user'}
          </button>
        </>
      }
    >
      <form id="create-user-form" onSubmit={handleSubmit} className="space-y-4 p-5">
        {generalError && (
          <div className="rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] p-3 text-[13px] text-danger">
            {generalError}
          </div>
        )}

        <div>
          <label className={labelCls} htmlFor="admin-user-email">
            Email address
          </label>
          <input
            id="admin-user-email"
            type="email"
            className={inputCls}
            data-od-id="input-admin-user-email"
            data-testid="input-admin-user-email"
            placeholder="alice@example.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoFocus
          />
          {fieldErrors.email && (
            <p className="mt-1 text-[12px] text-danger">{fieldErrors.email}</p>
          )}
        </div>

        <div>
          <label className={labelCls} htmlFor="admin-user-name">
            Full name <span className="text-muted font-normal">(optional)</span>
          </label>
          <input
            id="admin-user-name"
            className={inputCls}
            data-od-id="input-admin-user-name"
            data-testid="input-admin-user-name"
            placeholder="Alice Smith"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          {fieldErrors.name && (
            <p className="mt-1 text-[12px] text-danger">{fieldErrors.name}</p>
          )}
        </div>

        <div>
          <label className={labelCls} htmlFor="admin-user-password">
            Password <span className="text-muted font-normal">(optional)</span>
          </label>
          <input
            id="admin-user-password"
            type="password"
            className={inputCls}
            data-od-id="input-admin-user-password"
            data-testid="input-admin-user-password"
            placeholder="••••••••••••"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          {fieldErrors.password ? (
            <p className="mt-1 text-[12px] text-danger">{fieldErrors.password}</p>
          ) : (
            <p className="mt-1 text-[11px] text-muted">
              Leave blank to create a passwordless account (the user can log in via invite or SSO).
            </p>
          )}
        </div>
      </form>
    </Modal>
  );
}
