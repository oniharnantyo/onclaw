import { useState } from "react";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { Icon } from "../components/ui/Icon";
import { cx } from "../lib/helpers";

const rowInputCls =
  'rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 text-[13px] text-fg2 placeholder:text-muted focus:border-accent outline-none font-mono';
import {
  ApiError,
  formatApiError,
  type ApiMcpServer,
  type ApiMcpSecretRowInput,
  type McpServerPayload,
  type McpTransport,
} from "../lib/api";

export interface McpServerDialogProps {
  /** Existing row when editing; null/undefined opens the add form. */
  server?: ApiMcpServer | null;
  /** Sibling names for the client-side duplicate check (case-insensitive). */
  existingServers?: ApiMcpServer[];
  onClose: () => void;
  /** Submits the structured payload: resolve closes the dialog, reject keeps
   * it open with the error inline. The caller owns the endpoint choice —
   * workspace registry and agent-private saves share this dialog. */
  onSave: (payload: McpServerPayload) => Promise<void> | void;
}

const TRANSPORTS: { value: McpTransport; label: string }[] = [
  { value: 'stdio', label: 'stdio — run a local command' },
  { value: 'streamable_http', label: 'streamable_http — Streamable HTTP endpoint' },
  { value: 'sse', label: 'sse — Server-Sent Events endpoint' },
];

// Editable secret row: values are write-only — the input starts and stays
// empty; a stored secret surfaces only as its `value_hint`. An empty value on
// submit keeps the stored secret (name-keyed merge on the server).
interface SecretRowDraft {
  localKey: string;
  name: string;
  value: string;
  hint?: string;
}

let rowSeq = 0;
function nextRowKey(): string {
  rowSeq += 1;
  return `row-${rowSeq}`;
}

function rowsFromServer(rows: ApiMcpServer['env']): SecretRowDraft[] {
  return (rows || []).map((r) => ({
    localKey: nextRowKey(),
    name: r.name,
    value: '',
    hint: r.value_hint || undefined,
  }));
}

interface FieldErrors {
  name?: string;
  command?: string;
  url?: string;
  [key: string]: string | undefined;
}

