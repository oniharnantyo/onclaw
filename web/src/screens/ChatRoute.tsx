import { useEffect, useRef } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { AssistantRuntimeProvider } from '@assistant-ui/react';
import { useStore, useWorkspace, useThread } from '../store';
import { useChatRuntime } from '../chat/runtime';
import type { AttachmentChip } from '../lib/attachments';
import { ErrorState } from '../components/ErrorState';
import { ChatView } from '../components/chat/ChatView';
import { RightPanel } from '../components/chat/RightPanel';
import {
  ensureWorkspaceKey,
  hydrateSession,
  fetchSessionTranscript,
  applyServerTranscript,
  attachCatchUpStream,
  isBoundSessionId,
} from '../lib/livechat';
import notFoundSvg from '../assets/not-found.svg';

function ChatRouteActive({
  cleanId,
  agent,
  channel,
  person,
  tenant,
}: {
  cleanId: string;
  agent: any;
  channel: any;
  person: any;
  tenant: any;
}) {
  const navigate = useNavigate();
  const location = useLocation();
  const pos = useStore((s: any) => s.pos);
  const ui = useStore((s: any) => s.ui);
  const toast = useStore((s: any) => s.toast);
  const chatRuntime = useChatRuntime(cleanId);

  // Run transcript handoff (integrate-scheduler 7.4): the runs screen
  // navigates here with the run's session address. One store write injects
  // (or reuses) the session entry in this agent's thread and activates it;
  // the state is consumed immediately so a reload doesn't re-inject.
  const openRun = (location.state as any)?.openRun as
    | { sessionId: string; schedulerName?: string; title?: string; langfuseUrl?: string }
    | undefined;
  useEffect(() => {
    if (!openRun?.sessionId || !cleanId) return;
    const title = openRun.schedulerName ? 'Run · ' + openRun.schedulerName : 'Scheduled run';
    useStore.getState().openRunSession(cleanId, openRun.sessionId, title, openRun.schedulerName, openRun.langfuseUrl);
    navigate(location.pathname, { replace: true, state: null });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openRun?.sessionId, cleanId]);

  const addChannelMember = (id: string) => useStore.getState().addChannelMember(cleanId, id);
  const removeChannelMember = (id: string) => useStore.getState().removeChannelMember(cleanId, id);
  const openMember = (id: string) => navigate(`/c/${id}`);
  // Channels only (add-right-panel 1.5): the header's avatar stack opens (or
  // focuses — dedup) the panel's members tab. Agent chats never offer one.
  const openMembersTab = () =>
    useStore.getState().openPanelTab({ kind: 'members', title: 'Members', payload: { chatId: cleanId } });

  const chatAgent = agent || (channel ? (tenant?.agents || []).find((a: any) => a.id === channel.agentId) || null : null);
  const threadState = useThread(cleanId);
  const target = agent
    ? ({ kind: 'agent' as const, obj: agent })
    : channel
    ? ({ kind: 'channel' as const, obj: channel })
    : ({ kind: 'person' as const, obj: person });

  const session = threadState.list.find((x: any) => x.id === threadState.active) || null;
  const thread = session ? session.messages : [];

  const workspaceId = tenant?.sub || tenant?.id;

  // Workspace entry: provision the per-workspace chat key (JWT → key
  // exchange) when the slot is absent; the connect state covers failure.
  useEffect(() => {
    if (!workspaceId) return;
    void ensureWorkspaceKey(workspaceId);
  }, [workspaceId]);

  // Transcript hydration: opening a chat whose session is server-bound
  // (sess_<uuid>) replaces the local thread with the authoritative server
  // transcript — two browsers converge on the same history. Legacy counter
  // sessions hydrate nothing. On success the catch-up stream attaches (D4):
  // in-flight re-attachment — the hydrated cursor resumes the server's event
  // feed so a reload mid-turn streams the remainder of the run to completion.
  const catchUpRef = useRef<{ key: string; abort: AbortController } | null>(null);
  const boundSessionId = session && isBoundSessionId(session.id) ? session.id : null;
  useEffect(() => {
    if (!boundSessionId || !chatAgent) return;
    const slug = chatAgent.slug || chatAgent.id;
    const key = `${workspaceId}:${cleanId}:${boundSessionId}`;
    // One live attach per session key: a re-run while the stream is open
    // never opens a duplicate. The ref is cleared on cleanup, so StrictMode's
    // setup→cleanup→setup cycle (and chat A→B→A switches) re-attach instead
    // of inheriting a guard whose stream was just aborted — the aborted first
    // attempt otherwise killed the attach for good (reload mid-run showed a
    // frozen transcript and never streamed the rest of the turn).
    if (catchUpRef.current?.key === key) return;
    const abort = new AbortController();
    catchUpRef.current = { key, abort };
    void hydrateSession({ workspaceId, agentSlug: slug, chatId: cleanId, sessionId: boundSessionId, signal: abort.signal,
      // A run session opened from the runs screen knows its schedule's name —
      // stamp messages that carry no origin tag of their own (7.4).
      originTag: (session as any)?.schedulerName,
    }).then((hydrated) => {
      if (abort.signal.aborted) return;
      // null means the fetch failed — the store is untouched so a transient
      // network miss never wipes a still-valid meter value.
      if (hydrated) {
        // Meter + popover detail rows (assistant-ui context-display adoption):
        // the hydrated transcript's last turn_usage restores finalInput and,
        // when the wire reported them, the turn input/output + server
        // breakdown. finalInputTokens ?? null keeps the existing contract —
        // a transcript without usage clears rather than shows a stale value.
        useStore.getState().recordThreadUsage(workspaceId, cleanId, hydrated.finalInputTokens ?? null, {
          input: hydrated.turnInputTokens,
          output: hydrated.turnOutputTokens,
          ...(hydrated.contextBreakdown ? { contextBreakdown: hydrated.contextBreakdown } : {}),
        });
        // Attach unconditionally rather than sniffing the transcript for an
        // unfinished turn: history only shows committed events, so a run
        // between events (or mid-tool-call) is invisible to heuristics. The
        // server answers the probe with [DONE] immediately when no run is
        // active (design D2 Phase 3), and a live turn this page started keeps
        // its own stream — skip only when the composer is already running.
        if (!useStore.getState().ui.running) {
          attachCatchUpStream({
            workspaceId,
            agentSlug: slug,
            chatId: cleanId,
            sessionId: boundSessionId,
            after: hydrated.lastEventId,
            signal: abort.signal,
          });
        }
      }
    });
    // Chat switch / unmount aborts the catch-up stream (clean teardown).
    return () => {
      abort.abort();
      if (catchUpRef.current?.key === key) catchUpRef.current = null;
    };
  }, [workspaceId, cleanId, boundSessionId, chatAgent?.slug, chatAgent?.id]);

  // Approval pickup (D6): while a pending approval card exists for the bound
  // session, poll the session-events endpoint (~2s) and replace the card with
  // the resumed turn's output once the server shows the approval resolved.
  const pendingApprovalInterrupt = Boolean(
    boundSessionId &&
      (thread || []).some(
        (m: any) =>
          m.author === 'agent' &&
          (m.tools || []).some(
            (t: any) =>
              t.approval &&
              !t.approval.resolved &&
              (t.approval.sessionId || t.approval.session_id || boundSessionId) === boundSessionId
          )
      )
  );
  useEffect(() => {
    if (!boundSessionId || !chatAgent || !pendingApprovalInterrupt) return;
    const slug = chatAgent.slug || chatAgent.id;
    const timer = setInterval(async () => {
      try {
        const hydrated = await fetchSessionTranscript(workspaceId, slug, boundSessionId);
        // Transcript resolved — refresh the meter + popover detail rows from
        // the latest turn usage (same contract as the hydration effect).
        useStore.getState().recordThreadUsage(workspaceId, cleanId, hydrated.finalInputTokens ?? null, {
          input: hydrated.turnInputTokens,
          output: hydrated.turnOutputTokens,
          ...(hydrated.contextBreakdown ? { contextBreakdown: hydrated.contextBreakdown } : {}),
        });
        if (hydrated.messages.length > 0 && hydrated.pendingInterruptIds.length === 0) {
          applyServerTranscript(workspaceId, cleanId, boundSessionId, hydrated.messages);
        }
      } catch {
        // transient poll failure — the next tick retries
      }
    }, 2000);
    return () => clearInterval(timer);
  }, [workspaceId, cleanId, boundSessionId, pendingApprovalInterrupt, chatAgent?.slug, chatAgent?.id]);

  const channelMembers = channel
    ? (() => {
        const ids =
          channel.members && channel.members.length
            ? channel.members
            : channel.agentId
            ? [channel.agentId]
            : [];
        const out: any[] = [];
        ids.forEach((id: string) => {
          if (out.some((m: any) => m.id === id)) return;
          const a = (tenant?.agents || []).find((x: any) => x.id === id);
          if (a) {
            out.push({ id, kind: 'agent', name: a.name, avatar: a.avatar, agent: a });
            return;
          }
          const p = (tenant?.people || []).find((x: any) => x.id === id);
          if (p) out.push({ id, kind: 'person', name: p.name, presence: p.presence });
        });
        return out;
      })()
    : null;

  const memberCandidates = channel
    ? (() => {
        const inCh = new Set([...(channel.members || []), channel.agentId].filter(Boolean));
        const out: any[] = [];
        (tenant?.agents || []).forEach((a: any) => {
          if (!inCh.has(a.id)) out.push({ id: a.id, kind: 'agent', name: a.name, avatar: a.avatar, agent: a });
        });
        (tenant?.people || []).forEach((p: any) => {
          if (!inCh.has(p.id)) out.push({ id: p.id, kind: 'person', name: p.name, presence: p.presence });
        });
        return out;
      })()
    : [];

  const copyText = (text: string) => {
    if (navigator.clipboard)
      navigator.clipboard
        .writeText(text)
        .then(
          () => toast('Copied to clipboard'),
          () => toast('Clipboard blocked by the browser', 'danger')
        );
    else toast('Clipboard unavailable in this frame', 'danger');
  };

  return (
    <>
      <AssistantRuntimeProvider runtime={chatRuntime.runtime}>
        {/* key: the whole view (Composer tray included) remounts per
            conversation, so switching chats empties the attachment tray and
            the Composer's cleanup aborts its in-flight uploads. */}
        <ChatView
          key={cleanId}
          tenant={tenant}
          target={target}
          agent={chatAgent}
          thread={thread}
          session={session}
          channelMembers={channelMembers}
          onOpenMembers={openMembersTab}
          typing={ui.running}
          busy={ui.running}
          compacting={ui.compacting}
          allowAttachments={target.kind === 'agent'}
          onSend={(text: string, chips?: AttachmentChip[]) =>
            chatRuntime.onNew(
              {
                role: 'user',
                content: [{ type: 'text', text }],
              } as unknown as import('@assistant-ui/react').AppendMessage,
              chips
            )
          }
          onCancel={chatRuntime.onCancel}
          onCopy={copyText}
          onRefresh={chatRuntime.onReload}
          onBranch={chatRuntime.branchNav}
          onEditSubmit={(mid: string, text: string) =>
            chatRuntime.onEdit({
              sourceId: mid,
              role: 'user',
              content: [{ type: 'text', text }],
            } as unknown as import('@assistant-ui/react').AppendMessage)
          }
        />
      </AssistantRuntimeProvider>
      {/* Right panel (add-right-panel 1.2/1.5): docked ≥xl, overlay sheet
          <xl — the chrome ContextPanel established, now hosting registered
          sources. Members data/callbacks ride along as the source context;
          the panel itself reads its tabs from the store's panel slice. */}
      <RightPanel
        sourceContext={{
          channelMembers,
          memberCandidates,
          primaryAgentId: channel?.agentId,
          onAddMember: addChannelMember,
          onRemoveMember: removeChannelMember,
          onOpenMember: openMember,
        }}
      />
    </>
  );
}

