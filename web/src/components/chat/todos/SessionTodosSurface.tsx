// Session todos surface (add-session-todos-surface D2/D3/D4/D6): a persistent
// chip in the direct-chat header that opens the session's current todo
// checklist. D2 — the chip always shows state while a plan exists (active
// item + spinner while one runs, done/total otherwise). D3 — an anchored
// popover above the trigger at/above the 640px floor (ContextRing's idiom,
// same z-tier), a fixed bottom sheet below it. D4 — auto-surface: the
// popover opens itself on a run's FIRST todo_write and collapses when the
// run finishes or the list completes; presence is an event, not a layout
// tax. Present-only everywhere: no plan or an agent without todo_write →
// nothing renders (D6: direct agent chats only — ChatHeader gates the mount).
import { useCallback, useEffect, useRef, useState } from 'react';
import { Icon } from '../../ui/Icon';
import { cx } from '../../../lib/helpers';
import { useStore } from '../../../store';
import { agentExposesTodoWrite, hasOpenItems, useSessionTodoPlan } from '../../../lib/sessionTodos';
import { TodoPlanRows, todoRatio } from '../../../lib/generativeUi/TodoChecklistCard';
// The chip's active spinner copies TodoChecklistCard's TodoMark span, whose
// animation lives in generativeUi.css — the registry CSS is imported there, so a
// standalone mount needs it too (Vite dedupes the double import).
import '../../../lib/generativeUi/generativeUi.css';

const WIDE_QUERY = '(min-width: 640px)';

/** Shared panel body: transcript-card-register header (Todos · agent · rev N)
 * + the exact transcript rows + a dismiss control. Used by the anchored
 * popover and the bottom sheet so the two surfaces cannot drift. */
function TodosPanel({ plan, agent, onDismiss }: { plan: any; agent: any; onDismiss: () => void }) {
  return (
    <>
      <div className="flex items-center gap-2 px-2.5 py-1.5">
        <Icon name="check-circle" size={13} className="shrink-0 text-meta"/>
        <span className="font-mono text-[12px] text-fg2">Todos</span>
        <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-muted">
          {agent?.name}{plan.revision !== null ? ' · rev ' + plan.revision : ''}
        </span>
        <button type="button" data-od-id="todos-dismiss" title="Hide todos" aria-label="Hide todos"
          onClick={onDismiss}
          className="flex h-5 w-5 shrink-0 items-center justify-center rounded text-muted transition-colors hover:text-fg2">
          <Icon name="x" size={13}/>
        </button>
      </div>
      <TodoPlanRows items={plan.items}/>
    </>
  );
}

