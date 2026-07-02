import React, { useState, useEffect } from 'react';
import { useReactTable, getCoreRowModel, flexRender, createColumnHelper } from '@tanstack/react-table';
import {
  Plus,
  Trash,
  X,
  Lightning,
  ClipboardText,
  ToggleLeft,
  ToggleRight,
  Eye,
} from '@phosphor-icons/react';
import type { Agent } from './Agents';
import Tooltip from './Tooltip';
import {
  validateRegex,
  validateCommand,
  validateScript,
  validateTimeout,
} from './hookValidation';
import jsBeautify from 'js-beautify';

const js_beautify = typeof jsBeautify === 'function'
  ? jsBeautify
  : (jsBeautify as any).js_beautify || (jsBeautify as any).default;

export interface Hook {
  id: string;
  name: string;
  scope: string;
  event: string;
  handler_type: string;
  config: string;
  matcher?: string;
  timeout_ms: number;
  on_timeout: string;
  priority: number;
  enabled: number;
  created_at?: string;
  updated_at?: string;
}

export interface HookExecution {
  id: string;
  hook_id: string;
  event: string;
  agent: string;
  channel: string;
  session_id: string;
  decision: string;
  stdout?: string;
  stderr?: string;
  error_message?: string;
  execution_duration_ms: number;
  executed_at: string;
}

interface HooksProps {
  agents: Agent[];
  showToast: (msg: string, type?: 'success' | 'error') => void;
}

const DEFAULT_SCRIPT_TEMPLATE = `function handle(ctx) {
  // Modify this logic
  return {
    decision: 'allow',
    reason: ''
  };
}`;

