import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type {
  WebhookDeliveryView,
  WebhookEndpointView,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";

import { DeliveriesModal } from "../components/webhooks/DeliveriesModal";
import { renderUI } from "./test-utils";
import { useNavStore } from "../../../stores/nav";

let rows: WebhookDeliveryView[];

vi.mock("../../../lib/client", () => ({
  client: {
    webhooks: {
      deliveries: vi.fn(async (): Promise<WebhookDeliveryView[]> => [...rows]),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as Client & {
  webhooks: { deliveries: MockFn };
};

function makeDelivery(
  patch: Partial<WebhookDeliveryView> = {}
): WebhookDeliveryView {
  return {
    id: "d1",
    attemptedAt: "2026-11-09T10:00:00Z",
    status: 202,
    outcome: "accepted",
    sessionId: "",
    messageId: "",
    via: "gitlab",
    senderDeliveryId: "",
    note: "",
    createdAt: "",
    ...patch,
  };
}

function makeEndpoint(
  patch: Partial<WebhookEndpointView> = {}
): WebhookEndpointView {
  return {
    id: "w1",
    name: "GitLab deploys",
    agentId: "a1",
    groupId: "",
    targetName: "Scout",
    prompt: "",
    enabled: true,
    verifiers: null,
    url: "http://127.0.0.1:8787/webhooks/w1",
    lastOutcome: "",
    lastStatus: 0,
    lastAttemptedAt: "",
    createdAt: "",
    updatedAt: "",
    ...patch,
  };
}

describe("DeliveriesModal", () => {
  beforeEach(() => {
    rows = [];
    vi.clearAllMocks();
    useNavStore.setState({
      menu: "settings",
      chatTarget: null,
      returnMenu: "agents",
    });
  });

  it("renders rows newest-first with outcome, status, via, and sender id", async () => {
    rows = [
      makeDelivery({
        id: "d2",
        outcome: "accepted",
        status: 202,
        via: "gitlab",
      }),
      makeDelivery({
        id: "d1",
        outcome: "duplicate",
        status: 200,
        senderDeliveryId: "svix_abc",
      }),
    ];

    const { findByTestId } = await renderUI(
      <DeliveriesModal
        endpoint={makeEndpoint()}
        refreshKey={0}
        onClose={(): void => undefined}
      />
    );

    // The store's order renders verbatim (newest first per the contract).
    const list = await findByTestId("webhook-deliveries-list");

    for (const id of ["d2", "d1"]) {
      expect(
        list.querySelector(`[data-testid="webhook-delivery-${id}"]`)
      ).not.toBeNull();
    }

    expect(
      (await findByTestId("webhook-delivery-outcome-d2")).textContent
    ).toBe("accepted");
    expect((await findByTestId("webhook-delivery-status-d2")).textContent).toBe(
      "HTTP 202"
    );
    expect((await findByTestId("webhook-delivery-via-d2")).textContent).toBe(
      "via gitlab"
    );
    expect((await findByTestId("webhook-delivery-sender-d1")).textContent).toBe(
      "svix_abc"
    );
  });

  it("gives auth-failed entries the danger treatment with their note", async () => {
    rows = [
      makeDelivery({
        id: "d3",
        outcome: "auth-failed",
        status: 401,
        note: "signature did not verify",
      }),
    ];

    const { findByTestId } = await renderUI(
      <DeliveriesModal
        endpoint={makeEndpoint()}
        refreshKey={0}
        onClose={(): void => undefined}
      />
    );

    const chip = await findByTestId("webhook-delivery-outcome-d3");

    expect(chip.textContent).toBe("auth-failed");
    // Distinct outcome treatment: the danger-tinted chip classes.
    expect(chip.className).toContain("text-dangertext");

    expect((await findByTestId("webhook-delivery-note-d3")).textContent).toBe(
      "signature did not verify"
    );
  });

  it("links an accepted agent-target row to its session through the nav store", async () => {
    rows = [makeDelivery({ id: "d4", sessionId: "s9", outcome: "accepted" })];
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(
      <DeliveriesModal
        endpoint={makeEndpoint({ agentId: "a1" })}
        refreshKey={0}
        onClose={(): void => undefined}
      />
    );

    await user.click(await findByTestId("webhook-delivery-open-d4"));

    const nav = useNavStore.getState();

    expect(nav.menu).toBe("chat");
    expect(nav.chatTarget).toEqual({ agentID: "a1", sessionID: "s9" });
  });

  it("links a dispatched group-target row to the group transcript", async () => {
    rows = [
      makeDelivery({
        id: "d5",
        messageId: "m3",
        sessionId: "",
        outcome: "ran",
        via: "slack",
      }),
    ];
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(
      <DeliveriesModal
        endpoint={makeEndpoint({ agentId: "", groupId: "g1" })}
        refreshKey={0}
        onClose={(): void => undefined}
      />
    );

    expect(
      (await findByTestId("webhook-delivery-open-d5")).textContent
    ).toContain("Open group");

    await user.click(await findByTestId("webhook-delivery-open-d5"));

    expect(useNavStore.getState().chatTarget).toEqual({ groupID: "g1" });
  });

  it("non-dispatched outcomes carry no link affordance", async () => {
    rows = [
      makeDelivery({
        id: "d6",
        outcome: "paused",
        status: 503,
        sessionId: "s9",
      }),
    ];

    const { findByTestId, queryByTestId } = await renderUI(
      <DeliveriesModal
        endpoint={makeEndpoint()}
        refreshKey={0}
        onClose={(): void => undefined}
      />
    );

    await findByTestId("webhook-delivery-d6");

    expect(queryByTestId("webhook-delivery-open-d6")).toBeNull();
  });

  it("shows the empty state and refetches when the refresh key bumps", async () => {
    const { findByTestId, rerender } = await renderUI(
      <DeliveriesModal
        endpoint={makeEndpoint()}
        refreshKey={0}
        onClose={(): void => undefined}
      />
    );

    expect(await findByTestId("webhook-deliveries-empty")).toBeTruthy();
    expect(mockedClient.webhooks.deliveries).toHaveBeenCalledTimes(1);

    // A completed self-test bumps the section's refresh key; the open log
    // refetches and shows the new row.
    rows = [makeDelivery({ id: "d7", via: "self-test" })];

    await act(async () => {
      await rerender(
        <DeliveriesModal
          endpoint={makeEndpoint()}
          refreshKey={1}
          onClose={(): void => undefined}
        />
      );
    });

    expect(mockedClient.webhooks.deliveries).toHaveBeenCalledTimes(2);
    expect(await findByTestId("webhook-delivery-d7")).toBeTruthy();
    expect((await findByTestId("webhook-delivery-via-d7")).textContent).toBe(
      "via self-test"
    );
  });
});
