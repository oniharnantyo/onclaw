import { RefreshCw, TriangleAlert } from "lucide-react";
import type { ReactElement } from "react";

import { AgentAvatar } from "../../../components";
import { Button } from "../../../components/ui/Button";
import { StatusDot } from "../../../components/ui/StatusDot";
import type { StatusDotStatus } from "../../../components/ui/StatusDot";
import { PROMPTS_STATUS } from "../types";
import type { AgentCardProps } from "../types";

/**
 * One agent in the grid (tasks 4.1, 4.4, 4.7): onclaw's card idiom — flat
 * surface on a hairline border with a subtle border shift on hover (no
 * lift/scale) — carrying the avatar, name, and presence dot in the title
 * row, a single human-voiced meta line (the description, else the model
 * name — never the raw agent ID; the hash lives in Configure only), and the
 * prompt-generation state. The action row is chat + configure: the agent's
 * delete affordance moved into the configure dialog's footer, so the card
 * never carries it.
 */
export function AgentCard({
  agent,
  onChat,
  onConfigure,
  onRetry,
}: AgentCardProps): ReactElement {
  const generating = agent.PromptsStatus === PROMPTS_STATUS.GENERATING;
  const failed = agent.PromptsStatus === PROMPTS_STATUS.FAILED;

  // Presence dot rides the prompt lifecycle: pulsing while prompts
  // generate, danger on failure, resting once ready.
  const presence: StatusDotStatus = failed
    ? "error"
    : generating
      ? "running"
      : "idle";

  // Card voice (task 4.4): description first, else the model name from the
  // agent record — an ID never renders on the card.
  const metaLine = agent.Description || agent.Model || "No model selected";

  return (
    <article
      data-testid={`agent-card-${agent.ID}`}
      className="flex flex-col rounded-md border border-line bg-surface p-4 transition-colors hover:border-[color-mix(in_oklab,var(--fg)_24%,transparent)]"
      aria-label={`Agent ${agent.Name}`}
    >
      <div className="flex items-start gap-3">
        <AgentAvatar id={agent.ID} avatar={agent.Avatar} size={44} />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5">
            <h3 className="truncate text-[15px] font-semibold text-fg">
              {agent.Name}
            </h3>
            <StatusDot status={presence} live className="shrink-0" />
          </div>
          <p
            className="mt-0.5 line-clamp-2 text-xs text-muted"
            data-testid={`agent-card-meta-${agent.ID}`}
          >
            {metaLine}
          </p>
        </div>
      </div>

      {generating && (
        <p
          className="mt-3 flex items-center gap-2 text-xs text-muted"
          data-testid={`agent-card-generating-${agent.ID}`}
        >
          <span
            className="h-3.5 w-3.5 shrink-0 animate-spin rounded-full border-2 border-muted/40 border-t-muted"
            aria-hidden="true"
          />
          Writing its prompts…
        </p>
      )}

      {failed && (
        <div
          className="mt-3 flex items-center justify-between gap-2 rounded-md border border-danger/30 bg-danger/10 p-2.5 text-xs text-danger"
          data-testid={`agent-card-failed-${agent.ID}`}
        >
          <div className="min-w-0 flex-1">
            <p className="flex items-center gap-1 font-semibold">
              <TriangleAlert aria-hidden="true" className="size-3.5 shrink-0" />
              Prompt generation failed
            </p>
            <p
              className="mt-0.5 truncate text-[11px]"
              title={agent.PromptsError || undefined}
            >
              {agent.PromptsError || "Prompt generation failed"}
            </p>
          </div>
          <button
            type="button"
            className="shrink-0 rounded-md border border-danger/40 bg-surface px-2 py-1 text-[11px] font-semibold text-danger transition-colors hover:bg-danger hover:text-accenton disabled:opacity-50"
            data-testid={`agent-retry-${agent.ID}`}
            aria-label={`Retry prompt generation for ${agent.Name}`}
            onClick={() => onRetry(agent.ID)}
          >
            <span className="flex items-center gap-1">
              <RefreshCw aria-hidden="true" className="size-3" />
              Retry
            </span>
          </button>
        </div>
      )}

      <div className="mt-auto flex items-center gap-2 pt-3">
        <Button
          size="sm"
          className="flex-1"
          data-testid={`agent-chat-${agent.ID}`}
          onClick={() => onChat(agent.ID)}
        >
          Open chat
        </Button>
        <Button
          variant="ghost"
          size="sm"
          data-testid={`agent-configure-${agent.ID}`}
          onClick={() => onConfigure(agent.ID)}
        >
          Configure
        </Button>
      </div>
    </article>
  );
}
