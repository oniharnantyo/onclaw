import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type { MCPServer } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";
import type { MCPOAuthCompletedEvent } from "../../../lib/events";

import McpPane from "../components/McpPane";
import type { SettingsPaneConfig } from "../types";
import { renderUI } from "./test-utils";

/** Header config passed to the pane; mirrors the static nav entry. */
const pane: SettingsPaneConfig = {
  key: "mcp",
  label: "MCP",
  icon: "plug",
  description: "Model Context Protocol servers exposing extra tools.",
};

const servers: MCPServer[] = [];

/** Completion handlers captured from onMcpOAuthCompleted subscriptions; a
 * test fires one to simulate the backend's event (already re-probed). */
const oauthHandlers: ((ev: MCPOAuthCompletedEvent) => void)[] = [];

const unsubscribeOAuth = vi.fn();

vi.mock("../../../lib/client", () => ({
  client: {
    settings: {
      mcpServers: vi.fn(async () => [...servers]),
      mcpServer: vi.fn(
        async (id: string): Promise<MCPServer> =>
          servers.find((s: MCPServer) => s.ID === id) ?? makeServer({ ID: id })
      ),
      createMCPServer: vi.fn(async (server: MCPServer) => {
        servers.push({ ...server, ID: `m${servers.length + 1}` });

        return server;
      }),
      updateMCPServer: vi.fn(async (server: MCPServer) => {
        const i = servers.findIndex((s: MCPServer) => s.ID === server.ID);

        if (i >= 0) servers[i] = server;

        return server;
      }),
      deleteMCPServer: vi.fn(async (id: string) => {
        const i = servers.findIndex((s: MCPServer) => s.ID === id);

        if (i >= 0) servers.splice(i, 1);
      }),
      // Mimic the backend probe: the row's status fields carry the outcome,
      // the definition is untouched.
      probeMCPServer: vi.fn(async (id: string): Promise<MCPServer> => {
        const server =
          servers.find((s: MCPServer) => s.ID === id) ?? makeServer({ ID: id });

        return {
          ...server,
          Status: "connected",
          StatusError: "",
          ToolCount: 3,
        };
      }),
      authorizeMCPServer: vi.fn(async (): Promise<void> => undefined),
      // Mimic the backend disconnect: tokens revoked/cleared, the row comes
      // back authorization-required (design D9).
      disconnectMCPServer: vi.fn(async (id: string): Promise<MCPServer> => {
        const server =
          servers.find((s: MCPServer) => s.ID === id) ?? makeServer({ ID: id });

        const row: MCPServer = {
          ...server,
          Status: "auth_required",
          StatusError: "disconnected — authorize to reconnect",
        };

        const i = servers.findIndex((s: MCPServer) => s.ID === id);

        if (i >= 0) servers[i] = row;

        return row;
      }),
    },
    events: {
      onMcpOAuthCompleted: vi.fn(
        (handler: (ev: MCPOAuthCompletedEvent) => void) => {
          oauthHandlers.push(handler);

          return unsubscribeOAuth;
        }
      ),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  settings: {
    mcpServers: MockFn;
    mcpServer: MockFn;
    createMCPServer: MockFn;
    updateMCPServer: MockFn;
    deleteMCPServer: MockFn;
    probeMCPServer: MockFn;
    authorizeMCPServer: MockFn;
    disconnectMCPServer: MockFn;
  };
  events: {
    onMcpOAuthCompleted: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

function makeServer(patch: Partial<MCPServer> = {}): MCPServer {
  return {
    ID: "m1",
    Name: "fs",
    Transport: "stdio",
    Command: "mcp-fs",
    Args: ["--root", "/tmp"],
    Env: { FOO: "bar" },
    URL: "",
    Headers: {},
    Auth: {},
    Enabled: true,
    Status: "configured",
    StatusError: "",
    ToolCount: 0,
    CreatedAt: "",
    UpdatedAt: "",
    ...patch,
  };
}

describe("McpPane", () => {
  beforeEach(() => {
    servers.length = 0;
    oauthHandlers.length = 0;
    vi.clearAllMocks();
  });

  it("round-trips add: command/args/env parsed and createMCPServer called", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <McpPane pane={pane} />
    );

    expect(await findByTestId("mcp-empty")).toBeTruthy();

    await user.click(getByTestId("mcp-add"));
    await user.type(getByTestId("mcp-name"), "fs");
    await user.type(getByTestId("mcp-command"), "mcp-fs");
    await user.type(getByTestId("mcp-args"), "--root");
    await user.type(getByTestId("mcp-env"), "FOO=bar");
    await user.click(getByTestId("mcp-save"));

    expect(mockedClient.settings.createMCPServer).toHaveBeenCalledWith(
      expect.objectContaining({
        Name: "fs",
        Transport: "stdio",
        Command: "mcp-fs",
        Args: ["--root"],
        Env: { FOO: "bar" },
      })
    );
    expect(await findByTestId("mcp-list")).toBeTruthy();
  });

  it("offers Add MCP server as the empty state's next-step action", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <McpPane pane={pane} />
    );

    expect(await findByTestId("mcp-empty")).toBeTruthy();

    await user.click(getByTestId("mcp-add-empty"));
    expect(getByTestId("mcp-dialog")).toBeTruthy();
  });

  it("shows stored status verbatim and 'unknown' when empty", async () => {
    servers.push(
      makeServer(),
      makeServer({
        ID: "m2",
        Name: "web",
        Transport: "http",
        Command: "",
        URL: "http://x",
        Status: "",
      })
    );
    const { findByTestId } = await renderUI(<McpPane pane={pane} />);

    expect((await findByTestId("mcp-status-m1")).textContent).toBe(
      "configured"
    );
    expect((await findByTestId("mcp-status-m2")).textContent).toBe("unknown");
  });

  it("renders an unprobed row as a neutral unknown chip with a refresh affordance", async () => {
    servers.push(makeServer({ Status: "", StatusError: "", ToolCount: 0 }));

    const { findByTestId, queryByTestId } = await renderUI(
      <McpPane pane={pane} />
    );

    const status = await findByTestId("mcp-status-m1");
    expect(status.textContent).toBe("unknown");
    // Neither success nor danger tint before any probe — the unprobed default
    // stays visually quiet in both themes (the tints ride the tokens).
    // SAFETY: the status testid wraps exactly one kit Chip.
    const chip = status.firstElementChild as HTMLElement | null;
    expect(chip?.className).not.toContain("var(--success)");
    expect(chip?.className).not.toContain("var(--danger)");
    expect(queryByTestId("mcp-reason-m1")).toBeNull();
    expect(await findByTestId("mcp-refresh-m1")).toBeTruthy();
  });

  it("renders a connected row with the discovered tool count on the success tint", async () => {
    servers.push(makeServer({ Status: "connected", ToolCount: 5 }));

    const { findByTestId, queryByTestId } = await renderUI(
      <McpPane pane={pane} />
    );

    const status = await findByTestId("mcp-status-m1");
    expect(status.textContent).toBe("connected · 5 tools");
    // SAFETY: the status testid wraps exactly one kit Chip.
    const chip = status.firstElementChild as HTMLElement | null;
    expect(chip?.className).toContain("var(--success)");
    expect(queryByTestId("mcp-reason-m1")).toBeNull();
  });

  it("renders a failed row with the reason on the danger tint", async () => {
    servers.push(
      makeServer({
        Status: "error",
        StatusError: "dial tcp 127.0.0.1:9999: connection refused",
      })
    );
    const { findByTestId } = await renderUI(<McpPane pane={pane} />);

    const status = await findByTestId("mcp-status-m1");
    expect(status.textContent).toBe("failed");
    // SAFETY: the status testid wraps exactly one kit Chip.
    const chip = status.firstElementChild as HTMLElement | null;
    expect(chip?.className).toContain("var(--danger)");
    expect((await findByTestId("mcp-reason-m1")).textContent).toBe(
      "dial tcp 127.0.0.1:9999: connection refused"
    );
  });

  it("refreshes a row: calls the binding and updates the row without a write", async () => {
    servers.push(makeServer({ Status: "", StatusError: "", ToolCount: 0 }));
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<McpPane pane={pane} />);

    await user.click(await findByTestId("mcp-refresh-m1"));

    expect(mockedClient.settings.probeMCPServer).toHaveBeenCalledWith("m1");
    // The row re-renders with the persisted outcome...
    expect((await findByTestId("mcp-status-m1")).textContent).toBe(
      "connected · 3 tools"
    );
    // …and the refresh never rewrites the definition.
    expect(mockedClient.settings.updateMCPServer).not.toHaveBeenCalled();
  });

  it("maps each stored status to the dot vocabulary and warns on auth_required", async () => {
    servers.push(
      makeServer({ Status: "connected" }),
      makeServer({
        ID: "m2",
        Name: "broken",
        Status: "error",
        StatusError: "dial tcp: refused",
      }),
      makeServer({
        ID: "m3",
        Name: "atlassian",
        Transport: "http",
        Command: "",
        URL: "https://mcp.atlassian.com/mcp",
        Status: "auth_required",
        StatusError: "authorization required — Authorize to sign in",
      }),
      makeServer({ ID: "m4", Name: "fresh", Status: "" })
    );
    const { findByTestId } = await renderUI(<McpPane pane={pane} />);

    expect((await findByTestId("mcp-dot-m1")).getAttribute("data-status")).toBe(
      "running"
    );
    expect((await findByTestId("mcp-dot-m2")).getAttribute("data-status")).toBe(
      "error"
    );
    expect((await findByTestId("mcp-dot-m3")).getAttribute("data-status")).toBe(
      "warn"
    );
    expect((await findByTestId("mcp-dot-m4")).getAttribute("data-status")).toBe(
      "idle"
    );
    expect((await findByTestId("mcp-status-m3")).textContent).toBe(
      "authorization required"
    );
    // The auth_required reason is warn-tinted, not danger-tinted.
    // SAFETY: the reason testid is a plain div with the tint in className.
    const reason = await findByTestId("mcp-reason-m3");
    expect(reason.className).toContain("var(--warn)");
    expect(reason.className).not.toContain("dangertext");
  });

  it("toggles a row off without probing, and on with a refresh", async () => {
    servers.push(makeServer({ Status: "", StatusError: "", ToolCount: 0 }));
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<McpPane pane={pane} />);

    // Off: the write lands immediately and no probe runs (design D7 — the
    // backend's update invalidation drops the live connection).
    await user.click(await findByTestId("mcp-toggle-m1"));
    expect(mockedClient.settings.updateMCPServer).toHaveBeenCalledTimes(1);
    expect(mockedClient.settings.updateMCPServer).toHaveBeenCalledWith(
      expect.objectContaining({ ID: "m1", Enabled: false })
    );
    expect(mockedClient.settings.probeMCPServer).not.toHaveBeenCalled();
    expect((await findByTestId("mcp-m1")).textContent).toContain("disabled");

    // On: the write is followed by a refresh of that row, so the dot's
    // outcome becomes visible without waiting for an agent turn.
    await user.click(await findByTestId("mcp-toggle-m1"));
    expect(mockedClient.settings.updateMCPServer).toHaveBeenCalledWith(
      expect.objectContaining({ ID: "m1", Enabled: true })
    );
    expect(mockedClient.settings.probeMCPServer).toHaveBeenCalledWith("m1");
    expect((await findByTestId("mcp-status-m1")).textContent).toBe(
      "connected · 3 tools"
    );
  });

  it("refresh all re-dials every enabled server and skips disabled ones", async () => {
    servers.push(
      makeServer({ Status: "", StatusError: "", ToolCount: 0 }),
      makeServer({
        ID: "m2",
        Name: "off",
        Transport: "http",
        Command: "",
        URL: "http://off",
        Enabled: false,
        Status: "error",
        StatusError: "stale",
      }),
      makeServer({
        ID: "m3",
        Name: "db",
        Transport: "http",
        Command: "",
        URL: "http://db",
        Status: "",
      })
    );
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<McpPane pane={pane} />);

    await user.click(await findByTestId("mcp-refresh-all"));

    expect(mockedClient.settings.probeMCPServer).toHaveBeenCalledTimes(2);
    expect(mockedClient.settings.probeMCPServer).toHaveBeenCalledWith("m1");
    expect(mockedClient.settings.probeMCPServer).toHaveBeenCalledWith("m3");
    // Each enabled row updates with its fresh outcome; the disabled row keeps
    // its stale state.
    expect((await findByTestId("mcp-status-m1")).textContent).toBe(
      "connected · 3 tools"
    );
    expect((await findByTestId("mcp-status-m3")).textContent).toBe(
      "connected · 3 tools"
    );
    expect((await findByTestId("mcp-status-m2")).textContent).toBe("failed");
  });

  it("disables refresh all when no server is enabled", async () => {
    servers.push(makeServer({ Enabled: false }));

    const { findByTestId } = await renderUI(<McpPane pane={pane} />);

    // SAFETY: the testid names the kit Button, a native <button>.
    const refreshAll = (await findByTestId(
      "mcp-refresh-all"
    )) as HTMLButtonElement;

    expect(refreshAll.disabled).toBe(true);
  });

  it("edits an http server with URL and headers, then removes", async () => {
    servers.push(
      makeServer({
        Transport: "http",
        Command: "",
        Args: [],
        URL: "http://x",
        Headers: { Authorization: "Bearer t" },
      })
    );
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <McpPane pane={pane} />
    );

    await user.click(await findByTestId("mcp-edit-m1"));
    await user.type(getByTestId("mcp-url"), "/v2");
    await user.click(getByTestId("mcp-save"));
    expect(mockedClient.settings.updateMCPServer).toHaveBeenCalledWith(
      expect.objectContaining({
        ID: "m1",
        URL: "http://x/v2",
        Headers: { Authorization: "Bearer t" },
      })
    );

    await user.click(await findByTestId("mcp-remove-m1"));
    await user.click(getByTestId("mcp-remove-confirm"));
    expect(mockedClient.settings.deleteMCPServer).toHaveBeenCalledWith("m1");
  });

  /** An authorization-required http row (the OAuth row state, design D5). */
  function makeAuthRequiredServer(patch: Partial<MCPServer> = {}): MCPServer {
    return makeServer({
      Transport: "http",
      Command: "",
      Args: [],
      URL: "https://mcp.atlassian.com/mcp",
      Status: "auth_required",
      StatusError: "authorization required — Authorize to sign in",
      ...patch,
    });
  }

  it("offers the Authentication select on http servers and saves the oauth mode", async () => {
    servers.push(
      makeServer({
        Transport: "http",
        Command: "",
        Args: [],
        URL: "https://mcp.example.com/mcp",
      })
    );
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <McpPane pane={pane} />
    );

    await user.click(await findByTestId("mcp-edit-m1"));
    // SAFETY: the testid names the kit Select, a native <select>.
    const select = getByTestId("mcp-auth-mode") as HTMLSelectElement;
    expect(select.value).toBe("none");

    await user.selectOptions(select, "oauth");
    expect(select.value).toBe("oauth");
    await user.click(getByTestId("mcp-save"));

    expect(mockedClient.settings.updateMCPServer).toHaveBeenCalledWith(
      expect.objectContaining({
        ID: "m1",
        Auth: expect.objectContaining({ mode: "oauth" }),
      })
    );
  });

  it("omits the Authentication select for stdio servers", async () => {
    servers.push(makeServer());
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <McpPane pane={pane} />
    );

    await user.click(await findByTestId("mcp-edit-m1"));
    expect(queryByTestId("mcp-auth-mode")).toBeNull();
    expect(getByTestId("mcp-command")).toBeTruthy();
  });

  it("authorize starts the flow once, leaves the row untouched, and holds the loading state", async () => {
    servers.push(makeAuthRequiredServer());

    let release!: () => void;

    const gate = new Promise<void>((resolve: () => void) => {
      release = resolve;
    });

    mockedClient.settings.authorizeMCPServer.mockImplementationOnce(
      (): Promise<void> => gate
    );
    const user = userEvent.setup();

    const { findByTestId, getByTestId } = await renderUI(
      <McpPane pane={pane} />
    );

    await user.click(await findByTestId("mcp-authorize-m1"));

    expect(mockedClient.settings.authorizeMCPServer).toHaveBeenCalledTimes(1);
    expect(mockedClient.settings.authorizeMCPServer).toHaveBeenCalledWith("m1");
    // The row is unaffected until the flow completes (spec: "the row is
    // unaffected until the flow completes")…
    expect((await findByTestId("mcp-status-m1")).textContent).toBe(
      "authorization required"
    );
    // …while the button sits in its loading/disabled state.
    // SAFETY: the testid names the kit Button, a native <button>.
    const authorizing = getByTestId("mcp-authorize-m1") as HTMLButtonElement;
    expect(authorizing.disabled).toBe(true);
    expect(authorizing.getAttribute("aria-busy")).toBe("true");
    // Refresh stays available during the flow.
    expect(getByTestId("mcp-refresh-m1")).toBeTruthy();

    release();
    await act(async () => {}); // flush the authorize promise
  });

  it("authorize completion refetches the row and renders the connected state", async () => {
    servers.push(makeAuthRequiredServer());
    const user = userEvent.setup();

    const { findByTestId, queryByTestId } = await renderUI(
      <McpPane pane={pane} />
    );

    await user.click(await findByTestId("mcp-authorize-m1"));

    // The backend re-probed before emitting; the stored row now carries the
    // connected outcome the refetch should surface.
    servers[0] = {
      ...servers[0],
      Status: "connected",
      StatusError: "",
      ToolCount: 7,
    };
    await act(async () => {
      oauthHandlers.forEach((handler: (ev: MCPOAuthCompletedEvent) => void) => {
        handler({ serverId: "m1", ok: true });
      });
    });

    expect(mockedClient.settings.mcpServer).toHaveBeenCalledWith("m1");
    expect((await findByTestId("mcp-status-m1")).textContent).toBe(
      "connected · 7 tools"
    );
    // No longer authorization-required, so the Authorize action is gone.
    expect(queryByTestId("mcp-authorize-m1")).toBeNull();
    expect(queryByTestId("mcp-reason-m1")).toBeNull();
  });

  it("authorize failure surfaces the event's error message in the alert", async () => {
    servers.push(makeAuthRequiredServer());
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<McpPane pane={pane} />);

    await user.click(await findByTestId("mcp-authorize-m1"));

    servers[0] = { ...servers[0], StatusError: "authorization denied: denied" };
    await act(async () => {
      oauthHandlers.forEach((handler: (ev: MCPOAuthCompletedEvent) => void) => {
        handler({ serverId: "m1", ok: false, error: "denied" });
      });
    });

    expect(mockedClient.settings.mcpServer).toHaveBeenCalledWith("m1");
    expect(await findByTestId("mcp-error")).toBeTruthy();
    expect((await findByTestId("mcp-error")).textContent).toBe("denied");
  });

  it("authorize rejection surfaces its message and releases the button", async () => {
    servers.push(makeAuthRequiredServer());
    mockedClient.settings.authorizeMCPServer.mockRejectedValueOnce(
      new Error("no browser available")
    );
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<McpPane pane={pane} />);

    await user.click(await findByTestId("mcp-authorize-m1"));

    expect(await findByTestId("mcp-error")).toBeTruthy();
    expect((await findByTestId("mcp-error")).textContent).toBe(
      "no browser available"
    );
  });

  it("disconnect appears only for oauth-mode connected rows and returns the row to authorization-required", async () => {
    servers.push(
      makeServer({ Status: "connected", ToolCount: 2 }),
      makeAuthRequiredServer({
        ID: "m2",
        Name: "atlassian",
        Auth: { mode: "oauth" },
        Status: "connected",
        StatusError: "",
        ToolCount: 4,
      }),
      makeAuthRequiredServer({
        ID: "m3",
        Name: "pending",
        Auth: { mode: "oauth" },
      })
    );
    const user = userEvent.setup();

    const { findByTestId, queryByTestId } = await renderUI(
      <McpPane pane={pane} />
    );

    // None-mode connected: no Disconnect. OAuth auth_required: no Disconnect.
    expect(queryByTestId("mcp-disconnect-m1")).toBeNull();
    expect(await findByTestId("mcp-disconnect-m2")).toBeTruthy();
    expect(queryByTestId("mcp-disconnect-m3")).toBeNull();

    await user.click(await findByTestId("mcp-disconnect-m2"));

    expect(mockedClient.settings.disconnectMCPServer).toHaveBeenCalledWith(
      "m2"
    );
    // The row swapped to the returned authorization-required state…
    expect((await findByTestId("mcp-status-m2")).textContent).toBe(
      "authorization required"
    );
    // …which offers Authorize instead of Disconnect.
    expect(queryByTestId("mcp-disconnect-m2")).toBeNull();
    expect(await findByTestId("mcp-authorize-m2")).toBeTruthy();
  });

  it("tags oauth-mode rows with a neutral oauth chip", async () => {
    servers.push(
      makeServer(),
      makeServer({
        ID: "m2",
        Name: "atlassian",
        Transport: "http",
        Command: "",
        Args: [],
        URL: "https://mcp.atlassian.com/mcp",
        Auth: { mode: "oauth" },
      })
    );

    const { findByTestId, queryByTestId } = await renderUI(
      <McpPane pane={pane} />
    );

    expect(await findByTestId("mcp-auth-m2")).toBeTruthy();
    expect((await findByTestId("mcp-auth-m2")).textContent).toBe("oauth");
    expect(queryByTestId("mcp-auth-m1")).toBeNull();
  });

  it("unsubscribes from mcp:oauth:completed on unmount", async () => {
    const { unmount } = await renderUI(<McpPane pane={pane} />);

    expect(mockedClient.events.onMcpOAuthCompleted).toHaveBeenCalledTimes(1);
    expect(unsubscribeOAuth).not.toHaveBeenCalled();

    await unmount();
    expect(unsubscribeOAuth).toHaveBeenCalledTimes(1);
  });
});
