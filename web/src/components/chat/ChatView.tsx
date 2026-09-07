import { useState, useRef, useEffect, useSyncExternalStore } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { api } from "../../lib/api";
import { getLiveChatStatus, subscribeLiveChat, retryLiveChat } from "../../lib/livechat";

import { ChatHeader } from "./ChatHeader";
import { Composer } from "./Composer";
import { UserMessage } from "./UserMessage";
import { OtherMessage } from "./OtherMessage";
import { AgentMessage } from "./AgentMessage";
import { ErrorEntry } from "./ErrorEntry";
import { ThinkingRow } from "./ThinkingRow";
import { toolCatalog } from "../../lib/toolCatalog";

function useResolveApproval(tenant: any, agent: any) {
  return async (agentSlug: string, sessionId: string, interruptId: string, approved: boolean) => {
    const slug = agentSlug || agent?.slug;
    if (!slug) return;
    await api.agents.resolveApproval(tenant.id, slug, sessionId, interruptId, approved);
  };
}

// $ invocation menu source: system skills (always), enabled workspace skills,
// and — in a direct chat — this agent's own tier. Disabled skills never list.
function useSkillGroups(tenant: any, agent: any) {
  const [groups, setGroups] = useState<any[]>([]);
  useEffect(() => {
    let mounted = true;
    const ws = tenant?.sub || tenant?.id;
    if (!ws) return;
    (async () => {
      try {
        const res = await api.skills.list(ws);
        if (!mounted) return;
        const skills = res.skills || [];
        const next = [
          {
            label: 'System',
            skills: skills
              .filter((s: any) => s.tier === 'system')
              .map((s: any) => ({ name: s.name, description: s.description })),
          },
          {
            label: 'Workspace',
            skills: skills
              .filter((s: any) => s.tier === 'workspace' && s.enabled !== false)
              .map((s: any) => ({ name: s.name, description: s.description })),
          },
        ];
        const agentSlug = agent?.slug || agent?.id;
        if (agentSlug) {
          try {
            const own = await api.agents.listSkills(ws, agentSlug);
            if (mounted && own?.skills?.length) {
              next.push({
                label: 'This agent',
                skills: own.skills.map((s: any) => ({ name: s.name, description: s.description })),
              });
            }
          } catch {
            // agent skills are optional context — the menu works without them
          }
        }
        if (mounted) setGroups(next.filter((g: any) => g.skills.length > 0));
      } catch {
        if (mounted) setGroups([]);
      }
    })();
    return () => {
      mounted = false;
    };
  }, [tenant?.sub, tenant?.id, agent?.slug, agent?.id]);
  return groups;
}

