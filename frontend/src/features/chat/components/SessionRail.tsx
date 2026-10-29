/**
 * Sessions rail (add-grouped-session-rail, tasks 4.1–4.4, design D4/D5):
 * every agent's sessions in one accordion column INSIDE the content island —
 * the island's own hairline border (border-e) separates it from the
 * conversation; the shell mounts it as the conversation column's left
 * neighbor. Groups derive from the store's all-agent `sessions` joined with
 * `agents` (design D5): the active agent first, then agents with sessions by
 * their most recent session activity, session-less agents last (name-sorted
 * among themselves). Each group row shows the agent's avatar, name, session
 * count, a per-agent new-session "+" that works from collapsed and expanded
 * rows alike and never requires the agent to be active first (design D6),
 * and a group running dot when any of the agent's sessions has a turn in
 * flight (the backend-fed app-wide running set — including turns parked on
 * approvals and sessions never opened here). Exactly one group is expanded
 * at a time (rail-local state, design D4): the active agent's; any context
 * move (a session click, create-and-open, the boot restore) overwrites the
 * expansion through an effect on `activeAgentID`, while clicking a collapsed
 * row expands it without touching the store — the open conversation is
 * unchanged. The expanded group lists that agent's sessions as compact 26px
 * rows (design D11's density carries over) — title truncated, the
 * last-activity relative time right-aligned on the same line (the critique
 * wave restored it: title-only rows left dozens of sessions
 * indistinguishable) — the four most recent plus the agent's active session
 * when it is older, behind a progressive "Load more sessions" control that
 * appends batches of eight and retires once the agent's sessions are
 * exhausted; once the revealed history outgrows the column, the group's rows
 * scroll in a region of their own beneath the pinned agent row — the agent
 * row never scrolls away, only the session list does — while a search lists
 * every match across all agents with the column scrolling as one, matching
 * groups expanded, the rest collapsed rows (the per-group window counts
 * reset when a search starts). The tree reads as a tree (impeccable
 * hierarchy pass): the group row carries the container voice — medium-weight
 * full-strength name over the children's regular muted rows, a mono count, a
 * disclosure chevron pointing right collapsed and down expanded — and the
 * session rows indent under it, their titles aligning beneath the group
 * name. The group's trailing corner holds the always-visible per-agent "+"
 * beside the dot + count + chevron metadata — the row reserves the corner
 * with trailing padding, so the primary action never hides behind a hover.
 * An entry whose session has a turn in
 * flight shows the running indicator inline: a spinner before the title with
 * the title pulsing, both static under prefers-reduced-motion. Each entry
 * carries a hover delete affordance (spec chat-view "Sessions organized per
 * agent") — a 24px hit area around the small glyph (design D6) — and a
 * type-agnostic confirmation dialog guards it; the backend's refusal for a
 * session with a turn in flight surfaces as a danger toast.
 * Navigation stays on the shell rail; selecting a session under any agent
 * swaps the whole conversation context through `selectAgent`. Hidden below
 * md — the mobile chat header takes over session switching. At lg+ the
 * column clears the native invisible-titlebar band (the window's first 25px
 * drag; pt-2 plus the label's own mt-4 puts the first row at the same height
 * as the chat header's). When the nav rail is collapsed, this column also
 * hosts the rail's expand control in a row above the label — the rail's own
 * 80px shell is reserved for the traffic lights. The column is drag-resizable
 * by its right edge (the same pattern as the right preview panel): the width
 * persists under `buddies.sessionRailWidth` and clamps to sane bounds, so a
 * stray drag can never squeeze the column away or swallow the conversation.
 * Structural motion follows the shared grammar (design D8): the column's
 * inline width animates on `--motion-base` + `--ease-standard` with the
 * transition dropped while a drag is live so it tracks the pointer 1:1, and
 * each group's content reveals through a `grid-template-rows` 0fr↔1fr
 * transition — the chevron's rotation rides the same tokens, in step with
 * the reveal. Reduced motion needs no branches: the global contract
 * collapses every transition to opacity-only, which snaps these moves.
 */
import { useEffect, useRef, useState } from "react";
import type {
  ChangeEvent,
  CSSProperties,
  PointerEvent as ReactPointerEvent,
  ReactElement,
} from "react";
import { ChevronDown, PanelLeftOpen, Plus, Search, Trash2 } from "lucide-react";

