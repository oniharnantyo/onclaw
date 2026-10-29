import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type {
  BrowserConfig,
  BrowserProfile,
  DocumentPresetEntry,
  ToolEntry,
  WebSearchChainEntry,
  WebSearchConfig,
  WebSearchProviderInfo,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";

import ToolsPane from "../components/ToolsPane";
import { renderUI } from "./test-utils";

const tools: ToolEntry[] = [];

/** The persisted browser config the mocked browser dialog read returns. */
let browserSettings: BrowserConfig = {
  defaultProfile: "default",
  headless: true,
  idleTimeoutSeconds: 120,
  actionTimeoutSeconds: 30,
  allowPrivate: false,
};

/** The browser profile fixture, mirroring the service's seeded default. */
let browserProfiles: BrowserProfile[] = [
  { name: "default", modified_at: "", size_bytes: 0 },
];

/** The provider catalog fixture, mirroring the Go registry order. */
const providers: WebSearchProviderInfo[] = [
  { id: "brave", label: "Brave", credential: "api_key" },
  { id: "exa", label: "Exa", credential: "api_key" },
  { id: "perplexity", label: "Perplexity", credential: "api_key" },
  { id: "firecrawl", label: "Firecrawl", credential: "api_key" },
  { id: "searxng", label: "SearXNG", credential: "base_url" },
];

/** The persisted web_search chain fixture the mocked config read returns. */
let chain: WebSearchConfig = { entries: [] };

/** The document preset fixtures the mocked dialog read returns. */
let documentPresets: DocumentPresetEntry[] = [];

/** The stored default document preset name; "" when none. */
let documentDefault = "";

vi.mock("../../../lib/client", () => ({
  client: {
    settings: {
      tools: vi.fn(async () => [...tools]),
      setToolEnabled: vi.fn(async (name: string, enabled: boolean) => {
        for (const t of tools) if (t.Name === name) t.Enabled = enabled;
      }),
      webSearchProviders: vi.fn(async () => [...providers]),
      webSearchConfig: vi.fn(async () => chain),
      saveWebSearchConfig: vi.fn(async (config: WebSearchConfig) => {
        // Mirror the service: assign ids to new entries, store the chain.
        chain = {
          entries: (config.entries ?? []).map(
            (entry: WebSearchChainEntry, i: number) => ({
              ...entry,
              id: entry.id || `entry-${i + 1}`,
            })
          ),
          request_timeout_seconds: config.request_timeout_seconds,
        };
      }),
      browserConfig: vi.fn(async () => browserSettings),
      browserProfiles: vi.fn(async () => [...browserProfiles]),
      documentPresets: vi.fn(async () => [...documentPresets]),
      defaultDocumentPreset: vi.fn(async () => documentDefault),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  settings: {
    tools: MockFn;
    setToolEnabled: MockFn;
    webSearchProviders: MockFn;
    webSearchConfig: MockFn;
    saveWebSearchConfig: MockFn;
    browserConfig: MockFn;
    browserProfiles: MockFn;
    documentPresets: MockFn;
    defaultDocumentPreset: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

describe("ToolsPane", () => {
  beforeEach(() => {
    tools.length = 0;
    chain = { entries: [] };
    browserSettings = {
      defaultProfile: "default",
      headless: true,
      idleTimeoutSeconds: 120,
      actionTimeoutSeconds: 30,
      allowPrivate: false,
    };
    browserProfiles = [{ name: "default", modified_at: "", size_bytes: 0 }];
    documentPresets = [];
    documentDefault = "";
    vi.clearAllMocks();
  });

  // jsdom lacks the dialog scroll lock's cleanup observability; nothing to
  // assert here — the lock releases on unmount via ModalShell's effect.
  afterEach(() => {
    document.documentElement.style.overflow = "";
    document.body.style.overflow = "";
  });

  it("lists tools with display name, icon tile, and description", async () => {
    tools.push({
      Name: "read_file",
      DisplayName: "Read File",
      IconKey: "file",
      Description: "Read a file from disk",
      Enabled: true,
    });
    const { findByTestId } = await renderUI(<ToolsPane />);

    const row = await findByTestId("tool-read_file");
    expect(row.textContent).toContain("Read File");
    expect(row.textContent).toContain("Read a file from disk");
    // The accent icon tile leads the row.
    expect(row.querySelector("svg")).toBeTruthy();
  });

  it("lists the four task tools in place of the retired todo tools", async () => {
    tools.push(
      {
        Name: "task_create",
        DisplayName: "Create Task",
        IconKey: "list-checks",
        Description: "Create a new task",
        Enabled: true,
      },
      {
        Name: "task_get",
        DisplayName: "Get Task",
        IconKey: "file",
        Description: "Get details of a task",
        Enabled: true,
      },
      {
        Name: "task_update",
        DisplayName: "Update Task",
        IconKey: "edit",
        Description: "Update a task",
        Enabled: true,
      },
      {
        Name: "task_list",
        DisplayName: "List Tasks",
        IconKey: "list",
        Description: "List all tasks",
        Enabled: true,
      }
    );
    const { findByTestId } = await renderUI(<ToolsPane />);

    for (const name of [
      "task_create",
      "task_get",
      "task_update",
      "task_list",
    ]) {
      const row = await findByTestId(`tool-${name}`);
      expect(row).toBeTruthy();
    }
  });

  it("offers Rescan catalog as the empty state's next-step action", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ToolsPane />);

    expect(await findByTestId("tools-empty")).toBeTruthy();

    await user.click(getByTestId("tools-reload"));
    expect(mockedClient.settings.tools).toHaveBeenCalledTimes(2);
  });

  it("toggles a tool off and on", async () => {
    tools.push({
      Name: "read_file",
      DisplayName: "Read File",
      IconKey: "file",
      Description: "",
      Enabled: true,
    });
    const user = userEvent.setup();
    const { findByTestId } = await renderUI(<ToolsPane />);

    // The kit Toggle is a role=switch button, not a checkbox input.
    const toggle = await findByTestId("tool-toggle-read_file");
    expect(toggle.getAttribute("role")).toBe("switch");
    expect(toggle.getAttribute("aria-label")).toBe("Enable read_file");
    expect(toggle.getAttribute("aria-checked")).toBe("true");

    await user.click(toggle);
    expect(mockedClient.settings.setToolEnabled).toHaveBeenCalledWith(
      "read_file",
      false
    );

    const toggledOff = await findByTestId("tool-toggle-read_file");
    expect(toggledOff.getAttribute("aria-checked")).toBe("false");

    await user.click(toggledOff);
    expect(mockedClient.settings.setToolEnabled).toHaveBeenCalledWith(
      "read_file",
      true
    );
  });

  it("opens the Web Search chain editor from the row's gear", async () => {
    tools.push(
      {
        Name: "read_file",
        DisplayName: "Read File",
        IconKey: "file",
        Description: "",
        Enabled: true,
      },
      {
        Name: "web_search",
        DisplayName: "Web Search",
        IconKey: "search",
        Description: "Search the web",
        Enabled: true,
      }
    );
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <ToolsPane />
    );

    // Only web_search carries the gear.
    expect(queryByTestId("btn-tool-config-read_file")).toBeNull();
    await user.click(await findByTestId("btn-tool-config-web_search"));

    expect(await findByTestId("tool-config-dialog")).toBeTruthy();
    expect(getByTestId("web-search-empty").textContent).toContain(
      "No providers configured"
    );
  });

  it("opens the Browser dialog from the row's gear", async () => {
    tools.push(
      {
        Name: "read_file",
        DisplayName: "Read File",
        IconKey: "file",
        Description: "",
        Enabled: true,
      },
      {
        Name: "browser",
        DisplayName: "Browser",
        IconKey: "globe",
        Description: "Automate Chromium",
        Enabled: true,
      }
    );
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <ToolsPane />
    );

    // Only browser and web_search carry the gear.
    expect(queryByTestId("btn-tool-config-read_file")).toBeNull();
    const gear = await findByTestId("btn-tool-config-browser");
    expect(gear.getAttribute("aria-label")).toBe("Configure Browser");

    await user.click(gear);

    await findByTestId("browser-config-dialog");
    expect(getByTestId("browser-profile-row-default").textContent).toContain(
      "default"
    );
  });

  it("opens the Document preset manager from the create_document row's gear", async () => {
    tools.push(
      {
        Name: "read_file",
        DisplayName: "Read File",
        IconKey: "file",
        Description: "",
        Enabled: true,
      },
      {
        Name: "create_document",
        DisplayName: "Create Document",
        IconKey: "file-text",
        Description: "Render markdown into a styled PDF",
        Enabled: true,
      }
    );
    documentPresets = [
      {
        id: "preset-1",
        name: "Client Report",
        format: "pdf",
        config: JSON.stringify({ schema_version: 1 }),
        hasTemplate: false,
        createdAt: "2026-10-01T12:00:00Z",
        updatedAt: "2026-10-01T12:00:00Z",
      },
    ];
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <ToolsPane />
    );

    // Only create_document carries the gear among this catalog's rows.
    expect(queryByTestId("btn-tool-config-read_file")).toBeNull();
    const gear = await findByTestId("btn-tool-config-create_document");
    expect(gear.getAttribute("aria-label")).toBe("Configure Create Document");

    await user.click(gear);

    await findByTestId("document-config-dialog");
    expect(getByTestId("document-tab-pdf").getAttribute("aria-selected")).toBe(
      "true"
    );
    expect(getByTestId("document-preset-row-Client Report")).toBeTruthy();

    // All three format tabs are live in the tabbed layout
    // (add-document-editing): PDF, DOCX, PPTX.
    expect(getByTestId("document-tab-docx").hasAttribute("disabled")).toBe(
      false
    );
    expect(getByTestId("document-tab-pptx").hasAttribute("disabled")).toBe(
      false
    );
  });

  it("adds providers with credentials and persists the ordered chain", async () => {
    tools.push({
      Name: "web_search",
      DisplayName: "Web Search",
      IconKey: "search",
      Description: "",
      Enabled: true,
    });
    const user = userEvent.setup();

    const { getByTestId, findByTestId, getAllByTestId } = await renderUI(
      <ToolsPane />
    );

    await user.click(await findByTestId("btn-tool-config-web_search"));
    await findByTestId("tool-config-dialog");

    await user.click(getByTestId("btn-web-search-add"));
    await user.click(getByTestId("btn-web-search-add"));

    const names = getAllByTestId(/^web-search-name-/);
    const selects = getAllByTestId(/^web-search-provider-/);

    await user.type(names[0], "Brave 1");
    await user.selectOptions(selects[0], "brave");
    await user.type(getByTestId(/^ws-cred-/), "brave-key");

    // The second row's credential line only renders once its provider is
    // chosen (SearXNG authenticates by Base URL).
    await user.selectOptions(selects[1], "searxng");
    const creds = getAllByTestId(/^ws-cred-/);
    await user.type(creds[1], "http://searx.local");

    // Rows render in add order with their index bubbles.
    const numbers = getAllByTestId("web-search-row-number");
    expect(numbers.map((n: HTMLElement) => n.textContent)).toEqual(["1", "2"]);

    await user.click(getByTestId("btn-tool-config-save"));

    expect(mockedClient.settings.saveWebSearchConfig).toHaveBeenCalledTimes(1);
    const payload = mockedClient.settings.saveWebSearchConfig.mock.calls[0][0];
    expect(payload.entries).toHaveLength(2);
    expect(payload.entries?.[0]).toMatchObject({
      id: "",
      name: "Brave 1",
      provider: "brave",
      api_key: "brave-key",
    });
    expect(payload.entries?.[1]).toMatchObject({
      id: "",
      provider: "searxng",
      base_url: "http://searx.local",
    });
  });

  it("reorders chain entries and persists the new order", async () => {
    tools.push({
      Name: "web_search",
      DisplayName: "Web Search",
      IconKey: "search",
      Description: "",
      Enabled: true,
    });
    chain = {
      entries: [
        { id: "entry-1", name: "Brave", provider: "brave", api_key: "k1" },
        { id: "entry-2", name: "Exa", provider: "exa", api_key: "k2" },
      ],
    };
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ToolsPane />);

    await user.click(await findByTestId("btn-tool-config-web_search"));
    await findByTestId("tool-config-dialog");

    // Loaded rows carry their stored secrets through edits (write-only round
    // trip: the value round-trips from the read).
    const moveButtons = getByTestId("tool-config-dialog").querySelectorAll(
      'button[aria-label="Move down"]'
    );

    // SAFETY: the dialog renders exactly one chain row with a Move down button.
    await user.click(moveButtons[0] as HTMLElement);

    await user.click(getByTestId("btn-tool-config-save"));

    const payload = mockedClient.settings.saveWebSearchConfig.mock.calls[0][0];
    expect(payload.entries?.map((e: WebSearchChainEntry) => e.id)).toEqual([
      "entry-2",
      "entry-1",
    ]);
  });

  it("removes a row with undo and persists the remaining chain", async () => {
    tools.push({
      Name: "web_search",
      DisplayName: "Web Search",
      IconKey: "search",
      Description: "",
      Enabled: true,
    });
    chain = {
      entries: [
        { id: "entry-1", name: "Brave", provider: "brave", api_key: "k1" },
        { id: "entry-2", name: "Exa", provider: "exa", api_key: "k2" },
      ],
    };
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId, getAllByTestId } =
      await renderUI(<ToolsPane />);

    await user.click(await findByTestId("btn-tool-config-web_search"));
    await findByTestId("tool-config-dialog");

    const removeButtons = getAllByTestId(/^btn-web-search-remove-/);

    await user.click(removeButtons[0]);

    // The undo bar names what went and restores it on click.
    expect(getByTestId("web-search-undo-bar").textContent).toContain(
      "Removed Brave"
    );
    await user.click(getByTestId("web-search-undo"));
    expect(queryByTestId("web-search-undo-bar")).toBeNull();

    // Remove again and save: one row remains.
    const removeAgain = getAllByTestId(/^btn-web-search-remove-/);
    await user.click(removeAgain[0]);
    await user.click(getByTestId("btn-tool-config-save"));

    const payload = mockedClient.settings.saveWebSearchConfig.mock.calls[0][0];
    expect(payload.entries).toHaveLength(1);
    expect(payload.entries?.[0]).toMatchObject({ id: "entry-2" });
  });

  it("rejects saving an api-key provider without its key", async () => {
    tools.push({
      Name: "web_search",
      DisplayName: "Web Search",
      IconKey: "search",
      Description: "",
      Enabled: true,
    });
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ToolsPane />);

    await user.click(await findByTestId("btn-tool-config-web_search"));
    await findByTestId("tool-config-dialog");

    await user.click(getByTestId("btn-web-search-add"));

    const selects = getByTestId("tool-config-dialog").querySelectorAll(
      'select[aria-label="Provider"]'
    );

    // SAFETY: the single added row renders exactly one provider select.
    await user.selectOptions(selects[0] as HTMLElement, "brave");

    await user.click(getByTestId("btn-tool-config-save"));

    expect(getByTestId("web-search-row-error").textContent).toContain(
      "Brave requires an API key"
    );
    expect(mockedClient.settings.saveWebSearchConfig).not.toHaveBeenCalled();
  });
});
