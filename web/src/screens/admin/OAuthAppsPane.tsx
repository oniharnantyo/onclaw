import { useEffect, useMemo, useState } from 'react';
import { cx } from '../../lib/helpers';
import { Icon } from '../../components/ui/Icon';
import { Chip } from '../../components/ui/Chip';
import { ErrorState } from '../../components/ErrorState';
import { formatApiError, ApiError } from '../../lib/api';
import { inputCls, labelCls } from '../../components/ui/constants';
import { adminOAuthAppsApi, type ApiOAuthApp } from '../../lib/connectionsApi';
import serverErrorSvg from '../../assets/server-error.svg';

interface OAuthAppsPaneProps {
  onToast: (text: string, kind?: string) => void;
}

/** The providers this instance ships OAuth recipes for (add-connection-oauth
 * 4.3). The apps list can name more — they render below the known rows. */
const KNOWN_PROVIDERS: { id: string; label: string }[] = [
  { id: 'atlassian', label: 'Atlassian (Jira & Confluence)' },
  { id: 'slack', label: 'Slack' },
  { id: 'linear', label: 'Linear' },
];

/**
 * Instance admin OAuth apps pane (add-connection-oauth 4.3): one app per
 * provider, shared by every workspace on the instance — workspaces consent
 * through it, tokens stay per-connection. The client secret is write-only:
 * after a save the row carries the last-4 hint only. The redirect URI is the
 * exact value derived from the instance's public base URL and must be
 * configured at the provider for consent to work. Master-tenant gating rides
 * AdminView's useIsAdmin guard, same as the other admin panes.
 */
