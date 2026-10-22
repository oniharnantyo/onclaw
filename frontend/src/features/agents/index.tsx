import { useEffect, useState } from "react";
import type {
  KeyboardEvent as ReactKeyboardEvent,
  MouseEvent as ReactMouseEvent,
  ReactElement,
} from "react";
import { MessageSquare, Plus, RefreshCw } from "lucide-react";

import type { Agent } from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { AgentAvatar, EmptyState, IconButton } from "../../components";
import { Button } from "../../components/ui/Button";
import { Skeleton } from "../../components/ui/Skeleton";
import { StatusDot, type StatusDotStatus } from "../../components/ui/StatusDot";
import { cn } from "../../lib/cn";
import { client } from "../../lib/client";
import { useNavStore } from "../../stores/nav";
import { AgentConfigPane } from "./components/AgentConfigPane";
import { CreateAgentModal } from "./components/CreateAgentModal";
import { PROMPTS_STATUS } from "./types";

/**
 * Agents menu (tasks 6.1–6.3, spec agent-management master-detail):
 * A master-detail layout composed of a fixed-width list column on the left (~w-72)
 * showing agent avatars, names, model lines, prompt lifecycle status with retry,
 * and chat affordances; and an AgentConfigPane detail column on the right for editing
 * the selected agent's full configuration in place without a modal dialog.
 */
