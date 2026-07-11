import { Link } from 'react-router-dom';
import {
  Plus,
  Trash,
  PencilSimple,
  Robot,
  Cpu,
  Wrench,
  Lightning,
  Star,
  BookOpen,
  Network,
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
  max_context_tokens: number;
  is_default: boolean;
  memory_config: string;
  skills_count: number;
  mcp_count: number;
}

interface AgentsProps {
  agents: Agent[];
  providers: Provider[];
  loadAgents: () => void;
  showToast: (msg: string, type?: 'success' | 'error') => void;
}

export default function Agents({ agents, loadAgents, showToast }: AgentsProps) {
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

  return (
    <div className="page-container">
      {/* Toolbar */}
      <div className="page-toolbar">
        <div className="page-toolbar-left">
          <span className="badge badge-inactive" aria-label={`${agents.length} agents`}>
            {agents.length} agent{agents.length !== 1 ? 's' : ''}
          </span>
        </div>
        <Link
          id="add-agent-btn"
          className="btn btn-primary btn-sm"
          to="/agents/new"
        >
          <Plus size={14} weight="bold" aria-hidden />
          Add Agent
        </Link>
      </div>

      {/* Agent cards */}
      {agents.length === 0 ? (
        <div className="empty-state" style={{ paddingTop: '80px' }}>
          <div className="empty-state-icon" aria-hidden="true">
            <Robot size={40} weight="duotone" />
          </div>
          <h3>No agents configured</h3>
          <p>Create an agent to start routing AI conversations through your providers.</p>
          <Link to="/agents/new" className="btn btn-primary btn-sm" style={{ marginTop: '8px' }}>
            <Plus size={14} weight="bold" aria-hidden />
            Add your first agent
          </Link>
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

                {/* Tools count */}
                <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <Wrench size={12} weight="duotone" style={{ color: 'var(--text-muted)', flexShrink: 0 }} aria-hidden />
                  {(() => {
                    const toolList = a.tools ? a.tools.split(',').filter(t => t.trim()) : [];
                    const toolCount = toolList.length;
                    return (
                      <span className="card-meta">
                        {toolCount === 0 ? 'No tools' : `${toolCount} tool${toolCount !== 1 ? 's' : ''}`}
                      </span>
                    );
                  })()}
                </div>

                {/* Reasoning support indicator */}
                {(() => {
                  let reasoningSupported = false;
                  let reasoningConfig = '';

                  if (a.model_metadata) {
                    try {
                      const meta = JSON.parse(a.model_metadata);
                      reasoningSupported = meta.thinking === true;
                    } catch (e) {
                      // If parsing fails, check reasoning_effort as fallback
                      reasoningSupported = a.reasoning_effort !== undefined && a.reasoning_effort !== '';
                    }
                  }

                  if (a.reasoning_effort) {
                    reasoningConfig = a.reasoning_effort;
                  }

                  return reasoningSupported ? (
                    <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                      <Lightning size={12} weight="duotone" style={{ color: reasoningConfig ? 'var(--accent)' : 'var(--text-muted)', flexShrink: 0 }} aria-hidden />
                      <span className="card-meta">
                        {reasoningConfig ? `Reasoning: ${reasoningConfig}` : 'Reasoning supported'}
                      </span>
                    </div>
                  ) : null;
                })()}

                {/* Skills count */}
                {a.skills_count > 0 && (
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                    <BookOpen size={12} weight="duotone" style={{ color: 'var(--text-muted)', flexShrink: 0 }} aria-hidden />
                    <span className="card-meta">
                      {a.skills_count} skill{a.skills_count !== 1 ? 's' : ''}
                    </span>
                  </div>
                )}

                {/* MCP servers count */}
                {a.mcp_count > 0 && (
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                    <Network size={12} weight="duotone" style={{ color: 'var(--text-muted)', flexShrink: 0 }} aria-hidden />
                    <span className="card-meta">
                      {a.mcp_count} MCP server{a.mcp_count !== 1 ? 's' : ''}
                    </span>
                  </div>
                )}
              </div>

              {/* Actions */}
              <div className="card-actions">
                <Link
                  id={`edit-agent-${a.name}`}
                  className="btn btn-secondary btn-sm"
                  to={`/agents/${a.name}`}
                  aria-label={`Edit agent ${a.name}`}
                >
                  <PencilSimple size={13} weight="bold" aria-hidden />
                  Edit
                </Link>
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
    </div>
  );
}
