import { useCallback } from 'react';
import { useExternalStoreRuntime } from '@assistant-ui/react';
import type { ThreadMessageLike, AppendMessage } from '@assistant-ui/react';
import { useStore, useWorkspace } from '../store';
import { mintSessionId } from '../store';
import { getWorkspaceKey } from '../store/workspaceKeys';
import type { Message, Agent } from '../data/types';
import { uid, nowTime, parseMentions, appendReasoningPart, appendToolPart } from '../lib/helpers';
import { runTurn } from '../lib/openresponses';
import { markLiveChatDisconnected, handleV1AuthFailure } from '../lib/livechat';
import { api } from '../lib/api';

const activeTimers: Record<string, any> = {};

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

/** Identity of the in-flight live turn, captured at the FIRST stream event so
 * the stop control can cancel the server-side run mid-stream (design D5).
 * `mid` is the optimistic agent message the turn streams into — a turn that
 * fails before producing anything gets that empty row retracted. */
const inFlight: { responseId?: string; agentSlug?: string; mid?: string; tid?: string; cid?: string } = {};
const clearInFlight = () => { inFlight.responseId = undefined; inFlight.agentSlug = undefined; inFlight.mid = undefined; inFlight.tid = undefined; inFlight.cid = undefined; };

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

  const running = useStore(s => !!s.ui.running);

  const convertMessage = useCallback((msg: Message): ThreadMessageLike => {
    // Note: convertMessage drops branch-variant resolution it previously had.
    // This is harmless today as branches map back to m.text, but worth documenting this coupling.
    const content: any[] = [{ type: 'text', text: msg.text || '' }];

    if (msg.tools && msg.tools.length > 0) {
      msg.tools.forEach((tool: any, i: number) => {
        // Gate tool parts on the currently running message
        const isCurrentlyRunning = running && msg.id === session?.messages[session.messages.length - 1]?.id;
        if (!isCurrentlyRunning) {
           content.push({
             type: 'tool-call',
             toolName: tool.name,
             // Real call ids are unique per invocation; legacy/seeded cards
             // only carry a name, so disambiguate by index — two calls to the
             // same tool in one turn (e.g. a 404 retry) must not collide in
             // assistant-ui's useResources.
             toolCallId: tool.callId ? `call_${tool.callId}` : `call_${msg.id}_${tool.name}_${i}`,
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
            cron: msg.cron,
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
    patchUi({ running: true });

    // Real turn via the OpenResponses /v1 surface when a workspace chat key
    // is held (agent = model, key = tenant). Without a key the chat shows
    // its connect/retry state — the live UI never runs canned replies.
    const apiKey = getWorkspaceKey(tid);
    if (apiKey) {
      const mid = uid('m');
      useStore.getState().pushMsg(tid, cid, {
        id: mid, author: 'agent', agentId: ag.id, ts: nowTime(), text: '',
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
      // startTurn streams into the already-pushed optimistic agent message,
      // so a post-auth-failure retry (design D4: clear slot → re-exchange
      // once → retry) reuses the same message instead of duplicating it.
      const startTurn = (key: string, attempt: number) => {
        // Restamped per attempt: the error/cancel paths clear the in-flight
        // identity, and a retried turn streams into the same message.
        inFlight.mid = mid; inFlight.tid = tid; inFlight.cid = cid;
        void runTurn(key, {
          agentSlug: (ag as any).slug || ag.id,
          input: origText,
          ...(binding.sessionId ? { sessionId: binding.sessionId } : {}),
          ...(binding.previousResponseId ? { previousResponseId: binding.previousResponseId } : {}),
        }, {
          onResponseId: (rid) => {
            inFlight.responseId = rid;
            inFlight.agentSlug = (ag as any).slug || ag.id;
          },
          onDelta: (delta) => {
            useStore.getState().updateTenant(tid, (t: any) => {
              const s = t.threads[cid]?.list.find((x: any) => x.id === t.threads[cid].active);
              const last = s?.messages[s.messages.length - 1];
              if (last && last.author === 'agent') last.text = (last.text || '') + delta;
              return t;
            });
          },
          onReasoningDelta: (delta) => {
            // Reasoning is its own ordered bubble (part) on the turn body —
            // never leaks into the visible text. Consecutive deltas extend
            // the open segment; a delta after a tool card opens a new one,
            // so round-2 reasoning sits between the cards and the text.
            patchTools((_tools, msg) => appendReasoningPart(msg, delta));
          },
          onToolCall: (name, callId, args) => patchTools((tools, msg) => {
            // The stream client fires this twice per call (added: argless card,
            // done: complete args) — update the existing card by call id.
            const existing = args ? tools.find((t: any) => t.callId === callId) : undefined;
            if (existing) { existing.args = args; return; }
            tools.push({ callId, name, args: args || '', ms: 0 });
            appendToolPart(msg, tools.length - 1);
          }),
          onToolOutput: (callId, _name, result, latencyMs, isError) => patchTools((tools) => {
            const card = [...tools].reverse().find((t: any) => t.callId === callId) || tools[tools.length - 1];
            if (!card) return;
            card.res = result;
            card.ms = latencyMs ?? 0;
            if (isError) card.error = result;
          }),
          onApprovalRequired: (a) => {
            patchTools((tools, msg) => {
              tools.push({ args: '', ms: 0, approval: { interruptId: a.interrupt_id, command: a.command, resolved: false } });
              appendToolPart(msg, tools.length - 1);
            });
            // The turn is paused server-side until the approval card is acted
            // on, so stop the composer spinner instead of spinning forever.
            clearInFlight();
            useStore.getState().patchUi({ running: false });
          },
          onUsage: (u) => {
            // Terminal-event meter capture (design D5): a usable final-call
            // input records; no usage block (or no number) clears so the
            // meter hides rather than showing a stale/zero value.
            useStore.getState().recordThreadUsage(tid, cid, u && typeof u.finalInputTokens === 'number' ? u.finalInputTokens : null);
          },
          onDone: (responseId) => {
            clearInFlight();
            // Record the minted response id on the assistant message it
            // produced — this is the `previous_response_id` chain link for the
            // next turn.
            if (responseId) useStore.getState().recordResponse(cid, mid, responseId);
            useStore.getState().patchUi({ running: false });
          },
          onError: (message, meta) => {
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
              id: uid('m'), author: 'error', ts: nowTime(), text: '', error: message,
            });
            useStore.getState().toast(message, 'error');
            useStore.getState().patchUi({ running: false });
          },
        });
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

  const onNew = useCallback(async (msg: AppendMessage) => {
    const target = useStore.getState().db[tenantId]?.agents.find((a: any) => a.id === chatId) ? 'agent' : 'channel';
    const text = msg.content.map((c: any) => c.text).join('') || '';

    useStore.getState().pushMsg(tenantId, chatId, { id: uid('m'), author: 'you', ts: nowTime(), text });

    const db = useStore.getState().db[tenantId];
    const agent = db.agents.find((a: any) => a.id === chatId);
    const channel = db.channels.find((c: any) => c.id === chatId);
    const chatAgent = agent || (channel ? db.agents.find((a: any) => a.id === channel.agentId) : null);

    if (target === 'agent' && !chatAgent) return;

    if (text.trim().toLowerCase().startsWith('/reset')) {
      useStore.getState().updateTenant(tenantId, (t) => {
        const th = t.threads[chatId];
        const s = th && th.list.find((x: any) => x.id === th.active);
        // Local clear only — no server call. Minting a fresh session id makes
        // the next live turn BIRTH a new server session (the old one is
        // abandoned server-side).
        // `sess` typing lands with the store's session-id work (task 3.1).
        if (s) { s.messages = []; s.title = 'New chat'; s.updated = nowTime(); s.sess = mintSessionId(); }
        return t;
      });
      useStore.getState().toast('Thread cleared');
      return;
    }

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

    if (chatAgent) respondFor(tenantId, chatId, chatAgent, text);
  }, [tenantId, chatId, respondFor]);

  const onCancel = useCallback(async () => {
    patchUi({ running: false });
    for (const k in activeTimers) {
      if (k.startsWith('respond-') || k.startsWith('refresh-') || k.startsWith('stream-')) {
        clearTimeout(activeTimers[k]);
        delete activeTimers[k];
      }
    }
    // Live cancel (design D5): the in-flight minted response id has the shape
    // `resp_<session>_<turn>`. Per the published codec (internal/openresponses
    // codec.go), decode = strip the `resp_` prefix, then split at the LAST
    // underscore (turn ids are UUIDs — never underscored). Partial text and
    // completed tool cards stay in the transcript; a cancel before anything
    // streamed retracts the empty optimistic row.
    const rid = inFlight.responseId;
    const agentSlug = inFlight.agentSlug;
    if (rid && agentSlug && rid.startsWith('resp_')) {
      const payload = rid.slice('resp_'.length);
      const cut = payload.lastIndexOf('_');
      if (cut > 0 && cut < payload.length - 1) {
        const sessionId = payload.slice(0, cut);
        const turn = payload.slice(cut + 1);
        retractIfEmpty();
        clearInFlight();
        void api.agents.cancelRun(tenantId, agentSlug, sessionId, turn).catch(() => {
          // The local stop already succeeded; a failed server cancel just
          // means the stream ends on its own.
        });
        return;
      }
    }
    retractIfEmpty();
    clearInFlight();
  }, [patchUi, tenantId]);

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
    const idx = s.messages.findIndex((x: any) => x.id === parentId);
    let origText = '';
    for (let i = idx - 1; i >= 0; i--) {
      if (s.messages[i].author === 'you') { origText = s.messages[i].text || ''; break; }
    }
    if (!origText) return;

    const apiKey = getWorkspaceKey(tenantId);
    if (!apiKey) {
      markLiveChatDisconnected(tenantId);
      return;
    }

    patchUi({ running: true });
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

    void runTurn(apiKey, {
      agentSlug: (agent as any).slug || agent.id,
      input: origText,
      ...(binding.sessionId ? { sessionId: binding.sessionId } : {}),
      ...(binding.previousResponseId ? { previousResponseId: binding.previousResponseId } : {}),
    }, {
      onResponseId: (rid) => {
        inFlight.responseId = rid;
        inFlight.agentSlug = (agent as any).slug || agent.id;
      },
      onDelta: (delta) => patchTarget((mm) => {
        mm.text = (mm.text || '') + delta;
        const branch = mm.branches?.[mm.branch];
        if (branch) branch.text = mm.text;
      }),
      onReasoningDelta: (delta) => patchTarget((mm) => {
        // Ordered bubble, same as the respondFor path — never mixed into text.
        // The active branch mirrors the streamed arrays by reference so the
        // variant picker renders the in-progress body.
        appendReasoningPart(mm, delta);
        const branch = mm.branches?.[mm.branch];
        if (branch) { branch.parts = mm.parts; branch.reasoning = mm.reasoning; }
      }),
      onToolCall: (name, callId, args) => patchTarget((mm) => {
        if (!mm.tools) mm.tools = [];
        // Added fires argless, done carries the complete args (see respondFor).
        const existing = args ? mm.tools.find((t: any) => t.callId === callId) : undefined;
        if (existing) { existing.args = args; return; }
        mm.tools.push({ callId, name, args: args || '', ms: 0 });
        appendToolPart(mm, mm.tools.length - 1);
        const branch = mm.branches?.[mm.branch];
        if (branch) { branch.tools = mm.tools; branch.parts = mm.parts; }
      }),
      onToolOutput: (callId, _name, result, latencyMs, isError) => patchTarget((mm) => {
        const tools = mm.tools || [];
        const card = [...tools].reverse().find((t: any) => t.callId === callId) || tools[tools.length - 1];
        if (!card) return;
        card.res = result;
        card.ms = latencyMs ?? 0;
        if (isError) card.error = result;
      }),
      onApprovalRequired: (a) => {
        patchTarget((mm) => {
          if (!mm.tools) mm.tools = [];
          mm.tools.push({ args: '', ms: 0, approval: { interruptId: a.interrupt_id, command: a.command, resolved: false } });
          appendToolPart(mm, mm.tools.length - 1);
          const branch = mm.branches?.[mm.branch];
          if (branch) { branch.tools = mm.tools; branch.parts = mm.parts; }
        });
        clearInFlight();
        useStore.getState().patchUi({ running: false });
      },
      onUsage: (u) => {
        // Same terminal-event meter capture as the respondFor path (design D5).
        useStore.getState().recordThreadUsage(tenantId, chatId, u && typeof u.finalInputTokens === 'number' ? u.finalInputTokens : null);
      },
      onDone: (responseId) => {
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
      },
      onError: (message, meta) => {
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
          id: uid('m'), author: 'error', ts: nowTime(), text: '', error: message,
        });
        useStore.getState().toast(message, 'error');
        useStore.getState().patchUi({ running: false });
      },
    });
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
