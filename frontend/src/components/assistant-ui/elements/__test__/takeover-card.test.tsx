/**
 * TakeoverCard tests (add-browser-login-handover task 4.2, spec
 * chat-approvals "Takeover card states"): pending renders Approve/Cancel;
 * Approve reaches `client.chat.beginBrowserTakeover` and flips the live-only
 * local state to active (Done/Cancel); Done reaches `resolveTakeover` with
 * outcome "completed"; Cancel reaches it with "cancelled" from either state;
 * a resolved receipt renders its wording with no controls.
 */
import { act } from "react";
import { renderUI } from "../../../__test__/test-utils";
import { mockedSurface } from "../../../../features/chat/__test__/test-utils";
import { fireEvent, screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type { Client } from "../../../../lib/client";
import type { InputApproval } from "../../../../features/chat/runtime/mapping";
import { useChatStore } from "../../../../features/chat/store";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the two takeover calls are reachable here. */
type MockedClient = Client & {
  chat: { beginBrowserTakeover: MockFn; resolveTakeover: MockFn };
};

function makeClient(): MockedClient {
  // SAFETY: the literal covers the surface this suite drives; the
  // Client-typed intersection defeats the structural check, so the
  // conversion funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>({
    chat: {
      beginBrowserTakeover: vi.fn(async (): Promise<void> => undefined),
      resolveTakeover: vi.fn(async (): Promise<void> => undefined),
    },
  });
}

vi.mock("../../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../../lib/client";
import { TakeoverCard } from "../takeover-card";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedBegin = (client as MockedClient).chat.beginBrowserTakeover;
const mockedResolveTakeover = (client as MockedClient).chat.resolveTakeover;

function takeoverApproval(extra: Partial<InputApproval> = {}): InputApproval {
  return {
    id: "int-2",
    prompt: "Sign-in is blocked — take over?",
    display: "decision",
    attention: "takeover",
    ...extra,
  };
}

async function click(testId: string): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByTestId(testId));
  });
}

beforeEach(async () => {
  vi.clearAllMocks();

  await act(async () => {
    useChatStore.setState({
      activeAgentID: "a1",
      activeSessionID: "s1",
      entries: [],
      busy: true,
    });
  });
});

describe("the takeover card (task 4.2)", () => {
  it("renders Approve/Cancel while pending", async () => {
    const { unmount } = await renderUI(
      <TakeoverCard approval={takeoverApproval()} />
    );

    const card = screen.getByTestId("takeover-card");

    expect(card.getAttribute("data-takeover-state")).toBe("pending");
    expect(screen.getByTestId("takeover-approve")).toBeTruthy();
    expect(screen.getByTestId("takeover-cancel")).toBeTruthy();
    expect(screen.queryByTestId("takeover-done")).toBeNull();

    await unmount();
  });

  it("Approve calls beginBrowserTakeover and flips the card to active", async () => {
    const { unmount } = await renderUI(
      <TakeoverCard approval={takeoverApproval()} />
    );

    await click("takeover-approve");

    expect(mockedBegin).toHaveBeenCalledTimes(1);
    expect(mockedBegin).toHaveBeenCalledWith("s1", "int-2");

    const card = screen.getByTestId("takeover-card");

    expect(card.getAttribute("data-takeover-state")).toBe("active");
    expect(screen.getByTestId("takeover-done")).toBeTruthy();
    expect(screen.queryByTestId("takeover-approve")).toBeNull();

    await unmount();
  });

  it("Done from active resolves the takeover as completed", async () => {
    const { unmount } = await renderUI(
      <TakeoverCard approval={takeoverApproval()} />
    );

    await click("takeover-approve");
    await click("takeover-done");

    expect(mockedResolveTakeover).toHaveBeenCalledTimes(1);
    expect(mockedResolveTakeover).toHaveBeenCalledWith(
      "s1",
      "int-2",
      "completed"
    );

    await unmount();
  });

  it("Cancel from pending resolves the takeover as cancelled", async () => {
    const { unmount } = await renderUI(
      <TakeoverCard approval={takeoverApproval()} />
    );

    await click("takeover-cancel");

    expect(mockedResolveTakeover).toHaveBeenCalledWith("s1", "int-2", "cancelled");

    await unmount();
  });

  it("Cancel from active also resolves the takeover as cancelled", async () => {
    const { unmount } = await renderUI(
      <TakeoverCard approval={takeoverApproval()} />
    );

    await click("takeover-approve");
    await click("takeover-cancel");

    expect(mockedResolveTakeover).toHaveBeenCalledWith("s1", "int-2", "cancelled");

    await unmount();
  });

  it("renders the completed receipt with no controls", async () => {
    const { unmount } = await renderUI(
      <TakeoverCard
        approval={takeoverApproval({ receipt: { outcome: "completed" } })}
      />
    );

    const card = screen.getByTestId("takeover-card");

    expect(card.getAttribute("data-takeover-state")).toBe("resolved");
    expect(screen.getByTestId("takeover-receipt").textContent).toBe(
      "Handed over — take a fresh snapshot"
    );
    expect(screen.queryByTestId("takeover-approve")).toBeNull();
    expect(screen.queryByTestId("takeover-done")).toBeNull();
    expect(screen.queryByTestId("takeover-cancel")).toBeNull();

    await unmount();
  });

  it("renders the cancelled and expired receipts with their own wording", async () => {
    const cancelled = await renderUI(
      <TakeoverCard
        approval={takeoverApproval({ receipt: { outcome: "cancelled" } })}
      />
    );

    expect(screen.getByTestId("takeover-receipt").textContent).toBe("Cancelled");

    await cancelled.unmount();

    const expired = await renderUI(
      <TakeoverCard
        approval={takeoverApproval({ receipt: { outcome: "expired" } })}
      />
    );

    expect(screen.getByTestId("takeover-receipt").textContent).toBe(
      "Expired — the agent can re-request"
    );

    await expired.unmount();
  });

  it("surfaces a failed begin as an error and stays pending", async () => {
    mockedBegin.mockRejectedValueOnce(new Error("Takeover refused"));

    const { unmount } = await renderUI(
      <TakeoverCard approval={takeoverApproval()} />
    );

    await click("takeover-approve");

    expect(screen.getByTestId("takeover-error").textContent).toBe(
      "Takeover refused"
    );
    expect(
      screen.getByTestId("takeover-card").getAttribute("data-takeover-state")
    ).toBe("pending");

    await unmount();
  });
});
