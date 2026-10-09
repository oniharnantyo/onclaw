// Pure display + join helpers for the Subagents panel source (design D5/D7)
// and the transcript cards' deep-link affordance (task 6.3). No React, no
// store, no registration side effects — ToolCall imports this module for the
// card-side helpers, so it must stay side-effect-free.

import { joinTaskForCard, type TaskEntry, type TaskRecord } from "../../../../../lib/tasks";
import type { PanelCardInput } from "../../../../../lib/panel/registry";

/** The D5 join key for a transcript card: the delegation's `description`, the
 * shell's `command` — the same value the backend writes into the task record
 * the merged list carries. '' when args are absent/unparseable (mid-stream). */
export function cardTaskDescription(card: PanelCardInput | undefined | null): string {
  if (!card || typeof card.args !== 'string' || !card.args.trim()) return '';
  try {
    const args: unknown = JSON.parse(card.args);
    if (!args || typeof args !== 'object' || Array.isArray(args)) return '';
    const v = (args as Record<string, unknown>).description ?? (args as Record<string, unknown>).command;
    return typeof v === 'string' ? v.trim() : '';
  } catch {
    return '';
  }
}

/**
 * The card's 0-based ordinal among same-description cards of its message —
 * the D5 order tiebreak. `siblings` is the folded `tools[]` array
 * AgentMessage passes down; cards match their slot by reference or callId.
 * Returns -1 when there is no siblings context (or no description): the
 * deep-linker then matches by description alone and trusts only a UNIQUE
 * match (the callId-stable fallback).
 */
export function cardOrderIndex(card: PanelCardInput, siblings?: PanelCardInput[] | null): number {
  const description = cardTaskDescription(card);
  if (!description || !Array.isArray(siblings)) return -1;
  const idx = siblings.findIndex(
    (s) => s === card || (card.callId !== undefined && s?.callId === card.callId)
  );
  const limit = idx === -1 ? siblings.length : idx;
  let ordinal = 0;
  for (let i = 0; i < limit; i++) {
    if (cardTaskDescription(siblings[i]) === description) ordinal += 1;
  }
  return ordinal;
}

/**
 * The taskId a card deep-links to, or null when the panel should open on the
 * list view. With a sibling ordinal: the pinned D5 join (joinTaskForCard).
 * Without one: description alone, and only a unique match is trusted.
 */
export function deepLinkTaskId(entries: TaskEntry[], description: string, orderIndex: number): string | null {
  if (!description) return null;
  if (orderIndex >= 0) return joinTaskForCard(entries, description, orderIndex)?.taskId ?? null;
  const matches = entries.filter((e) => e.description === description);
  return matches.length === 1 ? matches[0].taskId : null;
}

const NAME_EXCERPT = 48;

/** Row/detail display name: shell tasks show a command excerpt (the
 * description IS the command on that lane); the task record carries no
 * subagent name for delegations — the honest 'Agent'. */
export function taskDisplayName(entry: TaskEntry): string {
  if (entry.kind !== 'shell') return 'Agent';
  const d = (entry.description || '').replace(/\s+/g, ' ').trim();
  if (!d) return 'Shell';
  return d.length > NAME_EXCERPT ? d.slice(0, NAME_EXCERPT - 1) + '…' : d;
}

/** D4 stop-gating note: foreground delegations are not tasks — the parent
 * run's stop is the only halt for them (spec "Task stop affordance"). */
export const FOREGROUND_STOP_NOTE =
  'Only background tasks can be stopped — a foreground delegation runs inside the parent and halts with the run.';

/**
 * D4 stop gating (pure). Enabled ONLY while a joinable task is still running
 * — a running entry is a background task by construction (foreground
 * delegations never become tasks, D3). An ended entry needs no stop. NO
 * entry (the detail addressed a foreground delegation, or the task is
 * unknown) renders the disabled control with the "background only"
 * explanation.
 */
export function stopGating(entry: TaskEntry | null | undefined): { enabled: boolean; note: string | null } {
  if (!entry) return { enabled: false, note: FOREGROUND_STOP_NOTE };
  if (entry.status !== 'running') return { enabled: false, note: null };
  return { enabled: true, note: null };
}

/**
 * True when a task the watcher previously saw RUNNING has gone terminal —
 * the badge-dot transition (6.2). Terminal entries that appear without a
 * prior running sighting (a list fold, a reload, the restart-lost
 * `created`) never badge: they are history, not a finish.
 */
export function terminalBadgeDue(prev: TaskEntry[] | undefined, next: TaskEntry[] | undefined): boolean {
  if (!prev || !next) return false;
  const before = new Map(prev.map((e) => [e.taskId, e.status]));
  return next.some((e) => before.get(e.taskId) === 'running' && e.status !== 'running');
}

const EXCERPT = 72;

/** One-line excerpt of a trajectory record for the live preview strip: text
 * verbatim (squeezed), tool calls by name, results shortened. */
export function recordExcerpt(record: TaskRecord): string {
  const squeeze = (s: string) => s.replace(/\s+/g, ' ').trim();
  if (record.type === 'tool_call') return '→ ' + (record.name || 'tool');
  const line = squeeze(record.type === 'tool_result' ? record.result || '' : record.text || '');
  if (!line) return record.type === 'tool_result' ? '(no result)' : '';
  return line.length > EXCERPT ? line.slice(0, EXCERPT - 1) + '…' : line;
}
