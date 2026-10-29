import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { fireEvent } from "@testing-library/dom";
import userEvent from "@testing-library/user-event";
import type { IntegrationConnectInput } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { MCPServer } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";
import type { MCPOAuthCompletedEvent } from "../../../lib/events";

import IntegrationsPane from "../IntegrationsPane";
import type { SettingsPaneConfig } from "../../settings/types";
import { renderUI } from "../../settings/__test__/test-utils";

/** Header config passed to the pane; mirrors the static nav entry. */
const pane: SettingsPaneConfig = {
  key: "integrations",
  label: "Integrations",
  icon: "globe",
  description: "Connect external services — GitLab, GitHub, and more.",
};

const servers: MCPServer[] = [];

/** Completion handlers captured from onMcpOAuthCompleted subscriptions; a
 * test fires one to simulate the backend's event (already re-probed). */
const oauthHandlers: ((ev: MCPOAuthCompletedEvent) => void)[] = [];

const unsubscribeOAuth = vi.fn();

let nextId = 0;

function makeServer(patch: Partial<MCPServer> = {}): MCPServer {
  nextId += 1;

  return {
    ID: `m${nextId}`,
    Name: "linear",
    Transport: "http",
    Command: "",
    Args: [],
    Env: {},
    URL: "https://mcp.linear.app/mcp",
    Headers: {},
    Auth: {},
    Service: "linear",
    CredentialSet: false,
    CredentialHint: "",
    Enabled: true,
    Status: "",
    StatusError: "",
    ToolCount: 0,
    CreatedAt: "",
    UpdatedAt: "",
    ...patch,
  };
}

