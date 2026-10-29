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
    // The integrations pane subscribes to OAuth completions on mount (the
    // MCP pane's refetch pattern); no SettingsView test drives the flow.
    events: {
      onMcpOAuthCompleted: vi.fn((): (() => void) => (): void => undefined),
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

/** Sidebar order is the grouped nav order: Providers, Integrations, then
 * the AUTOMATION panes (Skills, Tools, Hooks, MCP). */
const navItemIds = [
  "settings-nav-item-providers",
  "settings-nav-item-integrations",
  "settings-nav-item-skills",
  "settings-nav-item-tools",
  "settings-nav-item-hooks",
  "settings-nav-item-mcp",
];

describe("SettingsView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // The pane label persists in the store across renders; reset per test.
    useNavStore.setState({ settingsPaneLabel: "Providers" });
  });

  it("opens on Providers: page title, grouped sidebar, and only that pane mounted", async () => {
    const { findByTestId, queryByTestId, getByTestId } = await renderUI(
      <SettingsView />
    );

    expect(await findByTestId("providers-pane")).toBeTruthy();
    expect(getByTestId("settings-pane-title").textContent).toBe("Providers");

    // The page chrome: Back header is retired; the grouped section list remains.
    expect(queryByTestId("settings-back")).toBeNull();

    for (const id of navItemIds) {
      expect(getByTestId(id)).toBeTruthy();
    }

    // Only the active pane mounts.
    expect(queryByTestId("skills-pane")).toBeNull();
    expect(mockedClient.settings.providers).toHaveBeenCalled();
  });

  it("groups the sidebar into MODELS, INTEGRATIONS, and AUTOMATION sections", async () => {
    const { getByTestId } = await renderUI(<SettingsView />);

    for (const id of [
      "settings-nav-section-models",
      "settings-nav-section-integrations",
      "settings-nav-section-automation",
    ]) {
      expect(getByTestId(id)).toBeTruthy();
    }

    // The AUTOMATION section carries its panes in nav order: Skills, Tools,
    // Hooks, MCP.
    const automationItems = Array.from(
      getByTestId("settings-nav-section-automation").querySelectorAll(
        "[data-testid^='settings-nav-item-']"
      )
    ).map((el: Element): string => el.getAttribute("data-testid") ?? "");

    expect(automationItems).toEqual([
      "settings-nav-item-skills",
      "settings-nav-item-tools",
      "settings-nav-item-hooks",
      "settings-nav-item-mcp",
    ]);
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

  it("selects the Integrations pane from the INTEGRATIONS section", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<SettingsView />);

    await user.click(getByTestId("settings-nav-item-integrations"));

    expect(
      getByTestId("settings-nav-item-integrations").getAttribute(
        "aria-selected"
      )
    ).toBe("true");
    expect(await findByTestId("integrations-pane")).toBeTruthy();
    expect(getByTestId("settings-pane-title").textContent).toBe("Integrations");
  });

  it("filters the pane list by label from the search input and restores it on clear", async () => {
    const user = userEvent.setup();

    const { getByTestId, queryByTestId } = await renderUI(<SettingsView />);

    const search = getByTestId("settings-nav-search");

    await user.type(search, "tool");

    // Only the label match survives; the other sections drop out.
    expect(getByTestId("settings-nav-item-tools")).toBeTruthy();
    expect(queryByTestId("settings-nav-item-providers")).toBeNull();
    expect(queryByTestId("settings-nav-item-skills")).toBeNull();

    await user.clear(search);

    // Clearing restores the full grouped list.
    for (const id of navItemIds) {
      expect(getByTestId(id)).toBeTruthy();
    }
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
