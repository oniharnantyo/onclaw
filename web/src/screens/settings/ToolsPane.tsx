import { useState, useEffect, useRef } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Toggle } from "../../components/ui/Toggle";
import { Modal } from "../../components/ui/Modal";
import { inputCls, labelCls } from "../../components/ui/constants";
import { ErrorState } from "../../components/ErrorState";
import { api, formatApiError, ApiError, type ApiToolSettings, type ApiToolConfigField } from "../../lib/api";
import {
  SEARCH_PROVIDERS,
  SEARCH_ROTATION_WINDOW,
  WEB_SEARCH_TOOL_KEY,
  blankSearchDraft,
  searchConfigPayload,
  searchCredentialKind,
  searchDraftsFromConfig,
  searchEntryCount,
  searchProviderOption,
  searchTimeoutFromConfig,
  validateSearchDrafts,
  type SearchEntryDraft,
} from "../../lib/toolCatalog";
import serverErrorSvg from "../../assets/server-error.svg";

export interface ToolsPaneProps {
  tenant: any;
  onToast?: (text: string, kind?: string) => void;
  onUpdate?: (fn: any) => void;
}

interface FieldErrors {
  [field: string]: string;
}

export function ToolsPane({ tenant, onToast = () => {} }: ToolsPaneProps) {
  const [tools, setTools] = useState<ApiToolSettings[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);
  const [configTool, setConfigTool] = useState<ApiToolSettings | null>(null);
  const [saving, setSaving] = useState(false);

  const targetWsId = tenant.sub || tenant.id;

  const loadTools = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const res = await api.tools.list(targetWsId);
      setTools(res.tools || []);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 0) {
        return;
      }
      setLoadError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadTools();
  }, [targetWsId]);

  const replaceTool = (updated: ApiToolSettings) => {
    setTools((prev) => prev.map((t) => (t.key === updated.key ? updated : t)));
  };

  const handleToggle = async (tool: ApiToolSettings) => {
    const nextEnabled = !tool.enabled;
    try {
      const res = await api.tools.update(targetWsId, tool.key, { enabled: nextEnabled });
      if (res?.tool) replaceTool(res.tool);
      onToast(`${nextEnabled ? 'Enabled' : 'Disabled'} ${tool.display_name}`);
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to update ${tool.display_name}`), 'danger');
    }
  };

  const handleConfigSaved = (updated: ApiToolSettings) => {
    replaceTool(updated);
    setConfigTool(null);
    onToast(`Saved ${updated.display_name} configuration`);
  };

  return (
    <div data-od-id="pane-tools" data-testid="pane-tools">
      <div className="mb-4 flex items-center justify-between">
        <div>
          <h3 className="text-[15px] font-semibold text-fg">Tools</h3>
          <p className="mt-0.5 text-[12px] text-muted">
            The workspace-wide tool status. Disabled tools are removed from every agent in this workspace.
          </p>
        </div>
      </div>

      {loading && tools.length === 0 ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-16 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
          ))}
        </div>
      ) : loadError ? (
        <div className="py-6">
          <ErrorState
            illustration={serverErrorSvg}
            title="Couldn't load tools"
            detail={loadError.message}
            primaryAction={{ label: 'Retry', onClick: loadTools }}
          />
        </div>
      ) : (
        <div className="space-y-3">
          {tools.map((tool) => (
            <div
              key={tool.key}
              className={cx(
                'flex items-center gap-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] p-3.5 transition-colors',
                !tool.enabled && 'opacity-70'
              )}
              data-od-id={'tool-' + tool.key}
              data-testid={'tool-' + tool.key}
            >
              <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent">
                <Icon name={tool.icon_key || 'plug'} size={16} />
              </span>

              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="truncate text-[14px] font-medium text-fg">{tool.display_name}</p>
                  {tool.key === WEB_SEARCH_TOOL_KEY && searchEntryCount(tool.config) > 0 ? (
                    <span className="font-mono text-[11px] text-muted" data-testid="web-search-summary">
                      {searchEntryCount(tool.config)} configured · {Math.min(SEARCH_ROTATION_WINDOW, searchEntryCount(tool.config))} stacked
                    </span>
                  ) : tool.configurable && !tool.configured ? (
                    <span className="text-[11px] text-[color-mix(in_oklab,var(--warn),black_38%)]">
                      Not configured — enable after setup
                    </span>
                  ) : null}
                </div>
                <p className="truncate text-[12px] text-muted">{tool.description}</p>
              </div>

              <div className="flex items-center gap-2">
                {tool.configurable ? (
                  <button
                    type="button"
                    aria-label={'Configure ' + tool.display_name}
                    data-od-id={'btn-tool-config-' + tool.key}
                    data-testid={'btn-tool-config-' + tool.key}
                    onClick={() => setConfigTool(tool)}
                    className="flex h-8 w-8 items-center justify-center rounded-md border border-line text-muted transition-colors hover:border-accent hover:text-fg"
                  >
                    <Icon name="cog" size={15} />
                  </button>
                ) : null}
                <Toggle
                  on={tool.enabled}
                  label={'Enable ' + tool.display_name}
                  onChange={() => handleToggle(tool)}
                />
              </div>
            </div>
          ))}
        </div>
      )}

      {configTool ? (
        configTool.key === WEB_SEARCH_TOOL_KEY ? (
          <WebSearchConfigDialog
            tool={configTool}
            wsId={targetWsId}
            saving={saving}
            onClose={() => setConfigTool(null)}
            onSaved={handleConfigSaved}
            onSaving={setSaving}
            onToast={onToast}
          />
        ) : (
          <ToolConfigDialog
            tool={configTool}
            wsId={targetWsId}
            saving={saving}
            onClose={() => setConfigTool(null)}
            onSaved={handleConfigSaved}
            onSaving={setSaving}
            onToast={onToast}
          />
        )
      ) : null}
    </div>
  );
}

interface ToolConfigDialogProps {
  tool: ApiToolSettings;
  wsId: string;
  saving: boolean;
  onClose: () => void;
  onSaved: (tool: ApiToolSettings) => void;
  onSaving: (saving: boolean) => void;
  onToast: (text: string, kind?: string) => void;
}

// ToolConfigDialog renders one labeled input per ConfigField — never a raw
// JSON textarea. Secret fields are write-only: empty keeps the stored
// credential, and the stored hint is shown beside the input.
export function ToolConfigDialog({ tool, wsId, saving, onClose, onSaved, onSaving, onToast = () => {} }: ToolConfigDialogProps) {
  const [values, setValues] = useState<Record<string, string>>({});
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [generalError, setGeneralError] = useState<string | null>(null);

  const schema = tool.config_schema || [];

  // Fields with show_if render only while the controlling sibling field holds
  // the required value — evaluated against live form state (falling back to
  // stored config), so conditional fields appear/disappear as the user edits.
  const visibleFields = schema.filter((field) => {
    if (!field.show_if) return true;
    const current = values[field.show_if.field] ?? defaultString(tool.config?.[field.show_if.field]);
    return current === field.show_if.equals;
  });

  const secretHint = (field: ApiToolConfigField): string | null => {
    const value = tool.config?.[field.key];
    if (value && typeof value === 'object' && 'hint' in (value as Record<string, unknown>)) {
      const hint = (value as Record<string, unknown>).hint;
      return hint ? String(hint) : null;
    }
    return null;
  };

  const handleSubmit = async () => {
    setFieldErrors({});
    setGeneralError(null);
    onSaving(true);
    const config: Record<string, unknown> = {};
    for (const field of visibleFields) {
      const raw = values[field.key];
      if (raw === undefined) continue;
      if (field.type === 'secret' && raw === '') continue; // empty keeps stored credential
      if (raw === '') continue;
      if (field.type === 'number') {
        const n = Number(raw);
        if (Number.isNaN(n)) {
          setFieldErrors((prev) => ({ ...prev, [field.key]: `${field.label} must be a number` }));
          onSaving(false);
          return;
        }
        config[field.key] = n;
      } else {
        config[field.key] = raw;
      }
    }
    try {
      const res = await api.tools.update(wsId, tool.key, { config });
      if (res?.tool) onSaved(res.tool);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.details.length > 0) {
        const errs: FieldErrors = {};
        for (const d of err.details) {
          if (d.field) errs[d.field] = d.message || 'Invalid value';
        }
        setFieldErrors(errs);
      } else {
        const msg = formatApiError(err, `Failed to save ${tool.display_name} configuration`);
        setGeneralError(msg);
        onToast(msg, 'danger');
      }
    } finally {
      onSaving(false);
    }
  };

  return (
    <Modal
      title={`${tool.display_name} configuration`}
      onClose={onClose}
      data-testid="tool-config-dialog"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            className="h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={saving}
            data-testid="btn-tool-config-save"
            onClick={handleSubmit}
            className="h-8 rounded-md bg-accent px-3 text-[12px] font-medium text-accenton transition-opacity hover:opacity-90 disabled:opacity-50"
          >
            {saving ? 'Saving…' : 'Save configuration'}
          </button>
        </>
      }
    >
      <div className="space-y-4 p-5">
        <p className="text-[12px] text-muted">{tool.description}</p>
        {generalError ? (
          <p className="text-[12px] text-danger" data-testid="tool-config-error">{generalError}</p>
        ) : null}

        {visibleFields.map((field) => {
          const hint = secretHint(field);
          const stored = hint ? `••••${hint}` : null;
          return (
            <div key={field.key} data-testid={'tool-config-field-' + field.key}>
              <label className={labelCls} htmlFor={'tool-field-' + field.key}>
                {field.label}{field.required ? ' *' : ''}
              </label>
              {field.type === 'enum' ? (
                <select
                  id={'tool-field-' + field.key}
                  className={inputCls}
                  value={values[field.key] ?? defaultString(tool.config?.[field.key])}
                  onChange={(e) => setValues((prev) => ({ ...prev, [field.key]: e.target.value }))}
                >
                  <option value="">Select…</option>
                  {(field.options || []).map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </select>
              ) : field.type === 'boolean' ? (
                <select
                  id={'tool-field-' + field.key}
                  className={inputCls}
                  value={values[field.key] ?? String(tool.config?.[field.key] ?? '')}
                  onChange={(e) => setValues((prev) => ({ ...prev, [field.key]: e.target.value }))}
                >
                  <option value="">Default</option>
                  <option value="true">Enabled</option>
                  <option value="false">Disabled</option>
                </select>
              ) : (
                <div className="flex items-center gap-2">
                  <input
                    id={'tool-field-' + field.key}
                    type={field.type === 'secret' ? 'password' : field.type === 'number' ? 'number' : 'text'}
                    className={cx(inputCls, fieldErrors[field.key] && 'border-danger')}
                    placeholder={stored ? 'Leave empty to keep ••••' + hint : undefined}
                    value={values[field.key] ?? ''}
                    onChange={(e) => setValues((prev) => ({ ...prev, [field.key]: e.target.value }))}
                  />
                  {stored && field.type === 'secret' ? (
                    <span className="whitespace-nowrap font-mono text-[11px] text-muted">{stored}</span>
                  ) : null}
                </div>
              )}
              {fieldErrors[field.key] ? (
                <p className="mt-1 text-[12px] text-danger" data-testid={'tool-config-field-error-' + field.key}>
                  {fieldErrors[field.key]}
                </p>
              ) : null}
              {field.help ? <p className="mt-1 text-[11px] leading-4 text-muted">{field.help}</p> : null}
            </div>
          );
        })}
      </div>
    </Modal>
  );
}

// Map a server 422 validation detail onto the offending draft row: the detail
// field names the entry by index path ("entries.1.api_key") or, failing that,
// by entry id. Unidentifiable details fall through to the dialog level.
function mapDetailToDraft(field: string | undefined, drafts: SearchEntryDraft[]): SearchEntryDraft | null {
  if (!field) return null;
  const m = /entries\[?\.?(\d+)/.exec(field);
  if (m) {
    const idx = Number(m[1]);
    return drafts[idx] ?? null;
  }
  return drafts.find((d) => d.id && field.includes(d.id)) ?? null;
}

interface WebSearchConfigDialogProps {
  tool: ApiToolSettings;
  wsId: string;
  saving: boolean;
  onClose: () => void;
  onSaved: (tool: ApiToolSettings) => void;
  onSaving: (saving: boolean) => void;
  onToast: (text: string, kind?: string) => void;
}

// WebSearchConfigDialog is the provider-stack list editor (design D10) that
// replaces the flat form for web.search only. Rows carry their own reorder
// pair, name, provider select, and per-provider credential; nothing persists
// until Save, which submits the whole ordered list with entry ids — a known
// id with an empty key field keeps the stored secret.
export function WebSearchConfigDialog({ tool, wsId, saving, onClose, onSaved, onSaving, onToast = () => {} }: WebSearchConfigDialogProps) {
  const [drafts, setDrafts] = useState<SearchEntryDraft[]>(() => searchDraftsFromConfig(tool.config));
  const [timeoutText, setTimeoutText] = useState<string>(() => searchTimeoutFromConfig(tool.config));
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});
  const [timeoutError, setTimeoutError] = useState<string | null>(null);
  const [generalError, setGeneralError] = useState<string | null>(null);
  const [pendingRemoval, setPendingRemoval] = useState<{ draft: SearchEntryDraft; index: number } | null>(null);
  const undoTimer = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (undoTimer.current !== null) window.clearTimeout(undoTimer.current);
    };
  }, []);

  const clearUndoTimer = () => {
    if (undoTimer.current !== null) {
      window.clearTimeout(undoTimer.current);
      undoTimer.current = null;
    }
  };

  const updateDraft = (localKey: string, patch: Partial<SearchEntryDraft>) => {
    setDrafts((prev) => prev.map((d) => (d.localKey === localKey ? { ...d, ...patch } : d)));
    setRowErrors((prev) => {
      if (!(localKey in prev)) return prev;
      const next = { ...prev };
      delete next[localKey];
      return next;
    });
  };

  const move = (index: number, dir: -1 | 1) => {
    setDrafts((prev) => {
      const j = index + dir;
      if (j < 0 || j >= prev.length) return prev;
      const next = [...prev];
      [next[index], next[j]] = [next[j], next[index]];
      return next;
    });
  };

  const removeRow = (index: number) => {
    const removed = drafts[index];
    if (!removed) return;
    clearUndoTimer();
    setDrafts((prev) => prev.filter((_, i) => i !== index));
    setRowErrors((prev) => {
      const next = { ...prev };
      delete next[removed.localKey];
      return next;
    });
    setPendingRemoval({ draft: removed, index });
    undoTimer.current = window.setTimeout(() => {
      setPendingRemoval(null);
      undoTimer.current = null;
    }, 5000);
  };

  const undoRemove = () => {
    if (!pendingRemoval) return;
    clearUndoTimer();
    setDrafts((prev) => {
      const next = [...prev];
      next.splice(Math.min(pendingRemoval.index, next.length), 0, pendingRemoval.draft);
      return next;
    });
    setPendingRemoval(null);
  };

  const handleSubmit = async () => {
    setGeneralError(null);
    const errors = validateSearchDrafts(drafts, timeoutText);
    setRowErrors(errors.rows);
    setTimeoutError(errors.timeout);
    if (Object.keys(errors.rows).length > 0 || errors.timeout) return;
    onSaving(true);
    try {
      const res = await api.tools.update(wsId, tool.key, { config: searchConfigPayload(drafts, timeoutText) });
      if (res?.tool) onSaved(res.tool);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.details.length > 0) {
        const nextRows: Record<string, string> = {};
        let dialogLevel: string | null = null;
        for (const d of err.details) {
          const draft = mapDetailToDraft(d.field, drafts);
          if (draft) {
            nextRows[draft.localKey] = d.message || 'Invalid value';
          } else if (d.field === 'request_timeout_seconds') {
            setTimeoutError(d.message || 'Invalid timeout');
          } else {
            dialogLevel = dialogLevel ? `${dialogLevel} ${d.message || ''}`.trim() : d.message || 'Invalid value';
          }
        }
        setRowErrors(nextRows);
        if (dialogLevel) setGeneralError(dialogLevel);
      } else {
        const msg = formatApiError(err, `Failed to save ${tool.display_name} configuration`);
        setGeneralError(msg);
        onToast(msg, 'danger');
      }
    } finally {
      onSaving(false);
    }
  };

  return (
    <Modal
      title={`${tool.display_name} configuration`}
      onClose={onClose}
      boxClassName="md:max-w-xl"
      data-testid="tool-config-dialog"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            className="h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={saving}
            data-testid="btn-tool-config-save"
            onClick={handleSubmit}
            className="h-8 rounded-md bg-accent px-3 text-[12px] font-medium text-accenton transition-opacity hover:opacity-90 disabled:opacity-50"
          >
            {saving ? 'Saving…' : 'Save configuration'}
          </button>
        </>
      }
    >
      <div className="space-y-4 p-5">
        <p className="text-[12px] text-muted">{tool.description}</p>
        {generalError ? (
          <p className="text-[12px] text-danger" data-testid="tool-config-error">{generalError}</p>
        ) : null}

        <div>
          <div className="flex items-center justify-between">
            <span className="text-[12px] font-medium text-fg2">Provider stack</span>
            <button
              type="button"
              data-testid="btn-web-search-add"
              onClick={() => setDrafts((prev) => [...prev, blankSearchDraft()])}
              className="flex h-7 items-center gap-1 rounded-md border border-line px-2 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              <Icon name="plus" size={13} /> Add
            </button>
          </div>
          <p className="mt-1 text-[11px] leading-4 text-muted">
            Requests try the first three in order — first success wins.
          </p>
          <p className="text-[11px] leading-4 text-muted">
            Lower entries stand by until promoted into the top three.
          </p>
        </div>

        {drafts.length === 0 ? (
          <p
            className="rounded-md border border-dashed border-line px-3 py-4 text-center text-[12px] text-muted"
            data-testid="web-search-empty"
          >
            No providers configured — web.search will error until you add one.
          </p>
        ) : (
          <div className="space-y-2.5">
            {drafts.map((draft, index) => {
              const inRotation = index < SEARCH_ROTATION_WINDOW;
              const kind = searchCredentialKind(draft.provider);
              return (
                <div
                  key={draft.localKey}
                  className={cx(
                    'rounded-lg border border-line bg-[color-mix(in_oklab,var(--fg)_2%,var(--surface))] p-3 transition-colors',
                    !inRotation && 'opacity-60'
                  )}
                  data-testid="web-search-row"
                >
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-2">
                    <div className="flex shrink-0 overflow-hidden rounded-md border border-line">
                      <button
                        type="button"
                        aria-label="Move up"
                        disabled={index === 0}
                        onClick={() => move(index, -1)}
                        className="flex h-6 w-6 items-center justify-center text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg disabled:pointer-events-none disabled:opacity-35"
                      >
                        <Icon name="up" size={12} />
                      </button>
                      <span aria-hidden className="w-px self-stretch bg-line" />
                      <button
                        type="button"
                        aria-label="Move down"
                        disabled={index === drafts.length - 1}
                        onClick={() => move(index, 1)}
                        className="flex h-6 w-6 items-center justify-center text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg disabled:pointer-events-none disabled:opacity-35"
                      >
                        <Icon name="down" size={12} />
                      </button>
                    </div>
                    <span
                      className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] font-mono text-[10px] leading-none text-muted"
                      data-testid="web-search-row-number"
                    >
                      {index + 1}
                    </span>
                    <input
                      aria-label="Name"
                      placeholder="Name"
                      className={cx(inputCls, 'h-8 min-w-0 flex-1 basis-[150px] text-[13px]', rowErrors[draft.localKey] && 'border-danger')}
                      value={draft.name}
                      onChange={(e) => updateDraft(draft.localKey, { name: e.target.value })}
                    />
                    <select
                      aria-label="Provider"
                      className={cx(inputCls, 'h-8 w-auto shrink-0 grow-0 basis-[128px] text-[13px]')}
                      value={draft.provider}
                      onChange={(e) => updateDraft(draft.localKey, { provider: e.target.value })}
                    >
                      <option value="">Select…</option>
                      {SEARCH_PROVIDERS.map((p) => (
                        <option key={p.value} value={p.value}>{p.label}</option>
                      ))}
                    </select>
                    <div className="ml-auto flex items-center gap-1.5">
                      <span
                        className={cx(
                          'whitespace-nowrap rounded-full px-2 py-0.5 text-[10px] font-medium leading-4',
                          inRotation
                            ? 'bg-[color-mix(in_oklab,var(--accent)_12%,transparent)] text-accenttext'
                            : 'bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted'
                        )}
                        data-testid="web-search-rotation"
                      >
                        {inRotation ? 'in rotation' : '(standby)'}
                      </span>
                      <button
                        type="button"
                        aria-label="Remove entry"
                        data-testid="btn-web-search-remove"
                        onClick={() => removeRow(index)}
                        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger"
                      >
                        <Icon name="x" size={13} />
                      </button>
                    </div>
                  </div>

                  {draft.provider !== '' ? (
                    <div className="mt-2 flex items-center gap-2 pl-1 md:pl-[86px]">
                      <span
                        aria-hidden
                        className="hidden select-none font-mono text-[12px] leading-none text-[color-mix(in_oklab,var(--fg)_24%,transparent)] md:inline"
                      >
                        └─
                      </span>
                      <label className="w-14 shrink-0 text-[11px] text-muted" htmlFor={'ws-cred-' + draft.localKey}>
                        {kind === 'base_url' ? 'Base URL' : 'API key'}
                      </label>
                      <input
                        id={'ws-cred-' + draft.localKey}
                        aria-label={kind === 'base_url' ? 'Base URL' : 'API key'}
                        type={kind === 'base_url' ? 'text' : 'password'}
                        placeholder={kind === 'base_url' ? undefined : draft.hint ? `•••• ${draft.hint}` : undefined}
                        className={cx(inputCls, 'h-8 flex-1 font-mono text-[13px]')}
                        value={kind === 'base_url' ? draft.baseUrl : draft.apiKey}
                        onChange={(e) =>
                          updateDraft(draft.localKey, kind === 'base_url' ? { baseUrl: e.target.value } : { apiKey: e.target.value })
                        }
                      />
                    </div>
                  ) : null}

                  {rowErrors[draft.localKey] ? (
                    <p className="mt-1.5 flex items-center gap-1 text-[12px] text-danger" data-testid="web-search-row-error">
                      <Icon name="alert" size={12} /> {rowErrors[draft.localKey]}
                    </p>
                  ) : null}
                </div>
              );
            })}
          </div>
        )}

        {pendingRemoval ? (
          <div
            className="flex items-center gap-2.5 rounded-md border border-line bg-warm px-3.5 py-2.5"
            data-testid="web-search-undo-bar"
            role="status"
          >
            <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] text-danger">
              <Icon name="x" size={12} />
            </span>
            <p className="flex-1 text-[12px] text-fg2">
              Removed {pendingRemoval.draft.name || searchProviderOption(pendingRemoval.draft.provider)?.label || 'provider entry'}
            </p>
            <button
              type="button"
              data-testid="web-search-undo"
              onClick={undoRemove}
              className="h-7 rounded-md border border-line px-2.5 text-[12px] font-medium text-accenttext transition-colors hover:border-accent"
            >
              Undo
            </button>
          </div>
        ) : null}

        <div data-testid="web-search-timeout-field">
          <label className={labelCls} htmlFor="web-search-timeout">Request timeout (per attempt)</label>
          <div className="relative w-24">
            <input
              id="web-search-timeout"
              type="number"
              min={1}
              max={60}
              className={cx(inputCls, 'pr-8', timeoutError && 'border-danger')}
              value={timeoutText}
              onChange={(e) => {
                setTimeoutText(e.target.value);
                if (timeoutError) setTimeoutError(null);
              }}
            />
            <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[12px] text-muted">s</span>
          </div>
          {timeoutError ? (
            <p className="mt-1 text-[12px] text-danger" data-testid="web-search-timeout-error">{timeoutError}</p>
          ) : null}
          <p className="mt-1 text-[11px] leading-4 text-muted">Bounds each attempt — worst case ≈ 3 × timeout.</p>
        </div>
      </div>
    </Modal>
  );
}

function defaultString(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'object') return '';
  return String(value);
}
