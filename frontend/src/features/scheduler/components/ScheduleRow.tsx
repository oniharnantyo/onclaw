/**
 * ScheduleRow (task 6.3, design D12): one schedule in the Schedules list —
 * last-run outcome dot with its outcome and time as the label (activating it
 * opens the run's session when one exists), the schedule's name and the
 * stored human-readable recurrence, the agent's avatar, the relative next
 * fire, the enabled toggle, and the edit / run-now / delete actions. The
 * row is presentational: every mutation flows up to the view, which owns
 * the client calls and the refetch.
 */
import { Pencil, Play, Trash2 } from "lucide-react";
import type { ReactElement } from "react";

import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { AgentAvatar, IconButton } from "../../../components";
import {
  StatusDot,
  type StatusDotStatus,
} from "../../../components/ui/StatusDot";
import { Toggle } from "../../../components/ui/Toggle";
import { relativeTime } from "../../../lib/relativeTime";
import { useNavStore } from "../../../stores/nav";
import type { ScheduleView } from "../../../lib/client";

export interface ScheduleRowProps {
  schedule: ScheduleView;
  /** The target agent; null renders the deterministic fallback avatar. */
  agent: Agent | null;
  /** Disables the toggle while its setEnabled call is in flight. */
  toggleBusy: boolean;
  /** Disables run-now while its call is in flight. */
  runBusy: boolean;
  onToggle: (next: boolean) => void;
  onRunNow: () => void;
  onEdit: () => void;
  onDelete: () => void;
}

const MINUTE_MS = 60_000;

const HOUR_MS = 60 * MINUTE_MS;

const DAY_MS = 24 * HOUR_MS;

/** Cutoff where "in Nd" stops being useful and a calendar date takes over. */
const WEEK_MS = 7 * DAY_MS;

const shortDate = new Intl.DateTimeFormat(undefined, {
  day: "numeric",
  month: "short",
});

/** The row's future-facing counter for the next fire ("in 2h", a short date
 * past a week). Disabled schedules and missing stamps read as a dash. */
function formatNextFire(iso: string, now: Date = new Date()): string {
  const at = new Date(iso).getTime();

  if (Number.isNaN(at)) return "—";

  const delta = at - now.getTime();

  if (delta <= 0) return "due now";

  if (delta < MINUTE_MS) return "in <1m";

  if (delta < HOUR_MS) return `in ${Math.floor(delta / MINUTE_MS)}m`;

  if (delta < DAY_MS) return `in ${Math.floor(delta / HOUR_MS)}h`;

  if (delta < WEEK_MS) return `in ${Math.floor(delta / DAY_MS)}d`;

  return shortDate.format(at);
}

/** Outcome → dot color: ran is the success chain, failed danger, missed a
 * warning, and a schedule that never ran stays idle. */
function lastRunDot(status: string): StatusDotStatus {
  if (status === "ran") return "running";

  if (status === "failed") return "error";

  if (status === "missed") return "warn";

  return "idle";
}

export function ScheduleRow({
  schedule,
  agent,
  toggleBusy,
  runBusy,
  onToggle,
  onRunNow,
  onEdit,
  onDelete,
}: ScheduleRowProps): ReactElement {
  const openChat = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.openChat
  );

  const lastRunable = schedule.lastRunSessionId !== "";

  const relative =
    schedule.lastRunAt !== null ? relativeTime(schedule.lastRunAt) : "";

  let lastRunLabel: string;

  if (schedule.lastRunStatus === "ran") lastRunLabel = `ran · ${relative}`;
  else if (schedule.lastRunStatus === "missed")
    lastRunLabel = `missed · ${relative}`;
  else if (schedule.lastRunStatus === "failed")
    lastRunLabel = `failed · ${schedule.lastRunError || relative}`;
  else lastRunLabel = "never run";

  const lastRunWhen =
    schedule.lastRunAt !== null
      ? new Date(schedule.lastRunAt).toLocaleString()
      : "";

  const lastRunTitle =
    schedule.lastRunStatus === "failed" && schedule.lastRunError
      ? `${lastRunWhen}: ${schedule.lastRunError}`
      : lastRunWhen !== ""
        ? `${schedule.lastRunStatus} ${lastRunWhen}`
        : "This schedule has not run yet";

  const openLastRun = (): void => {
    openChat(schedule.agentId, schedule.lastRunSessionId);
  };

  const nextLabel =
    schedule.enabled && schedule.nextFireAt !== null
      ? formatNextFire(schedule.nextFireAt)
      : "—";

  return (
    <div
      data-testid={`schedule-row-${schedule.id}`}
      className="flex items-center gap-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] px-3 py-2.5"
    >
      <AgentAvatar
        id={schedule.agentId}
        avatar={agent?.Avatar ?? null}
        size={32}
      />

      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-[13px] font-medium text-fg">
            {schedule.name}
          </span>
          {lastRunable ? (
            <button
              type="button"
              data-testid={`schedule-last-run-${schedule.id}`}
              title={`${lastRunTitle} — open its session`}
              onClick={openLastRun}
              className="flex min-w-0 shrink-0 items-center gap-1.5 rounded border border-line px-1.5 py-0.5 text-[11px] text-muted transition-colors hover:text-fg"
            >
              <StatusDot status={lastRunDot(schedule.lastRunStatus)} />
              <span className="max-w-[10rem] truncate">{lastRunLabel}</span>
            </button>
          ) : (
            <span
              data-testid={`schedule-last-run-${schedule.id}`}
              title={lastRunTitle}
              className="flex min-w-0 shrink-0 items-center gap-1.5 text-[11px] text-muted"
            >
              <StatusDot status={lastRunDot(schedule.lastRunStatus)} />
              <span className="max-w-[10rem] truncate">{lastRunLabel}</span>
            </span>
          )}
        </div>
        <p
          className="truncate text-[12px] text-muted"
          title={
            agent
              ? `${agent.Name} · ${schedule.description}`
              : schedule.description
          }
        >
          {agent ? `${agent.Name} · ` : ""}
          {schedule.description}
        </p>
      </div>

      <p
        className="shrink-0 text-[12px] text-fg2"
        data-testid={`schedule-next-${schedule.id}`}
      >
        Next: {nextLabel}
      </p>

      <Toggle
        on={schedule.enabled}
        label={
          schedule.enabled ? "Disable this schedule" : "Enable this schedule"
        }
        disabled={toggleBusy}
        testId={`schedule-toggle-${schedule.id}`}
        onChange={onToggle}
      />

      <div className="flex shrink-0 items-center gap-1">
        <IconButton
          label="Edit schedule"
          size="sm"
          testId={`schedule-edit-${schedule.id}`}
          icon={<Pencil aria-hidden="true" className="size-4" />}
          onClick={onEdit}
        />
        <IconButton
          label="Run now"
          size="sm"
          disabled={runBusy}
          testId={`schedule-run-${schedule.id}`}
          icon={<Play aria-hidden="true" className="size-4" />}
          onClick={onRunNow}
        />
        <IconButton
          label="Delete schedule"
          size="sm"
          testId={`schedule-delete-${schedule.id}`}
          icon={<Trash2 aria-hidden="true" className="size-4" />}
          onClick={onDelete}
        />
      </div>
    </div>
  );
}
