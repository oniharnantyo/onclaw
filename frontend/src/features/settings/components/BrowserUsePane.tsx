import { useEffect, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import type { MCPServer } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  BrowserConfig,
  BrowserProfile,
  ChromeProfileCandidate,
  VaultLogin,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { LoadingHint } from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Chip } from "../../../components/ui/Chip";
import { Icon } from "../../../components/ui/Icon";
import { Input } from "../../../components/ui/Input";
import { StatusDot } from "../../../components/ui/StatusDot";
import type { StatusDotStatus } from "../../../components/ui/StatusDot";
import { client } from "../../../lib/client";
import type { SettingsPaneConfig } from "../types";

/** The system server id the backend synthesizes (design D6); its row in the
 * MCP listing feeds the pane's status line. */
const SYSTEM_SERVER_ID = "browser-use";

/** The profile name the backend seeds and refuses to delete. */
const DEFAULT_PROFILE = "default";

/** The profile name rule the backend validates (spec browser-use
 * "Persistent profiles"); validated here so typos fail before the round
 * trip. */
const PROFILE_NAME_RE = /^[a-z0-9][a-z0-9_-]{0,31}$/;

/** Connection status vocabulary persisted by the backend probe; shared with
 * McpPane's reading of the same column. */
const STATUS_CONNECTED = "connected";

const STATUS_ERROR = "error";

/** The stored status as the kit dot's vocabulary: green when connected, red
 * when failed, neutral otherwise. */
function dotStatus(status: string): StatusDotStatus {
  if (status === STATUS_CONNECTED) return "running";

  if (status === STATUS_ERROR) return "error";

  return "idle";
}

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

/** The system server's row from the MCP listing; null when absent. The
 * synthesized System row rides the same shape as user rows (backend task 3:
 * MCPServerView with the system flag), so the listing reads verbatim. */
function systemServer(servers: MCPServer[]): MCPServer | null {
  return (
    servers.find(
      (s: MCPServer) =>
        s.ID === SYSTEM_SERVER_ID ||
        (s as MCPServer & { System?: boolean }).System === true
    ) ?? null
  );
}

/**
 * BrowserUsePane: the Browser Use system server's settings page (spec
 * settings "Browser Use pane", design D9) — a profiles-first surface: a
 * status line fed by the system row's live connection state, the profile
 * registry with create/delete and a make-default pass-through, the Saved
 * logins vault section, and the detected-Chrome import whose three
 * disclosures show before any import starts. Engine knobs (headless,
 * timeouts, allow-private) are not exposed — the stored config document is
 * only ever written by the make-default pass-through, and it applies at the
 * server's next spawn boundary (design D8); profile changes apply
 * immediately.
 */
