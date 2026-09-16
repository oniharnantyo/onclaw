import { useEffect, useMemo, useRef, useState, useImperativeHandle } from 'react';
import { cx, fmtInTz, fmtNextRun, formatDuration, formatTokens, relativeTime } from '../lib/helpers';
import { inputCls, labelCls } from '../components/ui/constants';
import { Segmented } from '../components/ui/Segmented';
import { MicroLabel } from '../components/ui/MicroLabel';
import { Icon } from '../components/ui/Icon';
import { Toggle } from '../components/ui/Toggle';
import { api, ApiError, formatApiError, type ApiChannel } from '../lib/api';
import { heartbeats, type ApiHeartbeat, type HeartbeatPayload } from '../lib/heartbeats';
import {
  buildPresetExpr, cronNext, cronNextRuns, describeCron, formatWallClock, parseCron,
  parseTimeInput, DOW_LABELS, DOW_ORDER,
} from '../lib/cronNext';

// Heartbeat pane (add-agent-heartbeat 6.3): the sixth edit-mode tab of the
// agent config modal, implementing gallery states A1–A7. Config rides the
// modal's Save through the imperative handle below — only Run now and Resume
// act immediately (they are safe single-shot actions the server gates
// independently). States: A1 off (dimmed form, footer hint), A2 on healthy
// (cadence line + tick status), A3 channel delivery picker, A4 friendly
// cadence dropdown (raw cron demoted to an Advanced collapsible with floor
// validation and next-runs preview), A5 auto-paused banner, A6 run-now in
// flight, A7 read-only without agents.write.

type CadenceId =
  | 'every5' | 'every15' | 'every30'
  | 'hourly' | 'every2h' | 'every4h' | 'every6h' | 'every12h'
  | 'daily' | 'weekly';

/** The primary flow is ONE friendly dropdown; fixed-interval options carry
 * their canonical expression, daily/weekly compose from the revealed fields
 * (minutes options start at 5 — the domain floor). */
const CADENCE_OPTIONS: Array<{ id: CadenceId; label: string; expr: string }> = [
  { id: 'every5', label: 'Every 5 minutes', expr: '*/5 * * * *' },
  { id: 'every15', label: 'Every 15 minutes', expr: '*/15 * * * *' },
  { id: 'every30', label: 'Every 30 minutes', expr: '*/30 * * * *' },
  { id: 'hourly', label: 'Every hour', expr: '0 * * * *' },
  { id: 'every2h', label: 'Every 2 hours', expr: '0 */2 * * *' },
  { id: 'every4h', label: 'Every 4 hours', expr: '0 */4 * * *' },
  { id: 'every6h', label: 'Every 6 hours', expr: '0 */6 * * *' },
  { id: 'every12h', label: 'Every 12 hours', expr: '0 */12 * * *' },
  { id: 'daily', label: 'Daily at…', expr: '' },
  { id: 'weekly', label: 'Weekly on…', expr: '' },
];

/** Synthetic, pick-proof dropdown entry shown when the saved expression (or
 * the Advanced cron override) is something the friendly options cannot express. */
const CUSTOM_OPTION_LABEL = 'Custom (cron)';

const MON_FRI: number[] = [1, 2, 3, 4, 5];
const DAY_CHIPS = DOW_ORDER.map((d) => ({ id: d, label: DOW_LABELS[d] }));

const INVALID_EXPR_MSG =
  'Invalid expression — use five fields: minute, hour, day-of-month, month, day-of-week.';
const FLOOR_MSG = 'Minimum cadence is every 5 minutes.';
const TIME_REQUIRED_MSG = 'Pick a time of day.';
const DAY_REQUIRED_MSG = 'Pick at least one day.';
const ACTIVE_PAIR_MSG = 'Active hours need both a start and an end — or neither.';
const ACTIVE_EQUAL_MSG = 'Active hours must not be equal — that window never fires.';
const CHANNEL_REQUIRED_MSG = 'Pick a channel for channel delivery.';

/** Client mirror of the domain floor rule (domain.heartbeat.go): the gap
 * between two successive firings bounds the tick rate, so it must stay at or
 * above five minutes. */
function belowFloor(expr: string, tz: string): boolean {
  const runs = cronNextRuns(expr, new Date(), tz, 2);
  if (runs.length < 2) return false;
  return runs[1].getTime() - runs[0].getTime() < 5 * 60_000;
}

/** The canonical 5-field expression a dropdown selection sends: fixed
 * intervals map directly; daily/weekly compose through the schedule editor's
 * preset builder (same cron day numbers, Sun=0…Sat=6). */
