// Floating run card (add-subagent-runs-surface D6, tasks 5.1/5.2): the
// session's run-status surface, replacing the header chip + anchored popover
// (add-session-todos-surface, whose popover requirement is removed). One
// dismissible card floats over the transcript — anchored in the chat pane's
// top corner at/above the 640px floor (WIDE_QUERY idiom), a full-height
// bottom sheet below it — hosting the Progress section (the session's todo
// checklist, same data lane and four row states as before) and the Agents
// section (the session's background tasks: delegation and shell rows with a
// live running marker, ended tasks collapsed into one "Ended · N ›" closing
// row that opens the right panel's Subagents source).
//
// Surface model (spec "Floating run card"): the card renders whenever a lane
// has content and the user has not dismissed it — the run's first todo write
// or first task activity IS the auto-surface, so it happens exactly once per
// run; dismissal is sticky for the remainder of the run (the next run's
// false→true transition re-arms it) and a chat switch resets it. Present-only
// everywhere: no todo plan and no task activity → nothing renders anywhere,
// and the header carries no run chip anymore.
//
// Lanes: the todos lane is useSessionTodoPlan (unchanged). The tasks lane is
// the per-chat task store (web/src/lib/tasks.ts) plus — without subscribing —
// unresulted `agent` / `background_shell` calls in the session's loaded
// events, so an in-flight delegation surfaces the card before any task entry
// exists. The ref-counted progress stream is subscribed ONLY while the card
// is actually visible (present and not dismissed); a dismissed card is closed
// and releases it.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Icon } from '../../ui/Icon';
import { useStore } from '../../../store';
import { agentExposesTodoWrite, useSessionTodoPlan } from '../../../lib/sessionTodos';
import { TodoPlanRows, todoRatio } from '../../../lib/generativeUi/TodoChecklistCard';
import { parseArgs } from '../../../lib/toolDisplay';
import { useTaskEntries, useTaskStream, type TaskEntry } from '../../../lib/tasks';
// The running markers copy TodoChecklistCard's TodoMark span, whose animation
// lives in generativeUi.css — the registry CSS is imported there, so a
// standalone mount needs it too (Vite dedupes the double import).
import '../../../lib/generativeUi/generativeUi.css';

const WIDE_QUERY = '(min-width: 640px)';

// ---------------------------------------------------------------------------
// Transcript call collection — task-lane presence signal + D5 display join.
// ---------------------------------------------------------------------------

/** One `agent` / `background_shell` call found in the session's loaded
 * events. It doubles as (a) the presence signal before any task entry exists
 * (an unresulted call = the delegation/shell is in flight) and (b) the
 * display-join counterpart of a TaskEntry: the delegation args carry
 * `subagent_type` (the row's subagent name) and the shell args the `command`
 * (the row's excerpt). Join is display-only (D5). */
interface TaskCallInfo {
  kind: 'delegation' | 'shell';
  description: string;
  subagentType: string;
  command: string;
  unresulted: boolean;
}

function collectTaskCalls(messages: any[] | undefined | null): TaskCallInfo[] {
  const out: TaskCallInfo[] = [];
  if (!Array.isArray(messages)) return out;
  for (const m of messages) {
    if (!Array.isArray(m?.tools)) continue;
    for (const t of m.tools) {
      if (t?.name !== 'agent' && t?.name !== 'background_shell') continue;
      const args = parseArgs(typeof t.args === 'string' ? t.args : null) || {};
      out.push({
        kind: t.name === 'agent' ? 'delegation' : 'shell',
        description: typeof args.description === 'string' ? args.description : '',
        subagentType: typeof args.subagent_type === 'string' ? args.subagent_type : '',
        command: typeof args.command === 'string' ? args.command : '',
        unresulted: !t.res,
      });
    }
  }
  return out;
}

/** D5 join, task-row side: the entry's ordinal among its kind+description
 * peers picks the same-indexed transcript call — the mirror image of
 * joinTaskForCard's description+order tiebreak. A miss degrades the row name,
 * never blocks anything. */
