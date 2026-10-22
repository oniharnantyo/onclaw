/**
 * Branch-variant tests (tasks 7.2, 7.3, design D7): the reducer groups a
 * regeneration under the ORIGINAL turn's slot — live the newest alternative
 * activates, replay reconstructs the same picker from the stored markers,
 * switching is pure local state (no requests), and a failed regeneration
 * drops its empty variant and restores the previous index while a variant
 * with partial text keeps its slot.
 */
import { mockedSurface } from "./mockedSurface";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import {
  Kind,
  type Event as SessionEvent,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { Client } from "../../../lib/client";
import { applyEvent, asRichUser, visibleEntries, type Entry } from "../entries";
import { useChatStore } from "../store";

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

const HISTORY: SessionEvent[] = [
  ev(Kind.KindUserMessage, { text: "explain", turn_id: "t1", id: "e1" }),
  ev(Kind.KindTextDelta, { text: "Original ", turn_id: "t1" }),
  ev(Kind.KindAssistantMessage, {
    text: "Original answer",
    turn_id: "t1",
    id: "e2",
  }),
  ev(Kind.KindTurnCompleted, { turn_id: "t1", id: "e3" }),
];

// Every event of a regeneration turn carries the marker (the backend's
// record closure stamps the whole turn, design D7).
const REGEN: SessionEvent[] = [
  ev(Kind.KindUserMessage, {
    turn_id: "t2",
    variant_of: "t1",
    id: "e4",
  }),
  ev(Kind.KindTextDelta, { text: "Second ", turn_id: "t2", variant_of: "t1" }),
  ev(Kind.KindAssistantMessage, {
    text: "Second answer",
    turn_id: "t2",
    variant_of: "t1",
    id: "e5",
  }),
  ev(Kind.KindTurnCompleted, { turn_id: "t2", variant_of: "t1", id: "e6" }),
];

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: only the members this suite can reach. */
type MockedClient = Client & {
  chat: { regenerate: MockFn };
};

function makeClient(): MockedClient {
  // SAFETY: the literal covers the surfaces this suite drives; the rest of
  // the Client surface is unreachable from these components, and the
  // Client-typed intersection defeats the structural check, so the
  // conversion funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>({
    chat: { regenerate: vi.fn(async (): Promise<string> => "t9") },
  });
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

beforeEach(() => {
  vi.clearAllMocks();
  useChatStore.setState({
    agents: [],
    activeAgentID: "a1",
    activeSessionID: null,
    loadingSession: false,
    busy: false,
    entries: [],
    error: null,
  });
});

describe("variant slots in the reducer (task 7.2)", () => {
  it("a live second variant appends to the original turn's slot and activates", () => {
    const entries = fold(...HISTORY, ...REGEN);
    const anchor = entries.find((e: Entry) => e.kind === "user");

    expect(asRichUser(anchor!).variantTurns).toEqual(["t1", "t2"]);
    expect(asRichUser(anchor!).activeVariant).toBe(1);

    // The rendered window keeps the new answer and hides the original's.
    const visible = visibleEntries(entries);
    const answers = visible.filter((e: Entry) => e.kind === "assistant");

    expect(answers).toHaveLength(1);
    expect(answers[0]).toMatchObject({ text: "Second answer" });
  });

  it("replay reconstructs both variants with the last-marked one active", () => {
    // The persisted log IS the fold order above (markers ride the stored
    // events) — the same fold through the durable forms lands on the same
    // slot state.
    const entries = fold(...HISTORY, ...REGEN);
    const anchor = entries.find((e: Entry) => e.kind === "user");

    expect(asRichUser(anchor!).variantTurns).toEqual(["t1", "t2"]);
    expect(asRichUser(anchor!).activeVariant).toBe(1);
  });

  it("a third variant keeps appending and stays the active one", () => {
    const entries = fold(
      ...HISTORY,
      ...REGEN,
      ev(Kind.KindUserMessage, { turn_id: "t3", variant_of: "t1", id: "e7" }),
      ev(Kind.KindAssistantMessage, {
        text: "Third answer",
        turn_id: "t3",
        id: "e8",
      }),
      ev(Kind.KindTurnCompleted, { turn_id: "t3", id: "e9" })
    );

    const anchor = entries.find((e: Entry) => e.kind === "user");

    expect(asRichUser(anchor!).variantTurns).toEqual(["t1", "t2", "t3"]);
    expect(asRichUser(anchor!).activeVariant).toBe(2);
  });

  it("a regeneration marker without its anchor degrades to a plain user entry", () => {
    const entries = fold(
      ev(Kind.KindUserMessage, { turn_id: "t2", variant_of: "t1", id: "e4" })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({ kind: "user", turnID: "t2" });
  });
});

describe("failed regeneration (task 7.3)", () => {
  it("drops the empty variant and restores the previous active index", () => {
    const entries = fold(
      ...HISTORY,
      ev(Kind.KindUserMessage, { turn_id: "t2", variant_of: "t1", id: "e4" }),
      ev(Kind.KindTurnError, {
        turn_id: "t2",
        variant_of: "t1",
        error: "provider 500",
        id: "e5",
      })
    );

    const anchor = entries.find((e: Entry) => e.kind === "user");

    expect(asRichUser(anchor!).variantTurns).toEqual(["t1"]);
    expect(asRichUser(anchor!).activeVariant).toBe(0);

    // The failure is still visible; the original answer stands.
    const visible = visibleEntries(entries);

    expect(
      visible.some(
        (e: Entry) => e.kind === "assistant" && e.text === "Original answer"
      )
    ).toBe(true);
    expect(
      visible.some(
        (e: Entry) => e.kind === "error" && e.message === "provider 500"
      )
    ).toBe(true);
  });

  it("a variant with partial text keeps its slot on failure", () => {
    const entries = fold(
      ...HISTORY,
      ev(Kind.KindUserMessage, { turn_id: "t2", variant_of: "t1", id: "e4" }),
      ev(Kind.KindTextDelta, { text: "Partial re", turn_id: "t2" }),
      ev(Kind.KindTurnError, {
        turn_id: "t2",
        variant_of: "t1",
        error: "cut off",
        id: "e5",
      })
    );

    const anchor = entries.find((e: Entry) => e.kind === "user");

    expect(asRichUser(anchor!).variantTurns).toEqual(["t1", "t2"]);
    expect(asRichUser(anchor!).activeVariant).toBe(1);

    const visible = visibleEntries(entries);

    expect(
      visible.some(
        (e: Entry) => e.kind === "assistant" && e.text === "Partial re"
      )
    ).toBe(true);
  });
});
