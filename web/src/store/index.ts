// @ts-nocheck
import { create } from 'zustand';
import { useEffect } from 'react';
import type { Workspace, Agent, Message, CronJob, Channel } from '../data/types';
import { seedDb, MENTION_REPLIES, REPLY_TEMPLATES } from '../data/seed';
import { uid, nowTime, parseMentions, craftReply } from '../lib/helpers';

export const useSearchShortcut = () => {
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        const el = document.getElementById('od-search');
        if (el) el.focus();
      }
    };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  }, []);
};

const POS_KEY = 'od-onclaw-pos';
const loadPos = () => {
  try { return JSON.parse(localStorage.getItem(POS_KEY) || 'null') || null; }
  catch (e) { return null; }
};
const savePos = (p: any) => {
  try { localStorage.setItem(POS_KEY, JSON.stringify(p)); }
  catch (e) {}
};

export interface AppState {
  db: Record<string, Workspace>;
  pos: {
    tenantId: string;
    view: string;
    chatId: string;
    showContext: boolean;
  };
  ui: {
    settingsOpen: boolean;
    settingsTab: string;
    configAgent: string | null;
    cronEdit: any | null;
    wsOpen: boolean;
    createWsOpen: boolean;
    toasts: any[];
    typing: boolean;
    streamId: string | null;
  };
  search: string;

  // actions
  patchUi: (p: Partial<AppState['ui']>) => void;
  goPos: (p: Partial<AppState['pos']>) => void;
  setSearch: (q: string) => void;
  toast: (text: string, kind?: string) => void;
  
  updateTenant: (tenantId: string, fn: (t: Workspace) => Workspace) => void;
  pushMsg: (tid: string, cid: string, msg: Message) => void;
  
  selectChat: (id: string) => void;
  openMember: (id: string) => void;
  addChannelMember: (chatId: string, id: string) => void;
  removeChannelMember: (chatId: string, id: string) => void;
  
  send: (text: string) => void;
  respondFor: (tid: string, cid: string, ag: Agent, origText: string, opts?: { delay?: number, mentioned?: boolean }) => void;
  refreshMessage: (mid: string) => void;
  branchNav: (mid: string, dir: number) => void;
  editSubmit: (mid: string, newText: string) => void;
  
  newSession: () => void;
  switchSession: (sid: string) => void;
  deleteSession: (sid: string) => void;
  switchTenant: (id: string) => void;
  upsertAgent: (values: Partial<Agent>) => void;
  
  runNow: (job: CronJob) => void;
  toggleCron: (job: CronJob) => void;
  deleteCron: (job: CronJob) => void;
  saveCron: (draft: Partial<CronJob>) => void;
  
  createWorkspace: (ws: Workspace) => void;
  deleteWorkspace: (id: string) => void;
}

const initialPos = (() => {
  const p = loadPos();
  return p && p.tenantId && p.view ? p : { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false };
})();

// cleanup-safe timers
const activeTimers: Record<string, ReturnType<typeof setTimeout>> = {};
const setSafeTimer = (key: string, fn: () => void, delay: number) => {
  if (activeTimers[key]) clearTimeout(activeTimers[key]);
  activeTimers[key] = setTimeout(() => {
    delete activeTimers[key];
    fn();
  }, delay);
};

