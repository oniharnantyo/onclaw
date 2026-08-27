// @ts-nocheck
import React from "react";
import { cx } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { ViewShell } from "../components/ui/ViewShell";
import { LastRunCell } from "../components/ui/LastRunCell";
import { Toggle } from "../components/ui/Toggle";

export function CronView({ tenant, onEdit, onToggle, onRunNow, onNew }) {
  return (
    <ViewShell odId="cron-view" title="Cron schedules"
      sub={'Recurring agent runs for ' + tenant.name + '. Expressions use standard 5-field cron in ' + tenant.tz + '.'}
      action={
        <button type="button" onClick={onNew} data-od-id="btn-new-schedule"
          className="flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]">
          <Icon name="plus" size={15} sw={2.2}/> New schedule
        </button>
      }>
      <div className="overflow-hidden rounded-lg border border-line" data-od-id="cron-table">
        <div className="hidden md:grid grid-cols-[1.4fr_1.1fr_0.7fr_1fr_84px] gap-3 border-b border-linesoft bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-4 py-2.5 font-mono text-[10px] uppercase tracking-[0.14em] text-muted">
          <span>Schedule</span><span>Expression</span><span>Next run</span><span>Last run</span><span className="text-right">State</span>
        </div>
        {tenant.cron.map((j) => {
          const agent = tenant.agents.find((a) => a.id === j.agentId);
          return (
            <div key={j.id} data-od-id={'cron-row-' + j.id}
              className="group flex flex-col gap-3 md:grid md:grid-cols-[1.4fr_1.1fr_0.7fr_1fr_84px] md:items-center md:gap-3 border-b border-linesoft px-4 py-3 transition-colors last:border-b-0 hover:bg-[color-mix(in_oklab,var(--fg)_3%,transparent)]">
              
              <div className="flex items-start justify-between md:contents">
                <div className="min-w-0">
                  <button type="button" onClick={() => onEdit(j)} className="truncate text-left text-[14px] font-medium text-fg hover:text-accent">{j.name}</button>
                  <p className="truncate text-[12px] text-muted">runs {agent ? agent.name : '—'}</p>
                </div>
                <div className="flex items-center justify-end gap-1 md:hidden">
                  <button type="button" onClick={() => onRunNow(j)} data-od-id={'cron-run-' + j.id} title="Run now" aria-label={'Run ' + j.name + ' now'}
                    className="flex h-7 w-7 items-center justify-center rounded-[6px] text-muted hover:bg-[color-mix(in_oklab,var(--fg)_9%,transparent)] hover:text-fg focus-visible:opacity-100 group-hover:opacity-100">
                    <Icon name="play" size={13}/>
                  </button>
                  <Toggle on={j.enabled} onChange={() => onToggle(j)} label={(j.enabled ? 'Pause ' : 'Resume ') + j.name}/>
                </div>
              </div>

              <div className="flex justify-between md:contents">
                <div>
                  <p className="font-mono text-[12px] text-fg2">{j.expr}</p>
                  <p className="text-[11px] text-muted">{j.human}</p>
                </div>
                <div className="text-right md:text-left">
                  <p className="md:hidden text-[10px] uppercase tracking-[0.14em] text-muted mb-0.5">Next run</p>
                  <span className="font-mono text-[12px] text-fg2">{j.enabled ? j.next : '—'}</span>
                </div>
              </div>
              
              <div className="flex items-center justify-between md:contents">
                <div className="md:hidden text-[10px] uppercase tracking-[0.14em] text-muted mr-2">Last run</div>
                <LastRunCell last={j.last}/>
              </div>

              <div className="hidden md:flex items-center justify-end gap-1">
                <button type="button" onClick={() => onRunNow(j)} data-od-id={'cron-run-' + j.id} title="Run now" aria-label={'Run ' + j.name + ' now'}
                  className="flex h-7 w-7 items-center justify-center rounded-[6px] text-muted opacity-0 transition-opacity hover:bg-[color-mix(in_oklab,var(--fg)_9%,transparent)] hover:text-fg focus-visible:opacity-100 group-hover:opacity-100">
                  <Icon name="play" size={13}/>
                </button>
                <Toggle on={j.enabled} onChange={() => onToggle(j)} label={(j.enabled ? 'Pause ' : 'Resume ') + j.name}/>
              </div>
            </div>
          );
        })}
        {tenant.cron.length === 0 && (
          <div className="px-4 py-10 text-center" data-od-id="cron-empty">
            <p className="text-[14px] font-medium text-fg">No schedules yet</p>
            <p className="mt-1 text-[13px] text-muted">Give an agent a recurring job — digests, sweeps, pipeline checks.</p>
          </div>
        )}
      </div>
      <p className="mt-4 text-[12px] leading-5 text-muted">
        Paused schedules keep their expression and history — flip the toggle to resume exactly where they left off.
      </p>
    </ViewShell>
  );
}

