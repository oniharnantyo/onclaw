import React, { useState } from 'react';
import {
  Plus,
  Trash,
  PencilSimple,
  Robot,
  X,
  Cpu,
  Wrench,
  Lightning,
  Star,
} from '@phosphor-icons/react';
import type { Provider } from './Providers';

export interface Agent {
  name: string;
  provider: string;
  model: string;
  model_metadata: string;
  reasoning_effort: string;
  reasoning_budget_tokens: number;
  system_prompt: string;
  workspace: string;
  tools: string;
  max_iterations: number;
  is_default: boolean;
}

interface AgentsProps {
  agents: Agent[];
  providers: Provider[];
  loadAgents: () => void;
  showToast: (msg: string, type?: 'success' | 'error') => void;
}

const DEFAULT_FORM = {
  name: '',
  provider: '',
  model: '',
  system_prompt: 'You are a helpful coding assistant.',
  reasoning_effort: '',
  max_iterations: 20,
  tools: 'shell',
  is_default: false,
};

export default function Agents({ agents, providers, loadAgents, showToast }: AgentsProps) {
  const [showModal, setShowModal] = useState(false);
  const [editingAgent, setEditingAgent] = useState<Agent | null>(null);
  const [agentForm, setAgentForm] = useState(DEFAULT_FORM);
  const [isSaving, setIsSaving] = useState(false);

  const openCreate = () => {
    setEditingAgent(null);
    setAgentForm({ ...DEFAULT_FORM, provider: providers[0]?.name || '' });
    setShowModal(true);
  };

  const openEdit = (a: Agent) => {
    setEditingAgent(a);
    setAgentForm({
      name: a.name,
      provider: a.provider,
      model: a.model,
      system_prompt: a.system_prompt,
      reasoning_effort: a.reasoning_effort,
      max_iterations: a.max_iterations,
      tools: a.tools,
      is_default: a.is_default,
    });
    setShowModal(true);
  };

  const closeModal = () => {
    setShowModal(false);
    setEditingAgent(null);
  };

  const saveAgent = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSaving(true);
    const url = editingAgent ? `/api/agents/${editingAgent.name}` : '/api/agents';
    const method = editingAgent ? 'PUT' : 'POST';
    try {
      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(agentForm),
      });
      if (res.ok) {
        showToast(`Agent ${editingAgent ? 'updated' : 'created'} successfully`);
        closeModal();
        loadAgents();
      } else {
        const data = await res.json();
        showToast(data.error || 'Failed to save agent', 'error');
      }
    } catch {
      showToast('Failed to save agent', 'error');
    } finally {
      setIsSaving(false);
    }
  };

  const deleteAgent = async (name: string) => {
    if (!confirm(`Delete agent "${name}"? This cannot be undone.`)) return;
    try {
      const res = await fetch(`/api/agents/${name}`, { method: 'DELETE' });
      if (res.ok) {
        showToast('Agent deleted');
        loadAgents();
      } else {
        showToast('Failed to delete agent', 'error');
      }
    } catch {
      showToast('Failed to delete agent', 'error');
    }
  };

  const set = (field: keyof typeof DEFAULT_FORM) =>
    (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) =>
      setAgentForm({ ...agentForm, [field]: e.target.value });

  return (
    <div className="page-container">
      {/* Toolbar */}
      <div className="page-toolbar">
        <div className="page-toolbar-left">
          <span className="badge badge-inactive" aria-label={`${agents.length} agents`}>
            {agents.length} agent{agents.length !== 1 ? 's' : ''}
          </span>
        </div>
        <button
          id="add-agent-btn"
          className="btn btn-primary btn-sm"
          onClick={openCreate}
        >
          <Plus size={14} weight="bold" aria-hidden />
          Add Agent
        </button>
      </div>

      {/* Agent cards */}
      {agents.length === 0 ? (
        <div className="empty-state" style={{ paddingTop: '80px' }}>
          <div className="empty-state-icon" aria-hidden="true">
            <Robot size={40} weight="duotone" />
          </div>
          <h3>No agents configured</h3>
          <p>Create an agent to start routing AI conversations through your providers.</p>
          <button className="btn btn-primary btn-sm" onClick={openCreate} style={{ marginTop: '8px' }}>
            <Plus size={14} weight="bold" aria-hidden />
            Add your first agent
          </button>
        </div>
      ) : (
        <div className="grid" role="list" aria-label="AI agents">
          {agents.map((a) => (
            <div key={a.name} className="card" role="listitem" aria-label={`Agent: ${a.name}`}>
              {/* Card header */}
              <div className="card-title">
                <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                  <div style={{
                    width: '32px',
                    height: '32px',
                    borderRadius: '6px',
                    background: 'var(--accent-muted)',
                    border: '1px solid rgba(34,197,94,0.15)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    color: 'var(--accent)',
                    flexShrink: 0,
                  }} aria-hidden="true">
                    <Robot size={16} weight="duotone" />
                  </div>
                  <span style={{ fontSize: '14px' }}>{a.name}</span>
                </div>
                {a.is_default && (
                  <span className="badge badge-default" aria-label="Default agent">
                    <Star size={9} weight="fill" aria-hidden />
                    Default
                  </span>
                )}
              </div>

              {/* Details */}
              <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <Cpu size={12} weight="duotone" style={{ color: 'var(--text-muted)', flexShrink: 0 }} aria-hidden />
                  <span className="card-meta">{a.provider} · <code style={{ fontSize: '11.5px' }}>{a.model}</code></span>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <Wrench size={12} weight="duotone" style={{ color: 'var(--text-muted)', flexShrink: 0 }} aria-hidden />
                  <span className="card-meta">{a.tools || 'no tools'}</span>
                </div>
                {a.reasoning_effort && (
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                    <Lightning size={12} weight="duotone" style={{ color: 'var(--text-muted)', flexShrink: 0 }} aria-hidden />
                    <span className="card-meta">Reasoning: {a.reasoning_effort}</span>
                  </div>
                )}
              </div>

              {/* Actions */}
              <div className="card-actions">
                <button
                  id={`edit-agent-${a.name}`}
                  className="btn btn-secondary btn-sm"
                  onClick={() => openEdit(a)}
                  aria-label={`Edit agent ${a.name}`}
                >
                  <PencilSimple size={13} weight="bold" aria-hidden />
                  Edit
                </button>
                <button
                  id={`delete-agent-${a.name}`}
                  className="btn btn-danger btn-sm"
                  onClick={() => deleteAgent(a.name)}
                  aria-label={`Delete agent ${a.name}`}
                >
                  <Trash size={13} weight="bold" aria-hidden />
                  Delete
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Modal */}
      {showModal && (
        <div
          className="modal-overlay"
          onClick={(e) => { if (e.target === e.currentTarget) closeModal(); }}
          role="dialog"
          aria-modal="true"
          aria-labelledby="agent-modal-title"
        >
          <form className="modal-content" onSubmit={saveAgent} noValidate>
            <div className="modal-header">
              <h2 id="agent-modal-title" className="modal-title">
                {editingAgent ? `Edit: ${editingAgent.name}` : 'Add AI Agent'}
              </h2>
              <button
                type="button"
                className="modal-close"
                onClick={closeModal}
                aria-label="Close dialog"
              >
                <X size={18} weight="bold" />
              </button>
            </div>

            {!editingAgent && (
              <div className="form-group">
                <label className="form-label" htmlFor="agent-name">Name</label>
                <input
                  id="agent-name"
                  type="text"
                  className="form-input"
                  value={agentForm.name}
                  onChange={set('name')}
                  placeholder="e.g. master, coder, analyst"
                  required
                  autoFocus
                />
              </div>
            )}

            <div className="form-group">
              <label className="form-label" htmlFor="agent-provider">LLM Provider</label>
              <select
                id="agent-provider"
                className="form-select"
                value={agentForm.provider}
                onChange={set('provider')}
                required
              >
                <option value="">Select a provider…</option>
                {providers.map((p) => (
                  <option key={p.name} value={p.name}>{p.name} ({p.provider_type})</option>
                ))}
              </select>
            </div>

            <div className="form-group">
              <label className="form-label" htmlFor="agent-model">Model Name</label>
              <input
                id="agent-model"
                type="text"
                className="form-input"
                value={agentForm.model}
                onChange={set('model')}
                placeholder="e.g. gpt-4o, claude-opus-4-5"
                required
              />
            </div>

            <div className="form-group">
              <label className="form-label" htmlFor="agent-prompt">System Prompt</label>
              <textarea
                id="agent-prompt"
                className="form-textarea"
                value={agentForm.system_prompt}
                onChange={set('system_prompt')}
                placeholder="Describe the agent's role and capabilities…"
                style={{ minHeight: '90px' }}
              />
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px' }}>
              <div className="form-group" style={{ marginBottom: 0 }}>
                <label className="form-label" htmlFor="agent-reasoning">Reasoning Effort</label>
                <select
                  id="agent-reasoning"
                  className="form-select"
                  value={agentForm.reasoning_effort}
                  onChange={set('reasoning_effort')}
                >
                  <option value="">None (default)</option>
                  <option value="low">Low</option>
                  <option value="medium">Medium</option>
                  <option value="high">High</option>
                </select>
              </div>

              <div className="form-group" style={{ marginBottom: 0 }}>
                <label className="form-label" htmlFor="agent-iterations">Max Iterations</label>
                <input
                  id="agent-iterations"
                  type="number"
                  className="form-input"
                  value={agentForm.max_iterations}
                  onChange={(e) => setAgentForm({ ...agentForm, max_iterations: parseInt(e.target.value) || 20 })}
                  min="1"
                  max="100"
                  required
                />
              </div>
            </div>

            <div className="form-group">
              <label className="form-label" htmlFor="agent-tools">Tools (comma-separated)</label>
              <input
                id="agent-tools"
                type="text"
                className="form-input"
                value={agentForm.tools}
                onChange={set('tools')}
                placeholder="e.g. shell, file_read, web_search"
              />
              <span className="form-hint">Leave empty to disable tool use</span>
            </div>

            <label style={{ display: 'flex', alignItems: 'center', gap: '10px', cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={agentForm.is_default}
                onChange={(e) => setAgentForm({ ...agentForm, is_default: e.target.checked })}
                style={{ width: '15px', height: '15px', accentColor: 'var(--accent)', cursor: 'pointer' }}
              />
              <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>
                Set as default agent
              </span>
            </label>

            <div className="modal-footer">
              <button
                type="button"
                className="btn btn-secondary"
                onClick={closeModal}
                disabled={isSaving}
              >
                Cancel
              </button>
              <button
                id="save-agent-btn"
                type="submit"
                className="btn btn-primary"
                disabled={isSaving}
              >
                {isSaving ? 'Saving…' : editingAgent ? 'Save Changes' : 'Create Agent'}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
