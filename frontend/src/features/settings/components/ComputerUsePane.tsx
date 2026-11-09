import { useEffect, useState } from "react";
import type { ReactElement } from "react";

import type {
  ComputerConfigView,
  ComputerPermission,
  ComputerStatus,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { LoadingHint } from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Chip } from "../../../components/ui/Chip";
import { StatusDot } from "../../../components/ui/StatusDot";
import type { StatusDotStatus } from "../../../components/ui/StatusDot";
import { Toggle } from "../../../components/ui/Toggle";
import { client } from "../../../lib/client";
import type { SettingsPaneConfig } from "../types";

/** The status-line vocabulary the backend resolves into ComputerStatus
 * (add-computer-use task 3.1's ComputerStatus* constants; the pane renders
 * them verbatim and maps each onto the kit dot and a tinted chip — off,
 * starting, downloading, and unavailable ride the neutral chip verbatim). */
const STATUS_STARTING = "starting";

const STATUS_READY = "ready";

const STATUS_AWAITING_PERMISSIONS = "awaiting-permissions";

const STATUS_DOWNLOADING = "downloading";

const STATUS_FAILED = "failed";

/** The driver acquisition vocabulary the PinStatus hook reports
 * (add-computer-use task 3.1's ComputerDriver* constants). */
const DRIVER_PENDING = "pending";

const DRIVER_DOWNLOADING = "downloading";

const DRIVER_READY = "ready";

const DRIVER_FAILED = "failed";

/** The stored status as the kit dot's vocabulary: green when ready, red
 * when failed, amber while a grant or download is outstanding, neutral
 * otherwise. */
function dotStatus(status: string): StatusDotStatus {
  if (status === STATUS_READY) return "running";

  if (status === STATUS_FAILED) return "error";

  if (
    status === STATUS_AWAITING_PERMISSIONS ||
    status === STATUS_DOWNLOADING ||
    status === STATUS_STARTING
  ) {
    return "warn";
  }

  return "idle";
}

/** Whether the webview reports Linux — gates the pane's platform
 * limitation note (add-computer-use design D9: X11 is the default path,
 * Wayland sessions are opt-in). */
const IS_LINUX = /Linux/i.test(navigator.userAgent);

/** Whether a permission row carries a Manage deep link; the backend leaves
 * the URL empty on platforms with no system settings pane to open. */
function hasManageURL(permission: ComputerPermission): boolean {
  return permission.manage_url !== "";
}

/**
 * ComputerUsePane: the Computer Use system server's settings page (spec
 * settings "Computer Use pane", add-computer-use design D8) — an enable
 * toggle whose action also raises the OS permission prompts host-side
 * (design D6: the driver never prompts), the system-permissions card with
 * each permission's live granted state, a Manage deep link into the
 * matching system settings pane, and a Check again action that re-probes
 * and applies a grant change (the backend restarts the driver when one
 * occurred, since permission answers are cached per daemon process); the
 * one-time driver download state with its verification outcome; and a
 * status line fed by the live supervision aggregate. In server mode the
 * whole lane answers available=false and the pane presents the capability
 * as unavailable instead of offering toggles that cannot work (design D9,
 * spec computer-use "Desktop-only availability"). Both themes ride the
 * contract tokens, mirroring BrowserUsePane.
 */
