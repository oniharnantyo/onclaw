import { useEffect, useState } from "react";
import type { ReactElement } from "react";

import type { MCPServer } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import {
  ConfirmDialog,
  EmptyState,
  FormField,
  FormGrid,
  IconButton,
  LoadingHint,
  ModalShell,
  PaneHeader,
  SettingsList,
  SettingsRow,
} from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Chip } from "../../../components/ui/Chip";
import { Icon } from "../../../components/ui/Icon";
import { Input } from "../../../components/ui/Input";
import { Select } from "../../../components/ui/Select";
import { StatusDot } from "../../../components/ui/StatusDot";
import type { StatusDotStatus } from "../../../components/ui/StatusDot";
import { Textarea } from "../../../components/ui/Textarea";
import { Toggle } from "../../../components/ui/Toggle";
import { client } from "../../../lib/client";
import type { MCPOAuthCompletedEvent } from "../../../lib/events";
import type { McpDialogState, SettingsPaneConfig } from "../types";

function emptyServer(): MCPServer {
  return {
    ID: "",
    Name: "",
    Transport: "stdio",
    Command: "",
    Args: [],
    Env: {},
    URL: "",
    Headers: {},
    Auth: {},
    Service: "",
    CredentialSet: false,
    CredentialHint: "",
    Enabled: true,
    Status: "",
    StatusError: "",
    ToolCount: 0,
    CreatedAt: "",
    UpdatedAt: "",
  };
}

/** Editor-friendly "KEY=VALUE per line" text for a string map. */
function mapToText(map: Record<string, string | undefined> | null): string {
  if (!map) return "";

  return (
    Object.entries(map)
      .map(([k, v]: [string, string | undefined]) => `${k}=${v ?? ""}`)
      .join("\n")
      // join produces no leading newline; keep blank lines out
      .trim()
  );
}

function textToMap(text: string): Record<string, string> {
  const entries: [string, string][] = [];

  for (const line of text.split("\n")) {
    const trimmed = line.trim();

    if (!trimmed) continue;
    const eq = trimmed.indexOf("=");

    if (eq <= 0) continue;
    entries.push([trimmed.slice(0, eq), trimmed.slice(eq + 1)]);
  }

  return Object.fromEntries(entries);
}

/** Warn-tinted chip for a row that is switched off (kit Chip + warn tint). */
function DisabledChip(): ReactElement {
  return (
    <Chip className="border-[color-mix(in_oklab,var(--warn)_35%,transparent)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)]">
      disabled
    </Chip>
  );
}

/** Connection status vocabulary persisted by the backend probe (the store's
 * mcp_servers.status column; anything else is the un-probed default).
 * auth_required is the OAuth row state (design D5): the definition dialed but
 * user authorization is the missing piece. */
const STATUS_CONNECTED = "connected";

const STATUS_ERROR = "error";

const STATUS_AUTH_REQUIRED = "auth_required";

/** The stored status as the kit dot's vocabulary (task 4.1): green when
 * connected, red when failed, amber when authorization is required, neutral
 * otherwise. */
function dotStatus(status: string): StatusDotStatus {
  if (status === STATUS_CONNECTED) return "running";

  if (status === STATUS_ERROR) return "error";

  if (status === STATUS_AUTH_REQUIRED) return "warn";

  return "idle";
}

/** The stored probe outcome as a tinted chip (task 2.4): connected shows the
 * discovered-tool count on a success tint, the failure state shows on a
 * danger tint, authorization-required shows on the warn tint (task 4.1),
 * and anything else renders verbatim — or "unknown" when un-probed — on the
 * neutral kit chip. Tints ride the contract tokens (color-mix over
 * --success/--danger/--warn), so both themes re-skin them. */
function StatusChip({ server }: { server: MCPServer }): ReactElement {
  if (server.Status === STATUS_CONNECTED) {
    const noun = server.ToolCount === 1 ? "tool" : "tools";

    return (
      <Chip className="border-[color-mix(in_oklab,var(--success)_35%,transparent)] bg-[color-mix(in_oklab,var(--success)_8%,transparent)]">
        connected · {server.ToolCount} {noun}
      </Chip>
    );
  }

  if (server.Status === STATUS_ERROR) {
    return (
      <Chip className="border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] text-dangertext">
        failed
      </Chip>
    );
  }

  if (server.Status === STATUS_AUTH_REQUIRED) {
    return (
      <Chip className="border-[color-mix(in_oklab,var(--warn)_35%,transparent)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)]">
        authorization required
      </Chip>
    );
  }

  return <Chip>{server.Status || "unknown"}</Chip>;
}

