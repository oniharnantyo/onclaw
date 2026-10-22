/**
 * Prompt-documents cache tests (design D7, task 6.2): one fetch per agent —
 * sequential and concurrent loads share the first read, distinct agents
 * fetch separately, a failed read stays uncached so the next open retries,
 * and the synchronous cache read answers immediately once warm.
 */
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type { PromptDocuments } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";
import { mockedSurface } from "./mockedSurface";

function docsFor(fill: string): PromptDocuments {
  return {
    Identity: fill.repeat(10),
    IdentityExists: true,
    Soul: "",
    SoulExists: false,
    Bootstrap: "",
    BootstrapExists: false,
  };
}

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  agents: { getPromptDocuments: MockFn };
};

function makeClient(): MockedClient {
  const c = {
    agents: {
      getPromptDocuments: vi.fn(
        async (agentID: string): Promise<PromptDocuments> => docsFor(agentID)
      ),
    },
  };

  // SAFETY: the literal covers exactly the surface this suite drives; the
  // Client-typed intersection defeats the structural check through the
  // shared relabeling bridge the other suites funnel through.
  return mockedSurface<MockedClient, object>(c);
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../lib/client";

import {
  cachedPromptDocuments,
  loadPromptDocuments,
  resetPromptDocumentsCache,
} from "../lib/promptDocs";

// Typed alias exposing the vi.fn mock behind the Client-shaped export.
// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedClient = client as MockedClient;

/** The mocked read, accessed through its owner so the method stays bound. */
function getPromptDocuments(): MockFn {
  return mockedClient.agents.getPromptDocuments;
}

beforeEach(() => {
  resetPromptDocumentsCache();
  getPromptDocuments().mockClear();
  getPromptDocuments().mockImplementation(
    async (agentID: string): Promise<PromptDocuments> => docsFor(agentID)
  );
});

describe("promptDocs cache", () => {
  it("fetches an agent's documents once across sequential loads", async () => {
    const first = await loadPromptDocuments("a1");
    const second = await loadPromptDocuments("a1");

    expect(getPromptDocuments()).toHaveBeenCalledTimes(1);
    expect(getPromptDocuments()).toHaveBeenCalledWith("a1");
    expect(second).toEqual(first);
  });

  it("serves concurrent loads of one agent from a single fetch", async () => {
    const [a, b, c] = await Promise.all([
      loadPromptDocuments("a1"),
      loadPromptDocuments("a1"),
      loadPromptDocuments("a1"),
    ]);

    expect(getPromptDocuments()).toHaveBeenCalledTimes(1);
    expect(a).toEqual(b);
    expect(b).toEqual(c);
  });

  it("fetches distinct agents separately", async () => {
    await loadPromptDocuments("a1");
    await loadPromptDocuments("a2");

    expect(getPromptDocuments()).toHaveBeenCalledTimes(2);
    expect(getPromptDocuments()).toHaveBeenCalledWith("a2");
  });

  it("answers from the synchronous cache once warm", async () => {
    expect(cachedPromptDocuments("a1")).toBeNull();

    const loaded = await loadPromptDocuments("a1");

    expect(cachedPromptDocuments("a1")).toEqual(loaded);
  });

  it("does not cache a failed read, so the next load retries", async () => {
    getPromptDocuments().mockRejectedValueOnce(new Error("backend down"));

    await expect(loadPromptDocuments("a1")).rejects.toThrow("backend down");
    expect(cachedPromptDocuments("a1")).toBeNull();

    const retried = await loadPromptDocuments("a1");

    expect(getPromptDocuments()).toHaveBeenCalledTimes(2);
    expect(retried).toEqual(docsFor("a1"));
  });

  it("shares one fetch among concurrent callers even while it fails", async () => {
    getPromptDocuments().mockRejectedValueOnce(new Error("backend down"));

    const attempts: Promise<PromptDocuments>[] = [
      loadPromptDocuments("a1"),
      loadPromptDocuments("a1"),
    ];

    await expect(attempts[0]).rejects.toThrow("backend down");
    await expect(attempts[1]).rejects.toThrow("backend down");
    expect(getPromptDocuments()).toHaveBeenCalledTimes(1);
  });
});
