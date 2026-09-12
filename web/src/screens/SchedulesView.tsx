import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { relativeTime, formatDuration, fmtNextRun } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { ViewShell } from "../components/ui/ViewShell";
import { LastRunCell } from "../components/ui/LastRunCell";
import { Toggle } from "../components/ui/Toggle";
import { useStore } from "../store";
import { schedulers, type Scheduler } from "../lib/schedulers";
import { ApiError } from "../lib/api";

/** Schedules screen on the live API (change integrate-scheduler, 7.2). The
 * store copy (tenant.schedules) is the render source; a load on mount — and
 * after every mutation — keeps the sidebar counts and this table in step. */
export function SchedulesView({ tenant, onEdit, onNew, onToast }: {
  tenant: any;
  onEdit: (s: Scheduler) => void;
  onNew: () => void;
  onToast: (text: string, kind?: string) => void;
}) {
  const navigate = useNavigate();
  const agents: any[] = tenant?.agents || [];
  const wsId: string = tenant?.id || tenant?.sub;
  // The store copy is the render source — mutations patch it optimistically
  // and the load on mount (and after editor saves) keeps it authoritative.
  const storeSchedules = useStore((s: any) => s.db[s.pos.tenantId]?.schedules);
  const schedules: Scheduler[] = storeSchedules ?? tenant?.schedules ?? [];
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [pendingId, setPendingId] = useState<string | null>(null);
  const alive = useRef(true);
  // Reset on (re)mount: StrictMode's dev double-invoke runs cleanup then the
  // effect again — the ref survives, so without the reset every post-await
  // guard below would bail and the spinner would never clear.
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; };
  }, []);

  const refresh = async () => {
    setLoading(true);
    setError(false);
    const ok = await useStore.getState().loadSchedules(wsId);
    if (!alive.current) return;
    setLoading(false);
    setError(!ok);
  };

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wsId]);

  const agentName = (s: Scheduler): string =>
    agents.find((a) => a.id === s.agent_id || a.slug === s.agent_id)?.name || '—';

  const toggle = async (s: Scheduler) => {
    if (pendingId) return;
    setPendingId(s.id);
    // Optimistic flip — the row answers instantly, the server response is
    // the truth and reconciles next_run_at on resume.
    useStore.getState().updateTenant(wsId, (t: any) => ({
      ...t,
      schedules: (t.schedules || []).map((x: Scheduler) => (x.id === s.id ? { ...x, enabled: !x.enabled } : x)),
    }));
    try {
      await schedulers.update(wsId, s.id, { enabled: !s.enabled });
      onToast((s.enabled ? 'Paused “' : 'Resumed “') + s.name + '”');
    } catch (err) {
      // Roll the optimistic flip back — the toggle never lies.
      useStore.getState().updateTenant(wsId, (t: any) => ({
        ...t,
        schedules: (t.schedules || []).map((x: Scheduler) => (x.id === s.id ? { ...x, enabled: s.enabled } : x)),
      }));
      onToast(err instanceof ApiError && err.status === 403
        ? "You don't have permission to change schedules."
        : 'Failed to update the schedule', 'danger');
    } finally {
      setPendingId(null);
    }
  };

  const runNow = async (s: Scheduler) => {
    if (pendingId) return;
    setPendingId(s.id);
    try {
      await schedulers.run(wsId, s.id);
      onToast('Triggered “' + s.name + '”');
    } catch (err) {
      // 409: a run is already in flight (design D9) — a notice, not a failure.
      onToast(err instanceof ApiError && err.status === 409
        ? '“' + s.name + '” is already running'
        : 'Failed to trigger “' + s.name + '”', 'danger');
    } finally {
      setPendingId(null);
    }
  };

  const openLastRun = (s: Scheduler) => {
    navigate('/runs?scheduler=' + encodeURIComponent(s.id));
  };

  const lastRunCell = (s: Scheduler) => {
    const last = s.last_run;
    if (!last) return <span className="text-[12px] text-muted">never</span>;
    return (
      <button type="button" onClick={() => openLastRun(s)} data-od-id={'schedule-lastrun-' + s.id}
        title={'Open runs for “' + s.name + '”'}
        className="flex items-center gap-1.5 text-[12px] text-fg2 rounded-[5px] transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg">
        <LastRunCell last={{ status: last.status, when: relativeTime(last.started_at), dur: formatDuration(last.duration_ms) }}/>
      </button>
    );
  };

  return (
    <ViewShell odId="schedules-view" title="Schedules"
      sub={'Recurring agent runs for ' + tenant.name + '. Recurrences use standard 5-field cron in ' + (tenant.tz || 'the workspace timezone') + '.'}
      action={
        <button type="button" onClick={onNew} data-od-id="btn-new-schedule"
          className="flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]">
          <Icon name="plus" size={15} sw={2.2}/> New schedule
        </button>
      }>
      {error ? (
        <div className="rounded-lg border border-line px-4 py-10 text-center" data-od-id="schedules-error">
          <p className="text-[14px] font-medium text-fg">Couldn't load schedules</p>
          <p className="mt-1 text-[13px] text-muted">Check the connection and try again.</p>
          <button type="button" onClick={() => void refresh()} data-od-id="btn-schedules-retry"
            className="mt-4 inline-flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg">
            Retry
          </button>
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-line" data-od-id="schedules-table">
          <div className="hidden md:grid grid-cols-[1.4fr_1.3fr_0.8fr_1.1fr_84px] gap-3 border-b border-linesoft bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-4 py-2.5 font-mono text-[10px] uppercase tracking-[0.14em] text-muted">
            <span>Schedule</span><span>Recurrence</span><span>Next run</span><span>Last run</span><span className="text-right">State</span>
          </div>
          {loading && schedules.length === 0 && (
            <div className="flex items-center justify-center gap-3 px-4 py-10" data-od-id="schedules-loading">
              <span className="h-5 w-5 animate-spin rounded-full border-2 border-line border-t-accent"/>
              <span className="text-[13px] text-muted">Loading schedules…</span>
            </div>
          )}
          {!loading && schedules.map((s) => (
            <div key={s.id} data-od-id={'schedule-row-' + s.id}
              className="group flex flex-col gap-3 md:grid md:grid-cols-[1.4fr_1.3fr_0.8fr_1.1fr_84px] md:items-center md:gap-3 border-b border-linesoft px-4 py-3 transition-colors last:border-b-0 hover:bg-[color-mix(in_oklab,var(--fg)_3%,transparent)]">

              <div className="flex items-start justify-between md:contents">
                <div className="min-w-0">
                  <button type="button" onClick={() => onEdit(s)} data-od-id={'schedule-name-' + s.id}
                    className="truncate text-left text-[14px] font-medium text-fg hover:text-accent">{s.name}</button>
                  <p className="truncate text-[12px] text-muted">runs {agentName(s)}</p>
                </div>
                <div className="flex items-center justify-end gap-1 md:hidden">
                  <button type="button" onClick={() => void runNow(s)} data-od-id={'schedule-run-' + s.id} title="Run now" aria-label={'Run ' + s.name + ' now'}
                    className="flex h-7 w-7 items-center justify-center rounded-[6px] text-muted hover:bg-[color-mix(in_oklab,var(--fg)_9%,transparent)] hover:text-fg focus-visible:opacity-100 group-hover:opacity-100">
                    <Icon name="play" size={13}/>
                  </button>
                  <Toggle on={s.enabled} onChange={() => void toggle(s)} label={(s.enabled ? 'Pause ' : 'Resume ') + s.name}/>
                </div>
              </div>

              <div className="flex justify-between md:contents">
                <div className="min-w-0">
                  <p className="truncate text-[12px] text-fg2">{s.human_label || s.expr}</p>
                  {/* Custom recurrences keep the raw expression visible next to
                      the label (schedules spec: table requirement). */}
                  {s.human_label && s.human_label !== s.expr && (
                    <p className="truncate font-mono text-[11px] text-muted">{s.expr}</p>
                  )}
                </div>
                <div className="text-right md:text-left">
                  <p className="md:hidden text-[10px] uppercase tracking-[0.14em] text-muted mb-0.5">Next run</p>
                  <span className="font-mono text-[12px] text-fg2" data-od-id={'schedule-next-' + s.id}>
                    {s.enabled ? fmtNextRun(s.next_run_at, tenant.tz) : '—'}
                  </span>
                </div>
              </div>

              <div className="flex items-center justify-between md:contents">
                <div className="md:hidden text-[10px] uppercase tracking-[0.14em] text-muted mr-2">Last run</div>
                {lastRunCell(s)}
              </div>

              <div className="hidden md:flex items-center justify-end gap-1">
                <button type="button" onClick={() => void runNow(s)} data-od-id={'schedule-run-' + s.id} title="Run now" aria-label={'Run ' + s.name + ' now'}
                  className="flex h-7 w-7 items-center justify-center rounded-[6px] text-muted opacity-0 transition-opacity hover:bg-[color-mix(in_oklab,var(--fg)_9%,transparent)] hover:text-fg focus-visible:opacity-100 group-hover:opacity-100">
                  <Icon name="play" size={13}/>
                </button>
                <Toggle on={s.enabled} onChange={() => void toggle(s)} label={(s.enabled ? 'Pause ' : 'Resume ') + s.name}/>
              </div>
            </div>
          ))}
          {!loading && schedules.length === 0 && (
            <div className="px-4 py-10 text-center" data-od-id="schedules-empty">
              <p className="text-[14px] font-medium text-fg">No schedules yet</p>
              <p className="mt-1 text-[13px] text-muted">Give an agent a recurring job — digests, sweeps, pipeline checks.</p>
              <button type="button" onClick={onNew} data-od-id="btn-empty-new-schedule"
                className="mt-4 inline-flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]">
                <Icon name="plus" size={15} sw={2.2}/> New schedule
              </button>
            </div>
          )}
        </div>
      )}
      <p className="mt-4 text-[12px] leading-5 text-muted">
        Paused schedules keep their recurrence and history — flip the toggle to resume at the next future occurrence.
      </p>
    </ViewShell>
  );
}
