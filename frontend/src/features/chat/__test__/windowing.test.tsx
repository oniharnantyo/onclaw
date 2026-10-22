/**
 * Transcript windowing tests (task 6.1, design D6): selectSession loads the
 * newest page through transcriptPage; "load earlier" folds the older page
 * through the SAME applyEvent into a fresh segment and prepends it — the
 * store's event-id dedupe keeps the fold idempotent against events the live
 * stream already applied; a page boundary falling inside a turn merges into
 * one entry (the boundary's leftover streaming entry is dropped); and the
 * height-delta anchor keeps the reader's place when content lands above
 * while live appends never touch the scroll.
 */
import { mockedSurface } from "./mockedSurface";
import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import {
  Kind,
  type Event as SessionEvent,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { Session } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";
import { applyEvent, mergePrepended, type Entry } from "../entries";
import { TRANSCRIPT_PAGE_SIZE, useChatStore } from "../store";

function ev(
  kind: SessionEvent["kind"],
  extra: Partial<SessionEvent> = {}
): SessionEvent {
  return { kind, occurred_at: "2026-01-01T00:00:00Z", ...extra };
}

function fold(...events: SessionEvent[]): Entry[] {
  return events.reduce<Entry[]>(
    (entries: Entry[], e: SessionEvent) => applyEvent(entries, e),
    []
  );
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
];

/** A 60-event page (the page size): persisted user/assistant pairs. */
function page(fromSeq: number, count: number): SessionEvent[] {
  const events: SessionEvent[] = [];

  for (let i = 0; i < count; i += 1) {
    const seq = fromSeq + i;

    events.push(
      ev(Kind.KindUserMessage, {
        text: `msg ${seq}`,
        turn_id: `t${seq}`,
        id: `e${seq}`,
        seq,
      })
    );
  }

  return events;
}

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the members the store drives here. */
type MockedClient = Client & {
  chat: {
    subscribe: MockFn;
    transcriptPage: MockFn;
    contextUsage: MockFn;
  };
};

let newestPage: SessionEvent[];

let olderPage: SessionEvent[];

function makeClient(): MockedClient {
  // SAFETY: the literal covers the surfaces this suite drives; the
  // Client-typed intersection defeats the structural check, so the
  // conversion funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>({
    chat: {
      subscribe: vi.fn(async (): Promise<() => void> => () => undefined),
      transcriptPage: vi.fn(
        async (
          _sessionID: string,
          beforeSeq: number
        ): Promise<SessionEvent[]> => (beforeSeq === 0 ? newestPage : olderPage)
      ),
      contextUsage: vi.fn(async () => ({ used: 0, limit: 1 })),
    },
  });
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedClient = client as MockedClient;

beforeEach(() => {
  vi.clearAllMocks();
  newestPage = [];
  olderPage = [];
  useChatStore.setState({
    agents: [],
    activeAgentID: "a1",
    sessions,
    activeSessionID: null,
    entries: [],
    loadingSession: false,
    loadingEarlier: false,
    oldestSeq: null,
    busy: false,
    error: null,
  });
});

describe("mergePrepended (task 6.1)", () => {
  it("prepends the older segment in order", () => {
    const older = fold(
      ev(Kind.KindUserMessage, { text: "old", turn_id: "t0" })
    );

    const newer = fold(
      ev(Kind.KindUserMessage, { text: "new", turn_id: "t1" })
    );

    const merged = mergePrepended(older, newer);

    // SAFETY: only user entries are folded here; UserEntry always carries
    // text.
    expect(merged.map((e: Entry) => (e as { text: string }).text)).toEqual([
      "old",
      "new",
    ]);
  });

  it("a boundary inside a turn renders it as one unbroken entry", () => {
    // The older page ends with deltas of turn t1; the newer page holds its
    // durable assistant message. The boundary's leftover streaming entry is
    // dropped; the full durable text stands.
    const older = fold(
      ev(Kind.KindUserMessage, { text: "q", turn_id: "t1" }),
      ev(Kind.KindTextDelta, { text: "partial ans", turn_id: "t1" })
    );

    const newer = fold(
      ev(Kind.KindAssistantMessage, {
        text: "partial answer (full)",
        turn_id: "t1",
        id: "e2",
      }),
      ev(Kind.KindTurnCompleted, { turn_id: "t1", id: "e3" })
    );

    const merged = mergePrepended(older, newer);

    expect(merged.filter((e: Entry) => e.kind === "assistant")).toHaveLength(1);
    // SAFETY: only user/assistant entries are asserted on; both carry text.
    expect(
      merged.some(
        (e: Entry) => (e as { text: string }).text === "partial answer (full)"
      )
    ).toBe(true);
    // The user prompt from the older segment survives.
    expect(merged.some((e: Entry) => e.kind === "user")).toBe(true);
  });

  it("keeps the live window's streaming entry (the turn still runs)", () => {
    const older = fold(
      ev(Kind.KindUserMessage, { text: "old", turn_id: "t0" })
    );

    const newer = fold(
      ev(Kind.KindTextDelta, { text: "streaming", turn_id: "t9" })
    );

    const merged = mergePrepended(older, newer);

    expect(
      merged.filter((e: Entry) => e.kind === "assistant" && e.streaming)
    ).toHaveLength(1);
  });
});