function joinCallsForEntries(entries: TaskEntry[], calls: TaskCallInfo[]): (TaskCallInfo | null)[] {
  const byKey = new Map<string, TaskCallInfo[]>();
  for (const c of calls) {
    const key = c.kind + '\u0000' + c.description;
    const list = byKey.get(key);
    if (list) list.push(c);
    else byKey.set(key, [c]);
  }
  const ordinals = new Map<string, number>();
  return entries.map((e) => {
    const key = e.kind + '\u0000' + e.description;
    const i = ordinals.get(key) ?? 0;
    ordinals.set(key, i + 1);
    return (byKey.get(key) ?? [])[i] ?? null;
  });
}

/** First line, trimmed — the one-liner form descriptions/commands render in. */
function firstLine(text: string): string {
  const t = (text || '').trim();
  const cut = t.indexOf('\n');
  return (cut === -1 ? t : t.slice(0, cut)).trim();
}

/** Row name: the delegation's subagent type from the joined call's args,
 * else the kind label 'Agent'; a shell row excerpts the joined call's
 * command, else its description. */
function taskRowName(entry: TaskEntry, call: TaskCallInfo | null): string {
  if (entry.kind === 'shell') {
    const command = firstLine(call?.command || entry.description);
    return command || 'Shell';
  }
  return (call && call.subagentType && firstLine(call.subagentType)) || 'Agent';
}

/** Row description one-liner: the task description, else the folded
 * completion summary (entries minted from task_completed carry it). */
function taskRowDescription(entry: TaskEntry): string {
  return firstLine(entry.description) || (entry.summary ? firstLine(entry.summary) : '');
}

// ---------------------------------------------------------------------------
// Agents section — one row per running task + the Ended closing row (5.2:
// header, rows, and the Ended summary all open the panel's Subagents source
// through onOpen; a joinable row passes its taskId, the rest open the list).
// ---------------------------------------------------------------------------

function AgentsSection({ entries, joinedCalls, onOpen }: {
  entries: TaskEntry[];
  joinedCalls: (TaskCallInfo | null)[];
  onOpen: (taskId?: string) => void;
}) {
  const endedCount = entries.reduce((n, e) => (e.status === 'running' ? n : n + 1), 0);
  return (
    <div>
      <button type="button" data-od-id="tasks-section" aria-haspopup="dialog"
        title="Open subagents" onClick={() => onOpen()}
        className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]">
        <Icon name="bot" size={13} className="shrink-0 text-meta"/>
        <span className="font-mono text-[12px] text-fg2">Agents</span>
        <span className="min-w-0 flex-1"/>
        <Icon name="chevright" size={13} className="shrink-0 text-muted"/>
      </button>
      {entries.map((entry, i) => {
        // `created` is restart-lost evidence — never rendered as running
        // (process-local honesty); it collapses into the Ended row instead.
        if (entry.status !== 'running') return null;
        return (
          <button key={entry.taskId || i} type="button" data-od-id={'task-row-' + entry.taskId}
            title={taskRowDescription(entry) || taskRowName(entry, joinedCalls[i])}
            onClick={() => onOpen(entry.taskId || undefined)}
            className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]">
            <span aria-hidden="true"
              className="od-genui-spin inline-block h-[11px] w-[11px] shrink-0 rounded-full border-[1.5px] border-accent border-t-transparent"/>
            <span className="max-w-[8rem] shrink-0 truncate font-mono text-[10px] text-fg2">
              {taskRowName(entry, joinedCalls[i])}
            </span>
            <span className="min-w-0 flex-1 truncate text-[12px] text-muted">
              {taskRowDescription(entry)}
            </span>
          </button>
        );
      })}
      {endedCount > 0 && (
        <button type="button" data-od-id="tasks-ended" aria-haspopup="dialog"
          title="Open subagents" onClick={() => onOpen()}
          className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]">
          <Icon name="check-circle" size={13} className="shrink-0 text-meta"/>
          <span className="font-mono text-[12px] text-fg2">Ended</span>
          <span className="min-w-0 flex-1"/>
          <span className="font-mono text-[10px] text-muted">{endedCount}</span>
          <Icon name="chevright" size={13} className="shrink-0 text-muted"/>
        </button>
      )}
    </div>
  );
}

