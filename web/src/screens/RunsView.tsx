import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { cx, formatDuration, formatTokens, fmtRunStarted } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { ViewShell } from "../components/ui/ViewShell";
import { Chip } from "../components/ui/Chip";
import { useStore } from "../store";
import { schedulers, type SchedulerRun } from "../lib/schedulers";
import { ApiError } from "../lib/api";

const PAGE = 100;

const FILTERS = [
  { id: 'all', label: 'All' },
  { id: 'completed', label: 'Succeeded' },
  { id: 'failed', label: 'Failed' },
];

const statusIcon = (s: SchedulerRun['status']): { icon: string; cls: string } | null => {
  switch (s) {
    case 'completed': return { icon: 'check', cls: 'text-[color-mix(in_oklab,var(--success),black_25%)]' };
    case 'failed': return { icon: 'x', cls: 'text-danger' };
    case 'running': return { icon: 'activity', cls: 'text-accent' };
    default: return { icon: 'clock', cls: 'text-muted' }; // cancelled / blocked / missed
  }
};

/** Run history on the live scheduler-runs endpoints (integrate-scheduler 7.4).
 * The workspace-wide feed is the default; `?scheduler=<id>` narrows to one
 * scheduler's runs (the schedules screen's last-run cell links here). Rows
 * open the run's transcript in its agent's chat. */
