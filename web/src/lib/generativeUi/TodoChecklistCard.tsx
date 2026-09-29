// Todo checklist card (generative-ui spec, design D5). Renders `todo_write`
// ARGS — the transcript keeps revision history, cards render from event args.
// Header `n/m · rev`: n counts done items only, failed items are counted in
// the denominator only. One row per item keyed by its stable item id, so rows
// whose id persists across rewrites restyle in place rather than remount.
// Four row states: done struck + dimmed, active spinning, pending dimmed,
// failed in the danger color with its reason beneath. Present-only: a turn
// with no todo_write renders nothing todo-related. Earlier same-turn rewrites
// collapse to a one-line "plan updated" summary (TodoUpdatedSummary).

import { Icon } from "../../components/ui/Icon";
import { cx } from "../helpers";
import type { GenerativeUiCtx } from "./registry";

export type TodoStatus = 'pending' | 'active' | 'done' | 'failed';

export interface TodoItem {
  key: string;
  text: string;
  status: TodoStatus;
  reason: string;
}

export interface TodoPlan {
  items: TodoItem[];
  revision: number | null;
}

const STATUSES: ReadonlySet<string> = new Set(['pending', 'active', 'done', 'failed']);

/** Tolerant `todo_write` parse: `items` with key/text/status(/reason) —
 * design D5 column names accepted in short and long form; revision from the
 * args, falling back to the echo result. Null when there is no usable item
 * list (the generic card remains the fallback). */
export function parseTodoPlan(
  args: Record<string, unknown> | null,
  res: Record<string, unknown> | null
): TodoPlan | null {
  const raw = args && Array.isArray(args.items)
    ? args.items
    : res && Array.isArray(res.items)
      ? res.items
      : null;
  if (!raw) return null;
  const items: TodoItem[] = [];
  const seen = new Set<string>();
  raw.forEach((item: unknown, i: number) => {
    const o = (item !== null && typeof item === 'object' ? item : {}) as Record<string, unknown>;
    const key = typeof o.key === 'string' && o.key
      ? o.key
      : typeof o.item_key === 'string' && o.item_key
        ? o.item_key
        : `item-${i}`;
    // Duplicate keys would make React remount siblings — disambiguate by position.
    const unique = seen.has(key) ? `${key}#${i}` : key;
    seen.add(unique);
    const status = typeof o.status === 'string' && STATUSES.has(o.status) ? (o.status as TodoStatus) : 'pending';
    items.push({
      key: unique,
      text: typeof o.text === 'string' ? o.text : typeof o.item_text === 'string' ? o.item_text : '',
      status,
      reason: typeof o.reason === 'string' ? o.reason : '',
    });
  });
  if (items.length === 0) return null;
  const argRev = args && typeof args.revision === 'number' ? args.revision : null;
  const resRev = res && typeof res.revision === 'number' ? res.revision : null;
  return { items, revision: argRev ?? resRev };
}

/** Header math: the numerator counts done items only; the denominator counts
 * every item, failed ones included. */
export function todoRatio(items: TodoItem[]): { done: number; total: number } {
  return {
    done: items.filter((i) => i.status === 'done').length,
    total: items.length,
  };
}

const ROW_TEXT: Record<TodoStatus, string> = {
  done: 'text-muted line-through',
  active: 'text-fg2',
  pending: 'text-muted',
  failed: 'text-danger',
};

function TodoMark({ status }: { status: TodoStatus }) {
  if (status === 'done') return <Icon name="check" size={13} className="shrink-0 text-muted"/>;
  if (status === 'active') {
    return (
      <span
        aria-hidden="true"
        className="od-genui-spin inline-block h-[11px] w-[11px] shrink-0 rounded-full border-[1.5px] border-accent border-t-transparent"
      />
    );
  }
  if (status === 'failed') return <Icon name="alert" size={13} className="shrink-0 text-danger"/>;
  return <span aria-hidden="true" className="inline-block h-[9px] w-[9px] shrink-0 rounded-full border border-muted"/>;
}

/** Shared row list — one `li` per item with the four row states (done struck
 * + dimmed, active spinning, pending dimmed, failed in danger with its reason
 * beneath). Exported for the session-todos popover (add-session-todos-surface
 * task 2.2) so popover rows and transcript cards can never drift. */
export function TodoPlanRows({ items }: { items: TodoItem[] }) {
  return (
    <ul className="border-t border-linesoft">
      {items.map((item) => (
        <li key={item.key} data-od-id={`todo-row-${item.key}`} className="px-2.5 py-1.5">
          <div className="flex items-center gap-2">
            <TodoMark status={item.status}/>
            <span className={cx('min-w-0 flex-1 truncate text-[12px]', ROW_TEXT[item.status])}>
              {item.text || '(untitled)'}
            </span>
          </div>
          {item.status === 'failed' && item.reason && (
            <p className="ms-[20px] mt-0.5 text-[11px] leading-4 text-danger">{item.reason}</p>
          )}
        </li>
      ))}
    </ul>
  );
}

interface TodoChecklistCardProps {
  plan: TodoPlan;
  odId: string;
}

export function TodoChecklistCard({ plan, odId }: TodoChecklistCardProps) {
  const { done, total } = todoRatio(plan.items);
  return (
    <div
      data-od-id={odId}
      className="mb-2 overflow-hidden rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
    >
      <div className="flex items-center gap-2 px-2.5 py-1.5">
        <Icon name="check-circle" size={13} className="shrink-0 text-meta"/>
        <span className="font-mono text-[12px] text-fg2">Todos</span>
        <span aria-live="polite" className="font-mono text-[10px] text-muted">
          {done}/{total}{plan.revision !== null ? ` · rev ${plan.revision}` : ''}
        </span>
      </div>
      <TodoPlanRows items={plan.items}/>
    </div>
  );
}

/** One-line collapse for earlier same-turn rewrites (spec: only the newest
 * todo_write renders the full checklist). Mirrors the generic card header's
 * height and typography so the turn's card stack stays level. */
export function TodoUpdatedSummary({ revision, odId }: { revision: number | null; odId: string }) {
  return (
    <div
      data-od-id={odId}
      className="mb-2 flex items-center gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-2.5 py-1.5"
    >
      <Icon name="check-circle" size={13} className="shrink-0 text-meta"/>
      <span className="font-mono text-[12px] text-fg2">Todos</span>
      <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted">
        Plan updated{revision !== null ? ` · rev ${revision}` : ''}
      </span>
    </div>
  );
}

/** Registry adapter: `todo_write` renders its args as the checklist. A failed
 * call or an unparsable item list falls back to the generic card. */
export function renderTodoCard(input: {
  args: Record<string, unknown> | null;
  res: Record<string, unknown> | null;
  ctx: GenerativeUiCtx;
}) {
  if (input.ctx.error) return null;
  const plan = parseTodoPlan(input.args, input.res);
  if (!plan) return null;
  return <TodoChecklistCard plan={plan} odId={'tool-' + input.ctx.tool}/>;
}
