import React, { useState } from 'react';
import { useReactTable, getCoreRowModel, flexRender, createColumnHelper } from '@tanstack/react-table';
import {
  Plus,
  Trash,
  PencilSimple,
  Key,
  X,
  Globe,
  Star,
  ShieldCheck,
  ShieldWarning,
  PlugsConnected,
} from '@phosphor-icons/react';

export interface Provider {
  name: string;
  provider_type: string;
  api_base: string;
  settings: string;
  enabled: boolean;
  is_default: boolean;
  secret_set: boolean;
}

interface ProvidersProps {
  providers: Provider[];
  loadProviders: () => void;
  showToast: (msg: string, type?: 'success' | 'error') => void;
}

const DEFAULT_PROVIDER_FORM = {
  name: '',
  provider_type: 'openai',
  api_base: '',
  settings: '{}',
  enabled: true,
};

export default function Providers({ providers, loadProviders, showToast }: ProvidersProps) {
  const [showProviderModal, setShowProviderModal] = useState(false);
  const [editingProvider, setEditingProvider] = useState<Provider | null>(null);
  const [providerForm, setProviderForm] = useState(DEFAULT_PROVIDER_FORM);
  const [isSaving, setIsSaving] = useState(false);

  const [showSecretModal, setShowSecretModal] = useState(false);
  const [secretProviderName, setSecretProviderName] = useState('');
  const [secretKeyInput, setSecretKeyInput] = useState('');
  const [secretStatus, setSecretStatus] = useState<{ set: boolean; hint: string }>({ set: false, hint: '' });
  const [isSavingSecret, setIsSavingSecret] = useState(false);

  const saveProvider = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSaving(true);
    const url = editingProvider ? `/api/providers/${editingProvider.name}` : '/api/providers';
    const method = editingProvider ? 'PUT' : 'POST';
    try {
      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(providerForm),
      });
      if (res.ok) {
        showToast(`Provider ${editingProvider ? 'updated' : 'created'} successfully`);
        setShowProviderModal(false);
        loadProviders();
      } else {
        const data = await res.json();
        showToast(data.error || 'Failed to save provider', 'error');
      }
    } catch {
      showToast('Failed to save provider', 'error');
    } finally {
      setIsSaving(false);
    }
  };

  const deleteProvider = async (name: string) => {
    if (!confirm(`Delete provider "${name}"? This cannot be undone.`)) return;
    try {
      const res = await fetch(`/api/providers/${name}`, { method: 'DELETE' });
      if (res.ok) {
        showToast('Provider deleted');
        loadProviders();
      } else {
        showToast('Failed to delete provider', 'error');
      }
    } catch {
      showToast('Failed to delete provider', 'error');
    }
  };

  const setDefaultProvider = async (name: string) => {
    try {
      const res = await fetch(`/api/providers/${name}/default`, { method: 'POST' });
      if (res.ok) {
        showToast(`Default provider set to ${name}`);
        loadProviders();
      }
    } catch {
      showToast('Failed to set default provider', 'error');
    }
  };

  const openSecretModal = async (name: string) => {
    setSecretProviderName(name);
    setSecretKeyInput('');
    setShowSecretModal(true);
    try {
      const res = await fetch(`/api/providers/${name}/secret`);
      if (res.ok) {
        setSecretStatus(await res.json());
      }
    } catch {
      setSecretStatus({ set: false, hint: '' });
    }
  };

  const saveSecret = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSavingSecret(true);
    try {
      const res = await fetch(`/api/providers/${secretProviderName}/secret`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ api_key: secretKeyInput }),
      });
      if (res.ok) {
        showToast('API Key saved successfully');
        setShowSecretModal(false);
        loadProviders();
      } else {
        const data = await res.json();
        showToast(data.error || 'Failed to save API key', 'error');
      }
    } catch {
      showToast('Failed to save API key', 'error');
    } finally {
      setIsSavingSecret(false);
    }
  };

  // TanStack Table
  const columnHelper = createColumnHelper<Provider>();
  const columns = [
    columnHelper.accessor('name', {
      header: 'Name',
      cell: (info) => (
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <div style={{
            width: '28px',
            height: '28px',
            borderRadius: '6px',
            background: 'var(--accent-muted)',
            border: '1px solid rgba(34,197,94,0.12)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'var(--accent)',
            flexShrink: 0,
          }} aria-hidden="true">
            <PlugsConnected size={14} weight="duotone" />
          </div>
          <div>
            <span style={{ fontWeight: 600, color: 'var(--text-bright)', fontSize: '13.5px' }}>
              {info.getValue()}
            </span>
            {info.row.original.is_default && (
              <span className="badge badge-default" style={{ marginLeft: '8px', fontSize: '10px' }}>
                <Star size={8} weight="fill" aria-hidden />
                Default
              </span>
            )}
          </div>
        </div>
      ),
    }),
    columnHelper.accessor('provider_type', {
      header: 'Type',
      cell: (info) => <code>{info.getValue()}</code>,
    }),
    columnHelper.accessor('api_base', {
      header: 'API Base',
      cell: (info) => info.getValue()
        ? <span style={{ fontFamily: 'var(--mono)', fontSize: '11.5px', color: 'var(--text)' }}>{info.getValue()}</span>
        : <span style={{ color: 'var(--text-subtle)', fontStyle: 'italic', fontSize: '12px' }}>default</span>,
    }),
    columnHelper.accessor('enabled', {
      header: 'Status',
      cell: (info) => (
        <span className={`badge ${info.getValue() ? 'badge-active' : 'badge-inactive'}`}>
          <span className="dot" aria-hidden />
          {info.getValue() ? 'Enabled' : 'Disabled'}
        </span>
      ),
    }),
    columnHelper.accessor('secret_set', {
      header: 'API Key',
      cell: (info) => (
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          {info.getValue() ? (
            <span style={{ display: 'flex', alignItems: 'center', gap: '4px', color: 'var(--success)', fontSize: '12px' }}>
              <ShieldCheck size={13} weight="fill" aria-hidden />
              Configured
            </span>
          ) : (
            <span style={{ display: 'flex', alignItems: 'center', gap: '4px', color: 'var(--warning)', fontSize: '12px' }}>
              <ShieldWarning size={13} weight="fill" aria-hidden />
              Missing
            </span>
          )}
          <button
            className="btn btn-ghost btn-xs"
            onClick={() => openSecretModal(info.row.original.name)}
            aria-label={`Set API key for ${info.row.original.name}`}
            title="Set API key"
          >
            <Key size={12} weight="bold" aria-hidden />
          </button>
        </div>
      ),
    }),
    columnHelper.display({
      id: 'actions',
      header: 'Actions',
      cell: (info) => {
        const p = info.row.original;
        return (
          <div style={{ display: 'flex', gap: '6px' }}>
            <button
              id={`edit-provider-${p.name}`}
              className="btn btn-secondary btn-xs"
              onClick={() => {
                setEditingProvider(p);
                setProviderForm({
                  name: p.name,
                  provider_type: p.provider_type,
                  api_base: p.api_base,
                  settings: p.settings,
                  enabled: p.enabled,
                });
                setShowProviderModal(true);
              }}
              aria-label={`Edit ${p.name}`}
              title="Edit"
            >
              <PencilSimple size={12} weight="bold" aria-hidden />
            </button>
            {!p.is_default && (
              <button
                id={`default-provider-${p.name}`}
                className="btn btn-ghost btn-xs"
                onClick={() => setDefaultProvider(p.name)}
                aria-label={`Set ${p.name} as default`}
                title="Set as default"
              >
                <Star size={12} weight="bold" aria-hidden />
              </button>
            )}
            <button
              id={`delete-provider-${p.name}`}
              className="btn btn-danger btn-xs"
              onClick={() => deleteProvider(p.name)}
              aria-label={`Delete ${p.name}`}
              title="Delete"
            >
              <Trash size={12} weight="bold" aria-hidden />
            </button>
          </div>
        );
      },
    }),
  ];

  const table = useReactTable({
    data: providers,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  return (
    <div className="page-container">
      {/* Toolbar */}
      <div className="page-toolbar">
        <div className="page-toolbar-left">
          <span className="badge badge-inactive">
            {providers.length} provider{providers.length !== 1 ? 's' : ''}
          </span>
        </div>
        <button
          id="add-provider-btn"
          className="btn btn-primary btn-sm"
          onClick={() => {
            setEditingProvider(null);
            setProviderForm(DEFAULT_PROVIDER_FORM);
            setShowProviderModal(true);
          }}
        >
          <Plus size={14} weight="bold" aria-hidden />
          Add Provider
        </button>
      </div>

      {/* Table or empty */}
      <div style={{ padding: '20px 24px', overflow: 'auto', flexGrow: 1 }}>
        {providers.length === 0 ? (
          <div className="empty-state">
            <div className="empty-state-icon" aria-hidden="true">
              <Globe size={40} weight="duotone" />
            </div>
            <h3>No providers configured</h3>
            <p>Add an LLM provider to connect agents to AI models like GPT-4, Claude, or Ollama.</p>
            <button
              className="btn btn-primary btn-sm"
              style={{ marginTop: '8px' }}
              onClick={() => {
                setEditingProvider(null);
                setProviderForm(DEFAULT_PROVIDER_FORM);
                setShowProviderModal(true);
              }}
            >
              <Plus size={14} weight="bold" aria-hidden />
              Add first provider
            </button>
          </div>
        ) : (
          <div style={{
            overflowX: 'auto',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-md)',
          }}>
            <table
              style={{
                width: '100%',
                borderCollapse: 'collapse',
                backgroundColor: 'var(--card)',
              }}
              aria-label="LLM Providers"
            >
              <thead>
                {table.getHeaderGroups().map((hg) => (
                  <tr key={hg.id} style={{ borderBottom: '1px solid var(--border)', background: 'rgba(0,0,0,0.15)' }}>
                    {hg.headers.map((h) => (
                      <th
                        key={h.id}
                        scope="col"
                        style={{
                          padding: '11px 16px',
                          fontSize: '11px',
                          fontWeight: 600,
                          color: 'var(--text-muted)',
                          textTransform: 'uppercase',
                          letterSpacing: '0.08em',
                          textAlign: 'left',
                          whiteSpace: 'nowrap',
                        }}
                      >
                        {flexRender(h.column.columnDef.header, h.getContext())}
                      </th>
                    ))}
                  </tr>
                ))}
              </thead>
              <tbody>
                {table.getRowModel().rows.map((row, idx) => (
                  <tr
                    key={row.id}
                    style={{
                      borderBottom: idx < table.getRowModel().rows.length - 1
                        ? '1px solid var(--border-soft)'
                        : 'none',
                      transition: 'background var(--t-fast)',
                    }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = 'var(--card-hover)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ''; }}
                  >
                    {row.getVisibleCells().map((cell) => (
                      <td
                        key={cell.id}
                        style={{ padding: '11px 16px', color: 'var(--text)', verticalAlign: 'middle' }}
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Provider modal */}
      {showProviderModal && (
        <div
          className="modal-overlay"
          onClick={(e) => { if (e.target === e.currentTarget) setShowProviderModal(false); }}
          role="dialog"
          aria-modal="true"
          aria-labelledby="provider-modal-title"
        >
          <form className="modal-content" onSubmit={saveProvider} noValidate>
            <div className="modal-header">
              <h2 id="provider-modal-title" className="modal-title">
                {editingProvider ? `Edit: ${editingProvider.name}` : 'Add LLM Provider'}
              </h2>
              <button
                type="button"
                className="modal-close"
                onClick={() => setShowProviderModal(false)}
                aria-label="Close dialog"
              >
                <X size={18} weight="bold" />
              </button>
            </div>

            {!editingProvider && (
              <div className="form-group">
                <label className="form-label" htmlFor="prov-name">Name</label>
                <input
                  id="prov-name"
                  type="text"
                  className="form-input"
                  value={providerForm.name}
                  onChange={(e) => setProviderForm({ ...providerForm, name: e.target.value })}
                  placeholder="e.g. openai-prod, anthropic-main"
                  required
                  autoFocus
                />
              </div>
            )}

            <div className="form-group">
              <label className="form-label" htmlFor="prov-type">Provider Type</label>
              <select
                id="prov-type"
                className="form-select"
                value={providerForm.provider_type}
                onChange={(e) => setProviderForm({ ...providerForm, provider_type: e.target.value })}
              >
                <option value="openai">openai</option>
                <option value="anthropic">anthropic</option>
                <option value="ollama">ollama</option>
                <option value="openai-compatible">openai-compatible</option>
              </select>
            </div>

            <div className="form-group">
              <label className="form-label" htmlFor="prov-api-base">API Base URL</label>
              <input
                id="prov-api-base"
                type="url"
                className="form-input"
                value={providerForm.api_base}
                onChange={(e) => setProviderForm({ ...providerForm, api_base: e.target.value })}
                placeholder="Leave blank to use provider default"
              />
            </div>

            <div className="form-group">
              <label className="form-label" htmlFor="prov-settings">Settings (JSON)</label>
              <textarea
                id="prov-settings"
                className="form-textarea"
                value={providerForm.settings}
                onChange={(e) => setProviderForm({ ...providerForm, settings: e.target.value })}
                placeholder="{}"
                style={{ minHeight: '80px', fontFamily: 'var(--mono)', fontSize: '12.5px' }}
              />
            </div>

            <label style={{ display: 'flex', alignItems: 'center', gap: '10px', cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={providerForm.enabled}
                onChange={(e) => setProviderForm({ ...providerForm, enabled: e.target.checked })}
                style={{ width: '15px', height: '15px', accentColor: 'var(--accent)', cursor: 'pointer' }}
              />
              <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>Enable this provider</span>
            </label>

            <div className="modal-footer">
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setShowProviderModal(false)}
                disabled={isSaving}
              >
                Cancel
              </button>
              <button
                id="save-provider-btn"
                type="submit"
                className="btn btn-primary"
                disabled={isSaving}
              >
                {isSaving ? 'Saving…' : editingProvider ? 'Save Changes' : 'Create Provider'}
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Secret key modal */}
      {showSecretModal && (
        <div
          className="modal-overlay"
          onClick={(e) => { if (e.target === e.currentTarget) setShowSecretModal(false); }}
          role="dialog"
          aria-modal="true"
          aria-labelledby="secret-modal-title"
        >
          <form className="modal-content" onSubmit={saveSecret} noValidate>
            <div className="modal-header">
              <h2 id="secret-modal-title" className="modal-title">
                API Key — {secretProviderName}
              </h2>
              <button
                type="button"
                className="modal-close"
                onClick={() => setShowSecretModal(false)}
                aria-label="Close dialog"
              >
                <X size={18} weight="bold" />
              </button>
            </div>

            <div style={{
              display: 'flex',
              alignItems: 'center',
              gap: '10px',
              padding: '12px',
              borderRadius: 'var(--radius-sm)',
              background: secretStatus.set ? 'var(--success-muted)' : 'var(--warning-muted)',
              border: `1px solid ${secretStatus.set ? 'rgba(52,211,153,0.2)' : 'rgba(251,191,36,0.2)'}`,
              fontSize: '13px',
            }}>
              {secretStatus.set ? (
                <>
                  <ShieldCheck size={16} weight="fill" style={{ color: 'var(--success)', flexShrink: 0 }} aria-hidden />
                  <span style={{ color: 'var(--text)' }}>
                    API key configured. Hint: <code>{secretStatus.hint}</code>
                  </span>
                </>
              ) : (
                <>
                  <ShieldWarning size={16} weight="fill" style={{ color: 'var(--warning)', flexShrink: 0 }} aria-hidden />
                  <span style={{ color: 'var(--text)' }}>No API key configured for this provider.</span>
                </>
              )}
            </div>

            <div className="form-group">
              <label className="form-label" htmlFor="secret-key-input">
                {secretStatus.set ? 'New API Key (leave blank to keep existing)' : 'API Key / Token'}
              </label>
              <input
                id="secret-key-input"
                type="password"
                className="form-input"
                value={secretKeyInput}
                onChange={(e) => setSecretKeyInput(e.target.value)}
                placeholder="sk-…"
                required
                autoFocus
                autoComplete="off"
              />
              <span className="form-hint">The key is encrypted at rest and never exposed in the UI.</span>
            </div>

            <div className="modal-footer">
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setShowSecretModal(false)}
                disabled={isSavingSecret}
              >
                Cancel
              </button>
              <button
                id="save-secret-btn"
                type="submit"
                className="btn btn-primary"
                disabled={isSavingSecret || !secretKeyInput.trim()}
              >
                {isSavingSecret ? 'Saving…' : 'Save Key'}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
