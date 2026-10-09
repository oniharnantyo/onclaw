// Per-session background task state (add-subagent-runs-surface, tasks 4.1):
// the client half of the session tasks API. This module owns the PINNED
// surface the floating card, Subagents panel, and transcript cards read:
//
//   - task state lives in the main store under `tasks` (a zustand slice in
//     store/index.ts — chatId-keyed, present-only: sessions without task
//     events carry nothing), addressed by the actions applyTaskList /
//     applyTaskFrame / applyTaskCompleted (re-exported here);
//   - `useTaskStream(chatId, enabled)` is the ref-counted live subscription:
//     the SSE progress stream runs while ANY consumer is enabled and tears
//     down when the last closes (chat-runtime spec — the subscription is
//     scoped to the consumer window). On (re)subscribe the REST list folds
//     first, then frames stream.
//
// Progress frames NEVER touch the transcript or the message list — they only
// grow the per-task trajectory buffers (chat-runtime spec). The transcript
// keeps exactly one card per delegation; `task_completed` is folded by the
// livechat translator (D9), not by this stream.
import { useEffect } from 'react';
import { api } from './api';
// API_ORIGIN/getToken are read through the module namespace instead of named
// imports: suites that mock './api' narrowly keep working — a missing export
// degrades to undefined rather than breaking the import binding (the same
// pattern livechat.ts/channelsLive.ts use).
import * as apiModule from './api';
import type { ApiSessionTask, ApiTaskRecord } from './api';
import { useStore } from '../store';

// ---------------------------------------------------------------------------
// Store-slice delegates. The actions live on the main store (a zustand slice
// in store/index.ts); these re-exports give consumers ONE import surface for
// the pinned contract (livechat's translator folds through applyTaskCompleted
// the same way).
// ---------------------------------------------------------------------------

/** Folds the session tasks REST list (merge, never clobber live buffers). */
export const applyTaskList = (chatId: string, tasks: TaskEntry[]): void =>
  useStore.getState().applyTaskList(chatId, tasks);

/** Appends one SSE progress frame to the addressed task's buffer. */
export const applyTaskFrame = (chatId: string, frame: TaskFrame): void =>
  useStore.getState().applyTaskFrame(chatId, frame);

/** Idempotent terminal status flip (`task_completed` + terminal frames). */
export const applyTaskCompleted = (chatId: string, payload: TaskCompletion): void =>
  useStore.getState().applyTaskCompleted(chatId, payload);

// ---------------------------------------------------------------------------
// Pinned types (wave-2 contract — do not rename)
// ---------------------------------------------------------------------------

export type TaskKind = 'delegation' | 'shell';

/** `created` is historical evidence (a task created without a persisted
 * completion — lost to a restart): never rendered as running. */
export type TaskStatus = 'running' | 'completed' | 'failed' | 'canceled' | 'created';

export interface TaskEntry {
  taskId: string;
  kind: TaskKind;
  status: TaskStatus;
  description: string;
  outputPath?: string;
  summary?: string;
}

export interface TaskRecord {
  sequence: number;
  type: 'text' | 'tool_call' | 'tool_result';
  text?: string;
  name?: string;
  args?: string;
  result?: string;
}

export interface TaskTerminal {
  outcome: 'completed' | 'failed' | 'canceled';
  outputPath?: string;
}

/** Completion payload for applyTaskCompleted — the folded `task_completed`
 * event (livechat translator) and the stream's terminal frame. */
export interface TaskCompletion {
  taskId: string;
  kind: string;
  outcome: string;
  outputPath?: string;
  summary?: string;
}

// ---------------------------------------------------------------------------
// Wire frames (pinned SSE contract 4): one task-keyed progress or terminal
// frame per `data:` payload. No frames for a task after its terminal frame.
// ---------------------------------------------------------------------------

export interface TaskProgressFrame {
  task_id: string;
  kind: string;
  sequence: number;
  record: { type: 'text' | 'tool_call' | 'tool_result'; text?: string; name?: string; args?: string; result?: string };
}

export interface TaskTerminalFrame {
  task_id: string;
  kind: string;
  sequence: number;
  terminal: { task_id: string; kind: string; outcome: 'completed' | 'failed' | 'canceled'; output_path?: string; summary?: string };
}