/** Shared card body: run header (agent name · rev + done/total + dismiss) +
 * the Progress section (the todo lane, unchanged) + the Agents section. Used
 * by the anchored card and the bottom sheet so the two surfaces cannot
 * drift. */
function RunCardBody({ plan, agent, running, entries, joinedCalls, onDismiss, onOpenSubagents }: {
  plan: any;
  agent: any;
  running: boolean;
  entries: TaskEntry[];
  joinedCalls: (TaskCallInfo | null)[];
  onDismiss: () => void;
  onOpenSubagents: (taskId?: string) => void;
}) {
  const doneTotal = plan ? todoRatio(plan.items) : null;
  return (
    <>
      <div className="flex items-center gap-2 px-2.5 py-1.5">
        {running ? (
          <span aria-hidden="true"
            className="od-genui-spin inline-block h-[11px] w-[11px] shrink-0 rounded-full border-[1.5px] border-accent border-t-transparent"/>
        ) : (
          <Icon name="check-circle" size={13} className="shrink-0 text-meta"/>
        )}
        <span className="font-mono text-[12px] text-fg2">Run</span>
        <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-muted">
          {agent?.name}{plan && plan.revision !== null ? ' · rev ' + plan.revision : ''}
        </span>
        {doneTotal && (
          <span aria-live="polite" className="font-mono text-[10px] text-muted">
            {doneTotal.done}/{doneTotal.total}
          </span>
        )}
        <button type="button" data-od-id="todos-dismiss" title="Hide run status" aria-label="Hide run status"
          onClick={onDismiss}
          className="flex h-5 w-5 shrink-0 items-center justify-center rounded text-muted transition-colors hover:text-fg2">
          <Icon name="x" size={13}/>
        </button>
      </div>
      {plan && <TodoPlanRows items={plan.items}/>}
      {entries.length > 0 && (
        <div className="border-t border-linesoft">
          <AgentsSection entries={entries} joinedCalls={joinedCalls} onOpen={onOpenSubagents}/>
        </div>
      )}
    </>
  );
}

