import { useState } from "react";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { cx } from "../lib/helpers";
import { api, formatApiError, type ApiProviderConfig } from "../lib/api";

export const PROVIDER_CATALOG_TYPES = [
  { id: 'openai', label: 'OpenAI', placeholder: 'https://api.openai.com (optional override)', requiresBaseUrl: false },
  { id: 'anthropic', label: 'Anthropic', placeholder: 'https://api.anthropic.com (optional override)', requiresBaseUrl: false },
  { id: 'gemini', label: 'Gemini', placeholder: 'https://generativelanguage.googleapis.com (optional override)', requiresBaseUrl: false },
  { id: 'openrouter', label: 'OpenRouter', placeholder: 'https://openrouter.ai (optional override)', requiresBaseUrl: false },
  { id: 'openai-compatible', label: 'OpenAI-compatible', placeholder: 'https://api.together.xyz', requiresBaseUrl: true },
  { id: 'anthropic-compatible', label: 'Anthropic-compatible', placeholder: 'https://api.anthropic-proxy.com', requiresBaseUrl: true },
] as const;

export function getProviderTypeLabel(type: string): string {
  const found = PROVIDER_CATALOG_TYPES.find((c) => c.id === type);
  return found ? found.label : type;
}

export interface ProviderFormDialogProps {
  workspaceId: string;
  provider?: ApiProviderConfig | null;
  onClose: () => void;
  onSaved: (provider: ApiProviderConfig) => void;
  onToast: (text: string, kind?: string) => void;
}

export function ProviderFormDialog({
  workspaceId,
  provider,
  onClose,
  onSaved,
  onToast,
}: ProviderFormDialogProps) {
  const isEdit = Boolean(provider);
  const [type, setType] = useState(provider?.type || 'openai');
  const [name, setName] = useState(provider?.name || '');
  const [baseUrl, setBaseUrl] = useState(provider?.base_url || '');
  const [key, setKey] = useState('');
  const [enabled] = useState(provider ? provider.enabled : true);
  const [saving, setSaving] = useState(false);

  const selectedTypeConfig =
    PROVIDER_CATALOG_TYPES.find((c) => c.id === type) || PROVIDER_CATALOG_TYPES[0];

  const isValid =
    name.trim().length > 0 &&
    (!selectedTypeConfig.requiresBaseUrl || baseUrl.trim().length > 0);

  const handleSubmit = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!isValid || saving) return;

    setSaving(true);
    try {
      if (isEdit && provider) {
        const payload: { name?: string; base_url?: string; key?: string; enabled?: boolean } = {
          name: name.trim(),
          enabled,
        };
        if (selectedTypeConfig.requiresBaseUrl || baseUrl.trim()) {
          payload.base_url = baseUrl.trim();
        }
        if (key.trim()) {
          payload.key = key.trim();
        }

        const res = await api.providers.patch(workspaceId, provider.id, payload);
        onToast(`${res.provider.name} updated`);
        onSaved(res.provider);
        onClose();
      } else {
        const payload: { type: string; name: string; base_url?: string; key?: string; enabled?: boolean } = {
          type,
          name: name.trim(),
          enabled,
        };
        if (baseUrl.trim()) {
          payload.base_url = baseUrl.trim();
        }
        if (key.trim()) {
          payload.key = key.trim();
        }

        const res = await api.providers.create(workspaceId, payload);
        onToast(`Provider ${res.provider.name} created`);
        onSaved(res.provider);
        onClose();
      }
    } catch (err: unknown) {
      onToast(
        formatApiError(err, isEdit ? 'Failed to update provider' : 'Failed to create provider'),
        'danger'
      );
    } finally {
      setSaving(false);
    }
  };

  const submitBtnId = isEdit && provider ? `btn-save-edit-${provider.id}` : 'btn-provider-create-confirm';

  return (
    <Modal
      title={isEdit ? `Edit ${provider?.name}` : 'Add provider'}
      onClose={onClose}
      odId="modal-provider"
      data-testid="modal-provider"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-od-id="btn-provider-cancel"
            data-testid="btn-provider-cancel"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="submit"
            form="provider-form"
            data-od-id={submitBtnId}
            data-testid={submitBtnId}
            disabled={!isValid || saving}
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent"
          >
            {saving
              ? isEdit
                ? 'Saving…'
                : 'Creating…'
              : isEdit
              ? 'Save changes'
              : 'Create provider'}
          </button>
        </>
      }
    >
      <form id="provider-form" onSubmit={handleSubmit} className="space-y-4 p-5">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label className={labelCls} htmlFor="prov-type">
              Provider type
            </label>
            <select
              id="prov-type"
              aria-label="Provider type"
              className={inputCls}
              value={type}
              onChange={(e) => setType(e.target.value)}
              disabled={isEdit}
            >
              {PROVIDER_CATALOG_TYPES.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.label}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className={labelCls} htmlFor="prov-name">
              Name
            </label>
            <input
              id="prov-name"
              aria-label={isEdit ? 'Edit name' : 'Provider name'}
              className={inputCls}
              placeholder="e.g. OpenAI Primary"
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
            />
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between">
            <label className={labelCls} htmlFor="prov-base-url">
              Base URL {selectedTypeConfig.requiresBaseUrl ? '(required)' : '(optional override)'}
            </label>
          </div>
          <input
            id="prov-base-url"
            aria-label={isEdit ? 'Edit base URL' : 'Base URL'}
            className={cx(inputCls, 'font-mono text-[13px]')}
            placeholder={selectedTypeConfig.placeholder}
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
          />
          <p className="mt-1 text-[11px] leading-4 text-muted">
            Origin + optional prefix only (e.g. https://api.example.com). Do not include version paths like /v1 — the server appends canonical paths automatically.
          </p>
        </div>

        <div>
          <label className={labelCls} htmlFor="prov-key">
            API key {isEdit ? '(write-only replacement)' : ''}
          </label>
          <input
            id="prov-key"
            aria-label={isEdit ? 'Edit API key' : 'API key'}
            type="password"
            className={cx(inputCls, 'font-mono text-[13px]')}
            placeholder={
              isEdit && provider?.key_set
                ? `Key set (••••${provider.key_hint || '••••'}). Leave blank to keep.`
                : 'sk-...'
            }
            value={key}
            onChange={(e) => setKey(e.target.value)}
          />
          <p className="mt-1 text-[11px] leading-4 text-muted">
            Write-only password input. Stored encrypted; never displayed or returned in API responses.
          </p>
        </div>
      </form>
    </Modal>
  );
}
