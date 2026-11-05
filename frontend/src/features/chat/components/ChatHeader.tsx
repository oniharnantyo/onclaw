/**
 * Chat header (task 6.3, spec chat-view "Chat header"): the bar above the
 * transcript — deliberately quiet, just the conversation's title (the active
 * session's, with the sessions rail's "Untitled session" convention; the
 * agent's name only before any session exists; the open group's title, or
 * its name when untitled — add-agent-groups design D9). Turn lifecycle lives
 * elsewhere: the sessions rail carries the per-session running dots and the
 * transcript surfaces approvals, so the header repeats none of it. The
 * group's settings entry lives in the right sidebar's group island (round 6),
 * and the far right hosts the right sidebar's toggle — the sidebar hides
 * entirely when minimized, so the header is its only affordance. Round 8:
 * with a group open the header also carries the member roster as a stacked
 * avatar row (GroupAvatarStack) — at-a-glance membership, with a green ring
 * on members whose task is currently running. Plain flow layout — nothing overlays the transcript, so
 * the header cannot intercept its clicks. At lg+ the content island runs
 * full height, so the header carries the titlebar band as top padding: the
 * window's first 25px is the native invisible titlebar (main.go) whose
 * clicks are consumed as window drags — pt-6 keeps the interactive toggle
 * 7px below it, and the header stays slim above the transcript. All colors
 * ride the ported tokens, so both themes re-skin without a re-render.
 */
import type { ReactElement } from "react";
import { PanelRightClose, PanelRightOpen } from "lucide-react";

import type {
  Agent,
  AgentGroup,
  AgentGroupMember,
  AgentGroupTask,
  Session,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { IconButton } from "../../../components";
import { GroupAvatarStack } from "../../groups/GroupAvatarStack";
import { useChatStore } from "../store";
import type { ChatStore } from "../types";

export function ChatHeader(): ReactElement {
  const agents = useChatStore((s: ChatStore) => s.agents);
  const sessions = useChatStore((s: ChatStore) => s.sessions);
  const groups = useChatStore((s: ChatStore) => s.groups);
  const groupMembers = useChatStore((s: ChatStore) => s.groupMembers);
  const groupTasks = useChatStore((s: ChatStore) => s.groupTasks);
  const activeAgentID = useChatStore((s: ChatStore) => s.activeAgentID);
  const activeSessionID = useChatStore((s: ChatStore) => s.activeSessionID);
  const activeGroupID = useChatStore((s: ChatStore) => s.activeGroupID);
  const sidebarCollapsed = useChatStore((s: ChatStore) => s.sidebarCollapsed);
  const toggleSidebar = useChatStore((s: ChatStore) => s.toggleSidebar);

  const agent = agents.find((a: Agent) => a.ID === activeAgentID) ?? null;

  const session =
    sessions.find((s: Session) => s.ID === activeSessionID) ?? null;

  const group = groups.find((g: AgentGroup) => g.ID === activeGroupID) ?? null;

  // The stack's inputs ride the same live slices the transcript and the
  // right bar's island read: the roster from the member rows, the running
  // set from the in-flight task rows' owners (the store scopes groupTasks
  // to the open group — the GroupActivity convention). A member is
  // "working" only while a dispatch is in flight; queued stays surface.
  const roster = group
    ? groupMembers
        .map(
          (m: AgentGroupMember): Agent | null =>
            agents.find((a: Agent) => a.ID === m.AgentID) ?? null
        )
        .filter((a: Agent | null): a is Agent => a !== null)
    : [];

  const runningIDs = new Set(
    groupTasks.flatMap((t: AgentGroupTask): string[] =>
      t.State === "running" && t.OwnerAgentID !== null ? [t.OwnerAgentID] : []
    )
  );

  // The header leads with the conversation's title — the sessions rail's
  // "Untitled session" convention covers a session that has not been titled
  // yet, the group's Title-or-Name convention covers a group (design D9) —
  // and only falls back to the agent's name before any session exists.
  const title = group
    ? group.Title || group.Name
    : session
      ? session.Title || "Untitled session"
      : (agent?.Name ?? "");

  return (
    <div
      className="flex shrink-0 items-center gap-2 border-b border-line bg-surface px-3 py-1.5 lg:pt-6"
      data-testid="chat-header"
    >
      <h1
        className="min-w-0 truncate font-display text-[13px] font-semibold text-fg"
        data-testid="chat-header-session-title"
      >
        {title}
      </h1>
      {group !== null && roster.length > 0 && (
        <GroupAvatarStack roster={roster} runningIDs={runningIDs} />
      )}
      <div className="ms-auto flex items-center gap-2">
        <IconButton
          label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
          size="sm"
          testId="btn-toggle-sidebar"
          icon={
            sidebarCollapsed ? (
              <PanelRightOpen aria-hidden="true" className="size-4" />
            ) : (
              <PanelRightClose aria-hidden="true" className="size-4" />
            )
          }
          onClick={toggleSidebar}
        />
      </div>
    </div>
  );
}
