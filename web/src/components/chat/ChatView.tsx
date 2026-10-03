import { useState, useRef, useEffect, useSyncExternalStore } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { api } from "../../lib/api";
import { useStore, useMessageQueue } from "../../store";
import { getLiveChatStatus, subscribeLiveChat, retryLiveChat } from "../../lib/livechat";

import { ChatHeader } from "./ChatHeader";
import { Composer } from "./Composer";
import { QueueStack } from "./QueueStack";
import { UserMessage } from "./UserMessage";
import { OtherMessage } from "./OtherMessage";
import { AgentMessage } from "./AgentMessage";
import { ErrorEntry } from "./ErrorEntry";
import { PromptBlockedNotice } from "./PromptBlockedNotice";
import { MemoryIngestedChip } from "./MemoryIngestedChip";
import { SkillCandidateChip } from "./SkillCandidateChip";
import { ThinkingRow } from "./ThinkingRow";
import { CompactionDivider } from "./CompactionDivider";
import { DayDivider, dayKeyOf, fullStamp, parseEntryDate } from "./DayDivider";
import { ConversationRail } from "./ConversationRail";
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

export function ChatView({ tenant, target, agent, thread, session, channelMembers, onOpenMembers,
  typing, busy, compacting, allowAttachments, documentsLens, registerDocInserter,
  onSend, onCancel, onCopy, onRefresh, onBranch, onEditSubmit  }: any) {
  const listRef = useRef(null);
  const atBottomRef = useRef(true);
  const [atBottom, setAtBottom] = useState(true);
  const toast = useStore((s: any) => s.toast);

  // Drag-drop attach surface (add-chat-attachments D14): the whole chat view
  // (message list + composer region) accepts file drags when attachments are
  // allowed. Chip state stays in the Composer — dropped files are handed over
  // through its imperative ref. dragenter/dragleave use a depth COUNTER
  // because firing on child boundaries would flicker the overlay otherwise;
  // `pointer-events-none` on the overlay keeps the counter honest.
  const composerRef = useRef<{ addFiles: (files: File[]) => void } | null>(null);
  const dragDepthRef = useRef(0);
  const [dragActive, setDragActive] = useState(false);

  const hasFileDrag = (e: any) => {
    const types = e.dataTransfer?.types;
    if (!types) return false;
    for (let i = 0; i < types.length; i++) if (types[i] === 'Files') return true;
    return false;
  };

  // Chrome/Edge: dropping a folder yields no `.files` at all (items still list
  // file entries). Firefox: the folder appears as a zero-byte, type-less File.
  // Both shapes reject with the explicit toast; genuine zero-byte files are
  // rejected server-side anyway, so mislabeling them costs nothing.
  const extractDropFiles = (e: any): { files: File[]; folder: boolean } => {
    const dt = e.dataTransfer;
    const files: File[] = Array.from(dt?.files || []);
    if (files.some((f) => f.size === 0 && !f.type)) return { files: [], folder: true };
    if (files.length === 0) {
      const items = dt?.items;
      for (let i = 0; i < (items?.length || 0); i++) {
        if (items[i].kind === 'file') return { files: [], folder: true };
      }
    }
    return { files, folder: false };
  };

  const onDragEnter = (e: any) => {
    if (!allowAttachments || !hasFileDrag(e)) return;
    e.preventDefault();
    dragDepthRef.current += 1;
    setDragActive(true);
  };
  const onDragOver = (e: any) => {
    if (!allowAttachments || !hasFileDrag(e)) return;
    // Required: without preventDefault the drop event never fires and the
    // browser NAVIGATES to the dropped file.
    e.preventDefault();
  };
  const onDragLeave = (e: any) => {
    if (!allowAttachments) return;
    dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
    if (dragDepthRef.current === 0) setDragActive(false);
  };
  const onDrop = (e: any) => {
    if (!allowAttachments || !hasFileDrag(e)) return;
    // Same navigation hazard as dragover — preventDefault is mandatory here.
    e.preventDefault();
    dragDepthRef.current = 0;
    setDragActive(false);
    const { files, folder } = extractDropFiles(e);
    if (folder) {
      toast("Folders can't be attached — drop files instead");
      return;
    }
    if (files.length) composerRef.current?.addFiles(files);
  };

  // long-history guard: render the latest window, load older on demand
  const [msgLimit, setMsgLimit] = useState(80);
  const sessionId = session && session.id;
  const [limitSession, setLimitSession] = useState(sessionId);
  if (sessionId !== limitSession) { setLimitSession(sessionId); setMsgLimit(80); }
  const visibleMsgs = thread.length > msgLimit ? thread.slice(thread.length - msgLimit) : thread;
  const hiddenMsgs = thread.length - visibleMsgs.length;

  // A new message lands at the bottom — a sent message included, so the
  // response starts in view — and chat/session switches reset to the bottom.
  useEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [thread.length, typing, target.obj.id, sessionId]);

  // Follow the streaming tail: deltas mutate the last message in place (no
  // length change, the effect above never fires), so pinning runs after every
  // render while a turn is in flight — but only while the user hasn't
  // scrolled away; onScroll keeps atBottomRef honest and the jump-to-bottom
  // button re-engages it.
  useEffect(() => {
    if (!busy && !typing) return;
    const el = listRef.current;
    if (el && atBottomRef.current) el.scrollTop = el.scrollHeight;
  });

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
  // Message queue rows for this chat (adopt-assistant-ui-elements 8.2): the
  // key mirrors the runtime's enqueue addressing exactly (pos.tenantId ::
  // chat id). Present-only — QueueStack renders nothing with an empty queue.
  const queueTenantId = useStore((s: any) => s.pos.tenantId);
  const queuedMessages = useMessageQueue(queueTenantId, target.obj.id);
  // Right panel slice (add-right-panel 1.4): the header toggle reflects these.
  const panelOpen = useStore((s: any) => s.panel.open);
  const panelBadge = useStore((s: any) => s.panel.badge);
  // Documents toggle (rework-document-chat-surfaces D2, 2026-09-28 user
  // pivot): pressed while the panel shows the documents tab; clicking closes
  // the tab (last tab closed closes the panel) or mints it back — the listing
  // fills the panel, so the tab strip never strands the documents surface.
  const documentsTabId = useStore((s: any) =>
    s.panel.open ? s.panel.tabs.find((t: any) => t.kind === 'documents')?.id ?? null : null);
  const documentsOpen = Boolean(documentsTabId);
  const toggleDocuments = () => {
    const st = useStore.getState();
    const id = st.panel.tabs.find((t: any) => t.kind === 'documents')?.id;
    if (id) st.closePanelTab(id);
    else st.openPanelTab({ kind: 'documents', title: 'Documents', payload: {} });
  };
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

  // Per-author rendering of one transcript entry — unchanged from the
  // previous inline map; extracted so the day-separator pass below can wrap
  // every entry kind uniformly.
  const renderEntry = (m: any) => {
    const isLast = m.id === (thread[thread.length - 1] || {}).id;
    const msgAgent = (m.agentId && tenant.agents.find((a: any) => a.id === m.agentId)) || agent;
    if (m.author === 'you') return <UserMessage m={m} onEdit={onEditSubmit} members={channelMembers}/>;
    if (m.author === 'other') return <OtherMessage m={m} members={channelMembers}/>;
    if (m.author === 'error') return <ErrorEntry m={m}/>;
    // Hook-blocked prompt notice (integrate-agent-hooks): a
    // `prompt_blocked` transcript entry renders in place of the
    // assistant reply that never came — live and hydrated alike.
    if (m.author === 'notice') return <PromptBlockedNotice m={m}/>;
    // Post-turn memory chip (integrate-agent-zero-memory D11):
    // counts + visibility breakdown only, with the provenance
    // drawer — live events and hydrated history entries share it.
    if (m.author === 'memory')
      return <MemoryIngestedChip m={m} workspaceId={workspaceId}/>;
    // Skill-curation chip (add-skill-curation-from-traces): a qualifying
    // run's transcript breadcrumb — live events and hydrated history
    // entries share it, mirroring the memory chip above.
    if (m.author === 'skill_candidate')
      return <SkillCandidateChip m={m}/>;
    // Context compaction marker (chat-compact-command): live events
    // and hydrated history entries share this divider component.
    if (m.author === 'compaction') return <CompactionDivider m={m}/>;
    return (
      <AgentMessage m={m} agent={msgAgent} inChannel={target.kind === 'channel'}
        busy={busy} isLast={isLast}
        onCopy={onCopy}
        onRefresh={onRefresh} onBranch={onBranch} members={channelMembers}
        sessionId={sessionId}
        onResolveApproval={handleResolveApproval}/>
    );
  };

  return (
    <section data-od-id="chat-view" aria-label={'Conversation with ' + (target.kind === 'channel' ? '#' + target.obj.name : target.obj.name)}
      onDragEnter={onDragEnter} onDragOver={onDragOver} onDragLeave={onDragLeave} onDrop={onDrop}
      className="relative flex min-w-0 flex-1 flex-col bg-bg">
      {dragActive && (
        <div data-testid="drop-overlay" className="pointer-events-none absolute inset-2 z-20 flex items-center justify-center rounded-[16px] border-2 border-dashed border-accent bg-[color-mix(in_oklab,var(--accent)_6%,transparent)]">
          <span className="flex items-center gap-2 rounded-full border border-line bg-surface px-4 py-2 text-[13px] font-medium text-fg shadow-[var(--elev-raised)]">
            <Icon name="down" size={14}/>
            Drop to attach
          </span>
        </div>
      )}
      {/* Header panel wiring (add-right-panel 1.4): open state + dot badge
          come straight from the store's panel slice; the toggle flips it.
          onOpenMembers (channels) opens the panel with the members tab
          focused — the configure prop is gone; configuration lives on the
          Agents screen. */}
      <ChatHeader target={target} agent={agent} session={session} channelMembers={channelMembers} usage={session?.usage} langfuseUrl={session?.langfuseUrl}
        onOpenMembers={onOpenMembers}
        panelOpen={panelOpen} panelBadge={panelBadge}
        onTogglePanel={() => useStore.getState().setPanelOpen(!panelOpen)}
        documentsAvailable={Boolean(documentsLens)} documentsOpen={documentsOpen}
        onToggleDocuments={toggleDocuments}/>
      {/* Rail column + transcript: the conversation rail is a real flex child
          at the LEFT edge of the chat pane (before the chat, not floating
          beside the centered column), so it stays pinned while the transcript
          scrolls in its own container. Hidden below md. */}
      <div className="flex min-h-0 flex-1">
        <ConversationRail entries={visibleMsgs} listRef={listRef}/>
        <div ref={listRef} onScroll={onScroll} role="log" aria-label="Messages" className="od-scroll min-w-0 flex-1 overflow-y-auto" data-od-id="message-list">
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
            {/* Day separators + hover timestamps (adopt-assistant-ui-elements
                9.2, design D11): a divider renders wherever the calendar day
                changes between consecutive DATED entries (never before the
                first dated entry, and never from an undated one); dated
                entries expose their full date+time as a hover title.
                Undated entries (legacy display `ts` strings) contribute
                neither. The compaction divider entry keeps its own dedicated
                marker above. */}
            {(() => {
              let lastDayKey: string | null = null;
              return visibleMsgs.map((m: any) => {
                const d = parseEntryDate(m);
                const out: any[] = [];
                if (d) {
                  const key = dayKeyOf(d);
                  if (lastDayKey !== null && key !== lastDayKey) {
                    out.push(<DayDivider key={'day-' + key} date={d}/>);
                  }
                  lastDayKey = key;
                }
                out.push(
                  <div key={m.id} data-msg-id={m.id} title={d ? fullStamp(d) : undefined}>
                    {renderEntry(m)}
                  </div>
                );
                return out;
              });
            })()}
            {/* The streaming agent message renders its own loading dots and
                caret — the thinking row is only for a turn with no agent
                message on the transcript yet, else the agent shows twice.
                A compact turn shows its own status row instead (no optimistic
                rows at all); once the compacted divider lands the tail is a
                compaction entry and neither row renders while the turn
                finishes. */}
            {typing && compacting && (
              <div className="flex gap-3 px-2 py-1" data-od-id="compaction-status" role="status" aria-label="Compacting context">
                <span className="inline-block h-2 w-2 animate-pulse self-center rounded-full bg-fg"/>
                <span className="self-center text-[13px] text-muted">Compacting context…</span>
              </div>
            )}
            {typing && !compacting && thread[thread.length - 1]?.author !== 'agent' && thread[thread.length - 1]?.author !== 'compaction' && <ThinkingRow agent={agent}/>}
          </div>
        )}
        </div>
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
        {/* Message queue stack (8.2): ordered cancelable rows under the
            running row while a turn is in flight — between the transcript
            and the composer; hidden entirely when nothing is queued. */}
        <QueueStack items={queuedMessages} running={busy} agentName={agent?.name}
          onRemove={(id) => useStore.getState().removeQueuedChatMessage(queueTenantId, target.obj.id, id)}/>
        <div className="mx-auto w-full max-w-[44rem] px-4 pb-4">
          <Composer agent={agent} running={busy} onSend={onSend} onCancel={onCancel}
            ref={composerRef} allowAttachments={allowAttachments} workspaceSlug={workspaceId}
            chatId={target.obj.id}
            registerDocMention={registerDocInserter}
            mentionOptions={target.kind === 'channel' ? channelMembers : null}
            allowCommands={target.kind === 'agent'}
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