export type TaskFrame = TaskProgressFrame | TaskTerminalFrame;

/** Wire task row → store entry. Unknown kinds collapse onto `delegation` —
 * the pinned TaskKind union is closed and the delegation lane is the generic
 * one; a future kind degrades to a card, never a crash. */
export function taskFromWire(t: ApiSessionTask): TaskEntry {
  return {
    taskId: typeof t?.task_id === 'string' ? t.task_id : '',
    kind: t?.kind === 'shell' ? 'shell' : 'delegation',
    status: t?.status ?? 'created',
    description: typeof t?.description === 'string' ? t.description : '',
    ...(t?.output_path ? { outputPath: t.output_path } : {}),
    ...(t?.summary ? { summary: t.summary } : {}),
  };
}

/** Wire trajectory record → buffered TaskRecord (the store's camel-neutral
 * shape — the record fields are already single words). */
export function taskRecordFromWire(r: ApiTaskRecord): TaskRecord {
  return {
    sequence: typeof r?.sequence === 'number' ? r.sequence : 0,
    type: r?.type,
    ...(r?.text !== undefined ? { text: r.text } : {}),
    ...(r?.name !== undefined ? { name: r.name } : {}),
    ...(r?.args !== undefined ? { args: r.args } : {}),
    ...(r?.result !== undefined ? { result: r.result } : {}),
  };
}

// ---------------------------------------------------------------------------
// Selectors (chatId-scoped, present-only). Stable EMPTY constants keep the
// zustand v5 selector contract (see EMPTY_QUEUE in store/index.ts): a fresh
// array per notification would re-render forever.
// ---------------------------------------------------------------------------

const EMPTY_TASK_ENTRIES: TaskEntry[] = [];
const EMPTY_TASK_RECORDS: TaskRecord[] = [];

/** The chat's task entries — running first, then ended; stable order. */
export function useTaskEntries(chatId: string): TaskEntry[] {
  return useStore((s: any) =>
    chatId ? s.tasks[chatId]?.entries || EMPTY_TASK_ENTRIES : EMPTY_TASK_ENTRIES
  );
}

/** One task's live trajectory buffer. Ended tasks replay over REST
 * (`api.agents.taskOutput`) — this holds what streamed while subscribed. */
export function useTaskRecords(chatId: string, taskId: string): TaskRecord[] {
  return useStore((s: any) =>
    chatId && taskId ? s.tasks[chatId]?.records?.[taskId] || EMPTY_TASK_RECORDS : EMPTY_TASK_RECORDS
  );
}

// ---------------------------------------------------------------------------
// D5 call↔task join (display-only): an `agent` tool card matches entries by
// its args.description; `orderIndex` breaks ties between same-description
// cards (the card's ordinal among same-description cards). Null when
// unjoinable — the card still renders and deep-links to the list view.
// ---------------------------------------------------------------------------

export function joinTaskForCard(entries: TaskEntry[], description: string, orderIndex: number): TaskEntry | null {
  if (!Array.isArray(entries) || !description) return null;
  const matches = entries.filter((e) => e.description === description);
  if (orderIndex < 0 || orderIndex >= matches.length) return null;
  return matches[orderIndex];
}

// ---------------------------------------------------------------------------
// SSE consumer — the pinned progress stream (`GET …/sessions/:sid/tasks/stream`).
// Same mechanics as livechat.streamSessionEvents: fetch + ReadableStream
// reader (not native EventSource, which cannot send the auth header). Every
// `data:` frame is one JSON task frame; the single-shot promise resolves when
// the server closes the stream, calls onError on network/HTTP failure, and
// tears down silently on abort. Reconnection is out of scope by design — a
// consumer that re-arms re-runs the subscribe path (list first, then stream).
// ---------------------------------------------------------------------------

export interface StreamSessionTasksOptions {
  workspaceId: string;
  agentSlug: string;
  sessionId: string;
  signal?: AbortSignal;
  onFrame: (frame: TaskFrame) => void;
  onDone?: () => void;
  onError?: (err: unknown) => void;
}

/** Absolute API origin without hard import-binding to api.ts exports: suites
 * that mock './api' narrowly (no API_ORIGIN) resolve to the same-origin
 * default instead of breaking the import. Evaluated lazily — module-scope
 * namespace reads would throw under those mocks. */
