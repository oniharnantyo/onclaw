import React, { useState, useEffect } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { ArrowLeft, Cpu, WarningCircle } from '@phosphor-icons/react';
import type { Agent } from '../components/Agents';
import type { Provider } from '../components/Providers';
import Tooltip from '../components/Tooltip';

import Hooks from '../components/Hooks';
import Skills from '../components/Skills';
import type { Skill } from '../components/Skills';
import Tools from '../components/Tools';
import MCP from '../components/MCP';
import PersonaEditor from '../components/PersonaEditor';

interface AgentDetailPageProps {
  agents: Agent[];
  providers: Provider[];
  loadAgents: () => void;
  showToast: (msg: string, type?: 'success' | 'error') => void;
  mode?: 'create' | 'edit';
  skills: Skill[];
  loadSkills: () => void;
}

const DEFAULT_FORM = {
  name: '',
  provider: '',
  model: '',
  system_prompt: 'You are a helpful coding assistant.',
  reasoning_effort: '',
  reasoning_budget_tokens: 0,
  max_iterations: 20,
  max_context_tokens: 0,
  tools: '',
  workspace: '',
  model_metadata: '',
  is_default: false,
};

type TabType = 'overview' | 'hooks' | 'skills' | 'memory' | 'mcp' | 'tools' | 'persona';

