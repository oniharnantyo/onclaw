import { create } from 'zustand';
import { useEffect, useMemo } from 'react';
import type { Workspace, Agent, Channel } from '../data/types';
import { seedDb, blankTenant } from "../data/seed";
import { uid, nowTime } from '../lib/helpers';
import { useAuthStore } from './auth';
import { useConnectionStore } from './connection';
import { api, pollAgentPromptsStatus, formatApiError, listAgentSessions, deleteAgentSession, ApiError, type ApiMemberView, type ApiChannel, type ApiChannelMessage, type ApiAgentSession } from '../lib/api';
import { schedulers, type Scheduler } from '../lib/schedulers';
// TurnUsageDetail (context popover, adopt-assistant-ui-elements D2/D7): the
// last terminal turn's reported input/output and optional server-provided
// context breakdown, kept alongside finalInput on the usage record.
import type { TurnUsageDetail } from '../lib/contextBreakdown';
// Namespace read for optional-at-runtime members (getToken): vitest's mock
// proxy throws when a narrow mock factory omits an export, so the session
// refetch gate reads it through the namespace inside a try/catch — the same
// tolerance as livechat.ts's bearerToken().
import * as apiModule from '../lib/api';
import type { ChannelLiveEvent } from '../lib/channelsLive';
import { overlayPersistedThreads, persistAllThreads, THREADS_KEY } from './threadPersistence';
// Panel registries (add-right-panel D1/D2): the badge watcher matches finished
// transcript tool cards against the registered candidate matchers; the tab
// dedup key and PanelTab type live there too (registry owns the shapes).
import { matchPanelCandidate, panelDedupKey, type PanelTab } from '../lib/panel/registry';

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

// Session title rule — byte-identical to the server's birth title (D2):
// first line of the input, trimmed, truncated at 42 chars with an ellipsis.
// Empty or whitespace-only input yields '' so the "New chat" fallback stays.
export const deriveSessionTitle = (text: string): string => {
  const firstLine = (text ?? '').split('\n', 1)[0] ?? '';
  const trimmed = firstLine.trim();
  return trimmed.length > 42 ? trimmed.slice(0, 42) + '…' : trimmed;
};

// ---------------------------------------------------------------------------
// Message queue (adopt-assistant-ui-elements 8.1, design D9): sends that land
// while a run is active in an agent chat hold here, in order, until the run's
// terminal event dispatches the first one. Per-(workspace, chat) local state —
// cancelable rows, never persisted, never synced.
// ---------------------------------------------------------------------------

export interface QueuedMessage {
  id: string;
  text: string;
  /** Ready attachment references captured at enqueue time (add-chat-attachments
   * D11) — they ride the turn when the entry dispatches. */
  attachments?: ChatAttachment[];
  enqueuedAt: string;
}

const queueKey = (tenantId: string, chatId: string): string => `${tenantId}::${chatId}`;

export interface AppState {
  db: Record<string, Workspace>;
  // Per-workspace marker that loadAgents completed against the server at least
  // once — route guards hold off on "no agents" decisions until this is set so
  // a fresh load never bounces to /welcome while the fetch is in flight.
  agentsLoaded: Record<string, true>;
  // Per-workspace marker that loadChannels completed (integrate-agent-channels):
  // channels are server-only (never seeded), so this gates "no channels yet"
  // decisions the same way agentsLoaded does.
  channelsLoaded: Record<string, true>;
  pos: {
    tenantId: string;
    view: string;
    chatId: string;
    showContext: boolean;
    railExpanded?: boolean;
  };
  /** Right panel slice (add-right-panel D1): per-chat, ephemeral — a chat
   * change resets it (goPos) and nothing persists it. `badge` is the header
   * toggle's dot: set ONLY by a panel-able tool finishing while the panel is
   * closed, cleared on open. The panel never opens or mutates tabs from a
   * tool event — every tab arrives through openPanelTab (a user click). */
  panel: {
    open: boolean;
    tabs: PanelTab[];
    activeId: string | null;
    badge: boolean;
  };
  ui: {
    configAgent: string | null;
    /** 'new' for a blank draft, a Scheduler row for edits, null when closed. */
    scheduleEdit: any | null;
    wsOpen: boolean;
    toasts: any[];
    running: boolean;
    // A /compact turn is in flight (chat-compact-command): the transcript
    // shows the "Compacting context…" status row instead of the thinking row.
    compacting?: boolean;
  };
  search: string;
  /** Ordered queued messages per `tenantId::chatId` (message queue 8.1) —
   * entries land while a run is active and dispatch first-in-first-out from
   * the run's terminal path. Ephemeral local state; nothing reads this slice
   * outside the queue actions, the ChatView stack, and the runtime dispatch. */
  messageQueue: Record<string, QueuedMessage[]>;

