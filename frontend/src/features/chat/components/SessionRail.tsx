/**
 * Sessions rail (tasks 2.1–2.2, 6.5, 7.1–7.2): the active agent's sessions in
 * a column INSIDE the content island — the island's own hairline border
 * (border-e) separates it from the conversation; the shell mounts it as the
 * conversation column's left neighbor. Header with the new-session
 * affordance and a client-side title filter. Rows are compact 26px
 * title-only kit SideRows (design D11) — the relative-time meta line is
 * dropped; an entry whose session has a turn in flight (the per-session
 * running set, which includes a turn paused on an approval — tracked for
 * every subscribed session, not just the open one) shows the running
 * indicator inline: a spinner before the title with the title pulsing, both
 * static under prefers-reduced-motion. The unfiltered list caps at the four
 * most recent sessions plus the active one when it is older, behind "Show N
 * older sessions" / "Show recent only" controls; the revealed list scrolls
 * within a max height once more than eight rows are visible. A non-empty
 * search lifts the cap and lists every match. Each entry carries a hover
 * delete affordance (spec chat-view "Sessions organized per agent"): a
 * type-agnostic confirmation dialog guards it; the backend's refusal for a
 * session with a turn in flight surfaces as a danger toast. Navigation
 * stays on the shell rail; switching sessions here reloads that session's
 * history through the store. Hidden below md — the mobile chat header takes
 * over session switching. At lg+ the column clears the native
 * invisible-titlebar band (the window's first 25px drag; pt-2 plus the
 * label's own mt-4 puts the first row at the same height as the chat
 * header's). When the nav rail is collapsed, this column also hosts the
 * rail's expand control in a row above the label — the rail's own 80px
 * shell is reserved for the traffic lights. The column is drag-resizable by
 * its right edge (the same pattern as the right preview panel): the width
 * persists under `buddies.sessionRailWidth` and clamps to sane bounds, so a
 * stray drag can never squeeze the column away or swallow the conversation.
 */
import { useRef, useState } from "react";
import type {
  ChangeEvent,
  CSSProperties,
  PointerEvent as ReactPointerEvent,
  ReactElement,
} from "react";
import { PanelLeftOpen, Plus, Search, Trash2 } from "lucide-react";

import type { Session } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { IconButton } from "../../../components";
import { cn } from "../../../lib/cn";
import { SectionLabel } from "../../../components/ui/SectionLabel";
import { SideRow } from "../../../components/ui/SideRow";
import { toast } from "../../../lib/toasts";
import { useLocalNumberPreference } from "../../../lib/useLocalPreference";
import { useNavStore } from "../../../stores/nav";
import { useChatStore, type ExtendedChatStore } from "../store";
import type { ChatStore } from "../types";

const RAIL_WIDTH_KEY = "buddies.sessionRailWidth";

const DEFAULT_RAIL_WIDTH = 224;

const MIN_RAIL_WIDTH = 176;

const MAX_RAIL_WIDTH = 400;

/** The unfiltered list's recent cap (design D11): the four most recent
 * sessions, plus the active one when it is older than those. */
const RECENT_CAP = 4;

/** The revealed list scrolls within a max height once more than eight rows
 * are visible (design D11): 8 × 26px rows plus a hint of the ninth. */
