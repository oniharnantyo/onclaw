import { useState, useEffect } from 'react';
import { Brain, Clock, CheckCircle, XCircle, Sparkle, ArrowsCounterClockwise } from '@phosphor-icons/react';

export interface DreamSweep {
  timestamp: string;
  agent: string;
  episodes_count: number;
  promotions: string[];
  supersessions: string[];
  score: number;
  review_model: string;
}

export interface StagedWrite {
  id: number;
  agent: string;
  operation: string;
  target: string;
  content: string;
  status: string;
  created_at: string;
}

interface MemoryProps {
  showToast: (msg: string, type?: 'success' | 'error') => void;
}

export default function Memory({ showToast }: MemoryProps) {
  const [dreams, setDreams] = useState<DreamSweep[]>([]);
  const [stagedWrites, setStagedWrites] = useState<StagedWrite[]>([]);
  const [loadingDreams, setLoadingDreams] = useState(true);
  const [loadingStaged, setLoadingStaged] = useState(true);
  const [activeSection, setActiveSection] = useState<'dreams' | 'approvals'>('dreams');

  const loadDreams = async () => {
    setLoadingDreams(true);
    try {
      const res = await fetch('/api/memory/dreams');
      if (res.ok) {
        setDreams(await res.json());
      }
    } catch {
      showToast('Failed to load dreaming sweeps', 'error');
    } finally {
      setLoadingDreams(false);
    }
  };

  const loadStagedWrites = async () => {
    setLoadingStaged(true);
    try {
      const res = await fetch('/api/memory/staged');
      if (res.ok) {
        setStagedWrites(await res.json());
      }
    } catch {
      showToast('Failed to load staged writes', 'error');
    } finally {
      setLoadingStaged(false);
    }
  };

  useEffect(() => { loadDreams(); }, []);
  useEffect(() => { loadStagedWrites(); }, []);

  const handleApprove = async (id: number) => {
    try {
      const res = await fetch(`/api/memory/staged/${id}/approve`, { method: 'POST' });
      if (res.ok) {
        showToast('Memory write approved');
        loadStagedWrites();
      } else {
        const data = await res.json();
        showToast(data.error || 'Failed to approve', 'error');
      }
    } catch {
      showToast('Failed to approve', 'error');
    }
  };

  const handleReject = async (id: number) => {
    try {
      const res = await fetch(`/api/memory/staged/${id}/reject`, { method: 'POST' });
      if (res.ok) {
        showToast('Memory write rejected');
        loadStagedWrites();
      } else {
        const data = await res.json();
        showToast(data.error || 'Failed to reject', 'error');
      }
    } catch {
      showToast('Failed to reject', 'error');
    }
  };

  const formatTimestamp = (ts: string) => {
    const d = new Date(ts);
    return d.toLocaleString(undefined, {
      month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
    });
  };

  return (
    <div className="page-container">
      <div className="page-toolbar">
        <div className="page-toolbar-left" style={{ display: 'flex', gap: '4px' }}>
          <button
            className={`btn btn-sm ${activeSection === 'dreams' ? 'btn-primary' : 'btn-secondary'}`}
            onClick={() => setActiveSection('dreams')}
          >
            <Brain size={14} weight="duotone" style={{ marginRight: '6px' }} />
            Dreaming Sweeps
            {dreams.length > 0 && <span className="badge badge-primary" style={{ marginLeft: '6px' }}>{dreams.length}</span>}
          </button>
          <button
            className={`btn btn-sm ${activeSection === 'approvals' ? 'btn-primary' : 'btn-secondary'}`}
            onClick={() => setActiveSection('approvals')}
          >
            <Clock size={14} weight="duotone" style={{ marginRight: '6px' }} />
            Pending Approvals
            {stagedWrites.length > 0 && (
              <span className="badge badge-warning" style={{ marginLeft: '6px' }}>{stagedWrites.length}</span>
            )}
          </button>
        </div>
        <div className="page-toolbar-right">
          <button className="btn btn-secondary btn-sm" onClick={() => { activeSection === 'dreams' ? loadDreams() : loadStagedWrites(); }}>
            <ArrowsCounterClockwise size={14} weight="bold" /> Refresh
          </button>
        </div>
      </div>

      <div style={{ padding: '20px 24px', overflow: 'auto', flexGrow: 1 }}>
        {activeSection === 'dreams' && (
          <>
            {loadingDreams ? (
              <div className="empty-state">
                <div className="empty-state-icon"><Brain size={40} weight="duotone" /></div>
                <p>Loading dreaming sweeps...</p>
              </div>
            ) : dreams.length === 0 ? (
              <div className="empty-state">
                <div className="empty-state-icon"><Brain size={40} weight="duotone" /></div>
                <h3>No dreaming sweeps yet</h3>
                <p>Dreaming consolidates episodic memories into durable facts. Results appear here after agents complete sessions.</p>
              </div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                {dreams.map((sweep, idx) => (
                  <div key={idx} className="card" style={{ padding: '16px 20px', cursor: 'default' }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: '10px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                        <Sparkle size={16} weight="duotone" style={{ color: 'var(--accent)' }} />
                        <strong style={{ fontSize: '14px' }}>Sweep #{dreams.length - idx}</strong>
                        <span className="badge badge-secondary" style={{ fontSize: '11px' }}>{sweep.agent}</span>
                      </div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '10px', fontSize: '12px', color: 'var(--text-muted)' }}>
                        <span>{formatTimestamp(sweep.timestamp)}</span>
                        <span style={{ fontFamily: 'monospace', fontWeight: 600, color: sweep.score >= 0.5 ? 'var(--accent)' : 'var(--text-muted)' }}>
                          {(sweep.score * 100).toFixed(0)}%
                        </span>
                      </div>
                    </div>

                    {sweep.episodes_count > 0 && (
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '10px' }}>
                        Episodes reviewed: {sweep.episodes_count}
                        {sweep.review_model && <> &middot; Model: {sweep.review_model}</>}
                      </div>
                    )}

                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                      {sweep.promotions.length > 0 && (
                        <div>
                          <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--accent)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: '6px' }}>
                            Promotions
                          </div>
                          <ul style={{ margin: 0, padding: 0, listStyle: 'none' }}>
                            {sweep.promotions.map((p, i) => (
                              <li key={i} style={{ fontSize: '13px', padding: '3px 0', color: 'var(--text)' }}>
                                &bull; {p}
                              </li>
                            ))}
                          </ul>
                        </div>
                      )}
                      {sweep.supersessions.length > 0 && (
                        <div>
                          <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--destructive)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: '6px' }}>
                            Supersessions
                          </div>
                          <ul style={{ margin: 0, padding: 0, listStyle: 'none' }}>
                            {sweep.supersessions.map((s, i) => (
                              <li key={i} style={{ fontSize: '13px', padding: '3px 0', color: 'var(--text-muted)' }}>
                                &bull; {s}
                              </li>
                            ))}
                          </ul>
                        </div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </>
        )}

        {activeSection === 'approvals' && (
          <>
            {loadingStaged ? (
              <div className="empty-state">
                <div className="empty-state-icon"><Clock size={40} weight="duotone" /></div>
                <p>Loading pending approvals...</p>
              </div>
            ) : stagedWrites.length === 0 ? (
              <div className="empty-state">
                <div className="empty-state-icon"><CheckCircle size={40} weight="duotone" /></div>
                <h3>No pending approvals</h3>
                <p>When write-approval mode is enabled, staged memory writes appear here for review before being applied.</p>
              </div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                {stagedWrites.map((sw) => (
                  <div key={sw.id} className="card" style={{ padding: '14px 18px', cursor: 'default' }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                      <div style={{ flexGrow: 1 }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px' }}>
                          <span className="badge badge-secondary">{sw.agent}</span>
                          <span className={`badge ${sw.operation === 'add' ? 'badge-primary' : sw.operation === 'replace' ? 'badge-warning' : 'badge-inactive'}`}>
                            {sw.operation}
                          </span>
                          <span style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
                            {formatTimestamp(sw.created_at)}
                          </span>
                        </div>
                        <div style={{ fontSize: '13px', color: 'var(--text)', fontFamily: 'monospace', background: 'var(--bg-elevated)', padding: '8px 10px', borderRadius: '6px', marginTop: '4px' }}>
                          {sw.content}
                        </div>
                        {sw.target && (
                          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
                            Target: {sw.target}
                          </div>
                        )}
                      </div>
                      <div style={{ display: 'flex', gap: '6px', marginLeft: '12px', flexShrink: 0 }}>
                        <button
                          className="btn btn-primary btn-sm"
                          onClick={() => handleApprove(sw.id)}
                          title="Approve this memory write"
                        >
                          <CheckCircle size={14} weight="bold" /> Approve
                        </button>
                        <button
                          className="btn btn-danger btn-sm"
                          onClick={() => handleReject(sw.id)}
                          title="Reject this memory write"
                        >
                          <XCircle size={14} weight="bold" /> Reject
                        </button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
