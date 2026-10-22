/**
 * Day dividers and per-message times (task 3.3, spec chat-view "Day
 * dividers"). The vendored day-separator element is a self-contained demo
 * list, so the thread computes "day changed vs the previous message" in the
 * message components (DayGate reads the message index and the thread's
 * messages off the runtime state) and renders the element's visual pieces —
 * the hairline / mono-label / hairline row and the hover-revealed mono time —
 * above each message. No divider ever renders above the first dated message,
 * and a message's time occupies no space until hover or focus reveals it.
 *
 * The turn separator reads the same boundary (`useDayBoundary`): a message
 * that already carries the divider's hairlines does not also carry the
 * turn's own rule, so the two never stack (`flatten-chat-transcript` D6).
 */
import type { ReactElement } from "react";

import { useAuiState, type AssistantState } from "@assistant-ui/react";

import { cn } from "../../../lib/cn";

const MS_PER_DAY = 86_400_000;

/** The time-of-day face a revealed message time wears. */
const TIME_FORMAT = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
});

const sameLocalDay = (a: Date, b: Date): boolean =>
  a.getFullYear() === b.getFullYear() &&
  a.getMonth() === b.getMonth() &&
  a.getDate() === b.getDate();

/** The divider's label: Today / Yesterday for the adjacent days, the locale
 * date otherwise (year shown only when it differs from the current one).
 * `now` is injectable so tests pin the clock. */
export function dayDividerLabel(at: string, now: Date = new Date()): string {
  const then = new Date(at);

  if (Number.isNaN(then.getTime())) return "";

  if (sameLocalDay(then, now)) return "Today";

  if (sameLocalDay(new Date(then.getTime() + MS_PER_DAY), now)) {
    return "Yesterday";
  }

  const options: Intl.DateTimeFormatOptions = {
    weekday: "long",
    day: "numeric",
    month: "long",
  };

  if (then.getFullYear() !== now.getFullYear()) options.year = "numeric";

  return new Intl.DateTimeFormat(undefined, options).format(then);
}

/** Local calendar-day key — dividers split on the reader's own days. */
function localDayKey(at: Date): string {
  return `${at.getFullYear()}-${at.getMonth()}-${at.getDate()}`;
}

/** The divider between consecutive messages of different calendar days,
 * rendered above a message when its day differs from the previous one's.
 * Reads the current message scope, so it must mount inside
 * ThreadPrimitive.Messages. */
export function DayGate(): ReactElement | null {
  const { at, changed } = useDayBoundary();

  if (!changed) return null;

  return <ChatDayDivider at={at.toISOString()} />;
}

/** The message in the current scope: its own time, and whether it opens a
 * new calendar day. */
export interface DayBoundary {
  /** The message's own timestamp (the epoch when it carries none). */
  at: Date;
  /** Whether the message opens a new calendar day. */
  changed: boolean;
}

/** Whether the message in the current scope opens a new calendar day, and
 * when. The turn separator reads it too (design D6): a message that already
 * carries the day divider's hairlines does not also carry the turn's own
 * rule, so the two never stack. */
export function useDayBoundary(): DayBoundary {
  const index = useAuiState((s: AssistantState) => s.message.index);
  const createdAt = useAuiState((s: AssistantState) => s.message.createdAt);

  const previousAt = useAuiState(
    (s: AssistantState) => s.thread.messages[s.message.index - 1]?.createdAt
  );

  const changed =
    createdAt !== undefined &&
    index > 0 &&
    previousAt !== undefined &&
    localDayKey(createdAt) !== localDayKey(previousAt);

  return { at: createdAt ?? new Date(0), changed };
}

/** The divider row itself: the vendored day-separator's visual pieces with
 * the full timestamp available on the element (title) for exact inspection. */
export function ChatDayDivider({ at }: { at: string }): ReactElement | null {
  const label = dayDividerLabel(at);

  if (label === "") return null;

  return (
    <div
      className="flex items-center gap-2.5 py-1"
      data-testid="day-divider"
      title={at}
    >
      <span aria-hidden="true" className="bg-foreground/[0.08] h-px flex-1" />
      <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
        {label}
      </span>
      <span aria-hidden="true" className="bg-foreground/[0.08] h-px flex-1" />
    </div>
  );
}

/** The message's own time of day, revealed on hover or focus without
 * reserving space (`hidden` until the group hover/focus-within fires).
 * `side` picks which edge of the message it hangs beside: assistant content
 * is left-aligned (time to its right), user bubbles right-aligned (time to
 * their left). Reads the current message scope. */
export function MessageTime({
  side,
}: {
  side: "start" | "end";
}): ReactElement | null {
  const createdAt = useAuiState((s: AssistantState) => s.message.createdAt);

  if (createdAt === undefined) return null;

  return (
    <span
      data-testid="message-time"
      className={cn(
        "pointer-events-none absolute top-1 hidden font-mono text-[10px] tabular-nums text-muted-foreground group-hover/msg:block group-focus-within/msg:block",
        side === "end" ? "start-full ms-2" : "end-full me-2"
      )}
    >
      {TIME_FORMAT.format(createdAt)}
    </span>
  );
}