// McpServerDialog is the structured transport-branched form (design D3/D4):
// one labeled control per property, never a JSON textarea. stdio renders
// command + args + env rows; streamable_http/sse render URL + header rows —
// the transport select reconfigures the form in place.
export function McpServerDialog({
  server,
  existingServers = [],
  onClose,
  onSave,
}: McpServerDialogProps) {
  const isEdit = Boolean(server);
  const [name, setName] = useState(server?.name || '');
  const [transport, setTransport] = useState<McpTransport>(server?.transport || 'stdio');
  const [command, setCommand] = useState(server?.command || '');
  const [argsText, setArgsText] = useState((server?.args || []).join(' '));
  const [envRows, setEnvRows] = useState<SecretRowDraft[]>(() => rowsFromServer(server?.env));
  const [url, setUrl] = useState(server?.url || '');
  const [headerRows, setHeaderRows] = useState<SecretRowDraft[]>(() => rowsFromServer(server?.headers));
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});
  const [generalError, setGeneralError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const isStdio = transport === 'stdio';
  const activeRows = isStdio ? envRows : headerRows;
  const setActiveRows = isStdio ? setEnvRows : setHeaderRows;
  const rowKind = isStdio ? { noun: 'variable', label: 'Environment variables' } : { noun: 'header', label: 'Headers' };

  const updateRow = (localKey: string, patch: Partial<SecretRowDraft>) => {
    setActiveRows((prev) => prev.map((r) => (r.localKey === localKey ? { ...r, ...patch } : r)));
    setRowErrors((prev) => {
      if (!(localKey in prev)) return prev;
      const next = { ...prev };
      delete next[localKey];
      return next;
    });
  };

  const addRow = () => setActiveRows((prev) => [...prev, { localKey: nextRowKey(), name: '', value: '' }]);

  const removeRow = (localKey: string) => {
    setActiveRows((prev) => prev.filter((r) => r.localKey !== localKey));
    setRowErrors((prev) => {
      const next = { ...prev };
      delete next[localKey];
      return next;
    });
  };

  const validate = (): boolean => {
    const errs: FieldErrors = {};
    const trimmedName = name.trim();
    if (!trimmedName) errs.name = 'Server name is required';
    else {
      const lower = trimmedName.toLowerCase();
      const clash = existingServers.some((s) => s.id !== server?.id && s.name.toLowerCase() === lower);
      if (clash) errs.name = 'A server with this name already exists';
    }
    if (isStdio && !command.trim()) errs.command = 'Command is required for stdio';
    if (!isStdio && !url.trim()) errs.url = 'URL is required for this transport';

    const rowErrs: Record<string, string> = {};
    const seen = new Map<string, SecretRowDraft[]>();
    activeRows.forEach((r) => {
      const trimmed = r.name.trim();
      if (!trimmed && !r.value.trim() && !r.hint) return; // dropped silently on submit
      if (!trimmed) {
        rowErrs[r.localKey] = `Name is required`;
        return;
      }
      const group = seen.get(trimmed) ?? [];
      group.push(r);
      seen.set(trimmed, group);
    });
    // Every member of a duplicate-name group is flagged (web.search precedent).
    for (const group of seen.values()) {
      if (group.length > 1) {
        for (const r of group) rowErrs[r.localKey] ??= 'Name must be unique';
      }
    }
    setFieldErrors(errs);
    setRowErrors(rowErrs);
    return Object.keys(errs).length === 0 && Object.keys(rowErrs).length === 0;
  };

  const buildRows = (rows: SecretRowDraft[]): ApiMcpSecretRowInput[] =>
    rows
      .filter((r) => r.name.trim() || r.value.trim() || r.hint)
      .map((r) => {
        const row: ApiMcpSecretRowInput = { name: r.name.trim() };
        // Empty value = keep the stored secret; only a typed value is sent.
        if (r.value.trim() !== '') row.value = r.value.trim();
        return row;
      });

  const buildPayload = (): McpServerPayload => {
    const payload: McpServerPayload = { name: name.trim(), transport };
    if (isStdio) {
      payload.command = command.trim();
      const args = argsText.trim().split(/\s+/).filter(Boolean);
      if (args.length) payload.args = args;
      const env = buildRows(envRows);
      if (env.length) payload.env = env;
    } else {
      payload.url = url.trim();
      const headers = buildRows(headerRows);
      if (headers.length) payload.headers = headers;
    }
    return payload;
  };

  const handleSubmit = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    setGeneralError(null);
    if (!validate()) return;
    setSaving(true);
    try {
      await onSave(buildPayload());
      onClose();
    } catch (err: unknown) {
      if (err instanceof ApiError && err.details && err.details.length > 0) {
        const errs: FieldErrors = {};
        const general: string[] = [];
        for (const d of err.details) {
          if (d.field === 'name' || d.field === 'command' || d.field === 'url') {
            errs[d.field] ??= d.message || 'Invalid value';
          } else {
            general.push(d.message || 'Invalid value');
          }
        }
        setFieldErrors(errs);
        if (general.length) setGeneralError(general.join(' '));
      } else {
        setGeneralError(formatApiError(err, isEdit ? `Failed to update ${server?.name}` : 'Failed to add the server'));
      }
    } finally {
      setSaving(false);
    }
  };

  const submitBtnId = isEdit ? 'btn-mcp-save' : 'btn-mcp-add-confirm';

  return (
    <Modal
      title={isEdit ? `Edit ${server?.name}` : 'Add MCP server'}
      onClose={onClose}
      odId="modal-mcp-server"
      data-testid="modal-mcp-server"
      boxClassName="md:max-w-xl"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-od-id="btn-mcp-cancel"
            data-testid="btn-mcp-cancel"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="submit"
            form="mcp-server-form"
            data-od-id={submitBtnId}
            data-testid={submitBtnId}
            disabled={saving}
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent"
          >
            {saving ? 'Saving…' : isEdit ? 'Save changes' : 'Add'}
          </button>
        </>
      }
    >
      <form id="mcp-server-form" onSubmit={handleSubmit} className="space-y-4 p-5">
        {generalError ? (
          <p className="text-[12px] text-danger" data-testid="mcp-dialog-error">
            {generalError}
          </p>
        ) : null}

        <div>
          <label className={labelCls} htmlFor="mcp-name">
            Server name
          </label>
          <input
            id="mcp-name"
            className={cx(inputCls, fieldErrors.name && 'border-danger')}
            placeholder="Server name — e.g. Sentry"
            value={name}
            aria-label="Server name"
            aria-invalid={Boolean(fieldErrors.name)}
            data-od-id="input-mcp-name"
            data-testid="input-mcp-name"
            autoFocus
            onChange={(e) => setName(e.target.value)}
          />
          {fieldErrors.name ? (
            <p className="mt-1 text-[12px] text-danger" data-testid="mcp-name-error">
              {fieldErrors.name}
            </p>
          ) : null}
        </div>

        <div>
          <label className={labelCls} htmlFor="mcp-transport">
            Transport
          </label>
          <select
            id="mcp-transport"
            className={inputCls}
            value={transport}
            aria-label="Transport"
            data-od-id="input-mcp-transport"
            data-testid="input-mcp-transport"
            onChange={(e) => setTransport(e.target.value as McpTransport)}
          >
            {TRANSPORTS.map((t) => (
              <option key={t.value} value={t.value}>
                {t.label}
              </option>
            ))}
          </select>
        </div>

        {isStdio ? (
          <>
            <div>
              <label className={labelCls} htmlFor="mcp-command">
                Command
              </label>
              <input
                id="mcp-command"
                className={cx(inputCls, 'font-mono text-[13px]', fieldErrors.command && 'border-danger')}
                placeholder="npx"
                value={command}
                aria-label="Command"
                aria-invalid={Boolean(fieldErrors.command)}
                data-od-id="input-mcp-command"
                data-testid="input-mcp-command"
                onChange={(e) => setCommand(e.target.value)}
              />
              {fieldErrors.command ? (
                <p className="mt-1 text-[12px] text-danger" data-testid="mcp-command-error">
                  {fieldErrors.command}
                </p>
              ) : null}
              <p className="mt-1 text-[11px] leading-4 text-muted">
                Runs on the OnClaw server host with the arguments and environment below.
              </p>
            </div>

            <div>
              <label className={labelCls} htmlFor="mcp-args">
                Arguments
              </label>
              <input
                id="mcp-args"
                className={cx(inputCls, 'font-mono text-[13px]')}
                placeholder="-y @modelcontextprotocol/server-github"
                value={argsText}
                aria-label="Arguments"
                data-od-id="input-mcp-args"
                data-testid="input-mcp-args"
                onChange={(e) => setArgsText(e.target.value)}
              />
              <p className="mt-1 text-[11px] leading-4 text-muted">Space-separated, passed to the command in order.</p>
            </div>

            <SecretRowsEditor
              label={rowKind.label}
              noun={rowKind.noun}
              rows={envRows}
              rowErrors={rowErrors}
              onUpdate={updateRow}
              onAdd={addRow}
              onRemove={removeRow}
              nameTestId="input-mcp-env-name"
              valueTestId="input-mcp-env-value"
              rowTestId="mcp-env-row"
              addTestId="btn-mcp-env-add"
              removeTestId="btn-mcp-env-remove"
            />
          </>
        ) : (
          <>
            <div>
              <label className={labelCls} htmlFor="mcp-url">
                URL
              </label>
              <input
                id="mcp-url"
                className={cx(inputCls, 'font-mono text-[13px]', fieldErrors.url && 'border-danger')}
                placeholder="https://mcp.example.com/mcp"
                value={url}
                aria-label="URL"
                aria-invalid={Boolean(fieldErrors.url)}
                data-od-id="input-mcp-url"
                data-testid="input-mcp-url"
                onChange={(e) => setUrl(e.target.value)}
              />
              {fieldErrors.url ? (
                <p className="mt-1 text-[12px] text-danger" data-testid="mcp-url-error">
                  {fieldErrors.url}
                </p>
              ) : null}
            </div>

            <SecretRowsEditor
              label={rowKind.label}
              noun={rowKind.noun}
              rows={headerRows}
              rowErrors={rowErrors}
              onUpdate={updateRow}
              onAdd={addRow}
              onRemove={removeRow}
              nameTestId="input-mcp-header-name"
              valueTestId="input-mcp-header-value"
              rowTestId="mcp-header-row"
              addTestId="btn-mcp-header-add"
              removeTestId="btn-mcp-header-remove"
            />
          </>
        )}
      </form>
    </Modal>
  );
}