function cadenceExpr(id: CadenceId, atTime: string, days: number[]): string {
  const opt = CADENCE_OPTIONS.find((o) => o.id === id);
  if (opt && opt.expr) return opt.expr;
  if (id === 'daily') {
    // days: [] keeps it plain daily — `M H * * *` with no dow restriction.
    return buildPresetExpr({ freq: 'daily', time: atTime, days: [], dayOfMonth: 1 }) || '';
  }
  return buildPresetExpr({ freq: 'weekly', time: atTime, days, dayOfMonth: 1 }) || '';
}

/** Evenly-spaced step set starting at 0 (a stepped every-N field) → N, else null. */
function stepFromZero(values: Set<number>, span: number): number | null {
  const a = Array.from(values).sort((x, y) => x - y);
  if (a.length < 2 || a[0] !== 0) return null;
  const step = a[1] - a[0];
  if (step < 1) return null;
  for (let i = 1; i < a.length; i++) if (a[i] - a[i - 1] !== step) return null;
  if (a[a.length - 1] + step < span) return null;
  return step;
}

interface CadenceMatch {
  id: CadenceId;
  time?: string;
  days?: number[];
}

/** Maps a stored expression back onto the friendliest dropdown option — the
 * inverse of cadenceExpr. Returns null when no option can express it (the
 * caller then shows `Custom (cron)` with the raw expression in Advanced). */
function cadenceFromExpr(expr: string): CadenceMatch | null {
  const p = parseCron(expr);
  if (!p) return null;
  if (p.dom.restricted || p.month.restricted) return null;
  const mins = Array.from(p.minute.values).sort((x, y) => x - y);
  const hours = Array.from(p.hour.values).sort((x, y) => x - y);

  if (!p.hour.restricted) {
    if (mins.length === 1 && mins[0] === 0) return { id: 'hourly' };
    const step = stepFromZero(p.minute.values, 60);
    if (step === 5) return { id: 'every5' };
    if (step === 15) return { id: 'every15' };
    if (step === 30) return { id: 'every30' };
    return null;
  }
  if (mins.length === 1 && mins[0] === 0) {
    // Stepped hours (every N hours); no match falls through to the
    // single-wall-clock options below (e.g. `0 9 * * 1-5`).
    const step = stepFromZero(p.hour.values, 24);
    if (step === 2) return { id: 'every2h' };
    if (step === 4) return { id: 'every4h' };
    if (step === 6) return { id: 'every6h' };
    if (step === 12) return { id: 'every12h' };
  }
  if (mins.length !== 1 || hours.length !== 1) return null;
  const time = formatWallClock(hours[0], mins[0]);
  if (!p.dow.restricted) return { id: 'daily', time };
  return { id: 'weekly', time, days: Array.from(p.dow.values).sort((x, y) => x - y) };
}

/** Label for the derived cadence line while editing: fixed intervals read
 * better as their dropdown names (describeCron phrases stepped minutes fine
 * but leaves stepped hours untouched); daily/weekly use the cron phraser. */
function editingLabel(cadence: CadenceId, override: string, expr: string): string {
  if (!override) {
    const opt = CADENCE_OPTIONS.find((o) => o.id === cadence);
    if (opt && opt.expr) return opt.label;
  }
  return describeCron(expr);
}

const inLabel = (iso: string): string => {
  const mins = Math.round((Date.parse(iso) - Date.now()) / 60000);
  if (mins < 1) return 'in <1 min';
  if (mins < 60) return `in ${mins} min`;
  return `in ${Math.round(mins / 60)} h`;
};

/** Imperative save handle the modal's Save button rides: the pane PUTs only
 * when the user touched it, so a pristine pane never creates a heartbeat row. */
export interface HeartbeatPaneHandle {
  /** Resolves true when the heartbeat is saved (or nothing needed saving). */
  save: () => Promise<boolean>;
}

interface HeartbeatPaneProps {
  ws: string;
  agentId: string;
  /** Workspace timezone — cadence, previews, and active hours run on it. */
  tz: string;
  /** agents.write gate: readers get the A7 summary, writers the full form. */
  canWrite: boolean;
  /** Keep-alive mount: the pane stays mounted (hidden) across tab switches so
   * edits survive and Save always reaches the handle. */
  hidden?: boolean;
  ref?: React.Ref<HeartbeatPaneHandle>;
}