import type {
  Agent,
  Session,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { AgentAvatar, IconButton } from "../../../components";
import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { cn } from "../../../lib/cn";
import { SectionLabel } from "../../../components/ui/SectionLabel";
import { SideRow } from "../../../components/ui/SideRow";
import { StatusDot } from "../../../components/ui/StatusDot";
import { toast } from "../../../lib/toasts";
import { relativeTime } from "../../../lib/relativeTime";
import { useLocalNumberPreference } from "../../../lib/useLocalPreference";
import { useNavStore } from "../../../stores/nav";
import { useChatStore, type ExtendedChatStore } from "../store";
import type { ChatStore } from "../types";

const RAIL_WIDTH_KEY = "buddies.sessionRailWidth";

const DEFAULT_RAIL_WIDTH = 224;

const MIN_RAIL_WIDTH = 176;

const MAX_RAIL_WIDTH = 400;

/** A group's initial window (design D4): the four most recent sessions,
 * plus the agent's active one when it is older than those. */
const RECENT_CAP = 4;

/** Each "Load more sessions" activation appends the next batch (design D4). */
const LOAD_MORE_BATCH = 8;

/**
 * One agent's accordion group (design D5): the agent's session rows in store
 * order (most recent activity first) and whether any of them has a turn in
 * flight (the group running dot).
 */
interface SessionGroup {
  agent: Agent;
  sessions: Session[];
  running: boolean;
}

/**
 * Reads the OS reduce-motion setting. Environments without the API (jsdom,
 * very old webviews) count as motion allowed — the running spinner and
 * pulse are the default experience and the query is only ever an
 * enhancement.
 */
function prefersReducedMotion(): boolean {
  try {
    return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  } catch {
    // No matchMedia in this environment.
    return false;
  }
}

export function SessionRail(): ReactElement {
  const activeAgentID = useChatStore((s: ChatStore) => s.activeAgentID);
  const agents = useChatStore((s: ChatStore) => s.agents);
  const sessions = useChatStore((s: ChatStore) => s.sessions);
  const activeSessionID = useChatStore((s: ChatStore) => s.activeSessionID);

  const runningSessions = useChatStore(
    (s: ExtendedChatStore) => s.runningSessions
  );

  const railCollapsed = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.railCollapsed
  );

  const toggleRail = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.toggleRail
  );

  const selectAgent = useChatStore((s: ChatStore) => s.selectAgent);
  const newSession = useChatStore((s: ChatStore) => s.newSession);
  const deleteSession = useChatStore((s: ExtendedChatStore) => s.deleteSession);
  const [query, setQuery] = useState("");
  // The session whose delete confirmation is open (one at a time).
  const [deleting, setDeleting] = useState<Session | null>(null);

  // Accordion state (design D4): rail-local, anchored to the active agent —
  // exactly one group expanded at a time. A context move (activeAgentID
  // change) overwrites it via the effect below; a collapsed-row click sets
  // it without touching the chat store.
  const [expandedAgentID, setExpandedAgentID] = useState<string | null>(
    activeAgentID
  );

  // Progressive load-more (design D4): per-agent visible window sizes;
  // absent means the initial window (the four most recent plus the agent's
  // active session when it is older). Survives expand/collapse; reset when
  // a search becomes non-empty, which lists every match anyway.
  const [visibleCounts, setVisibleCounts] = useState<Record<string, number>>(
    {}
  );

  // Read once at mount: the OS setting does not change mid-session, and a
  // static row must not start spinning late.
  const [reducedMotion] = useState<boolean>(prefersReducedMotion);

  const asideRef = useRef<HTMLElement | null>(null);

  // Live only between a resize handle's pointerdown and its release (design
  // D8): the width transition drops while it is up so the drag tracks the
  // pointer 1:1, and re-arms on release.
  const [dragging, setDragging] = useState(false);

  const [width, setWidth] = useLocalNumberPreference(
    RAIL_WIDTH_KEY,
    DEFAULT_RAIL_WIDTH
  );

  const needle = query.trim().toLowerCase();
  const searching = needle.length > 0;

  // Accordion anchor (design D4): a context move (an activeAgentID change —
  // session click, create-and-open, boot restore) expands the moved-to
  // agent's group and collapses the previously expanded one; clearing the
  // search restores the same grammar around the active agent. While the
  // search is up every matching group is expanded instead (design D5) and
  // the per-group load-more windows reset — a search lists every match.
  // The store edit arrives from outside the component, so an effect is the
  // only seam (no local event owns it); the reset is rail-local view state.
  useEffect(() => {
    if (searching) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setVisibleCounts({});
    } else {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setExpandedAgentID(activeAgentID);
    }
  }, [searching, activeAgentID]);

  const onResizeStart = (e: ReactPointerEvent<HTMLDivElement>): void => {
    e.preventDefault();
    const aside = asideRef.current;

    if (!aside) return;

    // Design D8: a live drag must track the pointer 1:1 — the width
    // transition drops for the drag's duration (the aside's className) and
    // re-arms when the pointer lets go (or the drag is cancelled).
    setDragging(true);

    // The handle rides the rail's right edge, so widening follows the
    // pointer rightward: width = pointer − left edge, clamped.
    const move = (ev: PointerEvent): void => {
      setWidth(
        Math.min(
          MAX_RAIL_WIDTH,
          Math.max(
            MIN_RAIL_WIDTH,
            Math.round(ev.clientX - aside.getBoundingClientRect().left)
          )
        )
      );
    };

    const stop = (): void => {
      setDragging(false);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
    window.addEventListener("pointercancel", stop);
  };

  // Client-side filter only: narrowing the list must never move the active
  // session or touch the store (spec: Session search filter).
  const matchesSearch = (session: Session): boolean =>
    (session.Title || "Untitled session").toLowerCase().includes(needle);

  // Groups (design D5): the store's `sessions` holds every agent's rows;
  // join them by AgentID with the agents for name/avatar. The store hands
  // them over sorted most recent activity first, so each agent's first row
  // carries its latest activity.
  const byAgent = new Map<string, Session[]>();

  for (const session of sessions) {
    const list = byAgent.get(session.AgentID);

    if (list) list.push(session);
    else byAgent.set(session.AgentID, [session]);
  }

  const withSessions: SessionGroup[] = [];
  const sessionless: SessionGroup[] = [];

  for (const agent of agents) {
    const list = byAgent.get(agent.ID) ?? [];

    const group: SessionGroup = {
      agent,
      sessions: list,
      running: list.some((s: Session): boolean => runningSessions.has(s.ID)),
    };

    if (list.length > 0) withSessions.push(group);
    else sessionless.push(group);
  }

  withSessions.sort(
    (a: SessionGroup, b: SessionGroup): number =>
      (Date.parse(b.sessions[0]?.LastActivityAt ?? "") || 0) -
      (Date.parse(a.sessions[0]?.LastActivityAt ?? "") || 0)
  );
  sessionless.sort((a: SessionGroup, b: SessionGroup): number =>
    (a.agent.Name || a.agent.ID).localeCompare(b.agent.Name || b.agent.ID)
  );

  // The active agent leads whether or not it has sessions (design D5).
  const activeGroup =
    withSessions.find(
      (g: SessionGroup): boolean => g.agent.ID === activeAgentID
    ) ??
    sessionless.find(
      (g: SessionGroup): boolean => g.agent.ID === activeAgentID
    );

  const groups: SessionGroup[] = [];

  if (activeGroup) groups.push(activeGroup);

  for (const group of withSessions) {
    if (group !== activeGroup) groups.push(group);
  }

  for (const group of sessionless) {
    if (group !== activeGroup) groups.push(group);
  }

  // While searching, every group with a match shows its matches; otherwise
  // exactly the expanded agent's group is open (design D4/D5).
  const isExpanded = (group: SessionGroup): boolean =>
    searching
      ? group.sessions.some((s: Session): boolean => matchesSearch(s))
      : group.agent.ID === expandedAgentID;

  // A group's visible rows: every match while searching; otherwise the
  // agent's window — the four most recent plus the agent's active session
  // when it falls outside them (design D4).
  const plainWindow = (group: SessionGroup): Session[] => {
    const shown = visibleCounts[group.agent.ID] ?? RECENT_CAP;
    const visible = group.sessions.slice(0, shown);

    const active = group.sessions.find(
      (s: Session): boolean => s.ID === activeSessionID
    );

    if (active && !visible.some((s: Session): boolean => s.ID === active.ID)) {
      visible.push(active);
    }

    return visible;
  };

  const visibleSessions = (group: SessionGroup): Session[] =>
    searching
      ? group.sessions.filter((s: Session): boolean => matchesSearch(s))
      : plainWindow(group);

  // The load-more control retires once the agent's sessions fit the window
  // (design D4); a search lists every match, so it hides while searching.
  const showsLoadMore = (group: SessionGroup): boolean =>
    !searching &&
    (visibleCounts[group.agent.ID] ?? RECENT_CAP) < group.sessions.length;

  // The grouped empty state (spec: Session search filter): with agents and
  // sessions present but no match under the needle, nothing expanded means
  // nothing matched anywhere.
  const noMatches =
    sessions.length > 0 &&
    searching &&
    groups.every((group: SessionGroup): boolean => !isExpanded(group));

  const confirmDelete = (): void => {
    const session = deleting;

    if (!session) return;
    setDeleting(null);
    void deleteSession(session.ID).then((): void => {
      // A refusal (the backend rejects a session with a turn in flight)
      // leaves the row in place — announce it; success needs no toast, the
      // entry simply disappears.
      const gone = !useChatStore
        .getState()
        .sessions.some((s: Session): boolean => s.ID === session.ID);

      if (!gone) {
        toast(
          useChatStore.getState().error ??
            "This session has a turn in flight — cancel it first",
          "danger"
        );
      }
    });
  };

  return (
    <aside
      ref={asideRef}
      className={cn(
        "relative hidden w-[var(--rail-width)] shrink-0 flex-col border-e border-line md:flex lg:pt-2",
        // Design D8: the column's inline width animates on the shared
        // motion grammar — but a live drag must track the pointer 1:1, so
        // the transition drops while dragging and re-arms on release.
        // Reduced motion needs no branch: the global contract collapses
        // every transition to opacity-only, which snaps this move.
        dragging
          ? "transition-none"
          : "transition-[width] duration-[var(--motion-base)] ease-[var(--ease-standard)]"
      )}
      style={
        // SAFETY: the rail reads its width through the CSS custom property
        // in the width class; React's style typing does not model custom
        // properties, so the record is asserted once here.
        { "--rail-width": `${width}px` } as CSSProperties
      }
      data-testid="chat-session-rail"
    >
      {/* Rail expand control (nav store): when the nav rail is collapsed its
          80px shell is reserved for the traffic lights, so the way back
          lives here — a row above the SESSIONS label. lg-only: below lg the
          nav rail is hidden entirely, so there is nothing to expand. */}
      {railCollapsed && (
        <div className="hidden items-center px-3 lg:flex lg:pt-4">
          <IconButton
            label="Expand navigation"
            size="sm"
            testId="btn-toggle-rail"
            icon={<PanelLeftOpen aria-hidden="true" className="size-4" />}
            onClick={toggleRail}
          />
        </div>
      )}
      {/* The header carries no action of its own (add-grouped-session-rail
          task 4.2): the per-agent "+" on each group row replaces the flat
          list's single new-session button. */}
      <SectionLabel>Sessions</SectionLabel>
      <div className="px-3 pb-2">
        <label className="relative block">
          <Search
            aria-hidden="true"
            className="pointer-events-none absolute start-2.5 top-1/2 size-4 -translate-y-1/2 text-muted"
          />
          <input
            type="text"
            className="h-8 w-full rounded-md border border-line bg-surface ps-8 pe-2.5 text-[13px] text-fg2 placeholder:text-muted focus:border-accent"
            placeholder="Search sessions"
            aria-label="Search sessions"
            data-testid="session-search"
            value={query}
            onChange={(e: ChangeEvent<HTMLInputElement>) =>
              setQuery(e.target.value)
            }
          />
        </label>
      </div>
      {/* The column is a flex stack of groups: the open group grows to the
          height left over and scrolls its rows internally (see `pinned`
          below), the collapsed rows stay fixed, and the container's own
          scroll is the fallback for a search view or more agents than fit. */}
      <div
        className="od-scroll flex min-h-0 flex-1 flex-col overflow-y-auto px-1.5 pb-2"
        data-testid="session-list"
      >
        {groups.map((group: SessionGroup) => {
          const expanded = isExpanded(group);

          // Rows stay mounted through both states so the 0fr close keeps
          // content height to animate; a collapsed group freezes its plain
          // window, an expanded one renders matches (search) or the window.
          const visible = expanded
            ? visibleSessions(group)
            : plainWindow(group);

          // Non-search grammar opens exactly one group: it pins its agent
          // row and scrolls its session rows in the column's leftover
          // height. A search expands every matching group at natural height
          // instead — the column scrolls as one.
          const pinned = expanded && !searching;

          return (
            // Tight inside a group, generous between groups (impeccable
            // hierarchy pass): every group after the first gets a small top
            // gap so the accordion's blocks separate at a glance.
            <div
              key={group.agent.ID}
              className={cn(
                "flex min-h-0 flex-col",
                group !== groups[0] && "mt-1.5",
                pinned &&
                  // The tiny floor keeps the pinned region (and its
                  // scrollbar) usable even when many collapsed agents crowd
                  // the column and the outer container must scroll too.
                  "min-h-[52px] flex-1"
              )}
            >
              {/* Group row (design D5): the container voice — medium-weight
                  full-strength name (child-selector override over SideRow's
                  regular label), mono count, disclosure chevron, and the
                  group running dot when any of the agent's sessions has a
                  turn in flight. The per-agent "+" (a sibling of the row
                  button, so no nested-button HTML) sits always-visible in
                  the trailing corner the row reserves via padding, beside
                  the metadata — no hover gating on the primary action.
                  Clicking the row expands the group and nothing else. */}
              <div
                className="group relative [&>button:first-child]:pe-9 [&_.truncate]:font-medium [&_.truncate]:text-fg"
                data-testid={`agent-group-${group.agent.ID}`}
              >
                <SideRow
                  odId={`chat-agent-group-${group.agent.ID}`}
                  label={group.agent.Name || group.agent.ID}
                  expanded={expanded}
                  icon={
                    <AgentAvatar
                      id={group.agent.ID}
                      avatar={group.agent.Avatar}
                      size={18}
                    />
                  }
                  sub={
                    <span className="flex shrink-0 items-center gap-1">
                      {group.running && (
                        // Group running dot (design D5): any of the agent's
                        // sessions has a turn in flight — the backend-fed
                        // app-wide set, so never-opened sessions light it
                        // too.
                        <span
                          className="flex shrink-0"
                          data-testid={`group-running-${group.agent.ID}`}
                        >
                          <StatusDot status="running" live={!reducedMotion} />
                        </span>
                      )}
                      <span
                        className="font-mono text-[11px] tabular-nums text-muted"
                        data-testid={`group-count-${group.agent.ID}`}
                      >
                        {group.sessions.length}
                      </span>
                      <ChevronDown
                        aria-hidden="true"
                        className={cn(
                          // Same tokens as the reveal it fronts (design
                          // D8): the rotation stays in step with the
                          // grid-rows transition.
                          "size-3 text-muted transition-transform duration-[var(--motion-base)] ease-[var(--ease-standard)]",
                          !expanded && "-rotate-90"
                        )}
                      />
                    </span>
                  }
                  onClick={() => setExpandedAgentID(group.agent.ID)}
                />
                <button
                  type="button"
                  aria-label={`New session for ${group.agent.Name || group.agent.ID}`}
                  title="New session"
                  data-testid={`group-new-session-${group.agent.ID}`}
                  onClick={() => void newSession(group.agent.ID)}
                  className="absolute end-1 top-1/2 flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
                >
                  <Plus aria-hidden="true" className="size-4" />
                </button>
              </div>
              {/* Accordion reveal (design D8): the group's content
                  expands/collapses through a grid-template-rows 0fr ↔ 1fr
                  transition on the shared motion grammar — no raw height
                  animation. The grid row item (min-h-0, overflow-hidden)
                  does the clipping so the 0fr track collapses fully while
                  the scroll region inside it keeps working. The rows stay
                  mounted through both states — bounded by the group's own
                  load-more window, never every session — so the 1fr→0fr
                  close animates with inherent content height instead of
                  snapping empty; a collapsed group's rows carry inert +
                  aria-hidden (toggled in the commit, React-18-safe), so
                  they are neither focusable nor announced (display:none
                  would kill the transition). Reduced motion needs no
                  branch: the global contract collapses every transition to
                  opacity-only. */}
              <div
                className={cn(
                  "grid transition-[grid-template-rows] duration-[var(--motion-base)] ease-[var(--ease-standard)]",
                  pinned && "min-h-0 flex-1"
                )}
                style={{ gridTemplateRows: expanded ? "1fr" : "0fr" }}
              >
                <div className="flex min-h-0 flex-col overflow-hidden">
                  <div
                    ref={(el: HTMLDivElement | null): void => {
                      // React 18 does not reliably model a conditional
                      // `inert` prop; the attribute toggles in the commit,
                      // keeping collapsed rows unfocusable for keyboard
                      // and assistive tech while the track animates.
                      if (el) el.toggleAttribute("inert", !expanded);
                    }}
                    className={cn(
                      pinned
                        ? // The open group's rows scroll in the height the
                          // column has left over while the agent row above
                          // stays pinned — only the session list scrolls.
                          "flex-1 overflow-y-auto od-scroll"
                        : // Collapsed track: rows ride the 0fr clip; a search
                          // lists matches at natural height and the column
                          // scrolls as one.
                          "shrink-0"
                    )}
                    aria-hidden={!expanded || undefined}
                    data-testid={`group-sessions-${group.agent.ID}`}
                  >
                    {visible.map((session: Session) => {
                      const running = runningSessions.has(session.ID);

                      return (
                        // Compact 26px rows indented under the group so child
                        // titles align beneath the group name (impeccable
                        // hierarchy pass): the child-selector overrides shrink
                        // the kit SideRow's 30px button; the running pulse
                        // rides the label's truncate span. Both motion classes
                        // drop under prefers-reduced-motion (static spinner,
                        // static title). A click under any agent swaps the
                        // whole conversation context (design D6).
                        <div
                          key={session.ID}
                          className={cn(
                            "group relative ml-[26px] [&>button]:h-[26px]",
                            running &&
                              !reducedMotion &&
                              "[&_.truncate]:animate-pulse"
                          )}
                        >
                          <SideRow
                            odId={`chat-session-${session.ID}`}
                            active={session.ID === activeSessionID}
                            title={session.Title || undefined}
                            label={session.Title || "Untitled session"}
                            sub={
                              <span
                                className="shrink-0 text-[11px] tabular-nums text-muted"
                                data-testid={`session-meta-${session.ID}`}
                              >
                                {relativeTime(session.LastActivityAt)}
                              </span>
                            }
                            icon={
                              running ? (
                                <span
                                  aria-hidden="true"
                                  data-testid={`session-running-${session.ID}`}
                                  className={cn(
                                    "h-3 w-3 shrink-0 rounded-full border-2 border-muted/40",
                                    !reducedMotion &&
                                      "animate-spin border-t-muted"
                                  )}
                                />
                              ) : undefined
                            }
                            onClick={() =>
                              void selectAgent(group.agent.ID, session.ID)
                            }
                          />
                          {/* Delete affordance (task 6.5): a sibling of the
                            row button, so no nested-button HTML; revealed on
                            hover/focus. The 24px hit area wraps the small
                            glyph rather than enlarging it (design D6, task
                            1.3). */}
                          <button
                            type="button"
                            aria-label={`Delete ${session.Title || "Untitled session"}`}
                            title="Delete session"
                            data-testid={`session-delete-${session.ID}`}
                            onClick={() => setDeleting(session)}
                            className="absolute end-1.5 top-1/2 flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded text-muted opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100 hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-danger"
                          >
                            <Trash2 aria-hidden="true" className="size-3" />
                          </button>
                        </div>
                      );
                    })}
                    {showsLoadMore(group) && (
                      // Child-aligned quiet control (impeccable hierarchy
                      // pass): indented with the session rows it extends,
                      // reading as part of the list rather than a floating
                      // caption.
                      <div className="ml-[26px] flex pb-1 pt-0.5">
                        <button
                          type="button"
                          data-testid={`group-load-more-${group.agent.ID}`}
                          onClick={() =>
                            setVisibleCounts(
                              (prev: Record<string, number>) => ({
                                ...prev,
                                [group.agent.ID]:
                                  (prev[group.agent.ID] ?? RECENT_CAP) +
                                  LOAD_MORE_BATCH,
                              })
                            )
                          }
                          className="flex h-6 items-center gap-1 rounded-md px-2 text-[11px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg2"
                        >
                          <ChevronDown aria-hidden="true" className="size-3" />
                          Load more sessions
                        </button>
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </div>
          );
        })}
        {sessions.length === 0 && (
          <p className="px-2.5 py-1.5 text-[12px] text-muted">
            No sessions yet
          </p>
        )}
        {noMatches && (
          <p className="px-2.5 py-1.5 text-[12px] text-muted">
            No matching sessions
          </p>
        )}
      </div>
      {deleting && (
        <ConfirmDialog
          title="Delete session?"
          testId="confirm-delete-session"
          confirmTestId="btn-confirm-delete-session"
          cancelTestId="btn-cancel-delete-session"
          onConfirm={confirmDelete}
          onClose={() => setDeleting(null)}
        >
          <p className="text-[13px] text-muted">
            “{deleting.Title || "Untitled session"}” and its transcript will be
            removed. This cannot be undone.
          </p>
        </ConfirmDialog>
      )}
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize sessions list"
        onPointerDown={onResizeStart}
        className="absolute inset-y-0 right-0 z-10 hidden w-1.5 cursor-col-resize hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] md:block"
        data-testid="session-rail-resize"
      />
    </aside>
  );
}
