import { useState, useEffect } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Toggle } from "../../components/ui/Toggle";
import { Modal } from "../../components/ui/Modal";
import { inputCls, labelCls } from "../../components/ui/constants";
import { ErrorState } from "../../components/ErrorState";
import { api, formatApiError, ApiError, type ApiToolSettings, type ApiToolConfigField } from "../../lib/api";
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
                  {tool.configurable && !tool.configured ? (
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
        <ToolConfigDialog
          tool={configTool}
          wsId={targetWsId}
          saving={saving}
          onClose={() => setConfigTool(null)}
          onSaved={handleConfigSaved}
          onSaving={setSaving}
          onToast={onToast}
        />
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
    for (const field of schema) {
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
            className="h-8 rounded-md bg-accent px-3 text-[12px] font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-50"
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

        {schema.map((field) => {
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

function defaultString(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'object') return '';
  return String(value);
}
