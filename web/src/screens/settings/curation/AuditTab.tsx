// Audit sub-tab (add-skill-curation-from-traces 9.3): the decision trail for
// a retention window (default 30 days, ?since= RFC 3339) — proposal summary,
// diff, verdict chip, reviewer, the recorded reason when rejected, and the
// cluster the proposal belonged to. Read-only: nothing here edits.
import { useEffect, useState } from "react";
import { Icon } from "../../../components/ui/Icon";
import { Chip } from "../../../components/ui/Chip";
import { api, type ApiAuditEntry } from "../../../lib/api";

const WINDOWS = [
  { id: '7d', label: '7 days', days: 7 },
  { id: '30d', label: '30 days', days: 30 },
  { id: '90d', label: '90 days', days: 90 },
] as const;

export interface AuditTabProps {
  wsSlug: string;
}

export function AuditTab({ wsSlug }: AuditTabProps) {
  const [entries, setEntries] = useState<ApiAuditEntry[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [windowId, setWindowId] = useState<(typeof WINDOWS)[number]['id']>('30d');

  const load = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const days = WINDOWS.find((w) => w.id === windowId)?.days ?? 30;
      const since = new Date(Date.now() - days * 86400000).toISOString();
      const res = await api.curation.listAudit(wsSlug, since);
      setEntries(res.entries || []);
    } catch (err: unknown) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (wsSlug) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload per workspace/window only
  }, [wsSlug, windowId]);

  if (loading && entries === null) {
    return (
      <div className="space-y-3" data-testid="audit-loading">
        {[0, 1, 2].map((i) => (
          <div key={i} className="h-14 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
        ))}
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="rounded-md border border-line px-4 py-6 text-center" data-testid="audit-error">
        <p className="text-[13px] font-medium text-fg">Couldn&apos;t load the audit trail</p>
        <p className="mt-1 text-[12px] text-muted">{loadError}</p>
        <button
          type="button"
          onClick={() => void load()}
          data-testid="btn-audit-retry"
          className="mt-3 h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
        >
          Retry
        </button>
      </div>
    );
  }

  return (
    <div data-testid="audit-tab">
      <div className="mb-3 flex flex-wrap items-center gap-2" data-testid="audit-window-picker">
        <span className="text-[11px] uppercase tracking-[0.14em] text-muted">Window</span>
        {WINDOWS.map((w) => (
          <button
            key={w.id}
            type="button"
            onClick={() => setWindowId(w.id)}
            className={
              'flex h-8 items-center rounded-md border px-3 text-[12px] font-medium transition-colors ' +
              (windowId === w.id
                ? 'border-accent bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg'
                : 'border-line text-muted hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg2')
            }
          >
            {w.label}
          </button>
        ))}
      </div>

      {(entries || []).length === 0 ? (
        <div className="rounded-md border border-line px-4 py-8 text-center" data-testid="audit-empty">
          <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
            <Icon name="history" size={20} />
          </div>
          <p className="text-[14px] font-medium text-fg">No decisions in this window</p>
          <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
            Every approve and reject is recorded here — the proposal, its diff, the verdict, and the reviewer&apos;s
            reason — so past decisions stay inspectable.
          </p>
        </div>
      ) : (
        <ul className="space-y-3" data-testid="audit-list">
          {(entries || []).map((e) => (
            <li key={e.id} className="rounded-md border border-line px-4 py-3" data-testid={'audit-' + e.skill_name}>
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-[13px] font-medium text-fg">{e.skill_name}</span>
                {e.verdict === 'approved' ? (
                  <Chip className="border-[color-mix(in_oklab,var(--success)_45%,transparent)] text-[color-mix(in_oklab,var(--success),black_25%)]">
                    approved
                  </Chip>
                ) : e.verdict === 'rejected' ? (
                  <Chip className="border-[color-mix(in_oklab,var(--danger)_45%,transparent)] text-danger">rejected</Chip>
                ) : (
                  <Chip>superseded</Chip>
                )}
                <Chip mono>cluster · {e.cluster_id}</Chip>
                {e.reviewer ? <span className="text-[11px] text-muted">by {e.reviewer}</span> : null}
                <span className="ml-auto text-[11px] text-muted">
                  {new Date(e.created_at).toLocaleString()}
                </span>
              </div>
              {e.verdict === 'rejected' && e.reason ? (
                <p className="mt-1 text-[12px] text-muted" data-testid={'audit-reason-' + e.skill_name}>
                  Reason: {e.reason}
                </p>
              ) : null}
              {e.diff ? (
                <pre className="od-scroll mt-2 max-h-40 overflow-auto whitespace-pre-wrap rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] p-2.5 font-mono text-[11px] leading-4 text-fg2" data-testid={'audit-diff-' + e.skill_name}>
                  {e.diff}
                </pre>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
