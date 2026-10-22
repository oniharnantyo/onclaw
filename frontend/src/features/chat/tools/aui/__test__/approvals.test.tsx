/**
 * Approval gate tests (task 4.4, spec chat-approvals "Approval card in
 * transcript"): a pending approval renders the vendored ApprovalCard with
 * the requested command and enabled Approve/Deny actions; the decision
 * rides the part's respondToApproval — and through the real runtime it
 * reaches the bound resolveApproval call the legacy card used; a decision
 * the runtime could not record (a stale interrupt) surfaces as a visible
 * error with the pending controls intact; resolved cards persist with their
 * approved/denied/cancelled wording and no actions; and a replayed
 * transcript rebuilds the pending state from the mapped parts alone.
 */
import { act } from "react";
import { mockedSurface } from "../../../__test__/mockedSurface";
import { renderUI } from "../../../__test__/test-utils";
import { fireEvent, screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import type { ToolApprovalResponse } from "@assistant-ui/react";

import {
  Kind,
  type Event as SessionEvent,
} from "../../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { applyEvent, asApproval, type Entry } from "../../../entries";
import { entriesToMessages } from "../../../runtime/mapping";
import { useChatStore } from "../../../store";
import { partProps, renderWithEntries } from "./harness";
import { ApprovalGate } from "../ApprovalGate";

function ev(
  kind: SessionEvent["kind"],
  extra: Partial<SessionEvent> = {}
): SessionEvent {
  return { kind, occurred_at: "2026-01-01T00:00:00Z", ...extra };
}

const REQUIRED: SessionEvent = ev(Kind.KindApprovalRequired, {
  turn_id: "t1",
  approval: {
    interrupt_id: "int-1",
    command: "rm -rf /tmp/build",
    checkpoint_id: "adk_t1",
  },
});

const RESOLVED: SessionEvent = ev(Kind.KindApprovalResolved, {
  turn_id: "t1",
  approval: { interrupt_id: "int-1", outcome: "approved" },
});

function fold(...events: SessionEvent[]): Entry[] {
  return events.reduce<Entry[]>(
    (entries: Entry[], e: SessionEvent) => applyEvent(entries, e),
    []
  );
}

import type { Client } from "../../../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: only resolveApproval is reachable here. */
type MockedClient = Client & {
  chat: { resolveApproval: MockFn };
};

function makeClient(): MockedClient {
  // SAFETY: the literal covers the surface this suite drives; the
  // Client-typed intersection defeats the structural check, so the
  // conversion funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>({
    chat: { resolveApproval: vi.fn(async (): Promise<void> => undefined) },
  });
}

