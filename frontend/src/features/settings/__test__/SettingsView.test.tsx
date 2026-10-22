import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type { Client } from "../../../lib/client";

import SettingsView from "../index";
import { useNavStore } from "../../../stores/nav";
import { renderUI } from "./test-utils";

vi.mock("../../../lib/client", () => ({
  client: {
    agents: {
      list: vi.fn(async () => []),
    },
    settings: {
      providers: vi.fn(async () => []),
      skills: vi.fn(async () => []),
      tools: vi.fn(async () => []),
      hooks: vi.fn(async () => []),
      mcpServers: vi.fn(async () => []),
      webSearchProviders: vi.fn(async () => []),
      webSearchConfig: vi.fn(async () => ({ entries: [] })),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  agents: {
    list: MockFn;
  };
  settings: {
    providers: MockFn;
    skills: MockFn;
    tools: MockFn;
    hooks: MockFn;
    mcpServers: MockFn;
    webSearchProviders: MockFn;
    webSearchConfig: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

/** Sidebar order is onclaw-v2's flat section order — no grouping. */
const navItemIds = [
  "settings-nav-item-providers",
  "settings-nav-item-mcp",
  "settings-nav-item-skills",
  "settings-nav-item-tools",
  "settings-nav-item-hooks",
];

describe("SettingsView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // The pane label persists in the store across renders; reset per test.
    useNavStore.setState({ settingsPaneLabel: "Providers" });
  });

  it("opens on Providers: page title, flat sidebar, and only that pane mounted", async () => {
    const { findByTestId, queryByTestId, getByTestId } = await renderUI(
      <SettingsView />
    );

    expect(await findByTestId("providers-pane")).toBeTruthy();
    expect(getByTestId("settings-pane-title").textContent).toBe("Providers");

    // The page chrome: Back header is retired; flat section list remains.
    expect(queryByTestId("settings-back")).toBeNull();

    for (const id of navItemIds) {
      expect(getByTestId(id)).toBeTruthy();
    }

    // Only the active pane mounts.
    expect(queryByTestId("skills-pane")).toBeNull();
    expect(mockedClient.settings.providers).toHaveBeenCalled();
  });

  it("switches the content pane when a sidebar entry is selected", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <SettingsView />
    );

    // Sidebar rows are tabs: the active one carries aria-selected.
    expect(
      getByTestId("settings-nav-item-providers").getAttribute("aria-selected")
    ).toBe("true");

    await user.click(getByTestId("settings-nav-item-skills"));

    expect(
      getByTestId("settings-nav-item-skills").getAttribute("aria-selected")
    ).toBe("true");
    expect(
      getByTestId("settings-nav-item-providers").getAttribute("aria-selected")
    ).toBe("false");
    expect(await findByTestId("skills-pane")).toBeTruthy();
    expect(queryByTestId("providers-pane")).toBeNull();
    expect(getByTestId("settings-pane-title").textContent).toBe("Skills");
    expect(mockedClient.settings.skills).toHaveBeenCalled();
  });

  it("publishes the active pane label to the nav store", async () => {
    const user = userEvent.setup();
    const { getByTestId } = await renderUI(<SettingsView />);

    // On mount the initial pane's label is already published.
    expect(useNavStore.getState().settingsPaneLabel).toBe("Providers");

    await user.click(getByTestId("settings-nav-item-skills"));

    expect(useNavStore.getState().settingsPaneLabel).toBe("Skills");
  });
});
