import type { SSEInitEvent, SSEMessageEvent, SSETurnEvent } from '../../types/chat';

export interface RunChatStreamCallbacks {
  onInit: (data: SSEInitEvent) => void;
  onMessage: (data: SSEMessageEvent) => void;
  onTurn: (data: SSETurnEvent) => void;
  onStreamError: (error: string) => void;
  onDone: () => void;
  onStopped: () => void;
  onConnectionError: (error: string) => void;
}

// Pure SSE stream reader, decoupled from React so the abort/error branching is
// unit-testable without a DOM. The caller owns the AbortController and passes
// its signal; on abort the reader rejects with AbortError and we surface
// onStopped (no toast, no re-fetch). Any other failure surfaces onConnectionError.
export async function runChatStream(
  body: string,
  signal: AbortSignal,
  cb: RunChatStreamCallbacks,
  fetchFn: typeof fetch = fetch,
): Promise<void> {
  try {
    const res = await fetchFn('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body,
      signal,
    });

    if (!res.ok) {
      const errData = await res.json();
      cb.onConnectionError(errData.error || 'Chat stream failed to start');
      return;
    }

    const reader = res.body?.getReader();
    if (!reader) {
      cb.onConnectionError('ReadableStream not supported');
      return;
    }

    const decoder = new TextDecoder('utf-8');
    let buffer = '';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const blocks = buffer.split('\n\n');
      buffer = blocks.pop() || '';

      for (const block of blocks) {
        const lines = block.split('\n');
        let event = '';
        let dataStr = '';
        for (const line of lines) {
          if (line.startsWith('event: ')) event = line.slice(7).trim();
          if (line.startsWith('data: ')) dataStr = line.slice(6).trim();
        }

        if (dataStr) {
          try {
            const data = JSON.parse(dataStr);
            if (event === 'init') {
              cb.onInit(data as SSEInitEvent);
            } else if (event === 'message') {
              cb.onMessage(data as SSEMessageEvent);
            } else if (event === 'turn') {
              cb.onTurn(data as SSETurnEvent);
            } else if (event === 'error') {
              const errData = data as { error: string };
              cb.onStreamError(errData.error || 'Stream error occurred');
            }
          } catch {
            // skip malformed data
          }
        }
      }
    }

    cb.onDone();
  } catch (err) {
    if ((err as { name?: string })?.name === 'AbortError') {
      cb.onStopped();
    } else {
      cb.onConnectionError('Stream interrupted due to connection error');
    }
  }
}