export function RunsView({ tenant, onToast }: {
  tenant: any;
  onToast: (text: string, kind?: string) => void;
}) {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const schedulerId = searchParams.get('scheduler');
  const agents: any[] = tenant?.agents || [];
  const schedules: any[] = tenant?.schedules || [];
  const wsId: string = tenant?.id || tenant?.sub;

  const [runs, setRuns] = useState<SchedulerRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [filter, setFilter] = useState('all');
  const alive = useRef(true);
  // Reset on (re)mount: StrictMode's dev double-invoke runs cleanup then the
  // effect again — the ref survives, so without the reset every post-await
  // guard below would bail and the spinner would never clear.
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; };
  }, []);

  useEffect(() => {
    setLoading(true);
    setError(false);
    const load = schedulerId
      ? schedulers.runs(wsId, schedulerId, { limit: PAGE })
        .then((res) => res?.runs || [])
      : schedulers.listRuns(wsId, { limit: PAGE })
        .then((res) => {
          const rows = res?.runs || [];
          // Mirror the workspace-wide feed into the store so the sidebar's
          // run counts read live data too.
          useStore.getState().updateTenant(wsId, (t: any) => ({ ...t, runs: rows }));
          return rows;
        });
    load.then((rows) => {
      if (!alive.current) return;
      setRuns(rows);
      setLoading(false);
    }).catch(() => {
      if (!alive.current) return;
      setRuns([]);
      setLoading(false);
      setError(true);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wsId, schedulerId]);

  const filtered = runs.filter((r) => filter === 'all' || r.status === filter);

  const agentName = (r: SchedulerRun): string =>
    r.agent_name || agents.find((a) => a.id === r.agent_id || a.slug === r.agent_id)?.name || '—';

  const schedulerName = (r: SchedulerRun): string =>
    r.scheduler_name || schedules.find((s) => s.id === r.scheduler_id)?.name || '';

  // Langfuse deep link (integrate-langfuse-tracing D6): the action renders
  // only when the run payload carries a non-empty URL — null/absent (tracing
  // unconfigured or the run predates tracing) means no action at all.
  const langfuseUrl = (r: SchedulerRun): string =>
    typeof r.langfuse_url === 'string' && r.langfuse_url ? r.langfuse_url : '';

  const openRun = (r: SchedulerRun) => {
    // A run's transcript is a session of the scheduler's agent (D7) — open it
    // through the normal chat route; ChatRoute consumes the run handoff,
    // injects the session, and hydrates the transcript by session id.
    const agentId = r.agent_id || agents.find((a) => a.name === r.agent_name)?.id;
    if (!agentId) {
      onToast("This run's agent is no longer available", 'danger');
      return;
    }
    const lf = langfuseUrl(r);
    navigate('/c/' + encodeURIComponent(agentId), {
      state: { openRun: {
        sessionId: r.session_id,
        schedulerName: schedulerName(r),
        // Traced runs carry the deep link so the transcript view can offer
        // the same action; untraced runs omit the key entirely.
        ...(lf ? { langfuseUrl: lf } : {}),
      } },
    });
  };

  return (
    <ViewShell odId="runs-view" title="Run history"
      sub={'Every scheduled agent execution in ' + tenant.name + ' — scheduled fires and run-now triggers.'}>
      {schedulerId && (
        <div className="mb-4 flex items-center gap-2" data-od-id="runs-scheduler-filter">
          <Chip mono>scheduler · {schedules.find((s) => s.id === schedulerId)?.name || schedulerId}</Chip>
          <button type="button" onClick={() => setSearchParams({})}
            className="flex h-7 items-center gap-1 rounded-md px-2 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2">
            <Icon name="x" size={12}/> Show all runs
          </button>
        </div>
      )}
      <div className="mb-4 flex gap-2" data-od-id="runs-filters">
        {FILTERS.map((f) => (
          <button key={f.id} type="button" onClick={() => setFilter(f.id)}
            className={cx('flex h-8 items-center rounded-md border px-3 text-[12px] font-medium transition-colors',
              filter === f.id ? 'border-accent bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg' : 'border-line text-muted hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg2')}>
            {f.label}
          </button>
        ))}
      </div>
      {error ? (
        <div className="rounded-lg border border-line px-4 py-10 text-center" data-od-id="runs-error">
          <p className="text-[14px] font-medium text-fg">Couldn't load runs</p>
          <p className="mt-1 text-[13px] text-muted">Check the connection and try again.</p>
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-line" data-od-id="runs-table">
          <div className="hidden md:grid grid-cols-[130px_1fr_90px_130px_90px_80px_100px] gap-3 border-b border-linesoft bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-4 py-2.5 font-mono text-[10px] uppercase tracking-[0.14em] text-muted">
            <span>Run</span><span>Agent</span><span>Trigger</span><span>Started</span><span>Duration</span><span>Tokens</span><span>Status</span>
          </div>
          {loading && runs.length === 0 && (
            <div className="flex items-center justify-center gap-3 px-4 py-10" data-od-id="runs-loading">
              <span className="h-5 w-5 animate-spin rounded-full border-2 border-line border-t-accent"/>
              <span className="text-[13px] text-muted">Loading runs…</span>
            </div>
          )}
          {!loading && filtered.map((r) => {
            const st = statusIcon(r.status);
            const clickable = Boolean(r.session_id);
            return (
              <div key={r.id} data-od-id={'run-row-' + r.id}
                role={clickable ? 'button' : undefined} tabIndex={clickable ? 0 : undefined}
                onClick={clickable ? () => openRun(r) : undefined}
                onKeyDown={clickable ? (e) => { if (e.key === 'Enter' || e.key === ' ') openRun(r); } : undefined}
                title={clickable ? 'Open transcript' : undefined}
                className={cx('flex flex-col gap-2 md:grid md:grid-cols-[130px_1fr_90px_130px_90px_80px_100px] md:items-center md:gap-3 border-b border-linesoft px-4 py-3 md:py-2.5 transition-colors last:border-b-0',
                  clickable && 'cursor-pointer hover:bg-[color-mix(in_oklab,var(--fg)_3%,transparent)]')}>

                {/* Mobile top row: Run ID and Status */}
                <div className="flex items-center justify-between md:contents">
                  <span className="flex items-center gap-1.5 font-mono text-[12px] text-fg2 md:text-left">
                    <span><span className="md:hidden text-[10px] uppercase tracking-wider text-muted mr-2">Run</span>{r.id}</span>
                    {langfuseUrl(r) && (
                      <a href={langfuseUrl(r)} target="_blank" rel="noopener"
                        data-od-id={'run-langfuse-' + r.id} title="Open in Langfuse" aria-label="Open in Langfuse"
                        onClick={(e) => e.stopPropagation()}
                        className="flex h-5 w-5 items-center justify-center rounded text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg2">
                        <Icon name="external-link" size={12}/>
                      </a>
                    )}
                  </span>
                  <span className={cx('flex items-center gap-1.5 font-mono text-[11px] md:justify-self-start', st?.cls)}>
                    <Icon name={st?.icon || 'clock'} size={12}/>{r.status}
                  </span>
                </div>

                {/* Agent and Trigger */}
                <div className="flex items-center justify-between md:contents">
                  <span className="truncate text-[13px] font-medium text-fg md:font-normal"><span className="md:hidden text-[10px] uppercase tracking-wider text-muted font-mono mr-2">Agent</span>{agentName(r)}</span>
                  <Chip mono className="justify-self-start">{r.trigger}</Chip>
                </div>

                {/* Started, Duration, Tokens for mobile */}
                <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 md:contents">
                  <span className="font-mono text-[12px] text-fg2"><span className="md:hidden text-[10px] uppercase tracking-wider text-muted mr-2">Started</span>{fmtRunStarted(r.started_at, tenant.tz)}</span>
                  <span className="font-mono text-[12px] text-fg2"><span className="md:hidden text-[10px] uppercase tracking-wider text-muted mr-2">Duration</span>{formatDuration(r.duration_ms)}</span>
                  <span className="font-mono text-[12px] text-fg2"><span className="md:hidden text-[10px] uppercase tracking-wider text-muted mr-2">Tokens</span>{formatTokens(r.tokens_used)}</span>
                </div>

              </div>
            );
          })}
          {!loading && runs.length === 0 && (
            <div className="px-4 py-10 text-center" data-od-id="runs-empty">
              <p className="text-[14px] font-medium text-fg">No runs yet</p>
              <p className="mt-1 text-[13px] text-muted">Create a schedule to run an agent unattended — fires land here with their transcripts.</p>
              <button type="button" onClick={() => navigate('/schedules')} data-od-id="btn-runs-new-schedule"
                className="mt-4 inline-flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]">
                <Icon name="plus" size={15} sw={2.2}/> New schedule
              </button>
            </div>
          )}
          {!loading && runs.length > 0 && filtered.length === 0 && (
            <p className="px-4 py-6 text-center text-[13px] text-muted" data-od-id="runs-filter-empty">No runs match this filter.</p>
          )}
        </div>
      )}
    </ViewShell>
  );
}
