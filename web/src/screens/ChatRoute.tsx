import { useEffect, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { AssistantRuntimeProvider } from '@assistant-ui/react';
import { useStore, useWorkspace, useThread } from '../store';
import { useChatRuntime } from '../chat/runtime';
import { ErrorState } from '../components/ErrorState';
import { ChatView } from '../components/chat/ChatView';
import { ContextPanel } from '../components/chat/ContextPanel';
import {
  ensureWorkspaceKey,
  hydrateSession,
  fetchSessionTranscript,
  applyServerTranscript,
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
  const pos = useStore((s: any) => s.pos);
  const goPos = useStore((s: any) => s.goPos);
  const ui = useStore((s: any) => s.ui);
  const toast = useStore((s: any) => s.toast);
  const chatRuntime = useChatRuntime(cleanId);

  const addChannelMember = (id: string) => useStore.getState().addChannelMember(cleanId, id);
  const removeChannelMember = (id: string) => useStore.getState().removeChannelMember(cleanId, id);
  const openMember = (id: string) => navigate(`/c/${id}`);

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
  // sessions hydrate nothing.
  const hydratedRef = useRef<string>('');
  const boundSessionId = session && isBoundSessionId(session.id) ? session.id : null;
  useEffect(() => {
    if (!boundSessionId || !chatAgent) return;
    const slug = chatAgent.slug || chatAgent.id;
    const key = `${workspaceId}:${cleanId}:${boundSessionId}`;
    if (hydratedRef.current === key) return;
    hydratedRef.current = key;
    void hydrateSession({ workspaceId, agentSlug: slug, chatId: cleanId, sessionId: boundSessionId }).then((hydrated) => {
      // null means the fetch failed — the store is untouched so a transient
      // network miss never wipes a still-valid meter value.
      if (hydrated) {
        useStore.getState().recordThreadUsage(workspaceId, cleanId, hydrated.finalInputTokens ?? null);
      }
    });
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
        // Transcript resolved — refresh the meter from the latest turn usage.
        useStore.getState().recordThreadUsage(workspaceId, cleanId, hydrated.finalInputTokens ?? null);
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
        <ChatView
          tenant={tenant}
          target={target}
          agent={chatAgent}
          thread={thread}
          session={session}
          channelMembers={channelMembers}
          onToggleMembers={() => goPos({ showContext: !pos.showContext })}
          typing={ui.running}
          busy={ui.running}
          onSend={(text: string) =>
            chatRuntime.onNew({
              role: 'user',
              content: [{ type: 'text', text }],
            } as unknown as import('@assistant-ui/react').AppendMessage)
          }
          onCancel={chatRuntime.onCancel}
          onConfigure={() => useStore.getState().patchUi({ configAgent: chatAgent?.id })}
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
          onAttach={() =>
            useStore
              .getState()
              .toast('Attachments arrive with the storage integration — connect it in Settings → Integrations')
          }
        />
      </AssistantRuntimeProvider>
      {pos.showContext && target.kind === 'channel' && (
        <>
          <div className="hidden xl:flex">
            <ContextPanel
              channelMembers={channelMembers}
              candidates={memberCandidates}
              primaryAgentId={channel?.agentId}
              onAddMember={addChannelMember}
              onRemoveMember={removeChannelMember}
              onClose={() => goPos({ showContext: false })}
              onOpenMember={openMember}
            />
          </div>
          <div className="fixed inset-0 z-40 flex xl:hidden" aria-modal="true" role="dialog">
            <div
              className="od-fade absolute inset-0 bg-[color-mix(in_oklab,var(--fg)_32%,transparent)]"
              onClick={() => goPos({ showContext: false })}
            />
            <div className="od-pop relative flex w-[300px] max-w-[85vw] ml-auto h-full flex-col bg-bg shadow-[var(--elev-raised)]">
              <ContextPanel
                channelMembers={channelMembers}
                candidates={memberCandidates}
                primaryAgentId={channel?.agentId}
                onAddMember={addChannelMember}
                onRemoveMember={removeChannelMember}
                onClose={() => goPos({ showContext: false })}
                onOpenMember={openMember}
              />
            </div>
          </div>
        </>
      )}
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
