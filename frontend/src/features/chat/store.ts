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
 * Session subscriptions persist across session and agent switches
 * (spec chat-view "Sessions organized per agent"): a turn in flight in a
 * background session keeps its rail indicator until a terminal event
 * retires it, so the per-session running set folds terminals for EVERY
 * subscribed session while the transcript folding stays active-session-only.
 * add-grouped-session-rail design D3: the running dots no longer DEPEND on
 * these subscriptions — the backend's turns:changed channel and the boot/
 * focus re-queries drive the same set for any session, subscribed or not.
 */
import { create, type StoreApi } from "zustand";

import {
  Kind,
  type Attachment as SessionAttachment,
  type Event as SessionEvent,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type {
  Agent,
  AgentGroup,
  AgentGroupMessage,
  AgentGroupTask,
  Session,
  Skill,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { GroupEvent } from "../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { WorkspaceSkill } from "../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { ModelCapability } from "../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { TurnsChangedEvent } from "../../lib/events";
import type { Unsubscribe } from "../../lib/transport";
import { client } from "../../lib/client";
import { useNavStore } from "../../stores/nav";
import { mapGroupMessages } from "../groups/groupEntries";
import { useAttachmentStore, type AttachmentChip } from "./attachments";
import {
  applyEvent,
  asEntry,
  asRichUser,
  entryID,
  expireUnresolvedInputs,
  mergePrepended,
} from "./entries";
import type { ToolCatalog } from "./tools/toolDisplay";
import type {
  ChatStore,
  Entry,
  PanelTab,
  PickerSkill,
  QueuedMessage,
} from "./types";

export type { ChatStore, QueuedMessage } from "./types";

/** A live terminal outcome of the viewed session (add-live-agent-avatar
 * design D4): reactions are edges, never derivable from a replayed
 * transcript, so each terminal mints a fresh object and the avatar hook
 * reacts to changes of the object's IDENTITY, not its value. */
export interface TurnOutcome {
  /** The terminal event's turn id; "" when the event carries none. */
  turnID: string;
  /** Which terminal ended the turn. */
  kind: Kind.KindTurnCompleted | Kind.KindTurnError | Kind.KindTurnCancelled;
  /** The event's timestamp as the entries fold reads it (applyEvent stamps
   * entries with occurred_at), else now. */
  at: string;
}

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
  /**
   * Sessions with a turn in flight (including approval-paused) — the rail's
   * running indicator source. Three idempotent sources fold into it
   * (add-grouped-session-rail design D2/D3): the per-session live ingest
   * path, the backend's turns:changed channel, and the boot/focus
   * re-queries that replace the set wholesale — dots no longer depend on
   * subscriptions.
   */
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
   * Refetches EVERY agent's sessions when any agent's schedules:changed
   * fires (add-scheduler design D10 + add-grouped-session-rail design D1):
   * a background fire creates a session, and the `agentID` argument is kept
   * for caller compatibility but ignored — the refetched all-agents list
   * updates every group's count in the rail without a reload. A superseded
   * store load or a failed fetch keeps the current list (keep-going).
   */
  refreshSessions: (agentID: string) => Promise<void>;
  /**
   * The viewed session's latest live terminal outcome (add-live-agent-avatar
   * design D4). Invariants: set only on the live ingest path for the ACTIVE
   * (viewed) session — a background session's terminal never sets it, and
   * the transcript replay folds in selectSession/loadEarlier never mint one
   * (a reloaded transcript must never react); null on load/init; reset to
   * null whenever the session switches or clears, beside the panelTabs
   * resets. Readers react to the object's identity, not its value.
   */
  lastTurnOutcome: TurnOutcome | null;
}

export type ExtendedChatStore = ChatStore & ChatStoreExtras;

/** Live subscriptions by session id: they persist across session switches so
 * background turns keep their rail indicators (session-row running state). */
const subscriptions = new Map<string, Unsubscribe>();

/** Live subscriptions by group id (add-agent-groups design D9): one per known
 * group, feeding the rail's busy dots for every group and the open group's
 * transcript appends. Wired by connectGroups (the view mounts it), never at
 * import time. */
const groupSubscriptions = new Map<string, Unsubscribe>();

/** Group message ids already folded into the open group's projection: the
 * dedupe key that keeps a live append from doubling a row the initial page
 * already carried (the session path's seenEventIDs seam). */
let seenGroupMessageIDs = new Set<string>();

/** Transcript page size (design D6: limit ≈ 60) for the newest-page load and
 * each "load earlier" fetch. */
export const TRANSCRIPT_PAGE_SIZE = 60;

/**
 * Stream-resume tunables (enhance-resumable-streams design D6), mirroring
 * the backend: `streamBeaconInterval` in internal/agents/manager_impl.go
 * drives the liveness beacon's period, and a busy session that has seen no
 * beacon (or sync) for twice that period counts as stale.
 */
export const STREAM_BEACON_INTERVAL_MS = 5000;

/** The staleness window: two missed beacons (design D6 trigger (b)). */
export const STREAM_STALENESS_MS = STREAM_BEACON_INTERVAL_MS * 2;

/** Per-turn live-stream tracking for the resync triggers (design D6): the
 * received TEXT-delta count — rebaselined by a folded sync, dropped at the
 * turn's terminal — and the last beacon observation. */
interface StreamTurnTracking {
  received: number;
  lastPing: { count: number; at: number } | null;
}

/** Tracking by turn id for the ACTIVE session (the ingest path only folds
 * the active session's events); a session switch starts it fresh. */
let streamTurns = new Map<string, StreamTurnTracking>();

/** The last liveness observation (a beacon or a sync) for the active
 * session, in ms — the staleness watch's clock reference. */
let lastStreamObservation = 0;

/** True once the current silence episode's staleness resync fired (design
 * D6: triggers fire on transitions, not on a timer loop); any liveness
 * observation or a watch (re)start clears it. */
let stalenessFired = false;

/** The busy period's staleness watch interval handle; null while idle. */
let streamWatchHandle: number | null = null;

/** The one-resync-in-flight guard (design D6): a burst of triggers collapses
 * into the first call; the guard clears when the promise settles. */
let resyncInFlight = false;

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

/** The group transcript's paging cursor (add-agent-groups design D6): the
 * same windowing contract as the session path — a short page means the
 * group's beginning is reached. */
function groupOldestSeqOf(page: AgentGroupMessage[]): number | null {
  if (page.length < TRANSCRIPT_PAGE_SIZE) return null;

  let min: number | null = null;

  for (const row of page) {
    if (row.Seq > 0 && (min === null || row.Seq < min)) min = row.Seq;
  }

  return min;
}

/** Inserts or refreshes one group row in the rail's list (a fresh open or a
 * rename must not wait for the next groups:changed). */
function upsertGroupRow(groups: AgentGroup[], row: AgentGroup): AgentGroup[] {
  const idx = groups.findIndex((g: AgentGroup): boolean => g.ID === row.ID);

  if (idx === -1) return [...groups, row];

  return groups.map((g: AgentGroup): AgentGroup => (g.ID === row.ID ? row : g));
}

/** The group half of "load earlier" (add-agent-groups design D6): pages the
 * transcript window backward through the same mapping, prepending the older
 * rows. Mirrors the session path's stale-guard and keep-going contract. */
async function loadGroupEarlierPage(): Promise<void> {
  const s = useChatStore.getState();
  const groupID = s.activeGroupID;

  if (!groupID || s.oldestSeq === null || s.loadingEarlier) return;

  const before = s.oldestSeq;
  useChatStore.setState({ loadingEarlier: true });

  try {
    const page = await client.groups.transcriptPage(
      groupID,
      before,
      TRANSCRIPT_PAGE_SIZE
    );

    if (useChatStore.getState().activeGroupID !== groupID) return;

    const fresh = page.filter(
      (m: AgentGroupMessage): boolean => !seenGroupMessageIDs.has(m.MessageID)
    );

    for (const m of fresh) seenGroupMessageIDs.add(m.MessageID);

    const rows = [...fresh, ...useChatStore.getState().groupMessages];

    useChatStore.setState({
      groupMessages: rows,
      entries: mapGroupMessages(rows),
      oldestSeq: groupOldestSeqOf(page),
      loadingEarlier: false,
    });
  } catch (err: unknown) {
    if (useChatStore.getState().activeGroupID !== groupID) return;
    useChatStore.setState({
      error: errorMessage(err, "Failed to load earlier messages"),
      loadingEarlier: false,
    });
  }
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

/**
 * Live per-token deltas awaiting the next animation-frame commit (task 5.1,
 * design D3) — a NEW path beside `eventBuffer` above, which stays the
 * page-load replay seam. The batch folds into `entries` as ONE setState at
 * the frame's rAF flush (or synchronously ahead of any non-delta event, so
 * arrival order — delta, tool card, delta, terminal — survives), and
 * subscribers never observe a half-applied batch. The buffer exists only
 * between scheduled flushes: an external getState() of `entries` between a
 * delta's arrival and its flush shows the last-flushed state — accepted,
 * nothing exposes the pending batch. The flush is scheduled when the FIRST
 * delta of a frame lands (leading edge, no trailing debounce), so the first
 * token commits in the very next frame — the same paint the un-coalesced
 * path would have hit, since React renders at the next vsync either way. */
let pendingDeltas: SessionEvent[] = [];

/** The scheduled frame flush's handle; null while nothing is pending. */
let pendingFlushHandle: number | null = null;

let nextQueueID = 0;

/** Minted right-panel tab ids (task 8.2). */
let nextPanelTabID = 0;

/** The in-flight (or settled) catalog load, so `client.tools()` runs once
 * per app run (task 3.3). A failed load resets it — the next call retries. */
let toolCatalogPromise: Promise<void> | null = null;

/** The raw skill-registry rows (the global library tier) as loaded at boot;
 * the per-agent merge in pickerSkills filters and appends the workspace
 * tier, so the registry is kept instead of only its filtered projection. */
let registrySkills: Skill[] = [];

/**
 * Merges the two skill tiers into the composer's picker list (spec
 * agent-skills "Skill eligibility gate"): the enabled registry rows that
 * pass the agent's skill allowlist (empty = all) form the global tier, and
 * the agent's own workspace skills — always available to it, allowlist or
 * not — form the agent tier and shadow a same-named global row, mirroring
 * the skill backend's precedence.
 */
function pickerSkills(
  agent: Agent | null,
  workspace: WorkspaceSkill[]
): PickerSkill[] {
  const agentTier: PickerSkill[] = workspace.map(
    (skill: WorkspaceSkill): PickerSkill => ({
      Name: skill.Name,
      Description: skill.Description,
      Tier: "agent",
    })
  );

  const shadowed = new Set(agentTier.map((skill: PickerSkill) => skill.Name));
  const allowed = agent?.Skills ?? [];

  const globalTier = registrySkills
    .filter(
      (row: Skill): boolean =>
        row.Enabled &&
        !shadowed.has(row.Name) &&
        (allowed.length === 0 || allowed.includes(row.Name))
    )
    .map((row: Skill): PickerSkill => ({
      Name: row.Name,
      Description: row.Description,
      Tier: "global",
    }));

  return [...globalTier, ...agentTier];
}

/**
 * Loads the agent's workspace skills tier and re-merges the picker list.
 * Fired on agent selection (the tier is per-agent) and at turn terminals (a
 * turn can author skills into the workspace — the fresh skill must be
 * invocable without an agent switch; the turn backend reads the tier fresh
 * per turn, this keeps the picker aligned). A failed read leaves the
 * current list; the next selection or terminal retries it.
 */
async function loadAgentSkills(agentID: string): Promise<void> {
  let workspace: WorkspaceSkill[];

  try {
    workspace = await client.agents.listWorkspaceSkills(agentID);
  } catch {
    return;
  }

  if (useChatStore.getState().activeAgentID !== agentID) return;

  const agent =
    useChatStore.getState().agents.find((a: Agent) => a.ID === agentID) ?? null;

  useChatStore.setState({ skills: pickerSkills(agent, workspace) });
}

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
  const busy = s.dispatching || s.activeTurnID !== null;

  // add-browser-login-handover, spec "Expired pending request": a busy→false
  // transition while an input entry is still pending means the turn ended
  // without the request ever being answered — fold it expired (the shared
  // helper in entries.ts; client-side only, no event is written). Live the
  // answering path records its input_resolved receipt first, so a pending
  // card never survives a terminal that legitimately resumed.
  if (s.busy && !busy) {
    // The staleness watch lives exactly as long as the busy period
    // (enhance-resumable-streams design D6 trigger (b)).
    stopStreamWatch();
    useChatStore.setState({
      busy,
      entries: expireUnresolvedInputs(s.entries),
    });

    return;
  }

  if (!s.busy && busy) startStreamWatch();

  useChatStore.setState({ busy });
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

/** True for the live-only per-token kinds the frame coalescer batches
 * (task 5.1, design D3): the two delta kinds only. Every other kind applies
 * immediately (after draining the pending batch), which keeps turn-terminal
 * state changes synchronous and preserves arrival order across a delta that
 * precedes a structural event. The stream-resume kinds stay out for the
 * same reason (enhance-resumable-streams design D7): a KindStreamSync must
 * REPLACE state in the tick it arrives — batching it behind deltas would
 * delay and re-order the reconciliation — and a KindStreamPing folds as a
 * no-op. */
function isCoalescedKind(kind: SessionEvent["kind"]): boolean {
  return kind === Kind.KindTextDelta || kind === Kind.KindReasoningDelta;
}

/** Folds the pending delta batch into `entries` as ONE commit. Runs on the
 * scheduled frame flush, and synchronously before any non-delta event — a
 * turn terminal therefore applies after the deltas that preceded it, in the
 * same tick. */
function flushPendingDeltas(): void {
  if (pendingFlushHandle !== null) {
    cancelAnimationFrame(pendingFlushHandle);
    pendingFlushHandle = null;
  }

  if (pendingDeltas.length === 0) return;

  const batch = pendingDeltas;
  pendingDeltas = [];

  useChatStore.setState((prev: ExtendedChatStore) => ({
    entries: batch.reduce(
      (entries: Entry[], e: SessionEvent): Entry[] => applyEvent(entries, e),
      prev.entries
    ),
  }));
}

/** Schedules the frame flush for the first delta of a frame; later deltas
 * of the same frame join the same batch. */
function scheduleDeltaFlush(): void {
  if (pendingFlushHandle !== null) return;

  pendingFlushHandle = requestAnimationFrame((): void => {
    pendingFlushHandle = null;
    flushPendingDeltas();
  });
}

/** Drops the pending batch without applying it: a session switch replaces
 * `entries` wholesale, so the session being left must not fold its
 * still-unflushed deltas into the new transcript. Cleared beside every
 * `eventBuffer` reset on a selection. */
function clearPendingDeltas(): void {
  if (pendingFlushHandle !== null) {
    cancelAnimationFrame(pendingFlushHandle);
    pendingFlushHandle = null;
  }

  pendingDeltas = [];
}

/** The tracking record for one turn, minted on first observation (design
 * D6: per active turn, keyed by the events' turn id). */
function streamTurnTracking(turnID: string | undefined): StreamTurnTracking {
  const key = turnID ?? "";
  let rec = streamTurns.get(key);

  if (!rec) {
    rec = { received: 0, lastPing: null };
    streamTurns.set(key, rec);
  }

  return rec;
}

/**
 * Requests one resync for the active session (enhance-resumable-streams
 * design D6): fire-and-forget — the snapshot broadcast comes back through
 * the normal subscription as a KindStreamSync and folds. The in-flight
 * guard collapses a burst of triggers into one call; a rejection is kept
 * going silently, the next trigger retries.
 */
function requestResync(): void {
  if (resyncInFlight) return;

  const sessionID = useChatStore.getState().activeSessionID;

  if (!sessionID) return;

  resyncInFlight = true;
  void client.chat
    .resync(sessionID)
    .catch((): void => {
      // Keep-going: a failed resync heals on the next trigger's attempt.
    })
    .finally((): void => {
      resyncInFlight = false;
    });
}

/** The busy period's staleness watch (design D6 trigger (b)): one interval
 * while the session is busy, ticking at the beacon's period; a tick that
 * finds no liveness observation for twice that period fires ONE resync per
 * silence episode. Started and stopped by setBusy's busy transitions. */
function startStreamWatch(): void {
  lastStreamObservation = Date.now();
  stalenessFired = false;

  if (streamWatchHandle !== null) return;

  streamWatchHandle = window.setInterval((): void => {
    if (!useChatStore.getState().busy) {
      stopStreamWatch();

      return;
    }

    if (stalenessFired) return;

    if (Date.now() - lastStreamObservation >= STREAM_STALENESS_MS) {
      // A live pipe refreshes the clock through its beacons, so this branch
      // only re-enters when the silence persisted for a whole window.
      stalenessFired = true;
      requestResync();
    }
  }, STREAM_BEACON_INTERVAL_MS);
}

function stopStreamWatch(): void {
  if (streamWatchHandle !== null) {
    window.clearInterval(streamWatchHandle);
    streamWatchHandle = null;
  }
}

/**
 * Records the live-stream observations the resync triggers read (design
 * D6), BEFORE the event folds — the ping's fold is a no-op, so this is the
 * only place the observation is taken. Runs on every path that applies live
 * events (ingest and the page-load buffer, where the subscribe catch-up
 * sync lands): the delta count every beacon compares against, the beacon's
 * counter and liveness stamp, and the sync's received-count rebaseline
 * (without it every post-resync ping would read as a permanent gap). A
 * beacon whose counter exceeds the received count fires the healing resync
 * directly (trigger (a)).
 */
function trackStreamEvent(e: SessionEvent): void {
  if (e.kind === Kind.KindTextDelta) {
    streamTurnTracking(e.turn_id).received += 1;

    return;
  }

  if (e.kind === Kind.KindStreamPing) {
    const rec = streamTurnTracking(e.turn_id);
    const count = e.deltas ?? 0;

    rec.lastPing = { count, at: Date.now() };
    lastStreamObservation = Date.now();
    stalenessFired = false;

    if (count > rec.received) requestResync();

    return;
  }

  if (e.kind === Kind.KindStreamSync) {
    const rec = streamTurnTracking(e.turn_id);

    rec.received = e.stream?.deltas ?? rec.received;
    lastStreamObservation = Date.now();
    stalenessFired = false;
  }
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

/**
 * Replaces the running set with the backend's authoritative answer
 * (add-grouped-session-rail design D2): the boot-time seed and the window-
 * focus re-query (the design risk table's "dots self-heal") both ride this.
 * Keep-going: a failed query leaves the current set for the turns:changed
 * stream to keep honest.
 */
async function syncRunningSessions(): Promise<void> {
  try {
    const running = await client.chat.runningSessions();

    useChatStore.setState({ runningSessions: new Set(running) });
  } catch {
    // Keep the last known set; the next turn flip or focus re-query retries.
  }
}

/** Folds one group's busy flag into the rail's set (add-agent-groups design
 * D9): busy = any task of the group with a member turn in flight. */
function setGroupBusy(groupID: string, busy: boolean): void {
  const prev = useChatStore.getState().busyGroups;

  if (prev.has(groupID) === busy) return;

  const next = new Set(prev);

  if (busy) next.add(groupID);
  else next.delete(groupID);
  useChatStore.setState({ busyGroups: next });
}

/** Applies one group's fetched task list: the open group's task rows update,
 * and the rail's busy dot follows any running task. */
function applyGroupTasks(groupID: string, tasks: AgentGroupTask[]): void {
  if (useChatStore.getState().activeGroupID === groupID) {
    useChatStore.setState({ groupTasks: tasks });
  }

  setGroupBusy(
    groupID,
    tasks.some((t: AgentGroupTask): boolean => t.State === "running")
  );
}

/** Fetches one group's tasks and applies them (the live task-updated path).
 * Keep-going: a failed fetch keeps the last known state. */
async function refreshTasksFor(groupID: string): Promise<void> {
  try {
    const tasks = await client.groups.tasks(groupID);

    applyGroupTasks(groupID, tasks);
  } catch {
    // Keep the last known tasks; the next event or refresh retries.
  }
}

/** Upserts one live transcript row into the OPEN group's projection
 * (add-agent-groups design D9): a new MessageID appends; a known one (a
 * copy-back replace after a regenerate, design D7) replaces in place. The
 * rendered entries re-map from the rows — the mapping is pure and the
 * MessageID-keyed entry ids keep the render stable. Events for a group that
 * is not open are ignored (its transcript loads on open). */
function ingestGroupMessage(groupID: string, row: AgentGroupMessage): void {
  const state = useChatStore.getState();

  if (state.activeGroupID !== groupID) return;

  const known = seenGroupMessageIDs.has(row.MessageID);
  let rows: AgentGroupMessage[];

  if (known) {
    rows = state.groupMessages.map((m: AgentGroupMessage): AgentGroupMessage =>
      m.MessageID === row.MessageID ? row : m
    );
  } else {
    seenGroupMessageIDs.add(row.MessageID);
    rows = [...state.groupMessages, row];
  }

  useChatStore.setState({
    groupMessages: rows,
    entries: mapGroupMessages(rows),
  });
}

/** Live handler factory: one closure per subscribed group (design D9). The
 * transcript ingest stays active-group-only; task updates refresh the busy
 * dot for EVERY group and the open group's task rows. */
function makeGroupIngest(groupID: string): (event: GroupEvent) => void {
  return (event: GroupEvent): void => {
    if (event.kind === "message-appended" && event.message) {
      ingestGroupMessage(groupID, event.message);

      return;
    }

    if (event.kind === "task-updated") {
      void refreshTasksFor(groupID);
    }
  };
}

/** Aligns the per-group live subscriptions with the known group list (design
 * D9): new groups subscribe, removed groups unsubscribe. Called from
 * refreshGroups once the list has landed. */
function syncGroupSubscriptions(groups: AgentGroup[]): void {
  const known = new Set(groups.map((g: AgentGroup): string => g.ID));

  for (const [id, off] of groupSubscriptions) {
    if (!known.has(id)) {
      off();
      groupSubscriptions.delete(id);
    }
  }

  for (const group of groups) {
    if (!groupSubscriptions.has(group.ID)) {
      groupSubscriptions.set(
        group.ID,
        client.groups.onGroupEvent(group.ID, makeGroupIngest(group.ID))
      );
    }
  }
}

/**
 * Wires the group surfaces' live sources (add-agent-groups design D9):
 * `groups:changed` refetches the list (and re-aligns the per-group event
 * subscriptions), and each group's own channel feeds the rail's busy dots
 * and the open group's transcript. Returns the combined unsubscribe. Called
 * once from the chat feature root's mount effect, beside
 * connectRunningSessions — the store module stays free of import-time side
 * effects.
 */
export function connectGroups(): Unsubscribe {
  try {
    const offChanged = client.events.onGroupsChanged((): void => {
      void useChatStore.getState().refreshGroups();
    });

    return (): void => {
      offChanged();

      for (const off of groupSubscriptions.values()) off();

      groupSubscriptions.clear();
    };
  } catch {
    // Keep-going at the seam: the client is injectable and the suites mock
    // only the members they drive, so a transport without the additive
    // groups channel opts out of the wiring instead of failing the mount.
    // The real client always carries it, and nothing else has subscribed
    // when this fires.
    return (): void => undefined;
  }
}

/**
 * Wires the running-dot sources that live outside the per-session ingest
 * path (add-grouped-session-rail design D2/D3): the backend's turns:changed
 * channel — one event per in-flight flip for ANY session, subscribed or not
 * — plus the window-focus re-query. Returns the combined unsubscribe.
 *
 * Called once from the chat feature root's mount effect (index.tsx), beside
 * the schedules:changed subscription: the store module must stay free of
 * import-time side effects, and the view is the feature's mounting surface.
 */
export function connectRunningSessions(): Unsubscribe {
  const offTurns = client.events.onTurnsChanged(
    (event: TurnsChangedEvent): void => {
      setSessionRunning(event.sessionId, event.busy);
    }
  );

  // Window focus (add-grouped-session-rail design D3, plus
  // enhance-resumable-streams design D6 trigger (c)): the rail re-queries
  // the running set, and a BUSY session gets one resync — a throttled or
  // suspended background window can miss beacons wholesale, so focus regain
  // is the one moment the counter comparison cannot cover on its own.
  const refetch = (): void => {
    void syncRunningSessions();

    if (useChatStore.getState().busy) requestResync();
  };

  window.addEventListener("focus", refetch);

  return (): void => {
    offTurns();
    window.removeEventListener("focus", refetch);
  };
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

/** The live path's fold (task 5.1): dedupe by event id, then either batch
 * the per-token delta kinds for the frame's single commit or — every other
 * kind — drain the pending batch first and fold this event immediately, so
 * arrival order survives and turn terminals stay synchronous. The replay
 * loops (selectSession / loadEarlier) fold with applyEvent directly and
 * never enter here; the page-load `eventBuffer` above intercepts live
 * events before this while a session's initial page is in flight. */
function ingest(e: SessionEvent): void {
  if (e.id) {
    if (seenEventIDs.has(e.id)) return;
    seenEventIDs.add(e.id);
  }

  // Stream-resume bookkeeping (enhance-resumable-streams design D6) runs
  // before the fold: a ping's fold is a no-op, so this is the only place
  // its observation is taken, and a sync's rebaseline must land before the
  // pings that follow it.
  trackStreamEvent(e);

  if (isCoalescedKind(e.kind)) {
    pendingDeltas.push(e);
    scheduleDeltaFlush();

    return;
  }

  flushPendingDeltas();

  useChatStore.setState((prev: ExtendedChatStore) => ({
    entries: applyEvent(prev.entries, e),
  }));

  // add-live-agent-avatar design D4: record the terminal as an edge for the
  // avatar hook. ingest runs only on the live path behind makeIngest's
  // active-session guard — a background session's terminal never lands here
  // — and the replay folds in selectSession / loadEarlier apply events
  // directly, so a reloaded transcript can never mint an outcome. A fresh
  // object per terminal: the hook reacts to the outcome's identity.
  if (
    e.kind === Kind.KindTurnCompleted ||
    e.kind === Kind.KindTurnError ||
    e.kind === Kind.KindTurnCancelled
  ) {
    useChatStore.setState({
      lastTurnOutcome: {
        turnID: e.turn_id ?? "",
        kind: e.kind,
        at: e.occurred_at || new Date().toISOString(),
      },
    });
    // A turn may have authored skills into its agent's workspace; the
    // picker re-reads the tier at every terminal so a fresh skill is
    // invocable without an agent switch.
    const terminalAgentID = useChatStore.getState().activeAgentID;

    if (terminalAgentID !== null) void loadAgentSkills(terminalAgentID);
  }

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

  if (turnID) {
    s.finishedTurnIDs.add(turnID);
    // The turn's stream tracking dies with it (enhance-resumable-streams
    // design D6): the next turn counts from zero.
    streamTurns.delete(turnID);
  }
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

/** The attachment metadata rows one group send rides in with (add-agent-groups
 * design D4): the ready uploads' capability keys resolve back through the
 * chip tray the composer filled — the send binding carries full attachment
 * metadata, not bare keys. */
function groupAttachments(keys: string[]): SessionAttachment[] {
  if (keys.length === 0) return [];

  const chips = useAttachmentStore.getState().chips;
  const attachments: SessionAttachment[] = [];

  for (const key of keys) {
    const chip = chips.find((c: AttachmentChip): boolean => c.key === key);

    if (chip)
      attachments.push({
        key,
        name: chip.name,
        mime: chip.mime,
        size: chip.size,
      });
  }

  return attachments;
}

/** The group send path (add-agent-groups design D4): the message routes
 * through the groups Send binding — the explicit recipient the mention
 * picker recorded rides along and clears with the send. The transcript
 * updates through the group's live events (the user row and any coordinator
 * line arrive as message-appended events), so no local echo is minted. */
async function dispatchGroup(text: string, keys: string[]): Promise<void> {
  const groupID = useChatStore.getState().activeGroupID;

  if (!groupID) return;

  useChatStore.setState({ dispatching: true });
  setBusy();

  try {
    await client.groups.send({
      GroupID: groupID,
      Text: text,
      Attachments: groupAttachments(keys),
      RecipientAgentID: useChatStore.getState().groupRecipient ?? "",
      ReplyToMessageID: "",
    });
    useChatStore.setState({ groupRecipient: null });
  } catch (err: unknown) {
    useChatStore.setState({
      error: errorMessage(err, "Failed to send message"),
    });
  }

  useChatStore.setState({ dispatching: false });
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

    groups: [],
    activeGroupID: null,
    groupMembers: [],
    groupMessages: [],
    groupTasks: [],
    busyGroups: new Set<string>(),
    groupRecipient: null,

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

    // add-live-agent-avatar design D4: no outcome until a live terminal of
    // the viewed session arrives.
    lastTurnOutcome: null,

    toolCatalog: {},

    init: async (): Promise<void> => {
      try {
        const [agents, providers, skills] = await Promise.all([
          client.agents.list(),
          client.settings.providers(),
          client.settings.skills(),
        ]);

        registrySkills = skills;
        set({
          agents,
          providers,
          // The registry tier only: the workspace tier rides the agent
          // selection that follows (loadAgentSkills).
          skills: pickerSkills(null, []),
        });
      } catch (err: unknown) {
        set({ error: errorMessage(err, "Failed to load chat") });

        return;
      }

      // Boot-time sync of the rail's running dots (add-grouped-session-rail
      // design D2): the backend's authoritative answer replaces the empty
      // set while the first selection loads. Keep-going: a failed seed
      // leaves the empty set for turns:changed and the focus re-query.
      void syncRunningSessions();

      // The rail's groups section (add-agent-groups design D9): the list and
      // the busy dots load beside the first selection; keep-going on failure.
      void get().refreshGroups();

      const target = useNavStore.getState().chatTarget;

      if (target) {
        useNavStore.getState().clearChatTarget();

        // A group target (design D9) opens the group conversation.
        if (target.groupID) {
          await get().openGroup(target.groupID);

          return;
        }

        if (
          target.agentID !== undefined &&
          (target.agentID !== get().activeAgentID ||
            (target.sessionID && target.sessionID !== get().activeSessionID))
        ) {
          await get().selectAgent(target.agentID, target.sessionID);
        }

        return;
      }

      if (!get().activeAgentID && get().agents.length > 0) {
        await get().selectAgent(get().agents[0].ID);
      }
    },

    selectAgent: async (
      agentID: string,
      sessionID?: string | null
    ): Promise<void> => {
      const seq = ++loadSeq;
      eventBuffer = null;
      clearPendingDeltas();
      // Leaving the group surface (add-agent-groups design D9): any agent-
      // session selection closes the open group and its state goes with it.
      seenGroupMessageIDs = new Set<string>();
      set({
        activeAgentID: agentID,
        error: null,
        activeGroupID: null,
        groupMessages: [],
        groupMembers: [],
        groupTasks: [],
        groupRecipient: null,
      });
      // The composer's model/effort pickers read the agent's provider
      // capabilities; a stale continuation must not overwrite a newer agent's.
      void loadModels(agentID);
      // The skill picker re-merges for the newly active agent: its workspace
      // tier is per-agent (loadAgentSkills).
      void loadAgentSkills(agentID);
      let sessions: Session[] = [];

      try {
        // One all-agents listing replaces the per-agent fetch (add-grouped-
        // session-rail design D1): the store holds every agent's sessions
        // and the rail groups the rows by their AgentID.
        sessions = sortSessions(await client.chat.allSessions());
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

      // add-grouped-session-rail design D3: the keep-set now spans every
      // agent's sessions, so this teardown loop no longer fires on agent
      // switches — subscriptions persist across them, and the running dots
      // no longer depend on subscriptions (turns:changed drives the dots
      // for any unsubscribed session).
      const keep = new Set(sessions.map((s: Session): string => s.ID));

      for (const [id, off] of subscriptions) {
        if (!keep.has(id)) {
          off();
          void client.chat.unsubscribe(id);
          subscriptions.delete(id);
        }
      }

      // The opened session must belong to the agent being selected — with
      // the all-agents list in the store, the plain newest row could belong
      // to another agent.
      const agentSessions = sessions.filter(
        (s: Session): boolean => s.AgentID === agentID
      );

      // An explicit null forces the fresh-chat state (the rail's + button):
      // no session is picked, none is created — the composer starts empty
      // and the send path mints the session on the first send. Subscriptions
      // persist (the keep-set teardown above already closed orphans), so a
      // background turn in the previously open session keeps streaming and
      // its rail dot keeps working.
      if (sessionID === null) {
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
          // Nothing to react for (add-live-agent-avatar design D4).
          lastTurnOutcome: null,
          // The override state resets to inherit; a newly created session is
          // seeded from the sticky default instead.
          modelOverride: "",
          effortOverride: "",
          // The windowing state restarts with the fresh chat, same as
          // selectSession's (task 6.1): the session just left may have paged
          // history, and a surviving marker would offer "load earlier" on a
          // transcript with nothing earlier to load.
          oldestSeq: null,
          loadingEarlier: false,
        });
        setBusy();

        return;
      }

      const next =
        sessionID && agentSessions.some((s: Session) => s.ID === sessionID)
          ? sessionID
          : agentSessions[0]?.ID;

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
          // No session to react for (add-live-agent-avatar design D4).
          lastTurnOutcome: null,
          // No open session: the override state resets to inherit; a newly
          // created session is seeded from the sticky default instead.
          modelOverride: "",
          effortOverride: "",
          // The windowing state restarts here too: an agent with no sessions
          // lands on the same session-less transcript as the rail's +.
          oldestSeq: null,
          loadingEarlier: false,
        });
      }
    },

    selectSession: async (sessionID: string): Promise<void> => {
      const seq = ++loadSeq;
      eventBuffer = { sessionID, seq, events: [] };
      clearPendingDeltas();

      seenEventIDs = new Set<string>();
      // The stream-resume tracking belongs to the session being left
      // (enhance-resumable-streams design D6): re-entering a mid-stream
      // session re-observes from the catch-up sync's rebaseline.
      streamTurns = new Map<string, StreamTurnTracking>();
      lastStreamObservation = 0;
      stalenessFired = false;
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
        // Reactions are edges (add-live-agent-avatar design D4): the session
        // being opened starts with no live outcome — replaying its
        // transcript below must never fire a gesture.
        lastTurnOutcome: null,
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

          // The buffered live events carry the same resume bookkeeping as
          // the ingest path (enhance-resumable-streams design D6): the
          // catch-up sync's rebaseline lands before its pings, which the
          // buffer's arrival order already guarantees.
          trackStreamEvent(e);

          entries = applyEvent(entries, e);

          if (isTerminalKind(e.kind) && e.turn_id) {
            get().finishedTurnIDs.add(e.turn_id);
          }
        }

        set({ entries, oldestSeq: oldestSeqOf(events), loadingSession: false });

        const turnState = await client.chat.turnState(sessionID);

        if (seq !== loadSeq) return;

        // add-browser-login-handover, spec "Expired pending request": a
        // transcript ending in a bare input_required while the session is NOT
        // busy (cold restart — the pending-value map is gone) folds expired
        // client-side, the same normalization as the live busy→false
        // transition. A busy (live-parked) session keeps the card pending.
        // Client-side only: the log stays append-only-from-turns.
        if (!turnState.busy) {
          set({
            entries: expireUnresolvedInputs(get().entries),
          });
        }

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

    // Opens a group conversation (add-agent-groups design D9, task 5.1):
    // the group row, roster, newest transcript page, and tasks load together,
    // the projection maps into the shared entries, and the surface switches —
    // activeSessionID clears so the composer targets the group. The loadSeq
    // guard mirrors selectAgent/selectSession: a superseded load must not
    // write state.
    openGroup: async (groupID: string): Promise<void> => {
      const seq = ++loadSeq;
      eventBuffer = null;
      clearPendingDeltas();
      seenEventIDs = new Set<string>();
      seenGroupMessageIDs = new Set<string>();
      set({
        activeGroupID: groupID,
        activeAgentID: null,
        activeSessionID: null,
        entries: [],
        groupMessages: [],
        groupMembers: [],
        groupTasks: [],
        groupRecipient: null,
        usage: null,
        loadingSession: true,
        error: null,
        queue: [],
        dispatching: false,
        activeTurnID: null,
        finishedTurnIDs: new Set<string>(),
        modelOverride: "",
        effortOverride: "",
        oldestSeq: null,
        loadingEarlier: false,
        panelTabs: [],
        activePanelTabID: null,
        lastTurnOutcome: null,
      });
      setBusy();

      try {
        const [group, members, page, tasks] = await Promise.all([
          client.groups.get(groupID),
          client.groups.members(groupID),
          client.groups.transcriptPage(groupID, 0, TRANSCRIPT_PAGE_SIZE),
          client.groups.tasks(groupID),
        ]);

        if (seq !== loadSeq) return;

        seenGroupMessageIDs = new Set(
          page.map((m: AgentGroupMessage): string => m.MessageID)
        );

        set({
          groups: upsertGroupRow(get().groups, group),
          groupMembers: members,
          groupMessages: page,
          groupTasks: tasks,
          entries: mapGroupMessages(page),
          loadingSession: false,
          oldestSeq: groupOldestSeqOf(page),
        });

        // The opened group's live channel rides the same seam as the rail's
        // subscriptions (design D9): ensure it is listening even when
        // refreshGroups has not run yet.
        syncGroupSubscriptions(get().groups);
      } catch (err: unknown) {
        if (seq !== loadSeq) return;

        set({
          error: errorMessage(err, "Failed to load group"),
          loadingSession: false,
        });
      }
    },

    // Creates a group (spec "Group entity and membership", scenario "Create
    // a group with members"): the binding writes the row and its initial
    // roster (the first member becomes the lead backend-side), then
    // refreshGroups aligns the rail's list and per-group subscriptions and
    // openGroup opens the created group's conversation — the new group
    // opens as a conversation, exactly the spec scenario. A failed create
    // records the store error and resolves null (the caller keeps its form
    // and announces it); the surface is unchanged.
    createGroup: async (
      name: string,
      title: string,
      instructions: string,
      memberAgentIDs: string[] | null
    ): Promise<AgentGroup | null> => {
      let group: AgentGroup;

      try {
        group = await client.groups.create(
          name,
          title,
          instructions,
          memberAgentIDs
        );
      } catch (err: unknown) {
        set({ error: errorMessage(err, "Failed to create group") });

        return null;
      }

      await get().refreshGroups();
      await get().openGroup(group.ID);

      return group;
    },

    // The rail's groups list and busy dots (add-agent-groups design D9):
    // fired at boot and on every groups:changed. One tasks read per group
    // (groups are few) feeds the busy set; the open group's task rows ride
    // the same pass. Keep-going: a failed fetch keeps the current list.
    refreshGroups: async (): Promise<void> => {
      try {
        const groups = await client.groups.list();

        const taskLists = await Promise.all(
          groups.map(async (g: AgentGroup): Promise<AgentGroupTask[]> => {
            try {
              return await client.groups.tasks(g.ID);
            } catch {
              return [];
            }
          })
        );

        const busy = new Set<string>();

        groups.forEach((g: AgentGroup, i: number): void => {
          if (
            taskLists[i].some(
              (t: AgentGroupTask): boolean => t.State === "running"
            )
          ) {
            busy.add(g.ID);
          }
        });

        useChatStore.setState({ groups, busyGroups: busy });
        syncGroupSubscriptions(groups);

        const activeID = useChatStore.getState().activeGroupID;

        if (activeID !== null) {
          const idx = groups.findIndex(
            (g: AgentGroup): boolean => g.ID === activeID
          );

          if (idx !== -1) useChatStore.setState({ groupTasks: taskLists[idx] });

          // The open group's roster rides the same pass: member and lead
          // mutations fire groups:changed (design D5), so the mention
          // picker's source stays fresh without a second subscription.
          try {
            const members = await client.groups.members(activeID);

            if (useChatStore.getState().activeGroupID === activeID) {
              useChatStore.setState({ groupMembers: members });
            }
          } catch {
            // Keep the last known roster; the next event or open retries.
          }
        }
      } catch {
        // Keep the last known list; the next groups:changed retries.
      }
    },

    // The open group's task rows (the live task-updated path and the task
    // controls' post-mutation refresh share this).
    refreshGroupTasks: async (): Promise<void> => {
      const groupID = get().activeGroupID;

      if (!groupID) return;

      await refreshTasksFor(groupID);
    },

    // The mention picker's explicit recipient (design D4): recorded between
    // the pick and the send, cleared with it.
    setGroupRecipient: (agentID: string | null): void => {
      set({ groupRecipient: agentID });
    },

    // Older history pages backward through the SAME reducer (task 6.1,
    // design D6): the older page folds into a fresh list — merging it into
    // the live window would wrongly extend trailing streaming entries — and
    // mergePrepended then joins the segments, dropping the boundary turn's
    // leftover streaming entry so it renders as one unbroken entry. With a
    // group open the same windowing contract pages the group transcript
    // (add-agent-groups design D6).
    loadEarlier: async (): Promise<void> => {
      const s = get();

      if (s.activeGroupID !== null) {
        await loadGroupEarlierPage();

        return;
      }

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

      // Only the viewed session's deltas can be pending (makeIngest's
      // active-session guard); a background delete must not drain them.
      if (useChatStore.getState().activeSessionID === sessionID) {
        clearPendingDeltas();
      }

      setSessionRunning(sessionID, false);

      const state = get();

      // The follow-up selection stays inside the deleted session's agent
      // (add-grouped-session-rail design D1: the store now holds every
      // agent's sessions, so the plain newest remaining row could belong to
      // another agent's group). Read the row's AgentID before the delete.
      const agentID = state.sessions.find(
        (s: Session): boolean => s.ID === sessionID
      )?.AgentID;

      const remaining = state.sessions.filter(
        (s: Session): boolean => s.ID !== sessionID
      );

      set({ sessions: remaining });

      if (state.activeSessionID !== sessionID) return;

      const agentRows = agentID
        ? remaining.filter((s: Session): boolean => s.AgentID === agentID)
        : [];

      const next = agentRows[0]?.ID;

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
        // Nothing left to react for (add-live-agent-avatar design D4).
        lastTurnOutcome: null,
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

    // Creation routes through selectAgent for BOTH same-agent and
    // cross-agent creation (add-grouped-session-rail design D6): the
    // refetched all-agents listing returns the persisted row — with the
    // seeded overrides already applied — so no optimistic insert is needed.
    // Bare calls (the composer's send-path auto-create, the rail's active-
    // group button) default to the active agent.
    newSession: async (agentID?: string): Promise<void> => {
      const target = agentID ?? get().activeAgentID;

      if (!target) return;

      try {
        const session = await client.chat.createSession(target);
        // Seed the new session with the agent's last-used composer override
        // (task 2.1, design D9); with no sticky default the row stays empty
        // and simply inherits the agent's configuration.
        const seed = storedOverride(target);

        if (seed) {
          try {
            await client.chat.setSessionOverrides(
              session.ID,
              seed.model,
              seed.effort
            );
          } catch (err: unknown) {
            // The session stays usable unseeded (it inherits); the failure
            // still surfaces instead of silently dropping the default.
            set({ error: errorMessage(err, "Failed to apply defaults") });
          }
        }

        await get().selectAgent(target, session.ID);
      } catch (err: unknown) {
        set({ error: errorMessage(err, "Failed to create session") });
      }
    },

    // Fresh chat without minting a session (the rail's + button): the view
    // clears to the empty state and the send path auto-creates the session
    // on the first send (task 3.4) — exactly the boot state of a
    // session-less agent, so pressing + never leaves an Untitled row behind.
    newChat: async (agentID?: string): Promise<void> => {
      const target = agentID ?? get().activeAgentID;

      if (!target) return;

      await get().selectAgent(target, null);
    },

    // Attachment keys ride the send (task 5.4, design D5): ready uploads
    // pass their capability keys; an attachment-only message (no text) is
    // sendable. While busy, the message queues with its keys (the module
    // map — QueuedMessage carries text only) and dispatches in order. With
    // a group open the send routes through the groups binding instead
    // (add-agent-groups design D4) — group sends carry no client-tracked
    // turn, so there is nothing to queue behind.
    send: async (text: string, attachmentKeys?: string[]): Promise<void> => {
      const trimmed = text.trim();
      const keys = attachmentKeys ?? [];

      if (!trimmed && keys.length === 0) return;

      if (get().activeGroupID !== null) {
        await dispatchGroup(trimmed, keys);

        return;
      }

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
    // firing for ANY agent emits schedules:changed, and the chat feature
    // refetches the all-agents listing through this action so the session
    // appears in the rail without a reload (add-grouped-session-rail design
    // D1). The `agentID` argument is kept for the caller's signature and
    // ignored — the refetch updates every group's count. The loadSeq guard
    // mirrors selectAgent: a store load that superseded this fire must not
    // be overwritten, and a failed fetch keeps the current list
    // (keep-going).
    refreshSessions: async (_agentID: string): Promise<void> => {
      const seq = loadSeq;

      try {
        const sessions = sortSessions(await client.chat.allSessions());

        if (seq !== loadSeq) return;
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

    // `null` is the pinned identity tab (design D1): the existing reducers
    // already read it correctly — closing a non-active tab leaves it, the
    // last close yields it, and every reset path writes it.
    activatePanelTab: (id: string | null): void => {
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
