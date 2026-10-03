import { useState, useEffect } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Toggle } from "../../components/ui/Toggle";
import { Chip } from "../../components/ui/Chip";
import { Segmented } from "../../components/ui/Segmented";
import { ErrorState } from "../../components/ErrorState";
import { api, formatApiError, ApiError, type ApiProviderConfig } from "../../lib/api";
import { useCanWriteProviders } from "../../lib/writePerms";
import {
  ProviderFormDialog,
  PROVIDER_CATALOG_TYPES,
  getProviderTypeLabel,
  isDecisionProviderType,
  PROVIDER_GROUP_LABELS,
} from "../../modals/ProviderFormDialog";
import serverErrorSvg from "../../assets/server-error.svg";

export { PROVIDER_CATALOG_TYPES, getProviderTypeLabel };

export interface ProvidersPaneProps {
  tenant: any;
  onToast: (text: string, kind?: string) => void;
  onUpdate?: (fn: any) => void;
}

export function ProvidersPane({ tenant, onToast, onUpdate }: ProvidersPaneProps) {
  // Creation, edit, enable/disable, and delete ride providers.write; Members
  // browse both tabs read-only (verify stays — reading is a permission too).
  const writer = useCanWriteProviders(tenant);
  const [providers, setProviders] = useState<ApiProviderConfig[]>(tenant.providers || []);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);
  const [dialogState, setDialogState] = useState<
    { mode: 'add'; group: 'language' | 'decision' } | { mode: 'edit'; provider: ApiProviderConfig } | null
  >(null);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);
  const [verifyStatus, setVerifyStatus] = useState<
    Record<string, { loading?: boolean; ok?: boolean; error?: string }>
  >({});

  // Pane-local tab state (D8): a client-side filter of the already-loaded
  // list — no second API call. Decision-class configs list only under the
  // Decision tab; language-model types only under Language models.
  const [tab, setTab] = useState<'language' | 'decision'>('language');

  const targetWsId = tenant.sub || tenant.id;

  const loadProviders = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const res = await api.providers.list(targetWsId);
      setProviders(res.providers || []);
      if (onUpdate) {
        onUpdate((t: any) => ({ ...t, providers: res.providers || [] }));
      }
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 0) {
        // Status 0 keeps the loading/empty state (handled by ConnectionBanner)
        return;
      }
      const apiErr = err instanceof Error ? err : new Error(String(err));
      setLoadError(apiErr);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadProviders();
  }, [targetWsId]);

  const handleSaved = (saved: ApiProviderConfig) => {
    if (dialogState?.mode === 'edit') {
      setProviders((prev) => prev.map((item) => (item.id === saved.id ? saved : item)));
      if (onUpdate) {
        onUpdate((t: any) => ({
          ...t,
          providers: (t.providers || []).map((item: any) => (item.id === saved.id ? saved : item)),
        }));
      }
    } else {
      setProviders((prev) => [...prev, saved]);
      if (onUpdate) {
        onUpdate((t: any) => ({ ...t, providers: [...(t.providers || []), saved] }));
      }
    }
  };

  const handleToggle = async (p: ApiProviderConfig) => {
    const nextEnabled = !p.enabled;
    try {
      const res = await api.providers.patch(targetWsId, p.id, { enabled: nextEnabled });
      setProviders((prev) => prev.map((item) => (item.id === p.id ? res.provider : item)));
      if (onUpdate) {
        onUpdate((t: any) => ({
          ...t,
          providers: (t.providers || []).map((item: any) => (item.id === p.id ? res.provider : item)),
        }));
      }
      onToast(`${nextEnabled ? 'Enabled' : 'Disabled'} ${p.name}`);
    } catch (err: unknown) {
      onToast(formatApiError(err, 'Failed to update provider state'), 'danger');
      loadProviders();
    }
  };

  const handleDelete = async (p: ApiProviderConfig) => {
    if (confirmDeleteId !== p.id) {
      setConfirmDeleteId(p.id);
      setTimeout(() => {
        setConfirmDeleteId((cur) => (cur === p.id ? null : cur));
      }, 4000);
      return;
    }

    try {
      await api.providers.delete(targetWsId, p.id);
      setProviders((prev) => prev.filter((item) => item.id !== p.id));
      if (onUpdate) {
        onUpdate((t: any) => ({
          ...t,
          providers: (t.providers || []).filter((item: any) => item.id !== p.id),
        }));
      }
      onToast(`${p.name} deleted`);
    } catch (err: unknown) {
      onToast(formatApiError(err, 'Failed to delete provider'), 'danger');
    } finally {
      setConfirmDeleteId(null);
    }
  };

  const handleVerify = async (p: ApiProviderConfig) => {
    setVerifyStatus((prev) => ({ ...prev, [p.id]: { loading: true } }));
    try {
      const res = await api.providers.verify(targetWsId, p.id);
      setVerifyStatus((prev) => ({
        ...prev,
        [p.id]: { loading: false, ok: res.ok, error: res.error },
      }));
    } catch (err: unknown) {
      const message = formatApiError(err, 'Verification request failed');
      setVerifyStatus((prev) => ({
        ...prev,
        [p.id]: { loading: false, ok: false, error: message },
      }));
      onToast(message, 'danger');
    }
  };

  const clearVerify = (id: string) => {
    setVerifyStatus((prev) => {
      const next = { ...prev };
      delete next[id];
      return next;
    });
  };

  const languageProviders = providers.filter((p) => !isDecisionProviderType(p.type));
  const decisionProviders = providers.filter((p) => isDecisionProviderType(p.type));
  const activeProviders = tab === 'decision' ? decisionProviders : languageProviders;

  return (
    <div className="max-w-xl" data-od-id="pane-providers" data-testid="pane-providers">
      {writer && activeProviders.length > 0 && (
        <div className="mb-4 flex justify-end">
          <button
            type="button"
            data-od-id="btn-provider-add"
            data-testid="btn-provider-add"
            onClick={() => setDialogState({ mode: 'add', group: tab })}
            className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
          >
            Add provider
          </button>
        </div>
      )}

      {!loading && !loadError && (
        <>
          <Segmented
            value={tab}
            onChange={(t) => setTab(t as 'language' | 'decision')}
            options={[
              {
                id: 'language',
                label: PROVIDER_GROUP_LABELS.language,
                testid: 'providers-tab-language',
              },
              {
                id: 'decision',
                label: PROVIDER_GROUP_LABELS.decision,
                testid: 'providers-tab-decision',
              },
            ]}
          />

          {tab === 'decision' && (
            <p
              className="mt-3 text-[12px] leading-5 text-muted"
              data-od-id="providers-decision-hint"
              data-testid="providers-decision-hint"
            >
              Decision providers power routing calls — they never serve chat or agent models.
            </p>
          )}
        </>
      )}

      {loading ? (
        <div className="py-8 text-center text-[13px] text-muted">Loading providers…</div>
      ) : loadError ? (
        <div className="py-8">
          <ErrorState
            variant="full"
            illustration={serverErrorSvg}
            title="Couldn't load providers"
            description="A server error occurred while loading providers."
            status={loadError instanceof ApiError ? loadError.status : 500}
            detail={loadError.message}
            primaryAction={{
              label: 'Retry',
              onClick: loadProviders,
            }}
          />
        </div>
      ) : tab === 'decision' && activeProviders.length === 0 ? (
        <div
          className="py-8 text-center"
          data-od-id="providers-decision-empty"
          data-testid="providers-decision-empty"
        >
          <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
            <Icon name="sliders" size={20} />
          </div>
          <p className="text-[14px] font-medium text-fg">No decision providers configured</p>
          <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
            Decision providers are the memory routing backend — the calls that decide what to
            remember run through them. They never serve chat or agent models.
          </p>
          {writer && (
            <button
              type="button"
              data-od-id="btn-provider-decision-empty-add"
              data-testid="btn-provider-decision-empty-add"
              onClick={() => setDialogState({ mode: 'add', group: 'decision' })}
              className="mt-3 inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              <Icon name="plus" size={13} /> Add a decision provider
            </button>
          )}
        </div>
      ) : activeProviders.length === 0 ? (
        <div className="py-8 text-center" data-od-id="providers-empty" data-testid="providers-empty">
          <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
            <Icon name="sliders" size={20} />
          </div>
          <p className="text-[14px] font-medium text-fg">No providers configured</p>
          <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
            Configure credentials for OpenAI, Anthropic, Gemini, OpenRouter, or custom compatible endpoints to power workspace agents.
          </p>
          {writer && (
            <button
              type="button"
              data-od-id="btn-provider-empty-add"
              data-testid="btn-provider-empty-add"
              onClick={() => setDialogState({ mode: 'add', group: 'language' })}
              className="mt-3 inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              <Icon name="plus" size={13} /> Add your first provider
            </button>
          )}
        </div>
      ) : (
        <div className="mt-4 space-y-3">
          {activeProviders.map((p) => {
            const vResult = verifyStatus[p.id];
            const isVerifying = vResult?.loading;

            return (
              <div
                key={p.id}
                className={cx(
                  'rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] p-3.5 transition-colors',
                  !p.enabled && 'opacity-70'
                )}
                data-od-id={'provider-' + p.id}
                data-testid={'provider-' + p.id}
              >
                <div className="flex items-center gap-3">
                  <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent">
                    <Icon name="sliders" size={16} />
                  </span>

                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="truncate text-[14px] font-medium text-fg">{p.name}</p>
                      <Chip>{getProviderTypeLabel(p.type)}</Chip>
                      {p.key_set ? (
                        <span className="font-mono text-[11px] text-muted">
                          Key: ••••{p.key_hint || '••••'}
                        </span>
                      ) : (
                        <span className="text-[11px] text-[color-mix(in_oklab,var(--warn),black_38%)]">
                          No key set
                        </span>
                      )}
                    </div>

                    {p.base_url ? (
                      <p className="truncate font-mono text-[11px] text-muted">{p.base_url}</p>
                    ) : null}
                  </div>

                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      data-od-id={'btn-verify-' + p.id}
                      data-testid={'btn-verify-' + p.id}
                      disabled={isVerifying || !p.key_set}
                      onClick={() => handleVerify(p)}
                      className="h-8 shrink-0 rounded-md border border-line px-2.5 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40 disabled:hover:border-line"
                    >
                      {isVerifying ? 'Verifying…' : 'Verify'}
                    </button>

                    {writer && (
                      <Toggle
                        on={p.enabled}
                        label={'Enable ' + p.name}
                        onChange={() => handleToggle(p)}
                      />
                    )}

                    {writer && (
                      <button
                        type="button"
                        aria-label={'Edit ' + p.name}
                        data-od-id={'btn-edit-' + p.id}
                        data-testid={'btn-edit-' + p.id}
                        onClick={() => setDialogState({ mode: 'edit', provider: p })}
                        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
                      >
                        <Icon name="edit" size={13} />
                      </button>
                    )}

                    {writer && confirmDeleteId === p.id ? (
                      <button
                        type="button"
                        data-od-id={'btn-delete-confirm-' + p.id}
                        data-testid={'btn-delete-confirm-' + p.id}
                        onClick={() => handleDelete(p)}
                        className="h-7 shrink-0 rounded-md border border-[color-mix(in_oklab,var(--danger)_45%,transparent)] px-2 text-[11px] font-medium text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)]"
                      >
                        Click to confirm
                      </button>
                    ) : writer ? (
                      <button
                        type="button"
                        aria-label={'Delete ' + p.name}
                        data-od-id={'btn-delete-' + p.id}
                        data-testid={'btn-delete-' + p.id}
                        onClick={() => handleDelete(p)}
                        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] hover:text-danger"
                      >
                        <Icon name="x" size={13} />
                      </button>
                    ) : null}
                  </div>
                </div>

                {vResult && !vResult.loading && (
                  <div className="mt-2.5">
                    {vResult.ok ? (
                      <div
                        className="flex items-center justify-between rounded-md bg-[color-mix(in_oklab,var(--success)_12%,transparent)] px-3 py-1.5 text-[12px] text-[color-mix(in_oklab,var(--success),black_20%)]"
                        data-od-id={'verify-success-' + p.id}
                        data-testid={'verify-success-' + p.id}
                      >
                        <span className="flex items-center gap-1.5 font-medium">
                          <Icon name="check" size={14} /> Connection verified successfully
                        </span>
                        <button
                          type="button"
                          aria-label="Dismiss verification result"
                          onClick={() => clearVerify(p.id)}
                          className="text-muted hover:text-fg"
                        >
                          <Icon name="x" size={12} />
                        </button>
                      </div>
                    ) : (
                      <div
                        className="flex items-start justify-between rounded-md bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] px-3 py-1.5 text-[12px] text-danger"
                        data-od-id={'verify-error-' + p.id}
                        data-testid={'verify-error-' + p.id}
                      >
                        <span className="flex items-start gap-1.5 font-medium">
                          <Icon name="alert" size={14} className="mt-0.5 shrink-0" />
                          <span>{vResult.error || 'Verification failed'}</span>
                        </span>
                        <button
                          type="button"
                          aria-label="Dismiss verification result"
                          onClick={() => clearVerify(p.id)}
                          className="text-muted hover:text-fg"
                        >
                          <Icon name="x" size={12} />
                        </button>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {dialogState && (
        <ProviderFormDialog
          workspaceId={targetWsId}
          provider={dialogState.mode === 'edit' ? dialogState.provider : null}
          typeGroup={dialogState.mode === 'add' ? dialogState.group : undefined}
          onClose={() => setDialogState(null)}
          onSaved={handleSaved}
          onToast={onToast}
        />
      )}
    </div>
  );
}
