import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type {
  ComputerConfigView,
  ComputerPermission,
  ComputerStatus,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";

import ComputerUsePane from "../components/ComputerUsePane";
import type { SettingsPaneConfig } from "../types";
import { renderUI } from "./test-utils";

/** Header config passed to the pane; mirrors the static nav entry. */
const pane: SettingsPaneConfig = {
  key: "computeruse",
  label: "Computer Use",
  icon: "eye",
  description: "The built-in Computer Use MCP server.",
};

/** The persisted toggle the mocked round trip reads and writes. */
let computerSettings: ComputerConfigView = { enabled: false };

/** The status aggregate the mocked read returns. */
let statusRead: ComputerStatus;

/** The fresh status the next Check again returns; null keeps the base. */
let checkStatus: ComputerStatus | null = null;

/** The deep links the Manage clicks opened (the client opener records). */
const openedURLs: string[] = [];

/** The ungranted macOS pair the desktop probes report (design D6). */
function ungrantedPermissions(): ComputerStatus["permissions"] {
  return [
    {
      name: "Accessibility",
      granted: false,
      manage_url:
        "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
    },
    {
      name: "Screen Recording",
      granted: false,
      manage_url:
        "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
    },
  ];
}

/** The status aggregate fixture: desktop build, everything granted, ready —
 * tests patch toward the state under test. */
function makeStatus(patch: Partial<ComputerStatus> = {}): ComputerStatus {
  return {
    available: true,
    enabled: false,
    status: "off",
    permissions: [
      { name: "Accessibility", granted: true, manage_url: "" },
      { name: "Screen Recording", granted: true, manage_url: "" },
    ],
    driver: { state: "ready" },
    supervision: "connected",
    ...patch,
  };
}

vi.mock("../../../lib/client", () => ({
  client: {
    settings: {
      computerConfig: vi.fn(async (): Promise<ComputerConfigView> => ({
        ...computerSettings,
      })),
      saveComputerConfig: vi.fn(async (view: ComputerConfigView) => {
        computerSettings = { ...view };
        // Mirror the backend aggregate: the status read carries the toggle.
        statusRead = { ...statusRead, enabled: view.enabled };
      }),
      computerStatus: vi.fn(async (): Promise<ComputerStatus> => ({
        ...statusRead,
        permissions: statusRead.permissions
          ? statusRead.permissions.map((p: ComputerPermission) => ({ ...p }))
          : statusRead.permissions,
      })),
      // Mimic the backend Check again: a re-probe (whose grant change
      // restarts the driver backend-side) returning the fresh aggregate.
      checkComputerPermissions: vi.fn(async (): Promise<ComputerStatus> => {
        if (checkStatus) statusRead = checkStatus;

        return {
          ...statusRead,
          permissions: statusRead.permissions
            ? statusRead.permissions.map((p: ComputerPermission) => ({ ...p }))
            : statusRead.permissions,
        };
      }),
      promptComputerPermissions: vi.fn(async (): Promise<void> => undefined),
      // The Manage deep link rides the host opener through the bound
      // service (add-computer-use design D6); the mock records the URL.
      openComputerManageURL: vi.fn(async (url: string): Promise<void> => {
        openedURLs.push(url);
      }),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  settings: {
    computerConfig: MockFn;
    saveComputerConfig: MockFn;
    computerStatus: MockFn;
    checkComputerPermissions: MockFn;
    promptComputerPermissions: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

describe("ComputerUsePane", () => {
  beforeEach(() => {
    computerSettings = { enabled: false };
    statusRead = makeStatus();
    checkStatus = null;
    openedURLs.length = 0;
    vi.clearAllMocks();
  });

  it("fresh enable shows the permission path: ungranted rows with Manage actions, awaiting status, no daemon", async () => {
    computerSettings = { enabled: true };
    statusRead = makeStatus({
      enabled: true,
      status: "awaiting-permissions",
      permissions: ungrantedPermissions(),
      // Supervision stays unconnected — no daemon runs pre-grant (spec
      // computer-use "Permission warm-up gate": zero tools, no daemon).
      supervision: "unknown",
      driver: { state: "ready" },
    });

    const { findByTestId, getByTestId, queryAllByText } = await renderUI(
      <ComputerUsePane pane={pane} />
    );

    await findByTestId("computeruse-pane");

    // The toggle carries the persisted enabled state.
    const toggle = getByTestId("computer-toggle");

    expect(toggle.getAttribute("aria-checked")).toBe("true");

    // Both permissions list with their live ungranted state and Manage.
    expect(
      getByTestId("computer-permission-Accessibility").getAttribute(
        "data-granted"
      )
    ).toBe("false");
    expect(
      getByTestId("computer-permission-Screen Recording").getAttribute(
        "data-granted"
      )
    ).toBe("false");
    expect(
      getByTestId("computer-permission-manage-Accessibility")
    ).toBeTruthy();
    expect(
      getByTestId("computer-permission-manage-Screen Recording")
    ).toBeTruthy();
    expect(queryAllByText("not granted").length).toBe(2);

    // The status line reports awaiting permissions (warn dot), not ready.
    expect(getByTestId("computer-status").getAttribute("data-status")).toBe(
      "awaiting-permissions"
    );
    expect(getByTestId("computer-status-dot").getAttribute("data-status")).toBe(
      "warn"
    );
    expect(getByTestId("computer-status").textContent).toContain(
      "waiting for permissions"
    );
    expect(getByTestId("computer-status").textContent).not.toContain("ready");
    // Probing on load never raises a prompt (design D6).
    expect(
      mockedClient.settings.promptComputerPermissions
    ).not.toHaveBeenCalled();
  });

  it("enabling saves first, then raises the OS prompts host-side (add-computer-use design D6)", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <ComputerUsePane pane={pane} />
    );

    await findByTestId("computeruse-pane");

    await user.click(getByTestId("computer-toggle"));

    expect(mockedClient.settings.saveComputerConfig).toHaveBeenCalledTimes(1);
    expect(mockedClient.settings.saveComputerConfig).toHaveBeenCalledWith({
      enabled: true,
    });
    expect(
      mockedClient.settings.promptComputerPermissions
    ).toHaveBeenCalledTimes(1);
    // The toggle flips and the status read refreshes after the round trip.
    expect(getByTestId("computer-toggle").getAttribute("aria-checked")).toBe(
      "true"
    );
    expect(mockedClient.settings.computerStatus).toHaveBeenCalledTimes(2);
  });

  it("disabling saves enabled=false and raises no prompt", async () => {
    computerSettings = { enabled: true };
    statusRead = makeStatus({ enabled: true, status: "ready" });
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <ComputerUsePane pane={pane} />
    );

    await findByTestId("computeruse-pane");

    await user.click(getByTestId("computer-toggle"));

    expect(mockedClient.settings.saveComputerConfig).toHaveBeenCalledWith({
      enabled: false,
    });
    expect(
      mockedClient.settings.promptComputerPermissions
    ).not.toHaveBeenCalled();
  });

  it("Check again applies a grant: re-probes, reports granted, and advances toward ready", async () => {
    statusRead = makeStatus({
      enabled: true,
      status: "awaiting-permissions",
      permissions: ungrantedPermissions(),
      supervision: "unknown",
    });
    // The user granted everything in System Settings; Check again returns
    // the fresh aggregate (the backend restarted the driver on the change).
    checkStatus = makeStatus({
      enabled: true,
      status: "ready",
      permissions: [
        {
          name: "Accessibility",
          granted: true,
          manage_url:
            "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
        },
        {
          name: "Screen Recording",
          granted: true,
          manage_url:
            "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
        },
      ],
      supervision: "connected",
    });
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <ComputerUsePane pane={pane} />
    );

    expect(getByTestId("computer-status").getAttribute("data-status")).toBe(
      "awaiting-permissions"
    );

    await user.click(getByTestId("computer-check-again"));

    expect(
      mockedClient.settings.checkComputerPermissions
    ).toHaveBeenCalledTimes(1);
    expect(getByTestId("computer-status").getAttribute("data-status")).toBe(
      "ready"
    );
    expect(getByTestId("computer-status-dot").getAttribute("data-status")).toBe(
      "running"
    );
    expect(
      (await findByTestId("computer-permission-Accessibility")).getAttribute(
        "data-granted"
      )
    ).toBe("true");
  });

  it("ready state renders green with granted permissions", async () => {
    computerSettings = { enabled: true };
    statusRead = makeStatus({ enabled: true, status: "ready" });

    const { getByTestId, findByTestId, queryAllByText } = await renderUI(
      <ComputerUsePane pane={pane} />
    );

    await findByTestId("computeruse-pane");

    expect(getByTestId("computer-status").getAttribute("data-status")).toBe(
      "ready"
    );
    expect(getByTestId("computer-status-dot").getAttribute("data-status")).toBe(
      "running"
    );
    expect(getByTestId("computer-status").textContent).toContain("ready");
    expect(
      getByTestId("computer-permission-Accessibility").getAttribute(
        "data-granted"
      )
    ).toBe("true");
    expect(queryAllByText("not granted").length).toBe(0);
    expect(getByTestId("computer-driver").getAttribute("data-state")).toBe(
      "ready"
    );
  });

  it("a failed driver verification surfaces the integrity failure", async () => {
    computerSettings = { enabled: true };
    statusRead = makeStatus({
      enabled: true,
      status: "failed",
      status_detail: "digest mismatch for cua-driver 0.41.2",
      permissions: [],
      driver: {
        state: "failed",
        detail: "digest mismatch for cua-driver 0.41.2",
      },
    });

    const { findByTestId, getByTestId, queryByTestId } = await renderUI(
      <ComputerUsePane pane={pane} />
    );

    await findByTestId("computeruse-pane");

    expect(getByTestId("computer-status-dot").getAttribute("data-status")).toBe(
      "error"
    );
    expect(getByTestId("computer-status-reason").textContent).toContain(
      "digest mismatch"
    );
    expect(getByTestId("computer-driver").getAttribute("data-state")).toBe(
      "failed"
    );
    expect(getByTestId("computer-driver-error").textContent).toContain(
      "digest mismatch for cua-driver 0.41.2"
    );
    // No permission ceremony on this fixture — no card renders.
    expect(queryByTestId("computer-permissions")).toBeNull();
  });

  it("server mode presents the capability as unavailable with no working toggle (design D9)", async () => {
    statusRead = makeStatus({
      available: false,
      enabled: false,
      status: "unavailable",
      permissions: null,
      driver: { state: "pending" },
      supervision: "",
    });

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <ComputerUsePane pane={pane} />
    );

    await findByTestId("computeruse-pane");

    // The unavailable presentation replaces every control that cannot work.
    expect(getByTestId("computer-unavailable").textContent).toContain(
      "server mode"
    );
    expect(queryByTestId("computer-toggle")).toBeNull();
    expect(queryByTestId("computer-check-again")).toBeNull();
    expect(queryByTestId("computer-permissions")).toBeNull();
    expect(queryByTestId("computer-driver")).toBeNull();

    // The status line still names the state.
    expect(getByTestId("computer-status").getAttribute("data-status")).toBe(
      "unavailable"
    );
    expect(getByTestId("computer-status-dot").getAttribute("data-status")).toBe(
      "idle"
    );
  });

  it("Manage opens the permission's deep link into System Settings", async () => {
    statusRead = makeStatus({
      enabled: true,
      status: "awaiting-permissions",
      permissions: ungrantedPermissions(),
    });

    const { getByTestId, findByTestId } = await renderUI(
      <ComputerUsePane pane={pane} />
    );

    await findByTestId("computeruse-pane");

    getByTestId("computer-permission-manage-Accessibility").click();

    expect(openedURLs).toEqual([
      "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
    ]);
  });
});
