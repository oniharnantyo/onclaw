/**
 * Group activity line (add-agent-groups design D9): the transcript-tail
 * "is my chat working" indicator for the group surface. A group send's
 * member turns run in the members' own sessions — between the send and the
 * copy-back the shared transcript is silent by design (routing writes no
 * coordinator line for mention and lead routes), so the only honest signal
 * is the routed work itself: the live task rows. While the open group has a
 * task queued or running, one quiet status line names the owners — "Scout is
 * working…" for the dispatch in flight, "Waiting for Jono…" for the
 * 2-active budget's queue — and disappears when the work reaches a resting
 * state. Mounted once after the message list (beside the session surface's
 * TurnActivity, which never fires for groups). Static under reduced motion:
 * the spinner drops, the text stays.
 */
import type { ReactElement } from "react";

import type { Agent } from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { useReducedMotion } from "../chat/tools/useReducedMotion";
import { useChatStore } from "../chat/store";
import type { ChatStore } from "../chat/types";

/** One clause of the activity line: who the work waits on, and in which
 * state. Running names the dispatch in flight; queued names the budget's
 * backlog. */
function clausesOf(
  tasks: { State: string; OwnerAgentID: string | null }[],
  agentName: (agentID: string) => string
): string[] {
  const working: string[] = [];
  const waiting: string[] = [];

  for (const task of tasks) {
    if (task.OwnerAgentID === null) continue;

    if (task.State === "running") working.push(agentName(task.OwnerAgentID));
    else if (task.State === "queued")
      waiting.push(agentName(task.OwnerAgentID));
  }

  const clauses: string[] = [];

  if (working.length > 0)
    clauses.push(
      `${working.join(", ")} ${working.length === 1 ? "is" : "are"} working…`
    );

  if (waiting.length > 0) clauses.push(`Waiting for ${waiting.join(", ")}…`);

  return clauses;
}

/** The group transcript's tail activity line. Renders nothing on the session
 * surface or while no routed work is in flight. */
export function GroupActivity(): ReactElement | null {
  const activeGroupID = useChatStore((s: ChatStore) => s.activeGroupID);
  const groupTasks = useChatStore((s: ChatStore) => s.groupTasks);
  const agents = useChatStore((s: ChatStore) => s.agents);

  const reduced = useReducedMotion();

  if (activeGroupID === null) return null;

  const inFlight = groupTasks.filter(
    (t: { State: string }) => t.State === "running" || t.State === "queued"
  );

  if (inFlight.length === 0) return null;

  const agentName = (agentID: string): string =>
    agents.find((a: Agent) => a.ID === agentID)?.Name ?? "a member";

  const clauses = clausesOf(inFlight, agentName);

  if (clauses.length === 0) return null;

  return (
    <div
      className="flex items-center gap-1.5 py-1 text-xs text-muted"
      data-testid="group-activity-line"
      role="status"
    >
      {!reduced && (
        <span
          aria-hidden="true"
          className="h-3 w-3 animate-spin rounded-full border-2 border-muted/40 border-t-muted motion-reduce:animate-none"
        />
      )}
      <span>{clauses.join(" · ")}</span>
    </div>
  );
}
