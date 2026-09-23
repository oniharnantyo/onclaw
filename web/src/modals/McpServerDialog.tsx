import { useEffect, useRef, useState } from "react";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { Icon } from "../components/ui/Icon";
import { cx } from "../lib/helpers";
import {
  ApiError,
  formatApiError,
  type ApiMcpDeviceBegin,
  type ApiMcpServer,
  type ApiMcpSecretRowInput,
  type McpAuthMode,
  type McpOAuthFlowApi,
  type McpServerPayload,
  type McpTransport,
} from "../lib/api";

const rowInputCls =
  'rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 text-[13px] text-fg2 placeholder:text-muted focus:border-accent outline-none font-mono';

export interface McpServerDialogProps {
  /** Existing row when editing; null/undefined opens the add form. */
  server?: ApiMcpServer | null;
  /** Sibling names for the client-side duplicate check (case-insensitive). */
  existingServers?: ApiMcpServer[];
  /** Scope-bound OAuth flow adapters (add-mcp-oauth-client 7.2): the caller
   * owns the endpoint choice exactly like onSave — the workspace pane passes
   * the api.mcp fns, an agent-private surface passes the api.agents mirrors.
   * Absent when the caller has no OAuth surface; saving an oauth-mode server
   * then closes the dialog and sign-in happens from the rows later. */
  oauthApi?: McpOAuthFlowApi;
  /** Called when a flow mutates the row server-side (device completion) so
   * the caller can refresh its list. */
  onAuthorized?: () => void;
  onClose: () => void;
  /** Submits the structured payload: resolve closes the dialog (or, for an
   * oauth-mode server with `oauthApi` wired, hands the saved row to the
   * sign-in step), reject keeps it open with the error inline. The caller
   * owns the endpoint choice — workspace registry and agent-private saves
   * share this dialog. */
  onSave: (payload: McpServerPayload) => Promise<ApiMcpServer | void> | void;
}

const TRANSPORTS: { value: McpTransport; label: string }[] = [
  { value: 'stdio', label: 'stdio — run a local command' },
  { value: 'streamable_http', label: 'streamable_http — Streamable HTTP endpoint' },
  { value: 'sse', label: 'sse — Server-Sent Events endpoint' },
];

const AUTH_MODES: { value: McpAuthMode; label: string }[] = [
  { value: 'none', label: 'none — static header rows only' },
  { value: 'oauth', label: 'oauth — sign in with the provider' },
];

// Launch presets (add-mcp-oauth-client 7.3): pre-filled templates that
// complete registration and authorization with no instance-admin
// configuration.
interface McpPreset {
  id: string;
  label: string;
  name: string;
  url: string;
  help: string;
}

