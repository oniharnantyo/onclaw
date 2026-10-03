import { useCallback } from 'react';
import { useExternalStoreRuntime } from '@assistant-ui/react';
import type { ThreadMessageLike, AppendMessage } from '@assistant-ui/react';
import { useStore, useWorkspace } from '../store';
import { getWorkspaceKey } from '../store/workspaceKeys';
import type { Message, Agent } from '../data/types';
import { uid, nowTime, parseMentions, appendReasoningPart, appendToolPart } from '../lib/helpers';
import { runTurn, sessionIdFromResponseId, type TurnUsage } from '../lib/openresponses';
import { markLiveChatDisconnected, handleV1AuthFailure, hydrateSession, attachCatchUpStream, abortCatchUpStream, isBoundSessionId } from '../lib/livechat';
import { api } from '../lib/api';
import type { AttachmentChip, DocumentMentionChip } from '../lib/attachments';
import { recordTurnTiming as stashTurnTiming } from './turnTiming';

/** Parseable ISO wall-clock instant for live entries — the display `ts`
 * (nowTime) is a locale string the transcript's day separators cannot parse,
 * so every minted entry also carries `at` (adopt-assistant-ui-elements D11). */
const nowIso = () => new Date().toISOString();

const activeTimers: Record<string, any> = {};

/** Composer chips → the chips array carried by the /v1 turn request. Ready
 * file chips become ChatAttachment references (add-chat-attachments D11);
 * document mention chips (add-reference-documents 10.4) pass through as their
 * `{kind: "document", documentId, name, path}` identity object. Filtering on
 * the ready state again here keeps rejected / failed / still-uploading chips
 * out of the wire even if a caller bypasses the send gate. */
const toTurnChips = (chips?: (AttachmentChip | DocumentMentionChip)[]): ChatAttachment[] | undefined => {
  const out: ChatAttachment[] = [];
  for (const c of chips || []) {
    if ('kind' in c && c.kind === 'document') {
      out.push({ kind: 'document', documentId: c.documentId, name: c.name, path: c.path });
    } else {
      const a = c as AttachmentChip;
      if (a.state === 'ready' && a.url) out.push({ id: a.id, name: a.name, mime: a.mime, size: a.size, url: a.url });
    }
  }
  return out.length ? out : undefined;
};

/** File-attachment references only — the optimistic transcript entry renders
 * chips from `attachments`, and a document mention's visible pill is the
 * markdown link in the text (never a second chip card). The document
 * identities ride the turn request only. Undefined when nothing file-shaped
 * remains, so a mention-only send stores no attachments key at all. */
const fileRefsOnly = (chips?: ChatAttachment[]): ChatAttachment[] | undefined => {
  const out = chips?.filter((a) => a.kind !== 'document');
  return out && out.length ? out : undefined;
};

// ---------------------------------------------------------------------------
// Live session binding (web-live-chat-sessions design D2/D5)
// ---------------------------------------------------------------------------

/**
 * Hybrid binding (design D2): a turn with no recorded response chain BIRTHS,
 * carrying the session's `sess_<uuid>` as `metadata.onclaw_session` (minted /
 * lazily migrated via the store's ensureSessionBinding); every later turn
 * chains via `previous_response_id` from the last recorded assistant `resp`.
 */
const resolveBinding = (tid: string, cid: string): { sessionId?: string; previousResponseId?: string } => {
  const chained = useStore.getState().getLastResponse(cid);
  if (chained) return { previousResponseId: chained };
  const sessionId = useStore.getState().ensureSessionBinding(cid);
  return sessionId ? { sessionId } : {};
};

/** The thread's ALREADY-bound server session id, read without minting one —
 * a compact turn binds like any metadata-bound request but never births a
 * session (chat-compact-command design D2). Undefined when the thread has no
 * binding yet. */
const existingBoundSessionId = (tid: string, cid: string): string | undefined => {
  const th = useStore.getState().db[tid]?.threads?.[cid];
  const s = th && th.list.find((x: any) => x.id === th.active);
  if (isBoundSessionId(s?.id)) return s!.id;
  if (isBoundSessionId(s?.sess)) return s!.sess;
  return undefined;
};

/** Identity of the in-flight live turn, captured at TURN START (not the first
 * stream event) so the stop control can always reach the server-side run.
 * `responseId` still arrives only with the first stream event — the cancel
 * endpoint is session-scoped, so stop-before-first-event addresses the run by
 * `sessionId` with a placeholder turn segment. `mid` is the optimistic agent
 * message the turn streams into — a turn that fails before producing anything
 * gets that empty row retracted. `abort` tears down the local SSE stream so
 * a stopped turn stops mutating the transcript immediately. */
const inFlight: { responseId?: string; agentSlug?: string; sessionId?: string; mid?: string; tid?: string; cid?: string; abort?: () => void; flush?: () => void } = {};
const clearInFlight = () => { inFlight.responseId = undefined; inFlight.agentSlug = undefined; inFlight.sessionId = undefined; inFlight.mid = undefined; inFlight.tid = undefined; inFlight.cid = undefined; inFlight.abort = undefined; inFlight.flush = undefined; };

/** Terminal-event meter capture (design D5), shared by every turn path
 * (ordinary, regenerate, compact): a usable final-call input records; no
 * usage block (or no number) clears so the meter hides rather than showing a
 * stale/zero value. The turn block feeds the context popover's input/output
 * detail rows and carries the server-provided context breakdown (D7) when
 * the wire reported one. */
const recordTurnUsage = (tid: string, cid: string, u: TurnUsage | null) => {
  const contextBreakdown = u?.contextBreakdown;
  useStore.getState().recordThreadUsage(
    tid,
    cid,
    u && typeof u.finalInputTokens === 'number' ? u.finalInputTokens : null,
    u ? {
      input: u.inputTokens,
      output: u.outputTokens,
      ...(contextBreakdown ? { contextBreakdown } : {}),
    } : undefined,
  );
};

