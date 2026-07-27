import React, { useState, useEffect } from 'react';
import { Clock, Cpu, FloppyDisk, Key, Sparkle, CheckCircle, ArrowsCounterClockwise } from '@phosphor-icons/react';
import Tooltip from './Tooltip';
import type { Provider } from './Providers';

interface ModelOption {
  id: string;
  contextWindow: number;
  thinking?: boolean;
  inputModalities?: string[];
}

interface EmbeddingsProps {
  providers: Provider[];
  showToast: (message: string, type?: 'success' | 'error') => void;
}

// Parse a Go duration string (e.g. "30s", "2m") into a whole-seconds string for
// the number field. Returns '' when absent or unparseable (falls back to default).
function durationToSeconds(duration: string): string {
  if (!duration) return '';
  const seconds = /^([\d.]+)s$/.exec(duration);
  if (seconds) return String(Math.round(parseFloat(seconds[1])));
  const minutes = /^([\d.]+)m$/.exec(duration);
  if (minutes) return String(Math.round(parseFloat(minutes[1]) * 60));
  return '';
}

// Format a whole-seconds value as a Go duration string; '' when empty/invalid so
// the backend applies its 30s default.
function secondsToDuration(value: string): string {
  const seconds = parseInt(value, 10);
  if (!Number.isFinite(seconds) || seconds <= 0) return '';
  return `${seconds}s`;
}

