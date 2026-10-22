import { useEffect, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import type {
  BrowserConfig,
  BrowserProfile,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { ModalShell } from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Icon } from "../../../components/ui/Icon";
import { Input } from "../../../components/ui/Input";
import { Select } from "../../../components/ui/Select";
import { Toggle } from "../../../components/ui/Toggle";
import { client } from "../../../lib/client";

/** The profile name the backend seeds and refuses to delete. */
const DEFAULT_PROFILE = "default";

/** The backend's profile name rule (spec add-browser-tool "Persistent
 * browser profiles"); validated here so typos fail before the round trip. */
const PROFILE_NAME_RE = /^[a-z0-9][a-z0-9_-]{0,31}$/;

/** Backend save-time bounds (settings_browser.go). */
const IDLE_TIMEOUT_MAX = 3600;

const ACTION_TIMEOUT_MAX = 600;

/** Human size for a profile's on-disk files. */
function humanSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;

  const units = ["KB", "MB", "GB"];
  let value = bytes;
  let unit = -1;

  do {
    value /= 1024;
    unit += 1;
  } while (value >= 1024 && unit < units.length - 1);

  const text = value.toFixed(value >= 10 ? 0 : 1);

  return `${text.endsWith(".0") ? text.slice(0, -2) : text} ${units[unit]}`;
}

/** Relative age of a profile directory; Go's zero time (the seeded default
 * profile has no directory yet) reads as an em dash. */
function modifiedLabel(iso: string): string {
  const ms = new Date(iso).getTime();

  if (!Number.isFinite(ms) || ms < 0) return "—";

  const minutes = Math.floor((Date.now() - ms) / 60000);

  if (minutes < 1) return "just now";

  if (minutes < 60) return `${minutes}m ago`;

  const hours = Math.floor(minutes / 60);

  if (hours < 24) return `${hours}h ago`;

  const days = Math.floor(hours / 24);

  if (days < 30) return `${days}d ago`;

  return new Date(iso).toLocaleDateString();
}

/**
 * BrowserConfigDialog: the browser tool's settings and profile manager
 * (spec add-browser-tool "Browser settings and profile management", D9) —
 * the runtime knobs (default profile, headless, private-network guard, the
 * two timeouts) plus the profile registry with create and per-row delete.
 * Nothing persists until Save configuration; profile changes apply to the
 * backend immediately.
 */