/** Message timing (assistant-ui "message timing" element adoption):
 * client-measured first-token latency, total turn time, and streamed speed,
 * recorded into the runtime-local registry (turnTiming.ts) at turn
 * completion. Never the store — persisted session state must stay timing-free
 * so hydrated history renders no line (9.1). Present-only: no first streamed
 * token → no record at all. Speed estimates tokens from the streamed visible
 * text length (~4 chars/token) over the streaming window; short streams
 * (≤250 ms) skip the noisy figure. */
const recordTurnTiming = (mid: string | undefined, t0: number, firstStreamMs: number | undefined, streamedChars: number) => {
  if (!mid || firstStreamMs === undefined) return;
  const totalMs = Math.round(performance.now() - t0);
  const streamMs = Math.max(1, totalMs - firstStreamMs);
  const tps = streamMs > 250 ? Math.round((streamedChars / 4) / (streamMs / 1000)) : undefined;
  stashTurnTiming(mid, { firstMs: Math.round(firstStreamMs), totalMs, ...(tps ? { tps } : {}) });
};

/** The chat's bound server session id (`sess_<uuid>`), for cancel addressing
 * before the minted response id exists. Legacy counter sessions bind nothing. */
const activeBoundSessionId = (tid: string, cid: string): string | undefined => {
  const th = useStore.getState().db[tid]?.threads?.[cid];
  const s = th && th.list.find((x: any) => x.id === th.active);
  return isBoundSessionId(s?.id) ? s!.id : undefined;
};

/** Retracts the optimistic empty agent message left behind by a turn that
 * died before any text or tool card landed (e.g. a provider 429). */
const retractIfEmpty = () => {
  const { mid, tid, cid } = inFlight;
  if (!mid || !tid || !cid) return;
  const th = useStore.getState().db[tid]?.threads?.[cid];
  if (!th || Array.isArray(th)) return;
  for (const sess of th.list || []) {
    const m = (sess.messages || []).find((x: any) => x.id === mid);
    if (!m) continue;
    if (!m.text && (!m.tools || m.tools.length === 0)) {
      useStore.getState().dropMsg(tid, cid, mid);
    }
    return;
  }
};

