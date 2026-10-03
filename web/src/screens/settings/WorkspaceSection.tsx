import { useEffect, useState } from "react";
import { cx } from "../../lib/helpers";
import { TimezoneSelect } from "../../components/ui/TimezoneSelect";
import { Chip } from "../../components/ui/Chip";
import { ModelCombobox } from "../../components/ui/ModelCombobox";
import { inputCls, labelCls } from "../../components/ui/constants";
import { PROVIDER_TYPES } from "../../lib/constants";
import {
  api,
  ApiError,
  formatApiError,
  type ApiDefaultModel,
  type ApiMemory,
  type ApiProviderConfig,
} from "../../lib/api";
import { useAuthStore } from "../../store/auth";
import { useCanWriteWorkspace } from "../../lib/writePerms";
import { isDecisionProviderType } from "../../modals/ProviderFormDialog";
import { useStore } from "../../store";

export interface WorkspaceSectionProps {
  tenant: any;
  onToast: (text: string, kind?: string) => void;
  onUpdate: (fn: any) => void;
  onLeaveWorkspace?: () => Promise<void> | void;
  onClose?: () => void;
}

export function WorkspaceSection({
  tenant,
  onToast,
  onUpdate,
  onLeaveWorkspace,
  onClose,
}: WorkspaceSectionProps) {
  const [ws, setWs] = useState({
    name: tenant.name,
    tz: tenant.tz,
    retention: tenant.retention,
  });
  const [confirmDel, setConfirmDel] = useState(false);
  const [savingWs, setSavingWs] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const currentUser = useAuthStore((s) => s.user);

  const targetWsId = tenant.sub || tenant.id;

  // Default model pair (refactor-workspace-settings D2): picked provider-first
  // over the workspace's configured providers, with the model from that
  // provider's catalog. Both empty = no default; the pair rides the PATCH as
  // `default_model` only when it differs from the hydrated value.
  const [providers, setProviders] = useState<ApiProviderConfig[]>([]);
  const [defaultProvider, setDefaultProvider] = useState('');
  const [defaultModel, setDefaultModel] = useState('');
  const [hydratedDefault, setHydratedDefault] = useState<ApiDefaultModel | null>(null);
  const [defaultLoaded, setDefaultLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;
    api.workspaces
      .get(targetWsId)
      .then((res) => {
        if (cancelled) return;
        const dm = res?.workspace?.default_model ?? null;
        setHydratedDefault(dm);
        setDefaultProvider(dm?.provider_id || '');
        setDefaultModel(dm?.model || '');
        setDefaultLoaded(true);
      })
      .catch(() => {
        // Offline / mock mode — the picker stays usable with no default.
      });
    return () => {
      cancelled = true;
    };
  }, [targetWsId]);

  useEffect(() => {
    let cancelled = false;
    api.providers
      .list(targetWsId)
      .then((res) => {
        if (!cancelled && res?.providers)
          // The default-model picker lists language-model providers only (5.3):
          // decision configs never serve chat or agent models.
          setProviders(res.providers.filter((p) => !isDecisionProviderType(p.type)));
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [targetWsId]);

  // Shared-memory (WORKSPACE.md) editor state — fully independent of the
  // workspace-details draft above so neither save rewrites the other.
  const [memory, setMemory] = useState<ApiMemory | null>(null);
  const [memContent, setMemContent] = useState("");
  const [memLoading, setMemLoading] = useState(true);
  const [memSaving, setMemSaving] = useState(false);
  const [memSaved, setMemSaved] = useState(false);
  const [memError, setMemError] = useState<string | null>(null);

  // Settings-management permission (workspace.write): Owner/Admin persist, so
  // the workspace fields render editable and the save control shows; Members
  // get the read-only view — fields disabled, save hidden. Same derivation as
  // the rest of the canWrite* family (lib/writePerms.ts).
  const canWriteWorkspace = useCanWriteWorkspace(tenant);
  const canWriteMemory = canWriteWorkspace;

  // Loads for every member (read state); offline failures stay silent so the
  // pane still renders in mock mode.
  useEffect(() => {
    let cancelled = false;
    setMemLoading(true);
    setMemError(null);
    api.memory
      .getWorkspace(targetWsId)
      .then((res: ApiMemory) => {
        if (cancelled) return;
        setMemory(res);
        setMemContent(res.content ?? "");
      })
      .catch((err: unknown) => {
        if (cancelled || (err instanceof ApiError && err.status === 0)) return;
        setMemError(formatApiError(err, "Failed to load shared memory"));
      })
      .finally(() => {
        if (!cancelled) setMemLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [targetWsId]);

  const saveMemory = async () => {
    setMemSaving(true);
    setMemError(null);
    try {
      const res = await api.memory.updateWorkspace(targetWsId, { content: memContent });
      setMemory(res);
      setMemSaved(true);
      onToast('Shared memory saved');
    } catch (err: unknown) {
      setMemError(formatApiError(err, "Failed to save shared memory"));
    } finally {
      setMemSaving(false);
    }
  };

  const saveWorkspace = async () => {
    setSavingWs(true);
    try {
      const body: { name: string; timezone: string; default_model?: ApiDefaultModel | null } = {
        name: ws.name,
        timezone: ws.tz,
      };
      // The pair rides the PATCH only when it differs from the hydrated
      // workspace payload — a save that never touched the picker leaves the
      // stored default untouched on the wire.
      const pair: ApiDefaultModel | null =
        defaultProvider && defaultModel ? { provider_id: defaultProvider, model: defaultModel } : null;
      if (defaultLoaded) {
        const unchanged =
          (hydratedDefault?.provider_id || '') === defaultProvider &&
          (hydratedDefault?.model || '') === defaultModel;
        if (!unchanged) body.default_model = pair;
      }
      await api.workspaces.patch(targetWsId, body);
      if (body.default_model !== undefined) setHydratedDefault(pair);
      onUpdate((t: any) => ({ ...t, ...ws }));
      // Mirror the server-confirmed workspace fields into the membership
      // cache so the switcher shows the saved name immediately.
      useAuthStore.setState((s) => ({
        memberships: s.memberships.map((m) =>
          m.workspace_id === targetWsId || m.workspace_slug === targetWsId
            ? {
                ...m,
                workspace_name: body.name,
                workspace: m.workspace ? { ...m.workspace, name: body.name, timezone: body.timezone } : m.workspace,
              }
            : m
        ),
      }));
      onToast('Workspace settings saved');
    } catch (err: unknown) {
      onToast(formatApiError(err, 'Failed to save workspace settings'), 'danger');
    } finally {
      setSavingWs(false);
    }
  };

  const handleLeave = async () => {
    if (!confirmDel) {
      setConfirmDel(true);
      setTimeout(() => setConfirmDel(false), 4000);
      return;
    }

    if (onLeaveWorkspace) {
      await onLeaveWorkspace();
      return;
    }

    setLeaving(true);
    try {
      if (!currentUser) throw new Error('Unauthenticated');
      await api.members.remove(targetWsId, currentUser.id);

      const remaining = useAuthStore
        .getState()
        .memberships.filter((m) => m.workspace_id !== targetWsId && m.workspace_slug !== targetWsId);
      useAuthStore.setState({ memberships: remaining });

      if (onClose) onClose();

      if (remaining.length > 0) {
        const next = remaining[0];
        const nextId = next.workspace_slug || next.workspace_id;
        useStore.getState().switchTenant(nextId);
        onToast('Left ' + tenant.name + ' — switched to ' + (next.workspace_name || nextId));
      } else {
        onToast('Left ' + tenant.name);
      }
    } catch (err: unknown) {
      onToast(formatApiError(err, 'Failed to leave workspace'), 'danger');
    } finally {
      setLeaving(false);
      setConfirmDel(false);
    }
  };

  return (
    <div className="max-w-md space-y-5" data-od-id="pane-workspace" data-testid="pane-workspace">
      <div>
        <label className={labelCls} htmlFor="ws-name">
          Workspace name
        </label>
        <input
          id="ws-name"
          className={cx(inputCls, !canWriteWorkspace && 'disabled:cursor-not-allowed disabled:opacity-60')}
          disabled={!canWriteWorkspace}
          value={ws.name}
          onChange={(e) => setWs({ ...ws, name: e.target.value })}
        />
      </div>
      <div>
        <label className={labelCls} htmlFor="ws-sub">
          Workspace URL
        </label>
        <div className="flex items-center gap-2">
          <input
            id="ws-sub"
            disabled
            className={cx(inputCls, 'font-mono text-[13px]')}
            value={tenant.sub + '.onclaw.app'}
          />
        </div>
      </div>
      <div className="grid grid-cols-1 gap-4">
        <div>
          <label className={labelCls} htmlFor="ws-tz">
            Timezone
          </label>
          <TimezoneSelect
            id="ws-tz"
            data-od-id="select-ws-tz"
            data-testid="select-ws-tz"
            value={ws.tz}
            disabled={!canWriteWorkspace}
            onChange={(tz) => setWs({ ...ws, tz })}
          />
        </div>
        <div>
          <label className={labelCls} htmlFor="ws-default-provider">
            Default model
          </label>
          <select
            id="ws-default-provider"
            data-od-id="select-ws-default-provider"
            data-testid="select-ws-default-provider"
            className={inputCls}
            disabled={!canWriteWorkspace}
            value={defaultProvider}
            onChange={(e) => {
              // Provider-first (web-app/settings): switching provider
              // re-queries that provider's catalog and drops the old model.
              setDefaultProvider(e.target.value);
              setDefaultModel('');
            }}
          >
            {providers.length === 0 ? (
              <option value="" disabled>
                No providers configured (Settings → Providers)
              </option>
            ) : (
              <option value="">No default — agents pin their own</option>
            )}
            {providers.map((p) => {
              const typeObj = PROVIDER_TYPES.find((t) => t.id === p.type);
              const typeLabel = typeObj ? typeObj.label : p.type;
              return (
                <option key={p.id} value={p.id}>
                  {p.name} ({typeLabel})
                </option>
              );
            })}
          </select>
          {defaultProvider ? (
            <div className="mt-3" data-testid="ws-default-model-picker">
              <ModelCombobox
                key={defaultProvider}
                workspaceId={targetWsId}
                providerId={defaultProvider}
                model={defaultModel}
                onModelChange={setDefaultModel}
                hideEffort
                disabled={!canWriteWorkspace}
              />
            </div>
          ) : (
            <p className="mt-1.5 text-[11px] leading-4 text-muted">
              New agents can inherit this pair instead of pinning a provider and model.
            </p>
          )}
        </div>
      </div>
      <div>
        <label className={labelCls} htmlFor="ws-ret">
          Thread retention
        </label>
        <select
          id="ws-ret"
          className={inputCls}
          disabled={!canWriteWorkspace}
          value={ws.retention}
          onChange={(e) => setWs({ ...ws, retention: e.target.value })}
        >
          {['30 days', '90 days', '1 year', 'Forever'].map((r: any) => (
            <option key={r}>{r}</option>
          ))}
        </select>
      </div>
      <div className="flex items-center justify-between gap-3 pt-1">
        {!canWriteWorkspace && (
          <p className="text-[11px] text-muted">Read-only — an Owner or Admin can edit workspace settings.</p>
        )}
        {canWriteWorkspace && (
          <button
            type="button"
            disabled={savingWs}
            onClick={saveWorkspace}
            data-od-id="btn-workspace-save"
            data-testid="btn-workspace-save"
            className="ml-auto flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-50"
          >
            {savingWs ? 'Saving…' : 'Save workspace'}
          </button>
        )}
      </div>
      <div className="pt-2" data-testid="ws-memory-editor">
        <div className="mb-1.5 flex items-center justify-between gap-3">
          <label className={cx(labelCls, 'mb-0')} htmlFor="ws-memory">
            Shared memory
          </label>
          <Chip mono>WORKSPACE.md</Chip>
        </div>
        <p className="mb-2 text-[12px] leading-5 text-muted">
          Markdown every agent in this workspace reads for shared context.
        </p>
        {memLoading ? (
          <div data-testid="ws-memory-loading" className="py-8 text-center font-mono text-[13px] text-muted">
            Loading memory…
          </div>
        ) : (
          <>
            <textarea
              id="ws-memory"
              data-od-id="textarea-workspace-memory"
              data-testid="textarea-workspace-memory"
              disabled={!canWriteMemory}
              value={memContent}
              onChange={(e) => {
                setMemContent(e.target.value);
                setMemSaved(false);
              }}
              rows={8}
              placeholder="Shared notes, conventions and context for every agent."
              className="w-full resize-y rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] p-3 font-mono text-[12.5px] leading-relaxed text-fg2 placeholder:text-muted focus:border-accent focus:outline-none disabled:cursor-not-allowed disabled:opacity-60"
            />
            <div className="mt-1.5 flex items-center justify-between gap-3">
              {memory ? (
                <p
                  data-testid="ws-memory-counter"
                  className={cx(
                    'font-mono text-[11px]',
                    memContent.length > memory.max_chars ? 'text-danger' : 'text-muted'
                  )}
                >
                  {memContent.length} / {memory.max_chars} chars
                </p>
              ) : (
                <span />
              )}
              <div className="flex items-center gap-3">
                {memError && (
                  <span data-testid="ws-memory-error" className="text-[12px] text-danger" role="alert">
                    {memError}
                  </span>
                )}
                {canWriteMemory && (
                  <>
                    {memSaved && !memError && (
                      <span data-testid="ws-memory-saved" className="text-[12px] font-medium text-fg2">
                        Saved
                      </span>
                    )}
                    <button
                      type="button"
                      disabled={memSaving}
                      onClick={saveMemory}
                      data-od-id="btn-workspace-memory-save"
                      data-testid="btn-workspace-memory-save"
                      className="flex h-8 items-center rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg disabled:opacity-50"
                    >
                      {memSaving ? 'Saving…' : 'Save memory'}
                    </button>
                  </>
                )}
              </div>
            </div>
            {!canWriteMemory && (
              <p className="mt-1 text-[11px] text-muted">Read-only — an Owner or Admin can edit shared memory.</p>
            )}
          </>
        )}
      </div>
      <div className="mt-8 rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] p-4">
        <p className="text-[13px] font-semibold text-fg">Danger zone</p>
        <p className="mt-1 text-[12px] leading-5 text-muted">
          Leave this workspace. You will lose access to its agents, channels and data until reinvited.
        </p>
        <button
          type="button"
          data-od-id="btn-workspace-leave"
          data-testid="btn-workspace-leave"
          disabled={leaving}
          onClick={handleLeave}
          className="mt-3 flex h-8 items-center rounded-md border border-[color-mix(in_oklab,var(--danger)_45%,transparent)] px-3 text-[12px] font-medium text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] disabled:opacity-50"
        >
          {confirmDel ? 'Click again to confirm' : 'Leave workspace'}
        </button>
      </div>
    </div>
  );
}
