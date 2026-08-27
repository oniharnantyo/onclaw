import { useState } from "react";
import { cx } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { ViewShell } from "../components/ui/ViewShell";
import { Chip } from "../components/ui/Chip";

export function RunsView({ tenant }: { tenant: Workspace }) {
  const [filter, setFilter] = useState('all');
  const runs = tenant.runs.filter((r: any) => filter === 'all' || r.status === filter);
  return (
    <ViewShell odId="runs-view" title="Run history"
      sub={'Every agent execution in ' + tenant.name + ' — chat turns, cron fires and API calls.'}>
      <div className="mb-4 flex gap-2" data-od-id="runs-filters">
        {[{ id: 'all', label: 'All' }, { id: 'success', label: 'Succeeded' }, { id: 'failed', label: 'Failed' }].map((f) => (
          <button key={f.id} type="button" onClick={() => setFilter(f.id)}
            className={cx('flex h-8 items-center rounded-md border px-3 text-[12px] font-medium transition-colors',
              filter === f.id ? 'border-accent bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg' : 'border-line text-muted hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg2')}>
            {f.label}
          </button>
        ))}
      </div>
      <div className="overflow-hidden rounded-lg border border-line" data-od-id="runs-table">
        <div className="hidden md:grid grid-cols-[110px_1fr_90px_110px_90px_80px_90px] gap-3 border-b border-linesoft bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-4 py-2.5 font-mono text-[10px] uppercase tracking-[0.14em] text-muted">
          <span>Run</span><span>Agent</span><span>Trigger</span><span>Started</span><span>Duration</span><span>Tokens</span><span>Status</span>
        </div>
        {runs.map((r: any) => {
          const agent = tenant.agents.find((a: Agent) => a.id === r.agentId);
          return (
            <div key={r.id} data-od-id={'run-row-' + r.id}
              className="flex flex-col gap-2 md:grid md:grid-cols-[110px_1fr_90px_110px_90px_80px_90px] md:items-center md:gap-3 border-b border-linesoft px-4 py-3 md:py-2.5 transition-colors last:border-b-0 hover:bg-[color-mix(in_oklab,var(--fg)_3%,transparent)]">
              
              {/* Mobile top row: Run ID and Status */}
              <div className="flex items-center justify-between md:contents">
                <span className="font-mono text-[12px] text-fg2 md:text-left"><span className="md:hidden text-[10px] uppercase tracking-wider text-muted mr-2">Run</span>{r.id}</span>
                <span className={cx('flex items-center gap-1.5 font-mono text-[11px] md:justify-self-start', r.status === 'success' ? 'text-[color-mix(in_oklab,var(--success),black_25%)]' : r.status === 'running' ? 'text-accent' : 'text-danger')}>
                  <Icon name={r.status === 'success' ? 'check' : r.status === 'running' ? 'activity' : 'x'} size={12}/>{r.status}
                </span>
              </div>
              
              {/* Agent and Trigger */}
              <div className="flex items-center justify-between md:contents">
                <span className="truncate text-[13px] font-medium text-fg md:font-normal"><span className="md:hidden text-[10px] uppercase tracking-wider text-muted font-mono mr-2">Agent</span>{agent ? agent.name : '—'}</span>
                <Chip mono className="justify-self-start">{r.trigger}</Chip>
              </div>

              {/* Started, Duration, Tokens for mobile */}
              <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 md:contents">
                <span className="font-mono text-[12px] text-fg2"><span className="md:hidden text-[10px] uppercase tracking-wider text-muted mr-2">Started</span>{r.when}</span>
                <span className="font-mono text-[12px] text-fg2"><span className="md:hidden text-[10px] uppercase tracking-wider text-muted mr-2">Duration</span>{r.dur}</span>
                <span className="font-mono text-[12px] text-fg2"><span className="md:hidden text-[10px] uppercase tracking-wider text-muted mr-2">Tokens</span>{r.tokens}</span>
              </div>
              
            </div>
          );
        })}
        {tenant.runs.length === 0
          ? <p className="px-4 py-6 text-center text-[13px] text-muted">No runs yet — chat with an agent or fire a schedule.</p>
          : runs.length === 0 && <p className="px-4 py-6 text-center text-[13px] text-muted">No runs match this filter.</p>}
      </div>
    </ViewShell>
  );
}