export default function BrowserUsePane(_props: {
  pane: SettingsPaneConfig;
}): ReactElement {
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [profiles, setProfiles] = useState<BrowserProfile[]>([]);
  const [defaultProfile, setDefaultProfile] = useState(DEFAULT_PROFILE);

  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [statusLoading, setStatusLoading] = useState(true);

  const [generalError, setGeneralError] = useState<string | null>(null);

  const [newName, setNewName] = useState("");
  const [newNameError, setNewNameError] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState<string | null>(null);
  const [makingDefault, setMakingDefault] = useState<string | null>(null);

  // Import flow state: closed → detected list (with disclosures) → per-row
  // import → result. Chrome absent is a hint, not an error.
  const [importOpen, setImportOpen] = useState(false);
  const [candidates, setCandidates] = useState<ChromeProfileCandidate[]>([]);
  const [detectLoading, setDetectLoading] = useState(false);
  const [detectError, setDetectError] = useState<string | null>(null);
  const [importingDir, setImportingDir] = useState<string | null>(null);
  const [importResult, setImportResult] = useState<string | null>(null);
  const [importError, setImportError] = useState<string | null>(null);

  // Saved logins state: redacted rows, an add form, and a per-row reveal.
  const [logins, setLogins] = useState<VaultLogin[]>([]);
  const [loginName, setLoginName] = useState("");
  const [loginOrigin, setLoginOrigin] = useState("");
  const [loginUsername, setLoginUsername] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [loginFormError, setLoginFormError] = useState<string | null>(null);
  const [savingLogin, setSavingLogin] = useState(false);
  const [revealedId, setRevealedId] = useState<string | null>(null);
  const [revealedPassword, setRevealedPassword] = useState("");

  useEffect(() => {
    const load = async (): Promise<void> => {
      try {
        const [config, list, serverRows, loginRows]: [
          BrowserConfig,
          BrowserProfile[],
          MCPServer[],
          VaultLogin[],
        ] = await Promise.all([
          client.settings.browserConfig(),
          client.settings.browserProfiles(),
          client.settings.mcpServers(),
          client.settings.vaultLogins(),
        ]);

        setProfiles(list);
        setDefaultProfile(config.defaultProfile || DEFAULT_PROFILE);
        setServers(serverRows);
        setLogins(loginRows);
      } catch (err) {
        setLoadError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
        setStatusLoading(false);
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

  /** Re-reads the redacted login list after an add or a remove. */
  const refreshLogins = async (): Promise<void> => {
    setLogins(await client.settings.vaultLogins());
  };

  /** The make-default pass-through (design D9): read the stored config,
   * swap only defaultProfile, save everything else unchanged. The backend
   * applies it at the server's next spawn boundary. */
  const makeDefault = async (name: string): Promise<void> => {
    setGeneralError(null);
    setMakingDefault(name);

    try {
      const config = await client.settings.browserConfig();

      await client.settings.saveBrowserConfig({
        ...config,
        defaultProfile: name,
      });

      setDefaultProfile(name);
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    } finally {
      setMakingDefault(null);
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
      // Deletion coordinates the profile browser's close, so it may take a
      // moment; the row list refreshes once it lands.
      await client.settings.deleteBrowserProfile(name);
      await refreshProfiles();
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    }
  };

  /** Loads this machine's detected Chrome profiles for the import list;
   * Chrome absent resolves to an empty list — a hint, not an error. */
  const detectProfiles = async (): Promise<void> => {
    setDetectError(null);
    setDetectLoading(true);

    try {
      setCandidates(await client.settings.detectChromeProfiles());
    } catch (err) {
      setDetectError(err instanceof Error ? err.message : String(err));
    } finally {
      setDetectLoading(false);
    }
  };

  const openImport = async (): Promise<void> => {
    setImportOpen(true);
    setImportResult(null);
    setImportError(null);
    await detectProfiles();
  };

  const importCandidate = async (dirName: string): Promise<void> => {
    setImportError(null);
    setImportResult(null);
    setImportingDir(dirName);

    try {
      // The backend copies the profile and returns its final registry name
      // — a collision is not an error; it appends a suffix.
      const finalName = await client.settings.importChromeProfile(dirName);

      setImportResult(finalName);
      await refreshProfiles();
    } catch (err) {
      setImportError(err instanceof Error ? err.message : String(err));
    } finally {
      setImportingDir(null);
    }
  };

  const saveLogin = async (): Promise<void> => {
    setGeneralError(null);
    setLoginFormError(null);

    const origin = loginOrigin.trim();
    const username = loginUsername.trim();
    const password = loginPassword;

    if (origin === "" || username === "" || password === "") {
      setLoginFormError("Origin, username, and password are required.");

      return;
    }

    setSavingLogin(true);

    try {
      // Rows created here record "user" as who handed the secret over;
      // agent-created rows carry the agent's name instead.
      await client.settings.saveVaultLogin(
        loginName.trim(),
        origin,
        username,
        password,
        "user"
      );

      setLoginName("");
      setLoginOrigin("");
      setLoginUsername("");
      setLoginPassword("");
      await refreshLogins();
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    } finally {
      setSavingLogin(false);
    }
  };

  const removeLogin = async (id: string): Promise<void> => {
    setGeneralError(null);

    if (revealedId === id) {
      setRevealedId(null);
      setRevealedPassword("");
    }

    try {
      await client.settings.removeVaultLogin(id);
      await refreshLogins();
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    }
  };

  const toggleReveal = async (id: string): Promise<void> => {
    setGeneralError(null);

    if (revealedId === id) {
      setRevealedId(null);
      setRevealedPassword("");

      return;
    }

    try {
      const password = await client.settings.revealVaultLogin(id);

      setRevealedId(id);
      setRevealedPassword(password);
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    }
  };

  const status = systemServer(servers);

  return (
    <div data-testid="browseruse-pane">
      {loading ? (
        <LoadingHint testId="browser-loading" />
      ) : loadError ? (
        <div role="alert" className="mb-4">
          <Alert tone="danger" testId="browser-load-error">
            {loadError}
          </Alert>
        </div>
      ) : (
        <div className="space-y-6">
          <p className="text-[12px] text-muted">
            Browser Use is a built-in MCP server — agents automate pages with
            Chromium through the mcp__browser-use__ tools, and profiles keep
            logins and cookies between sessions.
          </p>

          {/* Server status line, fed by the system row's live probe state. */}
          <div
            className="flex items-center gap-2"
            data-testid="browser-status"
            data-status={status ? status.Status : "absent"}
          >
            {statusLoading ? (
              <span className="text-[12px] text-muted">
                Checking server state…
              </span>
            ) : status ? (
              <>
                <span
                  className="inline-flex"
                  data-testid="browser-status-dot"
                  data-status={dotStatus(status.Status)}
                >
                  <StatusDot status={dotStatus(status.Status)} />
                </span>
                {status.Status === STATUS_CONNECTED ? (
                  <Chip className="border-[color-mix(in_oklab,var(--success)_35%,transparent)] bg-[color-mix(in_oklab,var(--success)_8%,transparent)]">
                    connected · {status.ToolCount}{" "}
                    {status.ToolCount === 1 ? "tool" : "tools"}
                  </Chip>
                ) : status.Status === STATUS_ERROR ? (
                  <Chip className="border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] text-dangertext">
                    failed
                  </Chip>
                ) : (
                  <Chip>{status.Status || "unknown"}</Chip>
                )}
                {status.StatusError ? (
                  <span
                    className="truncate text-xs text-dangertext"
                    title={status.StatusError}
                    data-testid="browser-status-reason"
                  >
                    {status.StatusError}
                  </span>
                ) : null}
              </>
            ) : (
              <span className="text-[12px] text-muted">
                Server status unavailable.
              </span>
            )}
          </div>

          {generalError ? (
            <div role="alert">
              <Alert tone="danger" testId="browser-error">
                {generalError}
              </Alert>
            </div>
          ) : null}

          {/* Profile registry: the default carries a badge plus a Make
              default pass-through on the other rows (design D9). */}
          <section className="space-y-3" data-testid="browser-profiles">
            <h3 className="text-[13px] font-semibold text-fg">Profiles</h3>
            <p className="text-[11px] leading-4 text-muted">
              Each profile is its own Chromium user-data directory under
              ~/.buddies/browser/profiles.
            </p>

            <div className="space-y-2">
              {profiles.map((profile: BrowserProfile) => {
                const isDefault = profile.name === defaultProfile;
                const confirming = confirmingDelete === profile.name;
                const isBuiltIn = profile.name === DEFAULT_PROFILE;

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
                          <span
                            className="ml-2 font-sans text-[11px] text-muted"
                            data-testid={`browser-profile-default-badge-${profile.name}`}
                          >
                            default
                          </span>
                        ) : null}
                        {isBuiltIn && !isDefault ? (
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
                    {!isDefault ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        loading={makingDefault === profile.name}
                        title="Applies at the server's next spawn."
                        aria-label={`Make ${profile.name} the default profile`}
                        data-testid={`browser-profile-make-default-${profile.name}`}
                        onClick={() => void makeDefault(profile.name)}
                      >
                        Make default
                      </Button>
                    ) : null}
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
                        disabled={isBuiltIn}
                        title={
                          isBuiltIn
                            ? "The default profile cannot be deleted"
                            : undefined
                        }
                        aria-label={
                          isBuiltIn
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
          </section>

          {/* Import from Chrome (design D9): the three disclosures render
              before any Import row action, so they are read before any copy
              begins; the manual source-path input is gone. */}
          <section className="space-y-3" data-testid="browser-import">
            <div className="flex items-center justify-between">
              <h3 className="text-[13px] font-semibold text-fg">
                Import from Chrome
              </h3>
              <Button
                variant="ghost"
                size="sm"
                data-testid="browser-import-open"
                onClick={() => void openImport()}
              >
                <Icon name="plus" size={13} />
                {importOpen ? "Refresh" : "Detect Chrome profiles"}
              </Button>
            </div>

            {importOpen ? (
              <div className="space-y-3 rounded-md border border-line p-3">
                <ul
                  className="list-disc space-y-1 pl-4 text-[11px] leading-4 text-muted"
                  data-testid="browser-import-disclosures"
                >
                  <li>
                    Close the source Chrome/Chromium first — its lockfile fights
                    the copy while the browser is running.
                  </li>
                  <li>
                    Long-lived profiles carry a large payload; the copy can take
                    a while and use significant disk.
                  </li>
                  <li>
                    Cookie decryption is best-effort: it works when the copy
                    stays on the same machine and browser (macOS Keychain,
                    Linux), and may not survive cross-machine copies or newer
                    Windows protections. Session cookies the jar captured still
                    carry those logins.
                  </li>
                </ul>

                {detectLoading ? (
                  <p
                    className="text-[12px] text-muted"
                    data-testid="browser-import-detecting"
                    role="status"
                  >
                    Detecting Chrome profiles…
                  </p>
                ) : detectError ? (
                  <div role="alert">
                    <Alert tone="danger" testId="browser-import-detect-error">
                      {detectError}
                    </Alert>
                  </div>
                ) : candidates.length === 0 ? (
                  <p
                    className="text-[12px] text-muted"
                    data-testid="browser-import-none"
                  >
                    No Chrome profiles were found. Install Google Chrome and
                    sign in once, then refresh — profiles from Chromium can
                    still be imported manually by creating a profile here.
                  </p>
                ) : (
                  <div className="space-y-2">
                    {candidates.map((candidate: ChromeProfileCandidate) => (
                      <div
                        key={candidate.dir_name}
                        className="flex items-center gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_2%,var(--surface))] px-3 py-2"
                        data-testid={`browser-import-row-${candidate.dir_name}`}
                      >
                        <div className="min-w-0 flex-1">
                          <p className="truncate text-[13px] text-fg">
                            {candidate.display_name}
                            {candidate.is_default ? (
                              <span className="ml-2 text-[11px] text-muted">
                                Chrome default
                              </span>
                            ) : null}
                          </p>
                          <p className="truncate font-mono text-[11px] text-muted">
                            {candidate.dir_name} ·{" "}
                            {humanSize(candidate.size_bytes)}
                          </p>
                        </div>
                        <Button
                          size="sm"
                          loading={importingDir === candidate.dir_name}
                          disabled={importingDir !== null}
                          aria-label={`Import Chrome profile ${candidate.display_name}`}
                          data-testid={`browser-import-button-${candidate.dir_name}`}
                          onClick={() =>
                            void importCandidate(candidate.dir_name)
                          }
                        >
                          Import
                        </Button>
                      </div>
                    ))}
                  </div>
                )}

                {importingDir ? (
                  <p
                    className="text-[12px] text-muted"
                    data-testid="browser-import-progress"
                    role="status"
                  >
                    Copying profile — this can take a while for large profiles…
                  </p>
                ) : null}

                {importResult ? (
                  <p
                    className="text-[12px] text-fg2"
                    data-testid="browser-import-result"
                    role="status"
                  >
                    Imported as{" "}
                    <span className="font-mono">{importResult}</span>
                  </p>
                ) : null}

                {importError ? (
                  <div role="alert">
                    <Alert tone="danger" testId="browser-import-error">
                      {importError}
                    </Alert>
                  </div>
                ) : null}
              </div>
            ) : null}
          </section>

          {/* Saved logins (design D5): redacted rows only — a password is
              shown solely through the explicit per-row reveal affordance,
              never in the list itself. */}
          <section className="space-y-3" data-testid="browser-logins">
            <h3 className="text-[13px] font-semibold text-fg">Saved logins</h3>
            <p className="text-[11px] leading-4 text-muted">
              Passwords are sealed in the OS vault; agents fill them without
              ever seeing them.
            </p>

            <div className="space-y-2">
              {logins.length === 0 ? (
                <p
                  className="text-[12px] text-muted"
                  data-testid="browser-logins-empty"
                >
                  No saved logins yet.
                </p>
              ) : (
                logins.map((login: VaultLogin) => (
                  <div
                    key={login.id}
                    className="flex items-center gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_2%,var(--surface))] px-3 py-2"
                    data-testid={`browser-login-row-${login.id}`}
                  >
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-[13px] text-fg">
                        {login.name || login.username}
                      </p>
                      <p className="truncate text-[11px] text-muted">
                        {login.username} · {login.origin} · added by{" "}
                        {login.added_by}
                      </p>
                    </div>
                    {revealedId === login.id ? (
                      <span
                        className="font-mono text-[12px] text-fg2"
                        data-testid={`browser-login-revealed-${login.id}`}
                      >
                        {revealedPassword}
                      </span>
                    ) : null}
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={
                        revealedId === login.id
                          ? "Hide password"
                          : `Reveal password for ${login.name || login.username}`
                      }
                      data-testid={`browser-login-reveal-${login.id}`}
                      onClick={() => void toggleReveal(login.id)}
                    >
                      {revealedId === login.id ? "Hide" : "Reveal"}
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={`Remove saved login ${login.name || login.username}`}
                      data-testid={`browser-login-remove-${login.id}`}
                      onClick={() => void removeLogin(login.id)}
                    >
                      <Icon name="x" size={12} /> Remove
                    </Button>
                  </div>
                ))
              )}
            </div>

            <div className="space-y-2 rounded-md border border-line p-3">
              <p className="text-[12px] font-medium text-fg2">Save a login</p>
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  placeholder="Name (optional)"
                  aria-label="Login name"
                  className="h-8 w-40 text-[13px]"
                  data-testid="browser-login-name"
                  value={loginName}
                  onChange={(e: ChangeEvent<HTMLInputElement>) =>
                    setLoginName(e.target.value)
                  }
                />
                <Input
                  placeholder="https://example.com"
                  aria-label="Origin"
                  className="h-8 w-56 text-[13px]"
                  data-testid="browser-login-origin"
                  value={loginOrigin}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => {
                    setLoginOrigin(e.target.value);

                    if (loginFormError) setLoginFormError(null);
                  }}
                />
                <Input
                  placeholder="Username"
                  aria-label="Username"
                  className="h-8 w-40 text-[13px]"
                  data-testid="browser-login-username"
                  value={loginUsername}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => {
                    setLoginUsername(e.target.value);

                    if (loginFormError) setLoginFormError(null);
                  }}
                />
                <Input
                  type="password"
                  placeholder="Password"
                  aria-label="Password"
                  autoComplete="off"
                  className="h-8 w-40 text-[13px]"
                  data-testid="browser-login-password"
                  value={loginPassword}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => {
                    setLoginPassword(e.target.value);

                    if (loginFormError) setLoginFormError(null);
                  }}
                />
                <Button
                  size="sm"
                  loading={savingLogin}
                  data-testid="browser-login-save"
                  onClick={() => void saveLogin()}
                >
                  Save login
                </Button>
              </div>
              {loginFormError ? (
                <p
                  className="text-[12px] text-dangertext"
                  data-testid="browser-login-form-error"
                >
                  {loginFormError}
                </p>
              ) : null}
            </div>
          </section>
        </div>
      )}
    </div>
  );
}
