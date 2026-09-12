import { useMemo, useState } from "react";
import { cx } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { Modal } from "../components/ui/Modal";
import { Toggle } from "../components/ui/Toggle";
import { Segmented } from "../components/ui/Segmented";
import { inputCls, labelCls } from "../components/ui/constants";
import {
  buildPresetExpr, cronNextRuns, describeCron, instantToWallTime, parseCron,
  presetFromExpr, wallTimeToInstant, DOW_ORDER, DOW_LABELS,
  type RecurrenceFreq,
} from "../lib/cronNext";
import { fmtInTz } from "../lib/helpers";
import { schedulers, type Scheduler } from "../lib/schedulers";
import { useStore } from "../store";
import { formatApiError } from "../lib/api";

type Mode = 'preset' | 'once' | 'custom';

const DAY_CHIPS = DOW_ORDER.map((d) => ({ id: d, label: DOW_LABELS[d] }));

/** ISO instant → datetime-local input value rendered on the workspace's wall
 * clock (the schedule's canonical timezone), e.g. "2026-09-15T09:00". */
const isoToLocalInput = (iso: string, tz: string | undefined): string => {
  try {
    const w = instantToWallTime(new Date(iso), tz || 'UTC');
    const pad = (v: number) => String(v).padStart(2, '0');
    return `${w.year}-${pad(w.month)}-${pad(w.day)}T${pad(w.hour)}:${pad(w.minute)}`;
  } catch {
    return '';
  }
};

/** datetime-local input value → wall time in tz → explicit ISO instant. */
const localInputToIso = (value: string, tz: string | undefined): string | null => {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(value || '');
  if (!m) return null;
  const wall = { year: Number(m[1]), month: Number(m[2]), day: Number(m[3]), hour: Number(m[4]), minute: Number(m[5]) };
  return wallTimeToInstant(wall, tz || 'UTC').toISOString();
};