const PRESETS: McpPreset[] = [
  {
    id: 'notion',
    label: 'Notion',
    name: 'Notion',
    url: 'https://mcp.notion.com/mcp',
    help: 'Connects with zero admin registration (dynamic client registration)',
  },
  {
    id: 'sentry',
    label: 'Sentry',
    name: 'Sentry',
    url: 'https://mcp.sentry.dev/mcp/{org}',
    help: 'OAuth sign-in; create the URL with your organization slug',
  },
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

// Device paste-back state (RFC 8628): the begin response plus the live poll
// round. Each poll re-arms the timer with a fresh object; a terminal outcome
// stops the loop.
interface DeviceFlowState {
  begin: ApiMcpDeviceBegin;
  interval: number;
  outcome: null | { status: 'completed' | 'expired' | 'denied' | 'error'; detail?: string };
}

// McpServerDialog is the structured transport-branched form (design D3/D4):
// one labeled control per property, never a JSON textarea. stdio renders
// command + args + env rows; streamable_http/sse render URL + header rows —
// the transport select reconfigures the form in place. URL transports add the
// auth mode (add-mcp-oauth-client 7.2): `oauth` reveals the BYO client rows
// and, after a save, the sign-in step (browser hand-off or the RFC 8628
// device paste-back screen).
export function McpServerDialog({
  server,
  existingServers = [],
  oauthApi,
  onAuthorized,
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
  const [authMode, setAuthMode] = useState<McpAuthMode>(server?.auth_mode === 'oauth' ? 'oauth' : 'none');
  const [oauthClientId, setOauthClientId] = useState(server?.oauth_client_id || '');
  const [oauthClientSecret, setOauthClientSecret] = useState('');
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});
  const [generalError, setGeneralError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  // OAuth sign-in step (post-save): the saved row the flows run against.
  const [flowServer, setFlowServer] = useState<ApiMcpServer | null>(null);
  const [deviceFlow, setDeviceFlow] = useState<DeviceFlowState | null>(null);
  const [flowError, setFlowError] = useState<string | null>(null);
  const [flowBusy, setFlowBusy] = useState(false);
  const [copied, setCopied] = useState(false);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

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
      if (authMode === 'oauth') {
        payload.auth_mode = 'oauth';
        // Write-only BYO rows: an empty secret keeps the stored one.
        if (oauthClientId.trim()) payload.oauth_client_id = oauthClientId.trim();
        if (oauthClientSecret.trim()) payload.oauth_client_secret = oauthClientSecret.trim();
      } else if (isEdit && server?.auth_mode === 'oauth') {
        // Switching an oauth row off: the explicit non-oauth mode clears the
        // stored BYO client server-side.
        payload.auth_mode = 'none';
      }
    }
    return payload;
  };

  const applyPreset = (p: McpPreset) => {
    if (!name.trim()) setName(p.name);
    setTransport('streamable_http');
    setUrl(p.url);
    setAuthMode('oauth');
    setGeneralError(null);
  };

  const startBrowserSignIn = async () => {
    if (!flowServer || !oauthApi) return;
    setFlowBusy(true);
    setFlowError(null);
    try {
      const res = await oauthApi.authorize(flowServer.id);
      if (res?.authorize_url) {
        // Top-level navigation, like the connections consent hand-off — the
        // sealed PKCE session rides an HttpOnly cookie through the provider
        // round trip, and the callback bounces back to the settings pane.
        window.location.assign(res.authorize_url);
        return;
      }
      setFlowError('The provider did not return an authorization URL');
    } catch (err: unknown) {
      if (err instanceof ApiError && /public base URL/.test(err.message || '')) {
        // Headless instance: the browser flow has no redirect target — fall
        // straight into the device flow.
        await startDeviceFlow();
        return;
      }
      setFlowError(formatApiError(err, 'Failed to start sign-in'));
    } finally {
      setFlowBusy(false);
    }
  };

  const startDeviceFlow = async () => {
    if (!flowServer || !oauthApi) return;
    setFlowBusy(true);
    setFlowError(null);
    try {
      const begin = await oauthApi.beginDevice(flowServer.id);
      setDeviceFlow({ begin, interval: begin.interval ?? 5, outcome: null });
    } catch (err: unknown) {
      setFlowError(formatApiError(err, 'The device flow could not be started'));
    } finally {
      setFlowBusy(false);
    }
  };

  // RFC 8628 §3.4: poll on the returned interval; slow_down backs it off.
  useEffect(() => {
    if (!deviceFlow || deviceFlow.outcome || !oauthApi || !flowServer) return;
    const serverId = flowServer.id;
    const timer = setTimeout(() => {
      oauthApi
        .pollDevice(serverId, deviceFlow.begin.device_session)
        .then((res) => {
          setDeviceFlow((prev) => {
            if (!prev || prev.outcome) return prev;
            if (res.status === 'pending')
              return { ...prev, outcome: null };
            if (res.status === 'slow_down')
              return { ...prev, interval: prev.interval + 5 };
            if (res.status === 'completed')
              return { ...prev, outcome: { status: 'completed' as const } };
            if (res.status === 'expired')
              return { ...prev, outcome: { status: 'expired' as const } };
            return { ...prev, outcome: { status: 'denied' as const, detail: res.detail } };
          });
        })
        .catch((err: unknown) => {
          setDeviceFlow((prev) =>
            prev && !prev.outcome
              ? {
                  ...prev,
                  outcome: {
                    status: 'error' as const,
                    detail: formatApiError(err, 'The device flow could not be checked'),
                  },
                }
              : prev
          );
        });
    }, deviceFlow.interval * 1000);
    return () => clearTimeout(timer);
  }, [deviceFlow, oauthApi, flowServer]);

  useEffect(() => {
    if (deviceFlow?.outcome?.status === 'completed') onAuthorized?.();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- one refresh per completion
  }, [deviceFlow]);

  useEffect(() => {
    return () => {
      if (copiedTimer.current) clearTimeout(copiedTimer.current);
    };
  }, []);

  const copyUserCode = () => {
    if (!deviceFlow) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(deviceFlow.begin.user_code).then(
        () => {
          setCopied(true);
          if (copiedTimer.current) clearTimeout(copiedTimer.current);
          copiedTimer.current = setTimeout(() => setCopied(false), 2000);
        },
        () => setCopied(false)
      );
    }
  };

  const handleSubmit = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    setGeneralError(null);
    if (!validate()) return;
    setSaving(true);
    try {
      const saved = await onSave(buildPayload());
      // OAuth-mode servers with the flow wired continue into the sign-in
      // step; every other save closes as before.
      if (authMode === 'oauth' && oauthApi && saved && saved.id) {
        setFlowServer(saved);
        return;
      }
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
  const cancelBtnCls =
    'flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg';
  const primaryBtnCls =
    'flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent';

  const footer = flowServer ? (
    <>
      <button
        type="button"
        onClick={onClose}
        data-od-id="btn-mcp-flow-close"
        data-testid="btn-mcp-flow-close"
        className={cancelBtnCls}
      >
        Close
      </button>
      {deviceFlow ? (
        deviceFlow.outcome ? (
          deviceFlow.outcome.status === 'completed' ? (
            <button
              type="button"
              onClick={onClose}
              data-od-id="btn-mcp-flow-done"
              data-testid="btn-mcp-flow-done"
              className={primaryBtnCls}
            >
              Done
            </button>
          ) : (
            <button
              type="button"
              onClick={() => void startDeviceFlow()}
              data-od-id="btn-mcp-device-restart"
              data-testid="btn-mcp-device-restart"
              className={primaryBtnCls}
            >
              Try again
            </button>
          )
        ) : (
          <button
            type="button"
            disabled
            data-od-id="btn-mcp-flow-waiting"
            data-testid="btn-mcp-flow-waiting"
            className={primaryBtnCls}
          >
            Waiting…
          </button>
        )
      ) : (
        <>
          <button
            type="button"
            onClick={() => void startDeviceFlow()}
            disabled={flowBusy}
            data-od-id="btn-mcp-device"
            data-testid="btn-mcp-device"
            className={cancelBtnCls}
          >
            Use device code
          </button>
          <button
            type="button"
            onClick={() => void startBrowserSignIn()}
            disabled={flowBusy}
            data-od-id="btn-mcp-signin"
            data-testid="btn-mcp-signin"
            className={primaryBtnCls}
          >
            {flowBusy ? 'Starting…' : 'Sign in with browser'}
          </button>
        </>
      )}
    </>
  ) : (
    <>
      <button
        type="button"
        onClick={onClose}
        data-od-id="btn-mcp-cancel"
        data-testid="btn-mcp-cancel"
        className={cancelBtnCls}
      >
        Cancel
      </button>
      <button
        type="submit"
        form="mcp-server-form"
        data-od-id={submitBtnId}
        data-testid={submitBtnId}
        disabled={saving}
        className={primaryBtnCls}
      >
        {saving ? 'Saving…' : isEdit ? 'Save changes' : 'Add'}
      </button>
    </>
  );

  return (
    <Modal
      title={flowServer ? `Sign in to ${flowServer.name}` : isEdit ? `Edit ${server?.name}` : 'Add MCP server'}
      onClose={onClose}
      odId="modal-mcp-server"
      data-testid="modal-mcp-server"
      boxClassName="md:max-w-xl"
      footer={footer}
    >
      {flowServer ? (
        <div className="space-y-3 p-5" data-testid="mcp-oauth-step">
          <p className="text-[13px] leading-5 text-fg2">
            {flowServer.name} is saved with OAuth authentication. Complete sign-in to store its
            access token — the row turns Connected once the provider consents.
          </p>
          {flowError ? (
            <p className="text-[12px] text-danger" data-testid="mcp-flow-error">
              {flowError}
            </p>
          ) : null}
          {deviceFlow ? (
            <div
              className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-3 py-3"
              data-testid="mcp-device-panel"
            >
              <p className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-muted">
                Device sign-in
              </p>
              <div className="flex items-center gap-2">
                <code
                  className="rounded-md border border-line bg-surface px-2.5 py-1.5 font-mono text-[14px] tracking-[0.14em] text-fg"
                  data-testid="mcp-device-code"
                >
                  {deviceFlow.begin.user_code}
                </code>
                <button
                  type="button"
                  onClick={copyUserCode}
                  aria-label="Copy user code"
                  data-od-id="btn-mcp-device-copy"
                  data-testid="btn-mcp-device-copy"
                  className="flex h-8 items-center gap-1 rounded-md border border-line px-2 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                >
                  <Icon name="copy" size={12} /> {copied ? 'Copied' : 'Copy'}
                </button>
              </div>
              <p className="mt-2 text-[12px] leading-4 text-fg2">
                Open{' '}
                <a
                  href={deviceFlow.begin.verification_uri_complete || deviceFlow.begin.verification_uri}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 font-mono text-accent hover:underline"
                  data-testid="link-mcp-device-verify"
                >
                  {deviceFlow.begin.verification_uri}
                  <Icon name="external-link" size={11} />
                </a>{' '}
                and paste the code.
              </p>
              <p
                className={cx(
                  'mt-2 flex items-center gap-1.5 text-[12px]',
                  deviceFlow.outcome
                    ? deviceFlow.outcome.status === 'completed'
                      ? 'text-[color-mix(in_oklab,var(--success),black_25%)]'
                      : deviceFlow.outcome.status === 'expired'
                      ? 'text-[color-mix(in_oklab,var(--warn),black_38%)]'
                      : 'text-danger'
                    : 'text-muted'
                )}
                data-testid="mcp-device-status"
              >
                {deviceFlow.outcome ? (
                  deviceFlow.outcome.status === 'completed' ? (
                    <>
                      <Icon name="check" size={12} />
                      Authorized — {flowServer.name} is connected.
                    </>
                  ) : deviceFlow.outcome.status === 'expired' ? (
                    'The code expired before authorization completed — start a new one.'
                  ) : deviceFlow.outcome.status === 'denied' ? (
                    `The authorization was denied${deviceFlow.outcome.detail ? ` — ${deviceFlow.outcome.detail}` : ''}.`
                  ) : (
                    deviceFlow.outcome.detail
                  )
                ) : (
                  <>
                    <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-accent" />
                    Waiting for authorization — this polls automatically.
                  </>
                )}
              </p>
            </div>
          ) : null}
        </div>
      ) : (
      <form id="mcp-server-form" onSubmit={handleSubmit} className="space-y-4 p-5">
        {generalError ? (
          <p className="text-[12px] text-danger" data-testid="mcp-dialog-error">
            {generalError}
          </p>
        ) : null}

        {!isEdit && (
          <div>
            <span className="text-[12px] font-medium text-fg2">Start from a preset</span>
            <div className="mt-1.5 flex flex-wrap gap-1.5">
              {PRESETS.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  title={p.help}
                  data-testid={'mcp-preset-' + p.id}
                  onClick={() => applyPreset(p)}
                  className="flex h-8 items-center gap-1.5 rounded-md border border-line px-2.5 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                >
                  {p.label}
                </button>
              ))}
            </div>
            <p className="mt-1 text-[11px] leading-4 text-muted">
              Presets fill the URL, transport, and OAuth auth mode — finish the sign-in after
              saving.
            </p>
          </div>
        )}

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
            onChange={(e) => {
              const next = e.target.value as McpTransport;
              setTransport(next);
              // OAuth is valid only on URL transports — a switch back to
              // stdio resets the auth mode (the domain rejects the pair).
              if (next === 'stdio') setAuthMode('none');
            }}
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
              {url.includes('{org}') ? (
                <p className="mt-1 text-[11px] leading-4 text-muted" data-testid="mcp-url-org-help">
                  replace {'{org}'} with your Sentry organization slug
                </p>
              ) : null}
            </div>

            <div>
              <label className={labelCls} htmlFor="mcp-auth-mode">
                Authentication
              </label>
              <select
                id="mcp-auth-mode"
                className={inputCls}
                value={authMode}
                aria-label="Authentication mode"
                data-od-id="input-mcp-auth-mode"
                data-testid="input-mcp-auth-mode"
                onChange={(e) => {
                  const next = e.target.value as McpAuthMode;
                  setAuthMode(next);
                  // Switching off oauth drops the BYO inputs; the payload's
                  // explicit `none` clears any stored client server-side.
                  if (next === 'none') {
                    setOauthClientId('');
                    setOauthClientSecret('');
                  }
                }}
              >
                {AUTH_MODES.map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </select>
            </div>

            {authMode === 'oauth' ? (
              <div className="space-y-2" data-testid="mcp-oauth-client">
                <div>
                  <label className={labelCls} htmlFor="mcp-oauth-client-id">
                    Client ID
                  </label>
                  <input
                    id="mcp-oauth-client-id"
                    className={cx(inputCls, 'font-mono text-[13px]')}
                    placeholder="client id — a metadata document URL also works"
                    value={oauthClientId}
                    aria-label="Client ID"
                    data-od-id="input-mcp-oauth-client-id"
                    data-testid="input-mcp-oauth-client-id"
                    onChange={(e) => setOauthClientId(e.target.value)}
                  />
                </div>
                <div>
                  <label className={labelCls} htmlFor="mcp-oauth-client-secret">
                    Client secret
                  </label>
                  <input
                    id="mcp-oauth-client-secret"
                    type="password"
                    className={cx(inputCls, 'font-mono text-[13px]')}
                    placeholder={
                      server?.oauth_client_secret_hint
                        ? `•••• ${server.oauth_client_secret_hint}`
                        : 'Client secret'
                    }
                    title={
                      server?.oauth_client_secret_hint
                        ? `Stored — leave empty to keep •••• ${server.oauth_client_secret_hint}`
                        : undefined
                    }
                    value={oauthClientSecret}
                    aria-label="Client secret"
                    data-od-id="input-mcp-oauth-client-secret"
                    data-testid="input-mcp-oauth-client-secret"
                    onChange={(e) => setOauthClientSecret(e.target.value)}
                  />
                </div>
                <p className="text-[11px] leading-4 text-muted">
                  Leave blank to auto-register (DCR) where the provider supports it;
                  bring-your-own app otherwise.
                </p>
              </div>
            ) : null}

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
      )}
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
