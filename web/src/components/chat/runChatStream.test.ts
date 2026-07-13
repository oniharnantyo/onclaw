import { runChatStream, type RunChatStreamCallbacks } from './runChatStream';

// Node-native SSE mocks (no DOM framework needed): Node provides fetch,
// Response, ReadableStream, TextEncoder, DOMException, and AbortController.

function makeAbortFetch(): any {
  return async (_url: string, init?: any) => {
    const signal: AbortSignal | undefined = init?.signal;
    const encoder = new TextEncoder();
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(encoder.encode('event: init\ndata: {"conversation_id":1,"context_window":100}\n\n'));
        controller.enqueue(
          encoder.encode(
            'event: message\ndata: {"content_blocks":[{"type":"assistant_gen_text","assistant_gen_text":{"text":"partial"}}]}\n\n',
          ),
        );
        const onAbort = () => controller.error(new DOMException('The operation was aborted.', 'AbortError'));
        if (signal) {
          if (signal.aborted) onAbort();
          else signal.addEventListener('abort', onAbort, { once: true });
        }
      },
    });
    return new Response(stream, { status: 200 });
  };
}

function makeSuccessFetch(): any {
  return async () => {
    const encoder = new TextEncoder();
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(encoder.encode('event: init\ndata: {"conversation_id":1}\n\n'));
        controller.enqueue(encoder.encode('event: message\ndata: {"content_blocks":[]}\n\n'));
        controller.close();
      },
    });
    return new Response(stream, { status: 200 });
  };
}

const noop = () => {};

function spy(counts: { done: number; stopped: number; connErr: number }): RunChatStreamCallbacks {
  return {
    onInit: noop,
    onMessage: noop,
    onTurn: noop,
    onStreamError: noop,
    onDone: () => {
      counts.done++;
    },
    onStopped: () => {
      counts.stopped++;
    },
    onConnectionError: () => {
      counts.connErr++;
    },
  };
}

export async function runChatStreamTests(): Promise<void> {
  // 1. Abort mid-stream → onStopped fires, NO onDone, NO onConnectionError
  {
    const counts = { done: 0, stopped: 0, connErr: 0 };
    const controller = new AbortController();
    const p = runChatStream('{}', controller.signal, spy(counts), makeAbortFetch());
    controller.abort();
    await p;
    if (counts.stopped !== 1) throw new Error(`Abort: expected onStopped=1, got ${counts.stopped}`);
    if (counts.done !== 0) throw new Error(`Abort: expected onDone=0, got ${counts.done}`);
    if (counts.connErr !== 0) throw new Error(`Abort: expected onConnectionError=0, got ${counts.connErr}`);
  }

  // 2. Clean completion → onDone fires, NO stopped/connErr
  {
    const counts = { done: 0, stopped: 0, connErr: 0 };
    await runChatStream('{}', new AbortController().signal, spy(counts), makeSuccessFetch());
    if (counts.done !== 1) throw new Error(`Success: expected onDone=1, got ${counts.done}`);
    if (counts.stopped !== 0) throw new Error(`Success: expected onStopped=0, got ${counts.stopped}`);
    if (counts.connErr !== 0) throw new Error(`Success: expected onConnectionError=0, got ${counts.connErr}`);
  }

  // 3. Non-2xx response → onConnectionError (with server error), NO done/stopped
  {
    const counts = { done: 0, stopped: 0, connErr: 0 };
    const notOk: any = async () => new Response(JSON.stringify({ error: 'boom' }), { status: 500 });
    await runChatStream('{}', new AbortController().signal, spy(counts), notOk);
    if (counts.connErr !== 1) throw new Error(`!ok: expected onConnectionError=1, got ${counts.connErr}`);
    if (counts.done !== 0 || counts.stopped !== 0) {
      throw new Error(`!ok: expected no done/stopped, got done=${counts.done} stopped=${counts.stopped}`);
    }
  }

  // 4. Network throw (non-abort) → onConnectionError, NOT onStopped
  {
    const counts = { done: 0, stopped: 0, connErr: 0 };
    const throws: any = async () => {
      throw new Error('network down');
    };
    await runChatStream('{}', new AbortController().signal, spy(counts), throws);
    if (counts.connErr !== 1) throw new Error(`Throw: expected onConnectionError=1, got ${counts.connErr}`);
    if (counts.stopped !== 0) throw new Error(`Throw: abort branch must not fire on generic network error`);
    if (counts.done !== 0) throw new Error(`Throw: expected onDone=0, got ${counts.done}`);
  }

  // 5. Re-fetch guard (Requirement 3): the abort path must NOT trigger the
  // re-fetch. In ChatProvider.runChat, onDone is the ONLY callback that calls
  // fetchMessages (setTimeout after STREAM_DONE); onStopped dispatches only
  // STREAM_STOPPED. So an abort must call onStopped and NOT onDone — otherwise
  // the in-memory partial would be wiped by the re-fetch ("vanish on stop").
  {
    const counts = { done: 0, stopped: 0, connErr: 0 };
    const controller = new AbortController();
    const p = runChatStream('{}', controller.signal, spy(counts), makeAbortFetch());
    controller.abort();
    await p;
    if (counts.stopped !== 1) throw new Error(`Re-fetch guard: expected onStopped=1, got ${counts.stopped}`);
    if (counts.done !== 0) throw new Error(`Re-fetch guard: abort must NOT call onDone (the re-fetch trigger), got ${counts.done}`);
  }
}
