import { create } from 'zustand';
import { useEffect, useMemo } from 'react';
import type { Workspace, Agent, CronJob } from '../data/types';
import { seedDb, blankTenant } from "../data/seed";
import { uid, nowTime } from '../lib/helpers';
import { useAuthStore } from './auth';
import { useConnectionStore } from './connection';
import { api, pollAgentPromptsStatus, formatApiError, type ApiMemberView } from '../lib/api';
import { overlayPersistedThreads, persistAllThreads } from './threadPersistence';

export { useConnectionStore, type ConnectionState } from './connection';

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
  catch { return null; }
};
const savePos = (p: any) => {
  try { localStorage.setItem(POS_KEY, JSON.stringify(p)); }
  catch { /* storage unavailable (private mode) — position just won't persist */ }
};

// Mint a server-addressable session id (D2). Falls back to a manual v4 when
// randomUUID is unavailable (older jsdom).
export const mintSessionId = (): string => {
  const c = globalThis.crypto as Crypto | undefined;
  if (c?.randomUUID) return 'sess_' + c.randomUUID();
  const bytes = new Uint8Array(16);
  (c ?? ({ getRandomValues: (a: Uint8Array) => a.forEach((_, i) => (a[i] = Math.floor(Math.random() * 256))) } as Crypto)).getRandomValues(bytes);
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
  return 'sess_' + hex.slice(0, 8) + '-' + hex.slice(8, 12) + '-' + hex.slice(12, 16) + '-' + hex.slice(16, 20) + '-' + hex.slice(20);
};

export interface AppState {
  db: Record<string, Workspace>;
  // Per-workspace marker that loadAgents completed against the server at least
  // once — route guards hold off on "no agents" decisions until this is set so
  // a fresh load never bounces to /welcome while the fetch is in flight.
  agentsLoaded: Record<string, true>;
  pos: {
    tenantId: string;
    view: string;
    chatId: string;
    showContext: boolean;
    railExpanded?: boolean;
  };
  ui: {
    configAgent: string | null;
    cronEdit: any | null;
    wsOpen: boolean;
    toasts: any[];
    running: boolean;
  };
  search: string;

  // actions
  patchUi: (p: Partial<AppState['ui']>) => void;
  goPos: (p: Partial<AppState['pos']>) => void;
  setSearch: (q: string) => void;
  toast: (text: string, kind?: string) => void;
  
  updateTenant: (tenantId: string, fn: (t: Workspace) => Workspace) => void;
  pushMsg: (tid: string, cid: string, msg: ChatMessage) => void;
  dropMsg: (tid: string, cid: string, messageId: string) => void;
  
  selectChat: (id: string) => void;
  openMember: (id: string) => void;
  addChannelMember: (chatId: string, id: string) => void;
  removeChannelMember: (chatId: string, id: string) => void;
  
  newSession: () => void;
  switchSession: (sid: string) => void;
  bindSession: (threadId: string, sess: string) => void;
  getSessionBinding: (threadId: string) => string | null;
  ensureSessionBinding: (threadId: string) => string | null;
  recordResponse: (threadId: string, messageId: string, resp: string) => void;
  recordThreadUsage: (threadId: string, chatId: string, finalInput: number | null) => void;
  getLastResponse: (threadId: string) => string | null;
  deleteSession: (sid: string) => void;
  switchTenant: (id: string) => void;
  upsertAgent: (values: Partial<Agent>) => string;
  loadAgents: (tenantId: string) => Promise<void>;
  pollAgent: (tenantId: string, agentId: string) => void;
  regenerateAgent: (tenantId: string, agentId: string, instruction?: string) => Promise<void>;
  setAgentPromptStatus: (
    tenantId: string,
    agentId: string,
    status: 'generating' | 'ready' | 'failed',
    error?: string | null
  ) => void;
  
  runNow: (job: CronJob) => void;
  toggleCron: (job: CronJob) => void;
  deleteCron: (job: CronJob) => void;
  saveCron: (draft: Partial<CronJob>) => void;
}