export default function AgentDetailPage({
  agents,
  providers,
  loadAgents,
  showToast,
  mode: initialMode,
  skills,
  loadSkills,
}: AgentDetailPageProps) {
  const { name } = useParams<{ name?: string }>();
  const navigate = useNavigate();

  // Determine mode
  let mode: 'create' | 'edit' = 'edit';
  if (initialMode) {
    mode = initialMode;
  } else if (!name) {
    mode = 'create';
  }

  // Determine active tab
  const [activeTab, setActiveTab] = useState<TabType>('overview');
  const [agentForm, setAgentForm] = useState(DEFAULT_FORM);
  const [isSaving, setIsSaving] = useState(false);
  const [hasLoadedAgent, setHasLoadedAgent] = useState(false);
  const [hasContextOverride, setHasContextOverride] = useState(false);

  const [models, setModels] = useState<{
    id: string;
    contextWindow: number;
    thinking: boolean;
    inputModalities: string[];
    reasoningOptions?: {
      type: string;
      values?: string[];
      min?: number;
      max?: number;
    }[];
  }[]>([]);
  const [loadingModels, setLoadingModels] = useState(false);
  const [modelsWarning, setModelsWarning] = useState<string | null>(null);
  const [showModelsDropdown, setShowModelsDropdown] = useState(false);

  useEffect(() => {
    if (!agentForm.provider) {
      setModels([]);
      setModelsWarning(null);
      return;
    }

    const fetchModels = async () => {
      setLoadingModels(true);
      setModelsWarning(null);
      try {
        const res = await fetch(`/api/providers/${encodeURIComponent(agentForm.provider)}/models`);
        if (res.ok) {
          const data = await res.json();
          setModels(data.models || []);
          if (data.warning) {
            setModelsWarning(data.warning);
          }
        } else {
          setModels([]);
          setModelsWarning("Failed to discover models for this provider.");
        }
      } catch {
        setModels([]);
        setModelsWarning("Failed to discover models for this provider.");
      } finally {
        setLoadingModels(false);
      }
    };

    fetchModels();
  }, [agentForm.provider]);

  const handleTabClick = (tabId: TabType) => {
    setActiveTab(tabId);
  };

  // Find agent if in edit mode
  const currentAgent = mode === 'edit' ? agents.find((a) => a.name === name) : null;

  const [memConfig, setMemConfig] = useState({
    curated_enabled: true,
    episodic_enabled: true,
    kg_enabled: true,
    embedding_provider: '',
    embedding_model: '',
    security_scan_enabled: true,
    extraction_enabled: true,
    retrieval_enabled: true,
    dreaming_enabled: true,
    staged_write_approval: false,
  });

  // Sync memory_config from agent row
  useEffect(() => {
    if (currentAgent && currentAgent.memory_config) {
      try {
        const parsed = JSON.parse(currentAgent.memory_config);
        setMemConfig({
          curated_enabled: parsed.curated_enabled !== false,
          episodic_enabled: parsed.episodic_enabled !== false,
          kg_enabled: parsed.kg_enabled !== false,
          embedding_provider: parsed.embedding_provider || '',
          embedding_model: parsed.embedding_model || '',
          security_scan_enabled: parsed.security_scan_enabled !== false,
          extraction_enabled: parsed.extraction_enabled !== false,
          retrieval_enabled: parsed.retrieval_enabled !== false,
          dreaming_enabled: parsed.dreaming_enabled !== false,
          staged_write_approval: parsed.staged_write_approval === true,
        });
      } catch (e) {
        console.error("Failed to parse memory config", e);
      }
    }
  }, [currentAgent]);

  // Initialize form
  useEffect(() => {
    if (mode === 'create') {
      setAgentForm({
        ...DEFAULT_FORM,
        provider: providers[0]?.name || '',
      });
      setHasContextOverride(false);
      setHasLoadedAgent(true);
    } else if (mode === 'edit' && currentAgent) {
      setAgentForm({
        name: currentAgent.name,
        provider: currentAgent.provider,
        model: currentAgent.model,
        system_prompt: currentAgent.system_prompt,
        reasoning_effort: currentAgent.reasoning_effort || '',
        reasoning_budget_tokens: currentAgent.reasoning_budget_tokens || 0,
        max_iterations: currentAgent.max_iterations,
        max_context_tokens: currentAgent.max_context_tokens || 0,
        tools: currentAgent.tools,
        workspace: currentAgent.workspace,
        model_metadata: currentAgent.model_metadata,
        is_default: currentAgent.is_default,
      });
      setHasContextOverride((currentAgent.max_context_tokens || 0) > 0);
      setHasLoadedAgent(true);
    } else if (mode === 'edit' && agents.length > 0 && !currentAgent) {
      // Finished loading agents, but didn't find this one
      setHasLoadedAgent(true);
    }
  }, [mode, currentAgent, agents.length, providers]);

  // Sync model metadata when the selected model changes
  useEffect(() => {
    if (!agentForm.model || loadingModels) return;

    // Check if user changed the model from the initial/saved one
    const isOriginalModel = mode === 'edit' && currentAgent && agentForm.model === currentAgent.model;
    if (isOriginalModel) {
      if (agentForm.model_metadata !== currentAgent.model_metadata) {
        setAgentForm((prev) => ({ ...prev, model_metadata: currentAgent.model_metadata }));
      }
      return;
    }

    const found = models.find((m) => m.id.toLowerCase() === agentForm.model.toLowerCase());
    const targetMetaStr = found
      ? JSON.stringify({
          context_window: found.contextWindow,
          thinking: found.thinking,
          input_modalities: found.inputModalities,
          reasoning_options: found.reasoningOptions || [],
        })
      : JSON.stringify({
          context_window: 0,
          thinking: false,
          input_modalities: ['text'],
          reasoning_options: [],
        });

    if (agentForm.model_metadata !== targetMetaStr) {
      setAgentForm((prev) => ({ ...prev, model_metadata: targetMetaStr }));
    }

    // Auto-update max_context_tokens when model changes if override is enabled
    if (hasContextOverride && found) {
      setAgentForm((prev) => ({
        ...prev,
        max_context_tokens: found.contextWindow,
      }));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentForm.model, models, loadingModels, mode, currentAgent, hasContextOverride]);

  // Log model metadata to browser console for debugging when selected model changes
  useEffect(() => {
    if (agentForm.model) {
      const selectedModelMeta = models.find((m) => m.id === agentForm.model);
      if (selectedModelMeta) {
        console.log("Model Metadata (api.json):", selectedModelMeta);
      }
    }
  }, [agentForm.model, models]);

  if (mode === 'edit' && agents.length === 0 && !hasLoadedAgent) {
    return (
      <div className="loading-screen">
        <div className="loading-spinner">
          <div className="spinner" role="status" aria-label="Loading" />
          <span style={{ color: 'var(--text-muted)', fontSize: '13px' }}>Loading agent details…</span>
        </div>
      </div>
    );
  }

  if (mode === 'edit' && !currentAgent && hasLoadedAgent) {
    return (
      <div className="page-container">
        <div className="empty-state" style={{ paddingTop: '80px' }}>
          <div className="empty-state-icon" aria-hidden="true" style={{ color: 'var(--error)' }}>
            <WarningCircle size={40} weight="duotone" />
          </div>
          <h3>Agent not found</h3>
          <p>The agent "{name}" does not exist in your configuration.</p>
          <Link to="/agents" className="btn btn-secondary btn-sm" style={{ marginTop: '16px' }}>
            Back to Agents
          </Link>
        </div>
      </div>
    );
  }

  const set = (field: keyof typeof DEFAULT_FORM) =>
    (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) =>
      setAgentForm({ ...agentForm, [field]: e.target.value });

  const saveAgent = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSaving(true);
    const url = mode === 'edit' ? `/api/agents/${name}` : '/api/agents';
    const method = mode === 'edit' ? 'PUT' : 'POST';
    try {
      const payload = {
        ...agentForm,
        max_context_tokens: hasContextOverride ? (agentForm.max_context_tokens || 0) : 0,
        memory_config: mode === 'edit' ? JSON.stringify(memConfig) : '{}',
      };
      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (res.ok) {
        showToast(`Agent ${mode === 'edit' ? 'updated' : 'created'} successfully`);
        loadAgents();
        navigate('/agents');
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

  const saveMemoryConfig = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSaving(true);
    try {
      const payload = {
        ...agentForm,
        max_context_tokens: hasContextOverride ? (agentForm.max_context_tokens || 0) : 0,
        memory_config: JSON.stringify(memConfig),
      };
      const res = await fetch(`/api/agents/${name}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (res.ok) {
        showToast('Agent memory configuration saved successfully');
        loadAgents();
      } else {
        const data = await res.json();
        showToast(data.error || 'Failed to save memory configuration', 'error');
      }
    } catch {
      showToast('Failed to save memory configuration', 'error');
    } finally {
      setIsSaving(false);
    }
  };

  // Tabs layout configuration
  const tabsConfig: { id: TabType; label: string }[] = [];
  tabsConfig.push({ id: 'overview', label: 'Overview' });
  if (mode === 'edit') {
    tabsConfig.push(
      { id: 'hooks', label: 'Hooks' },
      { id: 'skills', label: 'Skills' },
      { id: 'memory', label: 'Memory' },
      { id: 'mcp', label: 'MCP' },
      { id: 'tools', label: 'Tools' },
      { id: 'persona', label: 'Persona' }
    );
  }

  const filteredModels = models.filter((m) =>
    m.id.toLowerCase().includes(agentForm.model.toLowerCase())
  );
  const selectedModelMeta = models.find((m) => m.id === agentForm.model);

  // Parse stored model_metadata if available
  let parsedMetadata: typeof models[number] | null = null;
  if (agentForm.model_metadata) {
    try {
      const raw = JSON.parse(agentForm.model_metadata);
      parsedMetadata = {
        id: agentForm.model,
        contextWindow: raw.context_window !== undefined ? raw.context_window : (raw.contextWindow || 0),
        thinking: raw.thinking !== undefined ? raw.thinking : false,
        inputModalities: raw.input_modalities || raw.inputModalities || ['text'],
        reasoningOptions: raw.reasoning_options || raw.reasoningOptions || [],
      };
    } catch {}
  }

  const currentModelMeta = selectedModelMeta || parsedMetadata;
  const reasoningSupported = loadingModels || (currentModelMeta ? currentModelMeta.thinking === true : false);

  const isExactMatch = models.some(
    (m) => m.id.toLowerCase() === agentForm.model.toLowerCase()
  );
  const effortOption = currentModelMeta?.reasoningOptions?.find((o: any) => o.type === 'effort' || o.type === 'select' || (o.values && o.values.length > 0));
  const budgetOption = currentModelMeta?.reasoningOptions?.find((o: any) => o.type === 'range' || o.type === 'budget' || o.type === 'budget_tokens' || o.min !== undefined || o.max !== undefined);
  const toggleOption = currentModelMeta?.reasoningOptions?.find((o: any) => o.type === 'toggle' || o.type === 'checkbox');
  const reasoningValues = effortOption?.values || ['low', 'medium', 'high'];

  return (
    <div className="page-container">
      {/* Detail Header */}
      <div className="page-toolbar" style={{ borderBottom: '1px solid var(--border-soft)', paddingBottom: '16px', marginBottom: '20px' }}>
        <div className="page-toolbar-left" style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
          <button
            onClick={() => navigate('/agents')}
            className="btn btn-secondary btn-sm"
            style={{ padding: '8px', borderRadius: '50%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
            aria-label="Back to agents"
          >
            <ArrowLeft size={16} weight="bold" />
          </button>
          <div>
            <h1 style={{ fontSize: '18px', fontWeight: 600, color: 'var(--text-bright)', margin: 0 }}>
              {mode === 'create' ? 'Create Agent' : `Agent: ${name}`}
            </h1>
            {mode === 'edit' && currentAgent && (
              <span className="card-meta" style={{ display: 'flex', alignItems: 'center', gap: '4px', marginTop: '4px' }}>
                <Cpu size={12} /> {currentAgent.provider} · <code>{currentAgent.model}</code>
              </span>
            )}
          </div>
        </div>
      </div>

      {/* Tabs */}
      {tabsConfig.length > 1 && (
        <div className="tab-container">
          {tabsConfig.map((tab) => (
            <button
              key={tab.id}
              className={`tab-button ${activeTab === tab.id ? 'active' : ''}`}
              onClick={() => handleTabClick(tab.id)}
            >
              {tab.label}
            </button>
          ))}
        </div>
      )}

      {/* Tab Contents */}
      <div className="tab-content">
        {activeTab === 'overview' && (
          <div className="tab-pane">
            <form className="card" onSubmit={saveAgent} style={{ maxWidth: '650px', cursor: 'default' }} noValidate>
            <h3 style={{ marginBottom: '20px', color: 'var(--text-bright)' }}>Agent Details</h3>
            
            {mode === 'create' && (
              <div className="form-group">
                <label className="form-label" htmlFor="agent-name">
                  Name
                  <Tooltip content="Unique identifier name for this agent." position="bottom" align="left" />
                </label>
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
              <label className="form-label" htmlFor="agent-provider">
                LLM Provider
                <Tooltip content="Choose the model provider profile configured for this agent." position="bottom" align="left" />
              </label>
              <select
                id="agent-provider"
                className="form-select"
                value={agentForm.provider}
                onChange={set('provider')}
                required
              >
                <option value="">Select a provider…</option>
                {providers.map((p) => (
                  <option key={p.name} value={p.name}>
                    {p.name} ({p.provider_type})
                  </option>
                ))}
              </select>
            </div>

            <div className="form-group">
              <label className="form-label" htmlFor="agent-model">
                Model Name
                <Tooltip content="The specific model identifier to request (e.g. gpt-4o, claude-3-5-sonnet)." position="bottom" align="left" />
              </label>
              <div style={{ position: 'relative' }}>
                <input
                  id="agent-model"
                  type="text"
                  className="form-input"
                  value={agentForm.model}
                  onChange={set('model')}
                  placeholder={loadingModels ? "Loading models..." : "e.g. gpt-4o, claude-opus-4-5"}
                  onFocus={() => setShowModelsDropdown(true)}
                  onBlur={() => setTimeout(() => setShowModelsDropdown(false), 200)}
                  style={{ paddingRight: models.length > 0 ? '36px' : '12px' }}
                  required
                />
                {models.length > 0 && (
                  <button
                    type="button"
                    style={{
                      position: 'absolute',
                      right: '4px',
                      top: '50%',
                      transform: 'translateY(-50%)',
                      background: 'none',
                      border: 'none',
                      color: 'var(--text-muted)',
                      cursor: 'pointer',
                      display: 'flex',
                      alignItems: 'center',
                      padding: '8px',
                    }}
                    onClick={() => setShowModelsDropdown(!showModelsDropdown)}
                    onFocus={(e) => e.stopPropagation()}
                    aria-label="Toggle models list"
                  >
                    <span style={{ fontSize: '9px', opacity: 0.6 }}>▼</span>
                  </button>
                )}

                {showModelsDropdown && (filteredModels.length > 0 || (agentForm.model.trim() !== '' && !isExactMatch)) && (
                  <div
                    style={{
                      position: 'absolute',
                      top: '100%',
                      left: 0,
                      right: 0,
                      marginTop: '6px',
                      maxHeight: '220px',
                      overflowY: 'auto',
                      backgroundColor: '#161d31', // matched Sleek dark theme bg
                      border: '1px solid var(--border-soft)',
                      borderRadius: '6px',
                      boxShadow: '0 8px 24px rgba(0, 0, 0, 0.4)',
                      zIndex: 100,
                    }}
                  >
                    {filteredModels.map((m) => (
                      <div
                        key={m.id}
                        style={{
                          padding: '10px 14px',
                          cursor: 'pointer',
                          borderBottom: '1px solid var(--border-soft)',
                          fontSize: '13px',
                          display: 'flex',
                          flexDirection: 'column',
                          gap: '2px',
                        }}
                        onMouseDown={() => {
                          setAgentForm(prev => ({ ...prev, model: m.id }));
                          setShowModelsDropdown(false);
                        }}
                        className="model-option-item"
                      >
                        <span style={{ fontWeight: 500, color: 'var(--text)' }}>{m.id}</span>
                        <span style={{ fontSize: '11px', color: 'var(--text-muted)', display: 'flex', alignItems: 'center', gap: '6px', marginTop: '3px' }}>
                          <span
                            title={`Context: ${m.contextWindow.toLocaleString()} tokens`}
                            style={{
                              display: 'inline-flex',
                              alignItems: 'center',
                              gap: '4px',
                              backgroundColor: 'rgba(59, 130, 246, 0.15)',
                              color: '#93c5fd',
                              padding: '2px 6px',
                              borderRadius: '4px',
                            }}
                          >
                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ display: 'inline-block' }}>
                              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
                              <polyline points="14 2 14 8 20 8" />
                              <line x1="16" y1="13" x2="8" y2="13" />
                              <line x1="16" y1="17" x2="8" y2="17" />
                              <polyline points="10 9 9 9 8 9" />
                            </svg>
                            {(m.contextWindow / 1000).toFixed(0)}k
                          </span>
                          {m.thinking && (
                            <span
                              className="model-badge-hoverable"
                              title="Supports Reasoning / Thinking"
                              style={{
                                display: 'inline-flex',
                                alignItems: 'center',
                                backgroundColor: 'rgba(99, 102, 241, 0.15)',
                                color: '#a5b4fc',
                                padding: '2px 6px',
                                borderRadius: '4px',
                              }}
                            >
                              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ display: 'inline-block', flexShrink: 0 }}>
                                <circle cx="12" cy="12" r="3" />
                                <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
                              </svg>
                              <span className="model-badge-text">thinking</span>
                            </span>
                          )}
                          {m.inputModalities && m.inputModalities.includes('image') && (
                            <span
                              className="model-badge-hoverable"
                              title="Supports Vision / Image Input"
                              style={{
                                display: 'inline-flex',
                                alignItems: 'center',
                                backgroundColor: 'rgba(16, 185, 129, 0.15)',
                                color: '#34d399',
                                padding: '2px 6px',
                                borderRadius: '4px',
                              }}
                            >
                              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ display: 'inline-block', flexShrink: 0 }}>
                                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
                                <circle cx="12" cy="12" r="3" />
                              </svg>
                              <span className="model-badge-text">vision</span>
                            </span>
                          )}
                          <span
                            className="model-badge-hoverable"
                            title="Supports Tool Calling / Functions"
                            style={{
                              display: 'inline-flex',
                              alignItems: 'center',
                              backgroundColor: 'rgba(234, 179, 8, 0.15)',
                              color: '#fef08a',
                              padding: '2px 6px',
                              borderRadius: '4px',
                            }}
                          >
                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ display: 'inline-block', flexShrink: 0 }}>
                              <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
                            </svg>
                            <span className="model-badge-text">tools</span>
                          </span>
                        </span>
                      </div>
                    ))}

                    {agentForm.model.trim() !== '' && !isExactMatch && (
                      <div
                        style={{
                          padding: '10px 14px',
                          cursor: 'pointer',
                          backgroundColor: 'rgba(255, 255, 255, 0.02)',
                          fontSize: '13px',
                          display: 'flex',
                          alignItems: 'center',
                          color: 'var(--accent)',
                          fontWeight: 500,
                        }}
                        onMouseDown={() => {
                          setShowModelsDropdown(false);
                        }}
                        className="model-option-item"
                      >
                        <span>Use custom: &ldquo;{agentForm.model}&rdquo;</span>
                      </div>
                    )}
                  </div>
                )}
              </div>
              {modelsWarning && (
                <span className="form-hint" style={{ color: 'var(--warning)', marginTop: '4px', display: 'block' }}>
                  ⚠️ {modelsWarning}
                </span>
              )}
            </div>



            <div className="form-group">
              <label className="form-label" htmlFor="agent-prompt">
                System Prompt
                <Tooltip content="Instruction set defining the agent's character, constraints, and instructions." position="bottom" align="left" />
              </label>
              <textarea
                id="agent-prompt"
                className="form-textarea"
                value={agentForm.system_prompt}
                onChange={set('system_prompt')}
                placeholder="Describe the agent's role and capabilities…"
                style={{ minHeight: '120px' }}
              />
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px', marginBottom: '20px' }}>
              {/* Reasoning Effort Input */}
              {reasoningSupported && (effortOption || (!effortOption && !budgetOption && !toggleOption)) && (
                <div className="form-group" style={{ marginBottom: 0 }}>
                  <label className="form-label" htmlFor="agent-reasoning-effort">
                    Reasoning Effort
                    <Tooltip content="Controls the amount of reasoning tokens allocated (only supported by reasoning models)." position="bottom" align="left" />
                  </label>
                  {loadingModels ? (
                    <select
                      id="agent-reasoning-effort"
                      className="form-select"
                      disabled
                      style={{ opacity: 0.6 }}
                    >
                      <option>Loading models...</option>
                    </select>
                  ) : (
                    <select
                      id="agent-reasoning-effort"
                      className="form-select"
                      value={agentForm.reasoning_effort}
                      onChange={set('reasoning_effort')}
                    >
                      <option value="">None (default)</option>
                      {reasoningValues.map((val: string) => (
                        <option key={val} value={val}>
                          {val.charAt(0).toUpperCase() + val.slice(1)}
                        </option>
                      ))}
                    </select>
                  )}
                </div>
              )}

              {/* Reasoning Toggle Input */}
              {reasoningSupported && toggleOption && (
                <div className="form-group" style={{ marginBottom: 0 }}>
                  <label className="form-label" htmlFor="agent-reasoning-toggle">
                    Reasoning Toggle
                    <Tooltip content="Enable or disable reasoning capabilities." position="bottom" align="left" />
                  </label>
                  <div style={{ display: 'flex', alignItems: 'center', height: '38px' }}>
                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '14px', margin: 0, opacity: loadingModels ? 0.6 : 1 }}>
                      <input
                        id="agent-reasoning-toggle"
                        type="checkbox"
                        checked={agentForm.reasoning_effort === 'true' || agentForm.reasoning_effort === 'enabled'}
                        onChange={(e) => setAgentForm({ ...agentForm, reasoning_effort: e.target.checked ? 'true' : '' })}
                        disabled={loadingModels}
                      />
                      {loadingModels ? 'Loading models...' : 'Enable Reasoning'}
                    </label>
                  </div>
                </div>
              )}

              {/* Reasoning Budget Tokens Input */}
              {reasoningSupported && budgetOption && (
                <div className="form-group" style={{ marginBottom: 0 }}>
                  <label className="form-label" htmlFor="agent-reasoning-budget">
                    Reasoning Budget (Tokens)
                    <Tooltip content={`The maximum tokens allocated for reasoning. Allowed range: ${budgetOption.min} - ${budgetOption.max}.`} position="bottom" align="left" />
                  </label>
                  <input
                    id="agent-reasoning-budget"
                    type="number"
                    className="form-input"
                    value={agentForm.reasoning_budget_tokens || ''}
                    onChange={(e) => setAgentForm({ ...agentForm, reasoning_budget_tokens: parseInt(e.target.value) || 0 })}
                    min={budgetOption.min}
                    max={budgetOption.max}
                    placeholder={loadingModels ? 'Loading models...' : `e.g. 1024 (range: ${budgetOption.min} - ${budgetOption.max})`}
                    disabled={loadingModels}
                    style={loadingModels ? { opacity: 0.6 } : undefined}
                  />
                </div>
              )}

              {/* Disabled Placeholder when Reasoning is NOT supported */}
              {!reasoningSupported && (
                <div className="form-group" style={{ marginBottom: 0 }}>
                  <label className="form-label" htmlFor="agent-reasoning-disabled">
                    Reasoning Effort
                    <Tooltip content="Controls the amount of reasoning tokens allocated (only supported by reasoning models)." position="bottom" align="left" />
                  </label>
                  <select
                     id="agent-reasoning-disabled"
                     className="form-select"
                     value=""
                     disabled
                  >
                    <option value="">None (default)</option>
                  </select>
                  <span className="form-hint" style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px', display: 'block' }}>
                    Only supported by reasoning models.
                  </span>
                </div>
              )}

              {/* Max Iterations Input */}
              <div className="form-group" style={{ marginBottom: 0 }}>
                <label className="form-label" htmlFor="agent-iterations">
                  Max Iterations
                  <Tooltip content="The maximum execution loop turns the agent is allowed to take before stopping." position="bottom" align="right" />
                </label>
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

              {/* Override Max Context Checkbox */}
              <div className="form-group" style={{ marginBottom: 0, display: 'flex', flexDirection: 'column', justifyContent: 'center' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', height: '100%', minHeight: '38px', margin: 0 }}>
                  <input
                    id="agent-max-context-override"
                    type="checkbox"
                    checked={hasContextOverride}
                    onChange={(e) => {
                      const checked = e.target.checked;
                      setHasContextOverride(checked);
                      if (checked) {
                        setAgentForm(prev => ({
                          ...prev,
                          max_context_tokens: prev.max_context_tokens > 0 ? prev.max_context_tokens : (currentModelMeta?.contextWindow || 64000)
                        }));
                      } else {
                        setAgentForm(prev => ({
                          ...prev,
                          max_context_tokens: 0
                        }));
                      }
                    }}
                    style={{ width: '16px', height: '16px', accentColor: 'var(--accent)', cursor: 'pointer' }}
                  />
                  <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>
                    Override max context
                  </span>
                </label>
              </div>

              {/* Max Context Limit Input */}
              {hasContextOverride ? (
                <div className="form-group" style={{ marginBottom: 0 }}>
                  <label className="form-label" htmlFor="agent-max-context">
                    Max Context Limit
                    <Tooltip content="The maximum context tokens this agent is allowed to use." position="bottom" align="right" />
                  </label>
                  <input
                    id="agent-max-context"
                    type="number"
                    className="form-input"
                    value={agentForm.max_context_tokens || ''}
                    onChange={(e) => setAgentForm({ ...agentForm, max_context_tokens: parseInt(e.target.value) || 0 })}
                    min="1"
                    required
                  />
                  <span className="form-hint" style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px', display: 'block' }}>
                    Global default: 64,000 {currentModelMeta?.contextWindow ? `· Model limit: ${currentModelMeta.contextWindow.toLocaleString()}` : ''}
                  </span>
                </div>
              ) : (
                <div className="form-group" style={{ marginBottom: 0, opacity: 0.6 }}>
                  <label className="form-label" htmlFor="agent-max-context-disabled">
                    Max Context Limit
                    <Tooltip content="Inherited from global configurations." position="bottom" align="right" />
                  </label>
                  <input
                    id="agent-max-context-disabled"
                    type="text"
                    className="form-input"
                    value="Using global default"
                    disabled
                  />
                  <span className="form-hint" style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px', display: 'block' }}>
                    Global default (64,000) will be inherited
                  </span>
                </div>
              )}
            </div>

            <div className="form-group">
              <label className="form-label" htmlFor="agent-workspace">
                Workspace Directory
                <Tooltip content="Path to the agent's workspace directory. If empty, defaults to ~/.onclaw/workspace/<agent-name>." position="bottom" align="left" />
              </label>
              <input
                id="agent-workspace"
                type="text"
                className="form-input"
                value={agentForm.workspace || ''}
                onChange={set('workspace')}
                placeholder="e.g. ~/projects/my-project"
              />
              <span className="form-hint" style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px', display: 'block' }}>
                Empty resolves to the agent default <code>~/.onclaw/workspace/&lt;agent&gt;/</code>.
              </span>
            </div>



            <div style={{ marginBottom: '24px' }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: '10px', cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={agentForm.is_default}
                  onChange={(e) => setAgentForm({ ...agentForm, is_default: e.target.checked })}
                  style={{ width: '16px', height: '16px', accentColor: 'var(--accent)', cursor: 'pointer' }}
                />
                <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>
                  Set as default agent
                </span>
              </label>
            </div>

            <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => navigate('/agents')}
                disabled={isSaving}
              >
                Cancel
              </button>
              <button
                type="submit"
                className="btn btn-primary"
                disabled={isSaving}
              >
                {isSaving ? 'Saving…' : mode === 'edit' ? 'Save Changes' : 'Create Agent'}
              </button>
            </div>
          </form>
        </div>
      )}

        {activeTab === 'hooks' && (
          <Hooks
            agents={agents}
            showToast={showToast}
            pinnedScope={name}
          />
        )}

        {activeTab === 'skills' && (
          <Skills
            skills={skills}
            loadSkills={loadSkills}
            showToast={showToast}
            pinnedScope={name}
          />
        )}

        {activeTab === 'memory' && (
          <div className="tab-pane">
            <form className="card" onSubmit={saveMemoryConfig} style={{ maxWidth: '650px', cursor: 'default' }}>
              <h3 style={{ marginBottom: '20px', color: 'var(--text-bright)' }}>Agent Memory Configuration</h3>

              <div className="form-group">
                <label className="form-label" htmlFor="embedding-provider">
                  Embedding Provider
                  <Tooltip content="Provider for generating vector embeddings (e.g. openai, cohere, ollama)." position="bottom" align="left" />
                </label>
                <select
                  id="embedding-provider"
                  className="form-select"
                  value={memConfig.embedding_provider}
                  onChange={(e) => setMemConfig({ ...memConfig, embedding_provider: e.target.value })}
                >
                  <option value="">Default (from configuration)</option>
                  <option value="openai">OpenAI</option>
                  <option value="ollama">Ollama</option>
                  <option value="cohere">Cohere</option>
                </select>
              </div>

              <div className="form-group">
                <label className="form-label" htmlFor="embedding-model">
                  Embedding Model Override
                  <Tooltip content="Override model name for generating vector embeddings." position="bottom" align="left" />
                </label>
                <input
                  id="embedding-model"
                  type="text"
                  className="form-input"
                  value={memConfig.embedding_model}
                  onChange={(e) => setMemConfig({ ...memConfig, embedding_model: e.target.value })}
                  placeholder="e.g. text-embedding-3-small, nomic-embed-text"
                />
              </div>

              <div style={{ marginBottom: '16px' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '10px', cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={memConfig.curated_enabled}
                    onChange={(e) => setMemConfig({ ...memConfig, curated_enabled: e.target.checked })}
                    style={{ width: '16px', height: '16px', accentColor: 'var(--accent)', cursor: 'pointer' }}
                  />
                  <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>
                    Enable Curated Core Memory (MEMORY.md)
                  </span>
                </label>
              </div>

              <div style={{ marginBottom: '16px' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '10px', cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={memConfig.episodic_enabled}
                    onChange={(e) => setMemConfig({ ...memConfig, episodic_enabled: e.target.checked })}
                    style={{ width: '16px', height: '16px', accentColor: 'var(--accent)', cursor: 'pointer' }}
                  />
                  <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>
                    Enable Episodic Memory Summarization
                  </span>
                </label>
              </div>

              <div style={{ marginBottom: '16px' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '10px', cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={memConfig.kg_enabled}
                    onChange={(e) => setMemConfig({ ...memConfig, kg_enabled: e.target.checked })}
                    style={{ width: '16px', height: '16px', accentColor: 'var(--accent)', cursor: 'pointer' }}
                  />
                  <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>
                    Enable Knowledge Graph Entity Extraction
                  </span>
                </label>
              </div>

              <div style={{ marginBottom: '16px' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '10px', cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={memConfig.extraction_enabled}
                    onChange={(e) => setMemConfig({ ...memConfig, extraction_enabled: e.target.checked })}
                    style={{ width: '16px', height: '16px', accentColor: 'var(--accent)', cursor: 'pointer' }}
                  />
                  <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>
                    Enable Memory Extraction & Archival
                  </span>
                </label>
              </div>

              <div style={{ marginBottom: '16px' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '10px', cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={memConfig.retrieval_enabled}
                    onChange={(e) => setMemConfig({ ...memConfig, retrieval_enabled: e.target.checked })}
                    style={{ width: '16px', height: '16px', accentColor: 'var(--accent)', cursor: 'pointer' }}
                  />
                  <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>
                    Enable Durable Memory Retrieval (Tools)
                  </span>
                </label>
              </div>

              <div style={{ marginBottom: '16px' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '10px', cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={memConfig.dreaming_enabled}
                    onChange={(e) => setMemConfig({ ...memConfig, dreaming_enabled: e.target.checked })}
                    style={{ width: '16px', height: '16px', accentColor: 'var(--accent)', cursor: 'pointer' }}
                  />
                  <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>
                    Enable Memory Consolidation (Dreaming)
                  </span>
                </label>
              </div>

              <div style={{ marginBottom: '16px' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '10px', cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={memConfig.staged_write_approval}
                    onChange={(e) => setMemConfig({ ...memConfig, staged_write_approval: e.target.checked })}
                    style={{ width: '16px', height: '16px', accentColor: 'var(--accent)', cursor: 'pointer' }}
                  />
                  <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>
                    Require Approval for Memory Writes (Staged Writes)
                  </span>
                </label>
              </div>

              <div style={{ marginBottom: '24px' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '10px', cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={memConfig.security_scan_enabled}
                    onChange={(e) => setMemConfig({ ...memConfig, security_scan_enabled: e.target.checked })}
                    style={{ width: '16px', height: '16px', accentColor: 'var(--accent)', cursor: 'pointer' }}
                  />
                  <span className="form-label" style={{ margin: 0, cursor: 'pointer' }}>
                    Enable Security Scan for Memory Ingestion
                  </span>
                </label>
                {!memConfig.security_scan_enabled && (
                  <div style={{ marginTop: '8px', color: 'var(--error)', display: 'flex', alignItems: 'center', gap: '6px', fontSize: '13px' }}>
                    <WarningCircle size={16} weight="fill" />
                    <span>Caution: Disabling security scan exposes agent to exfiltration/injection threats</span>
                  </div>
                )}
              </div>

              <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
                <button
                  type="submit"
                  className="btn btn-primary"
                  disabled={isSaving}
                >
                  {isSaving ? 'Saving…' : 'Save Memory Configuration'}
                </button>
              </div>
            </form>
          </div>
        )}

        {activeTab === 'mcp' && (
          <MCP
            showToast={showToast}
            pinnedScope={name}
          />
        )}

        {activeTab === 'tools' && (
          <Tools
            showToast={showToast}
            variant="agent"
            agentName={mode === 'create' ? undefined : name}
            agentTools={agentForm.tools}
            onAgentToolsChange={(newTools) => {
              setAgentForm(prev => ({ ...prev, tools: newTools }));
              if (mode === 'edit') {
                loadAgents();
              }
            }}
          />
        )}

        {activeTab === 'persona' && (
          <div className="tab-pane">
            <PersonaEditor
              agentName={name!}
              showToast={showToast}
            />
          </div>
        )}
      </div>
    </div>
  );
}
