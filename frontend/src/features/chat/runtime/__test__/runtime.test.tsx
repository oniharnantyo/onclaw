/**
 * Runtime adapter tests (task 2.3, design D1): the external-store adapter
 * surfaces the store's state (isRunning, hasEarlier, the mapped messages)
 * and every callback routes into the matching store action — send, stop,
 * regenerate, the local variant switch, load earlier — plus the approval
 * decision, which rides the bound resolveApproval call the way the legacy
 * ApprovalCard submits it (the store exposes no approve/deny actions).
 */
import { act, useEffect } from "react";
import type { ReactNode } from "react";
import { mockedSurface } from "../../__test__/mockedSurface";
import { renderUI } from "../../__test__/test-utils";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import {
  Kind,
  type Event as SessionEvent,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { Client } from "../../../../lib/client";
import { applyEvent, type Entry } from "../../entries";
import { useChatStore } from "../../store";
import { useChatRuntime } from "../useChatRuntime";
import { ChatRuntimeProvider } from "../ChatRuntimeProvider";
import type { AssistantRuntime, ThreadMessage } from "@assistant-ui/react";

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

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: only the members the adapter actions reach. */
type MockedClient = Client & {
  chat: {
    send: MockFn;
    cancel: MockFn;
    regenerate: MockFn;
    transcriptPage: MockFn;
    resolveApproval: MockFn;
  };
};

function makeClient(): MockedClient {
  const c = {
    chat: {
      send: vi.fn(async (): Promise<string> => "turn-x"),
      cancel: vi.fn(async (): Promise<void> => undefined),
      regenerate: vi.fn(async (): Promise<string> => "turn-y"),
      transcriptPage: vi.fn(async (): Promise<SessionEvent[]> => []),
      resolveApproval: vi.fn(async (): Promise<void> => undefined),
    },
  };

  // SAFETY: the literal covers exactly the surface this suite drives; the
  // Client-typed intersection defeats the structural check, so the
  // conversion funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>(c);
}

vi.mock("../../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedClient = client as MockedClient;

/** The captured runtime of the last mounted probe. */
interface CapturedRuntime {
  runtime: AssistantRuntime | null;
}

const captured: CapturedRuntime = { runtime: null };

function RuntimeProbe(): ReactNode {
  const runtime = useChatRuntime();

  useEffect((): void => {
    captured.runtime = runtime;
  }, [runtime]);

  return null;
}

async function renderProbe(): Promise<void> {
  await renderUI(<RuntimeProbe />);
}

function seed(entries: Entry[]): void {
  useChatStore.setState({ activeSessionID: "s1", entries });
}

beforeEach(() => {
  vi.clearAllMocks();
  captured.runtime = null;
  useChatStore.setState({
    activeSessionID: "s1",
    entries: [],
    busy: false,
    dispatching: false,
    activeTurnID: null,
    oldestSeq: null,
    loadingEarlier: false,
    queue: [],
    error: null,
  });
});

describe("chat runtime adapter (task 2.3, design D1)", () => {
  it("mounts inert through ChatRuntimeProvider", async () => {
    await renderUI(
      <ChatRuntimeProvider>
        <span data-testid="provider-child" />
      </ChatRuntimeProvider>
    );

    expect(
      document.querySelector('[data-testid="provider-child"]')
    ).not.toBeNull();
  });

  it("appends through onNew into the store's send action", async () => {
    const send = vi.fn(async (): Promise<void> => undefined);

    useChatStore.setState({ send });
    await renderProbe();

    act(() => {
      captured.runtime?.thread.append("hello");
    });

    expect(send).toHaveBeenCalledWith("hello", []);
  });

  it("passes the stored attachment keys from the append into send", async () => {
    const send = vi.fn(async (): Promise<void> => undefined);

    useChatStore.setState({ send });
    await renderProbe();

    act(() => {
      captured.runtime?.thread.append({
        role: "user",
        content: [{ type: "text", text: "with file" }],
        attachments: [
          {
            id: "cap-key-1",
            type: "image",
            name: "shot.png",
            content: [],
            status: { type: "complete" },
          },
        ],
      });
    });

    expect(send).toHaveBeenCalledWith("with file", ["cap-key-1"]);
  });

  it("cancels through onCancel into the store's stop action", async () => {
    const stop = vi.fn((): void => undefined);

    useChatStore.setState({ stop });
    await renderProbe();

    act(() => {
      captured.runtime?.thread.cancelRun();
    });

    expect(stop).toHaveBeenCalledOnce();
  });

  it("exposes the store's busy flag as isRunning", async () => {
    const { rerender } = await renderUI(<RuntimeProbe />);

    expect(captured.runtime?.thread.getState().isRunning).toBe(false);

    await act(async () => {
      useChatStore.setState({ busy: true });
    });
    await rerender(<RuntimeProbe />);

    expect(captured.runtime?.thread.getState().isRunning).toBe(true);
  });

  it("surfaces hasEarlier and loads earlier pages through the store", async () => {
    const loadEarlier = vi.fn(async (): Promise<void> => undefined);

    const { rerender } = await renderUI(<RuntimeProbe />);

    expect(captured.runtime?.thread.getState().hasEarlier).toBe(false);

    await act(async () => {
      useChatStore.setState({ oldestSeq: 7, loadEarlier });
    });
    await rerender(<RuntimeProbe />);

    expect(captured.runtime?.thread.getState().hasEarlier).toBe(true);

    await act(async () => {
      await captured.runtime?.thread.loadEarlier();
    });

    expect(loadEarlier).toHaveBeenCalledOnce();
  });

  it("regenerates the reloaded message's turn through the store", async () => {
    const regenerate = vi.fn(async (): Promise<void> => undefined);

    const entries = fold(
      ev(Kind.KindUserMessage, { text: "ask", turn_id: "t1" })
    );

    useChatStore.setState({ entries, regenerate });
    await renderProbe();

    // The reload targets the assistant level; the parent id the runtime
    // reports is the user message's entry id. startRun is fire-and-forget;
    // act's flush lets its internal chain reach onReload.
    await act(async () => {
      captured.runtime?.thread.startRun({
        parentId: entries[0].id,
      });
    });

    expect(regenerate).toHaveBeenCalledWith("t1");
  });

  it("switches a variant branch locally through setActiveVariant", async () => {
    const setActiveVariant = vi.fn((): void => undefined);

    const entries = fold(
      ev(Kind.KindUserMessage, { text: "ask", turn_id: "t1", id: "u1" }),
      ev(Kind.KindAssistantMessage, {
        text: "one",
        turn_id: "t1",
        id: "a1",
      }),
      ev(Kind.KindTurnCompleted, { turn_id: "t1" }),
      ev(Kind.KindUserMessage, { turn_id: "t2", variant_of: "t1" }),
      ev(Kind.KindAssistantMessage, {
        text: "two",
        turn_id: "t2",
        variant_of: "t1",
      }),
      ev(Kind.KindTurnCompleted, { turn_id: "t2", variant_of: "t1" })
    );

    useChatStore.setState({ entries, setActiveVariant });
    await renderProbe();

    // The repository heads the newest variant; switching to the original
    // resolves to the anchor's slot 0 through the store's local switch.
    act(() => {
      captured.runtime?.thread.getMessageById("t1").switchToBranch({
        branchId: "t1",
      });
    });

    expect(setActiveVariant).toHaveBeenCalledWith("t1", 0);
  });

  it("answers a pending approval through the bound resolveApproval call", async () => {
    const entries = fold(
      ev(Kind.KindApprovalRequired, {
        turn_id: "t1",
        approval: { interrupt_id: "int-1", command: "make deploy" },
      })
    );

    seed(entries);
    await renderProbe();

    // The turn's message id is the turn id; the part id is the entry id.
    const toolCallId = entries[0].id;

    await act(async () => {
      await captured.runtime?.thread
        .getMessageById("t1")
        .getMessagePartByToolCallId(toolCallId)
        .respondToToolApproval({ approved: true });
    });

    expect(mockedClient.chat.resolveApproval).toHaveBeenCalledWith(
      "s1",
      "int-1",
      true
    );

    await act(async () => {
      await captured.runtime?.thread
        .getMessageById("t1")
        .getMessagePartByToolCallId(toolCallId)
        .respondToToolApproval({ approved: false });
    });

    expect(mockedClient.chat.resolveApproval).toHaveBeenCalledWith(
      "s1",
      "int-1",
      false
    );
  });

  it("renders the mapped entries as the thread's messages", async () => {
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "ask", turn_id: "t1" }),
      ev(Kind.KindAssistantMessage, {
        text: "answer",
        turn_id: "t1",
      }),
      ev(Kind.KindTurnCompleted, { turn_id: "t1" })
    );

    seed(entries);
    await renderProbe();

    const state = captured.runtime?.thread.getState();

    // The user message carries the entry id; the turn's message id is the
    // turn id the branch tree resolves through.
    expect(state?.messages.map((m: ThreadMessage): string => m.id)).toEqual([
      entries[0].id,
      "t1",
    ]);
    expect(state?.messages[0]?.content).toEqual([
      expect.objectContaining({ type: "text", text: "ask" }),
    ]);
    expect(state?.messages[1]?.content).toEqual([
      expect.objectContaining({ type: "text", text: "answer" }),
    ]);
  });
});