export default function ComputerUsePane(_props: {
  pane: SettingsPaneConfig;
}): ReactElement {
  const [enabled, setEnabled] = useState(false);
  const [status, setStatus] = useState<ComputerStatus | null>(null);

  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [generalError, setGeneralError] = useState<string | null>(null);

  // The toggle and the Check again action hold their own in-flight state so
  // the controls collapse while their round trip runs.
  const [toggling, setToggling] = useState(false);
  const [checking, setChecking] = useState(false);

  useEffect(() => {
    const load = async (): Promise<void> => {
      try {
        const [config, statusRead]: [ComputerConfigView, ComputerStatus] =
          await Promise.all([
            client.settings.computerConfig(),
            client.settings.computerStatus(),
          ]);

        setEnabled(config.enabled);
        setStatus(statusRead);
      } catch (err) {
        setLoadError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    };

    void load();
  }, []);

  /** The enable toggle (spec computer-use "Configuration and spawn
   * boundaries"): the save persists immediately and is the boundary the
   * backend's hooks fire on — enable kicks the ensure path (driver
   * download; the daemon itself stays down until every permission is
   * granted), disable stops the daemon and adapter. Enabling also raises
   * the OS permission prompts host-side (add-computer-use design D6) — the
   * pane's enable action is the only path that prompts; a failed raise
   * does not undo the stored choice, the status line reports the state
   * supervision converges on. */
  const toggleEnabled = async (next: boolean): Promise<void> => {
    setGeneralError(null);
    setToggling(true);

    try {
      await client.settings.saveComputerConfig({ enabled: next });
      setEnabled(next);

      if (next) await client.settings.promptComputerPermissions();

      setStatus(await client.settings.computerStatus());
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    } finally {
      setToggling(false);
    }
  };

  /** Check again (spec settings "Computer Use pane"): the backend re-probes
   * and applies a grant change — restarting the driver when one occurred —
   * and returns the fresh aggregate the pane swaps in. */
  const checkAgain = async (): Promise<void> => {
    setGeneralError(null);
    setChecking(true);

    try {
      setStatus(await client.settings.checkComputerPermissions());
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    } finally {
      setChecking(false);
    }
  };

  /** Manage opens the permission's deep link into the matching system
   * settings pane (the URL comes from the backend's permission probe;
   * rows without one render no action). The open rides the host's URL
   * opener through the bound service — the webview cannot open custom
   * schemes via window.open, and the service allowlists only the URLs it
   * issued itself. */
  const openManage = async (permission: ComputerPermission): Promise<void> => {
    setGeneralError(null);
    try {
      await client.settings.openComputerManageURL(permission.manage_url);
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    }
  };

  const available = status?.available === true;
  const permissions = status?.permissions ?? [];

  return (
    <div data-testid="computeruse-pane">
      {loading ? (
        <LoadingHint testId="computer-loading" />
      ) : loadError ? (
        <div role="alert" className="mb-4">
          <Alert tone="danger" testId="computer-load-error">
            {loadError}
          </Alert>
        </div>
      ) : (
        <div className="space-y-6">
          <p className="text-[12px] text-muted">
            Computer Use is a built-in MCP server — agents observe and operate
            native applications through the mcp__computer-use__ tools. The tools
            stay hidden and no daemon runs until every OS permission is granted.
          </p>

          {/* Server mode (design D9): the lane is absent — present the
              capability as unavailable, with no toggle that cannot work. */}
          {!available ? (
            <div
              className="rounded-md border border-line p-3"
              data-testid="computer-unavailable"
            >
              <p className="text-[13px] font-medium text-fg2">
                Unavailable in server mode.
              </p>
              <p className="mt-1 text-[12px] text-muted">
                Computer Use drives this machine's desktop, so it needs a
                desktop session. The app is running in server mode — start the
                desktop app to use it.
              </p>
            </div>
          ) : null}

          {/* Server status line, fed by the live supervision aggregate. */}
          <div
            className="flex items-center gap-2"
            data-testid="computer-status"
            data-status={status ? status.status : "absent"}
          >
            {status ? (
              <>
                <span
                  className="inline-flex"
                  data-testid="computer-status-dot"
                  data-status={dotStatus(status.status)}
                >
                  <StatusDot status={dotStatus(status.status)} />
                </span>
                {status.status === STATUS_READY ? (
                  <Chip className="border-[color-mix(in_oklab,var(--success)_35%,transparent)] bg-[color-mix(in_oklab,var(--success)_8%,transparent)]">
                    ready
                  </Chip>
                ) : status.status === STATUS_FAILED ? (
                  <Chip className="border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] text-dangertext">
                    failed
                  </Chip>
                ) : status.status === STATUS_AWAITING_PERMISSIONS ? (
                  <Chip className="border-[color-mix(in_oklab,var(--warn)_35%,transparent)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)]">
                    waiting for permissions
                  </Chip>
                ) : (
                  <Chip>{status.status || "unknown"}</Chip>
                )}
                {status.status_detail ? (
                  <span
                    className="truncate text-xs text-dangertext"
                    title={status.status_detail}
                    data-testid="computer-status-reason"
                  >
                    {status.status_detail}
                  </span>
                ) : null}
              </>
            ) : (
              <span className="text-[12px] text-muted">
                Server status unavailable.
              </span>
            )}
          </div>

          {/* Platform limitation note (add-computer-use design D9): Linux
              drives X11 by default; Wayland sessions are opt-in. */}
          {IS_LINUX && available ? (
            <p
              className="text-[11px] leading-4 text-muted"
              data-testid="computer-linux-note"
            >
              On Linux the driver uses X11 by default; running under Wayland
              requires opting in.
            </p>
          ) : null}

          {generalError ? (
            <div role="alert">
              <Alert tone="danger" testId="computer-error">
                {generalError}
              </Alert>
            </div>
          ) : null}

          {available ? (
            <>
              {/* Enable toggle: the save is the spawn boundary (spec
                  computer-use "Configuration and spawn boundaries"); the
                  enable action also raises the OS prompts host-side
                  (add-computer-use design D6). */}
              <section
                className="flex items-center justify-between gap-4 rounded-md border border-line p-3"
                data-testid="computer-enable"
              >
                <div className="min-w-0">
                  <p className="text-[13px] font-semibold text-fg">
                    Enable Computer Use
                  </p>
                  <p className="mt-0.5 text-[11px] leading-4 text-muted">
                    The vendored driver downloads once, digest-verified, on
                    first enable; disabling stops the daemon and adapter.
                  </p>
                </div>
                <Toggle
                  on={enabled}
                  label={
                    enabled ? "Disable Computer Use" : "Enable Computer Use"
                  }
                  disabled={toggling}
                  testId="computer-toggle"
                  onChange={(next: boolean) => void toggleEnabled(next)}
                />
              </section>

              {/* System permissions card (spec settings "Computer Use
                  pane"): each permission's live granted state, a Manage deep
                  link, and one Check again for the whole card. Probing never
                  raises a prompt — the prompts belong to the enable action.
                  Platforms with no permission ceremony probe an empty list
                  and skip the card. */}
              {permissions.length > 0 ? (
                <section
                  className="space-y-3"
                  data-testid="computer-permissions"
                >
                  <div className="flex items-center justify-between">
                    <h3 className="text-[13px] font-semibold text-fg">
                      System permissions
                    </h3>
                    <Button
                      variant="ghost"
                      size="sm"
                      loading={checking}
                      data-testid="computer-check-again"
                      onClick={() => void checkAgain()}
                    >
                      Check again
                    </Button>
                  </div>
                  <p className="text-[11px] leading-4 text-muted">
                    The daemon runs only when every permission is granted; after
                    granting one in System Settings, press Check again.
                  </p>

                  <div className="space-y-2">
                    {permissions.map((permission: ComputerPermission) => (
                      <div
                        key={permission.name}
                        className="flex items-center gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_2%,var(--surface))] px-3 py-2"
                        data-testid={`computer-permission-${permission.name}`}
                        data-granted={permission.granted ? "true" : "false"}
                      >
                        <div className="min-w-0 flex-1">
                          <p className="truncate text-[13px] text-fg">
                            {permission.name}
                          </p>
                        </div>
                        {permission.granted ? (
                          <Chip className="border-[color-mix(in_oklab,var(--success)_35%,transparent)] bg-[color-mix(in_oklab,var(--success)_8%,transparent)]">
                            granted
                          </Chip>
                        ) : (
                          <Chip className="border-[color-mix(in_oklab,var(--warn)_35%,transparent)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)]">
                            not granted
                          </Chip>
                        )}
                        {hasManageURL(permission) ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            aria-label={`Manage ${permission.name} in System Settings`}
                            data-testid={`computer-permission-manage-${permission.name}`}
                            onClick={() => openManage(permission)}
                          >
                            Manage
                          </Button>
                        ) : null}
                      </div>
                    ))}
                  </div>
                </section>
              ) : null}

              {/* One-time driver download state with its verification
                  outcome (spec settings "Computer Use pane"; spec
                  computer-use "Driver acquisition and integrity": a failed
                  digest check refuses to run and shows here). */}
              {status?.driver ? (
                <section
                  className="space-y-2"
                  data-testid="computer-driver"
                  data-state={status.driver.state}
                >
                  <h3 className="text-[13px] font-semibold text-fg">Driver</h3>
                  {status.driver.state === DRIVER_READY ? (
                    <p
                      className="text-[12px] text-fg2"
                      data-testid="computer-driver-state"
                      role="status"
                    >
                      Downloaded and digest-verified.
                    </p>
                  ) : status.driver.state === DRIVER_DOWNLOADING ? (
                    <p
                      className="text-[12px] text-muted"
                      data-testid="computer-driver-state"
                      role="status"
                    >
                      Downloading the pinned driver…
                    </p>
                  ) : status.driver.state === DRIVER_FAILED ? (
                    <div role="alert">
                      <Alert tone="danger" testId="computer-driver-error">
                        {status.driver.detail ||
                          "The driver failed verification and will not run."}
                      </Alert>
                    </div>
                  ) : status.driver.state === DRIVER_PENDING ? (
                    <p
                      className="text-[12px] text-muted"
                      data-testid="computer-driver-state"
                    >
                      Not downloaded yet — it downloads on first enable.
                    </p>
                  ) : (
                    <p
                      className="text-[12px] text-muted"
                      data-testid="computer-driver-state"
                    >
                      {status.driver.state || "unknown"}
                    </p>
                  )}
                </section>
              ) : null}
            </>
          ) : null}
        </div>
      )}
    </div>
  );
}
