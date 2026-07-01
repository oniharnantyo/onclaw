import React, { useState } from 'react';
import { useReactTable, getCoreRowModel, flexRender, createColumnHelper } from '@tanstack/react-table';
import {
  Plus,
  Trash,
  ArrowsCounterClockwise,
  X,
  Code,
  UploadSimple,
} from '@phosphor-icons/react';

export interface Skill {
  name: string;
  scope: string;
  source_type: string;
  source: string;
  skill_path: string;
  version: string;
  hash: string;
  description: string;
  enabled: boolean;
  installed_at: string;
  updated_at: string;
}

interface SkillsProps {
  skills: Skill[];
  loadSkills: () => void;
  showToast: (msg: string, type?: 'success' | 'error') => void;
}

interface DiscoveredSkillItem {
  name: string;
  description: string;
}

interface DiscoverResult {
  package_name: string;
  is_plugin: boolean;
  skills: DiscoveredSkillItem[];
}

export default function Skills({ skills, loadSkills, showToast }: SkillsProps) {
  const safeSkills = skills || [];
  const [showInstallModal, setShowInstallModal] = useState(false);
  const [installStep, setInstallStep] = useState<1 | 2>(1);
  const [isProcessing, setIsProcessing] = useState(false);

  // Form inputs
  const [source, setSource] = useState('');
  const [scope, setScope] = useState('global');
  const [branch, setBranch] = useState('');
  const [asName, setAsName] = useState('');
  const [force, setForce] = useState(false);

  // Discovery results
  const [discoverResult, setDiscoverResult] = useState<DiscoverResult | null>(null);
  const [selectedSkillNames, setSelectedSkillNames] = useState<Record<string, boolean>>({});

  const handleLocalDeviceUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    setIsProcessing(true);
    const formData = new FormData();
    formData.append('file', file);

    try {
      const res = await fetch('/api/skills/upload', {
        method: 'POST',
        body: formData,
      });

      if (res.ok) {
        const data = await res.json();
        setSource(data.source);
        setDiscoverResult({
          package_name: data.package_name,
          is_plugin: data.is_plugin,
          skills: data.skills || [],
        });
        const initialSelected: Record<string, boolean> = {};
        (data.skills || []).forEach((s: any) => {
          initialSelected[s.name] = false;
        });
        setSelectedSkillNames(initialSelected);
        setInstallStep(2);
      } else {
        const errorData = await res.json();
        showToast(errorData.error || 'Failed to upload archive', 'error');
      }
    } catch {
      showToast('Failed to upload archive', 'error');
    } finally {
      setIsProcessing(false);
    }
  };

  const handleDiscover = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!source.trim()) return;

    setIsProcessing(true);
    try {
      const res = await fetch('/api/skills/discover', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ source: source.trim(), branch: branch.trim() }),
      });

      if (res.ok) {
        const data: DiscoverResult = await res.json();
        setDiscoverResult(data);
        
        // Default to unchecked
        const initialSelected: Record<string, boolean> = {};
        data.skills.forEach(s => {
          initialSelected[s.name] = false;
        });
        setSelectedSkillNames(initialSelected);
        
        setInstallStep(2);
      } else {
        const data = await res.json();
        showToast(data.error || 'Failed to discover skills', 'error');
      }
    } catch {
      showToast('Failed to discover skills', 'error');
    } finally {
      setIsProcessing(false);
    }
  };

  const handleInstall = async () => {
    const selected = Object.keys(selectedSkillNames).filter(name => selectedSkillNames[name]);
    if (selected.length === 0) {
      showToast('Please select at least one skill to install', 'error');
      return;
    }

    setIsProcessing(true);
    try {
      const res = await fetch('/api/skills', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          source: source.trim(),
          selected_names: selected,
          scope,
          branch: branch.trim(),
          as_name: asName.trim(),
          force,
        }),
      });

      if (res.ok) {
        showToast(`Successfully installed ${selected.length} skill(s)`);
        setShowInstallModal(false);
        resetForm();
        loadSkills();
      } else {
        const data = await res.json();
        showToast(data.error || 'Failed to install skills', 'error');
      }
    } catch {
      showToast('Failed to install skills', 'error');
    } finally {
      setIsProcessing(false);
    }
  };

  const deleteSkill = async (name: string, scope: string) => {
    if (!confirm(`Uninstall skill "${name}"?`)) return;

    try {
      const res = await fetch(`/api/skills/${name}?scope=${encodeURIComponent(scope)}`, { method: 'DELETE' });
      if (res.ok) {
        showToast('Skill successfully uninstalled');
        loadSkills();
      } else {
        showToast('Failed to uninstall skill', 'error');
      }
    } catch {
      showToast('Failed to uninstall skill', 'error');
    }
  };

  const updateSkill = async (name: string, scope: string) => {
    showToast(`Updating skill "${name}"...`);
    try {
      const res = await fetch(`/api/skills/${name}/update?scope=${encodeURIComponent(scope)}`, { method: 'POST' });
      if (res.ok) {
        showToast('Skill successfully updated');
        loadSkills();
      } else {
        const data = await res.json();
        showToast(data.error || 'Failed to update skill', 'error');
      }
    } catch {
      showToast('Failed to update skill', 'error');
    }
  };

  const resetForm = () => {
    setSource('');
    setScope('global');
    setBranch('');
    setAsName('');
    setForce(false);
    setDiscoverResult(null);
    setSelectedSkillNames({});
    setInstallStep(1);
  };

  const toggleSelectSkill = (name: string) => {
    setSelectedSkillNames(prev => ({
      ...prev,
      [name]: !prev[name],
    }));
  };

  const toggleSelectAll = () => {
    if (!discoverResult) return;
    const allSelected = discoverResult.skills.every(s => selectedSkillNames[s.name]);
    const nextSelected: Record<string, boolean> = {};
    discoverResult.skills.forEach(s => {
      nextSelected[s.name] = !allSelected;
    });
    setSelectedSkillNames(nextSelected);
  };

  // TanStack Table
  const columnHelper = createColumnHelper<Skill>();
  const columns = [
    columnHelper.accessor('name', {
      header: 'Name',
      cell: (info) => (
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontWeight: 600 }}>
          <Code size={16} weight="duotone" style={{ color: 'var(--accent-color)' }} />
          <span>{info.getValue()}</span>
        </div>
      ),
    }),
    columnHelper.accessor('scope', {
      header: 'Scope',
      cell: (info) => (
        <span className={`badge ${info.getValue() === 'global' ? 'badge-primary' : 'badge-secondary'}`}>
          {info.getValue()}
        </span>
      ),
    }),
    columnHelper.accessor('source_type', {
      header: 'Source Type',
      cell: (info) => (
        <span style={{ textTransform: 'capitalize', fontSize: '12px' }}>
          {info.getValue()}
        </span>
      ),
    }),
    columnHelper.accessor('description', {
      header: 'Description',
      cell: (info) => (
        <span style={{ color: 'var(--text-muted)', fontSize: '13px', display: 'block', maxWidth: '350px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {info.getValue() || '(no description)'}
        </span>
      ),
    }),
    columnHelper.accessor('source', {
      header: 'Source',
      cell: (info) => (
        <span style={{ fontSize: '12px', color: 'var(--text-muted)', fontFamily: 'monospace', display: 'block', maxWidth: '200px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={info.getValue()}>
          {info.getValue()}
        </span>
      ),
    }),
    columnHelper.display({
      id: 'actions',
      header: 'Actions',
      cell: (info) => {
        const skill = info.row.original;
        return (
          <div style={{ display: 'flex', gap: '6px' }}>
            <button
              className="btn btn-secondary btn-sm"
              onClick={() => updateSkill(skill.name, skill.scope)}
              title="Update skill"
              style={{ padding: '4px' }}
            >
              <ArrowsCounterClockwise size={15} />
            </button>
            <button
              className="btn btn-danger btn-sm"
              onClick={() => deleteSkill(skill.name, skill.scope)}
              title="Uninstall skill"
              style={{ padding: '4px' }}
            >
              <Trash size={15} />
            </button>
          </div>
        );
      },
    }),
  ];

  const table = useReactTable({
    data: safeSkills,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  return (
    <div className="page-container">
      {/* Toolbar */}
      <div className="page-toolbar">
        <div className="page-toolbar-left">
          <span className="badge badge-inactive">
            {safeSkills.length} skill{safeSkills.length !== 1 ? 's' : ''}
          </span>
        </div>
        <button
          id="install-skill-btn"
          className="btn btn-primary btn-sm"
          onClick={() => {
            resetForm();
            setShowInstallModal(true);
          }}
        >
          <Plus size={14} weight="bold" aria-hidden />
          Install Skill
        </button>
      </div>

      {/* Content */}
      <div style={{ padding: '20px 24px', overflow: 'auto', flexGrow: 1 }}>
        {safeSkills.length === 0 ? (
          <div className="empty-state">
            <div className="empty-state-icon" aria-hidden="true">
              <Code size={40} weight="duotone" />
            </div>
            <h3>No skills installed</h3>
            <p>Install modular procedure packs that agents dynamically invoke via the skill tool.</p>
            <button
              className="btn btn-primary btn-sm"
              style={{ marginTop: '8px' }}
              onClick={() => {
                resetForm();
                setShowInstallModal(true);
              }}
            >
              <Plus size={14} weight="bold" aria-hidden />
              Install first skill
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
              aria-label="Agent Skills"
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

      {/* Installation Modal */}
      {showInstallModal && (
        <div
          className="modal-overlay"
          onClick={(e) => { if (e.target === e.currentTarget) setShowInstallModal(false); }}
          role="dialog"
          aria-modal="true"
          aria-labelledby="skill-modal-title"
        >
          <div className="modal-content" style={{ maxWidth: '500px' }}>
            <div className="modal-header">
              <h2 id="skill-modal-title" className="modal-title">
                Install Skill ({installStep}/2)
              </h2>
              <button
                type="button"
                className="modal-close"
                onClick={() => setShowInstallModal(false)}
                aria-label="Close dialog"
              >
                <X size={18} weight="bold" />
              </button>
            </div>

            {installStep === 1 ? (
              <form onSubmit={handleDiscover} noValidate>
                <div className="form-group">
                  <label className="form-label" htmlFor="source">Source URL or Repository</label>
                  <input
                    id="source"
                    type="text"
                    className="form-input"
                    placeholder="e.g. vercel-labs/skills or https://...tar.gz"
                    value={source}
                    onChange={(e) => setSource(e.target.value)}
                    required
                    autoFocus
                  />
                  <span style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px', display: 'block' }}>
                    Accepts: Owner/Repo, HTTP tar.gz/zip link, or Claude plugin folder.
                  </span>
                </div>

                <div style={{ display: 'flex', alignItems: 'center', margin: '20px 0', color: 'var(--text-muted)' }}>
                  <div style={{ flexGrow: 1, borderBottom: '1px solid var(--border-soft)' }} />
                  <span style={{ padding: '0 12px', fontSize: '11px', textTransform: 'uppercase', fontWeight: 600, letterSpacing: '0.05em' }}>or</span>
                  <div style={{ flexGrow: 1, borderBottom: '1px solid var(--border-soft)' }} />
                </div>

                <div className="form-group">
                  <label className="form-label">Import archive from local device</label>
                  <label
                    style={{
                      display: 'flex',
                      flexDirection: 'column',
                      alignItems: 'center',
                      justifyContent: 'center',
                      border: '2px dashed var(--border)',
                      borderRadius: 'var(--radius-md)',
                      padding: '20px',
                      cursor: 'pointer',
                      background: 'var(--bg-elevated)',
                      transition: 'border-color var(--t-fast)',
                    }}
                    className="table-row-hover"
                  >
                    <UploadSimple size={24} style={{ color: 'var(--text-muted)', marginBottom: '8px' }} />
                    <span style={{ fontSize: '12px', fontWeight: 600 }}>Click to select a local file</span>
                    <span style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '2px' }}>
                      Accepts: .zip, .tar.gz, .tgz
                    </span>
                    <input
                      type="file"
                      accept=".zip,.tar.gz,.tgz"
                      style={{ display: 'none' }}
                      onChange={handleLocalDeviceUpload}
                      disabled={isProcessing}
                    />
                  </label>
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                  <div className="form-group">
                    <label className="form-label" htmlFor="scope">Scope</label>
                    <select
                      id="scope"
                      className="form-select"
                      value={scope}
                      onChange={(e) => setScope(e.target.value)}
                    >
                      <option value="global">Global (all agents)</option>
                      <option value="master">Master Agent</option>
                    </select>
                  </div>

                  <div className="form-group">
                    <label className="form-label" htmlFor="branch">GitHub Branch (Optional)</label>
                    <input
                      id="branch"
                      type="text"
                      className="form-input"
                      placeholder="e.g. main"
                      value={branch}
                      onChange={(e) => setBranch(e.target.value)}
                    />
                  </div>
                </div>

                <div className="modal-footer" style={{ borderTop: '1px solid var(--border)', padding: '16px 0 0 0', display: 'flex', justifyContent: 'flex-end', gap: '10px' }}>
                  <button type="button" className="btn btn-secondary" onClick={() => setShowInstallModal(false)}>Cancel</button>
                  <button type="submit" className="btn btn-primary" disabled={isProcessing}>
                    {isProcessing ? 'Discovering...' : 'Discover'}
                  </button>
                </div>
              </form>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
                  <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>Discovered Package:</span>
                  <strong style={{ fontSize: '14px' }}>{discoverResult?.package_name}</strong>
                  {discoverResult?.is_plugin && (
                    <span style={{ fontSize: '11px', color: 'var(--accent)' }}>Claude Plugin Manifest Detected</span>
                  )}
                </div>

                <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <span style={{ fontSize: '12px', fontWeight: 600 }}>Select Skills to Install</span>
                    <button className="btn-link" onClick={toggleSelectAll} style={{ fontSize: '12px', color: 'var(--accent)', background: 'none', border: 'none', cursor: 'pointer', padding: 0 }}>
                      {discoverResult?.skills.every(s => selectedSkillNames[s.name]) ? 'Deselect All' : 'Select All'}
                    </button>
                  </div>

                  <div style={{ maxHeight: '180px', overflowY: 'auto', border: '1px solid var(--border)', borderRadius: 'var(--radius-md)', padding: '8px' }}>
                    {discoverResult?.skills.map(s => (
                      <label key={s.name} style={{ display: 'flex', alignItems: 'flex-start', gap: '8px', padding: '8px', cursor: 'pointer', borderBottom: '1px solid var(--border-soft)' }}>
                        <input
                          type="checkbox"
                          checked={!!selectedSkillNames[s.name]}
                          onChange={() => toggleSelectSkill(s.name)}
                          style={{ marginTop: '3px' }}
                        />
                        <div>
                          <div style={{ fontSize: '13px', fontWeight: 600 }}>{s.name}</div>
                          <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>{s.description}</div>
                        </div>
                      </label>
                    ))}
                  </div>
                </div>

                {discoverResult?.skills.length === 1 && (
                  <div className="form-group">
                    <label className="form-label" htmlFor="asName">Custom Rename (Optional)</label>
                    <input
                      id="asName"
                      type="text"
                      className="form-input"
                      placeholder="e.g. custom-math"
                      value={asName}
                      onChange={(e) => setAsName(e.target.value)}
                    />
                  </div>
                )}

                <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '12px' }}>
                  <input
                    type="checkbox"
                    checked={force}
                    onChange={(e) => setForce(e.target.checked)}
                  />
                  <span>Force Overwrite (if skill collision occurs)</span>
                </label>

                <div className="modal-footer" style={{ borderTop: '1px solid var(--border)', padding: '16px 0 0 0', display: 'flex', justifyContent: 'space-between', gap: '10px' }}>
                  <button type="button" className="btn btn-secondary" onClick={() => setInstallStep(1)}>Back</button>
                  <div style={{ display: 'flex', gap: '10px' }}>
                    <button type="button" className="btn btn-secondary" onClick={() => setShowInstallModal(false)}>Cancel</button>
                    <button type="button" className="btn btn-primary" onClick={handleInstall} disabled={isProcessing}>
                      {isProcessing ? 'Installing...' : 'Install'}
                    </button>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
