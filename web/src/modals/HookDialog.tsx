import { useEffect, useState } from "react";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { Icon } from "../components/ui/Icon";
import { Toggle } from "../components/ui/Toggle";
import { cx } from "../lib/helpers";
import { ScriptEditor } from "./ScriptEditor";

const rowInputCls =
  'rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 text-[13px] text-fg2 placeholder:text-muted focus:border-accent outline-none font-mono';
import {
  api,
  ApiError,
  formatApiError,
  type ApiHook,
  type ApiHookPayload,
  type ApiHookSaveResult,
  type ApiHookTestResult,
  type ApiProviderConfig,
  type ApiMcpServer,
  type HookEvent,
  type HookFailurePolicy,
  type HookHandlerType,
} from "../lib/api";
import { toolCatalog } from "../lib/toolCatalog";
import {
  HOOK_EVENTS,
  HOOK_MATCHER_MAX_LENGTH,
  hookEventMeta,
  hookValueNoun,
  hookValueOptionsFor,
  countMatcherString,
  matcherTier,
} from "../lib/hooksUi";

export interface HookDialogProps {
  /** Existing row when editing; null/undefined opens the create form. */
  hook?: ApiHook | null;
  /** Workspace slug — feeds the tool catalog, MCP servers, providers, and
   * the dry-run endpoint (workspace level; the same rules validate every
   * hook level and a test records nothing). */
  wsSlug: string;
  /** Sibling names of the SAME level for the duplicate-name check. */
  existingNames?: string[];
  onClose: () => void;
  /** Submits the structured payload: resolve closes (the dialog first shows
   * the save response's match count beside the matcher field), reject
   * keeps it open with the error inline. */
  onSave: (payload: ApiHookPayload) => Promise<ApiHookSaveResult | void>;
}

const HANDLER_OPTIONS: { value: HookHandlerType; label: string }[] = [
  { value: 'command', label: 'command — local program' },
  { value: 'script', label: 'script — JavaScript, in-process' },
  { value: 'http', label: 'http — webhook' },
  { value: 'mcp_tool', label: 'mcp_tool — workspace MCP tool' },
  { value: 'prompt', label: 'prompt — LLM evaluator' },
];

const MATCHER_HELPER =
  'Empty or * = every occurrence · names and families (mcp__github.*) split on , | space match exactly · anything else is an unanchored regex';

const IF_HELPER =
  'Input-level gate — runs only when the tool name matches and the pattern hits its input JSON; otherwise the hook is skipped silently. Format: Tool(pattern).';

// `config.script` stores the COMPLETE function expression — what the editor
// shows is what is stored and what runs. The seed carries the full wrapper.
const SCRIPT_TEMPLATE = `(function (input) {
  return { decision: "allow", reason: "" };
});`;

// Save-time wrapper shape check — the client mirror of the backend's
// compile-the-raw-source contract: the buffer must BE the complete
// `(function(input){ … })` expression. Both ends are tolerant (Prettier's
// reflow — `(function(input) { … });` — still passes by design).
const SCRIPT_WRAPPER_PREFIX = /^\s*\(function\s*\(\s*input\s*\)\s*\{/;
const SCRIPT_WRAPPER_SUFFIX = /\}\s*\)\s*;?\s*$/;
const SCRIPT_WRAPPER_ERROR = 'Keep the (function(input){ … }) wrapper — edit only the body inside';

