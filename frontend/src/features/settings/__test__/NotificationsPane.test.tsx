/**
 * Component tests for the Notifications pane (add-desktop-notifications
 * task 6.1, spec settings "Notifications pane"): loads the persisted
 * singleton on open, saves durably on each toggle with the full settings
 * object, reflects the saved state, and — master off — renders the category
 * toggles disabled while their stored values ride along. The lib client is
 * mocked wholesale.
 */
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type {
  NotificationSettings,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";

import NotificationsPane from "../components/NotificationsPane";
import type { SettingsPaneConfig } from "../types";
import { renderUI } from "./test-utils";

/** Header config passed to the pane; mirrors the static nav entry. */
const pane: SettingsPaneConfig = {
  key: "notifications",
  label: "Notifications",
  icon: "bell",
  description:
    "Desktop notifications for agent turns, attention requests, schedules, and groups.",
};

let saved: NotificationSettings;

vi.mock("../../../lib/client", () => ({
  client: {
    notifications: {
      getNotificationSettings: vi.fn(
        async (): Promise<NotificationSettings> => saved
      ),
      saveNotificationSettings: vi.fn(
        async (next: NotificationSettings): Promise<void> => {
          saved = next;
        }
      ),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  notifications: {
    getNotificationSettings: MockFn;
    saveNotificationSettings: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

function makeSettings(
  patch: Partial<NotificationSettings> = {}
): NotificationSettings {
  return {
    Enabled: true,
    Turns: true,
    Attention: true,
    Schedules: true,
    Groups: true,
    UpdatedAt: "2026-01-01T00:00:00Z",
    ...patch,
  };
}

/** The category row's toggle element. */
function categoryToggle(
  queries: { getByTestId: (id: string) => HTMLElement },
  field: string
): HTMLElement {
  return queries.getByTestId(`notifications-toggle-${field.toLowerCase()}`);
}

describe("NotificationsPane", () => {
  beforeEach(() => {
    saved = makeSettings();
    vi.clearAllMocks();
  });

  it("loads the persisted settings on open and reflects them", async () => {
    saved = makeSettings({ Schedules: false });

    const { getByTestId, findByTestId } = await renderUI(
      <NotificationsPane pane={pane} />
    );

    expect(await findByTestId("notifications-pane")).toBeTruthy();
    expect(mockedClient.notifications.getNotificationSettings).toHaveBeenCalled();

    // The master and the loaded category values ride the persisted row.
    expect(
      getByTestId("notifications-toggle-master").getAttribute("aria-checked")
    ).toBe("true");
    expect(
      categoryToggle({ getByTestId }, "turns").getAttribute("aria-checked")
    ).toBe("true");
    expect(
      categoryToggle({ getByTestId }, "schedules").getAttribute("aria-checked")
    ).toBe("false");
    expect(
      categoryToggle({ getByTestId }, "groups").getAttribute("aria-checked")
    ).toBe("true");
  });

  it("saves the full updated settings on a category toggle", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <NotificationsPane pane={pane} />
    );

    await findByTestId("notifications-pane");

    await user.click(categoryToggle({ getByTestId }, "schedules"));

    expect(
      mockedClient.notifications.saveNotificationSettings
    ).toHaveBeenCalledWith(
      expect.objectContaining({
        Enabled: true,
        Turns: true,
        Attention: true,
        Schedules: false,
        Groups: true,
      })
    );

    // The pane reflects the saved state, and the master flag is untouched
    // (spec: the pane still shows the master flag enabled with the other
    // categories unchanged).
    expect(
      categoryToggle({ getByTestId }, "schedules").getAttribute("aria-checked")
    ).toBe("false");
    expect(
      getByTestId("notifications-toggle-master").getAttribute("aria-checked")
    ).toBe("true");
  });

  it("disables the category toggles with the master off while keeping stored values", async () => {
    const user = userEvent.setup();
    saved = makeSettings({ Attention: false });

    const { getByTestId, findByTestId } = await renderUI(
      <NotificationsPane pane={pane} />
    );

    await findByTestId("notifications-pane");

    await user.click(getByTestId("notifications-toggle-master"));

    // Master off persists…
    expect(
      getByTestId("notifications-toggle-master").getAttribute("aria-checked")
    ).toBe("false");

    // …every category toggle renders disabled (visually inactive)…
    for (const field of ["turns", "attention", "schedules", "groups"]) {
      const toggle = categoryToggle({ getByTestId }, field);
      expect((toggle as HTMLButtonElement).disabled).toBe(true);
    }

    // …while their stored values ride along untouched (Attention was off).
    expect(
      categoryToggle({ getByTestId }, "attention").getAttribute("aria-checked")
    ).toBe("false");
    expect(
      categoryToggle({ getByTestId }, "turns").getAttribute("aria-checked")
    ).toBe("true");
  });

  it("re-enables the category toggles with their values when the master returns on", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <NotificationsPane pane={pane} />
    );

    await findByTestId("notifications-pane");

    await user.click(getByTestId("notifications-toggle-master"));
    await user.click(getByTestId("notifications-toggle-master"));

    const turns = categoryToggle({ getByTestId }, "turns");

    expect((turns as HTMLButtonElement).disabled).toBe(false);
    expect(turns.getAttribute("aria-checked")).toBe("true");
    // Two saves: master off, then master on.
    expect(mockedClient.notifications.saveNotificationSettings).toHaveBeenCalledTimes(
      2
    );
  });

  it("shows the load failure and renders no rows", async () => {
    mockedClient.notifications.getNotificationSettings.mockRejectedValueOnce(
      new Error("bridge unavailable")
    );

    const { findByTestId, queryByTestId } = await renderUI(
      <NotificationsPane pane={pane} />
    );

    expect(await findByTestId("notifications-error")).toBeTruthy();
    expect(queryByTestId("notifications-list")).toBeNull();
  });

  it("shows a save failure without changing the reflected state", async () => {
    mockedClient.notifications.saveNotificationSettings.mockRejectedValueOnce(
      new Error("save failed")
    );
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <NotificationsPane pane={pane} />
    );

    await findByTestId("notifications-pane");

    await user.click(categoryToggle({ getByTestId }, "turns"));

    expect(await findByTestId("notifications-save-error")).toBeTruthy();
    // The pane keeps the last durable state: the toggle stays on.
    expect(
      categoryToggle({ getByTestId }, "turns").getAttribute("aria-checked")
    ).toBe("true");
  });
});
