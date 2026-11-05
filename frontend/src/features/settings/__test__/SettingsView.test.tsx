import { act } from "react";
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
      // The Browser Use pane reads on selection (rebrand-browser-use-system-mcp
      // 5.1): config round trip + profile list + system row status.
      browserConfig: vi.fn(async () => ({
        defaultProfile: "default",
        headless: true,
        idleTimeoutSeconds: 120,
        actionTimeoutSeconds: 30,
        allowPrivate: false,
      })),
      browserProfiles: vi.fn(async () => [
        { name: "default", modified_at: "", size_bytes: 0 },
      ]),
    },
    channels: {
      channel: vi.fn(async () => ({
        id: "telegram",
        enabled: false,
        tokenSet: false,
        tokenHint: "",
        proxyUrl: "",
        createdAt: "",
        updatedAt: "",
      })),
      chats: vi.fn(async () => []),
      pendingPairings: vi.fn(async () => []),
    },
    // The integrations pane subscribes to OAuth completions on mount (the
    // MCP pane's refetch pattern); no SettingsView test drives the flow. The
    // channels pane subscribes to its change/state events the same way.
    events: {
      onMcpOAuthCompleted: vi.fn((): (() => void) => (): void => undefined),
      onChannelsChanged: vi.fn((): (() => void) => (): void => undefined),
      onChannelsState: vi.fn((): (() => void) => (): void => undefined),
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

/** Sidebar order is the grouped nav order: Providers, Integrations, Channels,
 * then the AUTOMATION panes (Skills, Tools, Hooks, MCP). */
const navItemIds = [
  "settings-nav-item-providers",
  "settings-nav-item-integrations",
  "settings-nav-item-channels",
  "settings-nav-item-browseruse",
  "settings-nav-item-skills",
  "settings-nav-item-tools",
  "settings-nav-item-hooks",
  "settings-nav-item-mcp",
];

describe("SettingsView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // The pane label persists in the store across renders; reset per test.
    useNavStore.setState({
      settingsPaneLabel: "Providers",
      settingsPaneRequest: null,
    });
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

    // The INTEGRATIONS section carries its panes in nav order: Integrations,
    // Channels (add-telegram-channel task 9.1), Browser Use
    // (rebrand-browser-use-system-mcp 5.1).
    const integrationItems = Array.from(
      getByTestId("settings-nav-section-integrations").querySelectorAll(
        "[data-testid^='settings-nav-item-']"
      )
    ).map((el: Element): string => el.getAttribute("data-testid") ?? "");

    expect(integrationItems).toEqual([
      "settings-nav-item-integrations",
      "settings-nav-item-channels",
      "settings-nav-item-browseruse",
    ]);

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

  it("moves the sidebar selection with the arrow keys and labels the pane by the active tab", async () => {
    const user = userEvent.setup();

    // The settings render helper leaves prior containers mounted; the tab
    // hook reaches tabs by document id, so drop them first to keep the
    // document-id lookup scoped to this render.
    document.body.innerHTML = "";

    const { container, getByTestId, getByRole } = await renderUI(
      <SettingsView />
    );

    const providers = getByTestId("settings-nav-item-providers");

    // Roving tabindex: only the active tab is in the tab order.
    expect(providers.getAttribute("tabindex")).toBe("0");
    expect(
      getByTestId("settings-nav-item-integrations").getAttribute("tabindex")
    ).toBe("-1");

    providers.focus();
    await user.keyboard("{ArrowRight}");

    const integrations = getByTestId("settings-nav-item-integrations");

    expect(integrations.getAttribute("aria-selected")).toBe("true");
    expect(integrations.getAttribute("tabindex")).toBe("0");
    expect(providers.getAttribute("aria-selected")).toBe("false");
    expect(providers.getAttribute("tabindex")).toBe("-1");
    // Focus rides the selection.
    expect(document.activeElement).toBe(integrations);

    // The active pane body is the panel labelled by the selected tab.
    const panel = container.querySelector(
      `[role="tabpanel"][aria-labelledby="${integrations.id}"]`
    );

    expect(panel).toBeTruthy();

    // The tabs live inside the tablist container.
    const tablist = getByRole("tablist");

    expect(tablist.getAttribute("aria-label")).toBe("Settings sections");
    expect(tablist.contains(integrations)).toBe(true);
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

  it("selects the Channels pane from the INTEGRATIONS section", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<SettingsView />);

    await user.click(getByTestId("settings-nav-item-channels"));

    expect(
      getByTestId("settings-nav-item-channels").getAttribute("aria-selected")
    ).toBe("true");
    expect(await findByTestId("channels-pane")).toBeTruthy();
    expect(getByTestId("settings-pane-title").textContent).toBe("Channels");
  });

  it("selects the Browser Use pane from the INTEGRATIONS section", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<SettingsView />);

    await user.click(getByTestId("settings-nav-item-browseruse"));

    expect(
      getByTestId("settings-nav-item-browseruse").getAttribute("aria-selected")
    ).toBe("true");
    expect(await findByTestId("browseruse-pane")).toBeTruthy();
    expect(getByTestId("settings-pane-title").textContent).toBe("Browser Use");
    expect(mockedClient.settings.browserConfig).toHaveBeenCalled();
  });

  it("opens the pane a cross-menu openSettingsPane request named", async () => {
    // Prior renders stay mounted (see the keyboard test); drop them so the
    // testid lookups hit this render.
    document.body.innerHTML = "";

    const { getByTestId, findByTestId } = await renderUI(<SettingsView />);

    // The MCP pane's "open settings" on the Browser Use row rides the nav
    // store (design D11): the request names one pane, the view consumes it.
    await act(async () => {
      useNavStore.getState().openSettingsPane("browseruse");
    });

    expect(
      getByTestId("settings-nav-item-browseruse").getAttribute("aria-selected")
    ).toBe("true");
    expect(await findByTestId("browseruse-pane")).toBeTruthy();
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
