/**
 * Per-agent prompt-documents cache (design D7, task 6.2): the Instructions
 * segment of the context breakdown reads the agent's three workspace prompt
 * documents, fetched once per agent and cached at module level — the store
 * is out of this wave's file scope, so the cache lives here keyed by agent
 * id. Concurrent loads for one agent share a single in-flight promise; a
 * failed read is not cached, so a later open retries.
 */
import type { PromptDocuments } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { client } from "../../../lib/client";

/** Documents per agent id, cached after the first successful read. */
const cache = new Map<string, PromptDocuments>();

/** In-flight loads per agent id, so concurrent callers share one fetch. */
const inflight = new Map<string, Promise<PromptDocuments>>();

/** The cached documents for an agent, or null before the first read. */
export function cachedPromptDocuments(agentID: string): PromptDocuments | null {
  return cache.get(agentID) ?? null;
}

/**
 * Loads the agent's prompt documents, fetching at most once per agent:
 * a cache hit resolves immediately, a shared in-flight promise serves
 * concurrent callers, and only the first successful read is stored. The
 * read chains optionally — a client surface without the member (narrow
 * mocked surfaces, a transport regression) rejects instead of crashing
 * the caller.
 */
export function loadPromptDocuments(agentID: string): Promise<PromptDocuments> {
  const hit = cache.get(agentID);

  if (hit) return Promise.resolve(hit);

  const pending = inflight.get(agentID);

  if (pending) return pending;

  const load = Promise.resolve(client.agents?.getPromptDocuments?.(agentID))
    .then((docs: PromptDocuments | undefined): PromptDocuments => {
      if (!docs) throw new Error("prompt documents read unavailable");

      cache.set(agentID, docs);
      inflight.delete(agentID);

      return docs;
    })
    .catch((error: Error): never => {
      // A failed read stays uncached: the next open retries instead of
      // pinning the Instructions segment at zero forever.
      inflight.delete(agentID);
      throw error;
    });

  inflight.set(agentID, load);

  return load;
}

/** Drops every cached and in-flight read (test seam — the cache is
 * module-level, so suites must not leak agents between tests). */
export function resetPromptDocumentsCache(): void {
  cache.clear();
  inflight.clear();
}