export function SessionTodosSurface({ agent }: { agent: any }) {
  const { plan, callId } = useSessionTodoPlan();
  const running = useStore((s: any) => !!s.ui.running);

  // The popover must never survive a chat switch: render-time reset keyed on
  // the open chat (ContextRing's sanctioned adjustment pattern). The machine
  // refs re-baseline here too — the new chat's already-landed writes are
  // history, not live events, so switching into a mid-run chat does not
  // auto-open; the next write in it does.
  const chatKey = useStore((s: any) => s.pos.tenantId + '::' + s.pos.chatId);
  const [open, setOpen] = useState(false);
  // `auto` marks the CURRENT open as machine-initiated — the only opens the
  // D4 auto-collapse may close. User-opened popovers (chip click) are
  // deliberate and never insta-collapsed by run boundaries.
  const [auto, setAuto] = useState(false);
  const [openChatKey, setOpenChatKey] = useState(chatKey);
  const dismissedRef = useRef(false); // sticky user dismissal, per run
  const baselineRef = useRef<string | null>(null); // writes already accounted for
  const openedRef = useRef(false); // one-shot auto-open per run
  const prevRunningRef = useRef(false);
  if (openChatKey !== chatKey) {
    setOpenChatKey(chatKey);
    setOpen(false);
    setAuto(false);
    baselineRef.current = callId;
    dismissedRef.current = false;
    openedRef.current = false;
  }
  const rootRef = useRef<HTMLSpanElement>(null);

  // Viewport gate for the sheet (jsdom has no matchMedia — guard, listen,
  // clean up). Unknowable viewport defaults to the anchored popover.
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

  // D4 ARM: on the running false→true transition (and on mount while already
  // running — a reload mid-run snapshots hydrated history without opening),
  // snapshot the last accounted write and clear the per-run flags. Never
  // opens by itself.
  useEffect(() => {
    if (running && !prevRunningRef.current) {
      baselineRef.current = callId;
      dismissedRef.current = false;
      openedRef.current = false;
    }
    prevRunningRef.current = running;
  }, [running, callId]);

  // D4 AUTO-OPEN: a write landing mid-run with a callId the run hasn't
  // accounted for opens the popover once (each todo_write carries a unique
  // callId, but the one-shot flag keeps auto-open to the run's FIRST write).
  // The baseline always absorbs the seen callId so nothing re-triggers.
  useEffect(() => {
    if (running && callId && callId !== baselineRef.current && !dismissedRef.current && !openedRef.current) {
      openedRef.current = true;
      setAuto(true);
      setOpen(true);
    }
    if (callId !== baselineRef.current) baselineRef.current = callId;
  }, [callId, running]);

  // D4 AUTO-COLLAPSE: only machine-initiated opens collapse — run finished,
  // or every item reached done/failed. User dismissal is interaction-only.
  useEffect(() => {
    if (open && auto && (!running || (plan && !hasOpenItems(plan.items)))) {
      setAuto(false);
      setOpen(false);
    }
  }, [open, auto, running, plan]);

  // User dismissal (button, outside pointerdown, Escape): closes the surface
  // AND sticks for the rest of the run — later writes update the chip but
  // never reopen. Cleared by the next run's ARM.
  const dismiss = useCallback(() => {
    dismissedRef.current = true;
    setAuto(false);
    setOpen(false);
  }, []);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) dismiss();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') dismiss();
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open, dismiss]);

  // Present-only gate AFTER all hooks (ContextRing precedent): the component
  // may return null; hooks never conditionally.
  if (!plan || !agentExposesTodoWrite(agent)) return null;

  const { done, total } = todoRatio(plan.items);
  const activeItem = plan.items.find((i) => i.status === 'active') ?? null;

  return (
    <span ref={rootRef} className="relative flex items-center">
      <button type="button" data-od-id="todos-chip" aria-expanded={open} aria-haspopup="dialog"
        title={activeItem ? 'Todos · ' + activeItem.text : 'Todos · ' + done + ' of ' + total + ' done'}
        onClick={(e) => {
          e.stopPropagation();
          if (open) setAuto(false); // chip close ends the machine lifecycle
          setOpen(!open);
        }}
        className={cx(
          'flex min-w-0 max-w-[160px] items-center gap-1.5 rounded-full border border-line bg-surface px-2 py-0.5',
          'text-[11px] leading-4 text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]'
        )}>
        <Icon name="check-circle" size={13} className="shrink-0 text-meta"/>
        {activeItem ? (
          <>
            <span aria-hidden="true"
              className="od-genui-spin inline-block h-[11px] w-[11px] shrink-0 rounded-full border-[1.5px] border-accent border-t-transparent"/>
            <span className="min-w-0 truncate font-mono text-[10px]">{activeItem.text}</span>
          </>
        ) : (
          <span className="font-mono text-[10px] text-muted">{done}/{total}</span>
        )}
      </button>
      {open && wide && (
        <span role="dialog" aria-label="Session todos" data-od-id="todos-popover"
          className="absolute bottom-full left-0 z-30 mb-2 block w-72 overflow-hidden rounded-lg border border-linesoft bg-surface text-left shadow-lg">
          <TodosPanel plan={plan} agent={agent} onDismiss={dismiss}/>
        </span>
      )}
      {open && !wide && (
        <span role="dialog" aria-label="Session todos" data-od-id="todos-sheet"
          className="fixed inset-x-0 bottom-0 z-30 block max-h-[70vh] overflow-y-auto rounded-t-xl border-t border-linesoft bg-surface text-left shadow-lg">
          <TodosPanel plan={plan} agent={agent} onDismiss={dismiss}/>
        </span>
      )}
    </span>
  );
}
