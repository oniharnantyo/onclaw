import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type {
  WebhookDeliveryView,
  WebhookEndpointView,
  WebhookListenerView,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client, WebhooksChangedHandler } from "../../../lib/client";

import WebhooksSection from "../components/webhooks/WebhooksSection";
import { renderUI, setInput } from "./test-utils";

let endpoints: WebhookEndpointView[];

let listener: WebhookListenerView;

let deliveries: WebhookDeliveryView[];

let addressError: Error | null = null;

let testError: Error | null = null;

/** Handlers captured from the webhooks:changed subscription. */
const changedHandlers: WebhooksChangedHandler[] = [];

vi.mock("../../../lib/client", () => ({
  client: {
    // The wizard the section mounts reads the target picker's listings.
    agents: {
      list: vi.fn(async (): Promise<never[]> => []),
    },
    groups: {
      list: vi.fn(async (): Promise<never[]> => []),
    },
    webhooks: {
      list: vi.fn(async (): Promise<WebhookEndpointView[]> => [...endpoints]),
      listenerState: vi.fn(async (): Promise<WebhookListenerView> => ({
        ...listener,
      })),
      setListenerAddress: vi.fn(async (addr: string): Promise<void> => {
        if (addressError !== null) throw addressError;

        listener =
          addr === ""
            ? { serving: true, boundAddr: "127.0.0.1:8787", bindError: "" }
            : { serving: true, boundAddr: addr, bindError: "" };
      }),
      setEnabled: vi.fn(
        async (id: string, enabled: boolean): Promise<WebhookEndpointView> => {
          const found = endpoints.find(
            (row: WebhookEndpointView): boolean => row.id === id
          );

          // SAFETY: the fixture list the test set always holds the id.
          const next = { ...found!, enabled };

          endpoints = endpoints.map((row: WebhookEndpointView) =>
            row.id === id ? next : row
          );

          return next;
        }
      ),
      test: vi.fn(async (_id: string): Promise<void> => {
        if (testError !== null) throw testError;
      }),
      delete: vi.fn(async (): Promise<void> => undefined),
      presets: vi.fn(async (): Promise<never[]> => []),
      deliveries: vi.fn(async (): Promise<WebhookDeliveryView[]> => [
        ...deliveries,
      ]),
    },
    events: {
      onWebhooksChanged: vi.fn((handler: WebhooksChangedHandler) => {
        changedHandlers.push(handler);

        return (): void => undefined;
      }),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as Client & {
  webhooks: {
    list: MockFn;
    listenerState: MockFn;
    setListenerAddress: MockFn;
    setEnabled: MockFn;
    test: MockFn;
    deliveries: MockFn;
  };
  events: { onWebhooksChanged: MockFn };
};

function makeListener(
  patch: Partial<WebhookListenerView> = {}
): WebhookListenerView {
  return {
    serving: true,
    boundAddr: "127.0.0.1:8787",
    bindError: "",
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

/** Fires the backend's webhooks:changed push (every mutation announces). */
async function emitChanged(): Promise<void> {
  await act(async () => {
    for (const handler of changedHandlers) handler({ endpointId: "w1" });
  });
}

describe("WebhooksSection", () => {
  beforeEach(() => {
    endpoints = [];
    listener = makeListener();
    deliveries = [];
    addressError = null;
    testError = null;
    changedHandlers.length = 0;
    vi.clearAllMocks();
  });

  it("renders the listener serving chip and the endpoint rows with target and last outcome", async () => {
    endpoints = [
      makeEndpoint({
        lastOutcome: "accepted",
        lastStatus: 202,
        lastAttemptedAt: "2026-11-09T10:00:00Z",
      }),
      makeEndpoint({
        id: "w2",
        name: "Stripe bills",
        enabled: false,
        targetName: "",
        lastOutcome: "auth-failed",
        lastStatus: 401,
      }),
    ];

    const { findByTestId } = await renderUI(<WebhooksSection />);

    expect(await findByTestId("webhooks-section")).toBeTruthy();
    expect((await findByTestId("webhook-listener-state")).textContent).toBe(
      "serving"
    );

    const row = await findByTestId("webhook-endpoint-w1");
    expect(row.textContent).toContain("GitLab deploys");
    expect((await findByTestId("webhook-target-w1")).textContent).toBe("Scout");
    expect((await findByTestId("webhook-last-w1")).textContent).toBe(
      "accepted · HTTP 202"
    );

    // Dead target and the failed outcome both surface in the row.
    expect((await findByTestId("webhook-target-w2")).textContent).toBe(
      "target missing"
    );
    expect((await findByTestId("webhook-last-w2")).textContent).toBe(
      "auth-failed · HTTP 401"
    );
    expect((await findByTestId("webhook-last-w2")).className).toContain(
      "text-dangertext"
    );
  });

  it("surfaces a bind failure as an error state with its reason", async () => {
    listener = makeListener({
      serving: false,
      boundAddr: "",
      bindError: "listen tcp 127.0.0.1:8787: bind: address already in use",
    });

    const { findByTestId } = await renderUI(<WebhooksSection />);

    expect((await findByTestId("webhook-listener-state")).textContent).toBe(
      "bind failed"
    );
    expect((await findByTestId("webhook-bind-error")).textContent).toContain(
      "address already in use"
    );
  });

  it("saves the listener address and reads the rebound state", async () => {
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<WebhooksSection />);

    const field = await findByTestId("webhook-address");
    await setInput(field, "0.0.0.0:9000");
    await user.click(await findByTestId("webhook-address-save"));

    expect(mockedClient.webhooks.setListenerAddress).toHaveBeenCalledWith(
      "0.0.0.0:9000"
    );
    // The listener state was read back after the rebind.
    expect(mockedClient.webhooks.listenerState).toHaveBeenCalledTimes(2);
    expect((await findByTestId("webhook-listener-state")).textContent).toBe(
      "serving"
    );
  });

  it("warns before saving a non-loopback bind", async () => {
    const { findByTestId, queryByTestId } = await renderUI(<WebhooksSection />);

    // The loopback default is quiet.
    await findByTestId("webhook-address");
    expect(queryByTestId("webhook-loopback-warning")).toBeNull();

    await setInput(await findByTestId("webhook-address"), "192.168.1.10:8787");

    expect(await findByTestId("webhook-loopback-warning")).toBeTruthy();

    // Empty resets the loopback default — also quiet.
    await setInput(await findByTestId("webhook-address"), "");
    expect(queryByTestId("webhook-loopback-warning")).toBeNull();
  });

  it("reports a refused address save inline", async () => {
    addressError = new Error("invalid address");
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<WebhooksSection />);

    await user.click(await findByTestId("webhook-address-save"));

    expect((await findByTestId("webhook-address-error")).textContent).toBe(
      "invalid address"
    );
  });

  it("surfaces a successful self-test inline and refetches on webhooks:changed", async () => {
    endpoints = [makeEndpoint()];
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<WebhooksSection />);

    await user.click(await findByTestId("webhook-test-w1"));

    expect(mockedClient.webhooks.test).toHaveBeenCalledWith("w1");
    expect(
      (await findByTestId("webhook-test-result-w1")).textContent
    ).toContain("Accepted self-test");

    // The completed self-test announces itself; the section re-lists.
    const before = mockedClient.webhooks.list.mock.calls.length;

    await emitChanged();

    expect(mockedClient.webhooks.list.mock.calls.length).toBe(before + 1);
  });

  it("surfaces a failed self-test's error text inline", async () => {
    endpoints = [makeEndpoint()];
    testError = new Error("webhook self-test: the receiver answered 401");
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<WebhooksSection />);

    await user.click(await findByTestId("webhook-test-w1"));

    expect(
      (await findByTestId("webhook-test-result-w1")).textContent
    ).toContain("the receiver answered 401");
  });

  it("toggles an endpoint through setEnabled", async () => {
    endpoints = [makeEndpoint()];
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<WebhooksSection />);

    await user.click(await findByTestId("webhook-enabled-w1"));

    expect(mockedClient.webhooks.setEnabled).toHaveBeenCalledWith("w1", false);
  });

  it("opens the wizard from Add and the deliveries log from the row", async () => {
    endpoints = [makeEndpoint()];
    const user = userEvent.setup();

    const { findByTestId, getByRole, queryByTestId } = await renderUI(
      <WebhooksSection />
    );

    await user.click(await findByTestId("webhook-add"));
    expect(await findByTestId("webhook-wizard")).toBeTruthy();

    // Close through the shell's dialog close button (a fresh wizard sits on
    // Basics, whose footer has no Done).
    await user.click(getByRole("button", { name: "Close dialog" }));

    await user.click(await findByTestId("webhook-deliveries-w1"));
    expect(await findByTestId("webhook-deliveries-modal")).toBeTruthy();
    expect(mockedClient.webhooks.deliveries).toHaveBeenCalledWith("w1");

    // Closing the log returns to the section.
    await user.click(getByRole("button", { name: "Close dialog" }));
    expect(queryByTestId("webhook-deliveries-modal")).toBeNull();
  });
});
