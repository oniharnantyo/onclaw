/**
 * Group attribution (add-agent-groups design D7, chat-view spec "Turn
 * attribution"): the per-entry attribution the transcript's assistant turns
 * render in a GROUP conversation, in place of the session agent's stub.
 * Member rows open with that member's avatar and name; coordinator rows
 * render the system-row label (no agent attribution); an unresolvable
 * author renders nothing. The avatar is deliberately STATIC — the shared
 * AgentAvatar without live props, the same static rendering path the
 * session attribution stub uses — so no group entry ever mounts a second
 * live animated avatar instance under any motion setting; the right bar's
 * identity face stays the transcript's only live one. Task state never
 * renders beneath transcript messages (align-group-task-lifecycle design
 * D6, agent-groups spec "State stays off messages"): live work surfaces on
 * the activity line, stopped root runs on the tail notice, so the
 * attribution is author-only.
 */
import type { ReactElement } from "react";

import type { Agent } from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { AgentAvatar } from "../../components";
import { useChatStore } from "../chat/store";
import type { ExtendedChatStore } from "../chat/store";
import type { EntryAuthor } from "../chat/types";

export function GroupAttribution({
  author,
}: {
  author: EntryAuthor | null;
}): ReactElement | null {
  const agents = useChatStore((s: ExtendedChatStore) => s.agents);

  // An unresolvable author (a member agent deleted since, an unmapped
  // message id) renders no attribution stub.
  if (author === null) return null;

  // SAFETY: an author of kind member always carries the agent id it
  // resolved from; the mapping never mints one without.
  const agentID = author.agentID ?? "";

  const agent = agents.find((a: Agent): boolean => a.ID === agentID) ?? null;

  return (
    <div
      className="mb-1.5 flex flex-col gap-0.5"
      data-testid="group-attribution"
      data-author-kind={author.kind}
    >
      {author.kind === "coordinator" ? (
        // Coordinator rows (assignment, change, and error lines) are system
        // rows: the label carries the voice, the row's text names the
        // assignee or the failure.
        <div className="flex items-center gap-1.5">
          <span className="font-mono text-[11px] font-medium tracking-wide text-muted uppercase">
            Coordinator
          </span>
        </div>
      ) : (
        <div className="flex items-center gap-1.5">
          {/* Static face: no live props — the right bar keeps the
              transcript's only live avatar (add-agent-groups design D7). */}
          <AgentAvatar id={agentID} avatar={agent?.Avatar ?? null} size={16} />
          <span className="text-xs font-medium text-muted">
            {agent?.Name || agentID}
          </span>
        </div>
      )}
    </div>
  );
}