export default function Embeddings({ providers, showToast }: EmbeddingsProps) {
  const [provider, setProvider] = useState<string>('');
  const [model, setModel] = useState<string>('');
  const [timeoutSeconds, setTimeoutSeconds] = useState<string>('');
  const [loading, setLoading] = useState<boolean>(true);
  const [saving, setSaving] = useState<boolean>(false);

  const [models, setModels] = useState<ModelOption[]>([]);
  const [loadingModels, setLoadingModels] = useState<boolean>(false);
  const [showModelsDropdown, setShowModelsDropdown] = useState<boolean>(false);

  const loadConfig = async () => {
    setLoading(true);
    try {
      const res = await fetch('/api/config/embeddings');
      if (res.ok) {
        const cfgData = await res.json();
        setProvider(cfgData.provider || '');
        setModel(cfgData.model || '');
        setTimeoutSeconds(durationToSeconds(cfgData.timeout || ''));
      }
    } catch {
      showToast('Failed to load embeddings configuration', 'error');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadConfig();
  }, []);

  // Fetch models for the active provider (or default provider)
  useEffect(() => {
    const targetProvider = provider || providers.find((p) => p.is_default)?.name || providers[0]?.name;
    if (!targetProvider) {
      setModels([]);
      return;
    }

    const fetchModels = async () => {
      setLoadingModels(true);
      try {
        const res = await fetch(`/api/providers/${encodeURIComponent(targetProvider)}/models`);
        if (res.ok) {
          const data = await res.json();
          setModels(data.models || []);
        } else {
          setModels([]);
        }
      } catch {
        setModels([]);
      } finally {
        setLoadingModels(false);
      }
    };

    fetchModels();
  }, [provider, providers]);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    try {
      const res = await fetch('/api/config/embeddings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider: provider.trim(),
          model: model.trim(),
          timeout: secondsToDuration(timeoutSeconds),
        }),
      });

      if (res.ok) {
        showToast('Global embeddings configuration saved successfully', 'success');
      } else {
        const errText = await res.text();
        showToast(`Failed to save configuration: ${errText}`, 'error');
      }
    } catch {
      showToast('Failed to save embeddings configuration', 'error');
    } finally {
      setSaving(false);
    }
  };

  const selectedProviderObj = providers.find((p) => p.name === provider);

  const isEmbeddingModel = (id: string) => {
    const lower = id.toLowerCase();
    return (
      lower.includes('embed') ||
      lower.includes('bge') ||
      lower.includes('e5') ||
      lower.includes('gte') ||
      lower.includes('minilm') ||
      lower.includes('ada') ||
      lower.includes('voyage') ||
      lower.includes('nomic') ||
      lower.includes('mxbai') ||
      lower.includes('snowflake')
    );
  };

  // Strictly filter embedding models only
  const embeddingModels = models.filter((m) => isEmbeddingModel(m.id));

  const filteredModels = embeddingModels.filter((m) =>
    m.id.toLowerCase().includes(model.toLowerCase())
  );
  const isExactMatch = embeddingModels.some((m) => m.id.toLowerCase() === model.trim().toLowerCase());

  return (
    <div className="page-container" style={{ padding: '24px', maxWidth: '1000px', margin: '0 auto' }}>
      {/* Header section */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '24px' }}>
        <div>
          <h2 style={{ fontSize: '20px', fontWeight: 600, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: '8px', margin: 0 }}>
            <Cpu size={22} weight="duotone" style={{ color: 'var(--color-accent, #22C55E)' }} />
            Global Vector Embeddings
          </h2>
          <p style={{ fontSize: '13px', color: 'var(--text-muted, #94A3B8)', marginTop: '4px', margin: 0 }}>
            Configure default vector embedding settings for long-term memory archive search across all agents.
          </p>
        </div>
        <button
          className="btn btn-secondary"
          onClick={loadConfig}
          disabled={loading}
          style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '13px' }}
        >
          <ArrowsCounterClockwise size={16} className={loading ? 'spin' : ''} />
          Reload
        </button>
      </div>

      {/* Info Card / Summary Banner */}
      <div
        className="card"
        style={{
          padding: '16px 20px',
          marginBottom: '24px',
          background: 'rgba(30, 41, 59, 0.5)',
          border: '1px solid var(--border-color, #334155)',
          borderRadius: '8px',
          display: 'flex',
          gap: '16px',
          alignItems: 'flex-start',
        }}
      >
        <Sparkle size={24} weight="duotone" style={{ color: 'var(--color-accent, #22C55E)', flexShrink: 0, marginTop: '2px' }} />
        <div style={{ fontSize: '13px', lineHeight: '1.5', color: 'var(--text-secondary, #CBD5E1)' }}>
          <strong style={{ color: 'var(--text-primary, #F8FAFC)' }}>Fallback Precedence:</strong> Per-Agent Memory Overrides → <strong>Global Embeddings Config</strong> → Bootstrap System Config → Active Chat Provider.
          <div style={{ marginTop: '6px', color: 'var(--text-muted, #94A3B8)' }}>
            Agents inherit these settings unless explicitly overridden in their individual Memory configuration tab.
          </div>
        </div>
      </div>

      {loading ? (
        <div className="loading-state" style={{ padding: '48px', textAlign: 'center', color: 'var(--text-muted)' }}>
          Loading embeddings configuration...
        </div>
      ) : (
        <div className="card" style={{ padding: '24px', background: 'var(--card-bg, #0F172A)', border: '1px solid var(--border-color, #334155)', borderRadius: '12px' }}>
          <form onSubmit={handleSave}>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
              
              {/* Provider Select */}
              <div className="form-group">
                <label className="form-label" htmlFor="embedding-provider" style={{ display: 'flex', alignItems: 'center', gap: '6px', fontWeight: 500, marginBottom: '6px' }}>
                  <Key size={16} weight="duotone" />
                  Embedding Provider Profile
                  <Tooltip content="Select provider profile for vector embeddings. Choose Default to inherit each agent's active chat provider credentials." />
                </label>
                <select
                  id="embedding-provider"
                  className="form-control"
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                  style={{
                    width: '100%',
                    padding: '10px 12px',
                    borderRadius: '6px',
                    background: 'var(--input-bg, #1E293B)',
                    border: '1px solid var(--border-color, #475569)',
                    color: 'var(--text-primary, #F8FAFC)',
                    fontSize: '14px',
                  }}
                >
                  <option value="">Default (Inherit from Agent Chat Provider)</option>
                  {providers.map((p) => (
                    <option key={p.name} value={p.name}>
                      {p.name} ({p.provider_type}) {p.is_default ? '— [Default]' : ''}
                    </option>
                  ))}
                </select>
                <p className="form-help" style={{ fontSize: '12px', color: 'var(--text-muted, #94A3B8)', marginTop: '4px' }}>
                  Provider profile supplying API credentials and endpoint connection for vector embedding generation.
                </p>
                {selectedProviderObj && (
                  <div style={{ marginTop: '8px', display: 'flex', alignItems: 'center', gap: '8px', fontSize: '12px' }}>
                    <span style={{ padding: '2px 8px', borderRadius: '4px', background: 'rgba(34, 197, 94, 0.15)', color: '#22C55E', fontWeight: 500 }}>
                      Type: {selectedProviderObj.provider_type}
                    </span>
                    {selectedProviderObj.secret_set && (
                      <span style={{ display: 'flex', alignItems: 'center', gap: '4px', color: '#22C55E' }}>
                        <CheckCircle size={14} weight="fill" /> Secret Configured
                      </span>
                    )}
                  </div>
                )}
              </div>

              {/* Model Name Autocomplete Input (Strictly Embedding Models Only) */}
              <div className="form-group" style={{ position: 'relative' }}>
                <label className="form-label" htmlFor="embedding-model" style={{ display: 'flex', alignItems: 'center', gap: '6px', fontWeight: 500, marginBottom: '6px' }}>
                  <Cpu size={16} weight="duotone" />
                  Embedding Model Name
                  <Tooltip content="Select or type the vector embedding model name (e.g. text-embedding-3-small, text-embedding-004, nomic-embed-text)." />
                </label>
                <div style={{ position: 'relative' }}>
                  <input
                    id="embedding-model"
                    type="text"
                    className="form-control"
                    value={model}
                    onChange={(e) => setModel(e.target.value)}
                    onFocus={() => setShowModelsDropdown(true)}
                    onBlur={() => setTimeout(() => setShowModelsDropdown(false), 200)}
                    placeholder={loadingModels ? "Loading provider models..." : "e.g. text-embedding-3-small"}
                    style={{
                      width: '100%',
                      padding: '10px 12px',
                      paddingRight: embeddingModels.length > 0 ? '36px' : '12px',
                      borderRadius: '6px',
                      background: 'var(--input-bg, #1E293B)',
                      border: '1px solid var(--border-color, #475569)',
                      color: 'var(--text-primary, #F8FAFC)',
                      fontSize: '14px',
                    }}
                  />
                  {embeddingModels.length > 0 && (
                    <button
                      type="button"
                      style={{
                        position: 'absolute',
                        right: '4px',
                        top: '50%',
                        transform: 'translateY(-50%)',
                        background: 'none',
                        border: 'none',
                        color: 'var(--text-muted, #94A3B8)',
                        cursor: 'pointer',
                        display: 'flex',
                        alignItems: 'center',
                        padding: '8px',
                      }}
                      onClick={() => setShowModelsDropdown(!showModelsDropdown)}
                      onFocus={(e) => e.stopPropagation()}
                      aria-label="Toggle embedding models list"
                    >
                      <span style={{ fontSize: '10px', opacity: 0.7 }}>▼</span>
                    </button>
                  )}
                </div>

                {showModelsDropdown && (filteredModels.length > 0 || (model.trim() !== '' && !isExactMatch)) && (
                  <div
                    style={{
                      position: 'absolute',
                      top: '100%',
                      left: 0,
                      right: 0,
                      marginTop: '6px',
                      maxHeight: '220px',
                      overflowY: 'auto',
                      backgroundColor: '#1E293B',
                      border: '1px solid var(--border-color, #475569)',
                      borderRadius: '6px',
                      boxShadow: '0 8px 24px rgba(0, 0, 0, 0.4)',
                      zIndex: 100,
                    }}
                  >
                    {filteredModels.length === 0 ? (
                      <div style={{ padding: '10px 14px', fontSize: '12px', color: 'var(--text-muted, #94A3B8)' }}>
                        No matching embedding models found. Type a custom model name above.
                      </div>
                    ) : (
                      filteredModels.map((m) => (
                        <div
                          key={m.id}
                          style={{
                            padding: '10px 14px',
                            cursor: 'pointer',
                            borderBottom: '1px solid rgba(71, 85, 105, 0.4)',
                            fontSize: '13px',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                          }}
                          onMouseDown={() => {
                            setModel(m.id);
                            setShowModelsDropdown(false);
                          }}
                          className="model-option-item"
                        >
                          <span style={{ fontWeight: 500, color: 'var(--text-primary, #F8FAFC)' }}>{m.id}</span>
                          <span
                            style={{
                              fontSize: '10px',
                              padding: '2px 6px',
                              borderRadius: '4px',
                              background: 'rgba(34, 197, 94, 0.15)',
                              color: '#22C55E',
                              fontWeight: 500,
                            }}
                          >
                            Embedding
                          </span>
                        </div>
                      ))
                    )}
                  </div>
                )}
                <p className="form-help" style={{ fontSize: '12px', color: 'var(--text-muted, #94A3B8)', marginTop: '4px' }}>
                  Discovered vector embedding models for the provider. Leave blank to rely on provider default.
                </p>
              </div>

              {/* Embedding Timeout */}
              <div className="form-group">
                <label className="form-label" htmlFor="embedding-timeout" style={{ display: 'flex', alignItems: 'center', gap: '6px', fontWeight: 500, marginBottom: '6px' }}>
                  <Clock size={16} weight="duotone" />
                  Embedding Timeout (seconds)
                  <Tooltip content="Maximum time to wait for a single embedding request. Leave blank for the 30-second default." />
                </label>
                <input
                  id="embedding-timeout"
                  type="number"
                  min={1}
                  className="form-control"
                  value={timeoutSeconds}
                  onChange={(e) => setTimeoutSeconds(e.target.value)}
                  placeholder="30"
                  style={{
                    width: '100%',
                    padding: '10px 12px',
                    borderRadius: '6px',
                    background: 'var(--input-bg, #1E293B)',
                    border: '1px solid var(--border-color, #475569)',
                    color: 'var(--text-primary, #F8FAFC)',
                    fontSize: '14px',
                  }}
                />
                <p className="form-help" style={{ fontSize: '12px', color: 'var(--text-muted, #94A3B8)', marginTop: '4px' }}>
                  Per-request timeout for embedding generation. Blank uses the 30s default.
                </p>
              </div>

              {/* Action Buttons */}
              <div style={{ marginTop: '12px', display: 'flex', justifyContent: 'flex-end' }}>
                <button
                  type="submit"
                  className="btn btn-primary"
                  disabled={saving}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '8px',
                    padding: '10px 20px',
                    borderRadius: '6px',
                    fontWeight: 600,
                    cursor: saving ? 'not-allowed' : 'pointer',
                  }}
                >
                  <FloppyDisk size={18} weight="bold" />
                  {saving ? 'Saving...' : 'Save Embeddings Config'}
                </button>
              </div>

            </div>
          </form>
        </div>
      )}
    </div>
  );
}