/** McpPane: add/edit/remove MCP server definitions with enabled state and
 * live connection state (task 2.4, spec agent-mcp "Probe and tool
 * discovery"). Rows render the stored probe outcome on load; the per-row
 * Refresh affordance re-dials the stored definition (invalidate-then-dial,
 * design D6) and updates the row with connected + tool count or failed +
 * reason — the definition itself is never touched. Refresh all fans the same
 * re-dial over every enabled server. Each row's toggle enables/disables
 * in place (design D7): off drops the live connection immediately, on is
 * followed by a refresh so the dot turns green without waiting for an agent
 * turn. Authorization-required rows lead with Authorize (design D2): it
 * starts the out-of-band browser flow and returns immediately, the row is
 * unaffected until the backend emits mcp:oauth:completed — which refetches
 * the already re-probed row — and OAuth-connected rows offer Disconnect to
 * revoke the tokens and return to authorization-required (design D9). The
 * action cluster sits at the row's right edge as icon buttons with semantic
 * color: Authorize is the one labeled, accent-filled call to action,
 * Disconnect is warn-tinted, Remove is danger-tinted, Refresh and Edit stay
 * neutral; the confirm dialog still owns the destructive fill. */
export default function McpPane({
  pane,
}: {
  pane: SettingsPaneConfig;
}): ReactElement {
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [dialog, setDialog] = useState<McpDialogState | null>(null);
  const [saving, setSaving] = useState(false);
  const [removing, setRemoving] = useState<MCPServer | null>(null);

  const [refreshing, setRefreshing] = useState<Set<string>>(
    () => new Set<string>()
  );

  // Servers with an authorization flow in flight (design D2): the id stays
  // in the set from the Authorize click until the mcp:oauth:completed event
  // (or a rejection) clears it, so the row button holds its loading state.
  const [authorizing, setAuthorizing] = useState<Set<string>>(
    () => new Set<string>()
  );

  const load = async (): Promise<void> => {
    setLoading(true);
    setLoadError(null);

    try {
      setServers(await client.settings.mcpServers());
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  // OAuth completion (design D2): the backend emits mcp:oauth:completed once
  // the browser flow ends — after re-probing the row — so the fetched row
  // carries the fresh state; a failed flow also surfaces its reason in the
  // shared alert. The returned unsubscribe tears the subscription down on
  // unmount.
  useEffect(() => {
    const onCompleted = async (ev: MCPOAuthCompletedEvent): Promise<void> => {
      setAuthorizing((prev: Set<string>) => {
        const next = new Set(prev);

        next.delete(ev.serverId);

        return next;
      });

      if (!ev.ok) setLoadError(ev.error || "Authorization failed");

      try {
        const fresh = await client.settings.mcpServer(ev.serverId);
        setServers((prev: MCPServer[]) =>
          prev.map((s: MCPServer) => (s.ID === fresh.ID ? fresh : s))
        );
      } catch (err) {
        setLoadError(err instanceof Error ? err.message : String(err));
      }
    };

    return client.events.onMcpOAuthCompleted(
      (ev: MCPOAuthCompletedEvent): void => void onCompleted(ev)
    );
  }, []);

  const canSave =
    dialog !== null &&
    dialog.draft.Name.trim() !== "" &&
    (dialog.draft.Transport === "stdio"
      ? dialog.draft.Command.trim() !== ""
      : dialog.draft.URL.trim() !== "");

  const save = async (): Promise<void> => {
    if (!dialog) return;
    setSaving(true);

    const server: MCPServer = {
      ...dialog.draft,
      Args: dialog.argsText
        .split("\n")
        .map((a: string) => a.trim())
        .filter(Boolean),
      Env: textToMap(dialog.envText),
      Headers: textToMap(dialog.headersText),
    };

    try {
      if (dialog.mode === "add") {
        await client.settings.createMCPServer(server);
      } else {
        await client.settings.updateMCPServer(server);
      }

      setDialog(null);
      await load();
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const confirmRemove = async (): Promise<void> => {
    if (!removing) return;
    await client.settings.deleteMCPServer(removing.ID);
    setRemoving(null);
    await load();
  };

  // Row refresh: re-dials the saved definition on demand (the backend
  // invalidates its cached connection first, design D6) and swaps the row for
  // the persisted outcome (connected + tool count, or failed + reason)
  // without touching any other row. In-flight rows live in the `refreshing`
  // set so the row button, its toggle, and Refresh all can all read it.
  const refresh = async (server: MCPServer): Promise<void> => {
    setRefreshing((prev: Set<string>) => new Set(prev).add(server.ID));

    try {
      const refreshed = await client.settings.probeMCPServer(server.ID);
      setServers((prev: MCPServer[]) =>
        prev.map((s: MCPServer) => (s.ID === refreshed.ID ? refreshed : s))
      );
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setRefreshing((prev: Set<string>) => {
        const next = new Set(prev);

        next.delete(server.ID);

        return next;
      });
    }
  };

  // Row toggle (design D7): off writes enabled=false — the backend's update
  // invalidation drops the live connection immediately; on writes
  // enabled=true and then refreshes the row so its dot turns green without
  // waiting for an agent turn. The dialog's Enabled toggle is separate.
  const toggleEnabled = async (server: MCPServer): Promise<void> => {
    setRefreshing((prev: Set<string>) => new Set(prev).add(server.ID));

    try {
      const updated = await client.settings.updateMCPServer({
        ...server,
        Enabled: !server.Enabled,
      });

      setServers((prev: MCPServer[]) =>
        prev.map((s: MCPServer) => (s.ID === updated.ID ? updated : s))
      );

      if (updated.Enabled) await refresh(updated);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setRefreshing((prev: Set<string>) => {
        const next = new Set(prev);

        next.delete(server.ID);

        return next;
      });
    }
  };

  // Refresh all (design D6): fans the row refresh over every enabled server
  // concurrently; each row updates as its own outcome lands, and a failure
  // surfaces through the shared load-error path from its own row.
  const refreshAll = async (): Promise<void> => {
    await Promise.allSettled(
      servers
        .filter((s: MCPServer) => s.Enabled)
        .map((s: MCPServer) => refresh(s))
    );
  };

  // Row authorize (design D2): starts the out-of-band browser flow and
  // returns immediately — the backend keeps the row's stored state until the
  // flow completes, so the id stays in `authorizing` (button loading) until
  // the completion event clears it; a rejection surfaces its message and
  // releases the button.
  const authorize = async (server: MCPServer): Promise<void> => {
    setAuthorizing((prev: Set<string>) => new Set(prev).add(server.ID));

    try {
      await client.settings.authorizeMCPServer(server.ID);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
      setAuthorizing((prev: Set<string>) => {
        const next = new Set(prev);

        next.delete(server.ID);

        return next;
      });
    }
  };

  // Row disconnect (design D9): revokes and clears the stored tokens and
  // swaps the row for the returned authorization-required state. Reversible
  // — Authorize again — so no confirm dialog.
  const disconnect = async (server: MCPServer): Promise<void> => {
    try {
      const row = await client.settings.disconnectMCPServer(server.ID);
      setServers((prev: MCPServer[]) =>
        prev.map((s: MCPServer) => (s.ID === row.ID ? row : s))
      );
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    }
  };

  const openDialog = (server: MCPServer): void => {
    setDialog({
      mode: server.ID ? "edit" : "add",
      draft: { ...server },
      argsText: (server.Args ?? []).join("\n"),
      envText: mapToText(server.Env),
      headersText: mapToText(server.Headers),
    });
  };

  const updateDraft = (patch: Partial<MCPServer>): void => {
    if (!dialog) return;
    setDialog({ ...dialog, draft: { ...dialog.draft, ...patch } });
  };

  const isStdio = dialog?.draft.Transport === "stdio";

  const openAdd = (): void => openDialog(emptyServer());

  return (
    <div data-testid="mcp-pane">
      <PaneHeader
        description={pane.description}
        actions={
          <>
            <Button
              variant="ghost"
              size="sm"
              data-testid="mcp-refresh-all"
              disabled={
                refreshing.size > 0 ||
                !servers.some((s: MCPServer) => s.Enabled)
              }
              onClick={() => void refreshAll()}
            >
              Refresh all
            </Button>
            <Button size="sm" data-testid="mcp-add" onClick={openAdd}>
              Add MCP server
            </Button>
          </>
        }
      />

      {loading ? (
        <LoadingHint testId="mcp-loading" />
      ) : loadError ? (
        <div role="alert">
          <Alert tone="danger" testId="mcp-error">
            {loadError}
          </Alert>
        </div>
      ) : servers.length === 0 ? (
        <EmptyState
          title="No MCP servers configured."
          description="MCP servers expose extra tools to every agent — add one to extend the catalog."
          icon={<Icon name="plug" size={18} />}
          variant="boxed"
          testId="mcp-empty"
          action={
            <Button size="sm" data-testid="mcp-add-empty" onClick={openAdd}>
              Add MCP server
            </Button>
          }
        />
      ) : (
        <SettingsList testId="mcp-list">
          {servers.map((server: MCPServer) => (
            <SettingsRow
              key={server.ID}
              title={server.Name}
              testId={`mcp-${server.ID}`}
              lead={
                <span
                  className="inline-flex"
                  data-testid={`mcp-dot-${server.ID}`}
                  data-status={dotStatus(server.Status)}
                >
                  <StatusDot
                    status={dotStatus(server.Status)}
                    className="shrink-0"
                  />
                </span>
              }
              badges={
                <>
                  <Chip>{server.Transport}</Chip>
                  {server.Auth?.mode === "oauth" && (
                    <span
                      className="inline-flex"
                      data-testid={`mcp-auth-${server.ID}`}
                    >
                      <Chip>oauth</Chip>
                    </span>
                  )}
                  {!server.Enabled && <DisabledChip />}
                  <span
                    className="inline-flex"
                    data-testid={`mcp-status-${server.ID}`}
                  >
                    <StatusChip server={server} />
                  </span>
                </>
              }
              detail={
                server.Transport === "stdio"
                  ? [server.Command, ...(server.Args ?? [])].join(" ")
                  : server.URL
              }
              extra={
                server.StatusError ? (
                  <div
                    className={`truncate text-xs ${
                      server.Status === STATUS_AUTH_REQUIRED
                        ? "text-[color-mix(in_oklab,var(--warn),black_38%)]"
                        : "text-dangertext"
                    }`}
                    title={server.StatusError}
                    data-testid={`mcp-reason-${server.ID}`}
                  >
                    {server.StatusError}
                  </div>
                ) : null
              }
              actions={
                <>
                  {server.Status === STATUS_AUTH_REQUIRED && (
                    <Button
                      size="sm"
                      loading={authorizing.has(server.ID)}
                      data-testid={`mcp-authorize-${server.ID}`}
                      onClick={() => void authorize(server)}
                    >
                      <Icon name="shield" size={14} />
                      Authorize
                    </Button>
                  )}
                  {server.Auth?.mode === "oauth" &&
                    server.Status === STATUS_CONNECTED && (
                      <IconButton
                        label={`Disconnect ${server.Name}`}
                        size="sm"
                        testId={`mcp-disconnect-${server.ID}`}
                        className="text-[color-mix(in_oklab,var(--warn),black_38%)] hover:bg-[color-mix(in_oklab,var(--warn)_12%,transparent)] hover:text-[color-mix(in_oklab,var(--warn),black_38%)]"
                        icon={<Icon name="logout" size={16} />}
                        onClick={() => void disconnect(server)}
                      />
                    )}
                  <Toggle
                    on={server.Enabled}
                    testId={`mcp-toggle-${server.ID}`}
                    label={
                      server.Enabled
                        ? `Disable ${server.Name}`
                        : `Enable ${server.Name}`
                    }
                    disabled={refreshing.has(server.ID)}
                    onChange={() => void toggleEnabled(server)}
                  />
                  <IconButton
                    label={`Refresh ${server.Name}`}
                    size="sm"
                    testId={`mcp-refresh-${server.ID}`}
                    disabled={refreshing.has(server.ID)}
                    icon={
                      <Icon
                        name="refresh"
                        size={16}
                        className={
                          refreshing.has(server.ID) ? "animate-spin" : ""
                        }
                      />
                    }
                    onClick={() => void refresh(server)}
                  />
                  <IconButton
                    label={`Edit ${server.Name}`}
                    size="sm"
                    testId={`mcp-edit-${server.ID}`}
                    icon={<Icon name="edit" size={16} />}
                    onClick={() => openDialog(server)}
                  />
                  <IconButton
                    label={`Remove ${server.Name}`}
                    size="sm"
                    testId={`mcp-remove-${server.ID}`}
                    className="text-danger hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] hover:text-danger"
                    icon={<Icon name="trash" size={16} />}
                    onClick={() => setRemoving(server)}
                  />
                </>
              }
            />
          ))}
        </SettingsList>
      )}

      {dialog && (
        <ModalShell
          title={
            dialog.mode === "add"
              ? "Add MCP server"
              : `Edit ${dialog.draft.Name}`
          }
          testId="mcp-dialog"
          onClose={() => setDialog(null)}
          actions={
            <>
              <Button
                variant="ghost"
                size="sm"
                data-testid="mcp-cancel"
                onClick={() => setDialog(null)}
              >
                Cancel
              </Button>
              <Button
                size="sm"
                data-testid="mcp-save"
                disabled={!canSave || saving}
                onClick={() => void save()}
              >
                Save
              </Button>
            </>
          }
        >
          <div className="mt-4">
            <FormGrid>
              <FormField id="mcp-name" label="Name">
                <Input
                  id="mcp-name"
                  className="w-full"
                  data-testid="mcp-name"
                  autoFocus
                  value={dialog.draft.Name}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                    updateDraft({ Name: e.target.value })
                  }
                />
              </FormField>
              <FormField id="mcp-transport" label="Transport">
                <Select
                  id="mcp-transport"
                  className="w-full"
                  data-testid="mcp-transport"
                  value={dialog.draft.Transport}
                  onChange={(e: React.ChangeEvent<HTMLSelectElement>) =>
                    updateDraft({ Transport: e.target.value })
                  }
                >
                  <option value="stdio">stdio</option>
                  <option value="http">http</option>
                  <option value="sse">sse</option>
                </Select>
              </FormField>
              {isStdio ? (
                <>
                  <FormField id="mcp-command" label="Command">
                    <Input
                      id="mcp-command"
                      className="w-full"
                      data-testid="mcp-command"
                      value={dialog.draft.Command}
                      onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                        updateDraft({ Command: e.target.value })
                      }
                    />
                  </FormField>
                  <FormField id="mcp-args" label="Arguments (one per line)">
                    <Textarea
                      id="mcp-args"
                      className="w-full font-mono"
                      data-testid="mcp-args"
                      value={dialog.argsText}
                      onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                        setDialog({ ...dialog, argsText: e.target.value })
                      }
                    />
                  </FormField>
                </>
              ) : (
                <>
                  <FormField id="mcp-url" label="URL">
                    <Input
                      id="mcp-url"
                      className="w-full"
                      data-testid="mcp-url"
                      value={dialog.draft.URL}
                      onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                        updateDraft({ URL: e.target.value })
                      }
                    />
                  </FormField>
                  <FormField id="mcp-auth-mode" label="Authentication">
                    <Select
                      id="mcp-auth-mode"
                      className="w-full"
                      data-testid="mcp-auth-mode"
                      value={dialog.draft.Auth?.mode ?? "none"}
                      onChange={(e: React.ChangeEvent<HTMLSelectElement>) =>
                        updateDraft({
                          Auth: { ...dialog.draft.Auth, mode: e.target.value },
                        })
                      }
                    >
                      <option value="none">None (static headers)</option>
                      <option value="oauth">OAuth 2.1</option>
                    </Select>
                    {dialog.draft.Auth?.mode === "oauth" && (
                      <p className="text-xs text-fg2">
                        Tokens are obtained in your browser when you click
                        Authorize on the row; static headers are still sent.
                      </p>
                    )}
                  </FormField>
                  <FormField
                    id="mcp-headers"
                    label="Headers (KEY=VALUE per line)"
                  >
                    <Textarea
                      id="mcp-headers"
                      className="w-full font-mono"
                      data-testid="mcp-headers"
                      value={dialog.headersText}
                      onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                        setDialog({ ...dialog, headersText: e.target.value })
                      }
                    />
                  </FormField>
                </>
              )}
              <FormField id="mcp-env" label="Environment (KEY=VALUE per line)">
                <Textarea
                  id="mcp-env"
                  className="w-full font-mono"
                  data-testid="mcp-env"
                  value={dialog.envText}
                  onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                    setDialog({ ...dialog, envText: e.target.value })
                  }
                />
              </FormField>
              <label className="flex items-center gap-2 text-[12px] font-medium text-fg2">
                <Toggle
                  on={dialog.draft.Enabled}
                  testId="mcp-enabled"
                  label="Enabled"
                  onChange={(next: boolean) => updateDraft({ Enabled: next })}
                />
                Enabled
              </label>
            </FormGrid>
          </div>
        </ModalShell>
      )}

      {removing && (
        <ConfirmDialog
          title={`Remove ${removing.Name}?`}
          testId="mcp-remove-dialog"
          confirmLabel="Remove"
          confirmTestId="mcp-remove-confirm"
          cancelTestId="mcp-remove-cancel"
          onConfirm={() => void confirmRemove()}
          onClose={() => setRemoving(null)}
        />
      )}
    </div>
  );
}