export function OAuthAppsPane({ onToast }: OAuthAppsPaneProps) {
  const [apps, setApps] = useState<ApiOAuthApp[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);
  const [saving, setSaving] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});
  // Per-provider draft fields; the secret never round-trips back into state.
  const [drafts, setDrafts] = useState<Record<string, { clientId: string; secret: string }>>({});

  const fetchApps = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const res = await adminOAuthAppsApi.list();
      setApps(res?.apps || []);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 0) {
        // Status 0 keeps the loading/empty state (handled by ConnectionBanner)
        return;
      }
      setLoadError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchApps();
  }, []);

  // Prefill drafts from the loaded rows; a save merges the echoed app (which
  // carries the fresh client_id + hint) so the secret field comes back empty.
  useEffect(() => {
    setDrafts((prev) => {
      const next: typeof prev = {};
      for (const app of apps) {
        next[app.provider] = {
          clientId: prev[app.provider]?.clientId ?? app.client_id ?? '',
          secret: '',
        };
      }
      return next;
    });
  }, [apps]);

  const providers = useMemo(() => {
    const known = new Set(KNOWN_PROVIDERS.map((p) => p.id));
    const extra = apps
      .filter((a) => !known.has(a.provider))
      .map((a) => ({ id: a.provider, label: a.provider }));
    return [...KNOWN_PROVIDERS, ...extra];
  }, [apps]);

  const appFor = (id: string) => apps.find((a) => a.provider === id);
  const draftFor = (id: string) => drafts[id] || { clientId: appFor(id)?.client_id || '', secret: '' };

  const handleSave = async (provider: { id: string; label: string }) => {
    if (saving) return;
    const draft = draftFor(provider.id);
    if (!draft.clientId.trim() || !draft.secret.trim()) return;
    setSaving(provider.id);
    setRowErrors((prev) => ({ ...prev, [provider.id]: '' }));
    const wasRegistered = Boolean(appFor(provider.id)?.client_id);
    try {
      const res = await adminOAuthAppsApi.save(provider.id, {
        client_id: draft.clientId.trim(),
        client_secret: draft.secret,
      });
      const app = res?.app;
      if (app) {
        setApps((prev) => [...prev.filter((a) => a.provider !== provider.id), app]);
      }
      onToast(`${provider.label} app ${wasRegistered ? 'updated' : 'registered'}`);
    } catch (err: unknown) {
      // Missing instance-admin permission, invalid credentials, provider
      // constraints — all arrive as error envelopes.
      setRowErrors((prev) => ({
        ...prev,
        [provider.id]: formatApiError(err, `Failed to save the ${provider.label} app`),
      }));
    } finally {
      setSaving(null);
    }
  };

  const handleCopy = async (providerId: string) => {
    const uri = appFor(providerId)?.redirect_uri;
    if (!uri) return;
    try {
      await navigator.clipboard.writeText(uri);
      setCopied(providerId);
      window.setTimeout(() => setCopied((c) => (c === providerId ? null : c)), 1500);
    } catch {
      onToast("Couldn't access the clipboard — select the URI and copy it manually", 'danger');
    }
  };

  return (
    <div className="space-y-6" data-od-id="pane-admin-oauth-apps" data-testid="pane-admin-oauth-apps">
      <div>
        <h2 className="text-[18px] font-semibold text-fg">OAuth apps</h2>
        <p className="mt-1 max-w-2xl text-[13px] leading-5 text-muted">
          One app per provider, shared by every workspace on this instance — members consent
          through it and their tokens stay per-connection. Without a registration here, the
          provider's connections stay unavailable.
        </p>
      </div>

      {loading ? (
        <div className="flex h-48 items-center justify-center rounded-lg border border-line bg-surface">
          <div className="flex items-center gap-3 text-[13px] text-muted">
            <div className="h-5 w-5 animate-spin rounded-full border-2 border-line border-t-accent" />
            Loading apps…
          </div>
        </div>
      ) : loadError ? (
        <div className="flex min-h-[320px] items-center justify-center rounded-lg border border-line bg-surface p-6">
          <ErrorState
            variant="full"
            illustration={serverErrorSvg}
            title="Couldn't load OAuth apps"
            description="A server error occurred while loading the app registrations."
            status={loadError instanceof ApiError ? loadError.status : 500}
            detail={loadError.message}
            primaryAction={{
              label: 'Retry',
              onClick: fetchApps,
            }}
          />
        </div>
      ) : (
        <div className="space-y-3">
          {providers.map((p) => {
            const app = appFor(p.id);
            const registered = Boolean(app?.client_id);
            const draft = draftFor(p.id);
            const busy = saving === p.id;
            const complete = draft.clientId.trim() !== '' && draft.secret.trim() !== '';
            return (
              <div
                key={p.id}
                className="rounded-lg border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] p-4"
                data-testid={'oauth-app-row-' + p.id}
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex min-w-0 items-center gap-2.5">
                    <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted">
                      <Icon name="key" size={15} />
                    </span>
                    <p className="min-w-0 truncate text-[14px] font-medium text-fg">{p.label}</p>
                  </div>
                  {registered ? (
                    <span
                      className="inline-flex items-center gap-1.5 font-mono text-[11px] text-success"
                      data-testid={'oauth-app-status-' + p.id}
                    >
                      <Icon name="check" size={12} /> Registered
                    </span>
                  ) : (
                    <span
                      className="inline-flex items-center gap-1.5 font-mono text-[11px] text-muted"
                      data-testid={'oauth-app-status-' + p.id}
                    >
                      Not registered
                    </span>
                  )}
                </div>

                <div className="mt-3 grid gap-3 sm:grid-cols-2">
                  <div>
                    <label className={labelCls} htmlFor={'oauth-client-id-' + p.id}>
                      Client ID
                    </label>
                    <input
                      id={'oauth-client-id-' + p.id}
                      value={draft.clientId}
                      onChange={(e) =>
                        setDrafts((prev) => ({
                          ...prev,
                          [p.id]: { clientId: e.target.value, secret: prev[p.id]?.secret || '' },
                        }))
                      }
                      placeholder="From the provider's app settings"
                      autoComplete="off"
                      spellCheck={false}
                      data-testid={'input-oauth-client-id-' + p.id}
                      className={cx(inputCls, 'font-mono')}
                    />
                  </div>
                  <div>
                    <label className={labelCls} htmlFor={'oauth-client-secret-' + p.id}>
                      Client secret
                    </label>
                    <input
                      id={'oauth-client-secret-' + p.id}
                      type="password"
                      value={draft.secret}
                      onChange={(e) =>
                        setDrafts((prev) => ({
                          ...prev,
                          [p.id]: { clientId: prev[p.id]?.clientId || '', secret: e.target.value },
                        }))
                      }
                      placeholder={registered ? 'Enter a new secret to replace it' : 'Paste the client secret'}
                      autoComplete="new-password"
                      data-testid={'input-oauth-secret-' + p.id}
                      className={cx(inputCls, 'font-mono')}
                    />
                    <p className="mt-1 text-[11px] leading-4 text-muted">
                      {registered && app?.client_secret_hint
                        ? `Saved — only its last 4 are shown: ····${app.client_secret_hint}. Stored encrypted, never displayed in full.`
                        : 'Stored encrypted and never displayed again — only its last 4.'}
                    </p>
                  </div>
                </div>

                {app?.redirect_uri && (
                  <div className="mt-3" data-testid={'oauth-redirect-' + p.id}>
                    <span className={labelCls}>Redirect URI</span>
                    <div className="flex items-center gap-2">
                      <code
                        className="min-w-0 flex-1 truncate rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-3 py-2 font-mono text-[12px] text-fg2"
                        data-testid={'oauth-redirect-uri-' + p.id}
                        title={app.redirect_uri}
                      >
                        {app.redirect_uri}
                      </code>
                      <button
                        type="button"
                        onClick={() => void handleCopy(p.id)}
                        data-testid={'btn-oauth-copy-' + p.id}
                        aria-label="Copy redirect URI"
                        className="flex h-9 shrink-0 items-center gap-1.5 rounded-md border border-line px-2.5 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                      >
                        <Icon name="copy" size={12} />
                        {copied === p.id ? 'Copied' : 'Copy'}
                      </button>
                    </div>
                    <p className="mt-1 text-[11px] leading-4 text-muted">
                      Configure this exact URI as an allowed callback in the provider's app
                      settings — it comes from this instance's public base URL.
                    </p>
                  </div>
                )}

                {rowErrors[p.id] && (
                  <p
                    role="alert"
                    data-testid={'oauth-app-error-' + p.id}
                    className="mt-3 flex items-start gap-1.5 rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] px-3 py-2 text-[12px] leading-4 text-danger"
                  >
                    <Icon name="alert" size={13} className="mt-0.5 shrink-0" />
                    {rowErrors[p.id]}
                  </p>
                )}

                <div className="mt-3 flex items-center justify-between gap-3">
                  <Chip mono className="opacity-75">
                    {p.id}
                  </Chip>
                  <button
                    type="button"
                    onClick={() => void handleSave(p)}
                    disabled={!complete || busy}
                    data-testid={'btn-oauth-save-' + p.id}
                    className="flex h-8 shrink-0 items-center gap-2 rounded-md bg-accent px-3.5 text-[12px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
                  >
                    {busy && <Icon name="loader" size={12} className="animate-spin" />}
                    {busy ? 'Saving…' : registered ? 'Update app' : 'Register app'}
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
