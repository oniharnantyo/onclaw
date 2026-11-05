/**
 * Delta coalescing tests (task 5.1, design D3): N per-token deltas within
 * one animation frame land in exactly ONE store commit, a turn terminal
 * flushes the pending batch synchronously (before its own fold, same tick —
 * ordering preserved), a session switch drops the batch instead of folding
 * it into the new transcript, and the page-load `eventBuffer` replay seam
 * stays on its own path (its suites run untouched).
 *
 * The frame is driven by an explicit rAF stub rather than real timers: the
 * store schedules with the global `requestAnimationFrame`, so the stub both
 * proves nothing commits before the frame and lets the flush run on demand.
 */
import {
  beforeEach,
  afterEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";

import {
  Kind,
  type Event as SessionEvent,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { Session } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";
import { useChatStore } from "../store";
import type { Entry } from "../types";

function ev(
  kind: SessionEvent["kind"],
  extra: Partial<SessionEvent> = {}
): SessionEvent {
  return { kind, occurred_at: "2026-01-01T00:00:00Z", ...extra };
}

const sessions: Session[] = [
  {
    ID: "s1",
    AgentID: "a1",
    Title: "",
    Model: "",
    Effort: "",
    CreatedAt: "2026-01-01T00:00:00Z",
    LastActivityAt: "2026-01-01T00:00:00Z",
  },
  {
    ID: "s2",
    AgentID: "a1",
    Title: "",
    Model: "",
    Effort: "",
    CreatedAt: "2026-01-01T00:00:00Z",
    LastActivityAt: "2026-01-01T00:00:00Z",
  },
];

/** The live handler the client mock captured from selectSession's
 * subscribe; every event this suite emits rides it. */
let liveHandler: ((e: SessionEvent) => void) | null = null;

type MockFn = ReturnType<typeof vi.fn>;

type MockedClient = Client & {
  agents: { listWorkspaceSkills: MockFn };
  chat: {
    subscribe: MockFn;
    transcriptPage: MockFn;
    turnState: MockFn;
    contextUsage: MockFn;
  };
};

function makeClient(): MockedClient {
  const c = {
    agents: {
      listWorkspaceSkills: vi.fn(async () => []),
    },
    chat: {
      subscribe: vi.fn(
        async (
          _id: string,
          handler: (e: SessionEvent) => void
        ): Promise<() => void> => {
          liveHandler = handler;

          return (): void => {
            if (liveHandler === handler) liveHandler = null;
          };
        }
      ),
      transcriptPage: vi.fn(async (): Promise<SessionEvent[]> => []),
      turnState: vi.fn(async () => ({ busy: false, turn_id: "" })),
      contextUsage: vi.fn(async () => ({ used: 0, limit: 200000 })),
    },
  };

  // SAFETY: the literal mocks only the surface this suite drives; the
  // Client-typed intersection defeats the structural check, so the
  // conversion funnels through the shared mockedSurface bridge.
  return c as unknown as MockedClient;
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

// ---------------------------------------------------------------------------
// The frame stub: a manual requestAnimationFrame queue.
// ---------------------------------------------------------------------------

let rafQueue: Map<number, FrameRequestCallback>;

let rafNextID: number;

function installRafStub(): void {
  rafQueue = new Map();
  rafNextID = 0;

  vi.stubGlobal(
    "requestAnimationFrame",
    (callback: FrameRequestCallback): number => {
      rafNextID += 1;
      rafQueue.set(rafNextID, callback);

      return rafNextID;
    }
  );
  vi.stubGlobal("cancelAnimationFrame", (id: number): void => {
    rafQueue.delete(id);
  });
}

/** Runs every callback scheduled on the stubbed frame — the frame landing. */
function runFrame(): void {
  const callbacks = [...rafQueue.values()];

  rafQueue.clear();

  for (const callback of callbacks) callback(0);
}

function emit(...events: SessionEvent[]): void {
  for (const e of events) liveHandler?.(e);
}

function resetStore(): void {
  useChatStore.setState({
    agents: [],
    providers: [],
    skills: [],
    activeAgentID: "a1",
    sessions,
    activeSessionID: null,
    models: [],
    entries: [],
    usage: null,
    loadingSession: false,
    error: null,
    busy: false,
    dispatching: false,
    activeTurnID: null,
    finishedTurnIDs: new Set<string>(),
    queue: [],
    panelTabs: [],
    activePanelTabID: null,
    // Seeded as running so the first delta's rail flip (setSessionRunning)
    // cannot add a subscriber notification the commit counts would see.
    runningSessions: new Set<string>(["s1"]),
    lastTurnOutcome: null,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  liveHandler = null;
  installRafStub();
  resetStore();
});

afterEach(() => {
  // Drain anything a test left pending so no cross-test flush survives the
  // stub's removal, then give the globals back to jsdom.
  runFrame();
  vi.unstubAllGlobals();
});

describe("delta coalescing at ingest (task 5.1, design D3)", () => {
  it("N deltas within one frame produce exactly one subscriber notification", async () => {
    await useChatStore.getState().selectSession("s1");
    expect(liveHandler).not.toBeNull();

    const spy = vi.fn();
    const unsubscribe = useChatStore.subscribe(spy);

    for (let i = 0; i < 8; i += 1) {
      emit(ev(Kind.KindTextDelta, { text: `${i}`, turn_id: "t1" }));
    }

    // Buffered: no commit, no notification, getState still last-flushed.
    expect(spy).not.toHaveBeenCalled();
    expect(useChatStore.getState().entries).toHaveLength(0);

    runFrame();

    expect(spy).toHaveBeenCalledTimes(1);

    const entries = useChatStore.getState().entries;

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      turnID: "t1",
      text: "01234567",
      streaming: true,
    });

    unsubscribe();
  });

  it("turn_completed flushes the pending batch synchronously, ordered, without a frame", async () => {
    await useChatStore.getState().selectSession("s1");

    emit(
      ev(Kind.KindTextDelta, { text: "Hel", turn_id: "t1" }),
      ev(Kind.KindTextDelta, { text: "lo", turn_id: "t1" })
    );
    emit(ev(Kind.KindTurnCompleted, { turn_id: "t1", id: "e-tc" }));

    // No frame ran: the terminal's same-tick flush already applied the
    // deltas that preceded it, then finalized them.
    const entries = useChatStore.getState().entries;

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      text: "Hello",
      streaming: false,
    });
    expect(useChatStore.getState().lastTurnOutcome).toMatchObject({
      turnID: "t1",
      kind: Kind.KindTurnCompleted,
    });
    expect(useChatStore.getState().activeTurnID).toBeNull();

    // The scheduled frame is now a no-op — nothing applies twice.
    const settled = useChatStore.getState().entries;

    runFrame();
    expect(useChatStore.getState().entries).toBe(settled);
  });

  it("a non-delta event drains the batch first, so arrival order survives", async () => {
    await useChatStore.getState().selectSession("s1");

    emit(
      ev(Kind.KindTextDelta, { text: "Hel", turn_id: "t1" }),
      ev(Kind.KindToolActivity, {
        turn_id: "t1",
        id: "e-tool",
        tool: { call_id: "c1", name: "read_file", arguments: "{}" },
      }),
      ev(Kind.KindTextDelta, { text: "lo", turn_id: "t1" })
    );
    runFrame();

    const kinds = useChatStore
      .getState()
      .entries.map((entry: Entry): string => entry.kind);

    // The tool card sits between the two text runs exactly as it would
    // without coalescing: the buffered first delta flushed before the
    // structural event, the trailing delta opened a fresh entry after it.
    expect(kinds).toEqual(["assistant", "tool", "assistant"]);
    expect(
      useChatStore
        .getState()
        .entries.map((entry: Entry): unknown =>
          "text" in entry ? entry.text : undefined
        )
    ).toEqual(["Hel", undefined, "lo"]);
  });

  it("a session switch drops the pending batch instead of folding it into the new transcript", async () => {
    await useChatStore.getState().selectSession("s1");

    emit(ev(Kind.KindTextDelta, { text: "stale", turn_id: "t1" }));

    await useChatStore.getState().selectSession("s2");
    runFrame();

    expect(useChatStore.getState().activeSessionID).toBe("s2");
    expect(useChatStore.getState().entries).toHaveLength(0);
  });

  it("deltas of a superseded event id are still deduped before batching", async () => {
    await useChatStore.getState().selectSession("s1");

    emit(
      ev(Kind.KindTextDelta, { id: "e-d1", text: "once", turn_id: "t1" }),
      ev(Kind.KindTextDelta, { id: "e-d1", text: "again", turn_id: "t1" })
    );
    runFrame();

    const entries = useChatStore.getState().entries;

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({ kind: "assistant", text: "once" });
  });
});
