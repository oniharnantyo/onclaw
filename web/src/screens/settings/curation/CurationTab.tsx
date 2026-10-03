// Curation tab (add-skill-curation-from-traces 9.1): the loop's review
// surface inside Skills settings. The header carries the cycle status line —
// cadence pointer, last run time and outcome, pattern and proposal counts —
// and the run-curation-now control (Owner/Admin, visible running state, 409
// → "already running" notice). Three sub-tabs: Candidates / Patterns / Audit.
import { useCallback, useEffect, useState } from "react";
import { cx } from "../../../lib/helpers";
import { Icon } from "../../../components/ui/Icon";
import { ApiError, api, type ApiCycleStatus } from "../../../lib/api";
import { CandidatesList } from "./CandidatesList";
import { CandidateTakeover } from "./CandidateTakeover";
import { PatternsTab } from "./PatternsTab";
import { AuditTab } from "./AuditTab";

type SubTab = 'candidates' | 'patterns' | 'audit';

const SUB_TABS: Array<{ id: SubTab; label: string }> = [
  { id: 'candidates', label: 'Candidates' },
  { id: 'patterns', label: 'Patterns' },
  { id: 'audit', label: 'Audit' },
];

/** Human summary of one cycle outcome: "succeeded", "failed (stage)", "running now", "not run yet". */
export function cycleOutcomeLine(status: ApiCycleStatus | null): string {
  if (!status || status.state === 'idle') return 'not run yet';
  if (status.state === 'running') return 'running now';
  if (status.state === 'failed') {
    const failedStage = (status.stages || []).find((s) => !s.ok);
    return failedStage ? `failed at ${failedStage.stage}` : 'failed';
  }
  return 'succeeded';
}

export interface CurationTabProps {
  tenant: any;
  /** Owner/Admin (skills.write) — gates the run-now control and the review
   * footer; the same derived value the Skills library uses. */
  canWrite: boolean;
  onToast?: (text: string, kind?: string) => void;
  /** Deep link (?candidate=): the takeover opens straight onto this row. */
  focusCandidateId?: string | null;
  /** Parent hook: re-read the pending count after a decision. */
  onQueueChanged?: () => void;
}

export function CurationTab({ tenant, canWrite, onToast = () => {}, focusCandidateId, onQueueChanged }: CurationTabProps) {
  const wsSlug = tenant?.sub || tenant?.id;
  const [subTab, setSubTab] = useState<SubTab>('candidates');
  const [status, setStatus] = useState<ApiCycleStatus | null>(null);
  const [statusLoading, setStatusLoading] = useState(false);
  const [running, setRunning] = useState(false);
  const [runNotice, setRunNotice] = useState<string | null>(null);
  const [openCandidate, setOpenCandidate] = useState<string | null>(focusCandidateId || null);
  const [candidatesRefresh, setCandidatesRefresh] = useState(0);

  const loadStatus = useCallback(async () => {
    if (!wsSlug) return;
    setStatusLoading(true);
    try {
      const res = await api.curation.cycleStatus(wsSlug);
      setStatus(res.status);
    } catch {
      // Status is context, not the review surface — the tab works without it.
      setStatus(null);
    } finally {
      setStatusLoading(false);
    }
  }, [wsSlug]);

  useEffect(() => {
    void loadStatus();
  }, [loadStatus]);

  const runNow = async () => {
    setRunning(true);
    setRunNotice(null);
    try {
      const res = await api.curation.runCycle(wsSlug);
      setStatus(res.status);
      onToast('Curation cycle finished');
      onQueueChanged?.();
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 409) {
        // A cycle is already in flight (manual trigger or the ticker).
        setRunNotice('A curation cycle is already running — it will report here when it finishes.');
      } else {
        onToast(err instanceof ApiError ? err.message : 'Failed to run the curation cycle', 'danger');
      }
    } finally {
      setRunning(false);
    }
  };

  const decided = () => {
    setCandidatesRefresh((n) => n + 1);
    onQueueChanged?.();
    void loadStatus();
  };

  const counters = status?.counters;
  const lastFinished = status?.finished_at || status?.started_at;

  return (
    <div data-od-id="pane-curation" data-testid="pane-curation">
      {/* Cycle status line + run-now */}
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-[15px] font-semibold text-fg">Curation</h3>
          <p className="mt-0.5 text-[12px] leading-4 text-muted" data-testid="curation-status-line">
            {statusLoading && !status ? (
              'Loading cycle status…'
            ) : (
              <>
                Nightly by default — cadence and thresholds are workspace configuration (Settings → Workspace →
                Advanced).{' '}
                {status && status.state !== 'idle' ? (
                  <>
                    Last run{' '}
                    <span data-testid="curation-last-run">{lastFinished ? new Date(lastFinished).toLocaleString() : ''}</span>{' '}
                    — <span data-testid="curation-outcome">{cycleOutcomeLine(status)}</span>.{' '}
                  </>
                ) : (
                  <>No cycle has run in this workspace yet. </>
                )}
                <span data-testid="curation-pattern-count">{counters ? counters.pattern_count : 0} patterns</span>
                {' · '}
                <span data-testid="curation-proposal-count">
                  {counters ? counters.proposals_drafted : 0} proposals drafted
                </span>
                .
              </>
            )}
          </p>
        </div>
        {canWrite ? (
          <button
            type="button"
            onClick={() => void runNow()}
            disabled={running}
            data-testid="btn-run-curation"
            className="flex h-9 shrink-0 items-center gap-1.5 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-60"
          >
            {running ? (
              <>
                <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-line border-t-accent" />
                Running…
              </>
            ) : (
              <>
                <Icon name="play" size={12} /> Run curation now
              </>
            )}
          </button>
        ) : null}
      </div>

      {runNotice ? (
        <p
          className="mb-4 rounded-md border border-[color-mix(in_oklab,var(--warn)_45%,transparent)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)] px-3 py-2 text-[12px] text-[color-mix(in_oklab,var(--warn),black_38%)]"
          data-testid="curation-run-notice"
          role="status"
        >
          {runNotice}
        </p>
      ) : null}

      {/* Sub-tabs */}
      <div className="mb-4 flex gap-1 border-b border-linesoft" role="tablist" aria-label="Curation sections">
        {SUB_TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={subTab === t.id}
            onClick={() => setSubTab(t.id)}
            data-od-id={'curation-tab-' + t.id}
            data-testid={'curation-tab-' + t.id}
            className={cx(
              '-mb-px flex h-9 items-center gap-1.5 border-b-2 px-3 text-[13px] transition-colors',
              subTab === t.id
                ? 'border-accent font-medium text-fg'
                : 'border-transparent text-muted hover:text-fg2'
            )}
          >
            {t.label}
          </button>
        ))}
      </div>

      {subTab === 'candidates' && (
        <CandidatesList wsSlug={wsSlug} onOpen={(c) => setOpenCandidate(c.id)} refreshKey={candidatesRefresh} />
      )}
      {subTab === 'patterns' && <PatternsTab wsSlug={wsSlug} />}
      {subTab === 'audit' && <AuditTab wsSlug={wsSlug} />}

      {openCandidate ? (
        <CandidateTakeover
          wsSlug={wsSlug}
          candidateId={openCandidate}
          canWrite={canWrite}
          onClose={() => setOpenCandidate(null)}
          onDecided={decided}
          onToast={onToast}
        />
      ) : null}
    </div>
  );
}
