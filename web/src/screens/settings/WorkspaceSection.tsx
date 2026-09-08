import { useEffect, useMemo, useState } from "react";
import { cx } from "../../lib/helpers";
import { TimezoneSelect } from "../../components/ui/TimezoneSelect";
import { Chip } from "../../components/ui/Chip";
import { inputCls, labelCls } from "../../components/ui/constants";
import { MODELS } from "../../lib/constants";
import { api, ApiError, formatApiError, type ApiMemory } from "../../lib/api";
import { useAuthStore } from "../../store/auth";
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
    defaultModel: tenant.defaultModel,
    retention: tenant.retention,
  });
  const [confirmDel, setConfirmDel] = useState(false);
  const [savingWs, setSavingWs] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const currentUser = useAuthStore((s) => s.user);
  const memberships = useAuthStore((s) => s.memberships);

  const targetWsId = tenant.sub || tenant.id;

  // Shared-memory (WORKSPACE.md) editor state — fully independent of the
  // workspace-details draft above so neither save rewrites the other.
  const [memory, setMemory] = useState<ApiMemory | null>(null);
  const [memContent, setMemContent] = useState("");
  const [memLoading, setMemLoading] = useState(true);
  const [memSaving, setMemSaving] = useState(false);
  const [memSaved, setMemSaved] = useState(false);
  const [memError, setMemError] = useState<string | null>(null);

  // Settings-management permission: Owner/Admin (workspace.write) persist;
  // Members get the read-only view. Same derivation shape as canWriteSkills.
  const canWriteMemory = useMemo(() => {
    if (!memberships.length) return true; // offline / mock mode — affordance stays
    const mem = memberships.find(
      (m) =>
        m.workspace_id === targetWsId ||
        m.workspace_slug === targetWsId ||
        m.workspace_id === tenant?.id ||
        m.workspace_slug === tenant?.sub
    );
    if (!mem) return false;
    const role = mem.role;
    const roleName = (mem.role_name || role?.name || '').toLowerCase();
    const perms: string[] = role?.permissions || [];
    return (
      role?.is_owner === true ||
      roleName === 'superadmin' ||
      roleName === 'owner' ||
      roleName === 'admin' ||
      perms.some((p) => p === '*' || p === 'workspace.write' || p === 'workspace.*')
    );
  }, [memberships, targetWsId, tenant]);

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
      await api.workspaces.patch(targetWsId, {
        name: ws.name,
        timezone: ws.tz,
      });
      onUpdate((t: any) => ({ ...t, ...ws }));
      useAuthStore.setState((s) => ({
        memberships: s.memberships.map((m) =>
          m.workspace_id === targetWsId || m.workspace_slug === targetWsId
            ? {
                ...m,
                workspace_name: ws.name,
                workspace: m.workspace ? { ...m.workspace, name: ws.name, timezone: ws.tz } : m.workspace,
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
          className={inputCls}
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
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div>
          <label className={labelCls} htmlFor="ws-tz">
            Timezone
          </label>
          <TimezoneSelect
            id="ws-tz"
            data-od-id="select-ws-tz"
            data-testid="select-ws-tz"
            value={ws.tz}
            onChange={(tz) => setWs({ ...ws, tz })}
          />
        </div>
        <div>
          <label className={labelCls} htmlFor="ws-model">
            Default model
          </label>
          <select
            id="ws-model"
            className={inputCls}
            value={ws.defaultModel}
            onChange={(e) => setWs({ ...ws, defaultModel: e.target.value })}
          >
            {MODELS.map((m: any) => (
              <option key={m}>{m}</option>
            ))}
          </select>
        </div>
      </div>
      <div>
        <label className={labelCls} htmlFor="ws-ret">
          Thread retention
        </label>
        <select
          id="ws-ret"
          className={inputCls}
          value={ws.retention}
          onChange={(e) => setWs({ ...ws, retention: e.target.value })}
        >
          {['30 days', '90 days', '1 year', 'Forever'].map((r: any) => (
            <option key={r}>{r}</option>
          ))}
        </select>
      </div>
      <div className="flex justify-end pt-1">
        <button
          type="button"
          disabled={savingWs}
          onClick={saveWorkspace}
          data-od-id="btn-workspace-save"
          data-testid="btn-workspace-save"
          className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-50"
        >
          {savingWs ? 'Saving…' : 'Save workspace'}
        </button>
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