export function ChatRoute() {
  const { chatId } = useParams<{ chatId?: string }>();
  const navigate = useNavigate();
  const tenant = useWorkspace();
  const pos = useStore((s: any) => s.pos);
  const agentsLoaded = useStore((s: any) => Boolean(s.agentsLoaded?.[s.pos.tenantId]));

  const cleanId = (chatId || '').trim();
  const isWellFormed = Boolean(cleanId && /^[a-zA-Z0-9_-]+$/.test(cleanId));

  const agent = (tenant?.agents || []).find((a: any) => a.id === cleanId) || null;
  const channel = (tenant?.channels || []).find((c) => c.id === cleanId) || null;
  const person = (tenant?.people || []).find((p) => p.id === cleanId) || null;

  const valid = Boolean(agent || channel || person);

  useEffect(() => {
    if (cleanId && valid && cleanId !== pos.chatId) {
      useStore.getState().selectChat(cleanId);
    } else if (!cleanId && pos.chatId) {
      // /c is the empty picker — drop the persisted chat highlight so the
      // sidebar never shows an active conversation the URL doesn't have.
      // (The chatId change resets the panel slice via goPos.)
      useStore.getState().goPos({ chatId: '' });
    }
  }, [cleanId, valid, pos.chatId]);

  // /c with no id: the chat page with nothing open — pick a conversation from
  // the sidebar. Never auto-open the first agent.
  if (!cleanId) {
    return (
      <div
        className="flex h-full w-full flex-col items-center justify-center gap-2 px-4 text-center"
        data-testid="chat-empty"
      >
        <h2 className="text-[22px] font-medium tracking-tight text-fg">Select a conversation</h2>
        <p className="max-w-md text-[13px] leading-5 text-muted">
          Pick an agent, channel, or teammate from the sidebar to start chatting.
        </p>
      </div>
    );
  }

  const notFound = (
    <div className="flex h-full w-full items-center justify-center p-6 bg-surface">
      <ErrorState
        variant="full"
        illustration={notFoundSvg}
        title="Chat not found"
        description="The requested agent, channel, or person does not exist in this workspace."
        status={404}
        primaryAction={{
          label: 'Back to chats',
          onClick: () => navigate('/c'),
        }}
        secondaryAction={{
          label: 'View agents',
          onClick: () => navigate('/agents'),
        }}
      />
    </div>
  );

  // Malformed ids can never resolve — straight to not-found.
  if (!isWellFormed) return notFound;

  // Well-formed but unknown: the agent list may still be loading (deep link on
  // a fresh boot) — hold on a spinner before declaring it missing.
  if (!valid) {
    if (!agentsLoaded) {
      return (
        <div className="flex h-full w-full items-center justify-center" data-testid="chat-loading">
          <div className="h-7 w-7 animate-spin rounded-full border-2 border-line border-t-accent" />
        </div>
      );
    }
    return notFound;
  }

  return (
    <ChatRouteActive
      cleanId={cleanId}
      agent={agent}
      channel={channel}
      person={person}
      tenant={tenant}
    />
  );
}

export default ChatRoute;
