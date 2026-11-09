/**
 * ThreadEmptyState (enhance-chat-empty-state): the fresh transcript's
 * identity greeting — the agent's procedural avatar, the greeting line, and
 * an identity line carrying the agent's name plus the description's first
 * non-empty line, stacked and centered. The vertical centering inside the
 * viewport is ChatThread's job (the log's min-h-full idiom); this block only
 * stacks. An agent that cannot be resolved — a group transcript, or an
 * active id with no row — degrades to the greeting alone: no avatar, no
 * identity line (a face for nobody would be a lie). The avatar itself falls
 * back to the id-derived face when the stored config is absent
 * (AgentAvatar's contract), so a pre-picker agent keeps its established
 * identity. Colors ride the ported tokens, so both themes re-skin without a
 * re-render; the entrance stagger follows the vendored-element idiom, with
 * every leg carrying motion-reduce:animate-none so the reduced-motion reader
 * gets the block without the slide.
 */
import type { ReactElement } from "react";

import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { AgentAvatar } from "../../../components";

/** The description's first non-empty line, trimmed — the identity line's
 * optional tail. An empty or whitespace-only description yields nothing,
 * and the identity line then stops at the agent's name. */
function firstDescriptionLine(description: string): string {
  for (const line of description.split("\n")) {
    const trimmed = line.trim();

    if (trimmed.length > 0) return trimmed;
  }

  return "";
}

/**
 * The empty transcript's centered identity greeting (see the file header).
 */
export function ThreadEmptyState({
  agent,
}: {
  agent: Agent | null;
}): ReactElement {
  const line = agent === null ? "" : firstDescriptionLine(agent.Description);

  return (
    <div
      className="flex flex-col items-center gap-3 py-16 text-center"
      data-testid="thread-empty-state"
    >
      {agent !== null && (
        <div
          className="animate-in fade-in slide-in-from-bottom-1/2 fill-mode-both motion-reduce:animate-none"
          data-testid="thread-empty-state-avatar"
        >
          <AgentAvatar id={agent.ID} avatar={agent.Avatar} size={56} />
        </div>
      )}
      <p
        className="animate-in fade-in slide-in-from-bottom-1/2 fill-mode-both [animation-delay:150ms] motion-reduce:animate-none text-lg font-medium text-fg"
        data-testid="thread-empty-state-greeting"
      >
        How can I help you today?
      </p>
      {agent !== null && (
        <p
          className="animate-in fade-in slide-in-from-bottom-1/2 fill-mode-both [animation-delay:300ms] motion-reduce:animate-none max-w-full truncate text-sm text-muted"
          data-testid="thread-empty-state-identity"
        >
          {`With ${agent.Name}${line === "" ? "" : ` — ${line}`}`}
        </p>
      )}
    </div>
  );
}