export function HeartbeatPane({ ws, agentId, tz, canWrite, hidden, ref }: HeartbeatPaneProps) {
  const [serverHb, setServerHb] = useState<ApiHeartbeat | null>(null);
  const [defaultPrompt, setDefaultPrompt] = useState('');
  const [channels, setChannels] = useState<ApiChannel[]>([]);

  // Form state — PUT only on the modal's Save.
  const [enabled, setEnabled] = useState(false);
  const [cadence, setCadence] = useState<CadenceId>('every30');
  const [atTime, setAtTime] = useState('09:00');
  const [weeklyDays, setWeeklyDays] = useState<number[]>(MON_FRI);
  // Raw cron escape hatch: non-empty means it overrides the dropdown, which
  // then displays Custom (cron). The two are never both authoritative.
  const [cronOverride, setCronOverride] = useState('');
  const [advOpen, setAdvOpen] = useState(false);
  const [activeStart, setActiveStart] = useState('');
  const [activeEnd, setActiveEnd] = useState('');
  const [deliveryType, setDeliveryType] = useState<'creator_dm' | 'channel'>('creator_dm');
  const [channelId, setChannelId] = useState('');
  const [prompt, setPrompt] = useState('');

  // Dirty: the user touched the form since load. A pristine pane never PUTs,
  // so merely saving another tab cannot create a heartbeat row by accident.
  const [dirty, setDirty] = useState(false);
  const dirtyRef = useRef(false);
  const touch = () => {
    dirtyRef.current = true;
    setDirty(true);
  };

  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [generalError, setGeneralError] = useState<string | null>(null);
  const [runPending, setRunPending] = useState(false);
  const [runNotice, setRunNotice] = useState<{ text: string; danger: boolean } | null>(null);
  const [resuming, setResuming] = useState(false);

  // Hydrate the form from a read view. Only the initial load calls this —
  // refreshes update the server snapshot (status line, banner, labels) but
  // never clobber pending edits.
  const hydrate = (hb: ApiHeartbeat | null, template: string) => {
    if (!hb) {
      setEnabled(false);
      setCadence('every30');
      setAtTime('09:00');
      setWeeklyDays(MON_FRI);
      setCronOverride('');
      setAdvOpen(false);
      setActiveStart('');
      setActiveEnd('');
      setDeliveryType('creator_dm');
      setChannelId('');
      setPrompt(template);
      return;
    }
    const m = cadenceFromExpr(hb.expr);
    setEnabled(hb.enabled);
    setCadence(m ? m.id : 'every30');
    if (m?.time) setAtTime(m.time);
    if (m?.days) setWeeklyDays(m.days);
    setCronOverride(m ? '' : hb.expr);
    // Advanced opens on hydrate when it holds existing state the user must
    // see: a cron expression the dropdown cannot express, or active hours.
    setAdvOpen(!m || Boolean(hb.active_start || hb.active_end));
    setActiveStart(hb.active_start || '');
    setActiveEnd(hb.active_end || '');
    setDeliveryType(hb.delivery?.type === 'channel' ? 'channel' : 'creator_dm');
    setChannelId(hb.delivery?.channel_id || '');
    setPrompt(hb.prompt || '');
  };

  // Initial read: the heartbeat (or its absence) plus the checklist template.
  useEffect(() => {
    let mounted = true;
    if (!ws || !agentId) return;
    heartbeats
      .get(ws, agentId)
      .then((res) => {
        if (!mounted) return;
        const template = res.default_prompt || '';
        setDefaultPrompt(template);
        setServerHb(res.heartbeat);
        if (!dirtyRef.current) hydrate(res.heartbeat, template);
      })
      .catch(() => {
        // Optional context — the pane stays usable with never-created
        // defaults, matching the modal's other background loads.
      });
    return () => {
      mounted = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ws, agentId]);

  // Channel roster for the A3 delivery picker (and read-only name lookup).
  useEffect(() => {
    let mounted = true;
    if (!ws) return;
    api.channels
      .list(ws)
      .then((res) => {
        if (mounted) setChannels(res.channels || []);
      })
      .catch(() => {});
    return () => {
      mounted = false;
    };
  }, [ws]);

  /** Re-reads the heartbeat after an immediate action (run now / resume). */
  const refresh = async () => {
    try {
      const res = await heartbeats.get(ws, agentId);
      setDefaultPrompt(res.default_prompt || '');
      setServerHb(res.heartbeat);
    } catch {
      // Keep the last known snapshot on refresh failures.
    }
  };

  // ---- Derived values ------------------------------------------------------

  const override = cronOverride.trim();
  const expr = override || cadenceExpr(cadence, atTime, weeklyDays);
  const parsedExpr = useMemo(() => parseCron(expr), [expr]);

  const exprInvalid = Boolean(override) && !parsedExpr;
  const exprBelowFloor = Boolean(override) && !!parsedExpr && belowFloor(expr, tz);

  // Live dropdown gaps: an incomplete daily time or an empty weekly day set
  // cannot build an expression the server would accept.
  const dailyTimeMissing = !override && cadence === 'daily' && !parseTimeInput(atTime);
  const weeklyDayMissing = !override && cadence === 'weekly' && weeklyDays.length === 0;
  const cadenceProblem = dailyTimeMissing ? TIME_REQUIRED_MSG : weeklyDayMissing ? DAY_REQUIRED_MSG : '';

  // A4: three-entry next-runs preview for a valid cron override, exactly
  // like the schedule editor's preview.
  const preview = useMemo(
    () =>
      override && parsedExpr && !exprBelowFloor
        ? cronNextRuns(expr, new Date(), tz, 3)
        : [],
    [override, parsedExpr, exprBelowFloor, expr, tz]
  );

  // Live mirror of the active-hours pair rule (both-or-neither, not equal).
  const activeHoursError =
    (activeStart && !activeEnd) || (!activeStart && activeEnd)
      ? ACTIVE_PAIR_MSG
      : activeStart && activeEnd && activeStart === activeEnd
        ? ACTIVE_EQUAL_MSG
        : '';

  // A2: server-derived label/tick while viewing a saved heartbeat, cron math
  // while the user is editing (or before the first save).
  const cadenceLabel = !dirty && serverHb
    ? serverHb.human_label || describeCron(expr)
    : editingLabel(cadence, override, expr);
  const nextTickIso = useMemo(() => {
    if (!enabled) return null;
    if (!dirty && serverHb) return serverHb.next_tick_at;
    if (!parsedExpr) return null;
    return cronNext(expr, new Date(), tz)?.toISOString() || null;
  }, [enabled, dirty, serverHb, expr, parsedExpr, tz]);

  const paused = Boolean(serverHb && !serverHb.enabled && serverHb.failure_streak > 0);
  const locked = !enabled; // writer branch only; readers never reach it

  // Collapsed-state hint: what the advanced section holds, e.g.
  // "active 09:00–18:00 · cron 0 9 * * 1-5".
  const advSummary = [
    activeStart && activeEnd ? `active ${activeStart}–${activeEnd}` : '',
    override ? `cron ${override}` : '',
  ]
    .filter(Boolean)
    .join(' · ');

  const channelLabel = (id: string | undefined) => {
    if (!id) return 'Channel';
    const c = channels.find((ch) => ch.id === id);
    return c ? `#${c.slug || c.name}` : 'Channel';
  };

  // ---- Actions -------------------------------------------------------------

  const save = async (): Promise<boolean> => {
    setGeneralError(null);
    if (!canWrite || !dirtyRef.current) return true;

    // Client-side mirrors of the domain rules the server enforces on PUT —
    // a blocked save never leaves the browser. Expr and active-hours verdicts
    // already render live next to their inputs; only the ones the form does
    // not show on its own get stored as field errors.
    const exprProblem = !override
      ? ''
      : !parseCron(expr)
        ? INVALID_EXPR_MSG
        : belowFloor(expr, tz)
          ? FLOOR_MSG
          : '';
    const activeProblem =
      (activeStart && !activeEnd) || (!activeStart && activeEnd)
        ? ACTIVE_PAIR_MSG
        : activeStart && activeEnd && activeStart === activeEnd
          ? ACTIVE_EQUAL_MSG
          : '';
    const channelProblem = deliveryType === 'channel' && !channelId ? CHANNEL_REQUIRED_MSG : '';
    if (exprProblem || cadenceProblem || activeProblem || channelProblem) {
      const errors: Record<string, string> = {};
      if (exprProblem) errors.expr = exprProblem;
      if (channelProblem) errors['delivery.channel_id'] = channelProblem;
      setFieldErrors(errors);
      // A rejected cron override hides behind the collapsible — open it.
      if (exprProblem) setAdvOpen(true);
      return false;
    }
    setFieldErrors({});

    const payload: HeartbeatPayload = {
      enabled,
      expr,
      active_start: activeStart && activeEnd ? activeStart : null,
      active_end: activeStart && activeEnd ? activeEnd : null,
      delivery:
        deliveryType === 'channel'
          ? { type: 'channel', channel_id: channelId }
          : { type: 'creator_dm' },
      prompt,
    };
    try {
      const res = await heartbeats.update(ws, agentId, payload);
      dirtyRef.current = false;
      setDirty(false);
      setServerHb(res.heartbeat);
      setFieldErrors({});
      return true;
    } catch (err: unknown) {
      // Fielded 422s land next to the offending control (same keys the
      // server uses: expr, active_start, active_end, delivery.*).
      if (err instanceof ApiError && err.details.length > 0) {
        const fielded: Record<string, string> = {};
        const general: string[] = [];
        for (const d of err.details) {
          if (d.field) fielded[d.field] = d.message || 'Invalid value';
          else general.push(d.message || 'Invalid value');
        }
        setFieldErrors(fielded);
        if (fielded.expr) setAdvOpen(true); // the expr control lives in Advanced
        if (general.length) setGeneralError(general.join(' '));
      } else {
        setGeneralError(formatApiError(err, 'Failed to save the heartbeat'));
      }
      return false;
    }
  };

  // A6: fire one tick now. 409 while a tick is already in flight is a notice,
  // not a failure.
  const runNow = async () => {
    if (runPending) return;
    setRunPending(true);
    setRunNotice(null);
    try {
      await heartbeats.runNow(ws, agentId);
      await refresh();
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 409) {
        setRunNotice({ text: 'A tick is already running.', danger: false });
      } else {
        setRunNotice({ text: formatApiError(err, 'Failed to run the heartbeat tick'), danger: true });
      }
    } finally {
      setRunPending(false);
    }
  };

  // A5: re-enable an auto-paused heartbeat immediately, then re-read.
  const resume = async () => {
    if (resuming) return;
    setResuming(true);
    setGeneralError(null);
    try {
      const res = await heartbeats.resume(ws, agentId);
      setServerHb(res.heartbeat);
      setEnabled(true); // reflect the server's re-enable without marking dirty
      await refresh();
    } catch (err: unknown) {
      setGeneralError(formatApiError(err, 'Failed to resume the heartbeat'));
    } finally {
      setResuming(false);
    }
  };

  useImperativeHandle(ref, () => ({ save }));

  // ---- Shared fragments ----------------------------------------------------

  const last = serverHb?.last_tick ?? null;
  const statusSection = (
    <div
      data-testid="heartbeat-status"
      className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_35%,var(--surface))] px-3 py-2.5"
    >
      <p className="min-w-0 flex-1 text-[12px] text-fg2">
        {last ? (
          <>
            Last tick {relativeTime(last.started_at)} · {last.status}
            {last.delivery_status ? ` · ${last.delivery_status}` : ''} · {formatTokens(last.tokens_used)} tok ·{' '}
            {formatDuration(last.duration_ms)}
          </>
        ) : (
          'No ticks yet'
        )}
        {(serverHb?.failure_streak ?? 0) > 0 && (
          <>
            {' · '}
            <span className="text-danger" data-testid="heartbeat-failures">
              Failures in a row: {serverHb!.failure_streak}
            </span>
          </>
        )}
      </p>
      {canWrite && (
        <button
          type="button"
          onClick={runNow}
          disabled={runPending}
          data-testid="heartbeat-run-now"
          className="flex h-7 shrink-0 items-center gap-1.5 rounded border border-line px-2.5 text-[11px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40"
        >
          {runPending ? 'Running…' : 'Run now'}
        </button>
      )}
    </div>
  );

  // A5 banner — message-only for readers (A7), with actions for writers.
  const pausedBanner = paused ? (
    <div
      data-testid="heartbeat-paused-banner"
      className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] p-3"
    >
      <p className="flex items-center gap-1.5 text-[12px] text-danger">
        <Icon name="alert" size={13} />
        Auto-paused — {serverHb!.failure_streak} ticks failed in a row. Nothing will run until you resume.
      </p>
      {canWrite && (
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={resume}
            disabled={resuming}
            data-testid="heartbeat-resume"
            className="flex h-7 items-center rounded-md bg-accent px-3 text-[11px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] disabled:opacity-40"
          >
            {resuming ? 'Resuming…' : 'Resume'}
          </button>
          <button
            type="button"
            onClick={runNow}
            disabled={runPending}
            data-testid="heartbeat-run-now-banner"
            className="flex h-7 items-center rounded border border-line px-2.5 text-[11px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40"
          >
            Run now
          </button>
        </div>
      )}
    </div>
  ) : null;

  // ---- A7: read-only -------------------------------------------------------

  if (!canWrite) {
    return (
      <div data-testid="agent-heartbeat-pane" className="max-w-xl space-y-6" hidden={hidden}>
        {pausedBanner}
        <div>
          <MicroLabel>Heartbeat</MicroLabel>
          <p className="mt-1 text-[12px] leading-4 text-muted">
            A quiet scheduled check-in. Your agent reads a workspace activity digest and works its checklist — or
            stays silent.
          </p>
        </div>
        {!serverHb ? (
          <p className="text-[12px] text-muted" data-testid="heartbeat-none">
            No heartbeat is configured for this agent.
          </p>
        ) : (
          <>
            <div className="space-y-1.5 text-[12px] text-fg2" data-testid="heartbeat-summary">
              <p>
                <span className="text-muted">Cadence</span> — {serverHb.human_label || serverHb.expr}
              </p>
              <p>
                <span className="text-muted">Next tick</span> —{' '}
                {serverHb.next_tick_at ? fmtNextRun(serverHb.next_tick_at, tz) : 'Not scheduled'}
              </p>
              <p>
                <span className="text-muted">Delivery</span> —{' '}
                {serverHb.delivery?.type === 'channel' ? channelLabel(serverHb.delivery.channel_id) : 'Creator DM'}
              </p>
              <p>
                <span className="text-muted">Active hours</span> —{' '}
                {serverHb.active_start && serverHb.active_end
                  ? `${serverHb.active_start}–${serverHb.active_end} (${tz})`
                  : '24/7'}
              </p>
            </div>
            <div>
              <span className={labelCls}>Checklist</span>
              <div
                data-testid="heartbeat-prompt-preview"
                className="mt-1.5 max-h-72 overflow-auto whitespace-pre-wrap rounded-md border border-line bg-warm p-3 font-mono text-[12px] leading-relaxed text-fg2"
              >
                {serverHb.prompt || 'Empty — the heartbeat skips ticks until a checklist is saved.'}
              </div>
            </div>
            {statusSection}
          </>
        )}
        <p className="text-[11px] text-muted" data-testid="heartbeat-readonly-note">
          You can view this heartbeat but not edit it.
        </p>
      </div>
    );
  }

  // ---- A1–A6: writer -------------------------------------------------------

  const chipCls = (on: boolean) =>
    cx(
      'flex h-8 items-center rounded-md border px-3 text-[12px] font-medium transition-colors',
      on
        ? 'border-accent bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg'
        : 'border-line text-muted hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg2',
      locked && 'cursor-not-allowed opacity-70'
    );

  return (
    <div data-testid="agent-heartbeat-pane" className="max-w-xl space-y-6" hidden={hidden}>
      {pausedBanner}

      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <MicroLabel>Heartbeat</MicroLabel>
          <p className="mt-1 text-[12px] leading-4 text-muted">
            A quiet scheduled check-in. Your agent reads a workspace activity digest and works its checklist — or
            stays silent.
          </p>
        </div>
        <span data-testid="heartbeat-toggle" className="mt-0.5 shrink-0">
          <Toggle
            on={enabled}
            label="Enable heartbeat"
            onChange={(v: boolean) => {
              setEnabled(v);
              touch();
            }}
          />
        </span>
      </div>

      {/* The form stays visible while off (A1) — dimmed and inert until the
          toggle turns on. pointer-events guards the Segmented, which has no
          disabled attribute of its own. */}
      <div className={cx('space-y-5', locked && 'pointer-events-none opacity-60')}>
        <div>
          <label className={labelCls} htmlFor="hb-cadence">
            Cadence
          </label>
          <select
            id="hb-cadence"
            data-testid="heartbeat-cadence-select"
            className={inputCls}
            value={override ? 'custom' : cadence}
            disabled={locked}
            onChange={(e) => {
              const v = e.target.value as CadenceId | 'custom';
              if (v === 'custom') return; // display-only state, set from the Advanced input
              setCadence(v);
              setCronOverride(''); // a dropdown pick always replaces the cron override
              if (v === 'weekly') setWeeklyDays((d) => (d.length ? d : MON_FRI));
              touch();
            }}
          >
            {CADENCE_OPTIONS.map((o) => (
              <option key={o.id} value={o.id}>
                {o.label}
              </option>
            ))}
            {/* Synthetic display-only state, selected only while a raw
                Advanced override is present — never listed otherwise. */}
            {override && (
              <option value="custom" disabled>
                {CUSTOM_OPTION_LABEL}
              </option>
            )}
          </select>

          {(cadence === 'daily' || cadence === 'weekly') && !override && (
            <div className="mt-3 flex items-center gap-2">
              <span className="text-[12px] font-medium text-fg2">At</span>
              <input
                type="time"
                data-testid="heartbeat-cadence-time"
                aria-label="Time of day"
                className={cx(inputCls, 'w-32 font-mono text-[13px]')}
                value={atTime}
                disabled={locked}
                onChange={(e) => {
                  setAtTime(e.target.value);
                  touch();
                }}
              />
            </div>
          )}
          {cadence === 'weekly' && !override && (
            <div className="mt-3">
              <div className="flex flex-wrap gap-2" data-testid="heartbeat-day-chips">
                {DAY_CHIPS.map((d) => {
                  const on = weeklyDays.includes(d.id);
                  return (
                    <button
                      key={d.id}
                      type="button"
                      aria-pressed={on}
                      disabled={locked}
                      data-testid={'heartbeat-day-' + d.label.toLowerCase()}
                      onClick={() => {
                        setWeeklyDays(on ? weeklyDays.filter((x) => x !== d.id) : [...weeklyDays, d.id]);
                        touch();
                      }}
                      className={chipCls(on)}
                    >
                      {d.label}
                    </button>
                  );
                })}
              </div>
              {weeklyDayMissing && (
                <p className="mt-1 text-[11px] text-danger" data-testid="heartbeat-cadence-error">
                  {DAY_REQUIRED_MSG}
                </p>
              )}
            </div>
          )}
          {cadence === 'daily' && dailyTimeMissing && (
            <p className="mt-1 text-[11px] text-danger" data-testid="heartbeat-cadence-error">
              {TIME_REQUIRED_MSG}
            </p>
          )}
          {enabled && expr && (
            <p data-testid="heartbeat-cadence-line" className="mt-2 text-[12px] text-fg2">
              Runs {cadenceLabel}
              {nextTickIso && (
                <>
                  {' '}· Next tick {fmtNextRun(nextTickIso, tz)} ({inLabel(nextTickIso)})
                </>
              )}
            </p>
          )}
        </div>

        {/* Advanced settings (collapsed by default): the both-or-neither active
            hours window and the raw cron escape hatch live here so the primary
            flow stays a single friendly dropdown. */}
        <div>
          <button
            type="button"
            data-testid="heartbeat-advanced-toggle"
            aria-expanded={advOpen}
            disabled={locked}
            onClick={() => setAdvOpen((v) => !v)}
            className="flex items-center gap-1.5 text-[12px] font-medium text-fg2 transition-colors hover:text-fg disabled:opacity-40"
          >
            {advOpen ? '▾' : '▸'} Advanced settings
            {!advOpen && advSummary && <span className="font-normal text-muted">· {advSummary}</span>}
          </button>
          {advOpen && (
            <div
              data-testid="heartbeat-advanced"
              className="mt-3 space-y-4 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_35%,var(--surface))] p-3.5"
            >
              <div>
                <span className={labelCls}>
                  Active hours <span className="font-normal text-muted">(optional · workspace timezone)</span>
                </span>
                <div className="flex items-center gap-2">
                  <input
                    type="time"
                    data-testid="heartbeat-active-start"
                    aria-label="Active hours start"
                    className={cx(inputCls, 'w-32 font-mono text-[13px]')}
                    value={activeStart}
                    disabled={locked}
                    onChange={(e) => {
                      setActiveStart(e.target.value);
                      touch();
                    }}
                  />
                  <span className="text-[12px] text-muted">to</span>
                  <input
                    type="time"
                    data-testid="heartbeat-active-end"
                    aria-label="Active hours end"
                    className={cx(inputCls, 'w-32 font-mono text-[13px]')}
                    value={activeEnd}
                    disabled={locked}
                    onChange={(e) => {
                      setActiveEnd(e.target.value);
                      touch();
                    }}
                  />
                </div>
                {(activeHoursError || fieldErrors.active_start || fieldErrors.active_end) && (
                  <p className="mt-1 text-[11px] text-danger" data-testid="heartbeat-active-error">
                    {activeHoursError || fieldErrors.active_start || fieldErrors.active_end}
                  </p>
                )}
              </div>
              <div>
                <label className={labelCls} htmlFor="hb-expr">
                  Cron expression (overrides the cadence dropdown)
                </label>
                <input
                  id="hb-expr"
                  data-testid="heartbeat-cron-input"
                  className={cx(
                    inputCls,
                    'font-mono text-[13px]',
                    (exprInvalid || exprBelowFloor || fieldErrors.expr) && 'border-danger'
                  )}
                  value={cronOverride}
                  disabled={locked}
                  onChange={(e) => {
                    setCronOverride(e.target.value);
                    touch();
                  }}
                  placeholder="0 9 * * 1-5"
                />
                {exprInvalid || exprBelowFloor ? (
                  <p className="mt-1 text-[11px] text-danger" data-testid="heartbeat-expr-error">
                    {exprBelowFloor ? FLOOR_MSG : INVALID_EXPR_MSG}
                  </p>
                ) : !override ? (
                  <p className="mt-1 text-[11px] text-muted">
                    Standard 5-field cron in the workspace timezone — for cadences the dropdown cannot express.
                  </p>
                ) : (
                  <div className="mt-2" data-testid="heartbeat-next-runs">
                    <p className="text-[11px] uppercase tracking-[0.14em] text-muted">Next runs · {tz}</p>
                    <ul className="mt-1 space-y-0.5">
                      {preview.map((d) => (
                        <li key={d.toISOString()} className="font-mono text-[12px] text-fg2">
                          {fmtInTz(d.toISOString(), tz, {
                            weekday: 'short',
                            month: 'short',
                            day: 'numeric',
                            hour: '2-digit',
                            minute: '2-digit',
                          })}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
                {/* Server-side expr 422s surface here; the client-side floor and
                    shape errors render live above and are not duplicated. */}
                {!exprInvalid && !exprBelowFloor && fieldErrors.expr && (
                  <p className="mt-1 text-[11px] text-danger" data-testid="heartbeat-expr-error">
                    {fieldErrors.expr}
                  </p>
                )}
              </div>
            </div>
          )}
        </div>

        <div>
          <span className={labelCls}>Delivery</span>
          <Segmented
            value={deliveryType}
            onChange={(v: string) => {
              setDeliveryType(v as 'creator_dm' | 'channel');
              touch();
            }}
            options={[
              { id: 'creator_dm', label: 'Creator DM', testid: 'heartbeat-delivery-creator-dm' },
              { id: 'channel', label: 'Channel', testid: 'heartbeat-delivery-channel' },
            ]}
          />
          {deliveryType === 'channel' && (
            <div className="mt-3">
              <label className={labelCls} htmlFor="hb-channel">
                Post to channel
              </label>
              <select
                id="hb-channel"
                data-testid="heartbeat-channel-select"
                className={inputCls}
                value={channelId}
                disabled={locked}
                onChange={(e) => {
                  setChannelId(e.target.value);
                  touch();
                }}
              >
                <option value="">Pick a channel…</option>
                {channels.map((c) => (
                  <option key={c.id} value={c.id}>
                    #{c.slug || c.name}
                  </option>
                ))}
              </select>
              {(fieldErrors['delivery.channel_id'] || fieldErrors['delivery.type']) && (
                <p className="mt-1 text-[11px] text-danger" data-testid="heartbeat-channel-error">
                  {fieldErrors['delivery.channel_id'] || fieldErrors['delivery.type']}
                </p>
              )}
            </div>
          )}
          {deliveryType !== 'channel' && fieldErrors['delivery.type'] && (
            <p className="mt-1 text-[11px] text-danger" data-testid="heartbeat-channel-error">
              {fieldErrors['delivery.type']}
            </p>
          )}
          <p className="mt-1.5 text-[11px] leading-4 text-muted">
            {deliveryType === 'channel'
              ? 'The tick’s reply posts to the channel as the agent.'
              : 'The report lands in your direct messages with the agent.'}
          </p>
        </div>

        <div>
          <div className="flex items-center justify-between">
            <span className={labelCls}>Checklist</span>
            <button
              type="button"
              onClick={() => {
                setPrompt(defaultPrompt);
                touch();
              }}
              disabled={locked}
              data-testid="heartbeat-reset-prompt"
              className="text-[11px] font-medium text-accent transition-colors hover:text-[var(--accent-hover)] disabled:opacity-40"
            >
              Reset to default
            </button>
          </div>
          <textarea
            id="hb-prompt"
            data-testid="heartbeat-prompt"
            rows={5}
            disabled={locked}
            className={cx(inputCls, 'h-auto py-2 leading-relaxed')}
            value={prompt}
            onChange={(e) => {
              setPrompt(e.target.value);
              touch();
            }}
            placeholder="What the agent checks on every heartbeat — plain language, no JSON."
          />
          <p className="mt-1 text-[11px] leading-4 text-muted">
            An empty checklist skips the tick without a model call.
          </p>
        </div>
      </div>

      {statusSection}

      {generalError && (
        <p className="text-[12px] text-danger" data-testid="heartbeat-save-error">
          {generalError}
        </p>
      )}
      {runNotice && (
        <p className={cx('text-[12px]', runNotice.danger ? 'text-danger' : 'text-muted')} data-testid="heartbeat-run-notice">
          {runNotice.text}
        </p>
      )}
      {!enabled && (
        <p className="text-[11px] text-muted" data-testid="heartbeat-off-hint">
          Heartbeat is off — nothing runs until you enable it and save.
        </p>
      )}
    </div>
  );
}