export function ScheduleEditorModal({ draft, tenant, onClose, onChanged }: {
  /** Existing scheduler row, or null to create one. */
  draft: Scheduler | null;
  tenant: any;
  onClose: () => void;
  /** Fired after a successful create/update/delete so shared copies refresh. */
  onChanged: () => void;
}) {
  const isNew = !draft?.id;
  const agents: any[] = tenant?.agents || [];
  const channels: any[] = tenant?.channels || [];
  const tz: string = tenant?.tz || 'UTC';

  const preset = draft && draft.kind === 'recurring' ? presetFromExpr(draft.expr) : null;
  const [name, setName] = useState(draft?.name || '');
  const [agentId, setAgentId] = useState(draft?.agent_id || agents[0]?.id || '');
  const [prompt, setPrompt] = useState(draft?.prompt || '');
  const [mode, setMode] = useState<Mode>(draft?.kind === 'once' ? 'once' : preset ? 'preset' : draft ? 'custom' : 'preset');
  const [freq, setFreq] = useState<RecurrenceFreq>(preset?.freq || 'daily');
  const [time, setTime] = useState(preset?.time || '09:00');
  const [days, setDays] = useState<number[]>(preset?.days ?? [1, 2, 3, 4, 5]);
  const [dayOfMonth, setDayOfMonth] = useState(preset?.dayOfMonth || 1);
  const [customExpr, setCustomExpr] = useState(draft && draft.kind === 'recurring' ? draft.expr : '');
  const [runAtLocal, setRunAtLocal] = useState(draft?.run_at ? isoToLocalInput(draft.run_at, tz) : '');
  const [deliveryType, setDeliveryType] = useState<'thread' | 'channel'>(draft?.delivery?.type === 'channel' ? 'channel' : 'thread');
  const [channelId, setChannelId] = useState(draft?.delivery?.channel_id || channels[0]?.id || '');
  const [enabled, setEnabled] = useState(draft ? draft.enabled !== false : true);
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const parsedCustom = useMemo(() => parseCron(customExpr), [customExpr]);
  const onceIso = useMemo(() => (mode === 'once' ? localInputToIso(runAtLocal, tz) : null), [mode, runAtLocal, tz]);

  // The generated canonical expression — preset mode COMPOSES it, the user
  // never types cron notation there (schedules spec, editor requirement).
  const expr = mode === 'custom'
    ? customExpr.trim()
    : mode === 'preset'
      ? (buildPresetExpr({ freq, time, days, dayOfMonth }) || '')
      : '';

  const customInvalid = mode === 'custom' && !parsedCustom;
  const onceInvalid = mode === 'once' && (
    !onceIso || (onceIso !== null && Date.parse(onceIso) <= Date.now())
  );
  const promptMissing = !prompt.trim();
  const nameInvalid = name.trim().length <= 1;
  const valid = !nameInvalid && !promptMissing && !customInvalid && !onceInvalid;

  const preview = useMemo(() => {
    if (mode === 'once' || !expr) return [];
    return cronNextRuns(expr, new Date(), tz, 3);
  }, [expr, mode, tz]);

  const summary = mode === 'preset' && expr ? describeCron(expr) : '';
  const toast = useStore.getState().toast;

  const save = async () => {
    if (!valid || busy) return;
    setBusy(true);
    const payload: any = {
      name: name.trim(),
      agent_id: agentId,
      prompt: prompt.trim(),
      kind: mode === 'once' ? 'once' : 'recurring',
      delivery: deliveryType === 'channel' && channelId
        ? { type: 'channel', channel_id: channelId }
        : { type: 'thread' },
      enabled,
    };
    if (mode === 'once') {
      payload.run_at = onceIso;
      payload.expr = '';
    } else {
      payload.expr = expr;
      payload.run_at = null;
    }
    try {
      if (isNew) {
        await schedulers.create(tenant.id || tenant.sub, payload);
        toast('Schedule “' + payload.name + '” created');
      } else {
        await schedulers.update(tenant.id || tenant.sub, draft!.id, payload);
        toast('Schedule “' + payload.name + '” saved');
      }
      onChanged();
      onClose();
    } catch (err) {
      toast(formatApiError(err, 'Failed to save the schedule'), 'danger');
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!draft || busy) return;
    // Two-step confirm (house pattern): the first click arms the button.
    if (!confirmDelete) { setConfirmDelete(true); return; }
    setBusy(true);
    try {
      await schedulers.delete(tenant.id || tenant.sub, draft.id);
      toast('Schedule “' + draft.name + '” deleted');
      onChanged();
      onClose();
    } catch (err) {
      toast(formatApiError(err, 'Failed to delete the schedule'), 'danger');
      setBusy(false);
    }
  };

  return (
    <Modal title={isNew ? 'New schedule' : 'Edit schedule'} onClose={onClose} odId="schedule-editor-modal" wide
      footer={<>
        {!isNew && (
          <button type="button" onClick={remove} data-od-id="btn-schedule-delete"
            className={cx('mr-auto flex h-9 items-center gap-1.5 rounded-md px-3 text-[13px] font-medium transition-colors',
              confirmDelete ? 'bg-[color-mix(in_oklab,var(--danger)_14%,transparent)] text-danger' : 'text-danger hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)]')}>
            <Icon name="x" size={14}/> {confirmDelete ? 'Click again to confirm' : 'Delete'}
          </button>
        )}
        <button type="button" onClick={onClose} className="flex h-9 items-center rounded-md px-3.5 text-[13px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2">Cancel</button>
        <button type="button" disabled={!valid || busy} data-od-id="btn-schedule-save" onClick={save}
          className="flex h-9 items-center rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent">
          {isNew ? 'Create schedule' : 'Save changes'}
        </button>
      </>}>
      <div className="space-y-5 p-5">
        <div>
          <label className={labelCls} htmlFor="sch-name">Schedule name</label>
          <input id="sch-name" className={inputCls} value={name} onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Morning ops digest" autoFocus data-testid="input-schedule-name"/>
          {nameInvalid && <p className="mt-1.5 text-[11px] text-danger">Required — at least two characters.</p>}
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label className={labelCls} htmlFor="sch-agent">Agent</label>
            <select id="sch-agent" className={inputCls} value={agentId} onChange={(e) => setAgentId(e.target.value)} data-testid="select-schedule-agent">
              {agents.map((a: any) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </select>
          </div>
          <div>
            <label className={labelCls} htmlFor="sch-tz">Timezone</label>
            <input id="sch-tz" className={cx(inputCls, 'font-mono text-[13px]')} value={tz} disabled
              title="Workspace timezone — change it in Settings → Workspace"/>
          </div>
        </div>

        <div>
          <label className={labelCls}>Recurrence</label>
          <Segmented
            value={mode}
            onChange={(m: string) => setMode(m as Mode)}
            options={[
              { id: 'preset', label: 'Recurring', testid: 'seg-mode-preset' },
              { id: 'once', label: 'Once', testid: 'seg-mode-once' },
              { id: 'custom', label: 'Custom', testid: 'seg-mode-custom' },
            ]}/>
          {mode === 'preset' && (
            <div className="mt-3 space-y-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_35%,var(--surface))] p-3.5">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <div>
                  <label className={labelCls} htmlFor="sch-freq">Frequency</label>
                  <select id="sch-freq" className={inputCls} value={freq} data-testid="select-schedule-freq"
                    onChange={(e) => setFreq(e.target.value as RecurrenceFreq)}>
                    <option value="hourly">Hourly</option>
                    <option value="daily">Daily</option>
                    <option value="weekly">Weekly</option>
                    <option value="monthly">Monthly</option>
                  </select>
                </div>
                {freq !== 'hourly' && (
                  <div>
                    <label className={labelCls} htmlFor="sch-time">Time</label>
                    <input id="sch-time" type="time" className={cx(inputCls, 'font-mono text-[13px]')} value={time} data-testid="input-schedule-time"
                      onChange={(e) => setTime(e.target.value)}/>
                  </div>
                )}
              </div>
              {(freq === 'daily' || freq === 'weekly') && (
                <div>
                  <label className={labelCls}>Days</label>
                  <div className="flex flex-wrap gap-2" data-testid="day-chips">
                    {DAY_CHIPS.map((d) => {
                      const on = days.includes(d.id);
                      return (
                        <button key={d.id} type="button" aria-pressed={on} data-testid={'day-chip-' + d.label.toLowerCase()}
                          onClick={() => setDays(on ? days.filter((x) => x !== d.id) : [...days, d.id])}
                          className={cx('h-8 rounded-md border px-3 text-[12px] font-medium transition-colors',
                            on ? 'border-accent bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg'
                              : 'border-line text-muted hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg2')}>
                          {d.label}
                        </button>
                      );
                    })}
                  </div>
                  {freq === 'daily' && <p className="mt-1.5 text-[11px] text-muted">Leave every day selected for a daily schedule.</p>}
                </div>
              )}
              {freq === 'monthly' && (
                <div>
                  <label className={labelCls} htmlFor="sch-dom">Day of month</label>
                  <select id="sch-dom" className={inputCls} value={dayOfMonth} data-testid="select-schedule-dom"
                    onChange={(e) => setDayOfMonth(Number(e.target.value))}>
                    {Array.from({ length: 28 }, (_, i) => i + 1).map((d) => <option key={d} value={d}>{d}</option>)}
                  </select>
                </div>
              )}
              <p className="text-[12px] text-fg2" data-testid="schedule-summary">
                {summary || 'Pick a time to build the recurrence.'}
              </p>
            </div>
          )}
          {mode === 'once' && (
            <div className="mt-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_35%,var(--surface))] p-3.5">
              <label className={labelCls} htmlFor="sch-runat">Run at</label>
              <input id="sch-runat" type="datetime-local" className={cx(inputCls, 'font-mono text-[13px]')}
                value={runAtLocal} data-testid="input-schedule-runat"
                onChange={(e) => setRunAtLocal(e.target.value)}/>
              {onceInvalid && (
                <p className="mt-1.5 text-[11px] text-danger" data-testid="error-schedule-runat">
                  {runAtLocal ? 'One-shot schedules fire in the future — pick a later time.' : 'Required — pick when the schedule should run once.'}
                </p>
              )}
            </div>
          )}
          {mode === 'custom' && (
            <div className="mt-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_35%,var(--surface))] p-3.5">
              <label className={labelCls} htmlFor="sch-expr">Cron expression</label>
              <input id="sch-expr" className={cx(inputCls, 'font-mono text-[13px]')} value={customExpr} data-testid="input-schedule-expr"
                onChange={(e) => setCustomExpr(e.target.value)} placeholder="0 9 * * 1-5"/>
              {customInvalid ? (
                <p className="mt-1.5 text-[11px] text-danger" data-testid="error-schedule-expr">
                  Invalid expression — use five fields: minute, hour, day-of-month, month, day-of-week.
                </p>
              ) : customExpr.trim() ? (
                <div className="mt-2" data-testid="schedule-next-runs">
                  <p className="text-[11px] uppercase tracking-[0.14em] text-muted">Next runs · {tz}</p>
                  <ul className="mt-1 space-y-0.5">
                    {preview.map((d) => (
                      <li key={d.toISOString()} className="font-mono text-[12px] text-fg2">
                        {fmtInTz(d.toISOString(), tz, { weekday: 'short', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
                      </li>
                    ))}
                  </ul>
                </div>
              ) : (
                <p className="mt-1.5 text-[11px] text-muted">Standard 5-field cron in the workspace timezone.</p>
              )}
            </div>
          )}
        </div>

        <div>
          <label className={labelCls} htmlFor="sch-prompt">Task prompt</label>
          <textarea id="sch-prompt" rows={4} className={cx(inputCls, 'h-auto py-2 leading-5')} value={prompt} data-testid="input-schedule-prompt"
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="What the agent should do on every fire — plain language, no JSON."/>
          {promptMissing && <p className="mt-1.5 text-[11px] text-danger" data-testid="error-schedule-prompt">Required — tell the agent what to do.</p>}
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label className={labelCls}>Delivery</label>
            <Segmented value={deliveryType} onChange={(v: string) => setDeliveryType(v as 'thread' | 'channel')}
              options={[
                { id: 'thread', label: 'Keep in thread', testid: 'seg-delivery-thread' },
                { id: 'channel', label: 'Channel', testid: 'seg-delivery-channel' },
              ]}/>
            <p className="mt-1.5 text-[11px] text-muted">
              {deliveryType === 'thread' ? 'The run\'s transcript is the deliverable.' : 'The final reply posts to the channel as the agent.'}
            </p>
          </div>
          {deliveryType === 'channel' && (
            <div>
              <label className={labelCls} htmlFor="sch-channel">Post to</label>
              <select id="sch-channel" className={inputCls} value={channelId} data-testid="select-schedule-channel"
                onChange={(e) => setChannelId(e.target.value)}>
                {channels.length === 0 && <option value="">No channels yet</option>}
                {channels.map((c: any) => <option key={c.id} value={c.id}>#{c.slug || c.name}</option>)}
              </select>
            </div>
          )}
        </div>

        <div className="flex items-center justify-between rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_35%,var(--surface))] px-4 py-3">
          <div>
            <p className="text-[13px] font-medium text-fg">Enabled</p>
            <p className="text-[12px] text-muted">Paused schedules keep their history and fire nothing.</p>
          </div>
          <Toggle on={enabled} onChange={setEnabled} label="Enable this schedule"/>
        </div>
      </div>
    </Modal>
  );
}