// Legacy rows stored the bare function BODY (the runtime used to wrap it);
// wrap body-only values for display so those rows upgrade naturally on
// re-save, while fresh wrapped rows display verbatim.
function scriptForDisplay(script: unknown): string {
  if (typeof script !== 'string' || !script.trim()) return '';
  return /^\s*\(function/.test(script) ? script : `(function(input){\n${script}\n})`;
}

const DEFAULT_TIMEOUT_BY_HANDLER: Record<HookHandlerType, number> = {
  http: 5000,
  command: 5000,
  mcp_tool: 5000,
  prompt: 15000,
  script: 5000,
};

// Editable secret row: values are write-only — the input starts empty; a
// stored secret surfaces only as its masked hint. An empty value (or echoing
// the hint) on submit keeps the stored secret server-side.
interface SecretRowDraft {
  localKey: string;
  name: string;
  value: string;
  hint?: string;
}

// Structured (non-secret) key/value row — mcp_tool input entries.
interface KVRowDraft {
  localKey: string;
  key: string;
  value: string;
}

let rowSeq = 0;
function nextRowKey(): string {
  rowSeq += 1;
  return `hrow-${rowSeq}`;
}

function secretRowsFrom(rows: any): SecretRowDraft[] {
  return (Array.isArray(rows) ? rows : []).map((r: any) => ({
    localKey: nextRowKey(),
    name: typeof r?.name === 'string' ? r.name : '',
    value: '',
    hint: typeof r?.value === 'string' ? r.value : undefined,
  }));
}

function kvRowsFrom(input: any): KVRowDraft[] {
  if (!input || typeof input !== 'object' || Array.isArray(input)) return [];
  return Object.keys(input).map((k) => ({
    localKey: nextRowKey(),
    key: k,
    value: typeof input[k] === 'string' ? input[k] : JSON.stringify(input[k]),
  }));
}

interface HookDialogForm {
  name: string;
  event: HookEvent;
  /** The plain matcher string (D19): empty/`*` = all, charset entries =
   * exact/family list, anything else = unanchored regex. */
  matcher: string;
  /** The optional input gate `ToolName(pattern)` (D20) — tool events only. */
  ifRule: string;
  handlerType: HookHandlerType;
  url: string;
  headerRows: SecretRowDraft[];
  command: string;
  argsText: string;
  cwd: string;
  envRows: SecretRowDraft[];
  serverId: string;
  toolName: string;
  inputRows: KVRowDraft[];
  /** The sandboxed script source (D22) — script handler only. */
  script: string;
  provider: string;
  model: string;
  promptTemplate: string;
  maxInvocations: string;
  timeout: string;
  onFailure: HookFailurePolicy;
  enabled: boolean;
}

function formFromHook(hook: ApiHook | null | undefined, providers: ApiProviderConfig[]): HookDialogForm {
  const cfg: any = hook?.config || {};
  const handlerType = (hook?.handler_type || 'command') as HookHandlerType;
  return {
    name: hook?.name || '',
    event: (hook?.event || 'pre_tool_use') as HookEvent,
    matcher: typeof hook?.matcher === 'string' ? hook.matcher : '',
    ifRule: typeof hook?.if === 'string' ? hook.if : '',
    handlerType,
    url: typeof cfg.url === 'string' ? cfg.url : '',
    headerRows: secretRowsFrom(cfg.headers),
    command: typeof cfg.command === 'string' ? cfg.command : '',
    argsText: Array.isArray(cfg.args) ? cfg.args.join(' ') : '',
    cwd: typeof cfg.cwd === 'string' ? cfg.cwd : '',
    envRows: secretRowsFrom(cfg.env),
    serverId: typeof cfg.server === 'string' ? cfg.server : '',
    toolName: typeof cfg.tool === 'string' ? cfg.tool : '',
    inputRows: kvRowsFrom(cfg.input),
    script: scriptForDisplay(cfg.script),
    provider: typeof cfg.provider === 'string' ? cfg.provider : providers[0]?.id || '',
    model: typeof cfg.model === 'string' ? cfg.model : '',
    promptTemplate: typeof cfg.prompt === 'string' ? cfg.prompt : '',
    maxInvocations: typeof cfg.max_invocations_per_run === 'number' ? String(cfg.max_invocations_per_run) : '5',
    timeout: String(hook?.timeout_ms || DEFAULT_TIMEOUT_BY_HANDLER[handlerType]),
    onFailure: hook?.on_failure === 'block' ? 'block' : 'allow',
    enabled: hook ? hook.enabled !== false : true,
  };
}

// HookDialog is the structured create/edit form for one agent lifecycle hook
// (spec: Hooks pane — dialog contract). One labeled control per property —
// never a JSON textarea. The matcher is Claude Code's single string (D19):
// one monospace input with a live match count and a static syntax helper; the
// optional `if` input gate appears on tool events only (D20). The Test
// section performs a REAL dry run of the current configuration and records
// nothing.
export function HookDialog({ hook, wsSlug, existingNames = [], onClose, onSave }: HookDialogProps) {
  const isEdit = Boolean(hook);
  const [form, setForm] = useState<HookDialogForm>(() => formFromHook(hook, []));
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [generalError, setGeneralError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  // Save response state: the match report renders beside the matcher field
  // and the footer flips to Done so it is actually readable.
  const [savedMatchCount, setSavedMatchCount] = useState<{ matched: number; of: number } | null>(null);
  const [saved, setSaved] = useState(false);
  // Script handler (D23): the editor's live parse verdict gates save, and the
  // Format button runs Prettier standalone in a lazy chunk.
  const [scriptSyntaxError, setScriptSyntaxError] = useState(false);
  const [formattingScript, setFormattingScript] = useState(false);

  // Applies-to options (tool events): the workspace-visible tool names via
  // the shared catalog cache plus the family entries from the workspace MCP
  // registry. Prompt handlers additionally need the provider catalog.
  const [toolNames, setToolNames] = useState<{ key: string; name: string }[]>([]);
  const [mcpServers, setMcpServers] = useState<ApiMcpServer[]>([]);
  const [providers, setProviders] = useState<ApiProviderConfig[]>([]);

  useEffect(() => {
    let mounted = true;
    if (!wsSlug) return;
    toolCatalog
      .ensure(wsSlug)
      .then(() => {
        if (mounted) setToolNames(toolCatalog.entries());
      })
      .catch(() => {});
    api.mcp
      .list(wsSlug)
      .then((res) => {
        if (mounted && res?.servers) setMcpServers(res.servers);
      })
      .catch(() => {});
    api.providers
      .list(wsSlug)
      .then((res) => {
        if (mounted && res?.providers) setProviders(res.providers);
      })
      .catch(() => {});
    return () => {
      mounted = false;
    };
  }, [wsSlug]);

  // Providers resolving after the form seeded: default the evaluator provider
  // once, so the prompt section is never silently empty on create.
  useEffect(() => {
    if (providers.length > 0 && !form.provider) {
      setForm((f) => ({ ...f, provider: providers[0].id }));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reseed once when providers arrive
  }, [providers]);

  const patch = (p: Partial<HookDialogForm>) => setForm((f) => ({ ...f, ...p }));

  const eventMeta = hookEventMeta(form.event);
  const valueOptions = hookValueOptionsFor(form.event, toolNames, mcpServers);
  const isToolEvent = eventMeta.valueKind === 'tools';
  // Live match count (D19): recomputed per keystroke over the event's
  // candidates; null when the string cannot count (invalid/over-long regex) —
  // then the last server report (post-save) fills the slot instead.
  const liveCount = countMatcherString(form.matcher, valueOptions);
  const countReadout: { matched: number; of: number } | null =
    liveCount !== null ? { matched: liveCount, of: valueOptions.length } : savedMatchCount ?? null;

  // --- Test panel state (dry run; nothing is recorded) ---
  const [testOpen, setTestOpen] = useState(false);
  const [testEvent, setTestEvent] = useState<HookEvent>(form.event);
  const [testTool, setTestTool] = useState('web.fetch');
  const [testOrigin, setTestOrigin] = useState('user');
  const [testStatus, setTestStatus] = useState('completed');
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<ApiHookTestResult | null>(null);
  const [testError, setTestError] = useState<string | null>(null);

  const buildConfig = (): unknown => {
    switch (form.handlerType) {
      case 'http': {
        const cfg: any = { url: form.url.trim() };
        const headers = buildSecretRows(form.headerRows);
        if (headers.length) cfg.headers = headers;
        return cfg;
      }
      case 'command': {
        const cfg: any = { command: form.command.trim() };
        const args = form.argsText.trim().split(/\s+/).filter(Boolean);
        if (args.length) cfg.args = args;
        const env = buildSecretRows(form.envRows);
        if (env.length) cfg.env = env;
        if (form.cwd.trim()) cfg.cwd = form.cwd.trim();
        return cfg;
      }
      case 'mcp_tool': {
        const cfg: any = {
          server: form.serverId,
          tool: form.toolName.trim(),
        };
        const input: Record<string, string> = {};
        for (const row of form.inputRows) {
          if (row.key.trim()) input[row.key.trim()] = row.value;
        }
        if (Object.keys(input).length) cfg.input = input;
        return cfg;
      }
      case 'script':
        return { script: form.script };
      case 'prompt': {
        const cfg: any = {
          provider: form.provider,
          model: form.model.trim(),
          prompt: form.promptTemplate,
        };
        const max = parseInt(form.maxInvocations.trim(), 10);
        if (Number.isFinite(max) && max > 0) cfg.max_invocations_per_run = max;
        return cfg;
      }
    }
  };

  const buildPayload = (): ApiHookPayload => ({
    name: form.name.trim(),
    event: form.event,
    matcher: form.matcher,
    ...(form.ifRule.trim() ? { if: form.ifRule.trim() } : {}),
    handler_type: form.handlerType,
    config: buildConfig(),
    timeout_ms: parseInt(form.timeout.trim(), 10) || DEFAULT_TIMEOUT_BY_HANDLER[form.handlerType],
    on_failure: form.onFailure,
    enabled: form.enabled,
  });

  const validate = (): boolean => {
    const errs: Record<string, string> = {};
    const name = form.name.trim();
    if (!name) errs.name = 'Hook name is required';
    else {
      const lower = name.toLowerCase();
      if (existingNames.some((n) => n.toLowerCase() === lower && (!isEdit || n !== hook?.name))) {
        errs.name = 'A hook with this name already exists';
      }
    }
    // Loose mirror of the backend's tier rules (D19): only the regex tier can
    // be malformed or over-long, and prompt evaluators must narrow match-all.
    if (matcherTier(form.matcher) === 'regex') {
      const pattern = form.matcher.trim();
      if (pattern.length > HOOK_MATCHER_MAX_LENGTH) {
        errs.matcher = `Matcher must be at most ${HOOK_MATCHER_MAX_LENGTH} characters`;
      } else {
        try {
          new RegExp(pattern);
        } catch {
          errs.matcher = 'Matcher is not a valid regular expression';
        }
      }
    }
    if (form.handlerType === 'prompt' && matcherTier(form.matcher) === 'all') {
      errs.matcher = 'Prompt evaluators require a matcher — a match-all evaluator fires on every occurrence';
    }
    switch (form.handlerType) {
      case 'http':
        if (!form.url.trim()) errs['config.url'] = 'URL is required for webhooks';
        break;
      case 'command':
        if (!form.command.trim()) errs['config.command'] = 'Command is required';
        break;
      case 'mcp_tool':
        if (!form.serverId) errs['config.server'] = 'Server is required';
        if (!form.toolName.trim()) errs['config.tool'] = 'Tool name is required';
        break;
      case 'script':
        if (!form.script.trim()) {
          errs['config.script'] = 'Script is required';
        } else if (!SCRIPT_WRAPPER_PREFIX.test(form.script) || !SCRIPT_WRAPPER_SUFFIX.test(form.script)) {
          // Shape gate before the parse gate — a stripped/altered wrapper is
          // the contract breach the backend also rejects at save time.
          errs['config.script'] = SCRIPT_WRAPPER_ERROR;
        } else if (scriptSyntaxError) {
          // Mirror of the server's compile-check 422 (D22) — never send a
          // script the backend would reject.
          errs['config.script'] = 'Script has a syntax error — fix it before saving';
        }
        break;
      case 'prompt':
        if (!form.provider) errs['config.provider'] = 'Provider is required';
        if (!form.model.trim()) errs['config.model'] = 'Model is required';
        if (!form.promptTemplate.trim()) errs['config.prompt'] = 'Prompt is required';
        break;
    }
    const t = parseInt(form.timeout.trim(), 10);
    if (!Number.isFinite(t) || t < 1 || t > 60000) {
      errs.timeout_ms = 'Timeout must be between 1 and 60000 ms';
    }
    setFieldErrors(errs);
    return Object.keys(errs).length === 0;
  };

  const handleSubmit = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    setGeneralError(null);
    if (!validate()) return;
    setSaving(true);
    try {
      const result = await onSave(buildPayload());
      // The save response's match report renders beside the matcher field;
      // Done closes so the number is actually read.
      setSavedMatchCount(result && 'match_count' in result ? result.match_count ?? null : null);
      setSaved(true);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.details && err.details.length > 0) {
        const errs: Record<string, string> = {};
        const general: string[] = [];
        for (const d of err.details) {
          if (d.field) errs[d.field] = d.message || 'Invalid value';
          else general.push(d.message || 'Invalid value');
        }
        setFieldErrors(errs);
        if (general.length) setGeneralError(general.join(' '));
      } else {
        setGeneralError(formatApiError(err, isEdit ? `Failed to update ${hook?.name}` : 'Failed to add the hook'));
      }
    } finally {
      setSaving(false);
    }
  };

  // Format (D23): Prettier standalone loads in its own lazy chunk on first
  // click and beautifies the buffer in place. The button is disabled while a
  // parse error is present, so a format-time parse failure is not expected.
  // Prettier formats the full expression and may reflow the wrapper (e.g.
  // `(function(input) { … });`) — the tolerant shape check still passes.
  // A wrapper-shape error alone is not a parse error, so Format stays
  // available to restore a well-formed buffer.
  const formatScript = async () => {
    setFormattingScript(true);
    try {
      const [{ format }, babelPlugin, estreePlugin] = await Promise.all([
        import('prettier/standalone'),
        import('prettier/plugins/babel'),
        import('prettier/plugins/estree'),
      ]);
      const formatted = await format(form.script, { parser: 'babel', plugins: [babelPlugin, estreePlugin] });
      patch({ script: formatted });
      setFieldErrors((f) => {
        if (!f['config.script']) return f;
        const next = { ...f };
        delete next['config.script'];
        return next;
      });
    } catch {
      setFieldErrors((f) => ({ ...f, 'config.script': 'The script could not be formatted' }));
    } finally {
      setFormattingScript(false);
    }
  };

  const runTest = async () => {
    setTesting(true);
    setTestError(null);
    setTestResult(null);
    try {
      const payload = buildPayload();
      const overrides: any = {};
      if (testEvent !== form.event) overrides.event = testEvent;
      const effectiveEvent = testEvent;
      if (effectiveEvent === 'pre_tool_use' || effectiveEvent === 'post_tool_use') {
        overrides.tool_name = testTool;
      }
      if (effectiveEvent === 'run_started' || effectiveEvent === 'user_prompt_submit') {
        overrides.origin = testOrigin;
      }
      if (effectiveEvent === 'run_finished') {
        overrides.status = testStatus;
      }
      const result = await api.hooks.test(wsSlug, { ...payload, ...(isEdit ? { id: hook!.id } : {}), overrides });
      setTestResult(result);
    } catch (err: unknown) {
      setTestError(formatApiError(err, 'The test run failed'));
    } finally {
      setTesting(false);
    }
  };

  // Read-only preview of the synthetic event payload the dry run sends.
  const testPreview = JSON.stringify(
    {
      event: testEvent,
      origin: testOrigin,
      ...(testEvent === 'run_finished' ? { status: testStatus } : {}),
      ...(testEvent === 'pre_tool_use' || testEvent === 'post_tool_use'
        ? { tool: { name: testTool, call_id: 'hook-test' } }
        : {}),
      workspace: { name: 'this workspace' },
      agent: { name: 'hook-test' },
    },
    null,
    2
  );

  const err = (key: string) => fieldErrors[key];

  return (
    <Modal
      title={isEdit ? `Edit ${hook?.name}` : 'Add hook'}
      onClose={onClose}
      odId="modal-hook"
      data-testid="modal-hook"
      boxClassName="w-full max-w-lg md:max-w-2xl"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-od-id="btn-hook-cancel"
            data-testid="btn-hook-cancel"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            {saved ? 'Close' : 'Cancel'}
          </button>
          {saved ? (
            <button
              type="button"
              onClick={onClose}
              data-od-id="btn-hook-done"
              data-testid="btn-hook-done"
              className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)]"
            >
              Done
            </button>
          ) : (
            <button
              type="submit"
              form="hook-form"
              data-od-id="btn-hook-save"
              data-testid="btn-hook-save"
              disabled={saving}
              className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent"
            >
              {saving ? 'Saving…' : isEdit ? 'Save changes' : 'Add hook'}
            </button>
          )}
        </>
      }
    >
      <form id="hook-form" onSubmit={handleSubmit} className="space-y-4 p-5">
        {generalError ? (
          <p className="text-[12px] text-danger" data-testid="hook-dialog-error">
            {generalError}
          </p>
        ) : null}

        <div>
          <label className={labelCls} htmlFor="hook-name">
            Hook name
          </label>
          <input
            id="hook-name"
            className={cx(inputCls, err('name') && 'border-danger')}
            placeholder="e.g. Policy gate"
            value={form.name}
            aria-label="Hook name"
            aria-invalid={Boolean(err('name'))}
            data-od-id="input-hook-name"
            data-testid="input-hook-name"
            autoFocus
            onChange={(e) => patch({ name: e.target.value })}
          />
          {err('name') && (
            <p className="mt-1 text-[12px] text-danger" data-testid="hook-name-error">
              {err('name')}
            </p>
          )}
        </div>

        <div>
          <label className={labelCls} htmlFor="hook-event">
            Event
          </label>
          <select
            id="hook-event"
            className={inputCls}
            value={form.event}
            aria-label="Event"
            data-od-id="select-hook-event"
            data-testid="select-hook-event"
            onChange={(e) => {
              const event = e.target.value as HookEvent;
              patch({ event });
              setTestEvent(event);
            }}
          >
            {HOOK_EVENTS.map((e) => (
              <option key={e.value} value={e.value}>
                {e.label}
                {e.blocking ? ' — can block' : ' — observes'}
              </option>
            ))}
          </select>
          <p className="mt-1 text-[11px] leading-4 text-muted" data-testid="hook-event-helper">
            {eventMeta.firesOn}
          </p>
        </div>

        <div>
          <div className="flex flex-wrap items-baseline justify-between gap-1.5">
            <label className={labelCls} htmlFor="hook-matcher">
              Matcher
            </label>
            {countReadout && (
              <span
                data-testid="hook-match-count"
                className="font-mono text-[11px] text-[color-mix(in_oklab,var(--success),black_25%)]"
              >
                Matches {countReadout.matched} of {countReadout.of} {hookValueNoun(form.event)}
              </span>
            )}
          </div>
          <input
            id="hook-matcher"
            className={cx(inputCls, 'font-mono text-[13px]', err('matcher') && 'border-danger')}
            placeholder="shell, read_file"
            value={form.matcher}
            aria-label="Matcher"
            aria-invalid={Boolean(err('matcher'))}
            data-od-id="input-hook-matcher"
            data-testid="input-hook-matcher"
            onChange={(e) => patch({ matcher: e.target.value })}
          />
          <p className="mt-1 text-[11px] leading-4 text-muted" data-testid="hook-matcher-helper">
            {MATCHER_HELPER}
          </p>
          {err('matcher') && (
            <p className="mt-1 text-[12px] text-danger" data-testid="hook-matcher-error">
              {err('matcher')}
            </p>
          )}
        </div>

        {isToolEvent && (
          <div>
            <label className={labelCls} htmlFor="hook-if">
              If <span className="text-muted font-normal">(optional)</span>
            </label>
            <input
              id="hook-if"
              className={cx(inputCls, 'font-mono text-[13px]', err('if') && 'border-danger')}
              placeholder="read_file(secret*)"
              value={form.ifRule}
              aria-label="If condition"
              aria-invalid={Boolean(err('if'))}
              data-od-id="input-hook-if"
              data-testid="input-hook-if"
              onChange={(e) => patch({ ifRule: e.target.value })}
            />
            <p className="mt-1 text-[11px] leading-4 text-muted" data-testid="hook-if-helper">
              {IF_HELPER}
            </p>
            {err('if') && <p className="mt-1 text-[12px] text-danger">{err('if')}</p>}
          </div>
        )}

        <div>
          <label className={labelCls} htmlFor="hook-handler">
            Handler
          </label>
          <select
            id="hook-handler"
            className={inputCls}
            value={form.handlerType}
            aria-label="Handler type"
            data-od-id="select-hook-handler"
            data-testid="select-hook-handler"
            onChange={(e) => {
              const handlerType = e.target.value as HookHandlerType;
              patch({
                handlerType,
                timeout: String(DEFAULT_TIMEOUT_BY_HANDLER[handlerType]),
                ...(handlerType === 'script' && !form.script.trim() ? { script: SCRIPT_TEMPLATE } : {}),
              });
            }}
          >
            {HANDLER_OPTIONS.map((h) => (
              <option key={h.value} value={h.value}>
                {h.label}
              </option>
            ))}
          </select>

          {form.handlerType === 'http' && (
            <div className="mt-3 space-y-3">
              <div>
                <label className={labelCls} htmlFor="hook-url">
                  URL
                </label>
                <input
                  id="hook-url"
                  className={cx(inputCls, 'font-mono text-[13px]', err('config.url') && 'border-danger')}
                  placeholder="https://hooks.example.com/onclaw"
                  value={form.url}
                  aria-label="URL"
                  aria-invalid={Boolean(err('config.url'))}
                  data-testid="input-hook-url"
                  onChange={(e) => patch({ url: e.target.value })}
                />
                {err('config.url') && <p className="mt-1 text-[12px] text-danger">{err('config.url')}</p>}
                <p className="mt-1 text-[11px] leading-4 text-muted">
                  The event JSON is POSTed here; a JSON body with a decision is honored.
                </p>
              </div>
              <SecretRows
                label="Headers"
                noun="header"
                rows={form.headerRows}
                errors={{}}
                onUpdate={(localKey, p) =>
                  patch({ headerRows: form.headerRows.map((r) => (r.localKey === localKey ? { ...r, ...p } : r)) })
                }
                onAdd={() => patch({ headerRows: [...form.headerRows, { localKey: nextRowKey(), name: '', value: '' }] })}
                onRemove={(localKey) => patch({ headerRows: form.headerRows.filter((r) => r.localKey !== localKey) })}
                nameTestId="input-hook-header-name"
                valueTestId="input-hook-header-value"
                rowTestId="hook-header-row"
                addTestId="btn-hook-header-add"
                removeTestId="btn-hook-header-remove"
              />
            </div>
          )}

          {form.handlerType === 'command' && (
            <div className="mt-3 space-y-3">
              <div>
                <label className={labelCls} htmlFor="hook-command">
                  Command
                </label>
                <input
                  id="hook-command"
                  className={cx(inputCls, 'font-mono text-[13px]', err('config.command') && 'border-danger')}
                  placeholder="/usr/local/bin/gate"
                  value={form.command}
                  aria-label="Command"
                  aria-invalid={Boolean(err('config.command'))}
                  data-testid="input-hook-command"
                  onChange={(e) => patch({ command: e.target.value })}
                />
                {err('config.command') && <p className="mt-1 text-[12px] text-danger">{err('config.command')}</p>}
                <p className="mt-1 text-[11px] leading-4 text-muted">
                  Exec form on the OnClaw host — the event JSON arrives on stdin; exit 2 blocks.
                </p>
              </div>
              <div>
                <label className={labelCls} htmlFor="hook-args">
                  Arguments
                </label>
                <input
                  id="hook-args"
                  className={cx(inputCls, 'font-mono text-[13px]')}
                  placeholder="--strict --policy ops"
                  value={form.argsText}
                  aria-label="Arguments"
                  data-testid="input-hook-args"
                  onChange={(e) => patch({ argsText: e.target.value })}
                />
                <p className="mt-1 text-[11px] leading-4 text-muted">Space-separated, passed to the command in order.</p>
              </div>
              <SecretRows
                label="Environment variables"
                noun="variable"
                rows={form.envRows}
                errors={{}}
                onUpdate={(localKey, p) =>
                  patch({ envRows: form.envRows.map((r) => (r.localKey === localKey ? { ...r, ...p } : r)) })
                }
                onAdd={() => patch({ envRows: [...form.envRows, { localKey: nextRowKey(), name: '', value: '' }] })}
                onRemove={(localKey) => patch({ envRows: form.envRows.filter((r) => r.localKey !== localKey) })}
                nameTestId="input-hook-env-name"
                valueTestId="input-hook-env-value"
                rowTestId="hook-env-row"
                addTestId="btn-hook-env-add"
                removeTestId="btn-hook-env-remove"
              />
              <div>
                <label className={labelCls} htmlFor="hook-cwd">
                  Working directory <span className="text-muted font-normal">(optional)</span>
                </label>
                <input
                  id="hook-cwd"
                  className={cx(inputCls, 'font-mono text-[13px]')}
                  placeholder="/srv/hooks"
                  value={form.cwd}
                  aria-label="Working directory"
                  data-testid="input-hook-cwd"
                  onChange={(e) => patch({ cwd: e.target.value })}
                />
              </div>
            </div>
          )}

          {form.handlerType === 'mcp_tool' && (
            <div className="mt-3 space-y-3">
              <div>
                <label className={labelCls} htmlFor="hook-server">
                  Server
                </label>
                <select
                  id="hook-server"
                  className={cx(inputCls, err('config.server') && 'border-danger')}
                  value={form.serverId}
                  aria-label="Server"
                  aria-invalid={Boolean(err('config.server'))}
                  data-testid="select-hook-server"
                  onChange={(e) => patch({ serverId: e.target.value })}
                >
                  <option value="">Select a workspace server…</option>
                  {mcpServers.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </select>
                {err('config.server') && <p className="mt-1 text-[12px] text-danger">{err('config.server')}</p>}
                <p className="mt-1 text-[11px] leading-4 text-muted">
                  Workspace MCP servers only — agent-private are unreachable.
                </p>
                {mcpServers.length === 0 && (
                  <p className="mt-1 text-[11px] leading-4 text-muted">
                    No workspace MCP servers registered yet — add one in Settings → MCP servers.
                  </p>
                )}
              </div>
              <div>
                <label className={labelCls} htmlFor="hook-tool-name">
                  Tool
                </label>
                <input
                  id="hook-tool-name"
                  className={cx(inputCls, 'font-mono text-[13px]', err('config.tool') && 'border-danger')}
                  placeholder="create_issue"
                  value={form.toolName}
                  aria-label="Tool"
                  aria-invalid={Boolean(err('config.tool'))}
                  data-testid="input-hook-tool-name"
                  onChange={(e) => patch({ toolName: e.target.value })}
                />
                {err('config.tool') && <p className="mt-1 text-[12px] text-danger">{err('config.tool')}</p>}
              </div>
              <div>
                <div className="flex items-center justify-between">
                  <span className={labelCls}>Input</span>
                  <button
                    type="button"
                    data-testid="btn-hook-input-add"
                    onClick={() => patch({ inputRows: [...form.inputRows, { localKey: nextRowKey(), key: '', value: '' }] })}
                    className="flex h-7 items-center gap-1 rounded-md border border-line px-2 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                  >
                    <Icon name="plus" size={13} /> Add field
                  </button>
                </div>
                {form.inputRows.length === 0 ? (
                  <p className="mt-2 rounded-md border border-dashed border-line px-3 py-3 text-center text-[12px] text-muted">
                    No input fields.
                  </p>
                ) : (
                  <div className="mt-2 space-y-2">
                    {form.inputRows.map((row) => (
                      <div key={row.localKey} className="flex items-center gap-2" data-testid="hook-input-row">
                        <input
                          aria-label="Field key"
                          placeholder="Key"
                          className={cx(rowInputCls, 'h-8 w-28 sm:w-40 shrink-0')}
                          value={row.key}
                          data-testid="input-hook-input-key"
                          onChange={(e) =>
                            patch({
                              inputRows: form.inputRows.map((r) =>
                                r.localKey === row.localKey ? { ...r, key: e.target.value } : r
                              ),
                            })
                          }
                        />
                        <input
                          aria-label="Field value"
                          placeholder="Value"
                          className={cx(rowInputCls, 'h-8 min-w-0 flex-1')}
                          value={row.value}
                          data-testid="input-hook-input-value"
                          onChange={(e) =>
                            patch({
                              inputRows: form.inputRows.map((r) =>
                                r.localKey === row.localKey ? { ...r, value: e.target.value } : r
                              ),
                            })
                          }
                        />
                        <button
                          type="button"
                          aria-label="Remove field"
                          data-testid="btn-hook-input-remove"
                          onClick={() => patch({ inputRows: form.inputRows.filter((r) => r.localKey !== row.localKey) })}
                          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger"
                        >
                          <Icon name="x" size={13} />
                        </button>
                      </div>
                    ))}
                  </div>
                )}
                <p className="mt-1 text-[11px] leading-4 text-muted">
                  ${'{event.*}'} placeholders are substituted before the call.
                </p>
              </div>
            </div>
          )}

          {form.handlerType === 'prompt' && (
            <div className="mt-3 space-y-3">
              <div>
                <span className={labelCls}>Evaluator model</span>
                <div className="mt-1 grid grid-cols-1 sm:grid-cols-2 gap-2">
                  <select
                    className={cx(inputCls, err('config.provider') && 'border-danger')}
                    value={form.provider}
                    aria-label="Provider"
                    aria-invalid={Boolean(err('config.provider'))}
                    data-testid="select-hook-provider"
                    onChange={(e) => patch({ provider: e.target.value })}
                  >
                    <option value="">Select a provider…</option>
                    {providers.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                  <input
                    className={cx(inputCls, 'font-mono text-[13px]', err('config.model') && 'border-danger')}
                    placeholder="claude-sonnet-5"
                    value={form.model}
                    aria-label="Model"
                    aria-invalid={Boolean(err('config.model'))}
                    data-testid="input-hook-model"
                    onChange={(e) => patch({ model: e.target.value })}
                  />
                </div>
                {(err('config.provider') || err('config.model')) && (
                  <p className="mt-1 text-[12px] text-danger">{err('config.provider') || err('config.model')}</p>
                )}
              </div>
              <div>
                <label className={labelCls} htmlFor="hook-prompt">
                  Prompt
                </label>
                <textarea
                  id="hook-prompt"
                  rows={4}
                  className={cx(inputCls, 'h-auto py-2', err('config.prompt') && 'border-danger')}
                  placeholder="Block any call whose args contain credentials or keys."
                  value={form.promptTemplate}
                  aria-label="Prompt"
                  aria-invalid={Boolean(err('config.prompt'))}
                  data-testid="input-hook-prompt"
                  onChange={(e) => patch({ promptTemplate: e.target.value })}
                />
                {err('config.prompt') && <p className="mt-1 text-[12px] text-danger">{err('config.prompt')}</p>}
                <p className="mt-1 text-[11px] leading-4 text-muted">
                  Evaluator sees the tool name + delimited args only — never the conversation. It must call decide(); free
                  text is ignored; an injection verdict always blocks.
                </p>
              </div>
              <div>
                <label className={labelCls} htmlFor="hook-max-invocations">
                  Max invocations per run
                </label>
                <input
                  id="hook-max-invocations"
                  type="number"
                  min={1}
                  className={cx(inputCls, err('config.max_invocations_per_run') && 'border-danger')}
                  value={form.maxInvocations}
                  aria-label="Max invocations per run"
                  data-testid="input-hook-max-invocations"
                  onChange={(e) => patch({ maxInvocations: e.target.value })}
                />
                <p className="mt-1 text-[11px] leading-4 text-muted">
                  Over cap → allow, recorded as capped.
                </p>
              </div>
            </div>
          )}

          {form.handlerType === 'script' && (
            <div className="mt-3 space-y-3">
              <div>
                <div className="flex items-center justify-between">
                  <label className={labelCls} htmlFor="hook-script">
                    Script
                  </label>
                  <button
                    type="button"
                    data-od-id="button-hook-script-format"
                    data-testid="button-hook-script-format"
                    disabled={scriptSyntaxError || formattingScript}
                    onClick={() => void formatScript()}
                    className="flex h-7 items-center rounded-md border border-line px-2 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40 disabled:hover:border-line disabled:hover:text-fg2"
                  >
                    {formattingScript ? 'Formatting…' : 'Format'}
                  </button>
                </div>
                <ScriptEditor
                  value={form.script}
                  onChange={(script) => patch({ script })}
                  onSyntaxError={setScriptSyntaxError}
                />
                {err('config.script') && (
                  <p className="mt-1 text-[12px] text-danger" data-testid="hook-script-error">
                    {err('config.script')}
                  </p>
                )}
                <p className="mt-1 text-[11px] leading-4 text-muted" data-testid="hook-script-helper">
                  Runs inside OnClaw — no network, files, or env access. Return {'{decision, reason}'} to block;
                  anything else allows. ES5.1 only — newer syntax (const, arrow functions, template literals) is not
                  supported.{' '}
                  {'The (function(input){ … }) wrapper is the harness structure — edit only the body.'}
                </p>
              </div>
            </div>
          )}
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className={labelCls} htmlFor="hook-timeout">
              Timeout (ms)
            </label>
            <input
              id="hook-timeout"
              type="number"
              min={1}
              className={cx(inputCls, err('timeout_ms') && 'border-danger')}
              value={form.timeout}
              aria-label="Timeout (ms)"
              aria-invalid={Boolean(err('timeout_ms'))}
              data-testid="input-hook-timeout"
              onChange={(e) => patch({ timeout: e.target.value })}
            />
            {err('timeout_ms') && <p className="mt-1 text-[12px] text-danger">{err('timeout_ms')}</p>}
          </div>
          <div>
            <span className={labelCls}>On failure</span>
            <div className="flex h-9 items-center gap-4">
              <label className="flex items-center gap-1.5 text-[13px] text-fg2">
                <input
                  type="radio"
                  name="hook-on-failure"
                  value="allow"
                  checked={form.onFailure === 'allow'}
                  onChange={() => patch({ onFailure: 'allow' })}
                  data-testid="radio-hook-failure-allow"
                />
                Continue on failure
              </label>
              <label className="flex items-center gap-1.5 text-[13px] text-fg2">
                <input
                  type="radio"
                  name="hook-on-failure"
                  value="block"
                  checked={form.onFailure === 'block'}
                  onChange={() => patch({ onFailure: 'block' })}
                  data-testid="radio-hook-failure-block"
                />
                Fail closed
              </label>
            </div>
            <p className="mt-1 text-[11px] leading-4 text-muted">
              What happens when the handler times out or errors.
            </p>
          </div>
        </div>

        <div className="flex items-center justify-between rounded-md border border-line px-3 py-2.5">
          <div>
            <p className="text-[13px] font-medium text-fg">Enabled</p>
            <p className="text-[11px] text-muted">Disabled hooks stay defined but never fire.</p>
          </div>
          <Toggle on={form.enabled} label="Enable this hook" onChange={(enabled: boolean) => patch({ enabled })} />
        </div>

        {!saved && (
          <div className="rounded-md border border-line" data-testid="hook-test-panel">
            <button
              type="button"
              onClick={() => setTestOpen(!testOpen)}
              aria-expanded={testOpen}
              data-testid="btn-hook-test-toggle"
              className="flex w-full items-center justify-between px-3 py-2.5 text-left text-[13px] font-medium text-fg"
            >
              <span>Test this hook</span>
              <Icon name={testOpen ? 'up' : 'down'} size={14} className="text-muted" />
            </button>
            {testOpen && (
              <div className="space-y-3 border-t border-line px-3 py-3">
                <p className="flex items-start gap-1.5 text-[11px] leading-4 text-muted" data-testid="hook-test-note">
                  <Icon name="alert" size={12} className="mt-0.5 shrink-0" />
                  Executes the real handler against a synthetic event — nothing is recorded.
                </p>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                  <div>
                    <label className={labelCls} htmlFor="hook-test-event">
                      Synthetic event
                    </label>
                    <select
                      id="hook-test-event"
                      className={inputCls}
                      value={testEvent}
                      aria-label="Synthetic event"
                      data-testid="select-hook-test-event"
                      onChange={(e) => setTestEvent(e.target.value as HookEvent)}
                    >
                      {HOOK_EVENTS.map((e) => (
                        <option key={e.value} value={e.value}>
                          {e.label}
                        </option>
                      ))}
                    </select>
                  </div>
                  {(testEvent === 'pre_tool_use' || testEvent === 'post_tool_use') && (
                    <div>
                      <label className={labelCls} htmlFor="hook-test-tool">
                        Tool name
                      </label>
                      <input
                        id="hook-test-tool"
                        className={cx(inputCls, 'font-mono text-[13px]')}
                        value={testTool}
                        aria-label="Tool name"
                        data-testid="input-hook-test-tool"
                        onChange={(e) => setTestTool(e.target.value)}
                      />
                    </div>
                  )}
                  {(testEvent === 'run_started' || testEvent === 'user_prompt_submit') && (
                    <div>
                      <label className={labelCls} htmlFor="hook-test-origin">
                        Origin
                      </label>
                      <select
                        id="hook-test-origin"
                        className={inputCls}
                        value={testOrigin}
                        aria-label="Origin"
                        data-testid="select-hook-test-origin"
                        onChange={(e) => setTestOrigin(e.target.value)}
                      >
                        <option value="user">user</option>
                        <option value="scheduler">scheduler</option>
                        <option value="channel">channel</option>
                      </select>
                    </div>
                  )}
                  {testEvent === 'run_finished' && (
                    <div>
                      <label className={labelCls} htmlFor="hook-test-status">
                        Outcome status
                      </label>
                      <select
                        id="hook-test-status"
                        className={inputCls}
                        value={testStatus}
                        aria-label="Outcome status"
                        data-testid="select-hook-test-status"
                        onChange={(e) => setTestStatus(e.target.value)}
                      >
                        <option value="completed">completed</option>
                        <option value="failed">failed</option>
                        <option value="cancelled">cancelled</option>
                      </select>
                    </div>
                  )}
                </div>
                <div>
                  <p className={cx(labelCls, 'mb-1')}>Event payload preview</p>
                  <pre
                    className="od-scroll max-h-40 overflow-auto rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] p-2 font-mono text-[11px] leading-4 text-fg2"
                    data-testid="hook-test-preview"
                  >
                    {testPreview}
                  </pre>
                </div>
                <button
                  type="button"
                  onClick={() => void runTest()}
                  disabled={testing}
                  data-testid="btn-hook-test-run"
                  className="flex h-8 items-center rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40"
                >
                  {testing ? 'Running…' : 'Run test'}
                </button>
                {testError && (
                  <p className="text-[12px] text-danger" data-testid="hook-test-error">
                    {testError}
                  </p>
                )}
                {testResult && (
                  <div
                    className="rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] px-3 py-2.5"
                    data-testid="hook-test-result"
                  >
                    <div className="flex items-center gap-2">
                      <span
                        className={cx(
                          'rounded-full px-2 py-0.5 font-mono text-[10px] font-semibold',
                          testResult.decision === 'block'
                            ? 'bg-[color-mix(in_oklab,var(--danger)_14%,transparent)] text-danger'
                            : testResult.decision === 'failure'
                              ? 'bg-[color-mix(in_oklab,var(--warn)_16%,transparent)] text-[color-mix(in_oklab,var(--warn),black_30%)]'
                              : 'bg-[color-mix(in_oklab,var(--success)_14%,transparent)] text-[color-mix(in_oklab,var(--success),black_25%)]'
                        )}
                        data-testid="hook-test-decision"
                      >
                        {testResult.decision}
                      </span>
                      <span className="font-mono text-[11px] text-muted">{testResult.duration_ms} ms</span>
                    </div>
                    {testResult.reason && <p className="mt-1.5 break-words text-[12px] text-fg2">{testResult.reason}</p>}
                    {testResult.error && <p className="mt-1.5 break-words text-[12px] text-danger">{testResult.error}</p>}
                    <div className="mt-1.5 flex flex-wrap gap-3 font-mono text-[11px] text-muted">
                      {typeof testResult.detail?.exit_code === 'number' && (
                        <span data-testid="hook-test-exit-code">exit {testResult.detail.exit_code}</span>
                      )}
                      {typeof testResult.detail?.http_status === 'number' && (
                        <span data-testid="hook-test-http-status">HTTP {testResult.detail.http_status}</span>
                      )}
                      {typeof testResult.detail?.token_count === 'number' && (
                        <span data-testid="hook-test-token-count">{testResult.detail.token_count} tokens</span>
                      )}
                    </div>
                    {testResult.console_lines && testResult.console_lines.length > 0 && (
                      <div className="mt-2">
                        <p className={cx(labelCls, 'mb-1')}>Console output</p>
                        <pre
                          className="od-scroll max-h-40 overflow-auto rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] p-2 font-mono text-[11px] leading-4 text-fg2"
                          data-testid="hook-test-console"
                        >
                          {testResult.console_lines.join('\n')}
                        </pre>
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </form>
    </Modal>
  );
}

function buildSecretRows(rows: SecretRowDraft[]): { name: string; value?: string }[] {
  return rows
    .filter((r) => r.name.trim() || r.value.trim() || r.hint)
    .map((r) => {
      const row: { name: string; value?: string } = { name: r.name.trim() };
      // Empty value = keep the stored secret; only a typed value is sent.
      if (r.value.trim() !== '') row.value = r.value.trim();
      return row;
    });
}

interface SecretRowsProps {
  label: string;
  noun: string;
  rows: SecretRowDraft[];
  errors: Record<string, string>;
  onUpdate: (localKey: string, patch: Partial<SecretRowDraft>) => void;
  onAdd: () => void;
  onRemove: (localKey: string) => void;
  nameTestId: string;
  valueTestId: string;
  rowTestId: string;
  addTestId: string;
  removeTestId: string;
}

// Single-line name/value rows (the MCP server dialog pattern). The value input
// is a password field that never echoes the stored secret — the stored hint
// renders in the placeholder, and an empty submit keeps the secret.
function SecretRows({
  label,
  noun,
  rows,
  onUpdate,
  onAdd,
  onRemove,
  nameTestId,
  valueTestId,
  rowTestId,
  addTestId,
  removeTestId,
}: SecretRowsProps) {
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
                  className={cx(rowInputCls, 'h-8 w-28 sm:w-40 shrink-0')}
                  value={row.name}
                  data-od-id={nameTestId}
                  data-testid={nameTestId}
                  onChange={(e) => onUpdate(row.localKey, { name: e.target.value })}
                />
                <input
                  aria-label={`${capNoun} value`}
                  type="password"
                  placeholder={row.hint ? `•••• ${row.hint} — leave unchanged to keep` : 'leave unchanged to keep'}
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