function apiOrigin(): string {
  try {
    return (apiModule as any).API_ORIGIN ?? '';
  } catch {
    return '';
  }
}

/** The workspace member's JWT (same credential request() attaches), read
 * through the namespace so narrow api mocks without getToken degrade to an
 * unauthenticated probe rather than throwing. */
function bearerToken(): string | null {
  try {
    return (apiModule as any).getToken?.() ?? null;
  } catch {
    return null;
  }
}

export async function streamSessionTasks(opts: StreamSessionTasksOptions): Promise<void> {
  const { workspaceId, agentSlug, sessionId, signal, onFrame, onDone, onError } = opts;
  const url = `${apiOrigin()}/api/v1/workspaces/${encodeURIComponent(
    workspaceId
  )}/agents/${encodeURIComponent(agentSlug)}/sessions/${encodeURIComponent(sessionId)}/tasks/stream`;

  // Same auth as every other API call (request()): the workspace member's JWT.
  const headers: Record<string, string> = { Accept: 'text/event-stream' };
  const token = bearerToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  let finished = false;
  const finish = () => {
    if (finished) return;
    finished = true;
    onDone?.();
  };

  try {
    const res = await fetch(url, { headers, signal });
    if (!res.ok || !res.body) {
      throw new Error(`session task stream failed (HTTP ${res.status})`);
    }
    const contentType = res.headers.get('Content-Type') || '';
    if (contentType && !contentType.includes('text/event-stream')) {
      throw new Error(`session task stream returned unexpected content type ${contentType}`);
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let sawDone = false;
    const handleFrame = (frame: string) => {
      const data = frame
        .split('\n')
        .filter((line) => line.startsWith('data:'))
        .map((line) => line.slice('data:'.length).replace(/^ /, ''))
        .join('\n');
      if (!data) return;
      if (data === '[DONE]') {
        sawDone = true;
        return;
      }
      // A malformed frame must not tear down a healthy stream — skip it.
      try {
        onFrame(JSON.parse(data));
      } catch {
        // unparseable frame — ignored
      }
    };
    // The abort path must also unblock a pending read (mock streams and some
    // proxies don't reject in-flight reads on their own).
    const teardown = () => {
      try {
        void reader.cancel();
      } catch {
        // already closed
      }
    };
    if (signal) {
      if (signal.aborted) {
        teardown();
        return;
      }
      signal.addEventListener('abort', teardown, { once: true });
    }

    try {
      for (;;) {
        const { value, done } = await reader.read();
        if (value) {
          // Normalize CRLF so frames split identically however the server
          // ends its lines (a lone trailing \r pairs with the next chunk).
          buffer = (buffer + decoder.decode(value, { stream: true })).replace(/\r\n/g, '\n');
          let cut: number;
          while ((cut = buffer.indexOf('\n\n')) !== -1) {
            const frame = buffer.slice(0, cut);
            buffer = buffer.slice(cut + 2);
            handleFrame(frame);
            if (sawDone) break;
          }
        }
        if (sawDone || done) break;
      }
      // A server that closes without the trailing blank line still gets its
      // final frame processed.
      if (!sawDone) {
        buffer += decoder.decode();
        if (buffer.trim()) handleFrame(buffer);
      }
    } finally {
      signal?.removeEventListener('abort', teardown);
      teardown();
    }
    // Aborted mid-stream: clean teardown — neither onDone nor onError.
    if (signal?.aborted) return;
    finish();
  } catch (err) {
    if (signal?.aborted) return;
    onError?.(err);
    return;
  }
}

// ---------------------------------------------------------------------------
// Ref-counted subscription (chat-runtime spec — "subscription scoped to the
// consumer window"): the stream runs while ANY consumer for the chat is
// enabled; the last close tears it down. The REST list folds BEFORE the
// stream opens on every (re)subscribe, so a re-armed panel repopulates ended
// tasks and sequence dedup keeps replayed frames from double-appending.
// ---------------------------------------------------------------------------

const taskStreamRefcounts = new Map<string, number>();
const taskStreamControllers = new Map<string, AbortController>();

/** Starts the chat's subscription: REST list first (applyTaskList), then the
 * SSE stream, dispatching progress/terminal frames to the store. */
function startTaskStream(chatId: string, workspaceId: string, agentSlug: string, sessionId: string): void {
  // Last-writer-wins per chat (mirrors attachCatchUpStream): a superseded
  // controller — a session switch racing an old subscribe — aborts.
  taskStreamControllers.get(chatId)?.abort();
  const controller = new AbortController();
  taskStreamControllers.set(chatId, controller);
  const aborted = () => controller.signal.aborted;

  void (async () => {
    // The merged task list first (spec: on subscribe, fetch the list then
    // stream). A failure (old backend, transient) still opens the stream —
    // frames create their own entries.
    try {
      const res = await api.agents.listTasks(workspaceId, agentSlug, sessionId);
      if (!aborted()) {
        useStore.getState().applyTaskList(chatId, (res?.tasks || []).map(taskFromWire));
      }
    } catch {
      // old backend / offline — the stream path below still runs
    }
    if (aborted()) return;
    await streamSessionTasks({
      workspaceId,
      agentSlug,
      sessionId,
      signal: controller.signal,
      onFrame: (frame) => {
        if (aborted() || !frame || typeof frame.task_id !== 'string' || !frame.task_id) return;
        if ('terminal' in frame && frame.terminal) {
          useStore.getState().applyTaskCompleted(chatId, {
            taskId: frame.terminal.task_id || frame.task_id,
            kind: frame.terminal.kind || frame.kind,
            outcome: frame.terminal.outcome,
            ...(frame.terminal.output_path ? { outputPath: frame.terminal.output_path } : {}),
            ...(frame.terminal.summary ? { summary: frame.terminal.summary } : {}),
          });
        } else if ('record' in frame && frame.record) {
          useStore.getState().applyTaskFrame(chatId, frame);
        }
      },
    });
  })();
}

/** Acquires one consumer token for the chat; resolves an unsubscribe that is
 * idempotent (StrictMode double-invokes) and tears the stream down when the
 * LAST consumer for the chat releases. */
function acquireTaskStream(chatId: string, workspaceId: string, sessionId: string): () => void {
  const next = (taskStreamRefcounts.get(chatId) || 0) + 1;
  taskStreamRefcounts.set(chatId, next);
  if (next === 1) startTaskStream(chatId, workspaceId, chatId, sessionId);
  let released = false;
  return () => {
    if (released) return;
    released = true;
    const count = (taskStreamRefcounts.get(chatId) || 1) - 1;
    if (count <= 0) {
      taskStreamRefcounts.delete(chatId);
      taskStreamControllers.get(chatId)?.abort();
      taskStreamControllers.delete(chatId);
    } else {
      taskStreamRefcounts.set(chatId, count);
    }
  };
}

/** The active session's server address for a chat — the binding the tasks
 * endpoints address (mirrors deleteSession's address rule, plus `sched_`):
 * the `sess` slot when set, else the id itself when it is server-bound. */
function activeTaskSessionId(s: any, chatId: string): string | null {
  if (!chatId) return null;
  const th = s.db[s.pos.tenantId]?.threads?.[chatId];
  if (!th || Array.isArray(th)) return null;
  const sess = th.list?.find((x: any) => x.id === th.active);
  if (!sess) return null;
  if (typeof sess.sess === 'string' && sess.sess) return sess.sess;
  const id = typeof sess.id === 'string' ? sess.id : '';
  return id.startsWith('sess_') || id.startsWith('sched_') ? id : null;
}

/**
 * Subscribes the mounting consumer to the chat's session tasks stream while
 * `enabled` (chat-runtime spec: an open Subagents panel or expanded live
 * preview). The chat's active session supplies the wire address; when it
 * cannot be resolved there is nothing to subscribe to. A session switch
 * inside the chat re-subscribes through the effect's dependencies.
 */
export function useTaskStream(chatId: string, enabled: boolean): void {
  const workspaceId = useStore((s: any) => s.pos.tenantId);
  const sessionId = useStore((s: any) => activeTaskSessionId(s, chatId));
  useEffect(() => {
    if (!enabled || !chatId || !workspaceId || !sessionId) return;
    return acquireTaskStream(chatId, workspaceId, sessionId);
  }, [chatId, enabled, workspaceId, sessionId]);
}
