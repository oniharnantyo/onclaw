/**
 * Tools-catalog slice tests (task 3.3, design D3): the settings listing
 * loads once per app run into a name-keyed catalog; a known name resolves
 * its display name and icon key, an unknown/MCP name falls back to the raw
 * name at the read site, and a failed load is retryable by the next call.
 */
import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type { ToolEntry } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";

/** The mutable client seam behind the hoisted vi.mock: each test points
 * `settings.tools` where it needs (a vi.fn so call counts assert). */
const toolsMock = {
  impl: vi.fn(async (): Promise<ToolEntry[]> => []),
};

vi.mock("../../../lib/client", () => ({
  client: {
    settings: {
      tools: (): Promise<ToolEntry[]> => toolsMock.impl(),
    },
  },
}));

/** The settings.tools listing (services.ToolEntry). */
const TOOL_ROWS: ToolEntry[] = [
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
    Description: "",
    Enabled: true,
  },
];

beforeEach(() => {
  toolsMock.impl.mockClear();
  toolsMock.impl.mockImplementation(
    async (): Promise<ToolEntry[]> => TOOL_ROWS
  );
});

describe("toolCatalog slice", () => {
  it("loads the listing once and resolves a known name", async () => {
    const { useChatStore } = await import("../store");
    const { toolDisplayInfo } = await import("../tools/toolDisplay");

    await act(async () => {
      await useChatStore.getState().ensureToolCatalog();
      await useChatStore.getState().ensureToolCatalog();
    });

    const catalog = useChatStore.getState().toolCatalog;

    expect(catalog.read_file).toEqual({
      name: "read_file",
      displayName: "Read File",
      iconKey: "file",
    });
    expect(catalog.browser).toEqual({
      name: "browser",
      displayName: "Browser",
      iconKey: "globe",
    });
    // The one-shot: two ensure calls, one load.
    expect(toolsMock.impl).toHaveBeenCalledTimes(1);

    // Known name → catalog wins; unknown (MCP) name → raw-name fallback.
    expect(toolDisplayInfo("read_file", catalog)).toEqual({
      displayName: "Read File",
      iconKey: "file",
    });
    expect(toolDisplayInfo("mcp__x__y", catalog)).toEqual({
      displayName: "mcp__x__y",
      iconKey: "plug",
    });
  });

  it("a failed load stays empty and the next call retries", async () => {
    vi.resetModules();
    toolsMock.impl.mockImplementation(async (): Promise<ToolEntry[]> => {
      throw new Error("backend down");
    });

    const { useChatStore } = await import("../store");

    await act(async () => {
      await useChatStore.getState().ensureToolCatalog();
    });

    expect(useChatStore.getState().toolCatalog).toEqual({});

    // The failed one-shot reset itself: the next call retries and succeeds.
    toolsMock.impl.mockImplementation(
      async (): Promise<ToolEntry[]> => TOOL_ROWS
    );

    await act(async () => {
      await useChatStore.getState().ensureToolCatalog();
    });

    expect(useChatStore.getState().toolCatalog.read_file).toBeDefined();
  });
});