interface SecretRowsEditorProps {
  label: string;
  noun: string;
  rows: SecretRowDraft[];
  rowErrors: Record<string, string>;
  onUpdate: (localKey: string, patch: Partial<SecretRowDraft>) => void;
  onAdd: () => void;
  onRemove: (localKey: string) => void;
  nameTestId: string;
  valueTestId: string;
  rowTestId: string;
  addTestId: string;
  removeTestId: string;
}

// Single-line name/value rows (the web.search stack-editor pattern). The value
// input is a password field that never echoes the stored secret — the stored
// hint renders as the placeholder, and an empty submit keeps the secret.
function SecretRowsEditor({
  label,
  noun,
  rows,
  rowErrors,
  onUpdate,
  onAdd,
  onRemove,
  nameTestId,
  valueTestId,
  rowTestId,
  addTestId,
  removeTestId,
}: SecretRowsEditorProps) {
  const capNoun = noun.charAt(0).toUpperCase() + noun.slice(1);
  return (
    <div>
      <div className="flex items-center justify-between">
        <span className="text-[12px] font-medium text-fg2">{label}</span>
        <button
          type="button"
          onClick={onAdd}
          data-od-id={addTestId}
          data-testid={addTestId}
          className="flex h-7 items-center gap-1 rounded-md border border-line px-2 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
        >
          <Icon name="plus" size={13} /> Add {noun}
        </button>
      </div>

      {rows.length === 0 ? (
        <p className="mt-2 rounded-md border border-dashed border-line px-3 py-3 text-center text-[12px] text-muted">
          None configured.
        </p>
      ) : (
        <div className="mt-2 space-y-2">
          {rows.map((row) => (
            <div key={row.localKey} data-testid={rowTestId}>
              <div className="flex items-center gap-2">
                <input
                  aria-label={`${capNoun} name`}
                  placeholder="NAME"
                  className={cx(rowInputCls, 'h-8 w-28 sm:w-40 shrink-0', rowErrors[row.localKey] && 'border-danger')}
                  value={row.name}
                  data-od-id={nameTestId}
                  data-testid={nameTestId}
                  onChange={(e) => onUpdate(row.localKey, { name: e.target.value })}
                />
                <input
                  aria-label={`${capNoun} value`}
                  type="password"
                  placeholder={row.hint ? `•••• ${row.hint}` : 'Value'}
                  title={row.hint ? `Stored — leave empty to keep •••• ${row.hint}` : undefined}
                  className={cx(rowInputCls, 'h-8 min-w-0 flex-1')}
                  value={row.value}
                  data-od-id={valueTestId}
                  data-testid={valueTestId}
                  onChange={(e) => onUpdate(row.localKey, { value: e.target.value })}
                />
                <button
                  type="button"
                  aria-label={`Remove ${noun}`}
                  data-od-id={removeTestId}
                  data-testid={removeTestId}
                  onClick={() => onRemove(row.localKey)}
                  className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger"
                >
                  <Icon name="x" size={13} />
                </button>
              </div>
              {rowErrors[row.localKey] ? (
                <p className="mt-1 text-[12px] text-danger" data-testid="mcp-row-error">
                  {rowErrors[row.localKey]}
                </p>
              ) : null}
            </div>
          ))}
          <p className="text-[11px] leading-4 text-muted">
            Values are stored encrypted and never shown again — leave a value empty to keep the stored one.
          </p>
        </div>
      )}
    </div>
  );
}