export function SessionTodosSurface({ agent }: { agent: any }) {
  const { plan } = useSessionTodoPlan();
  // Same-chat scope (fix-thinking-leak-on-chat-switch): every lane selector
  // and the surface machine follow THIS chat's run, not a turn streaming in
  // some other chat.
  const chatId = useStore((s: any) => s.pos.chatId);
  const running = useStore((s: any) => !!s.ui.running && s.ui.runningChatId === s.pos.chatId);
  // Tasks lane: entries reflect the folded task_completed translator output
  // and — while this card is visible — the ref-counted progress stream.
  const entries = useTaskEntries(chatId);
  // Presence WITHOUT subscribing (D6): unresulted `agent` / `background_shell`
  // calls in the session's loaded events mean a delegation/shell is in flight
  // even before any task entry exists.
  const messages = useStore((s: any) => {
    const th = s.db[s.pos.tenantId]?.threads[s.pos.chatId];
    return th && !Array.isArray(th)
      ? th.list.find((x: any) => x.id === th.active)?.messages
      : undefined;
  });
  const taskCalls = useMemo(() => collectTaskCalls(messages), [messages]);
  const hasUnresultedCall = useMemo(() => taskCalls.some((c) => c.unresulted), [taskCalls]);
  const joinedCalls = useMemo(() => joinCallsForEntries(entries, taskCalls), [entries, taskCalls]);

  // The card never survives a chat switch: render-time reset keyed on the
  // open chat (ContextRing's sanctioned adjustment pattern); sticky dismissal
  // resets with it.
  const chatKey = useStore((s: any) => s.pos.tenantId + '::' + s.pos.chatId);
  const [dismissed, setDismissed] = useState(false);
  const [openChatKey, setOpenChatKey] = useState(chatKey);
  if (openChatKey !== chatKey) {
    setOpenChatKey(chatKey);
    setDismissed(false);
  }

  // Viewport gate for the sheet (jsdom has no matchMedia — guard, listen,
  // clean up). Unknowable viewport defaults to the anchored card.
  const [wide, setWide] = useState(() =>
    typeof window !== 'undefined' && typeof window.matchMedia === 'function'
      ? window.matchMedia(WIDE_QUERY).matches
      : true
  );
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return;
    const mq = window.matchMedia(WIDE_QUERY);
    const onChange = (e: MediaQueryListEvent) => setWide(e.matches);
    if (typeof mq.addEventListener === 'function') {
      mq.addEventListener('change', onChange);
      return () => mq.removeEventListener('change', onChange);
    }
    mq.addListener(onChange);
    return () => mq.removeListener(onChange);
  }, []);

  // Run boundary (arm): dismissal is sticky for the REMAINDER of a run — the
  // next run's false→true transition re-arms the card (a mount while already
  // running starts armed). Nothing else is per-run state: visibility is
  // presence-driven, so the run's first todo write or first task activity is
  // the one auto-surface.
  const prevRunningRef = useRef(false);
  useEffect(() => {
    if (running && !prevRunningRef.current) setDismissed(false);
    prevRunningRef.current = running;
  }, [running]);

  // Presence (present-only, D6): the todos lane needs the agent to expose
  // todo_write AND a plan; the tasks lane needs task entries (folded
  // completions, stream-fed rows) or an unresulted delegation/shell call.
  const todoLane = Boolean(plan && agentExposesTodoWrite(agent));
  const taskLane = entries.length > 0 || hasUnresultedCall;
  const present = todoLane || taskLane;

  // Escape dismisses while the card is visible.
  useEffect(() => {
    if (!present) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setDismissed(true);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [present]);

  // Stream consumer (chat-runtime spec): the session tasks progress stream is
  // subscribed ONLY while the card is actually visible — present and not
  // dismissed. A dismissed card is closed: the ref-counted subscription
  // releases; the next run re-arms.
  useTaskStream(chatId, present && !dismissed);

  // Section→panel activation (task 5.2): the Agents header, any task row, and
  // the Ended row open the right panel's Subagents source. A row with a
  // joinable task passes its taskId (the panel pre-selects detail); the
  // header and the Ended row open the list view (no taskId).
  const onOpenSubagents = useCallback((taskId?: string) => {
    useStore.getState().openPanelTab({
      kind: 'subagents',
      title: 'Subagents',
      payload: { chatId, ...(taskId ? { taskId } : {}) },
    });
  }, [chatId]);

  const dismiss = useCallback(() => setDismissed(true), []);

  // Present-only + sticky-dismissal gate AFTER all hooks (ContextRing
  // precedent): the component may return null; hooks never conditionally. A
  // dismissed card is closed — it renders nothing and releases the stream
  // until the next run re-arms it.
  if (!present || dismissed) return null;

  const body = (
    <RunCardBody plan={plan} agent={agent} running={running} entries={entries}
      joinedCalls={joinedCalls} onDismiss={dismiss} onOpenSubagents={onOpenSubagents}/>
  );

  return (
    <>
      {wide && (
        <div className="pointer-events-none absolute right-4 top-4 z-30 w-72">
          <div role="dialog" aria-label="Run status" data-od-id="run-card"
            className="pointer-events-auto overflow-hidden rounded-lg border border-linesoft bg-surface text-left shadow-lg">
            {body}
          </div>
        </div>
      )}
      {!wide && (
        <div role="dialog" aria-label="Run status" data-od-id="run-sheet"
          className="pointer-events-auto fixed inset-x-0 bottom-0 top-14 z-30 overflow-y-auto rounded-t-xl border-t border-linesoft bg-surface text-left shadow-lg">
          {body}
        </div>
      )}
    </>
  );
}