  // actions
  patchUi: (p: Partial<AppState['ui']>) => void;
  goPos: (p: Partial<AppState['pos']>) => void;
  setSearch: (q: string) => void;
  toast: (text: string, kind?: string) => void;

  /** Opens (or focuses — dedup key is kind + payload identity) one panel tab
   * and shows the panel. The only path a tab can enter the panel. */
  openPanelTab: (entry: { kind: string; title: string; payload: Record<string, unknown> }) => void;
  /** Focuses an existing tab; unknown ids are ignored. */
  focusPanelTab: (id: string) => void;
  /** Closes one tab; closing the last one closes the panel. */
  closePanelTab: (id: string) => void;
  /** Header toggle: show/hide the panel. Opening clears the dot badge. */
  setPanelOpen: (open: boolean) => void;

  /** Appends a message to the chat's queue and resolves the entry id. */
  enqueueChatMessage: (tenantId: string, chatId: string, text: string, attachments?: ChatAttachment[]) => string;
  /** Removes exactly one queued entry by id — cancelling it never touches
   * the other rows (spec: "Removing a queued entry SHALL cancel only that
   * entry"). */
  removeQueuedChatMessage: (tenantId: string, chatId: string, id: string) => void;
  /** Pops the first queued entry for the chat — the auto-dispatch primitive
   * (8.3). Resolves null when nothing is queued. */
  dequeueChatMessage: (tenantId: string, chatId: string) => QueuedMessage | null;
  
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
  recordThreadUsage: (threadId: string, chatId: string, finalInput: number | null, turn?: TurnUsageDetail) => void;
  getLastResponse: (threadId: string) => string | null;
  deleteSession: (sid: string) => void;
  /** Refetches the server session index for one agent chat and reconciles it
   * into the thread list (agent-session-index D4). Triggers are coalesced:
   * calls inside ~500ms collapse into a single fetch. */
  refetchAgentSessions: (tenantId: string, agentChatId: string) => Promise<void>;
  /** Opens a scheduler run's transcript inside its agent's chat
   * (integrate-scheduler 7.4): injects (or reuses) a session entry addressed
   * by the run's session id and makes it active. The run's session belongs to
   * the scheduler's agent, so the caller passes that agent's chat id. */
  openRunSession: (chatId: string, sessionId: string, title?: string, schedulerName?: string, langfuseUrl?: string) => void;
  /** Live schedules for the workspace (integrate-scheduler 7.2): replaces
   * tenant.schedules with the wire rows. Resolves false on failure so the
   * screen can render its error state. */
  loadSchedules: (tenantId: string) => Promise<boolean>;
  switchTenant: (id: string) => void;
  upsertAgent: (values: Partial<Agent>) => string;
  loadAgents: (tenantId: string) => Promise<void>;
  loadChannels: (tenantId: string) => Promise<void>;
  loadChannelRoster: (tenantId: string, channelId: string) => Promise<void>;
  /** Loads the channel feed over REST (cursor list); resolves to the last
   * seen seq — the `lastSeq` the live stream subscribes with (design D13). */
  loadChannelFeed: (tenantId: string, channelId: string, opts?: { after?: number; limit?: number }) => Promise<number>;
  /** Folds one live channel event into the store; message_posted appends the
   * wire row seq-deduped, the summon/run lifecycle events are no-ops here
   * (room-UI concerns, task 8.1). */
  applyChannelEvent: (tenantId: string, channelId: string, ev: ChannelLiveEvent) => void;
  pollAgent: (tenantId: string, agentId: string) => void;
  regenerateAgent: (tenantId: string, agentId: string, instruction?: string) => Promise<void>;
  setAgentPromptStatus: (
    tenantId: string,
    agentId: string,
    status: 'generating' | 'ready' | 'failed',
    error?: string | null
  ) => void;
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

// Wire → display mapping for the sidebar/room channel view (integrate-agent-
// channels). `name` carries the slug so the sidebar and room header keep the
// mock's `#handle` visual (design D6: the slug IS the #handle); unread starts
// at 0 — live channels have no seeded backlog to badge.
const channelFromApi = (c: ApiChannel): Channel => ({
  id: c.id,
  workspace_id: c.workspace_id,
  name: c.slug,
  slug: c.slug,
  purpose: c.purpose,
  conventions: c.conventions,
  created_at: c.created_at,
  updated_at: c.updated_at,
  unread: 0,
  members: [],
});

// Boot: reattach persisted threads (session bindings, resp chains, messages)
// to the seeded tenants so a reload continues the same server sessions.
const seededDb = seedDb();
for (const [tenantId, t] of Object.entries(seededDb)) {
  seededDb[tenantId] = overlayPersistedThreads(tenantId, t);
}

export const useStore = create<AppState>((set, get) => ({
  db: seededDb,
  agentsLoaded: {},
  channelsLoaded: {},
  pos: initialPos,
  panel: { open: false, tabs: [], activeId: null, badge: false },
  ui: {
    configAgent: null, scheduleEdit: null,
    wsOpen: false, toasts: [], running: false
  },
  search: '',
  messageQueue: {},

  // Ordered append (message queue 8.1): a fresh array per write keeps the
  // useMessageQueue selector referentially stable between changes.
  enqueueChatMessage: (tenantId, chatId, text, attachments) => {
    const id = uid('q');
    set((s: any) => {
      const key = queueKey(tenantId, chatId);
      const entry: QueuedMessage = {
        id,
        text,
        ...(attachments && attachments.length ? { attachments } : {}),
        enqueuedAt: new Date().toISOString(),
      };
      return { messageQueue: { ...s.messageQueue, [key]: [...(s.messageQueue[key] || []), entry] } };
    });
    return id;
  },

  removeQueuedChatMessage: (tenantId, chatId, id) => set((s: any) => {
    const key = queueKey(tenantId, chatId);
    const items = s.messageQueue[key];
    if (!items || !items.some((q: QueuedMessage) => q.id === id)) return s;
    return { messageQueue: { ...s.messageQueue, [key]: items.filter((q: QueuedMessage) => q.id !== id) } };
  }),

  dequeueChatMessage: (tenantId, chatId) => {
    const key = queueKey(tenantId, chatId);
    const items = get().messageQueue[key];
    if (!items || items.length === 0) return null;
    const [head] = items;
    set((s: any) => ({ messageQueue: { ...s.messageQueue, [key]: items.slice(1) } }));
    return head;
  },

  patchUi: (p) => {
    const wasRunning = get().ui.running;
    set((s: any) => ({ ui: { ...s.ui, ...p } }));
    // Turn-terminal refetch trigger (agent-session-index D4): `running`
    // true→false is the single choke point every terminal path funnels
    // through (live bridge EOF, stream error, cancel, compact) — schedule
    // one coalesced session-list refresh for the open chat.
    if (wasRunning && p.running === false) {
      const { tenantId, chatId } = get().pos;
      if (chatId) void get().refetchAgentSessions(tenantId, chatId);
    }
  },
  goPos: (p) => set((s: any) => {
    const newPos = { ...s.pos, ...p };
    savePos(newPos);
    // Panel reset (add-right-panel D1): tabs are run artifacts and belong to
    // the chat that opened them — a chat identity change starts the panel
    // clean; tabs from the previous chat never carry over.
    const chatChanged = p.chatId !== undefined && p.chatId !== s.pos.chatId;
    return chatChanged
      ? { pos: newPos, panel: { open: false, tabs: [], activeId: null, badge: false } }
      : { pos: newPos };
  }),
  setSearch: (q) => set({ search: q }),

  // Panel slice actions (add-right-panel D1/D7). openPanelTab dedups on
  // kind + stable payload identity: an artifact that already has a tab is
  // focused, never duplicated. Every open clears the badge — the user is
  // looking at the panel now.
  openPanelTab: (entry) => set((s: any) => {
    const dedupKey = panelDedupKey(entry.kind, entry.payload);
    const existing = s.panel.tabs.find((t: PanelTab) => t.dedupKey === dedupKey);
    if (existing) {
      return { panel: { ...s.panel, open: true, badge: false, activeId: existing.id } };
    }
    const tab: PanelTab = { id: uid('tab'), kind: entry.kind, title: entry.title, payload: entry.payload, dedupKey };
    return { panel: { open: true, badge: false, activeId: tab.id, tabs: [...s.panel.tabs, tab] } };
  }),

  focusPanelTab: (id) => set((s: any) =>
    s.panel.tabs.some((t: PanelTab) => t.id === id)
      ? { panel: { ...s.panel, activeId: id } }
      : s
  ),

  closePanelTab: (id) => set((s: any) => {
    const tabs = s.panel.tabs.filter((t: PanelTab) => t.id !== id);
    if (tabs.length === s.panel.tabs.length) return s;
    // Last tab closed closes the panel (spec); otherwise focus falls to the
    // nearest remaining tab when the active one went away.
    if (tabs.length === 0) return { panel: { open: false, tabs, activeId: null, badge: false } };
    const activeId = s.panel.activeId === id ? tabs[tabs.length - 1].id : s.panel.activeId;
    return { panel: { ...s.panel, tabs, activeId } };
  }),

  setPanelOpen: (open) => set((s: any) => ({
    panel: { ...s.panel, open, badge: open ? false : s.panel.badge },
  })),
  
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

  pushMsg: (tid, cid, msg) => {
    set((s: any) => {
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
      // Optimistic title (agent-session-index spec): first user message titles
      // the session immediately with the SAME rule the server applies at birth
      // (first line, trimmed, ≤42 chars + ellipsis) so the two agree by
      // construction; empty input leaves the "New chat" fallback.
      if (msg.author === 'you' && (sess.title === 'New chat' || !sess.title)) {
        const derived = deriveSessionTitle(msg.text);
        if (derived) sess.title = derived;
      }
      sess.updated = 'just now';
      return { db: d };
    });
    persistAllThreads(get().db);
  },

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
    // Channel entry keeps its members surface (add-right-panel 1.5): the
    // members tab opens (or focuses — dedup keeps re-selects stable) instead
    // of the retired showContext flag. Non-channels open no members tab.
    if (ch) state.openPanelTab({ kind: 'members', title: 'Members', payload: { chatId: id } });
    if (ch && ch.unread) {
      state.updateTenant(state.pos.tenantId, (tenant) => ({
        ...tenant,
        channels: tenant.channels.map((c: any) => (c.id === id ? { ...c, unread: 0 } : c))
      }));
    }
    // Agent chat open (D4): paint from the local overlay, then reconcile the
    // server session index. Coalesces with the loadAgents-gate trigger.
    if (t.agents.some((a: any) => a.id === id || a.slug === id)) {
      void state.refetchAgentSessions(state.pos.tenantId, id);
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
    // Only a server-bound interactive id (`sess_`/`sched_`) is a binding. A
    // stale non-bound value (e.g. a leaked `chan_` room id from before the
    // index filtered them) must not receive this thread's next live turn —
    // fall through and mint a fresh session for it instead.
    if (sess.sess && (sess.sess.startsWith('sess_') || sess.sess.startsWith('sched_'))) {
      if (!sess.id.startsWith('sess_')) {
        state.updateTenant(tid, (tenant: any) => {
          const t2 = tenant.threads[threadId];
          t2.list = t2.list.map((x: any) => (x.sess === sess.sess ? { ...x, id: sess.sess } : x));
          if (t2.active === sess.id) t2.active = sess.sess;
          return tenant;
        });
        persistAllThreads(get().db);
      }
      return sess.sess;
    }
    const minted = mintSessionId();
    state.updateTenant(tid, (tenant: any) => {
      const t2 = tenant.threads[threadId];
      t2.list = t2.list.map((x: any) => (x.id === sess.id ? { ...x, id: minted, sess: minted } : x));
      if (t2.active === sess.id) t2.active = minted;
      return tenant;
    });
    persistAllThreads(get().db);
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
  // addressing by (threadId, chatId). The optional turn block carries the
  // last call's input/output and any server-provided context breakdown for
  // the context popover's detail rows (assistant-ui context-display
  // adoption) — each field present only when the wire reported it.
  recordThreadUsage: (threadId, chatId, finalInput, turn) => {
    const state = get();
    state.updateTenant(threadId, (tenant) => {
      const th = tenant.threads[chatId];
      if (!th || Array.isArray(th)) return tenant;
      const sess = th.list.find((x: any) => x.id === th.active);
      if (!sess) return tenant;
      if (finalInput === null) delete sess.usage;
      else sess.usage = {
        finalInput,
        ...(turn?.input ? { input: turn.input } : {}),
        ...(turn?.output ? { output: turn.output } : {}),
        ...(turn?.contextBreakdown ? { contextBreakdown: turn.contextBreakdown } : {}),
        at: nowTime(),
      };
      return tenant;
    });
  },

  getLastResponse: (threadId) => {
    const s: any = get();
    const th = s.db[s.pos.tenantId]?.threads[threadId];
    if (!th || Array.isArray(th)) return null;
    const sess = th.list.find((x: any) => x.id === th.active);
    if (!sess) return null;
    // Never chain off a session whose binding is not a server-bound
    // interactive id — a leaked `chan_` room id would otherwise pull every
    // later turn of this thread back into the room session.
    if (sess.sess && !(sess.sess.startsWith('sess_') || sess.sess.startsWith('sched_'))) return null;
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
    const th: any = state.db[tid]?.threads?.[cid];
    const target = th && !Array.isArray(th) ? th.list.find((x: any) => x.id === sid) : null;
    // The session's server address: the sess binding, or the id itself once
    // ensureSessionBinding migrated it. Purely-local sessions (never sent)
    // have no server row — local removal is the whole job for them.
    const address =
      typeof target?.sess === 'string' ? target.sess
      : typeof target?.id === 'string' && target.id.startsWith('sess_') ? target.id
      : null;
    // Server first (D6): soft-delete the index row, then remove locally. Any
    // other failure aborts the delete — the sidebar must never disagree with
    // the index. 404 means the server has no row for this binding (pre-index
    // history, no backfill): nothing to soft-delete, so removal completes.
    let serverDelete: Promise<void>;
    try {
      serverDelete = address
        ? deleteAgentSession(tid, cid, address).then(
            () => {},
            (err: unknown) => {
              if (err instanceof ApiError && err.status === 404) return;
              throw err;
            }
          )
        : Promise.resolve();
    } catch (err) {
      // narrow api mocks export no deleteAgentSession — same abort path as a
      // failed request
      serverDelete = Promise.reject(err);
    }
    serverDelete
      .then(() => {
        get().updateTenant(tid, (tenant) => {
          const th2 = tenant.threads[cid];
          if (!th2 || Array.isArray(th2)) return tenant;
          th2.list = th2.list.filter((x: any) => x.id !== sid);
          if (th2.list.length === 0) {
            const s = { id: uid('s'), title: 'New chat', updated: nowTime(), messages: [] };
            th2.list.push(s);
            th2.active = s.id;
          } else if (th2.active === sid) {
            th2.active = th2.list[0].id;
          }
          return tenant;
        });
        get().patchUi({ running: false });
        get().toast('Session deleted');
      })
      .catch((err: unknown) => {
        get().toast(formatApiError(err, 'Failed to delete session'), 'danger');
      });
  },

  // Server session index for one agent chat (agent-session-index D4). No
  // token (nobody signed in) or non-agent chat (channels/people) — there is
  // no index to reconcile; resolve without a fetch.
  refetchAgentSessions: (tenantId, agentChatId) => {
    let token: string | null = null;
    try {
      token = (apiModule as any).getToken?.() ?? null;
    } catch {
      // narrow api mocks (runtime.test) export no getToken — degrade to
      // unauthenticated instead of throwing (namespace proxy throws on reads
      // of omitted exports).
    }
    if (!token || !isAgentChat(tenantId, agentChatId)) return Promise.resolve();
    return scheduleSessionRefetch(tenantId, agentChatId);
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
    get().loadChannels(id);
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
              provider_id: apiAgent.provider_id,
              provider: apiAgent.provider_id,
              model: apiAgent.model,
              temp: apiAgent.temperature ?? 1.0,
              temperature: apiAgent.temperature ?? 1.0,
              max_tokens: apiAgent.max_tokens,
              effort: apiAgent.effort,
              effective_context_window: apiAgent.effective_context_window,
              summarization_trigger_tokens: apiAgent.summarization_trigger_tokens,
              input_modalities: apiAgent.input_modalities,
              autonomy: apiAgent.autonomy,
              disabled_tools: apiAgent.disabled_tools || [],
              skills: apiAgent.skills || [],
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

        // agentsLoaded gate (D4): if an agent chat is already open (restored
        // position, deep link), reconcile its session index now that the
        // agent roster landed. Coalesces with the selectChat trigger.
        const pos = get().pos;
        if (pos.tenantId === tenantId && pos.chatId && res.agents.some((a) => a.id === pos.chatId || a.slug === pos.chatId)) {
          void get().refetchAgentSessions(tenantId, pos.chatId);
        }
      }
    } catch {
      // offline / fallback
    }
  },

  // Channel hydration (integrate-agent-channels): channels are server-only —
  // the seeded lists left the data path, so this replaces the tenant's
  // channels with the workspace's real rows (the sidebar's data source).
  // A failed load still sets the marker: with no seeds behind it, the sidebar
  // empty state and any "no channels yet" decision must converge even when
  // the endpoint answers 404 (old backend) instead of hanging guards.
  loadChannels: async (tenantId) => {
    set((s: any) => ({ channelsLoaded: { ...s.channelsLoaded, [tenantId]: true } }));
    try {
      const res = await api.channels.list(tenantId);
      const channels = res?.channels || [];
      get().updateTenant(tenantId, (t) => ({
        ...t,
        channels: channels.map(channelFromApi),
      }));
    } catch {
      // offline / old backend: zero channels, marker already set
    }
  },

  // Wire roster for one channel: rows stored verbatim under channelRoster;
  // the display channel's `members` gets the referenced user/agent ids so the
  // existing room member panel keeps resolving against agents/people.
  loadChannelRoster: async (tenantId, channelId) => {
    try {
      const res = await api.channels.members.list(tenantId, channelId);
      const members = res?.members || [];
      get().updateTenant(tenantId, (t) => ({
        ...t,
        channelRoster: { ...(t.channelRoster || {}), [channelId]: members },
        channels: (t.channels || []).map((c) =>
          c.id === channelId
            ? { ...c, members: members.map((m) => (m.member_type === 'agent' ? m.agent_id : m.user_id)).filter((x): x is string => Boolean(x)) }
            : c
        ),
      }));
    } catch {
      // transient: the roster stays whatever the last load produced
    }
  },

  // REST feed load (design D13: REST first, then the live stream). Stores the
  // wire rows ascending by seq and resolves the last seen seq — the cursor
  // the room's SSE subscription dedups against. A failed load resolves 0 so
  // the stream still attaches; dedup is best-effort by design.
  loadChannelFeed: async (tenantId, channelId, opts) => {
    try {
      const res = await api.channels.messages.list(tenantId, channelId, opts);
      const messages = res?.messages || [];
      get().updateTenant(tenantId, (t) => ({
        ...t,
        channelMessages: { ...(t.channelMessages || {}), [channelId]: messages },
      }));
      return messages.length ? messages[messages.length - 1].seq : opts?.after ?? 0;
    } catch {
      return 0;
    }
  },

  applyChannelEvent: (tenantId, channelId, ev) => {
    if (!ev || typeof ev.type !== 'string') return;
    // Only message_posted mutates store data today — summon_considering /
    // summon_decided / run_started / run_finished drive room-UI state that is
    // gated behind the ASCII gallery (task 8.1) and stay with the subscriber.
    if (ev.type !== 'message_posted') return;
    const msg = ev.payload as ApiChannelMessage;
    if (!msg || typeof msg.seq !== 'number') return;
    get().updateTenant(tenantId, (t) => {
      const existing = (t.channelMessages || {})[channelId] || [];
      // Seq-keyed dedup: the stream can overlap the REST catch-up window.
      if (existing.some((m) => m.seq === msg.seq)) return t;
      return {
        ...t,
        channelMessages: {
          ...(t.channelMessages || {}),
          [channelId]: [...existing, msg].sort((a, b) => a.seq - b.seq),
        },
      };
    });
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

  // Live schedules (integrate-scheduler 7.2): the workspace's wire rows are
  // the single source the schedules screen and the sidebar counts render from.
  // Mirrors loadChannels' tolerance: a failed load keeps the last good list
  // and resolves false so the screen can show its error state.
  loadSchedules: async (tenantId) => {
    try {
      const res = await schedulers.list(tenantId);
      const rows: Scheduler[] = res?.schedulers || [];
      get().updateTenant(tenantId, (t) => ({ ...t, schedules: rows }));
      return true;
    } catch {
      return false;
    }
  },

  // Run transcript pickup (7.4): the runs table navigates to the run's agent
  // chat with the run's session address; this injects the matching session
  // entry (server Born sessions are never in the agent-session index by
  // design, D7) and points the thread at it. The ChatRoute hydration effect
  // takes over from there — sched_ ids hydrate like sess_ ids.
  openRunSession: (chatId, sessionId, title, schedulerName, langfuseUrl) => {
    const state = get();
    const tid = state.pos.tenantId;
    state.updateTenant(tid, (tenant: any) => {
      const raw = tenant.threads[chatId];
      const th = raw && !Array.isArray(raw) ? raw : (tenant.threads[chatId] = { active: null, list: [] });
      let sess = th.list.find((x: any) => x.id === sessionId);
      if (!sess) {
        sess = {
          id: sessionId,
          sess: sessionId,
          title: title || 'Scheduled run',
          updated: '',
          messages: [],
          // Marks where the transcript's origin chip name came from; the
          // hydration path stamps messages that lack a tag of their own.
          ...(schedulerName !== undefined ? { schedulerName } : {}),
          // Observability deep link (integrate-langfuse-tracing D6): the
          // transcript header offers "Open in Langfuse" only when present.
          ...(langfuseUrl ? { langfuseUrl } : {}),
        };
        th.list.unshift(sess);
      }
      th.active = sessionId;
      return tenant;
    });
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

// ---------------------------------------------------------------------------
// Panel badge watcher (add-right-panel, spec "Tool-card panel affordances").
// Derives from the FOLDED transcript tool cards (`agent.tools[]` — the same
// data live turns and hydrated history carry), never from raw live events, so
// the affordance data is identical on either path. When a panel-able tool
// call FINISHES while the panel is closed, the header toggle gets a dot
// badge — and that is ALL this watcher may do: it never sets `open` and never
// touches `tabs[]`. The badge clears when the panel opens (setPanelOpen).
// ---------------------------------------------------------------------------

// Watch state: which chat's active session the signature set belongs to. A
// chat switch (or boot) re-baselines — the artifacts already sitting in a
// transcript when you arrive are history, not finishes.
let panelWatchKey: string | null = null;
let panelWatchSigs = new Set<string>();

useStore.subscribe((s: any) => {
  const { tenantId, chatId } = s.pos;
  const key = chatId ? `${tenantId}::${chatId}` : null;
  if (!key) {
    panelWatchKey = null;
    panelWatchSigs = new Set();
    return;
  }
  const th = s.db[tenantId]?.threads?.[chatId];
  const sess = th && !Array.isArray(th) ? th.list?.find((x: any) => x.id === th.active) : null;
  const sigs = new Set<string>();
  for (const m of sess?.messages || []) {
    for (const card of m?.tools || []) {
      // Finished means the fold has a result or an error — a mid-stream card
      // (no res/error yet) has produced nothing to open.
      if (!card || (card.res === undefined && card.error === undefined)) continue;
      const c = matchPanelCandidate(card);
      if (c) sigs.add(panelDedupKey(c.kind, c.payload));
    }
  }
  const switched = panelWatchKey !== key;
  const appeared = !switched && [...sigs].some((k) => !panelWatchSigs.has(k));
  panelWatchKey = key;
  panelWatchSigs = sigs;
  if (appeared && !s.panel.open && s.ui.running) {
    useStore.setState({ panel: { ...s.panel, badge: true } });
  }
});

// ---------------------------------------------------------------------------
// Agent session index (change agent-session-index, D4): the server list is
// authoritative for the per-agent sidebar index — order (last activity,
// newest first), titles, and the running flag. localStorage stays the
// immediate paint plus the archive for pre-index history.
// ---------------------------------------------------------------------------

// Coarse activity stamp for server rows the local overlay has never seen.
const relativeActivity = (iso?: string): string => {
  const t = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(t)) return 'just now';
  const mins = Math.floor((Date.now() - t) / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return mins + 'm ago';
  const hours = Math.floor(mins / 60);
  if (hours < 24) return hours + 'h ago';
  return Math.floor(hours / 24) + 'd ago';
};

// Server title wins only for the "New chat" fallback (and truly empty
// titles) and for server-confirmed rows; a fresh optimistic title from a
// session the server has not listed yet stays put — the rules agree by
// construction (deriveSessionTitle), so this only guards against clobbering
// with a stale or divergent payload.
const mergeTitle = (local: any, row: ApiAgentSession): string => {
  const serverTitle = row.title || '';
  const localTitle = typeof local.title === 'string' ? local.title : '';
  const serverWins = !localTitle || localTitle === 'New chat' || Boolean(local.serverBorn);
  return serverWins && serverTitle ? serverTitle : localTitle;
};

// Does a fresh reconciliation change anything the sidebar shows? Guards
// against pointless re-renders/persists on every trigger.
const sameIndex = (a: any[], b: any[]): boolean =>
  a.length === b.length &&
  a.every((x: any, i: number) => {
    const y = b[i];
    return x.id === y.id && x.title === y.title && Boolean(x.running) === Boolean(y.running);
  });

// Folds one server listing into the thread state: server rows first (the API
// delivers them last-activity-first), matched to local entries by the sess
// binding (or the migrated id). Purely-local sessions (pre-index history)
// keep their place below the server rows; a previously server-confirmed row
// that the list no longer returns was soft-deleted server-side and is
// dropped. The active pointer follows the session, not the slot, and the
// last-session rule applies (a chat never has zero sessions).
const applySessionIndex = (tenantId: string, chatId: string, rows: ApiAgentSession[]) => {
  useStore.getState().updateTenant(tenantId, (tenant: any) => {
    const raw = tenant.threads?.[chatId];
    const th = raw && !Array.isArray(raw) ? raw : { active: null, list: [] };
    const locals: any[] = Array.isArray(th.list) ? th.list.filter((x: any) => x && typeof x === 'object') : [];
    const consumed = new Set<any>();
    const serverEntries: any[] = [];

    for (const row of rows || []) {
      const sid = row?.session_id;
      if (!sid) continue;
      // Channel-room sessions (`chan_<channelID>_<agentID>`) belong to the
      // channel surface; listed here only because channel participation lives
      // in the same agent_sessions table. Adopting one would let it hijack
      // the active pointer on a fresh load and receive the direct chat's next
      // live turn (its binding is server-bound), leaking private turns into
      // the room transcript — so the direct-chat thread never adopts them.
      if (sid.startsWith('chan_')) continue;
      const local = locals.find((x: any) => !consumed.has(x) && (x.sess === sid || x.id === sid));
      if (!local) {
        // Server-only row (fresh browser / another device): an empty local
        // entry — selecting it hydrates the transcript from the events
        // endpoint via the sess binding.
        serverEntries.push({
          id: sid,
          sess: sid,
          title: row.title || '',
          updated: relativeActivity(row.last_active_at),
          lastActiveAt: row.last_active_at,
          running: Boolean(row.running),
          serverBorn: true,
          messages: [],
        });
        continue;
      }
      consumed.add(local);
      serverEntries.push({
        ...local,
        serverBorn: true,
        running: Boolean(row.running),
        title: mergeTitle(local, row),
        updated: local.updated || relativeActivity(row.last_active_at),
        lastActiveAt: row.last_active_at || local.lastActiveAt,
      });
    }

    const kept = locals.filter((x: any) => !consumed.has(x) && !x.serverBorn);
    const nextList = [...serverEntries, ...kept];

    let active = typeof th.active === 'string' ? th.active : null;
    if (!active || !nextList.some((x: any) => x.id === active)) {
      active = nextList[0]?.id ?? null;
    }

    if (!nextList.length) {
      const fresh = { id: uid('s'), title: 'New chat', updated: nowTime(), messages: [] };
      return { ...tenant, threads: { ...tenant.threads, [chatId]: { active: fresh.id, list: [fresh] } } };
    }

    if (active === th.active && Array.isArray(th.list) && sameIndex(th.list, nextList)) return tenant;
    return { ...tenant, threads: { ...tenant.threads, [chatId]: { ...th, active, list: nextList } } };
  });
};

// One debounced, coalesced fetch per (tenant, chat): triggers inside the
// window collapse into a single request; a trigger landing while a fetch is
// in flight schedules exactly one trailing refetch. Every caller's promise
// resolves when the fetch that satisfies it completes.
const REFETCH_DEBOUNCE_MS = 500;
interface RefetchSlot { timer: ReturnType<typeof setTimeout> | null; inflight: Promise<void> | null; resolvers: Array<() => void>; }
const refetchSlots = new Map<string, RefetchSlot>();
const refetchKey = (tenantId: string, chatId: string) => `${tenantId}::${chatId}`;

const fetchSessionIndex = async (tenantId: string, chatId: string): Promise<void> => {
  try {
    const res = await listAgentSessions(tenantId, chatId);
    applySessionIndex(tenantId, chatId, res?.sessions || []);
  } catch {
    // offline / old backend / transient — the local overlay remains the
    // visible truth until a listing succeeds.
  }
};

const scheduleSessionRefetch = (tenantId: string, chatId: string): Promise<void> => {
  const key = refetchKey(tenantId, chatId);
  const slot: RefetchSlot = refetchSlots.get(key) || { timer: null, inflight: null, resolvers: [] };
  refetchSlots.set(key, slot);
  if (slot.timer) clearTimeout(slot.timer);
  const run = async (): Promise<void> => {
    // Claim the resolvers registered before this run started — triggers that
    // land mid-run belong to the NEXT run (their own trailing fetch).
    const mine = slot.resolvers.splice(0);
    if (slot.inflight) await slot.inflight; // trailing refetch: trigger landed mid-fetch
    const fetch = fetchSessionIndex(tenantId, chatId);
    slot.inflight = fetch.finally(() => { slot.inflight = null; });
    await fetch; // fetchSessionIndex never rejects
    mine.forEach((resolve) => resolve());
  };
  return new Promise<void>((resolve) => {
    slot.resolvers.push(resolve);
    slot.timer = setTimeout(() => {
      slot.timer = null;
      void run();
    }, REFETCH_DEBOUNCE_MS);
  });
};

const isAgentChat = (tenantId: string, chatId: string): boolean => {
  const t: any = useStore.getState().db[tenantId];
  return Boolean(chatId && t?.agents?.some((a: any) => a.id === chatId || a.slug === chatId));
};

// Refetch triggers (D4): window focus, visibilitychange, and the `storage`
// event other tabs fire when they write onclaw.threads.v1. Installed once per
// document — the window-keyed flag keeps vi.resetModules() re-imports from
// stacking duplicate listeners.
const triggerOpenAgentSessionRefetch = () => {
  const { tenantId, chatId } = useStore.getState().pos;
  if (chatId) void useStore.getState().refetchAgentSessions(tenantId, chatId);
};

export const installSessionRefetchListeners = (): void => {
  if (typeof window === 'undefined' || typeof document === 'undefined') return;
  const w = window as any;
  if (w.__onclawSessionRefetchListeners) return;
  w.__onclawSessionRefetchListeners = true;
  window.addEventListener('focus', triggerOpenAgentSessionRefetch);
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') triggerOpenAgentSessionRefetch();
  });
  // The payload carries every tenant the writing tab persisted — refetch the
  // open chat of an affected tenant (or when the payload is unreadable).
  window.addEventListener('storage', (e: StorageEvent) => {
    if (e.key !== THREADS_KEY) return;
    let affected = new Set<string>();
    try {
      const parsed = JSON.parse(e.newValue || '{}');
      if (parsed && typeof parsed === 'object') affected = new Set(Object.keys(parsed));
    } catch {
      // malformed payload — treat as "unknown tenants" and just trigger
    }
    const { tenantId, chatId } = useStore.getState().pos;
    if (chatId && (affected.size === 0 || affected.has(tenantId))) {
      void useStore.getState().refetchAgentSessions(tenantId, chatId);
    }
  });
};
installSessionRefetchListeners();

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

// Message queue rows for one chat (8.1/8.2). Stable EMPTY_QUEUE keeps the
// zustand v5 selector contract (no fresh array per call → no render loop).
const EMPTY_QUEUE: QueuedMessage[] = [];

export const useMessageQueue = (tenantId: string, chatId: string): QueuedMessage[] =>
  useStore((s) => s.messageQueue[`${tenantId}::${chatId}`] || EMPTY_QUEUE);

export const useRuns = () => useStore((s: any) => s.db[s.pos.tenantId]?.runs || EMPTY_LIST);
