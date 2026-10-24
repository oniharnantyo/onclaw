/**
 * Chat feature store (tasks 9.1–9.4): per-agent session selection, the
 * transcript (durable replay reconciled with the live event stream), the
 * context usage, and the ordered local send queue.
 *
 * Turn lifecycle (design D7): `send` marks the store busy synchronously, so a
 * second send while a turn is in flight (or while the first is still being
 * dispatched) joins the FIFO `queue` instead of racing the backend's
 * one-turn-at-a-time lock. Queue draining happens exactly at turn terminals
 * (turn_completed / turn_error / turn_cancelled events, or a failed dispatch)
 * and dispatches one message at a time through the same path, preserving send
 * order.
 *
 * Session open order — subscribe first, then fetch the newest transcript
 * page (task 6.1, design D6), then read context usage — plus the event-id
 * dedupe in entries.ts makes the durable and live views converge without
 * duplicate rows. Older history pages on demand through `loadEarlier`, which
 * folds the older page through the same applyEvent and prepends it.
 *
 * Session subscriptions persist across session switches (session-row running
 * state, spec chat-view "Sessions organized per agent"): a turn in flight in
 * a background session keeps its rail indicator until a terminal event
 * retires it, so the per-session running set folds terminals for EVERY
 * subscribed session while the transcript folding stays active-session-only.
 */
import { create, type StoreApi } from "zustand";

