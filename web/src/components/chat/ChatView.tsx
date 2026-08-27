// @ts-nocheck
import { useState, useRef, useEffect } from "react";
import { cx, memberHandle } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { StatusDot } from "../ui/StatusDot";
import { MentionText } from "../ui/MentionText";
import { Chip } from "../ui/Chip";
import { STATUS, COMMANDS } from "../../data/seed";

import { ChatHeader } from "./ChatHeader";
import { Composer } from "./Composer";
import { UserMessage } from "./UserMessage";
import { OtherMessage } from "./OtherMessage";
import { AgentMessage } from "./AgentMessage";
import { ThinkingRow } from "./ThinkingRow";

export function ChatView({ tenant, target, agent, thread, session, channelMembers, onToggleMembers,
  typing, streamingId, busy, onConfigure,
  onSend, onCancel, onDoneStream, onAttach, onCopy, onRefresh, onBranch, onEditSubmit }) {
  const listRef = useRef(null);
  const atBottomRef = useRef(true);
  const [atBottom, setAtBottom] = useState(true);
  const prevLen = useRef(thread.length);
  const prevChat = useRef(target.obj.id);
  const prevSession = useRef(session && session.id);

  // long-history guard: render the latest window, load older on demand
  const [msgLimit, setMsgLimit] = useState(80);
  useEffect(() => { setMsgLimit(80); }, [session && session.id]);
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
    prevSession.current = session && session.id;
    if (grewUser) {
      // assistant-ui turnAnchor="top" — a fresh user message pins to the top of the viewport
      const nodes = el.querySelectorAll('[data-role="user"]');
      const node = nodes[nodes.length - 1];
      if (node) el.scrollTop = Math.max(0, node.offsetTop - 16);
      else el.scrollTop = el.scrollHeight;
    } else {
      el.scrollTop = el.scrollHeight;
    }
  }, [thread.length, typing, streamingId, target.obj.id, session && session.id]);

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
  const suggestions = agent ? [
    'Summarize the latest activity for me',
    'What are you working on right now?',
    'Draft a status update for the team',
    'What should I know about ' + tenant.name + ' today?'
  ] : [];

  return (
    <section data-od-id="chat-view" className="flex min-w-0 flex-1 flex-col bg-bg" aria-label={'Conversation with ' + (target.kind === 'channel' ? '#' + target.obj.name : target.obj.name)}>
      <ChatHeader target={target} agent={agent} channelMembers={channelMembers} onToggleMembers={onToggleMembers} onConfigure={onConfigure}/>
      <div ref={listRef} onScroll={onScroll} className="od-scroll relative flex-1 overflow-y-auto" data-od-id="message-list">
        {isEmpty ? (
          <div className="flex h-full flex-col items-center justify-center gap-2 px-4 text-center">
            {agent && <Avatar name={agent.name} kind="agent" size={44}/>}
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
            {visibleMsgs.map((m) => {
              const isLast = m.id === (thread[thread.length - 1] || {}).id;
              const msgAgent = (m.agentId && tenant.agents.find((a) => a.id === m.agentId)) || agent;
              if (m.author === 'you') return <UserMessage key={m.id} m={m} onEdit={onEditSubmit} members={channelMembers}/>;
              if (m.author === 'other') return <OtherMessage key={m.id} m={m} members={channelMembers}/>;
              return (
                <AgentMessage key={m.id} m={m} agent={msgAgent} inChannel={target.kind === 'channel'}
                  streaming={streamingId === m.id} busy={busy} isLast={isLast}
                  onDone={onDoneStream} onCopy={onCopy} onGrow={pinBottom}
                  onRefresh={onRefresh} onBranch={onBranch} members={channelMembers}/>
              );
            })}
            {typing && <ThinkingRow agent={agent}/>}
          </div>
        )}
      </div>
      <div className="relative shrink-0">
        <button type="button" onClick={scrollToBottom} data-od-id="btn-scroll-bottom"
          aria-label="Scroll to bottom" title="Scroll to bottom"
          className={cx('absolute -top-11 left-1/2 z-10 flex h-9 w-9 -translate-x-1/2 items-center justify-center rounded-full border border-line bg-surface text-fg2 shadow-[var(--elev-raised)] transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] hover:text-fg', (atBottom || isEmpty) && 'invisible')}>
          <Icon name="down" size={15}/>
        </button>
        <div className="mx-auto w-full max-w-[44rem] px-4 pb-4">
          <Composer agent={agent} running={busy} onSend={onSend} onCancel={onCancel} onAttach={onAttach}
            mentionOptions={target.kind === 'channel' ? channelMembers : null}/>
          {isEmpty && agent && suggestions.length > 0 && (
            <div className="mt-3 flex flex-wrap items-center justify-center gap-2 px-1" data-od-id="welcome-suggestions">
              {suggestions.map((s) => (
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

