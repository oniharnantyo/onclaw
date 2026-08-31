import { useState } from "react";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { Icon } from "../components/ui/Icon";
import { cx } from "../lib/helpers";

export interface ApiKeyDialogProps {
  onClose: () => void;
  onSave: (key: any) => void;
  onToast: (text: string, kind?: string) => void;
}

export function ApiKeyDialog({ onClose, onSave, onToast }: ApiKeyDialogProps) {
  const [name, setName] = useState('');
  const [createdKey, setCreatedKey] = useState<{
    id: string;
    name: string;
    masked: string;
    full: string;
    created: string;
  } | null>(null);

  const isValid = name.trim().length > 0;

  const handleCreate = (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!isValid) return;

    const suffix = Math.random().toString(36).slice(2, 6);
    const fullKey = 'oc_live_' + Math.random().toString(36).slice(2, 10) + suffix;
    const keyObj = {
      id: 'k_' + suffix,
      name: name.trim(),
      masked: 'oc_live_••••••••' + suffix,
      full: fullKey,
      created: 'Aug 2026',
    };

    onSave(keyObj);
    onToast("API key created — copy it now, it won't be shown again");
    setCreatedKey(keyObj);
  };

  const handleCopy = () => {
    if (!createdKey) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(createdKey.full).then(
        () => onToast('Key copied to clipboard'),
        () => onToast('Clipboard blocked by the browser', 'danger')
      );
    } else {
      onToast('Clipboard API not available', 'danger');
    }
  };

  if (createdKey) {
    return (
      <Modal
        title="Save your API key"
        onClose={onClose}
        odId="modal-api-key-created"
        data-testid="modal-api-key-created"
        footer={
          <button
            type="button"
            onClick={onClose}
            data-od-id="btn-key-done"
            data-testid="btn-key-done"
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]"
          >
            Done
          </button>
        }
      >
        <div className="space-y-4 p-5">
          <div className="rounded-md border border-[color-mix(in_oklab,var(--warn)_40%,transparent)] bg-[color-mix(in_oklab,var(--warn)_10%,transparent)] p-3 text-[12px] text-fg">
            <p className="font-semibold text-fg">Copy this key now</p>
            <p className="mt-0.5 text-muted">
              For security reasons, this key will never be displayed in full again. Store it securely in your secret manager or environment variables.
            </p>
          </div>

          <div>
            <label className={labelCls} htmlFor="created-key-name">
              Key name
            </label>
            <p id="created-key-name" className="text-[14px] font-medium text-fg">
              {createdKey.name}
            </p>
          </div>

          <div>
            <label className={labelCls} htmlFor="created-key-val">
              API key
            </label>
            <div className="flex items-center gap-2">
              <input
                id="created-key-val"
                readOnly
                className={cx(inputCls, 'font-mono text-[13px] select-all')}
                value={createdKey.full}
                aria-label="Generated API key"
                data-od-id="input-created-key"
                data-testid="input-created-key"
              />
              <button
                type="button"
                data-od-id="btn-copy-created-key"
                data-testid="btn-copy-created-key"
                aria-label="Copy key"
                onClick={handleCopy}
                className="flex h-9 shrink-0 items-center gap-1.5 rounded-md border border-line px-3 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
              >
                <Icon name="copy" size={14} /> Copy
              </button>
            </div>
          </div>
        </div>
      </Modal>
    );
  }

  return (
    <Modal
      title="Create API key"
      onClose={onClose}
      odId="modal-api-key"
      data-testid="modal-api-key"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-od-id="btn-key-cancel"
            data-testid="btn-key-cancel"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="submit"
            form="create-key-form"
            data-od-id="btn-confirm-create-key"
            data-testid="btn-confirm-create-key"
            disabled={!isValid}
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent"
          >
            Create key
          </button>
        </>
      }
    >
      <form id="create-key-form" onSubmit={handleCreate} className="space-y-4 p-5">
        <div>
          <label className={labelCls} htmlFor="key-name">
            Key name
          </label>
          <input
            id="key-name"
            className={inputCls}
            placeholder="e.g. CI / CD Pipeline"
            value={name}
            aria-label="Key name"
            data-od-id="input-key-name"
            data-testid="input-key-name"
            onChange={(e) => setName(e.target.value)}
            autoFocus
          />
          <p className="mt-1 text-[11px] leading-4 text-muted">
            A descriptive name to identify which service or integration is using this key.
          </p>
        </div>
      </form>
    </Modal>
  );
}