export function ChatView({ tenant, target, agent, thread, session, channelMembers, onToggleMembers,
  typing, busy, onConfigure,
  onSend, onCancel, onAttach, onCopy, onRefresh, onBranch, onEditSubmit  }: any) {
  const listRef = useRef(null);
  const atBottomRef = useRef(true);
  const [atBottom, setAtBottom] = useState(true);
  const prevLen = useRef(thread.length);
  const prevChat = useRef(target.obj.id);
  const prevSession = useRef(session && session.id);

  // long-history guard: render the latest window, load older on demand
  const [msgLimit, setMsgLimit] = useState(80);
  const sessionId = session && session.id;
  const [limitSession, setLimitSession] = useState(sessionId);
  if (sessionId !== limitSession) { setLimitSession(sessionId); setMsgLimit(80); }
  const visibleMsgs = thread.length > msgLimit ? thread.slice(thread.length - msgLimit) : thread;
  const hiddenMsgs = thread.length - visibleMsgs.length;

  const pinBottom = () => {
    const el = listRef.current;
    if (el && atBottomRef.current) el.scrollTop = el.scrollHeight;
  };

  useEffect(() => {
    const el = listRef.current;
    if (!el) return;
    const sameChat = target.obj.id === prevChat.current;
    const sameSession = (session && session.id) === prevSession.current;
    const lastMsg = thread[thread.length - 1];
    const grewUser = sameChat && sameSession && thread.length > prevLen.current && lastMsg && lastMsg.author === 'you';
    prevLen.current = thread.length;
    prevChat.current = target.obj.id;
    prevSession.current = sessionId;
    if (grewUser) {
      // assistant-ui turnAnchor="top" — a fresh user message pins to the top of the viewport
      const nodes = el.querySelectorAll('[data-role="user"]');
      const node = nodes[nodes.length - 1];
      if (node) el.scrollTop = Math.max(0, node.offsetTop - 16);
      else el.scrollTop = el.scrollHeight;
    } else {
      el.scrollTop = el.scrollHeight;
    }
  }, [thread.length, typing, target.obj.id, sessionId]); // eslint-disable-line react-hooks/exhaustive-deps -- scroll tracks thread.length only; full deps would re-run on every store update and jump the viewport

  const onScroll = (e) => {
    const el = e.currentTarget;
    const b = el.scrollHeight - el.scrollTop - el.clientHeight < 36;
    atBottomRef.current = b;
    setAtBottom(b);
  };
  const scrollToBottom = () => {
    const el = listRef.current;
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
  };

  const isEmpty = thread.length === 0 && !typing;
  const handleResolveApproval = useResolveApproval(tenant, agent);
  const skillGroups = useSkillGroups(tenant, agent);
  // Live chat connect state: when no usable workspace key exists (exchange
  // failed), the chat surfaces an explicit connect/retry banner — never a
  // canned mock reply.
  const liveStatus = useSyncExternalStore(subscribeLiveChat, getLiveChatStatus);
  const workspaceId = tenant?.sub || tenant?.id;
  // Tool display names come from the per-workspace catalog cache (design D7);
  // the bump re-renders the list once names resolve so cards pick them up.
  const [, setCatalogTick] = useState(0);
  useEffect(() => {
    let mounted = true;
    toolCatalog.ensure(workspaceId).then(() => { if (mounted) setCatalogTick((t) => t + 1); });
    return () => { mounted = false; };
  }, [workspaceId]);
  const suggestions = agent ? [
    'Summarize the latest activity for me',
    'What are you working on right now?',
    'Draft a status update for the team',
    'What should I know about ' + tenant.name + ' today?'
  ] : [];

  return (
    <section data-od-id="chat-view" className="flex min-w-0 flex-1 flex-col bg-bg" aria-label={'Conversation with ' + (target.kind === 'channel' ? '#' + target.obj.name : target.obj.name)}>
      <ChatHeader target={target} agent={agent} channelMembers={channelMembers} usage={session?.usage} onToggleMembers={onToggleMembers} onConfigure={onConfigure}/>
      <div ref={listRef} onScroll={onScroll} role="log" aria-label="Messages" className="od-scroll relative flex-1 overflow-y-auto" data-od-id="message-list">
        {isEmpty ? (
          <div className="flex h-full flex-col items-center justify-center gap-2 px-4 text-center">
            {agent && <Avatar name={agent.name} avatar={agent.avatar} kind="agent" size={44}/>}
            <h2 className="text-[22px] font-medium tracking-tight text-fg" data-od-id="welcome-title">
              {agent ? 'This is ' + agent.name : target.kind === 'channel' ? '#' + target.obj.name : target.obj.name}
            </h2>
            <p className="max-w-md text-[13px] leading-5 text-muted">
              {agent ? agent.role + '.'
                : target.kind === 'channel' ? target.obj.purpose + '.'
                : 'No messages yet — say hello.'}
            </p>
          </div>
        ) : (
          <div className="mx-auto flex w-full max-w-[44rem] flex-col gap-6 px-4 pb-6 pt-5">
            {hiddenMsgs > 0 && (
              <div className="flex justify-center px-2" data-od-id="load-earlier">
                <button type="button" onClick={() => setMsgLimit((l) => l + 400)}
                  className="rounded-full border border-line bg-surface px-3 py-1 text-[11px] text-muted transition-colors hover:text-fg">
                  Load earlier messages · {hiddenMsgs} hidden
                </button>
              </div>
            )}
            {hiddenMsgs === 0 && thread.length > 80 && session && (
              <p className="text-center text-[11px] text-muted">Beginning of “{session.title}”</p>
            )}
            {visibleMsgs.map((m: any) => {
              const isLast = m.id === (thread[thread.length - 1] || {}).id;
              const msgAgent = (m.agentId && tenant.agents.find((a: any) => a.id === m.agentId)) || agent;
              if (m.author === 'you') return <UserMessage key={m.id} m={m} onEdit={onEditSubmit} members={channelMembers}/>;
              if (m.author === 'other') return <OtherMessage key={m.id} m={m} members={channelMembers}/>;
              if (m.author === 'error') return <ErrorEntry key={m.id} m={m}/>;
              return (
                <AgentMessage key={m.id} m={m} agent={msgAgent} inChannel={target.kind === 'channel'}
                  busy={busy} isLast={isLast}
                  onCopy={onCopy} onGrow={pinBottom}
                  onRefresh={onRefresh} onBranch={onBranch} members={channelMembers}
                  sessionId={sessionId}
                  onResolveApproval={handleResolveApproval}/>
              );
            })}
            {/* The streaming agent message renders its own loading dots and
                caret — the thinking row is only for a turn with no agent
                message on the transcript yet, else the agent shows twice. */}
            {typing && thread[thread.length - 1]?.author !== 'agent' && <ThinkingRow agent={agent}/>}
          </div>
        )}
      </div>
      <div className="relative shrink-0">
        <button type="button" onClick={scrollToBottom} data-od-id="btn-scroll-bottom"
          aria-label="Scroll to bottom" title="Scroll to bottom"
          className={cx('absolute -top-11 left-1/2 z-10 flex h-9 w-9 -translate-x-1/2 items-center justify-center rounded-full border border-line bg-surface text-fg2 shadow-[var(--elev-raised)] transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] hover:text-fg', (atBottom || isEmpty) && 'invisible')}>
          <Icon name="down" size={15}/>
        </button>
        {agent && liveStatus.state === 'disconnected' && (
          <div data-od-id="live-chat-connect" role="status" className="mx-auto w-full max-w-[44rem] px-4 pb-2">
            <div className="flex items-center justify-between gap-3 rounded-md border border-line bg-surface px-3 py-2">
              <p className="min-w-0 flex-1 text-[12px] leading-5 text-muted">
                {liveStatus.message || 'Live chat is not connected to this workspace yet.'}
              </p>
              <button type="button" onClick={() => workspaceId && void retryLiveChat(workspaceId)}
                data-od-id="btn-live-chat-retry"
                className="shrink-0 rounded-md border border-line px-2.5 py-1 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg">
                Retry
              </button>
            </div>
          </div>
        )}
        <div className="mx-auto w-full max-w-[44rem] px-4 pb-4">
          <Composer agent={agent} running={busy} onSend={onSend} onCancel={onCancel} onAttach={onAttach}
            mentionOptions={target.kind === 'channel' ? channelMembers : null}
            skillGroups={skillGroups}/>
          {isEmpty && agent && suggestions.length > 0 && (
            <div className="mt-3 flex flex-wrap items-center justify-center gap-2 px-1" data-od-id="welcome-suggestions">
              {suggestions.map((s: any) => (
                <button key={s} type="button" onClick={() => onSend(s)}
                  className="rounded-full border border-line bg-surface px-3.5 py-1.5 text-[13px] font-normal text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] hover:text-fg">
                  {s}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
    </section>
  );
}

