// Subagents panel source (add-subagent-runs-surface 6.1/6.2, design D7): one
// source, two views. The list sections Running / Ended — running rows carry a
// live preview strip of the task's latest progress records; ended rows an
// outcome, relative time, and a two-line snippet. The detail view renders one
// task's child transcript — live via the session tasks progress stream
// (lib/tasks.ts), ended via the trajectory endpoint's replay, with a clear
// state when the output file was pruned — plus the D4 stop control.
//
// View state (which task is selected) lives INSIDE this module keyed on the
// tab (D7): SubagentsViews remounts per tab identity, so switching tabs or
// payloads resets it.
//
// Honest-time note: the merged task list carries no start/finish timestamps,
// so every time shown here is measured from when THIS browser session first
// observed the task (module-level first-seen stamps below). Running rows
// count up from first sight ("elapsed"); ended rows say when first seen
// ("seen … ago"). Nothing is fabricated.

import { useEffect, useState } from "react";
import { useStore, useWorkspace } from "../../../../../store";
import { Icon } from "../../../../ui/Icon";
import { formatDuration } from "../../../../../lib/helpers";
import { api } from "../../../../../lib/api";
import {
  applyTaskCompleted,
  applyTaskList,
  taskFromWire,
  taskRecordFromWire,
  useTaskEntries,
  useTaskRecords,
  useTaskStream,
  type TaskEntry,
  type TaskRecord,
  type TaskStatus,
} from "../../../../../lib/tasks";
import { FOREGROUND_STOP_NOTE, recordExcerpt, stopGating, taskDisplayName } from "./view";

// --- honest-time base --------------------------------------------------------

// First-observation stamps, module level so view switches and remounts keep
// the original zero. Test seam clears them.
const firstSeen = new Map<string, number>();

function firstSeenAt(chatId: string, taskId: string): number {
  const key = `${chatId}::${taskId}`;
  let t = firstSeen.get(key);
  if (t === undefined) {
    t = Date.now();
    firstSeen.set(key, t);
  }
  return t;
}

/** Test seam: forget first-seen stamps so suites start clean. */
export function resetFirstSeenForTests(): void {
  firstSeen.clear();
}

