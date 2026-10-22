/**
 * Chat menu (tasks 9.1–9.5): transcript, queued-message stack, composer, the
 * collapsible right sidebar, the floating run card, and the right preview
 * panel. Layout is pure composition — all state
 * (sessions, entries, turn lifecycle, queue, usage) lives in the feature
 * store, so unmounting any part (e.g. collapsing the sidebar) can never
 * interrupt an in-flight turn. The session layer left the chat surface
 * (redesign-shell-layout) and now mounts INSIDE the content island as its
 * left column (the shell composes rail + this view); the right sidebar hides
 * entirely when minimized — its toggle lives in the chat header.
 *
 * Below md the Sessions island is hidden and a compact header (agent +
 * session select + new) takes over its job, so no fixed rail can push the
 * transcript off-screen (design.md responsive grammar).
 */
import { useEffect } from "react";
import type { ChangeEvent, ReactElement } from "react";
import { Plus } from "lucide-react";

import type {
  Agent,
  Session,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { EmptyState, IconButton } from "../../components";
import { client } from "../../lib/client";
import type { SchedulesChangedEvent } from "../../lib/events";
import { useNavStore } from "../../stores/nav";
import type { ChatStore } from "./types";
import { useChatStore } from "./store";
import { ChatRuntimeProvider } from "./runtime/ChatRuntimeProvider";
import { ChatHeader } from "./components/ChatHeader";
import { RightPanel } from "./components/RightPanel";
import { RightSidebar } from "./components/RightSidebar";
import { ChatComposer } from "./composer/ChatComposer";
import { ChatThread } from "./thread/ChatThread";

/** Mobile stand-in for the session rail (below md). */
function MobileSessionBar(): ReactElement {
  const agents = useChatStore((s: ChatStore) => s.agents);
  const sessions = useChatStore((s: ChatStore) => s.sessions);
  const activeAgentID = useChatStore((s: ChatStore) => s.activeAgentID);
  const activeSessionID = useChatStore((s: ChatStore) => s.activeSessionID);
  const selectSession = useChatStore((s: ChatStore) => s.selectSession);
  const newSession = useChatStore((s: ChatStore) => s.newSession);

  return (
    <div
      className="glass-panel mx-3 mt-3 flex items-center gap-2 rounded-lg border border-line bg-surface px-3 py-2 md:hidden"
      data-testid="chat-mobile-bar"
    >
      {agents.find((a: Agent) => a.ID === activeAgentID)?.Name && (
        <span className="font-display text-sm font-semibold">
          {agents.find((a: Agent) => a.ID === activeAgentID)?.Name}
        </span>
      )}
      <select
        className="h-8 min-w-0 flex-1 rounded-md border border-line bg-surface px-2 text-[13px] text-fg2 focus:border-accent"
        aria-label="Session"
        value={activeSessionID ?? ""}
        onChange={(e: ChangeEvent<HTMLSelectElement>) =>
          void selectSession(e.target.value)
        }
      >
        {sessions.length === 0 && <option value="">No sessions yet</option>}
        {sessions.map((session: Session) => (
          <option key={session.ID} value={session.ID}>
            {session.Title || "Untitled session"}
          </option>
        ))}
      </select>
      <IconButton
        label="New session"
        size="sm"
        icon={<Plus aria-hidden="true" className="size-4" />}
        disabled={!activeAgentID}
        onClick={() => void newSession()}
      />
    </div>
  );
}

export default function ChatView(): ReactElement {
  const init = useChatStore((s: ChatStore) => s.init);
  const agents = useChatStore((s: ChatStore) => s.agents);

  const chatTarget = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.chatTarget
  );

  // Mount consumes a target set from another menu; a target set while the
  // view is already open (the composer's agent picker, task 2.3) re-runs
  // init, which consumes it and switches the conversation context. With no
  // target init() is idempotent, so the extra run is a no-op.
  useEffect(() => {
    void init();
  }, [init, chatTarget]);

  // A background schedule fire creates a session for its agent (add-scheduler
  // design D10); the event refetches the active agent's list through the
  // store so the session appears in the rail without a reload. The
  // subscription rides the chat feature's root component — the store itself
  // is module-level, and the view is the feature's mounting surface.
  useEffect(
    (): (() => void) =>
      client.events.onSchedulesChanged((event: SchedulesChangedEvent): void => {
        void useChatStore.getState().refreshSessions(event.agentId);
      }),
    []
  );

  if (agents.length === 0) {
    return (
      <div
        className="flex h-full min-w-0 flex-1 items-center justify-center p-6"
        data-testid="chat-view"
      >
        <EmptyState
          title="No agents yet"
          description="Create an agent in the Agents menu to start chatting."
        />
      </div>
    );
  }

  return (
    <div
      className="relative flex h-full min-w-0 flex-1"
      data-testid="chat-view"
    >
      <ChatRuntimeProvider>
        <div className="relative flex min-w-0 flex-1 flex-col bg-bg">
          <MobileSessionBar />
          <ChatHeader />
          <ChatThread />
          <ChatComposer />
        </div>
        <RightSidebar />
        <RightPanel />
      </ChatRuntimeProvider>
    </div>
  );
}