export function useChatRuntime(chatId: string) {
  const patchUi = useStore(s => s.patchUi);
  const tenantId = useStore(s => s.pos.tenantId);
  const tenant = useWorkspace();
  const threadData = tenant?.threads[chatId];
  const session = threadData && threadData.list.find((x: any) => x.id === threadData.active);
  const messages = session ? session.messages : [];

  // Chat-scoped run state (fix-thinking-leak-on-chat-switch): `running` is
  // tab-global store state — a turn streaming in chat A must not light chat
  // B's composer/thinking row, and must not gate B's message conversion as
  // "currently running". Writers stamp ui.runningChatId with the turn's chat.
  const running = useStore(s => !!s.ui.running && s.ui.runningChatId === chatId);

  // Auto-dispatch (message queue 8.3, design D9): when the active run hits a
  // terminal state, the FIRST queued message dispatches automatically —
  // through the NORMAL send path (pushMsg → respondFor, exactly what onNew
  // does), so a 409 from a session that went busy elsewhere (another tab,
  // cron) falls back to the existing catch-up-and-redispatch flow instead of
  // a bypass. The entry is dequeued before sending, so its row clears the
  // moment the dispatch starts.
  const dispatchQueuedHead = (tid: string, cid: string) => {
    const state = useStore.getState();
    const head = state.dequeueChatMessage(tid, cid);
    if (!head) return;
    const agent = state.db[tid]?.agents.find((a: any) => a.id === cid);
    if (!agent) return;
    state.pushMsg(tid, cid, {
      id: uid('m'), author: 'you', ts: nowTime(), at: nowIso(), text: head.text,
      ...(head.attachments ? { attachments: head.attachments } : {}),
    });
    respondFor(tid, cid, agent, head.text, { attachments: head.attachments });
  };

  const convertMessage = useCallback((msg: Message): ThreadMessageLike => {
    // Note: convertMessage drops branch-variant resolution it previously had.
    // This is harmless today as branches map back to m.text, but worth documenting this coupling.
    const content: any[] = [{ type: 'text', text: msg.text || '' }];

    if (msg.tools && msg.tools.length > 0) {
      // Last line of defense against Duplicate key in useResources
      // (fix-duplicate-tool-call-cards D3): duplicate card entries — e.g. a
      // turn corrupted before the stream-side dedupe existed — must never
      // produce duplicate resource keys. First occurrence wins.
      const emitted = new Set<string>();
      msg.tools.forEach((tool: any, i: number) => {
        // Gate tool parts on the currently running message
        const isCurrentlyRunning = running && msg.id === session?.messages[session.messages.length - 1]?.id;
        if (!isCurrentlyRunning) {
           // Real call ids are unique per invocation; legacy/seeded cards
           // only carry a name, so disambiguate by index — two calls to the
           // same tool in one turn (e.g. a 404 retry) must not collide in
           // assistant-ui's useResources.
           const toolCallId = tool.callId ? `call_${tool.callId}` : `call_${msg.id}_${tool.name}_${i}`;
           if (emitted.has(toolCallId)) return;
           emitted.add(toolCallId);
           content.push({
             type: 'tool-call',
             toolName: tool.name,
             toolCallId,
             args: { raw: tool.args },
           });
        }
      });
    }

    return {
      id: msg.id,
      role: msg.author === 'agent' ? 'assistant' : msg.author === 'error' ? 'system' : 'user',
      content,
      createdAt: msg.ts ? new Date(msg.ts) : undefined,
      metadata: {
        custom: {
          onclaw: {
            agentId: msg.agentId,
            scheduler: msg.scheduler,
            name: msg.name,
            author: msg.author
          }
        }
      }
    };
  }, [running, session]);

  const respondFor = useCallback((tid: string, cid: string, ag: Agent, origText: string, opts?: any) => {
    const delay = opts?.delay;
    const mentioned = opts?.mentioned;
    // queued: this call is the re-dispatch after a 409-conflict queue (the
    // active run that blocked it has drained) — a second conflict must
    // surface as a real failure instead of queueing again.
    const queued = opts?.queued;
    // attachments: the turn's ready chips (add-chat-attachments D11) as
    // ChatAttachment references — carried by the turn request AND by the
    // 409-conflict queued re-dispatch below.
    const attachments: ChatAttachment[] | undefined = opts?.attachments;
    // compacting: false — an ordinary turn never shows the compact status row
    // even if a stale compacting flag survived a chat/session switch.
    patchUi({ running: true, compacting: false, runningChatId: cid });

    // Real turn via the OpenResponses /v1 surface when a workspace chat key
    // is held (agent = model, key = tenant). Without a key the chat shows
    // its connect/retry state — the live UI never runs canned replies.
    const apiKey = getWorkspaceKey(tid);
    if (apiKey) {
      const mid = uid('m');
      // Message-timing t0 (turn start, not first stream event) and the ISO
      // wall-clock date for the transcript's day separators — `ts` stays the
      // display string; `at` is the parseable one.
      const t0 = performance.now();
      let firstStreamMs: number | undefined;
      let streamedChars = 0;
      useStore.getState().pushMsg(tid, cid, {
        id: mid, author: 'agent', agentId: ag.id, ts: nowTime(), at: nowIso(), text: '',
      });
      // Resolve birth-vs-chain AFTER the optimistic message lands so the
      // recorded `resp` chain reflects the thread's real history.
      const binding = resolveBinding(tid, cid);
      // Live tool/approval cards mutate the tools array of the streaming
      // agent message — same shape the mock path renders. `msg` gives the
      // callback access to the ordered body (parts) of the same message.
      const patchTools = (fn: (tools: any[], msg: any) => void) => {
        useStore.getState().updateTenant(tid, (t: any) => {
          const s = t.threads[cid]?.list.find((x: any) => x.id === t.threads[cid].active);
          const last = s?.messages[s.messages.length - 1];
          if (last && last.author === 'agent') {
            if (!last.tools) last.tools = [];
            fn(last.tools, last);
          }
          return t;
        });
      };

      // Stream deltas coalesce into ONE store write per macrotask: a stream
      // chunk can carry dozens of events, and per-event store updates drive
      // React into 50+ consecutive synchronous update cycles (each commit's
      // effects — assistant-ui's adapter resync — schedule the next one),
      // tripping the nested-update limit. The throw lands in runTurn's catch
      // and surfaced as a "Run failed: Maximum update depth exceeded" entry.
      // Low-frequency structural events (tool calls, outputs, approval, done,
      // error, stop) flush the buffer synchronously first, so part order and
      // partial-text detection stay exactly as with per-delta writes.
      let pendingText = '';
      let pendingReasoning: string[] = [];
      let flushQueued = false;
      let flushTimer: ReturnType<typeof setTimeout> | null = null;
      const flushDeltas = () => {
        if (flushQueued) { clearTimeout(flushTimer); flushQueued = false; }
        const text = pendingText; pendingText = '';
        const reasoning = pendingReasoning; pendingReasoning = [];
        if (!text && reasoning.length === 0) return;
        useStore.getState().updateTenant(tid, (t: any) => {
          const s = t.threads[cid]?.list.find((x: any) => x.id === t.threads[cid].active);
          const last = s?.messages[s.messages.length - 1];
          if (last && last.author === 'agent') {
            if (text) last.text = (last.text || '') + text;
            for (const d of reasoning) appendReasoningPart(last, d);
          }
          return t;
        });
      };
      const queueFlush = () => {
        if (flushQueued) return;
        flushQueued = true;
        flushTimer = setTimeout(flushDeltas, 0);
      };
      inFlight.flush = flushDeltas;

      // startTurn streams into the already-pushed optimistic agent message,
      // so a post-auth-failure retry (design D4: clear slot → re-exchange once
      // → retry) reuses the same message instead of duplicating it. A fresh
      // AbortController per attempt: the cancel path aborts the OPEN stream;
      // a controller created before startTurn could sit pre-aborted through
      // the queued re-dispatch and silently kill the retry.
      const startTurn = (key: string, attempt: number) => {
        const turnAbort = new AbortController();
        inFlight.mid = mid; inFlight.tid = tid; inFlight.cid = cid;
        inFlight.agentSlug = (ag as any).slug || ag.id;
        inFlight.sessionId = binding.sessionId;
        inFlight.abort = () => turnAbort.abort();
        void runTurn(key, {
          agentSlug: (ag as any).slug || ag.id,
          input: origText,
          ...(attachments?.length ? { attachments } : {}),
          ...(binding.sessionId ? { sessionId: binding.sessionId } : {}),
          ...(binding.previousResponseId ? { previousResponseId: binding.previousResponseId } : {}),
        }, {
          onResponseId: (rid) => {
            inFlight.responseId = rid;
            inFlight.agentSlug = (ag as any).slug || ag.id;
          },
          onDelta: (delta) => {
            if (firstStreamMs === undefined) firstStreamMs = performance.now() - t0;
            streamedChars += delta.length;
            pendingText += delta;
            queueFlush();
          },
          onReasoningDelta: (delta) => {
            // Reasoning is its own ordered bubble (part) on the turn body —
            // never leaks into the visible text. Consecutive deltas extend
            // the open segment; a delta after a tool card opens a new one,
            // so round-2 reasoning sits between the cards and the text.
            if (firstStreamMs === undefined) firstStreamMs = performance.now() - t0;
            pendingReasoning.push(delta);
            queueFlush();
          },
          onToolCall: (name, callId, args) => {
            flushDeltas();
            patchTools((tools, msg) => {
              // One card per call id for the life of the turn
              // (fix-duplicate-tool-call-cards D3): a repeat event for an
              // existing id mutates that card, never mints. Added fires
              // argless (undefined must not clobber); done is the
              // authoritative update even when its arguments string is
              // empty — gating the lookup on truthy args let an empty-args
              // done mint a duplicate card and crash useResources.
              const existing = tools.find((t: any) => t.callId === callId);
              if (existing) {
                if (args !== undefined) existing.args = args;
                return;
              }
              tools.push({ callId, name, args: args || '', ms: 0 });
              appendToolPart(msg, tools.length - 1);
            });
          },
          onToolOutput: (callId, _name, result, latencyMs, isError) => {
            flushDeltas();
            patchTools((tools) => {
              const card = [...tools].reverse().find((t: any) => t.callId === callId) || tools[tools.length - 1];
              if (!card) return;
              card.res = result;
              card.ms = latencyMs ?? 0;
              if (isError) card.error = result;
            });
          },
          onApprovalRequired: (a) => {
            flushDeltas();
            patchTools((tools, msg) => {
              // tool rides only on service-run write escalations
              // (add-integration-authority): the transcript card branches on it.
              tools.push({ args: '', ms: 0, approval: { interruptId: a.interrupt_id, command: a.command, resolved: false, ...(a.tool ? { tool: a.tool } : {}) } });
              appendToolPart(msg, tools.length - 1);
            });
            // The turn is paused server-side until the approval card is acted
            // on, so stop the composer spinner instead of spinning forever.
            clearInFlight();
            useStore.getState().patchUi({ running: false });
          },
          onUsage: (u) => {
            recordTurnUsage(tid, cid, u);
          },
          onDone: (responseId) => {
            flushDeltas();
            clearInFlight();
            // Record the minted response id on the assistant message it
            // produced — this is the `previous_response_id` chain link for the
            // next turn.
            if (responseId) useStore.getState().recordResponse(cid, mid, responseId);
            recordTurnTiming(mid, t0, firstStreamMs, streamedChars);
            useStore.getState().patchUi({ running: false });
            // Run finished with queued messages pending (message queue 8.3):
            // the first one dispatches automatically, in order.
            dispatchQueuedHead(tid, cid);
          },
          onError: (message, meta) => {
            flushDeltas();
            if (meta?.unauthorized && attempt === 0) {
              // Stale workspace key: clear the slot, re-exchange once, and
              // retry this turn with the fresh key; handleV1AuthFailure has
              // already surfaced the connect state if the exchange failed.
              void handleV1AuthFailure(tid).then((fresh) => {
                if (fresh) {
                  startTurn(fresh, 1);
                  return;
                }
                useStore.getState().patchUi({ running: false });
              });
              return;
            }
            if (meta?.conflict && !queued) {
              // 409 from the per-session run lock: another run still holds
              // this session (this page reloaded mid-turn, a second tab, or
              // cron). Queue the send behind it — attach the catch-up stream
              // so the active turn streams to completion HERE, then re-run
              // this turn once. The queued retry drops the guard: a second
              // conflict surfaces as a real failure instead of looping.
              retractIfEmpty();
              clearInFlight();
              const agentSlug = (ag as any).slug || ag.id;
              const sessionId = binding.sessionId || sessionIdFromResponseId(binding.previousResponseId || '');
              if (!sessionId) {
                // No session coordinates to reattach under — surface it.
                useStore.getState().pushMsg(tid, cid, {
                  id: uid('m'), author: 'error', ts: nowTime(), at: nowIso(), text: '', error: message,
                });
                useStore.getState().toast(message, 'error');
                useStore.getState().patchUi({ running: false });
                return;
              }
              useStore.getState().toast('A run is still active — your message will follow it.');
              useStore.getState().patchUi({ running: true, runningChatId: cid });
              void hydrateSession({ workspaceId: tid, agentSlug, chatId: cid, sessionId }).then((hydrated) => {
                attachCatchUpStream({
                  workspaceId: tid,
                  agentSlug,
                  chatId: cid,
                  sessionId,
                  after: hydrated?.lastEventId,
                  onDone: () => respondFor(tid, cid, ag, origText, { queued: true, attachments }),
                  onError: () => {
                    // The catch-up stream died mid-run — drop the queue; the
                    // composer is live again and the user can resend.
                    useStore.getState().toast('Lost the live stream — send your message again.');
                  },
                });
              });
              return;
            }
            // Terminal failure (e.g. provider 429): retract the empty
            // optimistic row — it would otherwise render as a forever-loading
            // placeholder next to the error toast. Runs before clearInFlight,
            // which wipes the turn identity retractIfEmpty reads.
            retractIfEmpty();
            clearInFlight();
            // In-thread error entry (design D8): makes the turn's outcome
            // visible where the retracted row used to be. Auth failures keep
            // the connect-state path above and never append.
            useStore.getState().pushMsg(tid, cid, {
              id: uid('m'), author: 'error', ts: nowTime(), at: nowIso(), text: '', error: message,
            });
            useStore.getState().toast(message, 'error');
            useStore.getState().patchUi({ running: false });
            // Terminal failure still ends the active run (message queue 8.3):
            // the first queued message proceeds — through the normal send
            // path, where its own failure handling applies.
            dispatchQueuedHead(tid, cid);
          },
        }, { signal: turnAbort.signal });
      };
      void startTurn(apiKey, 0);
      return;
    }

    // No usable chat key (design D8, task 5.3): the live UI never runs
    // canned replies. Surface the connect/retry state — the user's message
    // stays in the transcript and Retry re-provisions the workspace key.
    markLiveChatDisconnected(tid);
    useStore.getState().patchUi({ running: false });
  }, [patchUi]);

  /** Submits a /compact turn (chat-compact-command): a NORMAL /v1 request
   * carrying `metadata.onclaw_command: "compact"` with the focus text as
   * input. Never appends a user message and never mints optimistic rows —
   * while running the transcript shows only the "Compacting context…" status
   * row (ui.compacting). Terminal states (design D7): the compacted event
   * swaps the row for the compaction divider; quiet completion (no compacted
   * event) or failure leaves NOTHING in the thread. */
  const respondCompact = useCallback((tid: string, cid: string, ag: Agent, focus: string, attempt = 0) => {
    patchUi({ running: true, compacting: true, runningChatId: cid });
    const apiKey = getWorkspaceKey(tid);
    if (!apiKey) {
      markLiveChatDisconnected(tid);
      useStore.getState().patchUi({ running: false, compacting: false });
      return;
    }
    // Bind-only (design D2): chain from the last recorded resp, else use the
    // session's existing binding — a compact request never births a session.
    const chained = useStore.getState().getLastResponse(cid);
    const binding = chained
      ? { previousResponseId: chained }
      : { sessionId: existingBoundSessionId(tid, cid) };

    const turnAbort = new AbortController();
    // No `mid`: the stop control has no optimistic row to retract, but the
    // server-side run stays cancellable via the session/decoded response id.
    inFlight.tid = tid; inFlight.cid = cid;
    inFlight.agentSlug = (ag as any).slug || ag.id;
    inFlight.sessionId = binding.sessionId;
    inFlight.abort = () => turnAbort.abort();
    let dividerId: string | undefined;
    const finish = () => {
      clearInFlight();
      useStore.getState().patchUi({ running: false, compacting: false });
      // A compact turn is a real run (message queue 8.3): sends that queued
      // behind it dispatch from its terminal state like any other.
      dispatchQueuedHead(tid, cid);
    };
    void runTurn(apiKey, {
      agentSlug: (ag as any).slug || ag.id,
      input: focus,
      command: 'compact',
      ...(binding.sessionId ? { sessionId: binding.sessionId } : {}),
      ...(binding.previousResponseId ? { previousResponseId: binding.previousResponseId } : {}),
    }, {
      // A compact stream carries no output items — no text ever delta-frames.
      onDelta: () => {},
      onResponseId: (rid) => {
        inFlight.responseId = rid;
        inFlight.agentSlug = (ag as any).slug || ag.id;
      },
      onContextCompacted: ({ tokensBefore, tokensAfter }) => {
        // Success marker: the divider swaps in for the status row at the
        // compacted event; the turn keeps running until the terminal event
        // lands the summarizer usage.
        dividerId = uid('m');
        useStore.getState().pushMsg(tid, cid, {
          id: dividerId, author: 'compaction', ts: nowTime(), at: nowIso(), text: '',
          compaction: { tokensBefore, tokensAfter },
          summarySaved: true,
        } as any);
        useStore.getState().patchUi({ compacting: false });
      },
      onUsage: (u) => {
        // Same terminal-event meter capture as ordinary turns (design D5):
        // turn_completed carries the summarizer call's usage.
        recordTurnUsage(tid, cid, u);
      },
      onDone: (responseId) => {
        // The compact turn is a real run: record its minted response id on
        // the divider entry so later turns chain to it via
        // previous_response_id — the pre-compaction chain was invalidated by
        // the window replacement.
        if (responseId && dividerId) useStore.getState().recordResponse(cid, dividerId, responseId);
        finish();
      },
      onError: (message, meta) => {
        if (meta?.unauthorized && attempt === 0) {
          // Stale workspace key: clear the slot, re-exchange once, and retry
          // this compact turn with the fresh key (same contract as ordinary
          // turns, design D4). The status row stays up while retrying.
          void handleV1AuthFailure(tid).then((fresh) => {
            if (fresh) { respondCompact(tid, cid, ag, focus, 1); return; }
            finish();
          });
          return;
        }
        // Terminal failure (including a 409 from a concurrent run): the
        // status row retracts and NOTHING stays in the thread (design D7) —
        // a compact turn cannot be queued behind the active run like an
        // ordinary message, so the conflict surfaces as a toast only.
        finish();
        useStore.getState().toast(message, 'error');
      },
    }, { signal: turnAbort.signal });
  }, [patchUi]);

  // `chips` carries the composer's ready attachments (add-chat-attachments
  // D11) plus any document mention chips (add-reference-documents 10.4): both
  // land on the turn request — file chips also land on the optimistic user
  // entry so the bubble renders them exactly like a hydrated one, while a
  // document mention's visible pill is the markdown link in the text, so only
  // the file refs go on the entry. Attachment-only sends (empty text, ≥1
  // ready chip) are valid and proceed like any turn.
  const onNew = useCallback(async (msg: AppendMessage, chips?: (AttachmentChip | DocumentMentionChip)[]) => {
    const target = useStore.getState().db[tenantId]?.agents.find((a: any) => a.id === chatId) ? 'agent' : 'channel';
    const text = msg.content.map((c: any) => c.text).join('') || '';
    const trimmed = text.trim();
    const turnChips = toTurnChips(chips);
    const attachments = fileRefsOnly(turnChips);

    // /compact interception (chat-compact-command D7): agent chats only, an
    // exact `/compact` or `/compact <focus>` match submits a compact turn —
    // the command text never enters the message list. Every other /command
    // sends as ordinary text in every surface (D8); channel/team composers
    // never even open the command menu.
    const compactMatch = target === 'agent' ? trimmed.match(/^\/compact(?:\s+(.*))?$/) : null;
    if (compactMatch) {
      const db = useStore.getState().db[tenantId];
      const agent = db.agents.find((a: any) => a.id === chatId);
      if (agent) {
        respondCompact(tenantId, chatId, agent, (compactMatch[1] || '').trim());
        return;
      }
    }

    // Message queue (8.1, design D9): while THIS tab's run is active in an
    // agent chat, a send joins the local queue instead of racing the session
    // lock — ordered cancelable rows under the running row (QueueStack). The
    // gate is same-chat (fix-thinking-leak-on-chat-switch): a run streaming
    // in another chat holds no lock here, and a message queued behind it
    // would never drain (the run's terminal dispatch pops only its own
    // chat's queue). Cross-tab/cron conflicts on this chat's own session
    // still surface as 409s on a normal send and keep the existing
    // catch-up-and-redispatch machinery. /compact turns never queue (a
    // compact turn cannot run behind the active run — its own conflict path
    // surfaces as a toast).
    const uiState = useStore.getState().ui;
    if (target === 'agent' && uiState.running && uiState.runningChatId === chatId) {
      const db = useStore.getState().db[tenantId];
      const agent = db.agents.find((a: any) => a.id === chatId);
      if (agent) {
        useStore.getState().enqueueChatMessage(tenantId, chatId, text, attachments);
        return;
      }
    }

    useStore.getState().pushMsg(tenantId, chatId, {
      id: uid('m'), author: 'you', ts: nowTime(), at: nowIso(), text,
      ...(attachments ? { attachments } : {}),
    });

    const db = useStore.getState().db[tenantId];
    const agent = db.agents.find((a: any) => a.id === chatId);
    const channel = db.channels.find((c: any) => c.id === chatId);
    const chatAgent = agent || (channel ? db.agents.find((a: any) => a.id === channel.agentId) : null);

    if (target === 'agent' && !chatAgent) return;

    if (target === 'channel' && channel) {
      const ids = channel.members && channel.members.length ? channel.members : (channel.agentId ? [channel.agentId] : []);
      const channelMembers = ids.map((id: string) => {
        const a = db.agents.find((x: any) => x.id === id);
        if (a) return { id, kind: 'agent', name: a.name, agent: a };
        return null;
      }).filter(Boolean);

      const mentionedAgents = parseMentions(text, channelMembers)
        .filter((m: any) => m.kind === 'agent')
        .map((m: any) => db.agents.find((a: any) => a.id === m.id))
        .filter(Boolean) as Agent[];

      if (mentionedAgents.length > 0) {
        mentionedAgents.forEach((ag, i) => respondFor(tenantId, chatId, ag, text, { delay: 850 + i * 1200 + Math.random() * 400, mentioned: true }));
        return;
      }
    }

    // Channel mention fan-out stays text-only: the composer's attach button is
    // agent-chat-only (ChatRoute allowAttachments), so attachment chips never
    // reach here — document mention chips can (both surfaces pass a lens) and
    // ride the turn as pointer identity.
    if (chatAgent) respondFor(tenantId, chatId, chatAgent, text, { attachments: turnChips });
  }, [tenantId, chatId, respondFor, respondCompact]);

  const onCancel = useCallback(async () => {
    // Detach any followed catch-up stream FIRST (fix-chat-stop-on-reattached-run
    // D2): its per-event patchUi({running:true}) would re-assert the spinner
    // right after the stop cleared it. No-op for fresh turns — no live
    // catch-up stream is attached for the chat then.
    abortCatchUpStream(chatId);
    patchUi({ running: false, compacting: false });
    for (const k in activeTimers) {
      if (k.startsWith('respond-') || k.startsWith('refresh-') || k.startsWith('stream-')) {
        clearTimeout(activeTimers[k]);
        delete activeTimers[k];
      }
    }
    // Live cancel (design D5): the in-flight minted response id has the shape
    // `resp_<session>_<turn>`; decode it to cancel the server-side run.
    // Partial text and completed tool cards stay in the transcript; a cancel
    // before anything streamed retracts the empty optimistic row.
    // Stop BEFORE the first stream event: responseId is still unset, but the
    // run is already live and cancellable — the endpoint is session-scoped,
    // so address it by the bound session with a placeholder turn segment.
    // Skipping the server cancel here left the run streaming to completion:
    // the open SSE kept mutating the transcript after "stop", and a reload
    // re-attached to the live run (spinner came back).
    const rid = inFlight.responseId;
    const agentSlug = inFlight.agentSlug;
    const abort = inFlight.abort;
    const flush = inFlight.flush;
    const turnCid = inFlight.cid;
    const sessionId = sessionIdFromResponseId(rid) || inFlight.sessionId || activeBoundSessionId(tenantId, inFlight.cid || '');
    abort?.();
    // Land any coalesced deltas before the retract check: a stop after text
    // streamed must keep that text, not read an empty row.
    flush?.();
    if (agentSlug && sessionId) {
      const turn = rid ? rid.slice(('resp_' + sessionId + '_').length) : 'pending';
      void api.agents.cancelRun(tenantId, agentSlug, sessionId, turn).catch(() => {
        // The local stop already succeeded; a failed server cancel just
        // means the stream ends on its own.
      });
    } else if (!agentSlug) {
      // Followed run (fix-chat-stop-on-reattached-run): the client did not
      // start this turn in this view, so no turn identity is held. Address
      // the session-scoped endpoint by the chat's bound session ('pending'
      // placeholder turn) and the chat's own agent — the same lookup the
      // send path uses. The catch tolerates "nothing live".
      const db = useStore.getState().db[tenantId];
      const channel = db.channels.find((c: any) => c.id === chatId);
      const chatAgent = db.agents.find((a: any) => a.id === chatId)
        || (channel ? db.agents.find((a: any) => a.id === channel.agentId) : undefined);
      const boundSession = activeBoundSessionId(tenantId, chatId);
      if (chatAgent && boundSession) {
        void api.agents.cancelRun(tenantId, (chatAgent as any).slug || chatAgent.id, boundSession, 'pending').catch(() => {
          // The local stop already succeeded; a failed server cancel just
          // means the stream ends on its own.
        });
      }
    }
    retractIfEmpty();
    clearInFlight();
    // A stopped run is a finished run (message queue 8.3): the first queued
    // message dispatches instead of hanging in the stack forever.
    if (turnCid) dispatchQueuedHead(tenantId, turnCid);
  }, [patchUi, tenantId, chatId]);

  const onEdit = useCallback(async (msg: AppendMessage) => {
    const db = useStore.getState().db[tenantId];
    if (db.channels.find((c: any) => c.id === chatId)) return; // DM-only gating

    const text = msg.content.map((c: any) => c.text).join('') || '';
    const mid = (msg as any).sourceId || msg.parentId;

    useStore.getState().updateTenant(tenantId, (t) => {
      const th = t.threads[chatId];
      const s = th && th.list.find((x: any) => x.id === th.active);
      if (s) {
        const idx = s.messages.findIndex((x: any) => x.id === mid);
        if (idx !== -1) {
          s.messages = s.messages.slice(0, idx + 1);
          s.messages[idx].text = text;
        }
      }
      return t;
    });

    const agent = db.agents.find((a: any) => a.id === chatId);
    if (agent) respondFor(tenantId, chatId, agent, text);
  }, [tenantId, chatId, respondFor]);

  const onReload = useCallback(async (parentId: string | null) => {
    const db = useStore.getState().db[tenantId];
    if (db.channels.find((c: any) => c.id === chatId)) return; // DM-only gating

    const agent = db.agents.find((a: any) => a.id === chatId);
    if (!agent) return;

    const th = db.threads[chatId];
    const s = th && th.list.find((x: any) => x.id === th.active);
    const m = s && s.messages.find((x: any) => x.id === parentId);
    if (!m || m.author !== 'agent') return;

    // The regenerate prompt is the original user text: the nearest preceding
    // user message (regenerate is a REAL live turn — no canned templates).
    // Its attachment references re-send too (ids / capability URLs straight
    // from the stored entry — never re-uploads, add-chat-attachments D12).
    const idx = s.messages.findIndex((x: any) => x.id === parentId);
    let origText = '';
    let origAttachments: ChatAttachment[] | undefined;
    for (let i = idx - 1; i >= 0; i--) {
      if (s.messages[i].author === 'you') {
        origText = s.messages[i].text || '';
        // data/types Message doesn't declare attachments yet (global
        // ChatMessage does) — same cast idiom as the branches read below.
        origAttachments = (s.messages[i] as any).attachments;
        break;
      }
    }
    // Attachment-only turns regenerate like any other (empty text is valid).
    if (!origText && !(origAttachments && origAttachments.length)) return;

    const apiKey = getWorkspaceKey(tenantId);
    if (!apiKey) {
      markLiveChatDisconnected(tenantId);
      return;
    }

    patchUi({ running: true, compacting: false, runningChatId: chatId });
    // Seed the branch list with the current variant (full body — text, cards,
    // reasoning, ordered parts), then append a live branch that streams in
    // place (`n / total` comes from branches.length). The new variant gets a
    // FRESH body: sharing the old tools/parts arrays would leak new cards
    // into the previous variant.
    useStore.getState().updateTenant(tenantId, (t) => {
      const _th = t.threads[chatId];
      const _sess = _th && _th.list.find((x: any) => x.id === _th.active);
      const _mm = _sess && _sess.messages.find((x: any) => x.id === parentId);
      if (_mm) {
        const existingBranches = (_mm as any).branches || [
          { text: _mm.text, tools: _mm.tools, reasoning: _mm.reasoning, parts: _mm.parts },
        ];
        (_mm as any).branches = [...existingBranches, { text: '' }];
        (_mm as any).branch = (_mm as any).branches.length - 1;
        _mm.text = '';
        _mm.tools = [];
        _mm.parts = [];
        _mm.reasoning = '';
      }
      return t;
    });

    // The variant chains like any turn (append-only per design Risks).
    const binding = resolveBinding(tenantId, chatId);
    const patchTarget = (fn: (mm: any) => void) => {
      useStore.getState().updateTenant(tenantId, (t: any) => {
        const _th = t.threads[chatId];
        const _sess = _th && _th.list.find((x: any) => x.id === _th.active);
        const _mm = _sess && _sess.messages.find((x: any) => x.id === parentId);
        if (_mm) fn(_mm);
        return t;
      });
    };

    // Same cancel-addressing contract as startTurn: identity set at turn
    // start (not the first stream event), stream abortable by the stop
    // control. Deltas coalesce per macrotask, same as the respondFor path.
    let pendingText = '';
    let pendingReasoning: string[] = [];
    let flushQueued = false;
    let flushTimer: ReturnType<typeof setTimeout> | null = null;
    const flushDeltas = () => {
      if (flushQueued) { clearTimeout(flushTimer); flushQueued = false; }
      const text = pendingText; pendingText = '';
      const reasoning = pendingReasoning; pendingReasoning = [];
      if (!text && reasoning.length === 0) return;
      patchTarget((mm) => {
        if (text) {
          mm.text = (mm.text || '') + text;
          const branch = mm.branches?.[mm.branch];
          if (branch) branch.text = mm.text;
        }
        for (const d of reasoning) {
          appendReasoningPart(mm, d);
          const branch = mm.branches?.[mm.branch];
          if (branch) { branch.parts = mm.parts; branch.reasoning = mm.reasoning; }
        }
      });
    };
    const queueFlush = () => {
      if (flushQueued) return;
      flushQueued = true;
      flushTimer = setTimeout(flushDeltas, 0);
    };
    const turnAbort = new AbortController();
    inFlight.agentSlug = (agent as any).slug || agent.id;
    inFlight.sessionId = binding.sessionId;
    inFlight.abort = () => turnAbort.abort();
    inFlight.flush = flushDeltas;
    void runTurn(apiKey, {
      agentSlug: (agent as any).slug || agent.id,
      input: origText,
      ...(origAttachments && origAttachments.length ? { attachments: origAttachments } : {}),
      ...(binding.sessionId ? { sessionId: binding.sessionId } : {}),
      ...(binding.previousResponseId ? { previousResponseId: binding.previousResponseId } : {}),
    }, {
      onResponseId: (rid) => {
        inFlight.responseId = rid;
        inFlight.agentSlug = (agent as any).slug || agent.id;
      },
      onDelta: (delta) => {
        pendingText += delta;
        queueFlush();
      },
      onReasoningDelta: (delta) => {
        // Ordered bubble, same as the respondFor path — never mixed into text.
        pendingReasoning.push(delta);
        queueFlush();
      },
      onToolCall: (name, callId, args) => {
        flushDeltas();
        patchTarget((mm) => {
          if (!mm.tools) mm.tools = [];
          // One card per call id for the life of the turn
          // (fix-duplicate-tool-call-cards D3, same contract as respondFor):
          // a repeat event for an existing id mutates that card, never
          // mints. Added fires argless (undefined must not clobber); done is
          // the authoritative update even when its arguments string is
          // empty.
          const existing = mm.tools.find((t: any) => t.callId === callId);
          if (existing) {
            if (args !== undefined) existing.args = args;
            return;
          }
          mm.tools.push({ callId, name, args: args || '', ms: 0 });
          appendToolPart(mm, mm.tools.length - 1);
          const branch = mm.branches?.[mm.branch];
          if (branch) { branch.tools = mm.tools; branch.parts = mm.parts; }
        });
      },
      onToolOutput: (callId, _name, result, latencyMs, isError) => {
        flushDeltas();
        patchTarget((mm) => {
          const tools = mm.tools || [];
          const card = [...tools].reverse().find((t: any) => t.callId === callId) || tools[tools.length - 1];
          if (!card) return;
          card.res = result;
          card.ms = latencyMs ?? 0;
          if (isError) card.error = result;
        });
      },
      onApprovalRequired: (a) => {
        flushDeltas();
        patchTarget((mm) => {
          if (!mm.tools) mm.tools = [];
          // tool rides only on service-run write escalations
          // (add-integration-authority): the transcript card branches on it.
          mm.tools.push({ args: '', ms: 0, approval: { interruptId: a.interrupt_id, command: a.command, resolved: false, ...(a.tool ? { tool: a.tool } : {}) } });
          appendToolPart(mm, mm.tools.length - 1);
          const branch = mm.branches?.[mm.branch];
          if (branch) { branch.tools = mm.tools; branch.parts = mm.parts; }
        });
        clearInFlight();
        useStore.getState().patchUi({ running: false });
      },
      onUsage: (u) => {
        // Same terminal-event meter capture as the respondFor path (design D5).
        recordTurnUsage(tenantId, chatId, u);
      },
      onDone: (responseId) => {
        flushDeltas();
        clearInFlight();
        if (responseId) {
          // The variant is a real turn of this session: record its response id
          // on the message (and its branch) so later turns chain from it.
          useStore.getState().recordResponse(chatId, parentId || '', responseId);
          patchTarget((mm) => {
            const branch = mm.branches?.[mm.branch];
            if (branch) branch.resp = responseId;
          });
        }
        useStore.getState().patchUi({ running: false });
        // A regenerate is an active run too (message queue 8.3): sends that
        // queued behind it dispatch from its terminal state.
        dispatchQueuedHead(tenantId, chatId);
      },
      onError: (message, meta) => {
        flushDeltas();
        clearInFlight();
        if (meta?.unauthorized) {
          // D4: stale key — re-exchange once; the retried variant is the
          // user's explicit next action (Retry reconnects, then reload).
          void handleV1AuthFailure(tenantId).finally(() => {
            useStore.getState().patchUi({ running: false });
          });
          return;
        }
        // Nothing streamed into the new variant: drop it and restore the
        // previous one — an empty branch would render as a forever-loading
        // placeholder next to the error toast.
        patchTarget((mm) => {
          if (!mm.text && (!mm.tools || mm.tools.length === 0) && Array.isArray(mm.branches) && mm.branches.length > 1) {
            const existing = mm.branches.slice(0, -1);
            mm.branches = existing;
            mm.branch = existing.length - 1;
            mm.text = existing[mm.branch]?.text || '';
          }
        });
        // In-thread error entry (design D8) — auth failures never append.
        useStore.getState().pushMsg(tenantId, chatId, {
          id: uid('m'), author: 'error', ts: nowTime(), at: nowIso(), text: '', error: message,
        });
        useStore.getState().toast(message, 'error');
        useStore.getState().patchUi({ running: false });
        // Terminal failure ends the run (message queue 8.3): the first queued
        // message proceeds through the normal send path.
        dispatchQueuedHead(tenantId, chatId);
      },
    }, { signal: turnAbort.signal });
  }, [tenantId, chatId, patchUi]);

  const runtime = useExternalStoreRuntime({
    messages,
    convertMessage,
    isRunning: running,
    onNew,
    onCancel,
    onEdit,
    onReload: onReload as any
  });

  const branchNav = useCallback((mid: string, dir: number) => {
    const db = useStore.getState().db[tenantId];
    if (db.channels.find((c: any) => c.id === chatId)) return; // DM-only gating
    useStore.getState().updateTenant(tenantId, (t) => {
      const th = t.threads[chatId];
      const s = th && th.list.find((x: any) => x.id === th.active);
      const m = s && s.messages.find((x: any) => x.id === mid);
      if (m && (m as any).branches) {
        const blen = (m as any).branches.length;
        let nb = ((m as any).branch || 0) + dir;
        if (nb < 0) nb = 0;
        if (nb >= blen) nb = blen - 1;
        (m as any).branch = nb;
        (m as any).text = (m as any).branches[nb].text;
      }
      return t;
    });
  }, [tenantId, chatId]);

  return { runtime, onNew, onCancel, onEdit, onReload, branchNav, convertMessage };
}