const LIST_MAX_CLASS = "max-h-[228px]";

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

  const selectSession = useChatStore((s: ChatStore) => s.selectSession);
  const newSession = useChatStore((s: ChatStore) => s.newSession);
  const deleteSession = useChatStore((s: ExtendedChatStore) => s.deleteSession);
  const [query, setQuery] = useState("");
  // The session whose delete confirmation is open (one at a time).
  const [deleting, setDeleting] = useState<Session | null>(null);
  // Whether the older sessions beyond the recent cap are revealed (design
  // D11); a non-empty search ignores the cap entirely.
  const [showAll, setShowAll] = useState(false);
  // Read once at mount: the OS setting does not change mid-session, and a
  // static row must not start spinning late.
  const [reducedMotion] = useState<boolean>(prefersReducedMotion);

  const asideRef = useRef<HTMLElement | null>(null);

  const [width, setWidth] = useLocalNumberPreference(
    RAIL_WIDTH_KEY,
    DEFAULT_RAIL_WIDTH
  );

  const onResizeStart = (e: ReactPointerEvent<HTMLDivElement>): void => {
    e.preventDefault();
    const aside = asideRef.current;

    if (!aside) return;

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
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
  };

  const needle = query.trim().toLowerCase();
  const searching = needle.length > 0;

  // Client-side filter only: narrowing the list must never move the active
  // session or touch the store (spec: Session search filter).
  const matches = sessions.filter((session: Session): boolean =>
    (session.Title || "Untitled session").toLowerCase().includes(needle)
  );

  // The list grammar (design D11): a non-empty search lifts the cap and
  // lists every match; unfiltered, the four most recent show — plus the
  // active session when it is older than those — until "Show N older
  // sessions" reveals the rest.
  let visible: Session[];

  if (searching || showAll) {
    visible = matches;
  } else {
    const recent = matches.slice(0, RECENT_CAP);

    const active = matches.find(
      (session: Session): boolean => session.ID === activeSessionID
    );

    if (
      active &&
      !recent.some((session: Session): boolean => session.ID === active.ID)
    ) {
      recent.push(active);
    }

    visible = recent;
  }

  const olderCount = Math.max(0, matches.length - visible.length);

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
      className="relative hidden w-[var(--rail-width)] shrink-0 flex-col border-e border-line md:flex lg:pt-2"
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
      <SectionLabel
        action={
          <IconButton
            label="New session"
            size="sm"
            icon={<Plus aria-hidden="true" className="size-4" />}
            testId="btn-new-session"
            disabled={!activeAgentID}
            onClick={() => void newSession()}
          />
        }
      >
        Sessions
      </SectionLabel>
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
      <div
        className={cn(
          "od-scroll min-h-0 flex-1 overflow-y-auto px-1.5 pb-2",
          visible.length > 8 && LIST_MAX_CLASS
        )}
        data-testid="session-list"
      >
        {visible.map((session: Session) => {
          const running = runningSessions.has(session.ID);

          return (
            // Compact 26px title-only rows (design D11): the child-selector
            // overrides shrink the kit SideRow's 30px button; the running
            // pulse rides the label's truncate span. Both motion classes
            // drop under prefers-reduced-motion (static spinner, static
            // title).
            <div
              key={session.ID}
              className={cn(
                "group relative [&>button]:h-[26px]",
                running && !reducedMotion && "[&_.truncate]:animate-pulse"
              )}
            >
              <SideRow
                odId={`chat-session-${session.ID}`}
                active={session.ID === activeSessionID}
                title={session.Title || undefined}
                label={session.Title || "Untitled session"}
                icon={
                  running ? (
                    <span
                      aria-hidden="true"
                      data-testid={`session-running-${session.ID}`}
                      className={cn(
                        "h-3 w-3 shrink-0 rounded-full border-2 border-muted/40",
                        !reducedMotion && "animate-spin border-t-muted"
                      )}
                    />
                  ) : undefined
                }
                onClick={() => void selectSession(session.ID)}
              />
              {/* Delete affordance (task 6.5): a sibling of the row button,
                  so no nested-button HTML; revealed on hover/focus. */}
              <button
                type="button"
                aria-label={`Delete ${session.Title || "Untitled session"}`}
                title="Delete session"
                data-testid={`session-delete-${session.ID}`}
                onClick={() => setDeleting(session)}
                className="absolute end-1.5 top-1/2 flex h-5 w-5 -translate-y-1/2 items-center justify-center rounded text-muted opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100 hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-danger"
              >
                <Trash2 aria-hidden="true" className="size-3" />
              </button>
            </div>
          );
        })}
        {sessions.length === 0 && (
          <p className="px-2.5 py-1.5 text-[12px] text-muted">
            No sessions yet
          </p>
        )}
        {sessions.length > 0 && visible.length === 0 && (
          <p className="px-2.5 py-1.5 text-[12px] text-muted">
            No matching sessions
          </p>
        )}
      </div>
      {/* Cap controls (design D11): unfiltered lists longer than the recent
          cap offer the expander; a revealed list offers the way back. A
          non-empty search hides both — every match is already listed. */}
      {!searching && matches.length > RECENT_CAP && (
        <div className="flex justify-center px-3 pb-2">
          <button
            type="button"
            data-testid={showAll ? "btn-show-recent" : "btn-show-older"}
            onClick={() => setShowAll((v: boolean) => !v)}
            className="rounded px-2 py-0.5 text-[11px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg2"
          >
            {showAll
              ? "Show recent only"
              : `Show ${olderCount} older session${olderCount === 1 ? "" : "s"}`}
          </button>
        </div>
      )}
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
