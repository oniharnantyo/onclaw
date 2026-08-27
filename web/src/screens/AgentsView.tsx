// @ts-nocheck
import React from "react";
import { cx } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { ViewShell } from "../components/ui/ViewShell";
import { AgentCard } from "./AgentCard";

export function AgentsView({ tenant, onChat, onConfigure, onDeploy }) {
  const running = tenant.agents.filter((a) => a.status === 'running').length;
  return (
    <ViewShell odId="agents-view" title="Agents"
      sub={tenant.agents.length + ' agents in ' + tenant.name + ' — ' + running + ' running right now. Configuration lives in Settings → Agents.'}
      action={
        <button type="button" onClick={onDeploy} data-od-id="btn-deploy-agent"
          className="flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]">
          <Icon name="plus" size={15} sw={2.2}/> Deploy agent
        </button>
      }>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {tenant.agents.map((a) => (
          <AgentCard key={a.id} a={a} onChat={() => onChat(a.id)} onConfigure={() => onConfigure(a.id)}/>
        ))}
        {tenant.agents.length === 0 && (
          <div className="rounded-lg border border-dashed border-line px-6 py-12 text-center sm:col-span-2 xl:col-span-3" data-od-id="agents-empty">
            <p className="text-[15px] font-medium text-fg">No agents in {tenant.name} yet</p>
            <p className="mt-1 text-[13px] text-muted">Deploy one and it lands in the sidebar, ready to chat.</p>
          </div>
        )}
      </div>
    </ViewShell>
  );
}

