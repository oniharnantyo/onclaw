// Session todos data lane (add-session-todos-surface task 1.1): derives the
// direct-chat session's current todo plan from the newest `todo_write` tool
// call across the session's loaded messages. Web-only — the transcript's tool
// cards are the single source; no store state, no fetching, no polling. The
// live runtime mutates the last agent message's tools through deep-cloning
// updateTenant writes, so the messages array reference is the reactive
// trigger.

import { useMemo } from 'react';
import { parseArgs } from './toolDisplay';
import { parseTodoPlan, type TodoPlan, type TodoItem } from './generativeUi/TodoChecklistCard';
import { useStore } from '../store';

export interface LatestTodoCall {
  callId: string | null;
  plan: TodoPlan | null; // null when the newest call's list is empty/unparsable
}

/** The session's newest todo_write call: scan messages LAST→FIRST, within a
 * message scan its tools array LAST→FIRST — the first todo_write found is the
 * authoritative one. Returns null when no todo_write exists (or messages is
 * empty/undefined). */
export function latestTodoCall(messages: any[] | undefined | null): LatestTodoCall | null {
  if (!Array.isArray(messages)) return null;
  for (let i = messages.length - 1; i >= 0; i -= 1) {
    const tools = messages[i]?.tools;
    if (!Array.isArray(tools)) continue;
    for (let j = tools.length - 1; j >= 0; j -= 1) {
      const t = tools[j];
      if (t?.name !== 'todo_write') continue;
      // The newest call wins EVEN when unparsable — a cleared list must
      // clear the surface, never fall through to an older call's plan.
      return {
        callId: typeof t.callId === 'string' && t.callId ? t.callId : null,
        plan: parseTodoPlan(parseArgs(t.args), parseArgs(t.res)),
      };
    }
  }
  return null;
}

/** Convenience: latestTodoCall(messages)?.plan ?? null. The newest call stays
 * authoritative when unparsable — null here means "no visible plan", not
 * "scan further back". */
export function selectSessionTodos(messages: any[] | undefined | null): TodoPlan | null {
  return latestTodoCall(messages)?.plan ?? null;
}

/** True while any item is pending or active (auto-collapse predicate). */
export function hasOpenItems(items: TodoItem[]): boolean {
  return items.some((i) => i.status === 'pending' || i.status === 'active');
}

/** Present-only gate (denylist form, refactor-agent-tools-denylist): the
 * todo tools are exposed unless the agent's `disabled_tools` names
 * `todo_write` — an absent/empty denylist exposes everything. */
export function agentExposesTodoWrite(agent: any): boolean {
  if (!agent) return false;
  return !(Array.isArray(agent.disabled_tools) && agent.disabled_tools.includes('todo_write'));
}

// Stable constant for the no-call case — zustand v5 selector contract (see
// EMPTY_QUEUE in store/index.ts): minting a fresh object per store
// notification re-renders forever.
const NO_TODO_CALL: { plan: TodoPlan | null; callId: string | null } = { plan: null, callId: null };

/** Reactive hook over the store: the ACTIVE session of the CURRENT chat
 * (identical lookup to components/chat/ContextRing.tsx). Derivation is keyed
 * on the messages reference inside useMemo. */
export function useSessionTodoPlan(): { plan: TodoPlan | null; callId: string | null } {
  const messages = useStore((s: any) => {
    const th = s.db[s.pos.tenantId]?.threads[s.pos.chatId];
    return th && !Array.isArray(th)
      ? th.list.find((x: any) => x.id === th.active)?.messages
      : undefined;
  });
  return useMemo(() => {
    const call = latestTodoCall(messages);
    return call ? { plan: call.plan, callId: call.callId } : NO_TODO_CALL;
  }, [messages]);
}
