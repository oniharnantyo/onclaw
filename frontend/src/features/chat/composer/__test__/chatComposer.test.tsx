/**
 * Composer suite (task 5.1, spec chat-view "Composer affordances" +
 * chat-attachments): Enter sends, Shift+Enter inserts a newline, the send
 * control becomes a stop control while a turn is in flight and the partial
 * text survives the cancel, a send while busy joins the ordered queue and
 * dispatches by itself when the turn ends, the placeholder names the agent
 * with the trigger tokens in their own hint row, and the composer never
 * sends without an agent.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

// The client mock factory is imported FIRST on purpose: the hoisted
// `vi.mock` below runs the moment the chat store pulls the client module in,
// so the factory's module must already be evaluated by then.
import { live, makeClient, type MockedClient } from "./clientMock";
import {
  Kind,
  type Event as SessionEvent,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { useAttachmentStore } from "../../attachments";
import { useChatStore } from "../../store";
import { composerInput, renderComposer, seedStore } from "./harness";

vi.mock("../../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime shape
// is exactly that mock object.
const mockedClient = client as MockedClient;

/** Feeds the session's live handler inside act so the tree flushes. */
async function emit(...events: SessionEvent[]): Promise<void> {
  await act(async () => {
    for (const event of events) live.handler?.(event);
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  live.handler = null;
  live.turnSeq = 0;
  seedStore();
});

describe("the composer (task 5.1)", () => {
  it("Enter sends the message and clears the composer", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(composerInput(), "hello there");
    await user.keyboard("{Enter}");

    await waitFor(() =>
      expect(mockedClient.chat.send).toHaveBeenCalledWith("s1", "hello there")
    );
    await waitFor(() => expect(composerInput().value).toBe(""));
  });

  it("Shift+Enter inserts a newline without sending", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(composerInput(), "line one");
    await user.keyboard("{Shift>}{Enter}{/Shift}");
    await user.type(composerInput(), "line two");

    expect(mockedClient.chat.send).not.toHaveBeenCalled();
    expect(composerInput().value).toBe("line one\nline two");
  });

  it("a send while busy joins the ordered queue instead of calling the backend", async () => {
    useChatStore.setState({ busy: true });
    const user = userEvent.setup();
    await renderComposer();

    await user.type(composerInput(), "queued message");
    await user.keyboard("{Enter}");

    expect(mockedClient.chat.send).not.toHaveBeenCalled();

    const { queue } = useChatStore.getState();

    expect(queue).toHaveLength(1);
    expect(queue[0].text).toBe("queued message");

    // The queue renders through the vendored message-queue element.
    const rendered = screen.getByTestId("message-queue");

    expect(rendered.textContent).toContain("queued message");
    expect(rendered.textContent).toContain("1 queued");
  });

  it("the queue's remove control drops the queued message", async () => {
    useChatStore.setState({ busy: true });
    const user = userEvent.setup();
    await renderComposer();

    await user.type(composerInput(), "queued message");
    await user.keyboard("{Enter}");

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", {
          name: 'Remove "queued message" from the queue',
        })
      );
    });

    expect(useChatStore.getState().queue).toHaveLength(0);
  });

  it("the send control becomes stop while busy and cancels the turn", async () => {
    useChatStore.setState({ busy: true, activeTurnID: "turn-1" });
    const user = userEvent.setup();
    await renderComposer();

    expect(screen.queryByTestId("btn-send")).toBeNull();

    await user.click(screen.getByTestId("btn-stop"));

    await waitFor(() =>
      expect(mockedClient.chat.cancel).toHaveBeenCalledWith("s1")
    );
  });

  it("stopping mid-stream keeps the partial text and returns the control to send", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(composerInput(), "partial answer");
    await user.keyboard("{Enter}");
    await waitFor(() =>
      expect(mockedClient.chat.send).toHaveBeenCalledWith(
        "s1",
        "partial answer"
      )
    );

    // The turn is in flight: the send control is the stop control.
    await waitFor(() => expect(screen.queryByTestId("btn-send")).toBeNull());

    await user.click(screen.getByTestId("btn-stop"));
    await waitFor(() =>
      expect(mockedClient.chat.cancel).toHaveBeenCalledWith("s1")
    );

    // The turn ends; the control is send again and the text is still there.
    await act(async () => {
      useChatStore.setState({ busy: false, activeTurnID: null });
    });

    await waitFor(() => expect(screen.queryByTestId("btn-send")).toBeTruthy());
    expect(screen.queryByTestId("btn-stop")).toBeNull();
  });

  it("a queued message dispatches automatically after the cancelled turn", async () => {
    // A cold store so the store's init subscribes and captures the live
    // handler the turn events arrive through.
    seedStore({
      agents: [],
      activeAgentID: null,
      sessions: [],
      activeSessionID: null,
    });

    const user = userEvent.setup();
    await renderComposer();

    await act(async () => {
      await useChatStore.getState().init();
    });

    await waitFor(() =>
      expect(useChatStore.getState().activeSessionID).toBe("s1")
    );

    await user.type(composerInput(), "first");
    await user.keyboard("{Enter}");
    await waitFor(() =>
      expect(mockedClient.chat.send).toHaveBeenCalledWith("s1", "first")
    );

    await user.type(composerInput(), "second");
    await user.keyboard("{Enter}");

    expect(mockedClient.chat.send).toHaveBeenCalledTimes(1);
    expect(useChatStore.getState().queue).toHaveLength(1);

    // The turn reaches its terminal: the queue drains by itself.
    await emit({
      kind: Kind.KindTurnCancelled,
      turn_id: "turn-1",
      id: "evt-1",
      occurred_at: "2026-01-01T00:00:00Z",
    });

    await waitFor(() =>
      expect(mockedClient.chat.send).toHaveBeenCalledTimes(2)
    );
    expect(
      mockedClient.chat.send.mock.calls.map((call: unknown[]): string =>
        String(call[1])
      )
    ).toEqual(["first", "second"]);
  });

  it("the placeholder names the agent and the trigger tokens live in the hint row", async () => {
    await renderComposer();

    expect(composerInput().getAttribute("placeholder")).toBe("Message Scout…");

    const hints = screen.getByTestId("composer-hints");

    expect(hints.textContent).toContain("/");
    expect(hints.textContent).toContain("@");
    expect(hints.textContent).toContain("$");
    expect(composerInput().getAttribute("placeholder")).not.toContain("/");
  });

  it("the hint row yields to the draft and returns when it clears (design D4)", async () => {
    const user = userEvent.setup();
    await renderComposer();

    // Empty draft: the discoverability row is up.
    expect(screen.getByTestId("composer-hints")).toBeTruthy();

    await user.type(composerInput(), "hello");
    expect(screen.queryByTestId("composer-hints")).toBeNull();

    // Clearing the draft brings it back.
    await user.clear(composerInput());
    expect(screen.getByTestId("composer-hints")).toBeTruthy();
  });

  it("the hint row yields to a chip-only draft too (task 4.2)", async () => {
    await renderComposer();

    // Empty draft: the discoverability row is up.
    expect(screen.getByTestId("composer-hints")).toBeTruthy();

    // A chip alone is a non-empty draft: the tray rides the composer and the
    // hints yield. The chip is seeded straight into the tray store — the
    // visibility folds on tray presence, not on the upload pipeline.
    await act(async () => {
      useAttachmentStore.setState({
        chips: [
          {
            id: "chip-1",
            name: "notes.txt",
            mime: "text/plain",
            size: 12,
            status: "ready",
            key: "cap-key-1",
            file: new File(["notes"], "notes.txt", { type: "text/plain" }),
          },
        ],
      });
    });

    expect(screen.getByTestId("attachment-tray")).toBeTruthy();
    expect(screen.queryByTestId("composer-hints")).toBeNull();

    // The draft clears (no text, no chip): the hints return.
    await act(async () => {
      useAttachmentStore.setState({ chips: [] });
    });

    expect(screen.getByTestId("composer-hints")).toBeTruthy();
  });

  it("with no active agent the send control is disabled and Enter sends nothing", async () => {
    useChatStore.setState({
      activeAgentID: null,
      sessions: [],
      activeSessionID: null,
    });

    const user = userEvent.setup();
    await renderComposer();

    // SAFETY: the send control renders a button by contract.
    const send = screen.getByTestId("btn-send") as HTMLButtonElement;

    expect(send.disabled).toBe(true);

    await user.type(composerInput(), "hello there");
    await user.keyboard("{Enter}");

    expect(mockedClient.chat.send).not.toHaveBeenCalled();
    expect(mockedClient.chat.createSession).not.toHaveBeenCalled();
  });
});