export default function Hooks({ agents, showToast }: HooksProps) {
  const [hooks, setHooks] = useState<Hook[]>([]);
  const [executions, setExecutions] = useState<HookExecution[]>([]);
  const safeHooks = hooks || [];
  const safeExecutions = executions || [];
  const [activeSubTab, setActiveSubTab] = useState<'configured' | 'audit'>('configured');
  const [showAddModal, setShowAddModal] = useState(false);
  const [showDetailsModal, setShowDetailsModal] = useState(false);
  const [selectedHook, setSelectedHook] = useState<Hook | null>(null);
  const [selectedExecution, setSelectedExecution] = useState<HookExecution | null>(null);
  const [isProcessing, setIsProcessing] = useState(false);

  // Form Inputs
  const [name, setName] = useState('');
  const [scopeType, setScopeType] = useState('global');
  const [selectedAgent, setSelectedAgent] = useState('');
  const [event, setEvent] = useState('pre_tool_use');
  const [handlerType, setHandlerType] = useState('command');
  const [matcher, setMatcher] = useState('');
  const [timeoutMs, setTimeoutMs] = useState(5000);
  const [onTimeout, setOnTimeout] = useState('block');
  const [priority, setPriority] = useState(0);

  // Handler Specifics
  const [command, setCommand] = useState('');
  const [cwd, setCwd] = useState('');
  const [env, setEnv] = useState('');
  const [script, setScript] = useState(DEFAULT_SCRIPT_TEMPLATE);

  // Validation & Testing States
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [warnings, setWarnings] = useState<Record<string, string>>({});
  const [testResult, setTestResult] = useState<{ decision?: string; error?: string } | null>(null);
  const [isTesting, setIsTesting] = useState(false);
  const [touched, setTouched] = useState<Record<string, boolean>>({});

  useEffect(() => {
    loadHooks();
    loadExecutions();
  }, []);

  // Real-time validation
  useEffect(() => {
    const newErrors: Record<string, string> = {};
    const newWarnings: Record<string, string> = {};

    // Validate Matcher
    const matcherRes = validateRegex(matcher);
    if (!matcherRes.ok) {
      newErrors.matcher = matcherRes.error || 'Invalid regex';
    } else if (matcherRes.warn) {
      newWarnings.matcher = matcherRes.warn;
    }

    // Validate Timeout
    const timeoutRes = validateTimeout(timeoutMs);
    if (!timeoutRes.ok) {
      newErrors.timeoutMs = timeoutRes.error || 'Invalid timeout';
    }

    // Validate Handler
    if (handlerType === 'command') {
      const cmdRes = validateCommand(command);
      if (!cmdRes.ok) {
        newErrors.command = cmdRes.error || 'Invalid command';
      }
    } else if (handlerType === 'script') {
      const scriptRes = validateScript(script);
      if (!scriptRes.ok) {
        newErrors.script = scriptRes.error || 'Invalid script';
      }
    }

    setErrors(newErrors);
    setWarnings(newWarnings);
  }, [matcher, timeoutMs, handlerType, command, script]);

  // Reset test result on input changes
  useEffect(() => {
    setTestResult(null);
  }, [name, scopeType, selectedAgent, event, handlerType, matcher, timeoutMs, onTimeout, priority, command, cwd, env, script]);

  const loadHooks = async () => {
    try {
      const res = await fetch('/api/hooks');
      if (res.ok) {
        setHooks(await res.json());
      }
    } catch {
      showToast('Failed to load hooks', 'error');
    }
  };

  const loadExecutions = async () => {
    try {
      const res = await fetch('/api/hooks/executions');
      if (res.ok) {
        setExecutions(await res.json());
      }
    } catch {
      // Non-critical, ignore
    }
  };

  const resetForm = () => {
    setName('');
    setScopeType('global');
    setSelectedAgent('');
    setEvent('pre_tool_use');
    setHandlerType('command');
    setMatcher('');
    setTimeoutMs(5000);
    setOnTimeout('block');
    setPriority(0);
    setCommand('');
    setCwd('');
    setEnv('');
    setScript(DEFAULT_SCRIPT_TEMPLATE);
    setErrors({});
    setWarnings({});
    setTestResult(null);
    setIsTesting(false);
    setTouched({});
  };

  const validateAll = (): boolean => {
    // Mark all fields as touched to show any missing or invalid required values
    setTouched({
      name: true,
      matcher: true,
      timeoutMs: true,
      command: true,
      script: true,
    });

    const newErrors: Record<string, string> = {};
    const newWarnings: Record<string, string> = {};

    if (!name.trim()) {
      newErrors.name = 'Hook Name is required';
    }

    const matcherRes = validateRegex(matcher);
    if (!matcherRes.ok) {
      newErrors.matcher = matcherRes.error || 'Invalid regex';
    } else if (matcherRes.warn) {
      newWarnings.matcher = matcherRes.warn;
    }

    const timeoutRes = validateTimeout(timeoutMs);
    if (!timeoutRes.ok) {
      newErrors.timeoutMs = timeoutRes.error || 'Invalid timeout';
    }

    if (handlerType === 'command') {
      const cmdRes = validateCommand(command);
      if (!cmdRes.ok) {
        newErrors.command = cmdRes.error || 'Invalid command';
      }
    } else if (handlerType === 'script') {
      const scriptRes = validateScript(script);
      if (!scriptRes.ok) {
        newErrors.script = scriptRes.error || 'Invalid script';
      }
    }

    setErrors(newErrors);
    setWarnings(newWarnings);

    return Object.keys(newErrors).length === 0;
  };

  const buildHookPayload = () => {
    let configJson = '';
    if (handlerType === 'command') {
      const envVars = env.split(',').map(s => s.trim()).filter(Boolean);
      configJson = JSON.stringify({
        command: command.trim(),
        cwd: cwd.trim(),
        allowed_env_vars: envVars,
      });
    } else {
      configJson = JSON.stringify({
        script: script,
      });
    }

    const finalScope = scopeType === 'global' ? 'global' : selectedAgent;

    return {
      name: name.trim(),
      scope: finalScope.trim(),
      event: event.trim(),
      handler_type: handlerType,
      config: configJson,
      matcher: matcher.trim(),
      timeout_ms: Number(timeoutMs),
      on_timeout: onTimeout,
      priority: Number(priority),
    };
  };

  const handleAddHook = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!validateAll()) return;

    const finalScope = scopeType === 'global' ? 'global' : selectedAgent;
    if (scopeType === 'agent' && !finalScope) {
      showToast('Please select an agent', 'error');
      return;
    }

    setIsProcessing(true);
    try {
      const payload = buildHookPayload();
      const res = await fetch('/api/hooks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (res.ok) {
        showToast('Hook created successfully');
        setShowAddModal(false);
        resetForm();
        loadHooks();
      } else {
        const err = await res.json();
        showToast(err.error || 'Failed to add hook', 'error');
      }
    } catch {
      showToast('Failed to add hook', 'error');
    } finally {
      setIsProcessing(false);
    }
  };

  const handleBeautify = () => {
    if (!script) return;
    try {
      const formatted = js_beautify(script, { indent_size: 2 });
      setScript(formatted);
    } catch (err: any) {
      showToast(err.message || 'Failed to beautify script', 'error');
    }
  };

  const handleTestHook = async () => {
    if (!validateAll()) return;

    setIsTesting(true);
    setTestResult(null);

    try {
      const payload = buildHookPayload();
      const res = await fetch('/api/hooks/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (res.ok) {
        const data = await res.json();
        setTestResult({
          decision: data.decision,
          error: data.error,
        });
      } else {
        const err = await res.json();
        setTestResult({
          error: err.error || 'Failed to run test',
        });
      }
    } catch (err: any) {
      setTestResult({
        error: err.message || 'Failed to run test',
      });
    } finally {
      setIsTesting(false);
    }
  };

  const deleteHook = async (id: string) => {
    if (!confirm('Are you sure you want to delete this hook?')) return;

    try {
      const res = await fetch(`/api/hooks/${id}`, { method: 'DELETE' });
      if (res.ok) {
        showToast('Hook deleted successfully');
        loadHooks();
      } else {
        showToast('Failed to delete hook', 'error');
      }
    } catch {
      showToast('Failed to delete hook', 'error');
    }
  };

  const toggleHook = async (id: string, currentlyEnabled: boolean) => {
    try {
      const res = await fetch(`/api/hooks/${id}/toggle`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: !currentlyEnabled }),
      });
      if (res.ok) {
        showToast(`Hook ${!currentlyEnabled ? 'enabled' : 'disabled'} successfully`);
        loadHooks();
      } else {
        showToast('Failed to toggle hook', 'error');
      }
    } catch {
      showToast('Failed to toggle hook', 'error');
    }
  };

  // React Tables
  const hookColumnHelper = createColumnHelper<Hook>();
  const hookColumns = [
    hookColumnHelper.accessor('name', {
      header: 'Name',
      cell: (info) => (
        <div style={{ fontWeight: 600, display: 'flex', alignItems: 'center', gap: '8px' }}>
          <Lightning size={16} weight="duotone" style={{ color: 'var(--accent-color)' }} />
          <span>{info.getValue()}</span>
        </div>
      ),
    }),
    hookColumnHelper.accessor('event', {
      header: 'Event',
      cell: (info) => <code style={{ fontSize: '12px' }}>{info.getValue()}</code>,
    }),
    hookColumnHelper.accessor('scope', {
      header: 'Scope',
      cell: (info) => (
        <span className={`badge ${info.getValue() === 'global' ? 'badge-primary' : 'badge-secondary'}`}>
          {info.getValue()}
        </span>
      ),
    }),
    hookColumnHelper.accessor('handler_type', {
      header: 'Handler',
      cell: (info) => (
        <span style={{ textTransform: 'uppercase', fontSize: '11px', fontWeight: 600 }}>
          {info.getValue()}
        </span>
      ),
    }),
    hookColumnHelper.accessor('priority', {
      header: 'Priority',
      cell: (info) => <span>{info.getValue()}</span>,
    }),
    hookColumnHelper.accessor('enabled', {
      header: 'Status',
      cell: (info) => {
        const hook = info.row.original;
        const enabled = hook.enabled === 1;
        return (
          <button
            onClick={() => toggleHook(hook.id, enabled)}
            style={{
              background: 'none',
              border: 'none',
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              color: enabled ? 'var(--success-color, #10b981)' : 'var(--text-muted)',
              padding: 0,
            }}
            title={enabled ? 'Click to disable' : 'Click to enable'}
          >
            {enabled ? <ToggleRight size={28} weight="fill" /> : <ToggleLeft size={28} />}
          </button>
        );
      },
    }),
    hookColumnHelper.display({
      id: 'actions',
      header: 'Actions',
      cell: (info) => {
        const hook = info.row.original;
        return (
          <div style={{ display: 'flex', gap: '6px' }}>
            <button
              className="btn btn-secondary btn-sm"
              onClick={() => {
                setSelectedHook(hook);
                setShowDetailsModal(true);
              }}
              title="View Hook Details"
              style={{ padding: '4px' }}
            >
              <Eye size={15} />
            </button>
            <button
              className="btn btn-danger btn-sm"
              onClick={() => deleteHook(hook.id)}
              title="Delete Hook"
              style={{ padding: '4px' }}
            >
              <Trash size={15} />
            </button>
          </div>
        );
      },
    }),
  ];

  const hookTable = useReactTable({
    data: safeHooks,
    columns: hookColumns,
    getCoreRowModel: getCoreRowModel(),
  });

  const execColumnHelper = createColumnHelper<HookExecution>();
  const execColumns = [
    execColumnHelper.accessor('executed_at', {
      header: 'Time',
      cell: (info) => <span style={{ fontSize: '12px' }}>{new Date(info.getValue()).toLocaleString()}</span>,
    }),
    execColumnHelper.accessor('hook_id', {
      header: 'Hook ID',
      cell: (info) => <code style={{ fontSize: '11px' }}>{info.getValue()}</code>,
    }),
    execColumnHelper.accessor('event', {
      header: 'Event',
      cell: (info) => <code style={{ fontSize: '11px' }}>{info.getValue()}</code>,
    }),
    execColumnHelper.accessor('agent', {
      header: 'Agent',
      cell: (info) => <span style={{ fontSize: '12px' }}>{info.getValue()}</span>,
    }),
    execColumnHelper.accessor('channel', {
      header: 'Channel',
      cell: (info) => <span className="badge badge-secondary">{info.getValue()}</span>,
    }),
    execColumnHelper.accessor('decision', {
      header: 'Decision',
      cell: (info) => (
        <span className={`badge ${info.getValue() === 'allow' ? 'badge-primary' : 'badge-inactive'}`}>
          {info.getValue()}
        </span>
      ),
    }),
    execColumnHelper.accessor('execution_duration_ms', {
      header: 'Duration',
      cell: (info) => <span style={{ fontSize: '12px' }}>{info.getValue()} ms</span>,
    }),
    execColumnHelper.display({
      id: 'output',
      header: 'Logs',
      cell: (info) => {
        const exec = info.row.original;
        return (
          <button
            className="btn btn-secondary btn-sm"
            onClick={() => {
              setSelectedExecution(exec);
            }}
            title="View execution logs"
          >
            Logs
          </button>
        );
      },
    }),
  ];

  const execTable = useReactTable({
    data: safeExecutions,
    columns: execColumns,
    getCoreRowModel: getCoreRowModel(),
  });

  return (
    <div className="page-container">
      {/* Sub Tabs */}
      <div className="page-toolbar" style={{ borderBottom: '1px solid var(--border)', background: 'rgba(0,0,0,0.05)' }}>
        <div className="page-toolbar-left" style={{ display: 'flex', gap: '8px' }}>
          <button
            className={`btn ${activeSubTab === 'configured' ? 'btn-secondary' : 'btn-sm'}`}
            onClick={() => setActiveSubTab('configured')}
            style={{ fontWeight: activeSubTab === 'configured' ? 600 : 400 }}
          >
            Configured Hooks
          </button>
          <button
            className={`btn ${activeSubTab === 'audit' ? 'btn-secondary' : 'btn-sm'}`}
            onClick={() => setActiveSubTab('audit')}
            style={{ fontWeight: activeSubTab === 'audit' ? 600 : 400 }}
          >
            Audit Trail
          </button>
        </div>
        {activeSubTab === 'configured' && (
          <button
            className="btn btn-primary btn-sm"
            onClick={() => {
              resetForm();
              setShowAddModal(true);
            }}
          >
            <Plus size={14} weight="bold" /> Add Hook
          </button>
        )}
      </div>

      {/* Main Content Pane */}
      <div style={{ padding: '20px 24px', overflow: 'auto', flexGrow: 1 }}>
        {activeSubTab === 'configured' ? (
          safeHooks.length === 0 ? (
            <div className="empty-state">
              <div className="empty-state-icon">
                <Lightning size={40} weight="duotone" />
              </div>
              <h3>No hooks configured</h3>
              <p>Add lifecycle interceptors to enforce custom policies, validate actions, or audit runs.</p>
              <button className="btn btn-primary btn-sm" style={{ marginTop: '8px' }} onClick={() => setShowAddModal(true)}>
                <Plus size={14} weight="bold" /> Configure first hook
              </button>
            </div>
          ) : (
            <div style={{ overflowX: 'auto', border: '1px solid var(--border)', borderRadius: 'var(--radius-md)' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', backgroundColor: 'var(--card)' }}>
                <thead>
                  {hookTable.getHeaderGroups().map(hg => (
                    <tr key={hg.id} style={{ borderBottom: '1px solid var(--border)', background: 'rgba(0,0,0,0.15)' }}>
                      {hg.headers.map(h => (
                        <th key={h.id} scope="col" style={{ padding: '11px 16px', fontSize: '11px', fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', textAlign: 'left' }}>
                          {flexRender(h.column.columnDef.header, h.getContext())}
                        </th>
                      ))}
                    </tr>
                  ))}
                </thead>
                <tbody>
                  {hookTable.getRowModel().rows.map(row => (
                    <tr key={row.id} style={{ borderBottom: '1px solid var(--border)' }} className="table-row-hover">
                      {row.getVisibleCells().map(cell => (
                        <td key={cell.id} style={{ padding: '12px 16px', fontSize: '13px' }}>
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        ) : (
          safeExecutions.length === 0 ? (
            <div className="empty-state">
              <div className="empty-state-icon">
                <ClipboardText size={40} weight="duotone" />
              </div>
              <h3>No executions logged</h3>
              <p>Hook evaluation audit logs will be displayed here once interceptors run.</p>
            </div>
          ) : (
            <div style={{ overflowX: 'auto', border: '1px solid var(--border)', borderRadius: 'var(--radius-md)' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', backgroundColor: 'var(--card)' }}>
                <thead>
                  {execTable.getHeaderGroups().map(hg => (
                    <tr key={hg.id} style={{ borderBottom: '1px solid var(--border)', background: 'rgba(0,0,0,0.15)' }}>
                      {hg.headers.map(h => (
                        <th key={h.id} scope="col" style={{ padding: '11px 16px', fontSize: '11px', fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', textAlign: 'left' }}>
                          {flexRender(h.column.columnDef.header, h.getContext())}
                        </th>
                      ))}
                    </tr>
                  ))}
                </thead>
                <tbody>
                  {execTable.getRowModel().rows.map(row => (
                    <tr key={row.id} style={{ borderBottom: '1px solid var(--border)' }} className="table-row-hover">
                      {row.getVisibleCells().map(cell => (
                        <td key={cell.id} style={{ padding: '12px 16px', fontSize: '13px' }}>
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        )}
      </div>

      {/* Add Hook Modal */}
      {showAddModal && (
        <div
          className="modal-overlay"
          onClick={(e) => { if (e.target === e.currentTarget) setShowAddModal(false); }}
          role="dialog"
          aria-modal="true"
          aria-labelledby="modal-title"
        >
          <form className="modal-content" onSubmit={handleAddHook} style={{ maxWidth: '750px' }} noValidate>
            <div className="modal-header">
              <h2 id="modal-title" className="modal-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <Plus size={20} weight="bold" /> Configure New Hook
              </h2>
              <button type="button" className="modal-close" onClick={() => setShowAddModal(false)} aria-label="Close dialog">
                <X size={18} weight="bold" />
              </button>
            </div>
            <div className="form-group">
              <label className="form-label" htmlFor="hook-name">
                Hook Name *
                <Tooltip content="A unique, descriptive name for the hook." position="bottom" align="left" />
              </label>
              <input id="hook-name" type="text" className="form-input" value={name} onChange={e => setName(e.target.value)} placeholder="e.g. deny-unapproved-tools" required />
            </div>
            <div className="form-row" style={{ display: 'grid', gridTemplateColumns: scopeType === 'agent' ? '1fr 1fr 1fr' : '1fr 1fr', gap: '16px' }}>
              <div className="form-group">
                <label className="form-label" htmlFor="hook-event">
                  Lifecycle Event
                  <Tooltip content="The stage in the execution lifecycle when this hook runs. session_start, user_prompt_submit, pre_tool_use, and stop are blocking; post_tool_use is non-blocking." position="bottom" align="left" />
                </label>
                <select id="hook-event" className="form-select" value={event} onChange={e => setEvent(e.target.value)}>
                  <option value="session_start" title="Runs once when a new session starts. Blocking.">session_start</option>
                  <option value="user_prompt_submit" title="Runs when a user submits a prompt, before the agent processes it. Blocking.">user_prompt_submit</option>
                  <option value="pre_tool_use" title="Runs before a tool is executed. Blocking.">pre_tool_use</option>
                  <option value="post_tool_use" title="Runs after a tool has executed. Non-blocking.">post_tool_use</option>
                  <option value="stop" title="Runs when a session is stopped. Blocking.">stop</option>
                </select>
              </div>
              <div className="form-group">
                <label className="form-label" htmlFor="hook-scope-type">
                  Scope
                  <Tooltip content="Determines if the hook applies globally to all agents or only to a specific agent." position="bottom" align="right" />
                </label>
                <select
                  id="hook-scope-type"
                  className="form-select"
                  value={scopeType}
                  onChange={e => {
                    setScopeType(e.target.value);
                    if (e.target.value === 'agent' && agents.length > 0 && !selectedAgent) {
                      setSelectedAgent(agents[0].name);
                    }
                  }}
                >
                  <option value="global">global</option>
                  <option value="agent">agent</option>
                </select>
              </div>
              {scopeType === 'agent' && (
                <div className="form-group">
                  <label className="form-label" htmlFor="hook-selected-agent">
                    Select Agent
                    <Tooltip content="Choose which agent this hook is active for." position="bottom" align="right" />
                  </label>
                  <select
                    id="hook-selected-agent"
                    className="form-select"
                    value={selectedAgent}
                    onChange={e => setSelectedAgent(e.target.value)}
                    required
                  >
                    <option value="">Choose an agent...</option>
                    {agents.map(a => (
                      <option key={a.name} value={a.name}>{a.name}</option>
                    ))}
                  </select>
                </div>
              )}
            </div>
            <div className="form-row" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px' }}>
              <div className="form-group">
                <label className="form-label" htmlFor="hook-timeout">
                  Timeout (ms)
                  <Tooltip content="Maximum execution time allowed for this hook in milliseconds. Must be between 1 and 10000 ms." position="bottom" align="left" />
                </label>
                <input
                  id="hook-timeout"
                  type="number"
                  className={`form-input ${touched.timeoutMs && errors.timeoutMs ? 'is-invalid' : ''}`}
                  value={timeoutMs}
                  onChange={e => {
                    setTimeoutMs(Number(e.target.value));
                    setTouched(prev => ({ ...prev, timeoutMs: true }));
                  }}
                  aria-invalid={touched.timeoutMs && errors.timeoutMs ? 'true' : 'false'}
                  aria-describedby={touched.timeoutMs && errors.timeoutMs ? 'hook-timeout-error' : undefined}
                />
                {touched.timeoutMs && errors.timeoutMs && (
                  <span id="hook-timeout-error" className="form-error">
                    {errors.timeoutMs}
                  </span>
                )}
              </div>
              <div className="form-group">
                <label className="form-label" htmlFor="hook-on-timeout">
                  Timeout Policy
                  <Tooltip content="Action to take if the hook execution times out. 'block' fails closed; 'allow' fails open." position="bottom" align="right" />
                </label>
                <select id="hook-on-timeout" className="form-select" value={onTimeout} onChange={e => setOnTimeout(e.target.value)}>
                  <option value="block">block</option>
                  <option value="allow">allow</option>
                </select>
              </div>
            </div>
            <div className="form-row" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px' }}>
              <div className="form-group">
                <label className="form-label" htmlFor="hook-priority">
                  Priority
                  <Tooltip content="Execution order for multiple hooks on the same event. Lower numbers execute first." position="bottom" align="left" />
                </label>
                <input id="hook-priority" type="number" className="form-input" value={priority} onChange={e => setPriority(Number(e.target.value))} />
              </div>
              <div className="form-group">
                <label className="form-label" htmlFor="hook-matcher">
                  Tool Name Matcher (Regex)
                  <Tooltip content="RE2 regular expression to match tool names. Applies only to pre_tool_use and post_tool_use events." position="bottom" align="right" />
                </label>
                <input
                  id="hook-matcher"
                  type="text"
                  className={`form-input ${touched.matcher && errors.matcher ? 'is-invalid' : ''}`}
                  value={matcher}
                  onChange={e => {
                    setMatcher(e.target.value);
                    setTouched(prev => ({ ...prev, matcher: true }));
                  }}
                  placeholder="e.g. ^(write_file|run_command)$"
                  aria-invalid={touched.matcher && errors.matcher ? 'true' : 'false'}
                  aria-describedby={touched.matcher && errors.matcher ? 'hook-matcher-error' : undefined}
                />
                {touched.matcher && errors.matcher && (
                  <span id="hook-matcher-error" className="form-error">
                    {errors.matcher}
                  </span>
                )}
                {warnings.matcher && (
                  <span className="form-hint" style={{ color: 'var(--warning)' }}>
                    {warnings.matcher}
                  </span>
                )}
              </div>
            </div>
            <div className="form-group">
              <label className="form-label" htmlFor="hook-handler-type">
                Handler Type
                <Tooltip content="The execution engine for the hook. Shell command runs an external binary; JS Script runs sandboxed JavaScript." position="bottom" align="left" />
              </label>
              <select id="hook-handler-type" className="form-select" value={handlerType} onChange={e => setHandlerType(e.target.value)}>
                <option value="command" title="Runs an external shell command. Exit code 0 allows; non-zero blocks.">Shell Command</option>
                <option value="script" title="Runs sandboxed JavaScript. Must define a handle(ctx) function.">JS Script Sandboxed</option>
              </select>
            </div>

            {handlerType === 'command' ? (
              <>
                <div className="form-group">
                  <label className="form-label" htmlFor="hook-command">
                    Command *
                    <Tooltip content="The shell command to execute. Exit code 0 allows the event; any other exit code blocks it." position="bottom" align="left" />
                  </label>
                  <input
                    id="hook-command"
                    type="text"
                    className={`form-input ${touched.command && errors.command ? 'is-invalid' : ''}`}
                    value={command}
                    onChange={e => {
                      setCommand(e.target.value);
                      setTouched(prev => ({ ...prev, command: true }));
                    }}
                    placeholder="e.g. python3 audit.py"
                    aria-invalid={touched.command && errors.command ? 'true' : 'false'}
                    aria-describedby={touched.command && errors.command ? 'hook-command-error' : undefined}
                    required
                  />
                  {touched.command && errors.command && (
                    <span id="hook-command-error" className="form-error">
                      {errors.command}
                    </span>
                  )}
                </div>
                <div className="form-row" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px' }}>
                  <div className="form-group">
                    <label className="form-label" htmlFor="hook-cwd">
                      Working Directory
                      <Tooltip content="The working directory where the command runs, relative to the workspace root." position="bottom" align="left" />
                    </label>
                    <input id="hook-cwd" type="text" className="form-input" value={cwd} onChange={e => setCwd(e.target.value)} placeholder="relative to workspace" />
                  </div>
                  <div className="form-group">
                    <label className="form-label" htmlFor="hook-env">
                      Allowed Env Vars (comma-separated)
                      <Tooltip content="Comma-separated list of environment variables from the host system allowed in the command environment." position="bottom" align="right" />
                    </label>
                    <input id="hook-env" type="text" className="form-input" value={env} onChange={e => setEnv(e.target.value)} placeholder="e.g. PATH,HOME" />
                  </div>
                </div>
              </>
            ) : (
              <div className="form-group">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '4px' }}>
                  <label className="form-label" htmlFor="hook-script" style={{ marginBottom: 0 }}>
                    JavaScript Source *
                    <Tooltip content="Sandboxed JavaScript script. Must define a function handle(ctx) that returns {decision: 'allow'|'block', reason?: string}." position="bottom" align="left" />
                  </label>
                  <button
                    type="button"
                    className="btn btn-secondary btn-xs"
                    onClick={handleBeautify}
                    disabled={!script}
                  >
                    Beautify
                  </button>
                </div>
                <textarea
                  id="hook-script"
                  className={`form-textarea ${touched.script && errors.script ? 'is-invalid' : ''}`}
                  rows={6}
                  value={script}
                  onChange={e => {
                    setScript(e.target.value);
                    setTouched(prev => ({ ...prev, script: true }));
                  }}
                  placeholder={`function handle(ctx) {
  // Enforce policy on tool calls
  if (ctx.event === "pre_tool_use" && ctx.tool_name === "run_command") {
    return {
      decision: "block",
      reason: "Command execution is disabled in this environment"
    };
  }
  return { decision: "allow" };
}`}
                  style={{ fontFamily: 'monospace', fontSize: '13px' }}
                  aria-invalid={touched.script && errors.script ? 'true' : 'false'}
                  aria-describedby={touched.script && errors.script ? 'hook-script-error' : undefined}
                  required
                />
                {touched.script && errors.script && (
                  <span id="hook-script-error" className="form-error">
                    {errors.script}
                  </span>
                )}
              </div>
            )}

            {testResult && (
              <div style={{ padding: '0 24px 16px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                  <span style={{ fontSize: '13px', fontWeight: 500 }}>Test Result:</span>
                  {testResult.decision && (
                    <span className={`badge ${testResult.decision === 'allow' ? 'badge-active' : 'badge-error'}`}>
                      {testResult.decision}
                    </span>
                  )}
                </div>
                {testResult.error && (
                  <div style={{ fontSize: '12px', color: 'var(--error)', backgroundColor: 'var(--error-muted)', border: '1px solid var(--error-border)', borderRadius: 'var(--radius-sm)', padding: '8px 12px', fontFamily: 'monospace' }}>
                    {testResult.error}
                  </div>
                )}
              </div>
            )}

            <div className="modal-footer">
              <button type="button" className="btn btn-secondary btn-sm" onClick={() => setShowAddModal(false)}>
                Cancel
              </button>
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                onClick={handleTestHook}
                disabled={isProcessing || isTesting || Object.keys(errors).length > 0}
              >
                {isTesting ? 'Testing...' : 'Test Hook'}
              </button>
              <button
                type="submit"
                className="btn btn-primary btn-sm"
                disabled={isProcessing || isTesting || Object.keys(errors).length > 0}
              >
                {isProcessing ? 'Saving...' : 'Save Hook'}
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Details / Config Modal */}
      {showDetailsModal && selectedHook && (
        <div
          className="modal-overlay"
          onClick={(e) => { if (e.target === e.currentTarget) setShowDetailsModal(false); }}
          role="dialog"
          aria-modal="true"
        >
          <div className="modal-content" style={{ maxWidth: '600px' }}>
            <div className="modal-header">
              <h2 className="modal-title">Hook: {selectedHook.name}</h2>
              <button type="button" className="modal-close" onClick={() => setShowDetailsModal(false)}>
                <X size={18} weight="bold" />
              </button>
            </div>
            <table className="info-table" style={{ width: '100%', marginBottom: '16px' }}>
              <tbody>
                <tr>
                  <td style={{ fontWeight: 600, width: '150px' }}>ID</td>
                  <td><code>{selectedHook.id}</code></td>
                </tr>
                <tr>
                  <td style={{ fontWeight: 600 }}>Event</td>
                  <td><code>{selectedHook.event}</code></td>
                </tr>
                <tr>
                  <td style={{ fontWeight: 600 }}>Scope</td>
                  <td>{selectedHook.scope}</td>
                </tr>
                <tr>
                  <td style={{ fontWeight: 600 }}>Handler Type</td>
                  <td>{selectedHook.handler_type}</td>
                </tr>
                <tr>
                  <td style={{ fontWeight: 600 }}>Timeout (ms)</td>
                  <td>{selectedHook.timeout_ms} ms</td>
                </tr>
                <tr>
                  <td style={{ fontWeight: 600 }}>On Timeout</td>
                  <td>{selectedHook.on_timeout}</td>
                </tr>
                <tr>
                  <td style={{ fontWeight: 600 }}>Priority</td>
                  <td>{selectedHook.priority}</td>
                </tr>
                {selectedHook.matcher && (
                  <tr>
                    <td style={{ fontWeight: 600 }}>Tool Matcher</td>
                    <td><code>{selectedHook.matcher}</code></td>
                  </tr>
                )}
              </tbody>
            </table>

            <div style={{ marginTop: '12px' }}>
              <span style={{ fontWeight: 600, fontSize: '13px', display: 'block', marginBottom: '6px' }}>Configuration Payload:</span>
              <pre style={{
                padding: '12px',
                borderRadius: 'var(--radius-sm)',
                backgroundColor: 'rgba(0,0,0,0.3)',
                fontSize: '12px',
                color: 'var(--text)',
                overflowX: 'auto',
                fontFamily: 'monospace',
                whiteSpace: 'pre-wrap'
              }}>
                {JSON.stringify(JSON.parse(selectedHook.config), null, 2)}
              </pre>
            </div>
            <div className="modal-footer" style={{ marginTop: '16px' }}>
              <button className="btn btn-secondary btn-sm" onClick={() => setShowDetailsModal(false)}>
                Close
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Logs Modal */}
      {selectedExecution && (
        <div
          className="modal-overlay"
          onClick={(e) => { if (e.target === e.currentTarget) setSelectedExecution(null); }}
          role="dialog"
          aria-modal="true"
        >
          <div className="modal-content" style={{ maxWidth: '650px' }}>
            <div className="modal-header">
              <h2 className="modal-title">Execution Log Details</h2>
              <button type="button" className="modal-close" onClick={() => setSelectedExecution(null)}>
                <X size={18} weight="bold" />
              </button>
            </div>
            <table className="info-table" style={{ width: '100%', marginBottom: '16px' }}>
              <tbody>
                <tr>
                  <td style={{ fontWeight: 600, width: '150px' }}>Hook ID</td>
                  <td><code>{selectedExecution.hook_id}</code></td>
                </tr>
                <tr>
                  <td style={{ fontWeight: 600 }}>Session ID</td>
                  <td><code>{selectedExecution.session_id}</code></td>
                </tr>
                <tr>
                  <td style={{ fontWeight: 600 }}>Event Trigger</td>
                  <td><code>{selectedExecution.event}</code></td>
                </tr>
                <tr>
                  <td style={{ fontWeight: 600 }}>Decision</td>
                  <td>
                    <span className={`badge ${selectedExecution.decision === 'allow' ? 'badge-primary' : 'badge-inactive'}`}>
                      {selectedExecution.decision}
                    </span>
                  </td>
                </tr>
              </tbody>
            </table>

            {selectedExecution.error_message && (
              <div style={{ marginTop: '12px' }}>
                <span style={{ fontWeight: 600, fontSize: '13px', display: 'block', color: 'red' }}>Error:</span>
                <pre style={{ padding: '8px', borderRadius: '4px', background: 'rgba(255,0,0,0.1)', color: '#ff8888', fontFamily: 'monospace', fontSize: '12px' }}>
                  {selectedExecution.error_message}
                </pre>
              </div>
            )}

            {selectedExecution.stdout && (
              <div style={{ marginTop: '12px' }}>
                <span style={{ fontWeight: 600, fontSize: '13px', display: 'block' }}>Stdout Output:</span>
                <pre style={{ padding: '8px', borderRadius: '4px', background: 'rgba(0,0,0,0.3)', fontFamily: 'monospace', fontSize: '12px', overflowX: 'auto' }}>
                  {selectedExecution.stdout}
                </pre>
              </div>
            )}

            {selectedExecution.stderr && (
              <div style={{ marginTop: '12px' }}>
                <span style={{ fontWeight: 600, fontSize: '13px', display: 'block', color: 'orange' }}>Stderr Output:</span>
                <pre style={{ padding: '8px', borderRadius: '4px', background: 'rgba(0,0,0,0.3)', color: '#ffbb88', fontFamily: 'monospace', fontSize: '12px', overflowX: 'auto' }}>
                  {selectedExecution.stderr}
                </pre>
              </div>
            )}
            <div className="modal-footer" style={{ marginTop: '16px' }}>
              <button className="btn btn-secondary btn-sm" onClick={() => setSelectedExecution(null)}>
                Close
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
