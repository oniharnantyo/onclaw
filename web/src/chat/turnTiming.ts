/** Message-timing registry (assistant-ui "message timing" element adoption,
 * adopt-assistant-ui-elements 9.1, design D11): client-measured first-token
 * latency, total turn time, and streamed speed for COMPLETED live turns,
 * keyed by the assistant message the turn streamed into.
 *
 * Deliberately runtime-local module state (same idiom as the runtime's
 * `inFlight`) — never written to the store, so it cannot ride the persisted
 * thread cache and a reloaded (hydrated) thread carries no timing to render.
 * Present-only: entries appear only when a turn that streamed at least one
 * token completes; the UI renders the line only on hover of the message
 * actions. Speed is a text-length estimate (~4 chars/token) over the
 * streaming window — omitted for short streams where it is pure noise. */
export interface TurnTiming {
  firstMs: number;
  totalMs: number;
  tps?: number;
}

const turnTimings = new Map<string, TurnTiming>();

/** One entry per live assistant turn, tiny — but a session that runs for
 * days should not grow the map without bound. */
const MAX_TIMINGS = 500;

export const recordTurnTiming = (mid: string, timing: TurnTiming): void => {
  if (turnTimings.size >= MAX_TIMINGS) {
    const oldest = turnTimings.keys().next().value;
    if (oldest !== undefined) turnTimings.delete(oldest);
  }
  turnTimings.set(mid, timing);
};

export const getTurnTiming = (mid: string | undefined): TurnTiming | undefined =>
  mid ? turnTimings.get(mid) : undefined;