/** Coarse "first seen" age — the honest relative time (see header note). */
function seenAgo(ms: number): string {
  const mins = Math.floor(ms / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return mins + 'm ago';
  const hours = Math.floor(mins / 60);
  if (hours < 24) return hours + 'h ago';
  return Math.floor(hours / 24) + 'd ago';
}

/** Re-renders once a second while `active` — the elapsed counter's pulse. */
function useTicker(active: boolean): void {
  const [, setTick] = useState(0);
  useEffect(() => {
    if (!active) return;
    const id = setInterval(() => setTick((n: number) => n + 1), 1000);
    return () => clearInterval(id);
  }, [active]);
}

// --- session address ---------------------------------------------------------

/** The chat's active server session — the tasks endpoints' wire address
 * (mirrors lib/tasks.ts's activeTaskSessionId, which is not exported). */
function sessionAddress(s: any, chatId: string): string | null {
  if (!chatId) return null;
  const th = s.db[s.pos.tenantId]?.threads?.[chatId];
  if (!th || Array.isArray(th)) return null;
  const sess = th.list?.find((x: any) => x.id === th.active);
  if (!sess) return null;
  if (typeof sess.sess === 'string' && sess.sess) return sess.sess;
  const id = typeof sess.id === 'string' ? sess.id : '';
  return id.startsWith('sess_') || id.startsWith('sched_') ? id : null;
}

/** The session's tasks-API address. ws/agent mirror the file source's
 * derivation (ctx first, then the workspace entry and the chat's agent). */
function useTaskAddress(chatId: string, ctx: any): { ws: string; agent: string; sessionId: string | null } {
  const tenant = useWorkspace() as any;
  const sessionId = useStore((s: any) => sessionAddress(s, chatId));
  const ws: string = ctx?.workspaceId ?? tenant?.sub ?? tenant?.id ?? tenant?.slug ?? '';
  const agents: any[] = tenant?.agents || [];
  const chatAgent = agents.find((a: any) => a.slug === chatId || a.id === chatId);
  const agent: string = ctx?.agentSlug ?? chatAgent?.slug ?? chatAgent?.id ?? chatId ?? '';
  return { ws, agent, sessionId };
}

// --- status/kind chips -------------------------------------------------------

const STATUS_LABEL: Record<TaskStatus, string> = {
  running: 'Running',
  completed: 'Completed',
  failed: 'Failed',
  canceled: 'Canceled',
  // Restart-lost historical evidence — ended-style, muted, never live-looking.
  created: 'Created',
};

function StatusChip({ status }: { status: TaskStatus }) {
  const tone =
    status === 'running'
      ? 'text-accent border-[color-mix(in_oklab,var(--accent)_40%,transparent)]'
      : status === 'failed'
        ? 'text-danger border-[color-mix(in_oklab,var(--danger)_40%,transparent)]'
        : 'text-muted';
  return (
    <span className={'shrink-0 rounded-full border border-line px-1.5 py-px font-mono text-[9px] uppercase tracking-wide ' + tone} data-testid={'panel-subagent-status-' + status}>
      {STATUS_LABEL[status]}
    </span>
  );
}

function KindBadge({ kind }: { kind: TaskEntry['kind'] }) {
  return (
    <span className="shrink-0 rounded-full border border-line px-1.5 py-px font-mono text-[9px] uppercase tracking-wide text-muted">
      {kind}
    </span>
  );
}

function SectionLabel({ label }: { label: string }) {
  return (
    <p className="px-2 pb-1 pt-2 font-mono text-[9px] font-semibold uppercase tracking-[0.14em] text-muted">{label}</p>
  );
}

// --- list view ---------------------------------------------------------------

function RunningRow({ chatId, entry, onOpen }: { chatId: string; entry: TaskEntry; onOpen: (taskId: string) => void }) {
  const records = useTaskRecords(chatId, entry.taskId);
  const elapsed = formatDuration(Date.now() - firstSeenAt(chatId, entry.taskId));
  const tail = records.slice(-3);
  return (
    <button type="button" data-testid={'panel-subagent-' + entry.taskId} onClick={() => onOpen(entry.taskId)}
      className="flex w-full flex-col gap-1 rounded-md px-2 py-2 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]">
      <span className="flex min-w-0 items-center gap-2">
        <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-accent" aria-hidden/>
        <span className="min-w-0 truncate text-[13px] font-medium text-fg">{taskDisplayName(entry)}</span>
        <KindBadge kind={entry.kind}/>
        <span className="ml-auto shrink-0 font-mono text-[10px] text-muted" data-testid="panel-subagent-elapsed">{elapsed}</span>
        <Icon name="chevright" size={12} className="shrink-0 text-muted"/>
      </span>
      {entry.description && (
        <span className="block truncate text-[11.5px] leading-4 text-muted" title={entry.description}>{entry.description}</span>
      )}
      {/* Live preview strip: a tail window of the task's latest progress
          records, updating while the stream runs (spec "List shows live
          preview"). */}
      <span className="block rounded-md bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-1.5 py-1 font-mono text-[10px] leading-4 text-muted" data-testid="panel-subagent-preview">
        {tail.length === 0
          ? 'Waiting for progress…'
          : tail.map((r, i) => (
              <span key={r.sequence || i} className="block truncate" data-testid={'panel-subagent-preview-line-' + i}>
                {recordExcerpt(r)}
              </span>
            ))}
      </span>
    </button>
  );
}

function EndedRow({ chatId, entry, onOpen }: { chatId: string; entry: TaskEntry; onOpen: (taskId: string) => void }) {
  const seen = seenAgo(Date.now() - firstSeenAt(chatId, entry.taskId));
  const snippet = entry.summary || entry.description || '';
  return (
    <button type="button" data-testid={'panel-subagent-' + entry.taskId} onClick={() => onOpen(entry.taskId)}
      className="flex w-full flex-col gap-1 rounded-md px-2 py-2 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]">
      <span className="flex min-w-0 items-center gap-2">
        <span className="min-w-0 truncate text-[13px] font-medium text-fg2">{taskDisplayName(entry)}</span>
        <KindBadge kind={entry.kind}/>
        <StatusChip status={entry.status}/>
        <span className="ml-auto shrink-0 font-mono text-[10px] text-muted" data-testid="panel-subagent-seen" title="First seen in this session">{seen}</span>
        <Icon name="chevright" size={12} className="shrink-0 text-muted"/>
      </span>
      {/* Two-line snippet (summary when the task produced one, else the
          description), clamped by line-clamp. */}
      {snippet && (
        <span className="line-clamp-2 text-[11.5px] leading-4 text-muted" title={snippet}>{snippet}</span>
      )}
    </button>
  );
}

function TaskList({ chatId, entries, onOpen }: { chatId: string; entries: TaskEntry[]; onOpen: (taskId: string) => void }) {
  const running = entries.filter((e) => e.status === 'running');
  const ended = entries.filter((e) => e.status !== 'running');
  useTicker(running.length > 0);

  if (entries.length === 0) {
    return (
      <div className="p-4" data-testid="panel-subagents-empty">
        <p className="text-[12.5px] font-medium text-fg2">No subagent activity yet</p>
        <p className="mt-1 text-[12px] leading-5 text-muted">
          Delegations and background shells appear here while the agent works.
        </p>
      </div>
    );
  }
  return (
    <div className="p-2" data-testid="panel-subagents-list">
      {running.length > 0 && (
        <section>
          <SectionLabel label={'Running · ' + running.length}/>
          <div className="space-y-0.5">
            {running.map((e) => <RunningRow key={e.taskId} chatId={chatId} entry={e} onOpen={onOpen}/>)}
          </div>
        </section>
      )}
      {ended.length > 0 && (
        <section>
          <SectionLabel label={'Ended · ' + ended.length}/>
          <div className="space-y-0.5">
            {ended.map((e) => <EndedRow key={e.taskId} chatId={chatId} entry={e} onOpen={onOpen}/>)}
          </div>
        </section>
      )}
    </div>
  );
}

// --- detail view: trajectory -------------------------------------------------

type ReplayPhase = 'loading' | 'ready' | 'missing' | 'error';

/** Ended tasks replay from the trajectory endpoint (spec: "ended tasks'
 * trajectories SHALL replay from the trajectory endpoint"); a 404 means the
 * output file was pruned while the session evidence persisted — a clear
 * not-found state, never a crash. Running tasks ignore this entirely (the
 * live buffer feeds the view). */
function useReplay(
  address: { ws: string; agent: string; sessionId: string | null },
  entry: TaskEntry
): { phase: ReplayPhase; records: TaskRecord[]; message?: string } {
  const [state, setState] = useState<{ phase: ReplayPhase; records: TaskRecord[]; message?: string }>({ phase: 'loading', records: [] });
  const { ws, agent, sessionId } = address;
  useEffect(() => {
    // Running tasks feed from the live buffer — the replay arms only when the
    // task is ended (the status flip re-arms through the deps).
    if (entry.status === 'running') return;
    let alive = true;
    setState({ phase: 'loading', records: [] });
    if (!ws || !agent || !sessionId) {
      setState({ phase: 'error', records: [], message: "This task's session isn't connected — its trajectory can't be replayed." });
      return () => {
        alive = false;
      };
    }
    api.agents
      .taskOutput(ws, agent, sessionId, entry.taskId)
      .then((out) => {
        if (alive) setState({ phase: 'ready', records: (out?.records || []).map(taskRecordFromWire) });
      })
      .catch((err: unknown) => {
        if (!alive) return;
        if ((err as any)?.status === 404) setState({ phase: 'missing', records: [] });
        else setState({ phase: 'error', records: [], message: err instanceof Error ? err.message : String(err) });
      });
    return () => {
      alive = false;
    };
  }, [ws, agent, sessionId, entry.taskId, entry.status]);
  return state;
}

function RecordBlock({ record }: { record: TaskRecord }) {
  if (record.type === 'tool_call') {
    return (
      <div className="rounded-md border border-line bg-surface px-2 py-1.5" data-testid="panel-subagent-tool-call">
        <p className="font-mono text-[11px] text-fg2">{record.name || 'tool'}</p>
        {record.args && (
          <p className="mt-0.5 break-all font-mono text-[10.5px] leading-4 text-muted">{record.args.replace(/\s+/g, ' ').trim()}</p>
        )}
      </div>
    );
  }
  if (record.type === 'tool_result') {
    return (
      <pre className="od-scroll max-h-40 overflow-y-auto whitespace-pre-wrap break-words rounded-md bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-2 py-1.5 font-mono text-[10.5px] leading-4 text-muted" data-testid="panel-subagent-tool-result">
        {record.result || ''}
      </pre>
    );
  }
  return (
    <p className="whitespace-pre-wrap break-words text-[12.5px] leading-5 text-fg2" data-testid="panel-subagent-text">
      {record.text}
    </p>
  );
}

function Trajectory({ records }: { records: TaskRecord[] }) {
  if (records.length === 0) {
    return (
      <p className="mt-2 text-[12px] text-muted" data-testid="panel-subagent-trajectory-empty">
        No trajectory recorded for this task.
      </p>
    );
  }
  return (
    <div className="mt-2 space-y-1.5" data-testid="panel-subagent-trajectory">
      {records.map((r, i) => <RecordBlock key={r.sequence || i} record={r}/>)}
    </div>
  );
}

// --- detail view -------------------------------------------------------------

async function refreshTaskList(ws: string, agent: string, sessionId: string, chatId: string): Promise<void> {
  try {
    const res = await api.agents.listTasks(ws, agent, sessionId);
    applyTaskList(chatId, (res?.tasks || []).map(taskFromWire));
  } catch {
    // non-fatal — the panel keeps what it has
  }
}

function TaskDetail({
  chatId,
  ctx,
  entry,
  entriesLoaded,
  onBack,
}: {
  chatId: string;
  ctx: any;
  entry: TaskEntry | null;
  entriesLoaded: boolean;
  onBack: () => void;
}) {
  const address = useTaskAddress(chatId, ctx);
  const gating = stopGating(entry);
  const addressable = Boolean(address.ws && address.agent && address.sessionId);
  const canStop = gating.enabled && addressable;
  const [stopping, setStopping] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const replay = useReplay(address, entry ?? { taskId: '', kind: 'delegation', status: 'created', description: '' });
  useTicker(entry?.status === 'running');
  // Hooks before any early return: the live buffer for the addressed task
  // (empty taskId degrades to the stable empty constant inside the selector).
  const liveRecords = useTaskRecords(chatId, entry?.taskId ?? '');

  if (!entry) {
    // The list fold has not landed yet → resolving; the fold landed without
    // this id → the detail was addressed from a foreground delegation (not a
    // task): the stop control renders disabled with the "background only"
    // explanation (spec "Foreground shows disabled stop").
    return (
      <div className="p-3" data-testid={entriesLoaded ? 'panel-subagent-unknown' : 'panel-subagent-resolving'}>
        <BackRow onBack={onBack}/>
        {entriesLoaded ? (
          <>
            <p className="mt-2 text-[13px] font-semibold text-fg">Not a background task</p>
            <p className="mt-1 text-[12px] leading-5 text-muted">
              This delegation ran in the parent transcript and has no task record of its own.
            </p>
            <div className="mt-2.5 flex items-center gap-2" data-testid="panel-subagent-stop-row">
              <button type="button" disabled data-testid="panel-subagent-stop" aria-disabled="true"
                title={FOREGROUND_STOP_NOTE}
                className="flex h-7 shrink-0 cursor-not-allowed items-center gap-1.5 rounded-md border border-line px-2.5 text-[12px] font-medium text-muted opacity-50">
                <Icon name="stop" size={11}/> Stop
              </button>
              <span className="text-[11px] leading-4 text-muted" data-testid="panel-subagent-stop-note">{FOREGROUND_STOP_NOTE}</span>
            </div>
          </>
        ) : (
          <p className="mt-2 flex items-center gap-1.5 text-[12px] text-muted" data-testid="panel-subagent-loading">
            <span className="od-dot"/><span className="od-dot"/><span className="od-dot"/> Resolving task…
          </p>
        )}
      </div>
    );
  }

  const stop = async () => {
    if (!canStop || stopping) return;
    setStopping(true);
    setNotice(null);
    try {
      await api.agents.cancelTask(address.ws, address.agent, address.sessionId as string, entry.taskId);
      // Optimistic flip, no reload — the backend also emits x.task_completed,
      // which folds idempotently through the same action (D9).
      applyTaskCompleted(chatId, { taskId: entry.taskId, kind: entry.kind, outcome: 'canceled' });
    } catch (err) {
      if ((err as any)?.status === 409) {
        // The task already ended — surface a non-fatal notice and fold the
        // fresh list so the row reflects reality.
        setNotice('Task already ended.');
        void refreshTaskList(address.ws, address.agent, address.sessionId as string, chatId);
      } else {
        setNotice(err instanceof Error ? err.message : "Couldn't stop the task.");
      }
    } finally {
      setStopping(false);
    }
  };

  const running = entry.status === 'running';
  const records = running ? liveRecords : replay.records;
  const toolCalls = records.filter((r) => r.type === 'tool_call').length;

  return (
    <div className="p-3" data-testid="panel-subagent-detail">
      <BackRow onBack={onBack}/>
      <div className="mt-1.5 flex min-w-0 items-center gap-2">
        <span className="min-w-0 truncate text-[13px] font-semibold text-fg">{taskDisplayName(entry)}</span>
        <KindBadge kind={entry.kind}/>
        <StatusChip status={entry.status}/>
      </div>
      <p className="mt-1 font-mono text-[10px] uppercase tracking-[0.14em] text-muted" data-testid="panel-subagent-meta">
        {running
          ? `elapsed ${formatDuration(Date.now() - firstSeenAt(chatId, entry.taskId))}`
          : `seen ${seenAgo(Date.now() - firstSeenAt(chatId, entry.taskId))}`}
        {' · '}
        <span data-testid="panel-subagent-tool-count">{toolCalls} tool {toolCalls === 1 ? 'call' : 'calls'}</span>
      </p>
      {entry.description && (
        <p className="mt-1 break-words text-[12px] leading-5 text-muted">{entry.description}</p>
      )}

      {/* D4 stop: enabled only while the task is running AND its session is
          addressable; the explanation renders whenever the control is off. */}
      <div className="mt-2.5 flex items-center gap-2" data-testid="panel-subagent-stop-row">
        <button type="button" onClick={stop} disabled={!canStop || stopping}
          data-testid="panel-subagent-stop" data-od-id="panel-subagent-stop"
          title={canStop ? 'Stop this background task' : gating.note || 'Task already ended'}
          className="flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-line px-2.5 text-[12px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent">
          <Icon name="stop" size={11}/>{stopping ? 'Stopping…' : 'Stop'}
        </button>
        {gating.note && (
          <span className="text-[11px] leading-4 text-muted" data-testid="panel-subagent-stop-note">{gating.note}</span>
        )}
      </div>
      {notice && (
        <p className="mt-1.5 text-[11px] text-muted" data-testid="panel-subagent-notice">{notice}</p>
      )}

      {/* Child transcript: live records while running, the endpoint's replay
          once ended. Pruned output renders its own not-found state. */}
      {running ? (
        <Trajectory records={records}/>
      ) : replay.phase === 'loading' ? (
        <p className="mt-2 flex items-center gap-1.5 text-[12px] text-muted" data-testid="panel-subagent-replay-loading">
          <span className="od-dot"/><span className="od-dot"/><span className="od-dot"/> Loading trajectory…
        </p>
      ) : replay.phase === 'missing' ? (
        <div className="mt-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] px-2.5 py-2" data-testid="panel-subagent-replay-missing">
          <p className="text-[12px] font-medium text-fg2">Trajectory no longer available</p>
          <p className="mt-0.5 text-[11.5px] leading-4 text-muted">
            This task's output was pruned; its outcome and summary above are the persisted evidence.
          </p>
        </div>
      ) : replay.phase === 'error' ? (
        <p className="mt-2 text-[12px] text-muted" data-testid="panel-subagent-replay-error">{replay.message}</p>
      ) : (
        <Trajectory records={records}/>
      )}
    </div>
  );
}

function BackRow({ onBack }: { onBack: () => void }) {
  return (
    <button type="button" onClick={onBack} data-testid="panel-subagents-back" data-od-id="panel-subagents-back"
      className="flex items-center gap-1 text-[12px] font-medium text-muted transition-colors hover:text-fg">
      <Icon name="chevleft" size={12}/> All tasks
    </button>
  );
}

// --- view switching (state inside the module, D7) ----------------------------

function SubagentsViews({
  chatId,
  ctx,
  entries,
  initialTaskId,
}: {
  chatId: string;
  ctx: any;
  entries: TaskEntry[];
  initialTaskId: string | null;
}) {
  // The back state is one level deep by construction: rows open detail, back
  // returns to the list. `key` on SubagentsViews (tab identity) resets it.
  const [taskId, setTaskId] = useState<string | null>(initialTaskId);
  const entry = taskId ? entries.find((e) => e.taskId === taskId) ?? null : null;
  if (taskId) {
    return (
      <TaskDetail
        chatId={chatId}
        ctx={ctx}
        entry={entry}
        entriesLoaded={entries.length > 0}
        onBack={() => setTaskId(null)}
      />
    );
  }
  return <TaskList chatId={chatId} entries={entries} onOpen={setTaskId}/>;
}

// --- the source --------------------------------------------------------------

export function SubagentsSource({ tab, ctx }: { tab: any; ctx?: any }) {
  const posChatId = useStore((s: any) => s.pos.chatId);
  // The tab payload addresses the chat (deep-links carry {chatId, taskId});
  // candidate-opened tabs carry nothing and resolve to the mounted chat (the
  // browser mirror's precedent).
  const chatId: string =
    typeof tab?.payload?.chatId === 'string' && tab.payload.chatId ? tab.payload.chatId : posChatId || '';

  // The panel is the stream consumer while this source is mounted; the
  // ref-counted hook tears the subscription down on the last close.
  useTaskStream(chatId, Boolean(chatId));
  const entries = useTaskEntries(chatId);

  const initialTaskId =
    typeof tab?.payload?.taskId === 'string' && tab.payload.taskId ? tab.payload.taskId : null;

  return (
    <SubagentsViews
      key={tab?.id || chatId}
      chatId={chatId}
      ctx={ctx}
      entries={entries}
      initialTaskId={initialTaskId}
    />
  );
}
