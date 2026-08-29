import { useState } from 'react';
import { Modal } from '../../components/ui/Modal';
import { TimezoneSelect } from '../../components/ui/TimezoneSelect';
import { UserPicker } from '../../components/ui/UserPicker';
import { inputCls, labelCls } from '../../components/ui/constants';
import { api, formatApiError, ApiError, type ApiAdminWorkspaceItem } from '../../lib/api';

interface CreateTenantModalProps {
  onClose: () => void;
  onCreateSuccess: (workspace: ApiAdminWorkspaceItem) => void;
  onToast: (text: string, kind?: string) => void;
}

export function CreateTenantModal({ onClose, onCreateSuccess, onToast }: CreateTenantModalProps) {
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [slugEdited, setSlugEdited] = useState(false);
  const [timezone, setTimezone] = useState('America/Los_Angeles');
  const [ownerEmail, setOwnerEmail] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [generalError, setGeneralError] = useState<string | null>(null);

  const handleNameChange = (val: string) => {
    setName(val);
    if (!slugEdited) {
      const generated = val
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '');
      setSlug(generated);
    }
  };

  const handleSlugChange = (val: string) => {
    setSlugEdited(true);
    setSlug(val.toLowerCase().replace(/[^a-z0-9-]/g, ''));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFieldErrors({});
    setGeneralError(null);

    const trimmedName = name.trim();
    const trimmedSlug = slug.trim();
    const trimmedEmail = ownerEmail.trim();

    const errors: Record<string, string> = {};
    if (!trimmedName) errors.name = 'Workspace name is required';
    if (!trimmedSlug) errors.slug = 'Workspace slug is required';
    if (!trimmedEmail) {
      errors.ownerEmail = 'Owner is required';
    }

    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);
      return;
    }

    setSubmitting(true);
    try {
      const res = await api.admin.workspaces.create({
        name: trimmedName,
        slug: trimmedSlug,
        timezone,
        owner_email: trimmedEmail,
      });

      onToast(`Created tenant "${res.workspace.name}"`);
      onCreateSuccess(res.workspace);
      onClose();
    } catch (err: unknown) {
      if (err instanceof ApiError) {
        if (err.details && err.details.length > 0) {
          const detailMap: Record<string, string> = {};
          err.details.forEach((d) => {
            if (d.field) {
              const fieldName = d.field === 'owner_email' ? 'ownerEmail' : d.field;
              detailMap[fieldName] = d.message;
            }
          });
          setFieldErrors(detailMap);
        }
        const msg = formatApiError(err, 'Failed to create workspace');
        setGeneralError(msg);
        onToast(msg, 'danger');
      } else {
        const msg = formatApiError(err, 'Failed to create workspace');
        setGeneralError(msg);
        onToast(msg, 'danger');
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title="Create tenant"
      onClose={onClose}
      odId="modal-create-tenant"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-od-id="btn-admin-cancel-tenant"
            data-testid="btn-admin-cancel-tenant"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="submit"
            form="create-tenant-form"
            disabled={submitting || !name.trim() || !slug.trim() || !ownerEmail.trim()}
            data-od-id="btn-admin-submit-tenant"
            data-testid="btn-admin-submit-tenant"
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-50"
          >
            {submitting ? 'Creating…' : 'Create tenant'}
          </button>
        </>
      }
    >
      <form id="create-tenant-form" onSubmit={handleSubmit} className="space-y-4 p-5">
        {generalError && (
          <div className="rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] p-3 text-[13px] text-danger">
            {generalError}
          </div>
        )}

        <div>
          <label className={labelCls} htmlFor="admin-ws-name">
            Workspace name
          </label>
          <input
            id="admin-ws-name"
            className={inputCls}
            data-od-id="input-admin-ws-name"
            data-testid="input-admin-ws-name"
            placeholder="Acme Corp"
            value={name}
            onChange={(e) => handleNameChange(e.target.value)}
            autoFocus
          />
          {fieldErrors.name && (
            <p className="mt-1 text-[12px] text-danger">{fieldErrors.name}</p>
          )}
        </div>

        <div>
          <label className={labelCls} htmlFor="admin-ws-slug">
            Workspace slug
          </label>
          <div className="flex items-center gap-2">
            <input
              id="admin-ws-slug"
              className={inputCls}
              data-od-id="input-admin-ws-slug"
              data-testid="input-admin-ws-slug"
              placeholder="acme"
              value={slug}
              onChange={(e) => handleSlugChange(e.target.value)}
            />
          </div>
          {fieldErrors.slug ? (
            <p className="mt-1 text-[12px] text-danger">{fieldErrors.slug}</p>
          ) : (
            <p className="mt-1 text-[11px] text-muted">
              Used in identifier and routes. Lowercase letters, numbers, and hyphens.
            </p>
          )}
        </div>

        <div>
          <label className={labelCls} htmlFor="admin-ws-tz">
            Timezone
          </label>
          <TimezoneSelect
            id="admin-ws-tz"
            data-od-id="select-admin-ws-tz"
            data-testid="select-admin-ws-tz"
            value={timezone}
            onChange={(tz) => setTimezone(tz)}
          />
        </div>

        <div>
          <label className={labelCls} htmlFor="admin-ws-owner">
            Owner
          </label>
          <UserPicker
            id="admin-ws-owner"
            data-od-id="input-admin-ws-owner-email"
            data-testid="input-admin-ws-owner-email"
            valueKey="email"
            value={ownerEmail}
            onChange={(val) => {
              setOwnerEmail(val);
              if (fieldErrors.ownerEmail) {
                setFieldErrors((prev) => {
                  const copy = { ...prev };
                  delete copy.ownerEmail;
                  return copy;
                });
              }
            }}
            placeholder="Select owner…"
            error={Boolean(fieldErrors.ownerEmail)}
          />
          {fieldErrors.ownerEmail ? (
            <p className="mt-1 text-[12px] text-danger">{fieldErrors.ownerEmail}</p>
          ) : (
            <p className="mt-1 text-[11px] text-muted">
              The designated user will be assigned the Owner role.
            </p>
          )}
        </div>
      </form>
    </Modal>
  );
}