export const useStore = create<AppState>((set, get) => ({
  db: seedDb(),
  pos: initialPos,
  ui: {
    settingsOpen: false, settingsTab: 'workspace', configAgent: null, cronEdit: null,
    wsOpen: false, createWsOpen: false, toasts: [], typing: false, streamId: null
  },
  search: '',

  patchUi: (p) => set((s) => ({ ui: { ...s.ui, ...p } })),
  goPos: (p) => set((s) => {
    const newPos = { ...s.pos, ...p };
    savePos(newPos);
    return { pos: newPos };
  }),
  setSearch: (q) => set({ search: q }),
  
  toast: (text, kind) => {
    const id = uid('t');
    set((s) => ({ ui: { ...s.ui, toasts: [...s.ui.toasts, { id, text, kind }] } }));
    setSafeTimer(`toast-${id}`, () => {
      set((s) => ({ ui: { ...s.ui, toasts: s.ui.toasts.filter((t) => t.id !== id) } }));
    }, 3200);
  },

  updateTenant: (tenantId, fn) => set((s) => {
    const t = s.db[tenantId];
    if (!t) return s;
    return { db: { ...s.db, [tenantId]: fn(JSON.parse(JSON.stringify(t))) } };
  }),

  pushMsg: (tid, cid, msg) => set((s) => {
    const d = JSON.parse(JSON.stringify(s.db));
    const t = d[tid];
    if (!t) return s;
    const th = t.threads[cid] = t.threads[cid] || { active: null, list: [] };
    if (Array.isArray(th)) {
      t.threads[cid] = { active: null, list: th.length ? [{ id: uid('s'), title: 'Chat', updated: '', messages: th }] : [] };
    }
    const state = t.threads[cid];
    let sess = state.list.find((x: any) => x.id === state.active);
    if (!sess) {
      sess = { id: uid('s'), title: 'New chat', updated: nowTime(), messages: [] };
      state.list.unshift(sess);
      state.active = sess.id;
    }
    sess.messages.push(msg);
    if (msg.author === 'you' && (sess.title === 'New chat' || !sess.title)) {
      sess.title = msg.text.length > 42 ? msg.text.slice(0, 42) + '…' : msg.text;
    }
    sess.updated = 'just now';
    return { db: d };
  }),

  selectChat: (id) => {
    const state = get();
    state.goPos({ view: 'chats', chatId: id });
    const t = state.db[state.pos.tenantId];
    if (!t) return;
    const ch = t.channels.find((c) => c.id === id);
    if (ch) state.goPos({ showContext: true });
    else state.goPos({ showContext: false });
    if (ch && ch.unread) {
      state.updateTenant(state.pos.tenantId, (tenant) => ({
        ...tenant,
        channels: tenant.channels.map((c) => (c.id === id ? { ...c, unread: 0 } : c))
      }));
    }
  },

  openMember: (id) => {
    if (id) get().selectChat(id);
  },

  addChannelMember: (chatId, id) => {
    if (!id) return;
    const state = get();
    const tid = state.pos.tenantId;
    state.updateTenant(tid, (t) => ({
      ...t,
      channels: t.channels.map((c) => (c.id === chatId && !(c.members || []).includes(id) ? { ...c, members: [...(c.members || (c.agentId ? [c.agentId] : [])), id] } : c))
    }));
    const t = state.db[tid];
    const channel = t.channels.find((c) => c.id === chatId);
    const m = t.agents.find((x) => x.id === id) || t.people.find((x) => x.id === id);
    if (channel) state.toast((m ? m.name : 'Member') + ' added to #' + channel.name);
  },

  removeChannelMember: (chatId, id) => {
    const state = get();
    const tid = state.pos.tenantId;
    const t = state.db[tid];
    const channel = t.channels.find((c) => c.id === chatId);
    if (!id || (channel && id === channel.agentId)) return;
    state.updateTenant(tid, (tenant) => ({
      ...tenant,
      channels: tenant.channels.map((c) => (c.id === chatId ? { ...c, members: (c.members || []).filter((x) => x !== id) } : c))
    }));
    const m = t.agents.find((x) => x.id === id) || t.people.find((x) => x.id === id);
    if (channel) state.toast((m ? m.name : 'Member') + ' removed from #' + channel.name);
  },

  respondFor: (tid, cid, ag, origText, opts) => {
    const state = get();
    const delay = opts?.delay;
    const mentioned = opts?.mentioned;
    state.patchUi({ typing: true });
    setSafeTimer(`respond-${ag.id}`, () => {
      const isSlash = origText.trim().startsWith('/');
      const mid = uid('m');
      get().pushMsg(tid, cid, {
        id: mid, author: 'agent', agentId: ag.id, ts: nowTime(),
        text: mentioned ? MENTION_REPLIES[Math.floor(Math.random() * MENTION_REPLIES.length)] : craftReply(ag, origText),
        tools: isSlash || ag.tools.length === 0 ? undefined
          : [{ name: ag.tools[0] + '.query', args: 'q: ' + origText.slice(0, 48), ms: 600 + Math.floor(Math.random() * 900) }]
      });
      get().patchUi({ typing: false, streamId: mid });
      if (!mentioned && origText.trim().toLowerCase().startsWith('/schedule')) {
        setSafeTimer(`cron-edit-${mid}`, () => {
          get().patchUi({ cronEdit: { id: null, name: '', agentId: ag.id, expr: '0 9 * * 1-5', human: '', enabled: true } });
        }, 700);
      }
    }, delay || 850 + Math.random() * 550);
  },

  cancelReply: () => {
    get().patchUi({ typing: false, streamId: null });
    for (const k in activeTimers) {
      if (k.startsWith('respond-')) {
        clearTimeout(activeTimers[k]);
        delete activeTimers[k];
      }
    }
  },

  send: (text) => {
    const state = get();
    const tid = state.pos.tenantId;
    const cid = state.pos.chatId;
    const t = state.db[tid];
    
    // get targets
    const agent = t.agents.find((a) => a.id === cid) || null;
    const channel = t.channels.find((c) => c.id === cid) || null;
    const person = t.people.find((p) => p.id === cid) || null;
    const chatAgent = agent || (channel ? t.agents.find((a) => a.id === channel.agentId) || null : null);
    const target = agent ? { kind: 'agent', obj: agent } : channel ? { kind: 'channel', obj: channel } : { kind: 'person', obj: person };

    state.pushMsg(tid, cid, { id: uid('m'), author: 'you', ts: nowTime(), text });
    
    if (target.kind === 'person' || !chatAgent) return;
    
    if (text.trim().toLowerCase().startsWith('/reset')) {
      state.updateTenant(tid, (tenant) => {
        const th = tenant.threads[cid];
        const s = th && th.list.find((x) => x.id === th.active);
        if (s) { s.messages = []; s.title = 'New chat'; s.updated = nowTime(); }
        return tenant;
      });
      state.toast('Thread cleared');
      return;
    }
    
    if (target.kind === 'channel' && channel) {
      const ids = channel.members && channel.members.length ? channel.members : (channel.agentId ? [channel.agentId] : []);
      const channelMembers = ids.map(id => {
        const a = t.agents.find(x => x.id === id);
        if (a) return { id, kind: 'agent', name: a.name, agent: a };
        const p = t.people.find(x => x.id === id);
        if (p) return { id, kind: 'person', name: p.name, presence: p.presence };
        return null;
      }).filter(Boolean);
      
      const mentionedAgents = parseMentions(text, channelMembers)
        .filter((m: any) => m.kind === 'agent')
        .map((m: any) => t.agents.find((a) => a.id === m.id))
        .filter(Boolean) as Agent[];
        
      if (mentionedAgents.length > 0) {
        mentionedAgents.forEach((ag, i) => state.respondFor(tid, cid, ag, text, { delay: 850 + i * 1200 + Math.random() * 400, mentioned: true }));
        return;
      }
    }
    
    state.respondFor(tid, cid, chatAgent, text);
  },

  refreshMessage: (mid) => {
    const state = get();
    const tid = state.pos.tenantId;
    const cid = state.pos.chatId;
    const t = state.db[tid];
    
    const agent = t.agents.find((a) => a.id === cid) || null;
    const channel = t.channels.find((c) => c.id === cid) || null;
    const chatAgent = agent || (channel ? t.agents.find((a) => a.id === channel.agentId) || null : null);
    
    if (!chatAgent) return;
    
    const th = t.threads[cid];
    const s = th && th.list.find((x) => x.id === th.active);
    const m = s && s.messages.find((x) => x.id === mid);
    if (!m || m.author !== 'agent') return;
    
    const branches = m.branches || [];
    const curBranch = branches[m.branch || 0];
    const cur = (curBranch && curBranch.text) ? curBranch.text : m.text || '';
    
    let next = cur;
    for (let k = 0; k < 6 && next === cur; k++) next = REPLY_TEMPLATES[Math.floor(Math.random() * REPLY_TEMPLATES.length)];
    
    state.patchUi({ typing: true });
    setSafeTimer(`refresh-${mid}`, () => {
      get().updateTenant(tid, (tenant) => {
        const _th = tenant.threads[cid];
        const _sess = _th && _th.list.find((x) => x.id === _th.active);
        const _mm = _sess && _sess.messages.find((x) => x.id === mid);
        if (_mm) {
           const existingBranches = _mm.branches || [{ text: _mm.text } as Message];
           _mm.branches = [...existingBranches, { text: next } as Message];
           _mm.branch = _mm.branches.length - 1;
        }
        return tenant;
      });
      get().patchUi({ typing: false, streamId: mid });
    }, 650 + Math.random() * 450);
  },

  branchNav: (mid, dir) => {
    const state = get();
    const tid = state.pos.tenantId;
    const cid = state.pos.chatId;
    
    state.updateTenant(tid, (tenant) => {
      const th = tenant.threads[cid];
      const sess = th && th.list.find((x) => x.id === th.active);
      const mm = sess && sess.messages.find((x) => x.id === mid);
      if (mm) {
        const count = (mm.branches || []).length || 1;
        mm.branch = Math.min(count - 1, Math.max(0, (mm.branch || 0) + dir));
      }
      return tenant;
    });
    state.patchUi({ streamId: null });
  },

  editSubmit: (mid, newText) => {
    const state = get();
    const tid = state.pos.tenantId;
    const cid = state.pos.chatId;
    
    state.updateTenant(tid, (tenant) => {
      const th = tenant.threads[cid];
      const sess = th && th.list.find((x) => x.id === th.active);
      if (sess) {
        const i = sess.messages.findIndex((x) => x.id === mid);
        if (i >= 0) {
          sess.messages[i] = { ...sess.messages[i], text: newText };
          sess.messages = sess.messages.slice(0, i + 1);
        }
      }
      return tenant;
    });
    
    const t = get().db[tid];
    const agent = t.agents.find((a) => a.id === cid) || null;
    const channel = t.channels.find((c) => c.id === cid) || null;
    const person = t.people.find((p) => p.id === cid) || null;
    const chatAgent = agent || (channel ? t.agents.find((a) => a.id === channel.agentId) || null : null);
    
    if (person || !chatAgent) return;
    get().respondFor(tid, cid, chatAgent, newText);
  },

  newSession: () => {
    const state = get();
    const tid = state.pos.tenantId;
    const cid = state.pos.chatId;
    const t = state.db[tid];
    const target = t.agents.find((a) => a.id === cid) ? 'agent' : null;
    if (target !== 'agent') return;
    
    state.updateTenant(tid, (tenant) => {
      const th = tenant.threads[cid] = tenant.threads[cid] || { active: null, list: [] };
      const cur = th.list.find((x) => x.id === th.active);
      if (cur && cur.messages.length === 0) return tenant;
      const s = { id: uid('s'), title: 'New chat', updated: nowTime(), messages: [] };
      th.list.unshift(s);
      th.active = s.id;
      return tenant;
    });
    state.patchUi({ typing: false, streamId: null });
  },

  switchSession: (sid) => {
    const state = get();
    const tid = state.pos.tenantId;
    const cid = state.pos.chatId;
    state.updateTenant(tid, (tenant) => {
      const th = tenant.threads[cid];
      if (th && !Array.isArray(th)) th.active = sid;
      return tenant;
    });
    state.patchUi({ typing: false, streamId: null });
  },

  deleteSession: (sid) => {
    const state = get();
    const tid = state.pos.tenantId;
    const cid = state.pos.chatId;
    state.updateTenant(tid, (tenant) => {
      const th = tenant.threads[cid];
      if (!th || Array.isArray(th)) return tenant;
      th.list = th.list.filter((x) => x.id !== sid);
      if (th.list.length === 0) {
        const s = { id: uid('s'), title: 'New chat', updated: nowTime(), messages: [] };
        th.list.push(s);
        th.active = s.id;
      } else if (th.active === sid) {
        th.active = th.list[0].id;
      }
      return tenant;
    });
    state.patchUi({ typing: false, streamId: null });
    state.toast('Session deleted');
  },

  switchTenant: (id) => {
    const state = get();
    state.patchUi({ wsOpen: false });
    state.goPos({ tenantId: id, view: 'chats', chatId: state.db[id].agents[0].id });
    state.toast('Switched to ' + state.db[id].name);
  },

  upsertAgent: (values) => {
    const state = get();
    const ui = state.ui;
    const tid = state.pos.tenantId;
    
    if (ui.configAgent === 'new') {
      const id = uid('a');
      state.updateTenant(tid, (t) => ({
        ...t,
        agents: [...t.agents, { id, ...values, status: 'idle', lastActive: 'just now' } as Agent],
        threads: { ...t.threads, [id]: { active: null, list: [] } }
      }));
      state.patchUi({ configAgent: null });
      state.goPos({ view: 'chats', chatId: id });
      state.toast(values.name + ' deployed — it idles until its first message');
    } else {
      const aid = ui.configAgent;
      state.updateTenant(tid, (t) => ({
        ...t,
        agents: t.agents.map((a) => (a.id === aid ? { ...a, ...values } as Agent : a))
      }));
      state.patchUi({ configAgent: null });
      state.toast(values.name + ' updated — new settings apply to the next run');
    }
  },

  runNow: (job) => {
    const state = get();
    const rid = 'run_' + Math.floor(1000 + Math.random() * 8999);
    state.updateTenant(state.pos.tenantId, (t) => ({
      ...t,
      runs: [{ id: rid, agentId: job.agentId, trigger: 'manual', when: nowTime(), dur: '—', tokens: '—', status: 'running' } as any, ...t.runs]
    }));
    state.toast('Triggered “' + job.name + '”');
    
    setSafeTimer(`run-${rid}`, () => {
      get().updateTenant(get().pos.tenantId, (t) => ({
        ...t,
        runs: t.runs.map((r) => (r.id === rid ? { ...r, status: 'success', dur: '7s', tokens: '1.8k' } : r))
      }));
    }, 1600);
  },

  toggleCron: (job) => {
    const state = get();
    state.updateTenant(state.pos.tenantId, (t) => ({
      ...t,
      cron: t.cron.map((j) => (j.id === job.id ? { ...j, enabled: !j.enabled } : j))
    }));
    state.toast((job.enabled ? 'Paused “' : 'Resumed “') + job.name + '”');
  },

  deleteCron: (job) => {
    const state = get();
    state.updateTenant(state.pos.tenantId, (t) => ({
      ...t,
      cron: t.cron.filter((j) => j.id !== job.id)
    }));
    state.patchUi({ cronEdit: null });
    state.toast('Schedule “' + job.name + '” deleted');
  },

  saveCron: (draft) => {
    const state = get();
    if (draft.id) {
      state.updateTenant(state.pos.tenantId, (t) => ({
        ...t,
        cron: t.cron.map((j) => (j.id === draft.id ? { ...j, ...draft } as CronJob : j))
      }));
      state.toast('“' + draft.name + '” saved — fires ' + (draft.human || '').toLowerCase());
    } else {
      state.updateTenant(state.pos.tenantId, (t) => ({
        ...t,
        cron: [...t.cron, { ...draft, id: uid('cron'), next: 'per expression', last: null } as any]
      }));
      state.toast('Schedule “' + draft.name + '” created');
    }
    state.patchUi({ cronEdit: null });
  },

  createWorkspace: (ws) => {
    const state = get();
    // the actual normalization using `withSessions` shouldn't modify the argument, 
    // but we trust `ws` is valid since it's already normalized by `blankTenant`
    set((s) => ({ db: { ...s.db, [ws.id]: ws } }));
    state.patchUi({ createWsOpen: false });
    state.goPos({ tenantId: ws.id, view: 'chats', chatId: ws.agents.length ? ws.agents[0].id : '' });
    state.toast(ws.name + ' created — you are its owner');
  },

  deleteWorkspace: (id) => {
    const state = get();
    const rest = Object.keys(state.db).filter((k) => k !== id);
    if (rest.length === 0) {
      state.toast('OnClaw keeps at least one workspace active', 'danger');
      return;
    }
    const nextId = rest[0];
    const next = state.db[nextId];
    set((s) => {
      const nd = { ...s.db };
      delete nd[id];
      return { db: nd };
    });
    state.patchUi({ settingsOpen: false });
    state.goPos({ tenantId: nextId, view: 'chats', chatId: next.agents.length ? next.agents[0].id : '' });
    state.toast('Workspace deleted — switched to ' + next.name);
  }
}));

// Export selectors
export const useWorkspace = () => useStore((s) => s.db[s.pos.tenantId]);
export const useTenant = useWorkspace;
export const useThread = (chatId: string) => useStore((s) => {
  const t = s.db[s.pos.tenantId];
  if (!t || !t.threads[chatId]) return { active: null, list: [] };
  const th = t.threads[chatId];
  if (Array.isArray(th)) return th.length ? { active: 's0', list: [{ id: 's0', title: 'Chat', updated: '', messages: th }] } : { active: null, list: [] };
  return th;
});
export const useSessions = (chatId: string) => useStore((s) => {
  const th = s.db[s.pos.tenantId]?.threads[chatId];
  return th && !Array.isArray(th) ? th.list : [];
});
export const useRuns = () => useStore((s) => s.db[s.pos.tenantId]?.runs || []);