export default function BrowserConfigDialog({
  onClose,
}: {
  onClose: () => void;
}): ReactElement {
  const [profiles, setProfiles] = useState<BrowserProfile[]>([]);
  const [defaultProfile, setDefaultProfile] = useState(DEFAULT_PROFILE);
  const [headless, setHeadless] = useState(true);
  const [allowPrivate, setAllowPrivate] = useState(false);
  const [idleText, setIdleText] = useState("120");
  const [actionText, setActionText] = useState("30");

  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [idleError, setIdleError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [generalError, setGeneralError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const [newName, setNewName] = useState("");
  const [newNameError, setNewNameError] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState<string | null>(null);

  useEffect(() => {
    const load = async (): Promise<void> => {
      try {
        const [config, list]: [BrowserConfig, BrowserProfile[]] =
          await Promise.all([
            client.settings.browserConfig(),
            client.settings.browserProfiles(),
          ]);

        setProfiles(list);
        setDefaultProfile(config.defaultProfile || DEFAULT_PROFILE);
        setHeadless(config.headless);
        setAllowPrivate(config.allowPrivate);
        setIdleText(String(config.idleTimeoutSeconds));
        setActionText(String(config.actionTimeoutSeconds));
      } catch (err) {
        setLoadError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    };

    void load();
  }, []);

  /** Re-reads profiles — and the config, whose default profile the backend
   * resets when it was the deleted one. */
  const refreshProfiles = async (): Promise<void> => {
    const [config, list]: [BrowserConfig, BrowserProfile[]] = await Promise.all(
      [client.settings.browserConfig(), client.settings.browserProfiles()]
    );

    setProfiles(list);
    setDefaultProfile(config.defaultProfile || DEFAULT_PROFILE);
  };

  const save = async (): Promise<void> => {
    setGeneralError(null);

    const idle = Number(idleText);
    const action = Number(actionText);

    const badIdle =
      idleText.trim() === "" ||
      !Number.isFinite(idle) ||
      idle < 1 ||
      idle > IDLE_TIMEOUT_MAX;

    const badAction =
      actionText.trim() === "" ||
      !Number.isFinite(action) ||
      action < 1 ||
      action > ACTION_TIMEOUT_MAX;

    setIdleError(
      badIdle
        ? `Idle timeout must be between 1 and ${IDLE_TIMEOUT_MAX} seconds.`
        : null
    );
    setActionError(
      badAction
        ? `Action timeout must be between 1 and ${ACTION_TIMEOUT_MAX} seconds.`
        : null
    );

    if (badIdle || badAction) return;

    setSaving(true);

    try {
      await client.settings.saveBrowserConfig({
        defaultProfile,
        headless,
        idleTimeoutSeconds: idle,
        actionTimeoutSeconds: action,
        allowPrivate,
      });

      onClose();
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const createProfile = async (): Promise<void> => {
    setGeneralError(null);

    const name = newName.trim();

    if (!PROFILE_NAME_RE.test(name)) {
      setNewNameError(
        "Use lowercase letters, digits, hyphens, or underscores (up to 32 characters, starting with a letter or digit)."
      );

      return;
    }

    setNewNameError(null);

    try {
      await client.settings.createBrowserProfile(name);
      setNewName("");
      await refreshProfiles();
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    }
  };

  const removeProfile = async (name: string): Promise<void> => {
    setGeneralError(null);
    setConfirmingDelete(null);

    try {
      await client.settings.deleteBrowserProfile(name);
      await refreshProfiles();
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <ModalShell
      title="Browser configuration"
      testId="browser-config-dialog"
      onClose={onClose}
      actions={
        <>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            size="sm"
            loading={saving}
            data-testid="browser-config-save"
            onClick={() => void save()}
          >
            Save configuration
          </Button>
        </>
      }
    >
      {loading ? (
        <p className="text-[13px] text-muted">Loading…</p>
      ) : loadError ? (
        <div role="alert" className="mb-3">
          <Alert tone="danger" testId="browser-load-error">
            {loadError}
          </Alert>
        </div>
      ) : (
        <div className="space-y-4">
          <p className="text-[12px] text-muted">
            Automate pages with Chromium — profiles keep logins and cookies
            between sessions.
          </p>

          {generalError ? (
            <div role="alert">
              <Alert tone="danger" testId="browser-error">
                {generalError}
              </Alert>
            </div>
          ) : null}

          <div>
            <label
              className="mb-1.5 block text-[12px] font-medium text-fg2"
              htmlFor="browser-default-profile"
            >
              Default profile
            </label>
            <Select
              id="browser-default-profile"
              data-testid="browser-default-profile"
              className="h-8 w-48 text-[13px]"
              value={defaultProfile}
              onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                setDefaultProfile(e.target.value)
              }
            >
              {profiles.map((profile: BrowserProfile) => (
                <option key={profile.name} value={profile.name}>
                  {profile.name}
                </option>
              ))}
            </Select>
            <p className="mt-1 text-[11px] leading-4 text-muted">
              Actions that pass no profile run here.
            </p>
          </div>

          <div className="space-y-2.5">
            <label className="flex items-center gap-2 text-[12px] font-medium text-fg2">
              <Toggle
                on={headless}
                label="Headless"
                testId="browser-headless"
                onChange={setHeadless}
              />
              Headless (no window)
            </label>
            <label className="flex items-center gap-2 text-[12px] font-medium text-fg2">
              <Toggle
                on={allowPrivate}
                label="Allow private network"
                testId="browser-allow-private"
                onChange={setAllowPrivate}
              />
              Allow private network
            </label>
            <p className="text-[11px] leading-4 text-muted">
              Lets the agent open localhost and LAN addresses.
            </p>
          </div>

          <div className="flex gap-4">
            <div data-testid="browser-idle-timeout-field">
              <label
                className="mb-1.5 block text-[12px] font-medium text-fg2"
                htmlFor="browser-idle-timeout"
              >
                Idle timeout
              </label>
              <div className="relative w-24">
                <Input
                  id="browser-idle-timeout"
                  type="number"
                  min={1}
                  max={IDLE_TIMEOUT_MAX}
                  invalid={idleError !== null}
                  data-testid="browser-idle-timeout"
                  value={idleText}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => {
                    setIdleText(e.target.value);

                    if (idleError) setIdleError(null);
                  }}
                />
                <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[12px] text-muted">
                  s
                </span>
              </div>
              {idleError ? (
                <p
                  className="mt-1 text-[12px] text-dangertext"
                  data-testid="browser-idle-timeout-error"
                >
                  {idleError}
                </p>
              ) : null}
              <p className="mt-1 text-[11px] leading-4 text-muted">
                Closes an idle browser.
              </p>
            </div>
            <div data-testid="browser-action-timeout-field">
              <label
                className="mb-1.5 block text-[12px] font-medium text-fg2"
                htmlFor="browser-action-timeout"
              >
                Action timeout
              </label>
              <div className="relative w-24">
                <Input
                  id="browser-action-timeout"
                  type="number"
                  min={1}
                  max={ACTION_TIMEOUT_MAX}
                  invalid={actionError !== null}
                  data-testid="browser-action-timeout"
                  value={actionText}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => {
                    setActionText(e.target.value);

                    if (actionError) setActionError(null);
                  }}
                />
                <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[12px] text-muted">
                  s
                </span>
              </div>
              {actionError ? (
                <p
                  className="mt-1 text-[12px] text-dangertext"
                  data-testid="browser-action-timeout-error"
                >
                  {actionError}
                </p>
              ) : null}
              <p className="mt-1 text-[11px] leading-4 text-muted">
                Bounds each action.
              </p>
            </div>
          </div>

          <div>
            <div className="flex items-center justify-between">
              <span className="text-[12px] font-medium text-fg2">Profiles</span>
            </div>
            <p className="mt-1 text-[11px] leading-4 text-muted">
              Each profile is its own Chromium user-data directory under
              ~/.buddies/browser/profiles.
            </p>
          </div>

          <div className="space-y-2">
            {profiles.map((profile: BrowserProfile) => {
              const isDefault = profile.name === DEFAULT_PROFILE;
              const confirming = confirmingDelete === profile.name;

              return (
                <div
                  key={profile.name}
                  className="flex items-center gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_2%,var(--surface))] px-3 py-2"
                  data-testid={`browser-profile-row-${profile.name}`}
                >
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-mono text-[13px] text-fg">
                      {profile.name}
                      {isDefault ? (
                        <span className="ml-2 font-sans text-[11px] text-muted">
                          built-in
                        </span>
                      ) : null}
                    </p>
                    <p className="truncate text-[11px] text-muted">
                      {humanSize(profile.size_bytes)} ·{" "}
                      {modifiedLabel(profile.modified_at)}
                    </p>
                  </div>
                  {confirming ? (
                    <>
                      <span className="text-[11px] text-muted">
                        Delete this profile?
                      </span>
                      <Button
                        variant="destructive"
                        size="sm"
                        data-testid={`browser-profile-delete-${profile.name}`}
                        onClick={() => void removeProfile(profile.name)}
                      >
                        Delete
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        aria-label="Cancel delete"
                        onClick={() => setConfirmingDelete(null)}
                      >
                        Cancel
                      </Button>
                    </>
                  ) : (
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={isDefault}
                      title={
                        isDefault
                          ? "The default profile cannot be deleted"
                          : undefined
                      }
                      aria-label={
                        isDefault
                          ? "The default profile cannot be deleted"
                          : `Delete profile ${profile.name}`
                      }
                      data-testid={`browser-profile-delete-${profile.name}`}
                      onClick={() => setConfirmingDelete(profile.name)}
                    >
                      <Icon name="x" size={12} /> Delete
                    </Button>
                  )}
                </div>
              );
            })}
          </div>

          <div>
            <label
              className="mb-1.5 block text-[12px] font-medium text-fg2"
              htmlFor="browser-new-profile-name"
            >
              New profile
            </label>
            <div className="flex items-center gap-2">
              <Input
                id="browser-new-profile-name"
                placeholder="e.g. work"
                invalid={newNameError !== null}
                className="h-8 max-w-52 text-[13px]"
                data-testid="browser-new-profile-name"
                value={newName}
                onChange={(e: ChangeEvent<HTMLInputElement>) => {
                  setNewName(e.target.value);

                  if (newNameError) setNewNameError(null);
                }}
              />
              <Button
                variant="ghost"
                size="sm"
                data-testid="browser-create-profile"
                onClick={() => void createProfile()}
              >
                <Icon name="plus" size={13} /> Create
              </Button>
            </div>
            {newNameError ? (
              <p
                className="mt-1 text-[12px] text-dangertext"
                data-testid="browser-new-profile-error"
              >
                {newNameError}
              </p>
            ) : null}
          </div>
        </div>
      )}
    </ModalShell>
  );
}
