import React, { useState, useMemo, useEffect } from "react";
import { Icon } from "../components/ui/Icon";
import { ViewShell } from "../components/ui/ViewShell";
import { AgentCard } from "./AgentCard";
import type { Workspace, Agent } from "../data/types";
import { useStore } from "../store";

const PAGE_SIZE = 24;

export type AgentSortOption = 'newest' | 'name' | 'oldest';

export interface AgentsViewProps {
  tenant: Workspace;
  onChat: (id: string) => void;
  onConfigure: (id: string) => void;
  onDeploy: () => void;
}

export function AgentsView({ tenant, onChat, onConfigure, onDeploy }: AgentsViewProps) {
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState<AgentSortOption>('newest');
  const [page, setPage] = useState(1);

  const agents = tenant?.agents || [];
  const running = agents.filter((a: any) => a.status === 'running').length;
  const wsId = tenant?.id || tenant?.sub;

  useEffect(() => {
    if (wsId) {
      useStore.getState().loadAgents(wsId);
    }
  }, [wsId]);

  const handleRegenerate = (agentId: string) => {
    if (wsId) {
      useStore.getState().regenerateAgent(wsId, agentId);
    }
  };

  const filteredAgents = useMemo(() => {
    const q = search.trim().toLowerCase();
    const filtered = q
      ? agents.filter((a: Agent) => (a.name || '').toLowerCase().includes(q))
      : agents;

    if (sort === 'name') {
      return [...filtered].sort((a, b) =>
        (a.name || '').localeCompare(b.name || '', undefined, { sensitivity: 'base' })
      );
    }
    if (sort === 'oldest') {
      return [...filtered].reverse();
    }
    return filtered;
  }, [agents, search, sort]);

  const pageCount = Math.max(1, Math.ceil(filteredAgents.length / PAGE_SIZE));
  const currentPage = Math.max(1, Math.min(page, pageCount));
  const startIndex = (currentPage - 1) * PAGE_SIZE;
  const pagedAgents = filteredAgents.slice(startIndex, startIndex + PAGE_SIZE);

  const isSearchActive = search.trim().length > 0;
  const countLabel = isSearchActive
    ? `${filteredAgents.length} of ${agents.length}`
    : `${agents.length}`;

  return (
    <ViewShell
      odId="agents-view"
      title="Agents"
      sub={
        countLabel +
        ' agents in ' +
        (tenant?.name || '') +
        ' — ' +
        running +
        ' running right now. Configuration lives in Settings → Agents.'
      }
      action={
        <button
          type="button"
          onClick={onDeploy}
          data-od-id="btn-deploy-agent"
          data-testid="btn-deploy-agent"
          className="flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]"
        >
          <Icon name="plus" size={15} sw={2.2} /> Deploy agent
        </button>
      }
    >
      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div className="relative flex-1 max-w-xs sm:max-w-sm">
          <span className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted">
            <Icon name="search" size={14} />
          </span>
          <input
            type="text"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setPage(1);
            }}
            placeholder="Search agents…"
            aria-label="Search agents"
            data-od-id="agents-search"
            data-testid="agents-search"
            className="h-8 w-full rounded-md border border-line bg-surface pl-8 pr-2.5 text-[13px] text-fg2 placeholder:text-muted focus:border-accent outline-none"
          />
        </div>

        <div className="relative">
          <select
            value={sort}
            onChange={(e) => {
              setSort(e.target.value as AgentSortOption);
              setPage(1);
            }}
            aria-label="Sort agents"
            data-od-id="agents-sort"
            data-testid="agents-sort"
            className="h-8 appearance-none rounded-md border border-line bg-surface pl-3 pr-8 text-[13px] font-medium text-fg2 focus:border-accent outline-none cursor-pointer"
          >
            <option value="newest">Newest first</option>
            <option value="name">Name A–Z</option>
            <option value="oldest">Oldest</option>
          </select>
          <span className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-muted">
            <Icon name="chevdown" size={14} />
          </span>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {pagedAgents.map((a: Agent) => (
          <AgentCard
            key={a.id}
            a={a}
            onChat={() => onChat(a.id)}
            onConfigure={() => onConfigure(a.id)}
            onRegenerate={() => handleRegenerate(a.id)}
          />
        ))}

        {agents.length > 0 && filteredAgents.length === 0 && (
          <div
            className="rounded-lg border border-dashed border-line px-6 py-12 text-center col-span-full"
            data-od-id="agents-no-match"
            data-testid="agents-no-match"
          >
            <div className="mx-auto mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
              <Icon name="search" size={20} />
            </div>
            <p className="text-[15px] font-medium text-fg">
              No agents match &quot;{search}&quot;
            </p>
            <p className="mt-1 text-[13px] text-muted">
              Try searching for a different name or clear the filter.
            </p>
            <button
              type="button"
              onClick={() => {
                setSearch('');
                setPage(1);
              }}
              data-testid="btn-clear-search"
              className="mt-4 inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              <Icon name="x" size={13} /> Clear search
            </button>
          </div>
        )}

        {agents.length === 0 && (
          <div
            className="rounded-lg border border-dashed border-line px-6 py-12 text-center col-span-full"
            data-od-id="agents-empty"
            data-testid="agents-empty"
          >
            <div className="mx-auto mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
              <Icon name="bot" size={22} />
            </div>
            <p className="text-[15px] font-medium text-fg">No agents in {tenant?.name || ''} yet</p>
            <p className="mt-1 text-[13px] text-muted">
              Deploy one and it lands in the sidebar, ready to chat.
            </p>
            <button
              type="button"
              onClick={onDeploy}
              data-testid="btn-agents-empty-deploy"
              className="mt-4 inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              <Icon name="plus" size={13} /> Deploy your first agent
            </button>
          </div>
        )}
      </div>

      {filteredAgents.length > PAGE_SIZE && (
        <nav
          aria-label="Pagination"
          data-od-id="agents-pager"
          data-testid="agents-pager"
          className="mt-8 flex items-center justify-center gap-3 text-[13px] text-muted"
        >
          <button
            type="button"
            onClick={() => setPage(Math.max(1, currentPage - 1))}
            disabled={currentPage <= 1}
            data-testid="agents-pager-prev"
            className="inline-flex items-center gap-1 font-medium text-fg2 transition-colors hover:text-fg disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:text-fg2"
          >
            ‹ Prev
          </button>
          <span>·</span>
          <span data-testid="agents-pager-label">
            Page {currentPage} of {pageCount}
          </span>
          <span>·</span>
          <button
            type="button"
            onClick={() => setPage(Math.min(pageCount, currentPage + 1))}
            disabled={currentPage >= pageCount}
            data-testid="agents-pager-next"
            className="inline-flex items-center gap-1 font-medium text-fg2 transition-colors hover:text-fg disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:text-fg2"
          >
            Next ›
          </button>
        </nav>
      )}
    </ViewShell>
  );
}