vi.mock("../../../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedResolve = (client as MockedClient).chat.resolveApproval;

/** The pending gate's standalone part props (a manual respondToApproval
 * spy), for the gate-level scenarios. */
function pendingProps(
  respondToApproval: (response: ToolApprovalResponse) => Promise<void>
): ReturnType<typeof partProps> {
  return partProps({
    toolCallId: "c-approval",
    toolName: "execute",
    argsText: '{"command":"rm -rf /tmp/build"}',
    approval: { id: "int-1" },
    status: { type: "requires-action", reason: "interrupt" },
    respondToApproval,
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

describe("the pending gate (spec: Card actionable while pending)", () => {
  it("renders the requested command with enabled Approve and Deny actions", async () => {
    const { unmount } = await renderUI(
      <ApprovalGate part={pendingProps(async (): Promise<void> => undefined)} />
    );

    expect(screen.getByTestId("entry-approval").textContent).toContain(
      "rm -rf /tmp/build"
    );
    // SAFETY: the pending card renders both decision controls, and the
    // vendored ApprovalCard renders each of those slots as a button element.
    expect(
      (
        document.querySelector(
          '[data-slot="approval-card-allow-once"]'
        ) as HTMLButtonElement
      ).disabled
    ).toBe(false);
    // SAFETY: same as above — the deny slot is the vendored card's button.
    expect(
      (
        document.querySelector(
          '[data-slot="approval-card-deny"]'
        ) as HTMLButtonElement
      ).disabled
    ).toBe(false);

    await unmount();
  });

  it("Allow once and Deny respond with the decision on the part's approval API", async () => {
    const respond = vi.fn(async (): Promise<void> => undefined);

    const { unmount } = await renderUI(
      <ApprovalGate part={pendingProps(respond)} />
    );

    await act(async () => {
      fireEvent.click(
        document.querySelector('[data-slot="approval-card-allow-once"]')!
      );
    });

    expect(respond).toHaveBeenCalledWith({ approved: true });

    await act(async () => {
      fireEvent.click(
        document.querySelector('[data-slot="approval-card-deny"]')!
      );
    });

    expect(respond).toHaveBeenCalledWith({ approved: false });

    await unmount();
  });

  it("a rejected decision surfaces the conflict error and keeps the card pending", async () => {
    const respond = vi.fn(async (): Promise<void> => {
      throw new Error("approval no longer pending");
    });

    const { unmount } = await renderUI(
      <ApprovalGate part={pendingProps(respond)} />
    );

    await act(async () => {
      fireEvent.click(
        document.querySelector('[data-slot="approval-card-allow-once"]')!
      );
    });

    expect(screen.getByTestId("approval-error").textContent).toBe(
      "approval no longer pending"
    );
    // The decision must be re-submittable: the controls are back.
    expect(
      document.querySelector('[data-slot="approval-card-allow-once"]')
    ).toBeTruthy();
    expect(
      document.querySelector('[data-slot="approval-card-deny"]')
    ).toBeTruthy();

    await unmount();
  });
});

describe("resolved receipts (spec: Resolved card on replay)", () => {
  it("an approved decision with the command's result reads Approved with no actions", async () => {
    const { unmount } = await renderUI(
      <ApprovalGate
        part={partProps({
          toolCallId: "c-approval",
          toolName: "execute",
          argsText: '{"command":"make deploy"}',
          approval: { id: "int-1", approved: true },
          result: "deployed",
        })}
      />
    );

    const card = screen.getByTestId("entry-approval");

    expect(card.textContent).toContain("Approved");
    expect(card.textContent).toContain("deployed");
    expect(
      document.querySelector('[data-slot="approval-card-allow-once"]')
    ).toBeNull();
    expect(
      document.querySelector('[data-slot="approval-card-deny"]')
    ).toBeNull();

    await unmount();
  });

  it("a denied decision reads Denied; a cancelled one reads its own wording", async () => {
    const denied = await renderUI(
      <ApprovalGate
        part={partProps({
          toolCallId: "c-approval",
          toolName: "execute",
          argsText: '{"command":"make deploy"}',
          approval: { id: "int-1", approved: false },
        })}
      />
    );

    expect(screen.getByTestId("entry-approval").textContent).toContain(
      "Denied"
    );

    await denied.unmount();

    const cancelled = await renderUI(
      <ApprovalGate
        part={partProps({
          toolCallId: "c-approval",
          toolName: "execute",
          argsText: '{"command":"make deploy"}',
          approval: { id: "int-1", resolution: "cancelled" },
        })}
      />
    );

    expect(screen.getByTestId("entry-approval").textContent).toContain(
      "Cancelled before a decision"
    );

    await cancelled.unmount();
  });

  it("an approved decision whose turn has not produced the result yet reads Approved, running", async () => {
    const { unmount } = await renderUI(
      <ApprovalGate
        part={partProps({
          toolCallId: "c-approval",
          toolName: "execute",
          argsText: '{"command":"make deploy"}',
          approval: { id: "int-1", approved: true },
        })}
      />
    );

    expect(screen.getByTestId("entry-approval").textContent).toContain(
      "Approved, running"
    );

    await unmount();
  });
});

describe("the runtime chain (spec: Approve resumes the turn)", () => {
  it("a pending approval rebuilt from replayed entries is actionable and reaches resolveApproval", async () => {
    const entries = fold(REQUIRED);

    // The pending card is plain data on the mapped part — the replay path
    // and the live one map identically.
    const parts = entriesToMessages(entries)[0]?.content ?? [];

    expect(parts[0]).toMatchObject({
      type: "tool-call",
      approval: { id: "int-1" },
    });

    await renderWithEntries(entries, true);

    expect(screen.getByTestId("entry-approval").textContent).toContain(
      "rm -rf /tmp/build"
    );

    await act(async () => {
      fireEvent.click(
        document.querySelector('[data-slot="approval-card-allow-once"]')!
      );
    });

    await act(async () => {});

    expect(mockedResolve).toHaveBeenCalledWith("s1", "int-1", true);
  });

  it("a resolved approval on the replayed transcript renders its outcome", async () => {
    const entries = fold(REQUIRED, RESOLVED);

    expect(asApproval(entries[0]).outcome).toBe("approved");

    await renderWithEntries(entries, false);

    expect(screen.getByTestId("entry-approval").textContent).toContain(
      "Approved"
    );
    expect(
      document.querySelector('[data-slot="approval-card-allow-once"]')
    ).toBeNull();
    expect(mockedResolve).not.toHaveBeenCalled();
  });
});
