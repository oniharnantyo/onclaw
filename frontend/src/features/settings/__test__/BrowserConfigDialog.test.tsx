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
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";

import BrowserConfigDialog from "../components/BrowserConfigDialog";
import { renderUI, setInput } from "./test-utils";

/** The persisted browser config fixture the mocked config read returns. */
let config: BrowserConfig = {
  defaultProfile: "default",
  headless: true,
  idleTimeoutSeconds: 120,
  actionTimeoutSeconds: 30,
  allowPrivate: false,
};

/** The profile fixture the mocked listing returns, mirroring the service's
 * always-present default. */
let profiles: BrowserProfile[] = [
  { name: "default", modified_at: "", size_bytes: 0 },
];

vi.mock("../../../lib/client", () => ({
  client: {
    settings: {
      browserConfig: vi.fn(async () => config),
      saveBrowserConfig: vi.fn(async (next: BrowserConfig) => {
        config = next;
      }),
      browserProfiles: vi.fn(async () => [...profiles]),
      createBrowserProfile: vi.fn(async (name: string) => {
        profiles = [
          ...profiles,
          { name, modified_at: new Date().toISOString(), size_bytes: 0 },
        ].sort((a: BrowserProfile, b: BrowserProfile): number =>
          a.name.localeCompare(b.name)
        );
      }),
      deleteBrowserProfile: vi.fn(async (name: string) => {
        profiles = profiles.filter((p: BrowserProfile) => p.name !== name);
      }),
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
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

describe("BrowserConfigDialog", () => {
  beforeEach(() => {
    config = {
      defaultProfile: "default",
      headless: true,
      idleTimeoutSeconds: 120,
      actionTimeoutSeconds: 30,
      allowPrivate: false,
    };
    profiles = [{ name: "default", modified_at: "", size_bytes: 0 }];
    vi.clearAllMocks();
  });

  afterEach(() => {
    document.documentElement.style.overflow = "";
    document.body.style.overflow = "";
  });

  it("renders the loaded configuration values and profile rows", async () => {
    config = {
      defaultProfile: "work",
      headless: false,
      idleTimeoutSeconds: 300,
      actionTimeoutSeconds: 45,
      allowPrivate: true,
    };
    profiles = [
      { name: "default", modified_at: "", size_bytes: 0 },
      {
        name: "work",
        modified_at: "2026-10-01T12:00:00Z",
        size_bytes: 2048,
      },
    ];

    const { getByTestId, findByTestId } = await renderUI(
      <BrowserConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("browser-config-dialog");

    // SAFETY: the queries target this dialog's own rendered controls; the
    // select/input element types are what the pane renders for them.
    expect(
      (getByTestId("browser-default-profile") as HTMLSelectElement).value
    ).toBe("work");
    expect(getByTestId("browser-headless").getAttribute("aria-checked")).toBe(
      "false"
    );
    expect(
      getByTestId("browser-allow-private").getAttribute("aria-checked")
    ).toBe("true");
    // SAFETY: the query targets this dialog's timeout field, an
    // HTMLInputElement by construction.
    expect(
      (getByTestId("browser-idle-timeout") as HTMLInputElement).value
    ).toBe("300");
    // SAFETY: the query targets this dialog's timeout field, an
    // HTMLInputElement by construction.
    expect(
      (getByTestId("browser-action-timeout") as HTMLInputElement).value
    ).toBe("45");
    expect(getByTestId("browser-profile-row-default")).toBeTruthy();
    expect(getByTestId("browser-profile-row-work").textContent).toContain(
      "2 KB"
    );
  });

  it("saves the edited configuration", async () => {
    profiles = [
      { name: "default", modified_at: "", size_bytes: 0 },
      { name: "work", modified_at: "", size_bytes: 0 },
    ];
    const user = userEvent.setup();
    const onClose = vi.fn();

    const { getByTestId, findByTestId } = await renderUI(
      <BrowserConfigDialog onClose={onClose} />
    );

    await findByTestId("browser-config-dialog");

    await user.selectOptions(getByTestId("browser-default-profile"), "work");
    await user.click(getByTestId("browser-headless"));
    await setInput(getByTestId("browser-idle-timeout"), "300");
    await user.click(getByTestId("browser-config-save"));

    expect(mockedClient.settings.saveBrowserConfig).toHaveBeenCalledTimes(1);
    expect(mockedClient.settings.saveBrowserConfig).toHaveBeenCalledWith({
      defaultProfile: "work",
      headless: false,
      idleTimeoutSeconds: 300,
      actionTimeoutSeconds: 30,
      allowPrivate: false,
    });
    expect(onClose).toHaveBeenCalled();
  });

  it("disables delete for the default profile", async () => {
    const { getByTestId, findByTestId } = await renderUI(
      <BrowserConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("browser-config-dialog");

    const del = getByTestId("browser-profile-delete-default");
    expect(del.hasAttribute("disabled")).toBe(true);
    expect(del.getAttribute("title")).toContain("cannot be deleted");
    expect(mockedClient.settings.deleteBrowserProfile).not.toHaveBeenCalled();
  });

  it("deletes a profile after the inline confirm and refreshes", async () => {
    profiles = [
      { name: "default", modified_at: "", size_bytes: 0 },
      { name: "work", modified_at: "", size_bytes: 0 },
    ];
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <BrowserConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("browser-config-dialog");

    // First click arms the inline confirm; the second deletes.
    await user.click(getByTestId("browser-profile-delete-work"));
    await user.click(getByTestId("browser-profile-delete-work"));

    expect(mockedClient.settings.deleteBrowserProfile).toHaveBeenCalledWith(
      "work"
    );
    expect(queryByTestId("browser-profile-row-work")).toBeNull();
    expect(getByTestId("browser-profile-row-default")).toBeTruthy();
  });

  it("rejects an invalid profile name without calling the backend", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <BrowserConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("browser-config-dialog");

    await user.type(getByTestId("browser-new-profile-name"), "Bad Name");
    await user.click(getByTestId("browser-create-profile"));

    expect(getByTestId("browser-new-profile-error").textContent).toContain(
      "lowercase letters"
    );
    expect(mockedClient.settings.createBrowserProfile).not.toHaveBeenCalled();
  });

  it("creates a profile and refreshes the list", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <BrowserConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("browser-config-dialog");

    await user.type(getByTestId("browser-new-profile-name"), "work2");
    await user.click(getByTestId("browser-create-profile"));

    expect(mockedClient.settings.createBrowserProfile).toHaveBeenCalledWith(
      "work2"
    );
    expect(getByTestId("browser-profile-row-work2")).toBeTruthy();
    // SAFETY: the query targets the dialog's new-profile field, an
    // HTMLInputElement by construction.
    expect(
      (getByTestId("browser-new-profile-name") as HTMLInputElement).value
    ).toBe("");
  });
});