describe("store windowing (task 6.1)", () => {
  it("selectSession loads the newest page and pages back through loadEarlier", async () => {
    // 60 newest events (seq 41..100) — a FULL page means older history
    // exists; the older fetch returns a SHORT page (the beginning).
    newestPage = page(41, TRANSCRIPT_PAGE_SIZE);
    olderPage = page(1, 40);

    await useChatStore.getState().selectSession("s1");

    expect(mockedClient.chat.transcriptPage).toHaveBeenCalledWith(
      "s1",
      0,
      TRANSCRIPT_PAGE_SIZE
    );
    expect(useChatStore.getState().entries).toHaveLength(TRANSCRIPT_PAGE_SIZE);
    expect(useChatStore.getState().oldestSeq).toBe(41);

    await useChatStore.getState().loadEarlier();

    expect(mockedClient.chat.transcriptPage).toHaveBeenCalledWith(
      "s1",
      41,
      TRANSCRIPT_PAGE_SIZE
    );
    const entries = useChatStore.getState().entries;

    // Prepended: the oldest message now leads; nothing was lost.
    expect(entries).toHaveLength(TRANSCRIPT_PAGE_SIZE + 40);
    expect(entries[0]).toMatchObject({ text: "msg 1" });
    // A short page marks the session's beginning: the affordance hides.
    expect(useChatStore.getState().oldestSeq).toBeNull();
    expect(useChatStore.getState().loadingEarlier).toBe(false);
  });

  it("prepending is idempotent against events the live stream already applied", async () => {
    newestPage = page(41, TRANSCRIPT_PAGE_SIZE);
    // The older page re-delivers one event the store already folded live.
    olderPage = [
      ...page(1, 39),
      ev(Kind.KindUserMessage, {
        text: "msg 41",
        turn_id: "t41",
        id: "e41",
        seq: 41,
      }),
    ];

    await useChatStore.getState().selectSession("s1");

    // The live stream delivers msg 41 again (the subscribe-before-load
    // overlap) — folded once through the live handler's dedupe set.
    await act(async () => {
      // SAFETY: subscribe's handler is captured by the mocked client; the
      // index read keeps the lib target (no Array.prototype.at) happy.
      const calls = mockedClient.chat.subscribe.mock.calls;

      // SAFETY: the mocked subscribe captured exactly this handler shape.
      const handler = calls[calls.length - 1]?.[1] as (e: SessionEvent) => void;

      handler(
        ev(Kind.KindUserMessage, {
          text: "msg 41",
          turn_id: "t41",
          id: "e41",
        })
      );
    });

    await useChatStore.getState().loadEarlier();

    // SAFETY: only user entries match this prompt; UserEntry carries text.
    const copies = useChatStore
      .getState()
      .entries.filter((e: Entry) => (e as { text: string }).text === "msg 41");

    expect(copies).toHaveLength(1);
  });

  it("a failed loadEarlier surfaces the error and releases the flag", async () => {
    newestPage = page(41, TRANSCRIPT_PAGE_SIZE);
    await useChatStore.getState().selectSession("s1");

    mockedClient.chat.transcriptPage.mockRejectedValueOnce(
      new Error("store busy")
    );
    await useChatStore.getState().loadEarlier();

    expect(useChatStore.getState().error).toContain("store busy");
    expect(useChatStore.getState().loadingEarlier).toBe(false);
  });
});