vi.mock("../../../lib/client", () => ({
  client: {
    settings: {
      mcpServers: vi.fn(async (): Promise<MCPServer[]> => [...servers]),
      mcpServer: vi.fn(
        async (id: string): Promise<MCPServer> =>
          servers.find((s: MCPServer) => s.ID === id) ?? makeServer({ ID: id })
      ),
      authorizeMCPServer: vi.fn(async (): Promise<void> => undefined),
      // Mimic the backend connect: one service-tagged row per product (or
      // the single service), each dialed at its own ProductEndpoints entry
      // (falling back to Endpoint) and probed to connected.
      connectIntegration: vi.fn(
        async (input: IntegrationConnectInput): Promise<MCPServer> => {
          const services: string[] =
            input.Products && input.Products.length > 0
              ? input.Products
              : [input.Service];

          for (const service of services) {
            servers.push(
              makeServer({
                Name: service,
                Transport: "http",
                URL: input.ProductEndpoints?.[service] ?? input.Endpoint,
                Service: service,
                Status: "connected",
              })
            );
          }

          // SAFETY: services always has at least the pushed first row.
          return servers[servers.length - services.length];
        }
      ),
      disconnectIntegration: vi.fn(async (id: string): Promise<void> => {
        const i = servers.findIndex((s: MCPServer) => s.ID === id);

        if (i >= 0) servers.splice(i, 1);
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
    authorizeMCPServer: MockFn;
    connectIntegration: MockFn;
    disconnectIntegration: MockFn;
  };
  events: {
    onMcpOAuthCompleted: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

function tileCount(container: HTMLElement): number {
  return container.querySelectorAll('[data-testid^="integrations-tile-"]')
    .length;
}

/** The five Google Workspace family product ids, in catalog order. */
const familyIds = ["gmail", "calendar", "drive", "sheets", "docs"];

describe("IntegrationsPane", () => {
  beforeEach(() => {
    servers.length = 0;
    oauthHandlers.length = 0;
    nextId = 0;
    vi.clearAllMocks();
  });

  it("renders all 13 catalog tiles as one flat grid with no category headers", async () => {
    const { container, findByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    expect(await findByTestId("integrations-grid")).toBeTruthy();
    expect(tileCount(container)).toBe(13);
    // No category section headers, ever — category lives only in the rail.
    expect(container.querySelectorAll("h2, h3").length).toBe(0);
    expect(
      container.querySelectorAll("[data-testid^='integrations-section']").length
    ).toBe(0);
    // Every family product is its own tile (spec: "Each product SHALL
    // appear as its own tile") and the family head renders none.
    expect(
      container.querySelectorAll(
        '[data-testid="integrations-tile-google-workspace"]'
      ).length
    ).toBe(0);

    for (const id of familyIds) {
      expect(await findByTestId(`integrations-tile-${id}`)).toBeTruthy();
    }

    // Every chip shows its catalog-wide tile count (All = the whole grid;
    // productivity counts its three services plus the five products).
    expect((await findByTestId("integrations-chip-all")).textContent).toContain(
      "13"
    );
    expect(
      (await findByTestId("integrations-chip-productivity")).textContent
    ).toContain("8");
  });

  it("shows glyph, label and a success dot for a connected service", async () => {
    servers.push(
      makeServer({ Service: "linear", Status: "connected", ToolCount: 5 })
    );

    const { findByTestId } = await renderUI(<IntegrationsPane pane={pane} />);

    const tile = await findByTestId("integrations-tile-linear");
    expect(tile.textContent).toContain("Linear");
    // Linear has a known brand mark: an inline svg glyph, not the fallback.
    expect(tile.querySelector("svg")).toBeTruthy();
    expect(
      (await findByTestId("integrations-dot-linear")).getAttribute(
        "data-status"
      )
    ).toBe("running");
  });

  it("shows a danger dot and the failure reason on a failed tile", async () => {
    servers.push(
      makeServer({
        Service: "gitlab",
        Name: "gitlab",
        Status: "error",
        StatusError: "dial tcp: connection refused",
      })
    );

    const { findByTestId } = await renderUI(<IntegrationsPane pane={pane} />);

    expect(
      (await findByTestId("integrations-dot-gitlab")).getAttribute(
        "data-status"
      )
    ).toBe("error");
    expect(await findByTestId("integrations-reason-gitlab")).toBeTruthy();
    expect((await findByTestId("integrations-reason-gitlab")).textContent).toBe(
      "dial tcp: connection refused"
    );
  });

  it("shows each family product tile its own connection state", async () => {
    servers.push(
      makeServer({
        ID: "m1",
        Name: "gmail",
        Service: "gmail",
        Status: "connected",
      }),
      makeServer({
        ID: "m2",
        Name: "calendar",
        Service: "calendar",
        Status: "auth_required",
        StatusError: "authorization required — Authorize to sign in",
      }),
      makeServer({
        ID: "m3",
        Name: "sheets",
        Service: "sheets",
        Status: "error",
        StatusError: "dial tcp: connection refused",
      })
    );

    const { findByTestId, queryByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    // Each product tile's dot is its own row's state only (spec: each
    // product has its own connection state) — never a sibling's.
    expect(
      (await findByTestId("integrations-dot-gmail")).getAttribute("data-status")
    ).toBe("running");
    expect(
      (await findByTestId("integrations-dot-calendar")).getAttribute(
        "data-status"
      )
    ).toBe("warn");
    expect(
      (await findByTestId("integrations-dot-sheets")).getAttribute(
        "data-status"
      )
    ).toBe("error");
    expect(
      (await findByTestId("integrations-dot-drive")).getAttribute("data-status")
    ).toBe("idle");

    // The failed product surfaces its own reason on its own tile.
    expect(await findByTestId("integrations-reason-sheets")).toBeTruthy();
    expect((await findByTestId("integrations-reason-sheets")).textContent).toBe(
      "dial tcp: connection refused"
    );
    expect(queryByTestId("integrations-reason-gmail")).toBeNull();
    expect(queryByTestId("integrations-reason-calendar")).toBeNull();
  });

  it("narrows the grid by category chip with catalog-wide counts", async () => {
    const user = userEvent.setup();

    const { container, getByTestId, findByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    await findByTestId("integrations-grid");
    expect(tileCount(container)).toBe(13);

    await user.click(getByTestId("integrations-chip-productivity"));
    // Productivity counts its three single services plus the five Google
    // product tiles — the grid's tiles, not catalog entries.
    expect(tileCount(container)).toBe(8);

    // The chip counts reflect the catalog, not the search: a query matching
    // nothing leaves every count untouched.
    await user.type(getByTestId("integrations-search"), "zzz-no-match");
    expect(tileCount(container)).toBe(0);
    expect(getByTestId("integrations-chip-productivity").textContent).toContain(
      "8"
    );
    expect(getByTestId("integrations-chip-all").textContent).toContain("13");
  });

  it("composes search with the category (Productivity + 'gma' → the Google product tiles)", async () => {
    const user = userEvent.setup();

    const { container, getByTestId, findByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    await findByTestId("integrations-grid");
    await user.click(getByTestId("integrations-chip-productivity"));
    await user.type(getByTestId("integrations-search"), "gma");

    // Matching stays entry-level: the google-workspace entry matches as a
    // whole, so all five of its product tiles appear — no head tile.
    const tiles = container.querySelectorAll(
      '[data-testid^="integrations-tile-"]'
    );

    expect(tiles.length).toBe(5);

    const testids = Array.from(tiles).map(
      (t: Element): string => t.getAttribute("data-testid") ?? ""
    );

    for (const id of familyIds) {
      expect(testids).toContain(`integrations-tile-${id}`);
    }
  });

  it("shows the empty state on no match and clears both filters", async () => {
    const user = userEvent.setup();

    const { container, getByTestId, findByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    await findByTestId("integrations-grid");
    await user.click(getByTestId("integrations-chip-business"));
    await user.type(getByTestId("integrations-search"), "zzz");

    expect(await findByTestId("integrations-empty")).toBeTruthy();

    await user.click(getByTestId("integrations-clear-filters"));

    expect(tileCount(container)).toBe(13);
    expect(
      getByTestId("integrations-chip-all").getAttribute("aria-pressed")
    ).toBe("true");
    // SAFETY: the testid names the kit Input, a native <input>.
    expect((getByTestId("integrations-search") as HTMLInputElement).value).toBe(
      ""
    );
  });

  it("offers exactly the declared auth methods in the connect sheet", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    // Notion declares OAuth only: no token method, no credential field.
    await user.click(await findByTestId("integrations-tile-notion"));
    expect(await findByTestId("integrations-connect-sheet")).toBeTruthy();
    expect(getByTestId("integrations-auth-method-oauth_dcr")).toBeTruthy();
    expect(queryByTestId("integrations-auth-method-token")).toBeNull();
    expect(queryByTestId("integrations-credential")).toBeNull();
    await user.click(getByTestId("integrations-cancel"));

    // Slack declares the pre-registered client only: id + secret fields.
    await user.click(await findByTestId("integrations-tile-slack"));
    expect(await findByTestId("integrations-client-id")).toBeTruthy();
    expect(await findByTestId("integrations-client-secret")).toBeTruthy();
    expect(queryByTestId("integrations-credential")).toBeNull();
    await user.click(getByTestId("integrations-cancel"));

    // GitLab's templated endpoint asks for the instance, defaulted.
    await user.click(await findByTestId("integrations-tile-gitlab"));

    // SAFETY: the testid names the kit Input, a native <input>.
    const instance = getByTestId(
      "integrations-instance-url"
    ) as HTMLInputElement;

    expect(instance.value).toBe("https://gitlab.com");
  });

  it("submits the catalog-driven connect input for a family and a templated service", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    // Google Workspace: clicking any product tile (fresh install → gmail)
    // opens the family sheet — all five products checked by default, the
    // confidential client pair required, Google's consent params along.
    await user.click(await findByTestId("integrations-tile-gmail"));

    for (const id of familyIds) {
      // SAFETY: the testid names the kit Checkbox's native <input>.
      expect(
        (getByTestId(`integrations-product-${id}`) as HTMLInputElement).checked
      ).toBe(true);
    }

    await user.type(getByTestId("integrations-client-id"), "google-client");
    await user.type(getByTestId("integrations-client-secret"), "google-secret");
    await user.click(getByTestId("integrations-connect-submit"));

    expect(mockedClient.settings.connectIntegration).toHaveBeenCalledTimes(1);
    expect(mockedClient.settings.connectIntegration).toHaveBeenCalledWith(
      expect.objectContaining({
        Service: "google-workspace",
        Endpoint: "https://gmailmcp.googleapis.com/mcp/v1",
        AuthMethod: "oauth_confidential",
        ClientID: "google-client",
        ClientSecret: "google-secret",
        ExtraAuthParams: { access_type: "offline", prompt: "consent" },
        // The confidential method declares no header style: the bearer default.
        CredentialHeaderStyle: "",
        Products: familyIds,
      })
    );

    // SAFETY: the mock records the IntegrationConnectInput the pane submitted.
    const familyInput = mockedClient.settings.connectIntegration.mock
      .calls[0][0] as IntegrationConnectInput;

    // Every created product maps to its own catalog endpoint (design D6).
    expect(familyInput.ProductEndpoints).toEqual({
      gmail: "https://gmailmcp.googleapis.com/mcp/v1",
      calendar: "https://calendarmcp.googleapis.com/mcp/v1",
      drive: "https://drivemcp.googleapis.com/mcp/v1",
      sheets: "https://sheetsmcp.googleapis.com/mcp/v1",
      docs: "https://docsmcp.googleapis.com/mcp/v1",
    });

    // GitLab: the token method fills the instance template.
    await user.click(await findByTestId("integrations-tile-gitlab"));
    await user.click(getByTestId("integrations-auth-method-token"));
    await user.type(getByTestId("integrations-credential"), "glpat-token");
    await user.click(getByTestId("integrations-connect-submit"));

    expect(mockedClient.settings.connectIntegration).toHaveBeenCalledWith(
      expect.objectContaining({
        Service: "gitlab",
        Endpoint: "https://gitlab.com/api/v4/mcp",
        AuthMethod: "token",
        Credential: "glpat-token",
        ExtraAuthParams: null,
        // The catalog-declared header style rides along; single services
        // carry no product list or endpoints.
        CredentialHeaderStyle: "bearer",
        Products: null,
        ProductEndpoints: null,
      })
    );
  });

  it("keeps the stored credential when editing submits a blank field", async () => {
    servers.push(
      makeServer({
        Service: "linear",
        Status: "connected",
        CredentialSet: true,
        CredentialHint: "ab12",
      })
    );
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    await user.click(await findByTestId("integrations-tile-linear"));

    // A stored credential selects the token method; the field starts blank
    // with only the redacted hint as its placeholder.
    // SAFETY: the testid names a native radio <input>.
    expect(
      (getByTestId("integrations-auth-method-token") as HTMLInputElement)
        .checked
    ).toBe(true);

    // SAFETY: the testid names the kit Input, a native <input>.
    const credential = getByTestId(
      "integrations-credential"
    ) as HTMLInputElement;

    expect(credential.value).toBe("");
    expect(credential.placeholder).toBe("Stored (····ab12)");

    await user.click(getByTestId("integrations-connect-submit"));

    expect(mockedClient.settings.connectIntegration).toHaveBeenCalledWith(
      expect.objectContaining({ Service: "linear", Credential: "" })
    );

    // SAFETY: the mock records the IntegrationConnectInput the pane submitted.
    const arg = mockedClient.settings.connectIntegration.mock
      .calls[0][0] as IntegrationConnectInput;

    expect(arg.Credential).not.toContain("ab12");
  });

  it("offers Authorize on an auth_required tile and calls the binding", async () => {
    servers.push(
      makeServer({
        Service: "github",
        Name: "github",
        URL: "https://api.githubcopilot.com/mcp/",
        Status: "auth_required",
        StatusError: "authorization required — Authorize to sign in",
      })
    );
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<IntegrationsPane pane={pane} />);

    expect(
      (await findByTestId("integrations-dot-github")).getAttribute(
        "data-status"
      )
    ).toBe("warn");

    await user.click(await findByTestId("integrations-authorize-github"));

    expect(mockedClient.settings.authorizeMCPServer).toHaveBeenCalledTimes(1);
    expect(mockedClient.settings.authorizeMCPServer).toHaveBeenCalledWith("m1");
  });

  it("reuses the stored setup from a product tile edit and submits only new products", async () => {
    servers.push(
      makeServer({
        ID: "m1",
        Name: "gmail",
        Service: "gmail",
        Status: "connected",
        Auth: { client_id: "stored-client" },
        CredentialSet: true,
        CredentialHint: "ab12",
      })
    );
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    // Opening the connected gmail product tile reopens the family sheet on
    // the stored shared setup.
    await user.click(await findByTestId("integrations-tile-gmail"));

    // The connected product renders as a chip, not a checkbox...
    expect(
      await findByTestId("integrations-product-connected-gmail")
    ).toBeTruthy();
    expect(queryByTestId("integrations-product-gmail")).toBeNull();

    // ...while the not-yet-connected ones stay checkboxes, checked by
    // default.
    for (const id of ["calendar", "drive", "sheets", "docs"]) {
      // SAFETY: the testid names the kit Checkbox's native <input>.
      expect(
        (getByTestId(`integrations-product-${id}`) as HTMLInputElement).checked
      ).toBe(true);
    }

    // The stored client id prefills; the secret stays blank with only the
    // redacted hint as its placeholder.
    // SAFETY: the testids name the kit Input, a native <input>.
    expect(
      (getByTestId("integrations-client-id") as HTMLInputElement).value
    ).toBe("stored-client");
    // SAFETY: the testids name the kit Input, a native <input>.
    expect(
      (getByTestId("integrations-client-secret") as HTMLInputElement).value
    ).toBe("");
    // SAFETY: the testid names the kit Input, a native <input>.
    expect(
      (getByTestId("integrations-client-secret") as HTMLInputElement)
        .placeholder
    ).toBe("Stored (····ab12)");

    // Leave calendar the only newly checked product.
    for (const id of ["drive", "sheets", "docs"]) {
      fireEvent.click(getByTestId(`integrations-product-${id}`));
    }

    await user.type(getByTestId("integrations-client-secret"), "google-secret");
    await user.click(getByTestId("integrations-connect-submit"));

    expect(mockedClient.settings.connectIntegration).toHaveBeenCalledWith(
      expect.objectContaining({
        Service: "google-workspace",
        ClientID: "stored-client",
        Products: ["calendar"],
      })
    );

    // SAFETY: the mock records the IntegrationConnectInput the pane submitted.
    const arg = mockedClient.settings.connectIntegration.mock
      .calls[0][0] as IntegrationConnectInput;

    expect(arg.ProductEndpoints).toEqual({
      calendar: "https://calendarmcp.googleapis.com/mcp/v1",
    });
  });

  it("disables the family submit as 'Everything connected' once all products exist", async () => {
    for (const id of familyIds) {
      servers.push(makeServer({ Name: id, Service: id, Status: "connected" }));
    }

    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    await user.click(await findByTestId("integrations-tile-gmail"));

    // SAFETY: the testid names the kit Button, a native <button>.
    const submit = getByTestId(
      "integrations-connect-submit"
    ) as HTMLButtonElement;

    expect(submit.textContent).toContain("Everything connected");
    expect(submit.disabled).toBe(true);
    expect(mockedClient.settings.connectIntegration).not.toHaveBeenCalled();
  });

  it("refetches the row when the OAuth flow completes", async () => {
    servers.push(
      makeServer({
        Service: "github",
        Name: "github",
        Status: "auth_required",
      })
    );
    const user = userEvent.setup();

    const { findByTestId, queryByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    await user.click(await findByTestId("integrations-authorize-github"));

    // The backend re-probed before emitting; the stored row now carries the
    // connected outcome the refetch should surface.
    servers[0] = { ...servers[0], Status: "connected", ToolCount: 7 };
    await act(async () => {
      oauthHandlers.forEach((handler: (ev: MCPOAuthCompletedEvent) => void) => {
        handler({ serverId: "m1", ok: true });
      });
    });

    expect(mockedClient.settings.mcpServer).toHaveBeenCalledWith("m1");
    expect(
      (await findByTestId("integrations-dot-github")).getAttribute(
        "data-status"
      )
    ).toBe("running");
    // No longer authorization-required, so the Authorize action is gone.
    expect(queryByTestId("integrations-authorize-github")).toBeNull();
  });

  it("removes an integration through the confirm dialog", async () => {
    servers.push(makeServer({ Service: "linear", Status: "connected" }));
    const user = userEvent.setup();

    const { container, getByTestId, findByTestId } = await renderUI(
      <IntegrationsPane pane={pane} />
    );

    await user.click(await findByTestId("integrations-remove-linear"));
    await user.click(getByTestId("integrations-remove-confirm"));

    expect(mockedClient.settings.disconnectIntegration).toHaveBeenCalledWith(
      "m1"
    );
    // The refetch drops the row, so the tile returns to Not connected.
    expect(
      (await findByTestId("integrations-dot-linear")).getAttribute(
        "data-status"
      )
    ).toBe("idle");
    expect(tileCount(container)).toBe(13);
  });

  it("dims a disabled integration's tile with a neutral dot", async () => {
    servers.push(
      makeServer({
        Service: "linear",
        Status: "connected",
        Enabled: false,
      })
    );

    const { findByTestId } = await renderUI(<IntegrationsPane pane={pane} />);

    const tile = await findByTestId("integrations-tile-linear");
    expect(tile.className).toContain("opacity-60");
    expect(
      (await findByTestId("integrations-dot-linear")).getAttribute(
        "data-status"
      )
    ).toBe("idle");
  });

  it("unsubscribes from mcp:oauth:completed on unmount", async () => {
    const { unmount } = await renderUI(<IntegrationsPane pane={pane} />);

    expect(mockedClient.events.onMcpOAuthCompleted).toHaveBeenCalledTimes(1);
    expect(unsubscribeOAuth).not.toHaveBeenCalled();

    await unmount();
    expect(unsubscribeOAuth).toHaveBeenCalledTimes(1);
  });
});
