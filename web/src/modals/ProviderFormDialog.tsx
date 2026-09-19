import { useState } from "react";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { cx } from "../lib/helpers";
import { api, formatApiError, type ApiProviderConfig } from "../lib/api";

export const PROVIDER_CATALOG_TYPES = [
  { id: 'openai', label: 'OpenAI', placeholder: 'https://api.openai.com/v1 (optional override)', requiresBaseUrl: false },
  { id: 'anthropic', label: 'Anthropic', placeholder: 'https://api.anthropic.com/v1 (optional override)', requiresBaseUrl: false },
  { id: 'gemini', label: 'Gemini', placeholder: 'https://generativelanguage.googleapis.com/v1beta (optional override)', requiresBaseUrl: false },
  { id: 'openrouter', label: 'OpenRouter', placeholder: 'https://openrouter.ai/api/v1 (optional override)', requiresBaseUrl: false },
  { id: 'openai-compatible', label: 'OpenAI-compatible', placeholder: 'https://api.together.xyz/v1', requiresBaseUrl: true },
  { id: 'anthropic-compatible', label: 'Anthropic-compatible', placeholder: 'https://api.anthropic-proxy.com/v1', requiresBaseUrl: true },
] as const;

/** Provider types that accept the optional catalog-mapping hint (D3). The
 * four canonical types are catalog-mapped by their type alone. */
const CATALOG_MAPPED_TYPES = ['openai-compatible', 'anthropic-compatible'];

/** Curated catalog-mapping options — every id verified against the community
 * catalog cache (.onclaw/cache/models.dev.json top-level provider ids). */
const CATALOG_PROVIDER_OPTIONS = [
  { id: 'zai-coding-plan', label: 'Z.AI Coding Plan' },
  { id: 'zhipuai-coding-plan', label: 'Zhipu AI Coding Plan' },
  { id: 'zhipuai', label: 'Zhipu AI' },
  { id: 'openrouter', label: 'OpenRouter' },
  { id: 'deepseek', label: 'DeepSeek' },
  { id: 'mistral', label: 'Mistral' },
  { id: 'groq', label: 'Groq' },
  { id: 'fireworks-ai', label: 'Fireworks AI' },
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
  // Draft verification (refactor-workspace-settings D6): a test of the
  // entered values, never a save gate. State is display-only.
  const [verifying, setVerifying] = useState(false);
  const [verifyResult, setVerifyResult] = useState<{ ok: boolean; error?: string } | null>(null);
  // Explicit catalog-mapping selection (D3). null = untouched "auto": the
  // select DISPLAYS the host-derived suggestion but persists an EMPTY
  // catalog_provider so server-side host auto-detect keeps working; only an
  // explicit selection persists its id.
  const [catalogProvider, setCatalogProvider] = useState<string | null>(
    provider?.catalog_provider || null
  );

  const selectedTypeConfig =
    PROVIDER_CATALOG_TYPES.find((c) => c.id === type) || PROVIDER_CATALOG_TYPES[0];
  const isCatalogMappable = CATALOG_MAPPED_TYPES.includes(type);
  const catalogSelectValue = catalogProvider ?? provider?.suggested_catalog_provider ?? '';

  const isValid =
    name.trim().length > 0 &&
    (!selectedTypeConfig.requiresBaseUrl || baseUrl.trim().length > 0);

  // A credential is expressible when the user typed a key, or when editing a
  // config that has a stored key (blank field falls back to it server-side).
  const canVerify = key.trim().length > 0 || (isEdit && Boolean(provider?.key_set));

  const handleVerifyDraft = async () => {
    if (!canVerify || verifying) return;
    setVerifying(true);
    setVerifyResult(null);
    try {
      const body: {
        type: string;
        base_url?: string;
        key?: string;
        catalog_provider?: string;
        provider_id?: string;
      } = { type };
      // The stored-key fallback only makes sense against the config being
      // edited; creates always carry the typed key.
      if (isEdit && provider) body.provider_id = provider.id;
      if (baseUrl.trim()) body.base_url = baseUrl.trim();
      if (key.trim()) body.key = key.trim();
      if (isCatalogMappable) body.catalog_provider = catalogProvider ?? '';
      const res = await api.providers.verifyDraft(workspaceId, body);
      setVerifyResult(res.ok ? { ok: true } : { ok: false, error: res.error || 'Verification failed' });
    } catch (err: unknown) {
      setVerifyResult({ ok: false, error: formatApiError(err, 'Verification failed') });
    } finally {
      setVerifying(false);
    }
  };

  const handleSubmit = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!isValid || saving) return;

    setSaving(true);
    try {
      if (isEdit && provider) {
        const payload: { name?: string; base_url?: string; key?: string; enabled?: boolean; catalog_provider?: string } = {
          name: name.trim(),
          enabled,
        };
        if (isCatalogMappable) {
          payload.catalog_provider = catalogProvider ?? '';
        }
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
        const payload: { type: string; name: string; base_url?: string; key?: string; enabled?: boolean; catalog_provider?: string } = {
          type,
          name: name.trim(),
          enabled,
        };
        if (isCatalogMappable) {
          payload.catalog_provider = catalogProvider ?? '';
        }
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
            Full API base including the version path (e.g. https://api.example.com/v1) — the server appends resource paths like /models or /chat/completions on top.
          </p>
        </div>

        {isCatalogMappable && (
          <div>
            <label className={labelCls} htmlFor="prov-catalog">
              Catalog mapping (optional)
            </label>
            <select
              id="prov-catalog"
              aria-label="Catalog mapping"
              className={inputCls}
              value={catalogSelectValue}
              onChange={(e) => setCatalogProvider(e.target.value)}
            >
              <option value="">Auto-detect from host</option>
              {CATALOG_PROVIDER_OPTIONS.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.label}
                </option>
              ))}
            </select>
            <p className="mt-1 text-[11px] leading-4 text-muted">
              Maps this gateway onto the community model catalog — used for model lists and model capabilities (image input, reasoning, tool calling).
            </p>
          </div>
        )}

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

        {/* Draft verification (D6): tests the entered values without saving
            anything — the outcome is display-only and never gates Save. */}
        <div>
          <div className="flex min-h-8 flex-wrap items-center gap-3">
            <button
              type="button"
              onClick={handleVerifyDraft}
              disabled={!canVerify || verifying}
              title={canVerify ? undefined : 'Enter an API key to verify'}
              data-od-id="btn-provider-verify-draft"
              data-testid="btn-provider-verify-draft"
              className="flex h-8 items-center rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
            >
              {verifying ? 'Verifying…' : 'Verify connection'}
            </button>
            {verifying && (
              <span data-testid="provider-verify-busy" role="status" className="text-[12px] text-muted">
                Testing the entered credentials…
              </span>
            )}
            {!verifying && verifyResult && (
              <span
                data-testid="provider-verify-result"
                role="status"
                className={cx(
                  'text-[12px]',
                  verifyResult.ok ? 'font-medium text-success' : 'text-danger'
                )}
              >
                {verifyResult.ok ? 'Connection verified successfully' : verifyResult.error}
              </span>
            )}
          </div>
          {!verifying && !verifyResult && (
            <p className="mt-1 text-[11px] leading-4 text-muted">
              {isEdit && provider?.key_set && !key.trim()
                ? `Runs against the stored key (••••${provider.key_hint || '••••'}) with the type and base URL entered here.`
                : 'Tests the values entered here against the provider without saving anything.'}
            </p>
          )}
        </div>
      </form>
    </Modal>
  );
}
