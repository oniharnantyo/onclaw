import { useCallback } from 'react';
import { useExternalStoreRuntime } from '@assistant-ui/react';
import type { ThreadMessageLike, AppendMessage } from '@assistant-ui/react';
import { useStore, useWorkspace } from '../store';
import type { Message, Agent } from '../data/types';
import { uid, nowTime, parseMentions, craftReply } from '../lib/helpers';
import { MENTION_REPLIES, REPLY_TEMPLATES } from '../lib/constants';

const activeTimers: Record<string, any> = {};
const setSafeTimer = (key: string, fn: () => void, ms: number) => {
  if (activeTimers[key]) clearTimeout(activeTimers[key]);
  activeTimers[key] = setTimeout(() => { delete activeTimers[key]; fn(); }, ms);
};

export function useChatRuntime(chatId: string) {
  const patchUi = useStore(s => s.patchUi);
  const tenantId = useStore(s => s.pos.tenantId);
  const tenant = useWorkspace();
  const threadData = tenant?.threads[chatId];
  const session = threadData && threadData.list.find((x: any) => x.id === threadData.active);
  const messages = session ? session.messages : [];
  
  const running = useStore(s => !!s.ui.running);

  const convertMessage = useCallback((msg: Message): ThreadMessageLike => {
    // Note: convertMessage drops branch-variant resolution it previously had.
    // This is harmless today as branches map back to m.text, but worth documenting this coupling.
    const content: any[] = [{ type: 'text', text: msg.text || '' }];
    
    if (msg.tools && msg.tools.length > 0) {
      msg.tools.forEach((tool: any) => {
        // Gate tool parts on the currently running message
        const isCurrentlyRunning = running && msg.id === session?.messages[session.messages.length - 1]?.id;
        if (!isCurrentlyRunning) {
           content.push({
             type: 'tool-call',
             toolName: tool.name,
             toolCallId: `call_${msg.id}_${tool.name}`,
             args: { raw: tool.args },
           });
        }
      });
    }

    return {
      id: msg.id,
      role: msg.author === 'agent' ? 'assistant' : 'user',
      content,
      createdAt: msg.ts ? new Date(msg.ts) : undefined,
      metadata: {
        custom: {
          onclaw: {
            agentId: msg.agentId,
            cron: msg.cron,
            name: msg.name,
            author: msg.author
          }
        }
      }
    };
  }, [running, session]);

  const respondFor = useCallback((tid: string, cid: string, ag: Agent, origText: string, opts?: any) => {
    const delay = opts?.delay;
    const mentioned = opts?.mentioned;
    patchUi({ running: true });
    setSafeTimer(`respond-${ag.id}`, () => {
      const isSlash = origText.trim().startsWith('/');
      const mid = uid('m');
      const text = mentioned ? MENTION_REPLIES[Math.floor(Math.random() * MENTION_REPLIES.length)] : craftReply(ag, origText);
      const tools = isSlash || ag.tools.length === 0 ? undefined 
          : [{ name: ag.tools[0] + '.query', args: 'q: ' + origText.slice(0, 48), ms: 600 + Math.floor(Math.random() * 900) }];
      
      useStore.getState().pushMsg(tid, cid, {
        id: mid, author: 'agent', agentId: ag.id, ts: nowTime(),
        text, tools
      });
      useStore.getState().patchUi({ running: false });
      
      if (!mentioned && origText.trim().toLowerCase().startsWith('/schedule')) {
        setSafeTimer(`cron-edit-${mid}`, () => {
          useStore.getState().patchUi({ cronEdit: { id: null, name: '', agentId: ag.id, expr: '0 9 * * 1-5', human: '', enabled: true } });
        }, 700);
      }
    }, delay || 850 + Math.random() * 550);
  }, [patchUi]);

  const onNew = useCallback(async (msg: AppendMessage) => {
    const target = useStore.getState().db[tenantId]?.agents.find((a: any) => a.id === chatId) ? 'agent' : 'channel';
    const text = msg.content.map((c: any) => c.text).join('') || '';
    
    useStore.getState().pushMsg(tenantId, chatId, { id: uid('m'), author: 'you', ts: nowTime(), text });
    
    const db = useStore.getState().db[tenantId];
    const agent = db.agents.find((a: any) => a.id === chatId);
    const channel = db.channels.find((c: any) => c.id === chatId);
    const chatAgent = agent || (channel ? db.agents.find((a: any) => a.id === channel.agentId) : null);
    
    if (target === 'agent' && !chatAgent) return;
    
    if (text.trim().toLowerCase().startsWith('/reset')) {
      useStore.getState().updateTenant(tenantId, (t) => {
        const th = t.threads[chatId];
        const s = th && th.list.find((x: any) => x.id === th.active);
        if (s) { s.messages = []; s.title = 'New chat'; s.updated = nowTime(); }
        return t;
      });
      useStore.getState().toast('Thread cleared');
      return;
    }

    if (target === 'channel' && channel) {
      const ids = channel.members && channel.members.length ? channel.members : (channel.agentId ? [channel.agentId] : []);
      const channelMembers = ids.map((id: string) => {
        const a = db.agents.find((x: any) => x.id === id);
        if (a) return { id, kind: 'agent', name: a.name, agent: a };
        return null;
      }).filter(Boolean);
      
      const mentionedAgents = parseMentions(text, channelMembers)
        .filter((m: any) => m.kind === 'agent')
        .map((m: any) => db.agents.find((a: any) => a.id === m.id))
        .filter(Boolean) as Agent[];
        
      if (mentionedAgents.length > 0) {
        mentionedAgents.forEach((ag, i) => respondFor(tenantId, chatId, ag, text, { delay: 850 + i * 1200 + Math.random() * 400, mentioned: true }));
        return;
      }
    }
    
    if (chatAgent) respondFor(tenantId, chatId, chatAgent, text);
  }, [tenantId, chatId, respondFor]);

  const onCancel = useCallback(async () => {
    patchUi({ running: false });
    for (const k in activeTimers) {
      if (k.startsWith('respond-') || k.startsWith('refresh-') || k.startsWith('stream-')) {
        clearTimeout(activeTimers[k]);
        delete activeTimers[k];
      }
    }
  }, [patchUi]);
  
  const onEdit = useCallback(async (msg: AppendMessage) => {
    const db = useStore.getState().db[tenantId];
    if (db.channels.find((c: any) => c.id === chatId)) return; // DM-only gating

    const text = msg.content.map((c: any) => c.text).join('') || '';
    const mid = (msg as any).sourceId || msg.parentId;
    
    useStore.getState().updateTenant(tenantId, (t) => {
      const th = t.threads[chatId];
      const s = th && th.list.find((x: any) => x.id === th.active);
      if (s) {
        const idx = s.messages.findIndex((x: any) => x.id === mid);
        if (idx !== -1) {
          s.messages = s.messages.slice(0, idx + 1);
          s.messages[idx].text = text;
        }
      }
      return t;
    });

    const agent = db.agents.find((a: any) => a.id === chatId);
    if (agent) respondFor(tenantId, chatId, agent, text);
  }, [tenantId, chatId, respondFor]);
  
  const onReload = useCallback(async (parentId: string | null) => {
    const db = useStore.getState().db[tenantId];
    if (db.channels.find((c: any) => c.id === chatId)) return; // DM-only gating

    const agent = db.agents.find((a: any) => a.id === chatId);
    const chatAgent = agent;
    if (!chatAgent) return;
    
    const th = db.threads[chatId];
    const s = th && th.list.find((x: any) => x.id === th.active);
    const m = s && s.messages.find((x: any) => x.id === parentId);
    if (!m || m.author !== 'agent') return;
    
    const branches = (m as any).branches || [];
    const curBranch = branches[(m as any).branch || 0];
    const cur = (curBranch && curBranch.text) ? curBranch.text : m.text || '';
    
    let next = cur;
    for (let k = 0; k < 6 && next === cur; k++) next = REPLY_TEMPLATES[Math.floor(Math.random() * REPLY_TEMPLATES.length)];
    
    useStore.getState().patchUi({ running: true });
    setSafeTimer(`refresh-${parentId}`, () => {
      useStore.getState().updateTenant(tenantId, (t) => {
        const _th = t.threads[chatId];
        const _sess = _th && _th.list.find((x: any) => x.id === _th.active);
        const _mm = _sess && _sess.messages.find((x: any) => x.id === parentId);
        if (_mm) {
           const existingBranches = (_mm as any).branches || [{ text: _mm.text }];
           (_mm as any).branches = [...existingBranches, { text: next }];
           (_mm as any).branch = _mm.branches.length - 1;
        }
        return t;
      });
      useStore.getState().patchUi({ running: false });
    }, 650 + Math.random() * 450);
  }, [tenantId, chatId]);

  const runtime = useExternalStoreRuntime({
    messages,
    convertMessage,
    isRunning: running,
    onNew,
    onCancel,
    onEdit,
    onReload: onReload as any
  });

  const branchNav = useCallback((mid: string, dir: number) => {
    const db = useStore.getState().db[tenantId];
    if (db.channels.find((c: any) => c.id === chatId)) return; // DM-only gating
    useStore.getState().updateTenant(tenantId, (t) => {
      const th = t.threads[chatId];
      const s = th && th.list.find((x: any) => x.id === th.active);
      const m = s && s.messages.find((x: any) => x.id === mid);
      if (m && (m as any).branches) {
        const blen = (m as any).branches.length;
        let nb = ((m as any).branch || 0) + dir;
        if (nb < 0) nb = 0;
        if (nb >= blen) nb = blen - 1;
        (m as any).branch = nb;
        (m as any).text = (m as any).branches[nb].text;
      }
      return t;
    });
  }, [tenantId, chatId]);

  return { runtime, onNew, onCancel, onEdit, onReload, branchNav, convertMessage };
}