import {
  Kind,
  type Event as SessionEvent,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type {
  Agent,
  Session,
  Skill,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { ModelCapability } from "../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Unsubscribe } from "../../lib/transport";
import { client } from "../../lib/client";
import { useNavStore } from "../../stores/nav";
import {
  applyEvent,
  asEntry,
  asRichUser,
  entryID,
  mergePrepended,
} from "./entries";
import type { ToolCatalog } from "./tools/toolDisplay";
import type { ChatStore, Entry, PanelTab, QueuedMessage } from "./types";

export type { ChatStore, QueuedMessage } from "./types";

/**
 * Store state this wave adds (tasks 6.1, 6.5, 7.2, session-row running
 * state). The shared types file sits outside this wave's file scope, so the
 * extensions ride an intersection alias and the store instance is typed with
 * the widened shape — every read site that needs the new fields widens the
 * selector to ExtendedChatStore.
 */
export interface ChatStoreExtras {
  /** Seq of the oldest loaded transcript event; null once the session's
   * beginning is reached (the "load earlier" control hides). */
  oldestSeq: number | null;
  /** True while an older transcript page is being fetched. */
  loadingEarlier: boolean;
  /** Sessions with a turn in flight (including approval-paused) — the rail's
   * running indicator source, tracked for every subscribed session. */
  runningSessions: Set<string>;
  /** Fetches the next older transcript page and prepends it (task 6.1). */
  loadEarlier: () => Promise<void>;
  /** Deletes the session and its transcript after the UI confirmation
   * (task 6.5); refused turns surface through `error`. */
  deleteSession: (sessionID: string) => Promise<void>;
  /** Regenerates a turn's answer as a new variant (task 7.2, design D7). */
  regenerate: (turnID: string) => Promise<void>;
  /**
   * The send action widened with the attachment keys (task 5.4, design D5).
   * Intersected with the ChatStore member, so existing one-argument callers
   * keep compiling and the composer may pass ready upload keys.
   */
  send: (text: string, attachmentKeys?: string[]) => Promise<void>;
  /** Pure local variant switch (task 7.2): no request, index clamped. */
  setActiveVariant: (turnID: string, index: number) => void;
  /**
   * The settings tools catalog keyed by raw tool name (task 3.3, design
   * D3): the display names and icon keys the tool-card headers render.
   * Empty until the one-time load lands; unknown names fall back to the raw
   * name at the read site (toolDisplay.toolDisplayInfo).
   */
  toolCatalog: ToolCatalog;
  /** Loads `client.tools()` once per app run (task 3.3); idempotent, and a
   * failed load is retryable by the next call. */
  ensureToolCatalog: () => Promise<void>;
  /**
   * Refetches the active agent's session list when `agentID` matches it
   * (add-scheduler design D10): a schedule firing in the background creates
   * a session, and this makes it appear without a reload. A mismatched id,
   * a superseded agent switch, or a failed fetch keeps the current list.
   */
  refreshSessions: (agentID: string) => Promise<void>;
}

export type ExtendedChatStore = ChatStore & ChatStoreExtras;

/** Live subscriptions by session id: they persist across session switches so
 * background turns keep their rail indicators (session-row running state). */
const subscriptions = new Map<string, Unsubscribe>();

/** Transcript page size (design D6: limit ≈ 60) for the newest-page load and
 * each "load earlier" fetch. */
export const TRANSCRIPT_PAGE_SIZE = 60;

/** The seq to page from after a fetch, or null when the session's beginning
 * is reached (a short page, or a page holding no seqs at all). */
function oldestSeqOf(page: SessionEvent[]): number | null {
  if (page.length < TRANSCRIPT_PAGE_SIZE) return null;

  let min: number | null = null;

  for (const e of page) {
    const seq = e.seq ?? 0;

    if (seq > 0 && (min === null || seq < min)) min = seq;
  }

  return min;
}

/** Attachment keys queued alongside a queued message (task 5.4): the shared
 * QueuedMessage shape carries text only, so the keys ride this module map and
 * join the dispatch when the queue drains. */
const queuedAttachmentKeys = new Map<string, string[]>();

/** Session load sequence — stale async continuations of a superseded
 * session/agent switch must not write state. */
let loadSeq = 0;

/** Event ids already folded into the current session's entries. */
let seenEventIDs = new Set<string>();

/** Events buffered for the session currently loading its initial transcript
 * page (task 4.1, design D4). Folded on top of the page once it lands. */
let eventBuffer: {
  sessionID: string;
  seq: number;
  events: SessionEvent[];
} | null = null;

let nextQueueID = 0;

/** Minted right-panel tab ids (task 8.2). */
let nextPanelTabID = 0;

/** The in-flight (or settled) catalog load, so `client.tools()` runs once
 * per app run (task 3.3). A failed load resets it — the next call retries. */
let toolCatalogPromise: Promise<void> | null = null;

type ChatSetState = StoreApi<ExtendedChatStore>["setState"];

type ChatGetState = StoreApi<ExtendedChatStore>["getState"];

function errorMessage(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback;
}

/**
 * Sticky per-agent composer defaults (design D9): the last override the user
 * picked for this agent, stored under the D9 key family (the chat store lives
 * outside React, so the localStorage contract is applied directly rather
 * than through the useLocalPreference hook). A stored model seeds the agent's
 * newly created sessions; an absent or empty model means "no explicit
 * default" and the new session inherits the agent configuration.
 */
function stickyModelKey(agentID: string): string {
  return `buddies.chatDefaultModel.${agentID}`;
}

function stickyEffortKey(agentID: string): string {
  return `buddies.chatDefaultEffort.${agentID}`;
}

/** The agent's last-used override pair; only a stored model makes a seed. */
function storedOverride(
  agentID: string
): { model: string; effort: string } | null {
  try {
    const model = window.localStorage.getItem(stickyModelKey(agentID)) ?? "";

    if (model === "") return null;

    const effort = window.localStorage.getItem(stickyEffortKey(agentID)) ?? "";

    return { model, effort };
  } catch {
    // Storage unavailable: the sticky default is simply absent.
    return null;
  }
}

/** Records the pair after every explicit pick, clears included. */
function rememberOverride(
  agentID: string,
  model: string,
  effort: string
): void {
  try {
    window.localStorage.setItem(stickyModelKey(agentID), model);
    window.localStorage.setItem(stickyEffortKey(agentID), effort);
  } catch {
    // A failed write only loses the next session's seed; the session row
    // still carries the picked override.
  }
}

/** Loads the active agent's provider model capabilities (task 2.2). A failed
 * fetch leaves the pickers empty — the same keep-going contract as
 * refreshUsage below — while other surfaces stay usable. */
async function loadModels(agentID: string): Promise<void> {
  const s = useChatStore.getState();
  const providerID = s.agents.find((a: Agent) => a.ID === agentID)?.ProviderID;

  if (!providerID) {
    useChatStore.setState({ models: [] });

    return;
  }

  try {
    const models = await client.settings.providerModels(providerID);

    if (useChatStore.getState().activeAgentID === agentID) {
      useChatStore.setState({ models });
    }
  } catch {
    if (useChatStore.getState().activeAgentID === agentID) {
      useChatStore.setState({ models: [] });
    }
  }
}

function sortSessions(sessions: Session[]): Session[] {
  return [...sessions].sort((a: Session, b: Session) =>
    b.LastActivityAt.localeCompare(a.LastActivityAt)
  );
}

function setBusy(): void {
  const s = useChatStore.getState();
  useChatStore.setState({ busy: s.dispatching || s.activeTurnID !== null });
}

/** True when the kind ends a turn — the rail's running flag retires on any
 * of them (spec chat-view: "until the turn reaches a terminal outcome"). */
function isTerminalKind(kind: SessionEvent["kind"]): boolean {
  return (
    kind === Kind.KindTurnCompleted ||
    kind === Kind.KindTurnError ||
    kind === Kind.KindTurnCancelled
  );
}

/** True when the kind only occurs while its turn is in flight — the live
 * markers for the rail's running flag. Task activity is excluded: a
 * background task's events can outlive its turn and must not flag the
 * session (spec chat-view session rows: "a turn in flight"). */
function isTurnActivityKind(kind: SessionEvent["kind"]): boolean {
  return (
    kind === Kind.KindUserMessage ||
    kind === Kind.KindTextDelta ||
    kind === Kind.KindReasoningDelta ||
    kind === Kind.KindAssistantMessage ||
    kind === Kind.KindReasoning ||
    kind === Kind.KindToolActivity ||
    kind === Kind.KindApprovalRequired
  );
}

/** Folds one session's running flag into the per-session set. */
function setSessionRunning(sessionID: string, running: boolean): void {
  const prev = useChatStore.getState().runningSessions;

  if (prev.has(sessionID) === running) return;

  const next = new Set(prev);

  if (running) next.add(sessionID);
  else next.delete(sessionID);
  useChatStore.setState({ runningSessions: next });
}

/** The live handler factory: one closure per subscribed session. Turn
 * lifecycle events drive that session's rail flag for EVERY subscriber —
 * any turn-activity event marks it in flight (trigger-started turns
 * included; approval_required keeps an approval-paused turn flagged), a
 * terminal retires it — while the transcript folding, dedupe, and the
 * queue-driving terminal side effects stay active-session-only: a
 * background session's events must never touch the viewed transcript. */
function makeIngest(sessionID: string): (e: SessionEvent) => void {
  return (e: SessionEvent): void => {
    if (isTerminalKind(e.kind)) setSessionRunning(sessionID, false);
    else if (isTurnActivityKind(e.kind)) setSessionRunning(sessionID, true);

    if (useChatStore.getState().activeSessionID !== sessionID) return;

    if (eventBuffer !== null && eventBuffer.sessionID === sessionID) {
      eventBuffer.events.push(e);

      return;
    }

    ingest(e);
  };
}

/** Live handler + the ingest step of the replay loop share this: dedupe by
 * event id, fold, then run the turn-terminal side effects. */
function ingest(e: SessionEvent): void {
  if (e.id) {
    if (seenEventIDs.has(e.id)) return;
    seenEventIDs.add(e.id);
  }

  useChatStore.setState((prev: ExtendedChatStore) => ({
    entries: applyEvent(prev.entries, e),
  }));

  if (e.kind === Kind.KindTurnCompleted) {
    void refreshUsage();
    completeTurn(e.turn_id);
  } else if (e.kind === Kind.KindTurnError) {
    completeTurn(e.turn_id);
  } else if (e.kind === Kind.KindTurnCancelled) {
    // Cancellation is a terminal like the others (task 3.3): the reducer has
    // already finalized the streaming entries above, and the queued messages
    // must proceed without further user action.
    completeTurn(e.turn_id);
  }
}

function completeTurn(turnID?: string): void {
  const s = useChatStore.getState();

  if (turnID && s.activeTurnID && turnID !== s.activeTurnID) return;

  if (turnID) s.finishedTurnIDs.add(turnID);
  useChatStore.setState({ activeTurnID: null });
  setBusy();
  drainQueue();
}

async function refreshUsage(): Promise<void> {
  const sessionID = useChatStore.getState().activeSessionID;

  if (!sessionID) return;

  try {
    const usage = await client.chat.contextUsage(sessionID);

    if (useChatStore.getState().activeSessionID === sessionID) {
      useChatStore.setState({ usage });
    }
  } catch {
    // Keep the last known usage; the ring refreshes on the next terminal.
  }
}

/** One-at-a-time queue dispatch: pops the head and sends it through the
 * normal path. `dispatch` marks the store busy synchronously, so concurrent
 * drains cannot double-dispatch. */
function drainQueue(): void {
  if (useChatStore.getState().busy) return;
  const queue = useChatStore.getState().queue;

  if (queue.length === 0) return;
  const [head, ...rest] = queue;
  useChatStore.setState({ queue: rest });
  const keys = queuedAttachmentKeys.get(head.id) ?? [];

  queuedAttachmentKeys.delete(head.id);
  void dispatch(head.text, keys);
}

async function dispatch(
  text: string,
  attachmentKeys: string[] = []
): Promise<void> {
  const sessionID = useChatStore.getState().activeSessionID;

  if (!sessionID) return;
  useChatStore.setState({ dispatching: true });
  setBusy();
  let turnID = "";
  let failure: string | null = null;

  try {
    turnID =
      attachmentKeys.length > 0
        ? await client.chat.send(sessionID, text, attachmentKeys)
        : await client.chat.send(sessionID, text);
  } catch (err: unknown) {
    failure = errorMessage(err, "Failed to send message");
  }

  useChatStore.setState({ dispatching: false });

  if (failure !== null) {
    // Narrowed alias: setState's callback closure would widen `failure` back.
    const message = failure;
    useChatStore.setState((prev: ExtendedChatStore) => ({
      entries: [
        ...prev.entries,
        { kind: "error", id: entryID(), message, at: new Date().toISOString() },
      ],
    }));
    setBusy();
    drainQueue();

    return;
  }

  // The turn reached a terminal state while send() was resolving — its
  // completion already ran; just let the queue proceed.
  if (useChatStore.getState().finishedTurnIDs.has(turnID)) {
    setBusy();
    drainQueue();

    return;
  }

  useChatStore.setState({ activeTurnID: turnID });
  setSessionRunning(sessionID, true);
  setBusy();
}

export const useChatStore = create<ExtendedChatStore>(
  (set: ChatSetState, get: ChatGetState) => ({
    agents: [],
    providers: [],
    skills: [],

    activeAgentID: null,
    sessions: [],
    activeSessionID: null,

    models: [],
    modelOverride: "",
    effortOverride: "",

    entries: [],
    usage: null,
    loadingSession: false,
    error: null,

    oldestSeq: null,
    loadingEarlier: false,
    runningSessions: new Set<string>(),

    busy: false,
    dispatching: false,
    activeTurnID: null,
    finishedTurnIDs: new Set<string>(),
    queue: [],

    sidebarCollapsed: false,

    panelTabs: [],
    activePanelTabID: null,

    toolCatalog: {},

    init: async (): Promise<void> => {
      try {
        const [agents, providers, skills] = await Promise.all([
          client.agents.list(),
          client.settings.providers(),
          client.settings.skills(),
        ]);

        set({
          agents,
          providers,
          skills: skills.filter((s: Skill) => s.Enabled),
        });
      } catch (err: unknown) {
        set({ error: errorMessage(err, "Failed to load chat") });

        return;
      }

      const target = useNavStore.getState().chatTarget;

      if (target) {
        useNavStore.getState().clearChatTarget();

        if (
          target.agentID !== get().activeAgentID ||
          (target.sessionID && target.sessionID !== get().activeSessionID)
        ) {
          await get().selectAgent(target.agentID, target.sessionID);
        }

        return;
      }

      if (!get().activeAgentID && get().agents.length > 0) {
        await get().selectAgent(get().agents[0].ID);
      }
    },

    selectAgent: async (agentID: string, sessionID?: string): Promise<void> => {
      const seq = ++loadSeq;
      eventBuffer = null;
      set({ activeAgentID: agentID, error: null });
      // The composer's model/effort pickers read the agent's provider
      // capabilities; a stale continuation must not overwrite a newer agent's.
      void loadModels(agentID);
      let sessions: Session[] = [];

      try {
        sessions = sortSessions(await client.chat.sessions(agentID));
      } catch (err: unknown) {
        if (seq === loadSeq)
          set({
            error: errorMessage(err, "Failed to load sessions"),
            sessions,
          });

        return;
      }

      if (seq !== loadSeq) return;
      set({ sessions });

      // Subscriptions of sessions no longer in the new agent's list are
      // torn down (task 6.5 hygiene); the rest persist so background turns
      // keep their rail indicators.
      const keep = new Set(sessions.map((s: Session): string => s.ID));

      for (const [id, off] of subscriptions) {
        if (!keep.has(id)) {
          off();
          void client.chat.unsubscribe(id);
          subscriptions.delete(id);
        }
      }

      const next =
        sessionID && sessions.some((s: Session) => s.ID === sessionID)
          ? sessionID
          : sessions[0]?.ID;

      if (next) {
        await get().selectSession(next);
      } else {
        for (const [id, off] of subscriptions) {
          off();
          void client.chat.unsubscribe(id);
        }

        subscriptions.clear();
        set({
          activeSessionID: null,
          entries: [],
          usage: null,
          loadingSession: false,
          panelTabs: [],
          activePanelTabID: null,
          // No open session: the override state resets to inherit; a newly
          // created session is seeded from the sticky default instead.
          modelOverride: "",
          effortOverride: "",
        });
      }
    },

    selectSession: async (sessionID: string): Promise<void> => {
      const seq = ++loadSeq;
      eventBuffer = { sessionID, seq, events: [] };

      seenEventIDs = new Set<string>();
      // The queue belongs to the session being left: dispatch always targets
      // the active session, so pending (never-sent) entries are dropped with it
      // rather than delivered to the session being opened.
      // The override state hydrates from the session row (task 2.1, design
      // D1: empty means inherit); an unknown row hydrates as inherit too.
      const row = get().sessions.find((s: Session) => s.ID === sessionID);
      set({
        activeSessionID: sessionID,
        entries: [],
        usage: null,
        loadingSession: true,
        queue: [],
        dispatching: false,
        activeTurnID: null,
        finishedTurnIDs: new Set<string>(),
        modelOverride: row?.Model ?? "",
        effortOverride: row?.Effort ?? "",
        // The windowing state restarts with the session (task 6.1).
        oldestSeq: null,
        loadingEarlier: false,
        // Spec chat-panels: switching sessions resets the panel to closed.
        panelTabs: [],
        activePanelTabID: null,
      });
      setBusy();

      try {
        const off = await client.chat.subscribe(
          sessionID,
          makeIngest(sessionID)
        );

        if (seq !== loadSeq) {
          off();

          if (eventBuffer?.seq === seq) eventBuffer = null;

          return;
        }

        const previous = subscriptions.get(sessionID);

        if (previous) {
          previous();
        }

        subscriptions.set(sessionID, off);

        // Newest page first (task 6.1, design D6); "load earlier" pages back
        // through the same reducer. A short page means the session's
        // beginning is already loaded and the affordance stays hidden.
        const events = await client.chat.transcriptPage(
          sessionID,
          0,
          TRANSCRIPT_PAGE_SIZE
        );

        if (seq !== loadSeq) {
          if (eventBuffer?.seq === seq) eventBuffer = null;

          return;
        }

        const buffered =
          eventBuffer?.seq === seq && eventBuffer.sessionID === sessionID
            ? eventBuffer.events
            : [];

        eventBuffer = null;

        let entries: Entry[] = [];

        for (const e of events) {
          if (e.id) {
            if (seenEventIDs.has(e.id)) continue;
            seenEventIDs.add(e.id);
          }

          entries = applyEvent(entries, e);
        }

        for (const e of buffered) {
          if (e.id) {
            if (seenEventIDs.has(e.id)) continue;
            seenEventIDs.add(e.id);
          }

          entries = applyEvent(entries, e);

          if (isTerminalKind(e.kind) && e.turn_id) {
            get().finishedTurnIDs.add(e.turn_id);
          }
        }

        set({ entries, oldestSeq: oldestSeqOf(events), loadingSession: false });

        const turnState = await client.chat.turnState(sessionID);

        if (seq !== loadSeq) return;

        if (turnState.busy && !get().finishedTurnIDs.has(turnState.turn_id)) {
          const nextRunning = new Set(get().runningSessions);
          nextRunning.add(sessionID);
          set({
            activeTurnID: turnState.turn_id,
            runningSessions: nextRunning,
          });
          setBusy();
        }

        const usage = await client.chat.contextUsage(sessionID);

        if (seq !== loadSeq) return;
        set({ usage });
      } catch (err: unknown) {
        if (eventBuffer?.seq === seq) eventBuffer = null;

        if (seq !== loadSeq) return;

        set({
          error: errorMessage(err, "Failed to load session"),
          loadingSession: false,
        });
      }
    },

    // Older history pages backward through the SAME reducer (task 6.1,
    // design D6): the older page folds into a fresh list — merging it into
    // the live window would wrongly extend trailing streaming entries — and
    // mergePrepended then joins the segments, dropping the boundary turn's
    // leftover streaming entry so it renders as one unbroken entry.
    loadEarlier: async (): Promise<void> => {
      const s = get();
      const sessionID = s.activeSessionID;

      if (!sessionID || s.oldestSeq === null || s.loadingEarlier) return;

      const before = s.oldestSeq;
      set({ loadingEarlier: true });

      try {
        const page = await client.chat.transcriptPage(
          sessionID,
          before,
          TRANSCRIPT_PAGE_SIZE
        );

        if (useChatStore.getState().activeSessionID !== sessionID) return;

        let older: Entry[] = [];

        for (const e of page) {
          if (e.id) {
            if (seenEventIDs.has(e.id)) continue;
            seenEventIDs.add(e.id);
          }

          older = applyEvent(older, e);
        }

        useChatStore.setState((prev: ExtendedChatStore) => ({
          entries: mergePrepended(older, prev.entries),
          oldestSeq: oldestSeqOf(page),
          loadingEarlier: false,
        }));
      } catch (err: unknown) {
        if (useChatStore.getState().activeSessionID !== sessionID) return;
        useChatStore.setState({
          error: errorMessage(err, "Failed to load earlier messages"),
          loadingEarlier: false,
        });
      }
    },

    // Delete (task 6.5): the backend removes the session and its transcript
    // and refuses a session with a turn in flight — that refusal surfaces
    // through the store error (the rail toasts it); no client-side
    // preemption is attempted.
    deleteSession: async (sessionID: string): Promise<void> => {
      try {
        await client.chat.deleteSession(sessionID);
      } catch (err: unknown) {
        set({ error: errorMessage(err, "Failed to delete session") });

        return;
      }

      const off = subscriptions.get(sessionID);

      if (off) {
        off();
        subscriptions.delete(sessionID);
      }

      void client.chat.unsubscribe(sessionID);

      if (eventBuffer?.sessionID === sessionID) eventBuffer = null;

      setSessionRunning(sessionID, false);

      const state = get();

      const remaining = state.sessions.filter(
        (s: Session): boolean => s.ID !== sessionID
      );

      set({ sessions: remaining });

      if (state.activeSessionID !== sessionID) return;

      const next = remaining[0]?.ID;

      if (next) {
        await get().selectSession(next);

        return;
      }

      // Nothing left for the agent: the chat view opens empty.
      set({
        activeSessionID: null,
        entries: [],
        usage: null,
        loadingSession: false,
        queue: [],
        dispatching: false,
        activeTurnID: null,
        finishedTurnIDs: new Set<string>(),
        panelTabs: [],
        activePanelTabID: null,
        modelOverride: "",
        effortOverride: "",
      });
      setBusy();
    },

    // Regeneration is a real turn (design D7): it rides the same
    // busy/terminal machinery as a send — activeTurnID tracks it, terminals
    // retire it, and the queue waits behind it. Regenerating while busy is
    // refused locally (the backend would reject it the same way).
    regenerate: async (turnID: string): Promise<void> => {
      const s = get();
      const sessionID = s.activeSessionID;

      if (!sessionID || s.busy || s.dispatching) return;
      useChatStore.setState({ dispatching: true });
      setBusy();

      try {
        const newTurnID = await client.chat.regenerate(sessionID, turnID);

        // The turn reached a terminal while the call resolved — nothing to
        // track.
        if (get().finishedTurnIDs.has(newTurnID)) {
          useChatStore.setState({ dispatching: false });
          setBusy();

          return;
        }

        useChatStore.setState({
          dispatching: false,
          activeTurnID: newTurnID,
        });
        setSessionRunning(sessionID, true);
        setBusy();
      } catch (err: unknown) {
        useChatStore.setState({ dispatching: false });
        set({ error: errorMessage(err, "Failed to regenerate") });
        setBusy();
        drainQueue();
      }
    },

    // Pure local variant navigation (task 7.2, spec: switching is offline) —
    // no request, the displayed content switches on the next render.
    setActiveVariant: (turnID: string, index: number): void => {
      useChatStore.setState((prev: ExtendedChatStore) => ({
        entries: prev.entries.map((en: Entry): Entry => {
          if (en.kind !== "user") return en;

          // SAFETY: user entries minted by applyEvent carry the widened
          // RichUserEntry fields (variant slot) — the seam read narrows them.
          const anchor = asRichUser(en);

          if (!anchor.variantTurns?.includes(turnID)) return en;

          const clamped = Math.max(
            0,
            Math.min(index, anchor.variantTurns.length - 1)
          );

          return asEntry({ ...anchor, activeVariant: clamped });
        }),
      }));
    },

    newSession: async (): Promise<void> => {
      const agentID = get().activeAgentID;

      if (!agentID) return;

      try {
        let session = await client.chat.createSession(agentID);
        // Seed the new session with the agent's last-used composer override
        // (task 2.1, design D9); with no sticky default the row stays empty
        // and simply inherits the agent's configuration.
        const seed = storedOverride(agentID);

        if (seed) {
          try {
            await client.chat.setSessionOverrides(
              session.ID,
              seed.model,
              seed.effort
            );
            session = { ...session, Model: seed.model, Effort: seed.effort };
          } catch (err: unknown) {
            // The session stays usable unseeded (it inherits); the failure
            // still surfaces instead of silently dropping the default.
            set({ error: errorMessage(err, "Failed to apply defaults") });
          }
        }

        set((prev: ChatStore) => ({
          sessions: sortSessions([
            session,
            ...prev.sessions.filter((s: Session) => s.ID !== session.ID),
          ]),
        }));
        await get().selectSession(session.ID);
      } catch (err: unknown) {
        set({ error: errorMessage(err, "Failed to create session") });
      }
    },

    // Attachment keys ride the send (task 5.4, design D5): ready uploads
    // pass their capability keys; an attachment-only message (no text) is
    // sendable. While busy, the message queues with its keys (the module
    // map — QueuedMessage carries text only) and dispatches in order.
    send: async (text: string, attachmentKeys?: string[]): Promise<void> => {
      const trimmed = text.trim();
      const keys = attachmentKeys ?? [];

      if (!trimmed && keys.length === 0) return;

      // No open session: create the active agent's session first (task 3.4)
      // so typing + Enter never silently no-ops. `newSession` selects the
      // created session through the same path as the rail's new-session
      // button — exactly one session, no duplicates; on failure it records
      // the store error and the send is dropped.
      if (!get().activeSessionID) {
        if (!get().activeAgentID) return;
        await get().newSession();

        if (!get().activeSessionID) return;
      }

      if (get().busy) {
        nextQueueID += 1;

        const id = `queued-${nextQueueID}`;

        if (keys.length > 0) queuedAttachmentKeys.set(id, keys);

        const queued: QueuedMessage = { id, text: trimmed };

        set((prev: ExtendedChatStore) => ({
          queue: [...prev.queue, queued],
        }));

        return;
      }

      await dispatch(trimmed, keys);
    },

    removeQueued: (id: string): void => {
      queuedAttachmentKeys.delete(id);
      set((prev: ExtendedChatStore) => ({
        queue: prev.queue.filter((q: QueuedMessage) => q.id !== id),
      }));
    },

    // Overrides ride the session row (design D1): a pick persists through
    // SetSessionOverrides, mirrors onto the local row (the header chip and
    // session rail read it), and updates the sticky per-agent default that
    // seeds newly created sessions (design D9). With no open session the
    // pick only records the default — the auto-created session on send is
    // seeded from it.
    setModelOverride: async (model: string): Promise<void> => {
      const s = get();
      const agent = s.agents.find((a: Agent) => a.ID === s.activeAgentID);
      const sessionID = s.activeSessionID;

      // Switching models drops an effort override the new model cannot
      // express (design D2); clearing the model keeps the effort choice.
      let effort = s.effortOverride;

      if (model !== "" && effort !== "") {
        const efforts =
          s.models.find((m: ModelCapability) => m.ID === model)?.Efforts ?? [];

        if (!efforts.includes(effort)) effort = "";
      }

      if (sessionID) {
        try {
          await client.chat.setSessionOverrides(sessionID, model, effort);
        } catch (err: unknown) {
          set({ error: errorMessage(err, "Failed to save model") });

          return;
        }

        if (get().activeSessionID !== sessionID) return;
        set((prev: ChatStore) => ({
          modelOverride: model,
          effortOverride: effort,
          sessions: prev.sessions.map((row: Session) =>
            row.ID === sessionID
              ? { ...row, Model: model, Effort: effort }
              : row
          ),
        }));
      } else {
        set({ modelOverride: model, effortOverride: effort });
      }

      if (agent) rememberOverride(agent.ID, model, effort);
    },

    setEffortOverride: async (effort: string): Promise<void> => {
      const s = get();
      const agent = s.agents.find((a: Agent) => a.ID === s.activeAgentID);
      const sessionID = s.activeSessionID;

      if (sessionID) {
        try {
          await client.chat.setSessionOverrides(
            sessionID,
            s.modelOverride,
            effort
          );
        } catch (err: unknown) {
          set({ error: errorMessage(err, "Failed to save effort") });

          return;
        }

        if (get().activeSessionID !== sessionID) return;
        set((prev: ChatStore) => ({
          effortOverride: effort,
          sessions: prev.sessions.map((row: Session) =>
            row.ID === sessionID ? { ...row, Effort: effort } : row
          ),
        }));
      } else {
        set({ effortOverride: effort });
      }

      if (agent) rememberOverride(agent.ID, s.modelOverride, effort);
    },

    stop: (): void => {
      const sessionID = get().activeSessionID;

      if (!sessionID) return;

      // The turn_cancelled event drives the state change (busy back to idle,
      // queue drain); the call itself only reports a failed request.
      client.chat.cancel(sessionID).catch((err: Error): void => {
        set({
          error: err instanceof Error ? err.message : "Failed to stop the turn",
        });
      });
    },

    toggleSidebar: (): void => {
      set((prev: ChatStore) => ({ sidebarCollapsed: !prev.sidebarCollapsed }));
    },

    // The tools catalog loads once per app run (task 3.3, design D3): the
    // settings listing supplies the display names and icon keys the card
    // headers render; unknown/MCP names fall back to the raw name at the
    // read site. Keep-going contract: a failed load leaves the (empty)
    // catalog and resets the one-shot so a later call retries.
    ensureToolCatalog: async (): Promise<void> => {
      if (toolCatalogPromise) return toolCatalogPromise;

      toolCatalogPromise = (async (): Promise<void> => {
        try {
          const entries = await client.settings.tools();
          const catalog: ToolCatalog = {};

          for (const entry of entries) {
            catalog[entry.Name] = {
              name: entry.Name,
              displayName: entry.DisplayName,
              iconKey: entry.IconKey,
            };
          }

          useChatStore.setState({ toolCatalog: catalog });
        } catch {
          toolCatalogPromise = null;
        }
      })();

      return toolCatalogPromise;
    },

    // Background-created sessions (add-scheduler design D10): a schedule
    // firing for the active agent emits schedules:changed, and the chat
    // feature refetches through this action so the session appears in the
    // rail without a reload. The double activeAgentID check mirrors
    // loadModels: a superseded continuation must not write a newer agent's
    // list, and a failed fetch keeps the current list (keep-going).
    refreshSessions: async (agentID: string): Promise<void> => {
      if (get().activeAgentID !== agentID) return;

      try {
        const sessions = sortSessions(await client.chat.sessions(agentID));

        if (get().activeAgentID !== agentID) return;
        set({ sessions });
      } catch {
        // Keep the last known list; the next schedules:changed retries.
      }
    },

    // Panel state is pure view state (task 8.2): opening or closing a tab
    // touches nothing the turn lifecycle owns, so using the panel mid-turn
    // can never interrupt a streaming turn.
    openPanelTab: (tab: Omit<PanelTab, "id">): void => {
      const existing = get().panelTabs.find(
        (t: PanelTab) => t.identity === tab.identity
      );

      // Re-activating a row whose tab is already open must raise it, not
      // just no-op: the row click is the activation gesture.
      if (existing !== undefined) {
        set({ activePanelTabID: existing.id });

        return;
      }

      nextPanelTabID += 1;

      // SAFETY: the spread of the PanelTab union plus the id re-forms exactly
      // one union member at runtime; the union's per-member `data` shapes
      // defeat the structural check, so the re-formed record is asserted.
      const opened = {
        ...tab,
        id: `panel-${nextPanelTabID}`,
      } as PanelTab;

      set((prev: ChatStore) => ({
        panelTabs: [...prev.panelTabs, opened],
        activePanelTabID: opened.id,
      }));
    },

    activatePanelTab: (id: string): void => {
      set({ activePanelTabID: id });
    },

    closePanelTab: (id: string): void => {
      set((prev: ChatStore) => {
        const panelTabs = prev.panelTabs.filter((t: PanelTab) => t.id !== id);

        return {
          panelTabs,
          // Closing the active tab raises the last remaining one.
          activePanelTabID:
            prev.activePanelTabID === id
              ? (panelTabs[panelTabs.length - 1]?.id ?? null)
              : prev.activePanelTabID,
        };
      });
    },
  })
);