export default function AgentsView(): ReactElement {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const openChat = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.openChat
  );

  useEffect(() => {
    let mounted = true;

    client.agents
      .list()
      .then((list: Agent[]) => {
        if (!mounted) return;
        setAgents(list);

        if (list.length > 0) {
          setSelectedID((prev: string | null): string | null =>
            prev && list.some((a: Agent): boolean => a.ID === prev)
              ? prev
              : list[0].ID
          );
        }
      })
      .catch((err: Error) => {
        if (mounted)
          setError(
            err instanceof Error ? err.message : "Failed to load agents"
          );
      })
      .finally(() => {
        if (mounted) setLoading(false);
      });

    return (): void => {
      mounted = false;
    };
  }, []);

  /**
   * Refreshes or appends an agent row in place without full unmount of the list.
   */
  const upsert = (agent: Agent): void => {
    setAgents((prev: Agent[]) => {
      const exists = prev.some((a: Agent) => a.ID === agent.ID);

      return exists
        ? prev.map((a: Agent) => (a.ID === agent.ID ? agent : a))
        : [...prev, agent];
    });
    setSelectedID(agent.ID);
  };

  /**
   * Deleting an agent removes it from the list and selects the next available agent,
   * or transitions to the empty state when no agents remain.
   */
  const handleDeleted = (id: string): void => {
    setAgents((prev: Agent[]) => {
      const index = prev.findIndex((a: Agent) => a.ID === id);
      const remaining = prev.filter((a: Agent) => a.ID !== id);

      if (remaining.length === 0) {
        setSelectedID(null);
      } else if (selectedID === id) {
        const nextIndex = Math.min(index, remaining.length - 1);
        setSelectedID(remaining[nextIndex].ID);
      }

      return remaining;
    });
  };

  /**
   * List retry for a failed prompt generation: marks generating immediately,
   * then reconciles with the settled agent row.
   */
  const retryGeneration = (id: string): void => {
    const current = agents.find((a: Agent) => a.ID === id);

    if (!current) return;

    upsert({
      ...current,
      PromptsStatus: PROMPTS_STATUS.GENERATING,
      PromptsError: "",
    });

    client.agents
      .regeneratePrompts(id, "")
      .then((): Promise<Agent> => client.agents.get(id))
      .then((fresh: Agent): void => upsert(fresh))
      .catch((): void => {
        client.agents
          .list()
          .then((list: Agent[]): void => setAgents(list))
          .catch((): void => undefined);
      });
  };

  const selectedAgent =
    agents.find((a: Agent) => a.ID === selectedID) ??
    (agents.length > 0 ? agents[0] : null);

  return (
    // lg:pt-10 clears the window's first 25px native invisible titlebar drag band
    <div
      className="flex h-full min-h-0 min-w-0 flex-1 overflow-hidden lg:pt-10"
      data-testid="agents-view"
    >
      {/* List Column (~w-72 fixed width) */}
      <aside
        className="flex w-72 shrink-0 flex-col border-r border-line bg-surface/50"
        data-testid="agents-list-column"
        aria-label="Agents list"
      >
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-linesoft px-4">
          <h1 className="text-[15px] font-semibold text-fg">Agents</h1>
          <IconButton
            label="New agent"
            size="sm"
            testId="btn-new-agent"
            icon={<Plus aria-hidden="true" className="size-4" />}
            onClick={() => setCreating(true)}
          />
        </header>

        {error && (
          <p
            className="m-3 text-sm text-danger"
            role="alert"
            data-testid="agents-error"
          >
            {error}
          </p>
        )}

        <div className="od-scroll flex-1 overflow-y-auto p-2">
          {loading ? (
            <div className="space-y-2 p-2" aria-label="Loading agents">
              {[0, 1, 2].map((i: number) => (
                <div key={i} className="flex items-center gap-3 rounded-md p-2">
                  <Skeleton className="size-9 rounded-full" />
                  <div className="flex-1 space-y-1.5">
                    <Skeleton className="h-3.5 w-24" />
                    <Skeleton className="h-2.5 w-32" />
                  </div>
                </div>
              ))}
            </div>
          ) : agents.length === 0 ? (
            <div className="p-4">
              <EmptyState
                title="No agents yet"
                description="Create one and it is ready to chat."
                testId="agents-empty"
                action={
                  <Button
                    size="sm"
                    data-testid="btn-agents-empty-create"
                    onClick={() => setCreating(true)}
                  >
                    Create your first agent
                  </Button>
                }
              />
            </div>
          ) : (
            <div
              className="flex flex-col gap-1"
              role="list"
              aria-label="Agents"
              data-testid="agents-list"
            >
              {agents.map((agent: Agent) => {
                const isSelected = selectedAgent?.ID === agent.ID;

                const generating =
                  agent.PromptsStatus === PROMPTS_STATUS.GENERATING;

                const failed = agent.PromptsStatus === PROMPTS_STATUS.FAILED;

                const presence: StatusDotStatus = failed
                  ? "error"
                  : generating
                    ? "running"
                    : "idle";

                const metaLine = agent.Model || "No model selected";

                return (
                  <div
                    key={agent.ID}
                    role="button"
                    tabIndex={0}
                    aria-selected={isSelected}
                    data-testid={`agent-item-${agent.ID}`}
                    onClick={() => setSelectedID(agent.ID)}
                    onKeyDown={(e: ReactKeyboardEvent<HTMLDivElement>) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        setSelectedID(agent.ID);
                      }
                    }}
                    className={cn(
                      "group relative flex flex-col gap-1.5 rounded-md p-2.5 text-left transition-colors cursor-pointer",
                      isSelected
                        ? "bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg"
                        : "text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
                    )}
                  >
                    <div className="flex items-center gap-2.5 min-w-0">
                      <AgentAvatar
                        id={agent.ID}
                        avatar={agent.Avatar}
                        size={32}
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5 min-w-0">
                          <span className="truncate text-[13px] font-medium text-fg">
                            {agent.Name}
                          </span>
                          <StatusDot
                            status={presence}
                            live
                            className="shrink-0"
                          />
                        </div>
                        <p
                          className="truncate font-mono text-[11px] text-muted"
                          data-testid={`agent-item-model-${agent.ID}`}
                        >
                          {metaLine}
                        </p>
                      </div>

                      <button
                        type="button"
                        data-testid={`agent-chat-${agent.ID}`}
                        aria-label={`Open chat with ${agent.Name}`}
                        title={`Open chat with ${agent.Name}`}
                        className="opacity-0 group-hover:opacity-100 focus:opacity-100 shrink-0 rounded p-1 text-muted transition-opacity hover:bg-surface hover:text-fg"
                        onClick={(e: ReactMouseEvent<HTMLButtonElement>) => {
                          e.stopPropagation();
                          openChat(agent.ID);
                        }}
                      >
                        <MessageSquare className="size-4" aria-hidden="true" />
                      </button>
                    </div>

                    {generating && (
                      <p
                        className="mt-1 flex items-center gap-1.5 text-[11px] text-muted"
                        data-testid={`agent-item-generating-${agent.ID}`}
                      >
                        <span
                          className="h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-muted/40 border-t-muted"
                          aria-hidden="true"
                        />
                        Writing its prompts…
                      </p>
                    )}

                    {failed && (
                      <div
                        className="mt-1 flex items-center justify-between gap-1 rounded bg-danger/10 p-1.5 text-[11px] text-danger"
                        data-testid={`agent-item-failed-${agent.ID}`}
                      >
                        <div className="min-w-0 flex-1">
                          <p
                            className="truncate text-[11px]"
                            title={agent.PromptsError || undefined}
                          >
                            {agent.PromptsError || "Prompt generation failed"}
                          </p>
                        </div>
                        <button
                          type="button"
                          className="shrink-0 rounded border border-danger/40 bg-surface px-1.5 py-0.5 text-[10px] font-semibold text-danger transition-colors hover:bg-danger hover:text-accenton"
                          data-testid={`agent-retry-${agent.ID}`}
                          aria-label={`Retry prompt generation for ${agent.Name}`}
                          onClick={(e: ReactMouseEvent<HTMLButtonElement>) => {
                            e.stopPropagation();
                            retryGeneration(agent.ID);
                          }}
                        >
                          <span className="flex items-center gap-1">
                            <RefreshCw
                              aria-hidden="true"
                              className="size-2.5"
                            />
                            Retry
                          </span>
                        </button>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </aside>

      {/* Detail Column: shows AgentConfigPane for the selected agent */}
      <section
        className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-surface"
        data-testid="agents-detail-column"
        aria-label="Agent configuration"
      >
        {selectedAgent ? (
          <AgentConfigPane
            key={selectedAgent.ID}
            agent={selectedAgent}
            onSaved={upsert}
            onDeleted={handleDeleted}
          />
        ) : (
          <div className="flex flex-1 items-center justify-center p-8 text-center text-muted">
            <p>Select an agent from the list to view its configuration.</p>
          </div>
        )}
      </section>

      {creating && (
        <CreateAgentModal
          agents={agents}
          onClose={() => setCreating(false)}
          onCreated={upsert}
        />
      )}
    </div>
  );
}