const initialPos = (() => {
  const p = loadPos();
  return p && p.tenantId && p.view
    ? { railExpanded: false, ...p }
    : { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false, railExpanded: false };
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

// Boot: reattach persisted threads (session bindings, resp chains, messages)
// to the seeded tenants so a reload continues the same server sessions.
const seededDb = seedDb();
for (const [tenantId, t] of Object.entries(seededDb)) {
  seededDb[tenantId] = overlayPersistedThreads(tenantId, t);
}

export const useStore = create<AppState>((set, get) => ({
  db: seededDb,
  agentsLoaded: {},
  pos: initialPos,
  ui: {
    configAgent: null, cronEdit: null,
    wsOpen: false, toasts: [], running: false
  },
  search: '',

  patchUi: (p) => set((s: any) => ({ ui: { ...s.ui, ...p } })),
  goPos: (p) => set((s: any) => {
    const newPos = { ...s.pos, ...p };
    savePos(newPos);
    return { pos: newPos };
  }),
  setSearch: (q) => set({ search: q }),
  
  toast: (text, kind) => {
    const isNetworkToast =
      kind === 'network' ||
      text === 'Network connection failed. Please check your connection.' ||
      (kind === 'danger' && text.toLowerCase().includes('network connection failed'));
    if (isNetworkToast && useConnectionStore.getState().degraded) {
      return;
    }
    const id = uid('t');
    set((s: any) => ({ ui: { ...s.ui, toasts: [...s.ui.toasts, { id, text, kind }] } }));
    setSafeTimer(`toast-${id}`, () => {
      set((s: any) => ({ ui: { ...s.ui, toasts: s.ui.toasts.filter((t: any) => t.id !== id) } }));
    }, 3200);
  },

  updateTenant: (tenantId, fn) => set((s: any) => {
    let t = s.db[tenantId];
    if (!t) {
      const authMemberships = useAuthStore.getState().memberships;
      const mem = authMemberships.find((m) => m.workspace_id === tenantId || m.workspace_slug === tenantId);
      const wsName = mem?.workspace_name || mem?.workspace?.name || tenantId;
      const wsTz = mem?.workspace?.timezone || 'America/Los_Angeles';
      t = overlayPersistedThreads(tenantId, blankTenant({ name: wsName, sub: tenantId, tz: wsTz, starter: false }));
      t.id = tenantId;
    }
    return { db: { ...s.db, [tenantId]: fn(JSON.parse(JSON.stringify(t))) } };
  }),

  pushMsg: (tid, cid, msg) => set((s: any) => {
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

  // Removes a message from the thread's active session — used to retract the
  // optimistic empty agent message when a live turn fails before producing
  // anything (an empty row would render as a forever-loading placeholder).
  dropMsg: (tid, cid, messageId) => set((s: any) => {
    const d = JSON.parse(JSON.stringify(s.db));
    const t = d[tid];
    const th = t?.threads?.[cid];
    if (!th || Array.isArray(th)) return s;
    let dropped = false;
    th.list = th.list.map((x: any) => {
      if (dropped || !Array.isArray(x.messages)) return x;
      const next = x.messages.filter((m: any) => m.id !== messageId);
      if (next.length === x.messages.length) return x;
      dropped = true;
      return { ...x, messages: next };
    });
    return dropped ? { db: d } : s;
  }),

  selectChat: (id) => {
    const state = get();
    state.goPos({ view: 'chats', chatId: id });
    const t = state.db[state.pos.tenantId];
    if (!t) return;
    const ch = t.channels.find((c: any) => c.id === id);
    if (ch) state.goPos({ showContext: true });
    else state.goPos({ showContext: false });
    if (ch && ch.unread) {
      state.updateTenant(state.pos.tenantId, (tenant) => ({
        ...tenant,
        channels: tenant.channels.map((c: any) => (c.id === id ? { ...c, unread: 0 } : c))
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
      channels: t.channels.map((c: any) => (c.id === chatId && !(c.members || []).includes(id) ? { ...c, members: [...(c.members || (c.agentId ? [c.agentId] : [])), id] } : c))
    }));
    const t = state.db[tid];
    const channel = t.channels.find((c: any) => c.id === chatId);
    const m = t.agents.find((x: any) => x.id === id) || t.people.find((x: any) => x.id === id);
    if (channel) state.toast((m ? m.name : 'Member') + ' added to #' + channel.name);
  },

  removeChannelMember: (chatId, id) => {
    const state = get();
    const tid = state.pos.tenantId;
    const t = state.db[tid];
    const channel = t.channels.find((c: any) => c.id === chatId);
    if (!id || (channel && id === channel.agentId)) return;
    state.updateTenant(tid, (tenant) => ({
      ...tenant,
      channels: tenant.channels.map((c: any) => (c.id === chatId ? { ...c, members: (c.members || []).filter((x: any) => x !== id) } : c))
    }));
    const m = t.agents.find((x: any) => x.id === id) || t.people.find((x: any) => x.id === id);
    if (channel) state.toast((m ? m.name : 'Member') + ' removed from #' + channel.name);
  },

  newSession: () => {
    const state = get();
    const tid = state.pos.tenantId;
    const cid = state.pos.chatId;
    const t = state.db[tid];
    const target = t.agents.find((a: any) => a.id === cid) ? 'agent' : null;
    if (target !== 'agent') return;
    
    state.updateTenant(tid, (tenant) => {
      const th = tenant.threads[cid] = tenant.threads[cid] || { active: null, list: [] };
      const cur = th.list.find((x: any) => x.id === th.active);
      if (cur && cur.messages.length === 0) return tenant;
      const s = { id: uid('s'), title: 'New chat', updated: nowTime(), messages: [] };
      th.list.unshift(s);
      th.active = s.id;
      return tenant;
    });
    state.patchUi({ running: false });
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
    state.patchUi({ running: false });
  },

  bindSession: (threadId, sess) => {
    const state = get();
    state.updateTenant(state.pos.tenantId, (tenant) => {
      const th = tenant.threads[threadId];
      if (!th || Array.isArray(th)) return tenant;
      th.list = th.list.map((x: any) => (x.id === th.active ? { ...x, sess } : x));
      return tenant;
    });
  },

  getSessionBinding: (threadId) => {
    const s: any = get();
    const th = s.db[s.pos.tenantId]?.threads[threadId];
    if (!th || Array.isArray(th)) return null;
    const sess = th.list.find((x: any) => x.id === th.active);
    return sess?.sess || null;
  },

  // Lazy migration (D2/D7): on the session's next live turn, mint a
  // `sess_<uuid>` id, record it as `sess`, and replace non-`sess_` local ids
  // (legacy counter ids like `s1`). Already-bound sessions are returned as-is.
  ensureSessionBinding: (threadId) => {
    const state: any = get();
    const tid = state.pos.tenantId;
    const th = state.db[tid]?.threads[threadId];
    if (!th || Array.isArray(th)) return null;
    const sess = th.list.find((x: any) => x.id === th.active);
    if (!sess) return null;
    if (sess.sess) {
      if (!sess.id.startsWith('sess_')) {
        state.updateTenant(tid, (tenant) => {
          const t2 = tenant.threads[threadId];
          t2.list = t2.list.map((x: any) => (x.sess === sess.sess ? { ...x, id: sess.sess } : x));
          if (t2.active === sess.id) t2.active = sess.sess;
          return tenant;
        });
      }
      return sess.sess;
    }
    const minted = mintSessionId();
    state.updateTenant(tid, (tenant) => {
      const t2 = tenant.threads[threadId];
      t2.list = t2.list.map((x: any) => (x.id === sess.id ? { ...x, id: minted, sess: minted } : x));
      if (t2.active === sess.id) t2.active = minted;
      return tenant;
    });
    return minted;
  },

  recordResponse: (threadId, messageId, resp) => {
    const state = get();
    state.updateTenant(state.pos.tenantId, (tenant) => {
      const th = tenant.threads[threadId];
      if (!th || Array.isArray(th)) return tenant;
      const sess = th.list.find((x: any) => x.id === th.active);
      if (!sess) return tenant;
      sess.messages = sess.messages.map((m: any) => (m.id === messageId ? { ...m, resp } : m));
      return tenant;
    });
  },

  // Context-meter state (design D5): the latest terminal turn's final-call
  // input lives on the thread's ACTIVE session; null clears the field so the
  // meter hides instead of showing a stale/zero value. Isolation falls out of
  // addressing by (threadId, chatId).
  recordThreadUsage: (threadId, chatId, finalInput) => {
    const state = get();
    state.updateTenant(threadId, (tenant) => {
      const th = tenant.threads[chatId];
      if (!th || Array.isArray(th)) return tenant;
      const sess = th.list.find((x: any) => x.id === th.active);
      if (!sess) return tenant;
      if (finalInput === null) delete sess.usage;
      else sess.usage = { finalInput, at: nowTime() };
      return tenant;
    });
  },

  getLastResponse: (threadId) => {
    const s: any = get();
    const th = s.db[s.pos.tenantId]?.threads[threadId];
    if (!th || Array.isArray(th)) return null;
    const sess = th.list.find((x: any) => x.id === th.active);
    if (!sess) return null;
    for (let i = sess.messages.length - 1; i >= 0; i--) {
      const resp = sess.messages[i].resp;
      if (resp) return resp;
    }
    return null;
  },

  deleteSession: (sid) => {
    const state = get();
    const tid = state.pos.tenantId;
    const cid = state.pos.chatId;
    state.updateTenant(tid, (tenant) => {
      const th = tenant.threads[cid];
      if (!th || Array.isArray(th)) return tenant;
      th.list = th.list.filter((x: any) => x.id !== sid);
      if (th.list.length === 0) {
        const s = { id: uid('s'), title: 'New chat', updated: nowTime(), messages: [] };
        th.list.push(s);
        th.active = s.id;
      } else if (th.active === sid) {
        th.active = th.list[0].id;
      }
      return tenant;
    });
    state.patchUi({ running: false });
    state.toast('Session deleted');
  },

  switchTenant: (id) => {
    const state = get();
    state.patchUi({ wsOpen: false });
    if (!state.db[id]) {
      const authMemberships = useAuthStore.getState().memberships;
      const mem = authMemberships.find((m) => m.workspace_id === id || m.workspace_slug === id);
      const wsName = mem?.workspace_name || mem?.workspace?.name || id;
      const wsTz = mem?.workspace?.timezone || 'America/Los_Angeles';
      const newWs = overlayPersistedThreads(id, blankTenant({ name: wsName, sub: id, tz: wsTz, starter: false }));
      newWs.id = id;
      set((s: any) => ({
        db: {
          ...s.db,
          [id]: newWs,
        },
      }));
    }
    const targetWs = get().db[id];
    const firstAgent = targetWs?.agents?.[0]?.id || '';
    // Chat selection stays empty — the chat page renders with nothing
    // pre-opened instead of jumping to the first agent.
    state.goPos({ tenantId: id, view: 'chats', chatId: '' });
    if (targetWs?.channels) {
      state.updateTenant(id, (tenant) => ({
        ...tenant,
        channels: tenant.channels.map((c: any) =>
          c.id === firstAgent || c.agentId === firstAgent ? { ...c, unread: 0 } : c
        ),
      }));
    }
    state.toast('Switched to ' + (targetWs?.name || id));
    get().loadAgents(id);
  },

  loadAgents: async (tenantId) => {
    try {
      const res = await api.agents.list(tenantId);
      if (res?.agents) {
        set((s: any) => ({ agentsLoaded: { ...s.agentsLoaded, [tenantId]: true } }));
        get().updateTenant(tenantId, (t) => {
          const existingMap = new Map((t.agents || []).map((a) => [a.id, a]));
          const mergedAgents: Agent[] = res.agents.map((apiAgent) => {
            const existing = existingMap.get(apiAgent.id) || existingMap.get(apiAgent.slug);
            return {
              id: apiAgent.id,
              workspace_id: apiAgent.workspace_id,
              slug: apiAgent.slug,
              name: apiAgent.name,
              role: apiAgent.role,
              description: apiAgent.description,
              brief: apiAgent.brief,
              identity: apiAgent.identity,
              soul: apiAgent.soul,
              bootstrap: apiAgent.bootstrap,
              provider_id: apiAgent.provider_id,
              provider: apiAgent.provider_id,
              model: apiAgent.model,
              temp: apiAgent.temperature ?? 1.0,
              temperature: apiAgent.temperature ?? 1.0,
              max_tokens: apiAgent.max_tokens,
              effort: apiAgent.effort,
              effective_context_window: apiAgent.effective_context_window,
              summarization_trigger_tokens: apiAgent.summarization_trigger_tokens,
              autonomy: apiAgent.autonomy,
              tools: apiAgent.tools || [],
              skills: apiAgent.skills || [],
              mcp: apiAgent.mcp || [],
              avatar: apiAgent.avatar || {},
              prompts_status: apiAgent.prompts_status,
              prompts_error: apiAgent.prompts_error,
              status: existing?.status || 'idle',
              lastActive: existing?.lastActive || 'just now',
              channelPost: existing?.channelPost ?? false,
              created_by: apiAgent.created_by,
              updated_by: apiAgent.updated_by,
              created_at: apiAgent.created_at,
              updated_at: apiAgent.updated_at,
            };
          });
          return {
            ...t,
            agents: mergedAgents,
          };
        });

        res.agents.forEach((apiAgent) => {
          if (apiAgent.prompts_status === 'generating') {
            get().pollAgent(tenantId, apiAgent.id);
          }
        });
      }
    } catch {
      // offline / fallback
    }
  },

  pollAgent: (tenantId, agentId) => {
    pollAgentPromptsStatus(tenantId, agentId, {
      onUpdate: (updated) => {
        get().updateTenant(tenantId, (t) => ({
          ...t,
          agents: (t.agents || []).map((a) =>
            a.id === updated.id || a.slug === updated.slug
              ? {
                  ...a,
                  prompts_status: updated.prompts_status,
                  prompts_error: updated.prompts_error,
                  identity: updated.identity || a.identity,
                  soul: updated.soul || a.soul,
                  bootstrap: updated.bootstrap || a.bootstrap,
                }
              : a
          ),
        }));
      },
    }).catch(() => {});
  },

  regenerateAgent: async (tenantId, agentId, instruction) => {
    try {
      get().updateTenant(tenantId, (t) => ({
        ...t,
        agents: (t.agents || []).map((a) =>
          a.id === agentId || a.slug === agentId
            ? { ...a, prompts_status: 'generating', prompts_error: null }
            : a
        ),
      }));
      const res = await api.agents.regenerate(tenantId, agentId, instruction);
      if (res?.agent) {
        get().pollAgent(tenantId, res.agent.id || agentId);
      }
      get().toast('Regenerating prompts for agent…');
    } catch (err: unknown) {
      get().toast(formatApiError(err, 'Failed to regenerate prompt'), 'danger');
    }
  },

  setAgentPromptStatus: (tenantId, agentId, status, error) => {
    get().updateTenant(tenantId, (t) => ({
      ...t,
      agents: (t.agents || []).map((a) =>
        a.id === agentId || a.slug === agentId
          ? { ...a, prompts_status: status, prompts_error: error ?? null }
          : a
      ),
    }));
  },

  upsertAgent: (values) => {
    const state = get();
    const ui = state.ui;
    const tid = state.pos.tenantId;
    
    if (ui.configAgent === 'new') {
      const id = values.id || uid('a');
      state.updateTenant(tid, (t) => ({
        ...t,
        agents: [
          ...t.agents,
          {
            id,
            status: 'idle',
            lastActive: 'just now',
            prompts_status: values.prompts_status || 'generating',
            ...values,
          } as Agent,
        ],
        threads: { ...t.threads, [id]: { active: null, list: [] } }
      }));
      state.patchUi({ configAgent: null });
      state.goPos({ view: 'chats', chatId: id });
      state.toast(values.name + ' deployed — it idles until its first message');
      if (values.prompts_status === 'generating') {
        get().pollAgent(tid, id);
      }
      return id;
    } else {
      const aid = ui.configAgent;
      state.updateTenant(tid, (t) => ({
        ...t,
        agents: t.agents.map((a: any) => (a.id === aid ? { ...a, ...values } as Agent : a))
      }));
      state.patchUi({ configAgent: null });
      state.toast(values.name + ' updated — new settings apply to the next run');
      if (values.prompts_status === 'generating') {
        get().pollAgent(tid, aid || '');
      }
      return aid || '';
    }
  },

  runNow: (job) => {
    const state = get();
    const rid = 'run_' + Math.floor(1000 + Math.random() * 8999);
    const tid = state.pos.tenantId;
    state.updateTenant(tid, (t) => ({
      ...t,
      runs: [{ id: rid, agentId: job.agentId, trigger: 'manual', when: nowTime(), dur: '—', tokens: '—', status: 'running' } as any, ...t.runs]
    }));
    state.toast('Triggered “' + job.name + '”');
    
    setSafeTimer(`run-${rid}`, () => {
      get().updateTenant(tid, (t) => ({
        ...t,
        runs: t.runs.map((r: any) => (r.id === rid ? { ...r, status: 'success', dur: '7s', tokens: '1.8k' } : r))
      }));
      get().pushMsg(tid, job.agentId, {
        id: uid('m'),
        author: 'agent',
        agentId: job.agentId,
        ts: nowTime(),
        text: 'Scheduled execution for “' + job.name + '” completed successfully.',
        cron: job.id
      });
    }, 1600);
  },

  toggleCron: (job) => {
    const state = get();
    state.updateTenant(state.pos.tenantId, (t) => ({
      ...t,
      cron: t.cron.map((j: any) => (j.id === job.id ? { ...j, enabled: !j.enabled } : j))
    }));
    state.toast((job.enabled ? 'Paused “' : 'Resumed “') + job.name + '”');
  },

  deleteCron: (job) => {
    const state = get();
    state.updateTenant(state.pos.tenantId, (t) => ({
      ...t,
      cron: t.cron.filter((j: any) => j.id !== job.id)
    }));
    state.patchUi({ cronEdit: null });
    state.toast('Schedule “' + job.name + '” deleted');
  },

  saveCron: (draft) => {
    const state = get();
    if (draft.id) {
      state.updateTenant(state.pos.tenantId, (t) => ({
        ...t,
        cron: t.cron.map((j: any) => (j.id === draft.id ? { ...j, ...draft } as CronJob : j))
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
  }
}));

// Persist the threads slice (debounced) — survived `sess_<uuid>` bindings let
// a reload reattach to the same server sessions (hydration) instead of
// birthing fresh ones.
let threadPersistTimer: ReturnType<typeof setTimeout> | null = null;
useStore.subscribe(() => {
  if (threadPersistTimer) clearTimeout(threadPersistTimer);
  threadPersistTimer = setTimeout(() => persistAllThreads(useStore.getState().db), 350);
});

// Export selectors
//
// zustand v5 reads state through useSyncExternalStore, which requires every
// selector to return a referentially stable value — a selector that builds a
// fresh object/array per call makes React re-render forever ("The result of
// getSnapshot should be cached" → "Maximum update depth exceeded"). So these
// derived hooks select stable references only and build any derived object
// inside useMemo.

type ThreadState = Workspace['threads'][string];

const EMPTY_THREADS: ThreadState = { active: null, list: [] };
const EMPTY_LIST: never[] = [];

// Stable placeholder for a dangling workspace pointer (never a member, db empty).
const DEFAULT_WORKSPACE: Workspace = (() => {
  const w = blankTenant({ name: 'Workspace', sub: 'default', tz: 'America/Los_Angeles', starter: false });
  w.id = 'default';
  return w;
})();

// Placeholder workspace for an id we only know from a membership row (the real
// workspace has not been loaded into the local db cache yet).
const workspaceFromMembership = (tenantId: string, memberships: ApiMemberView[]): Workspace | null => {
  const mem = memberships.find((m) => m.workspace_id === tenantId || m.workspace_slug === tenantId);
  if (!mem) return null;
  const fallback = blankTenant({
    name: mem.workspace_name || mem.workspace?.name || tenantId,
    sub: mem.workspace_slug || tenantId,
    tz: mem.workspace?.timezone || 'America/Los_Angeles',
    starter: false,
  });
  fallback.id = mem.workspace_slug || tenantId;
  return fallback;
};

// Legacy threads were persisted as a bare message array; normalize to session shape.
const normalizeThread = (raw: unknown): ThreadState => {
  if (Array.isArray(raw)) {
    return raw.length ? { active: 's0', list: [{ id: 's0', title: 'Chat', updated: '', messages: raw }] } : EMPTY_THREADS;
  }
  return (raw as ThreadState) || EMPTY_THREADS;
};

export const useWorkspace = () => {
  const tenantId = useStore((s) => s.pos.tenantId);
  const ws = useStore((s: any) => s.db[tenantId]);
  const firstWs = useStore((s: any) => Object.values(s.db)[0]);
  const memberships = useAuthStore((s) => s.memberships);
  return useMemo(
    () => ws || workspaceFromMembership(tenantId, memberships) || firstWs || DEFAULT_WORKSPACE,
    [ws, tenantId, memberships, firstWs]
  );
};

export const useThread = (chatId: string) => {
  const raw = useStore((s: any) => s.db[s.pos.tenantId]?.threads[chatId]);
  return useMemo(() => normalizeThread(raw), [raw]);
};

export const useSessions = (chatId: string) => useStore((s: any) => {
  const th = s.db[s.pos.tenantId]?.threads[chatId];
  return th && !Array.isArray(th) ? th.list : EMPTY_LIST;
});

export const useRuns = () => useStore((s: any) => s.db[s.pos.tenantId]?.runs || EMPTY_LIST);
