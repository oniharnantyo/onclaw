import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type {
  BrowserConfig,
  BrowserProfile,
  ChromeProfileCandidate,
  VaultLogin,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { MCPServer } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";

import BrowserUsePane from "../components/BrowserUsePane";
import type { SettingsPaneConfig } from "../types";
import { renderUI } from "./test-utils";

/** Header config passed to the pane; mirrors the static nav entry. */
const pane: SettingsPaneConfig = {
  key: "browseruse",
  label: "Browser Use",
  icon: "monitor",
  description: "The built-in Browser Use MCP server.",
};

/** The persisted browser config the mocked round trip reads and writes. */
let browserSettings: BrowserConfig = {
  defaultProfile: "default",
  headless: true,
  idleTimeoutSeconds: 120,
  actionTimeoutSeconds: 30,
  allowPrivate: false,
};

/** The profile fixtures, mirroring the service's seeded default. */
let browserProfiles: BrowserProfile[] = [
  { name: "default", modified_at: "", size_bytes: 0 },
];

/** The detected Chrome candidates the mocked detector returns. */
let chromeCandidates: ChromeProfileCandidate[] = [];

/** The MCP listing fixture: the synthesized system row rides it. */
const servers: MCPServer[] = [];

/** The redacted vault rows the mocked listing returns. */
let vaultLogins: VaultLogin[] = [];

/** The last saved config capture, for pass-through assertions. */
let savedConfig: BrowserConfig | null = null;

const createdProfiles: string[] = [];

const deletedProfiles: string[] = [];

const importedDirs: string[] = [];

const savedLogins: {
  name: string;
  origin: string;
  username: string;
  password: string;
  addedBy: string;
}[] = [];

const removedLogins: string[] = [];

const revealedLogins: string[] = [];

const revealedPasswords = new Map<string, string>([["login-1", "hunter2"]]);

vi.mock("../../../lib/client", () => ({
  client: {
    settings: {
      browserConfig: vi.fn(async (): Promise<BrowserConfig> => ({
        ...browserSettings,
      })),
      saveBrowserConfig: vi.fn(async (config: BrowserConfig) => {
        savedConfig = { ...config };
        browserSettings = { ...config };
      }),
      browserProfiles: vi.fn(async (): Promise<BrowserProfile[]> => [
        ...browserProfiles,
      ]),
      createBrowserProfile: vi.fn(async (name: string) => {
        createdProfiles.push(name);
        browserProfiles = [
          ...browserProfiles,
          { name, modified_at: "", size_bytes: 0 },
        ];
      }),
      deleteBrowserProfile: vi.fn(async (name: string) => {
        deletedProfiles.push(name);
        browserProfiles = browserProfiles.filter(
          (p: BrowserProfile) => p.name !== name
        );
      }),
      detectChromeProfiles: vi.fn(
        async (): Promise<ChromeProfileCandidate[]> => [...chromeCandidates]
      ),
      importChromeProfile: vi.fn(async (dirName: string): Promise<string> => {
        importedDirs.push(dirName);

        const imported: BrowserProfile = {
          name: `${dirName.toLowerCase()}-2`,
          modified_at: "",
          size_bytes: 42,
        };

        browserProfiles = [...browserProfiles, imported];

        return imported.name;
      }),
      vaultLogins: vi.fn(async (): Promise<VaultLogin[]> => [...vaultLogins]),
      saveVaultLogin: vi.fn(
        async (
          name: string,
          origin: string,
          username: string,
          password: string,
          addedBy: string
        ): Promise<void> => {
          savedLogins.push({ name, origin, username, password, addedBy });
          vaultLogins = [
            ...vaultLogins,
            {
              id: `login-${vaultLogins.length + 1}`,
              name,
              username,
              origin,
              added_by: addedBy,
            },
          ];
        }
      ),
      removeVaultLogin: vi.fn(async (id: string): Promise<void> => {
        removedLogins.push(id);
        vaultLogins = vaultLogins.filter((l: VaultLogin) => l.id !== id);
      }),
      revealVaultLogin: vi.fn(async (id: string): Promise<string> => {
        revealedLogins.push(id);

        return revealedPasswords.get(id) ?? "";
      }),
      mcpServers: vi.fn(async (): Promise<MCPServer[]> => [...servers]),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  settings: {
    browserConfig: MockFn;
    saveBrowserConfig: MockFn;
    browserProfiles: MockFn;
    createBrowserProfile: MockFn;
    deleteBrowserProfile: MockFn;
    detectChromeProfiles: MockFn;
    importChromeProfile: MockFn;
    vaultLogins: MockFn;
    saveVaultLogin: MockFn;
    removeVaultLogin: MockFn;
    revealVaultLogin: MockFn;
    mcpServers: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

/** The synthesized system row the backend listing carries (design D6). */
function makeSystemServer(patch: Partial<MCPServer> = {}): MCPServer {
  return {
    ID: "browser-use",
    Name: "Browser Use",
    Transport: "stdio",
    Command: "/opt/buddies/buddies",
    Args: ["browser-mcp"],
    Env: {},
    URL: "",
    Headers: {},
    Auth: {},
    Service: "",
    CredentialSet: false,
    CredentialHint: "",
    Enabled: true,
    Status: "connected",
    StatusError: "",
    ToolCount: 11,
    CreatedAt: "",
    UpdatedAt: "",
    ...patch,
  };
}

/** The spec's detection fixture: Personal (Default) and Work (Profile 1). */
function seedChromeCandidates(): void {
  chromeCandidates = [
    {
      dir_name: "Default",
      display_name: "Personal",
      is_default: true,
      size_bytes: 1024 * 1024 * 512,
    },
    {
      dir_name: "Profile 1",
      display_name: "Work",
      is_default: false,
      size_bytes: 1024 * 1024 * 64,
    },
  ];
}

describe("BrowserUsePane", () => {
  beforeEach(() => {
    browserSettings = {
      defaultProfile: "default",
      headless: true,
      idleTimeoutSeconds: 120,
      actionTimeoutSeconds: 30,
      allowPrivate: false,
    };
    browserProfiles = [{ name: "default", modified_at: "", size_bytes: 0 }];
    chromeCandidates = [];
    servers.length = 0;
    vaultLogins = [
      {
        id: "login-1",
        name: "Example",
        username: "ada",
        origin: "https://example.com",
        added_by: "user",
      },
    ];
    savedConfig = null;
    createdProfiles.length = 0;
    deletedProfiles.length = 0;
    importedDirs.length = 0;
    savedLogins.length = 0;
    removedLogins.length = 0;
    revealedLogins.length = 0;
    vi.clearAllMocks();
  });

  it("shows no engine controls — no headless, allow-private, timeouts, or engine save", async () => {
    servers.push(makeSystemServer());

    const { findByTestId, queryByTestId, queryByText } = await renderUI(
      <BrowserUsePane pane={pane} />
    );

    await findByTestId("browseruse-pane");

    expect(queryByTestId("browser-headless")).toBeNull();
    expect(queryByTestId("browser-allow-private")).toBeNull();
    expect(queryByTestId("browser-idle-timeout")).toBeNull();
    expect(queryByTestId("browser-action-timeout")).toBeNull();
    expect(queryByTestId("browser-config-save")).toBeNull();
    expect(queryByTestId("browser-default-profile")).toBeNull();
    expect(queryByText("Engine")).toBeNull();
  });

  it("badges the configured default and passes the other config fields through on Make default", async () => {
    servers.push(makeSystemServer());
    browserProfiles.push({ name: "work", modified_at: "", size_bytes: 12 });
    // Non-default stored engine values must survive the pass-through.
    browserSettings = {
      defaultProfile: "default",
      headless: false,
      idleTimeoutSeconds: 300,
      actionTimeoutSeconds: 45,
      allowPrivate: true,
    };

    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <BrowserUsePane pane={pane} />
    );

    await findByTestId("browser-profile-row-default");
    expect(getByTestId("browser-profile-row-work").textContent).toContain(
      "default"
    );
    // The badge sits on the configured default's row only.
    expect(getByTestId("browser-profile-default-badge-default")).toBeTruthy();
    expect(
      getByTestId("browser-profile-row-default").textContent
    ).not.toContain("Make default");

    await user.click(getByTestId("browser-profile-make-default-work"));

    expect(mockedClient.settings.saveBrowserConfig).toHaveBeenCalledTimes(1);
    expect(savedConfig).toEqual({
      defaultProfile: "work",
      headless: false,
      idleTimeoutSeconds: 300,
      actionTimeoutSeconds: 45,
      allowPrivate: true,
    });
    // The badge moves to the new default.
    expect(
      await findByTestId("browser-profile-default-badge-work")
    ).toBeTruthy();
  });

  it("creates a profile and deletes it; the built-in default refuses to delete", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <BrowserUsePane pane={pane} />
    );

    expect(await findByTestId("browser-profile-row-default")).toBeTruthy();

    await user.type(getByTestId("browser-new-profile-name"), "work");
    await user.click(getByTestId("browser-create-profile"));

    expect(mockedClient.settings.createBrowserProfile).toHaveBeenCalledWith(
      "work"
    );
    expect(await findByTestId("browser-profile-row-work")).toBeTruthy();

    const defaultDelete = getByTestId(
      "browser-profile-delete-default"
    ) as HTMLButtonElement;

    expect(defaultDelete.disabled).toBe(true);

    await user.click(getByTestId("browser-profile-delete-work"));
    await user.click(getByTestId("browser-profile-delete-work"));

    expect(mockedClient.settings.deleteBrowserProfile).toHaveBeenCalledWith(
      "work"
    );
    expect(queryByTestId("browser-profile-row-work")).toBeNull();
  });

  it("rejects an invalid profile name before the round trip", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <BrowserUsePane pane={pane} />
    );

    await findByTestId("browseruse-pane");

    await user.type(getByTestId("browser-new-profile-name"), "Bad Name!");
    await user.click(getByTestId("browser-create-profile"));

    expect(mockedClient.settings.createBrowserProfile).not.toHaveBeenCalled();
    expect(getByTestId("browser-new-profile-error").textContent).toContain(
      "lowercase"
    );
  });

  it("shows the server status line fed by the system row, failed state with the reason", async () => {
    servers.push(
      makeSystemServer({
        Status: "error",
        StatusError: "browser-use: lock held by pid 4321",
      })
    );

    const { findByTestId } = await renderUI(<BrowserUsePane pane={pane} />);

    expect(
      (await findByTestId("browser-status-dot")).getAttribute("data-status")
    ).toBe("error");
    expect((await findByTestId("browser-status")).textContent).toContain(
      "failed"
    );
    expect((await findByTestId("browser-status-reason")).textContent).toContain(
      "lock held by pid 4321"
    );
  });

  it("shows the three disclosures before any Import takes effect, then imports a detected profile per row and names the final registry name", async () => {
    seedChromeCandidates();
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <BrowserUsePane pane={pane} />
    );

    await findByTestId("browseruse-pane");
    expect(queryByTestId("browser-import-disclosures")).toBeNull();

    await user.click(getByTestId("browser-import-open"));

    // The disclosures precede the first per-row Import in markup order.
    const disclosures = getByTestId("browser-import-disclosures");

    expect(disclosures.textContent).toContain("Close the source");
    expect(disclosures.textContent).toContain("large payload");
    expect(disclosures.textContent).toContain("best-effort");
    expect(
      disclosures.compareDocumentPosition(
        getByTestId("browser-import-button-Profile 1")
      ) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();

    // The detected list renders Chrome's display names, dir names, sizes.
    const workRow = getByTestId("browser-import-row-Profile 1");

    expect(workRow.textContent).toContain("Work");
    expect(workRow.textContent).toContain("Profile 1");
    expect(workRow.textContent).toContain("64 MB");
    expect(getByTestId("browser-import-row-Default").textContent).toContain(
      "Personal"
    );

    // Import delegates to the dir name and shows the returned final name.
    await user.click(getByTestId("browser-import-button-Profile 1"));

    expect(mockedClient.settings.importChromeProfile).toHaveBeenCalledWith(
      "Profile 1"
    );
    expect(await findByTestId("browser-import-result")).toBeTruthy();
    expect(getByTestId("browser-import-result").textContent).toContain(
      "profile 1-2"
    );
    expect(await findByTestId("browser-profile-row-profile 1-2")).toBeTruthy();
    // No manual source-path input exists (spec: the pane SHALL NOT offer one).
    expect(queryByTestId("browser-import-source")).toBeNull();
  });

  it("explains that no Chrome profiles were found instead of erroring", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <BrowserUsePane pane={pane} />
    );

    await findByTestId("browseruse-pane");

    await user.click(getByTestId("browser-import-open"));

    expect((await findByTestId("browser-import-none")).textContent).toContain(
      "No Chrome profiles were found"
    );
    expect(mockedClient.settings.detectChromeProfiles).toHaveBeenCalled();
  });

  it("lists saved logins redacted, adds through saveVaultLogin with addedBy user, removes, and reveals only in the reveal area", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <BrowserUsePane pane={pane} />
    );

    const row = await findByTestId("browser-login-row-login-1");

    expect(row.textContent).toContain("ada");
    expect(row.textContent).toContain("https://example.com");
    expect(row.textContent).toContain("added by user");
    // The password never renders in the list row.
    expect(row.textContent).not.toContain("hunter2");

    // Reveal is explicit and per-row; the password shows only in the
    // reveal area — the redacted summary line never carries it.
    await user.click(getByTestId("browser-login-reveal-login-1"));

    expect(mockedClient.settings.revealVaultLogin).toHaveBeenCalledWith(
      "login-1"
    );
    const revealed = await findByTestId("browser-login-revealed-login-1");

    expect(revealed.textContent).toBe("hunter2");
    const rowAfter = getByTestId("browser-login-row-login-1");

    // The redacted name/username/origin lines stay free of the password.
    const summary =
      rowAfter.querySelector("p")?.nextElementSibling?.textContent;

    expect(summary).toBe("ada · https://example.com · added by user");
    expect(summary).not.toContain("hunter2");

    // Add form delegates with addedBy "user".
    await user.type(getByTestId("browser-login-origin"), "https://acme.dev");
    await user.type(getByTestId("browser-login-username"), "grace");
    await user.type(getByTestId("browser-login-password"), "s3cret");
    await user.click(getByTestId("browser-login-save"));

    expect(mockedClient.settings.saveVaultLogin).toHaveBeenCalledWith(
      "",
      "https://acme.dev",
      "grace",
      "s3cret",
      "user"
    );
    expect(await findByTestId("browser-login-row-login-2")).toBeTruthy();

    // Remove.
    await user.click(getByTestId("browser-login-remove-login-1"));

    expect(mockedClient.settings.removeVaultLogin).toHaveBeenCalledWith(
      "login-1"
    );
    expect(queryByTestId("browser-login-row-login-1")).toBeNull();
  });
});
