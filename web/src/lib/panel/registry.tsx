// Panel source + candidate registries (change add-right-panel, design D2).
// Mirrors the generative-UI registry (lib/generativeUi/registry.tsx): a panel
// source registers a renderer per kind, a panel candidate registers a matcher
// that maps one transcript tool card to `{kind, title, payload}` — or null.
// Built-in sources (members, later file/browser) register through this same
// API the way a plugin would; nothing downstream edits the panel shell.

import type { ReactNode } from "react";

/** One panel tab's immutable content address. `payload` is opaque to the
 * shell; the source registered for `kind` gives it meaning. */
export interface PanelTab {
  id: string;
  kind: string;
  title: string;
  payload: Record<string, unknown>;
  /** kind + stable payload identity — the dedup key (design D1: opening an
   * artifact that already has a tab focuses it instead of duplicating). */
  dedupKey: string;
}

/** What a candidate matcher produces for a matching tool card. */
export interface PanelCandidate {
  kind: string;
  title: string;
  payload: Record<string, unknown>;
}

/** The transcript tool-card shape both live and hydrated paths fold into
 * `agent.tools[]` (lib/livechat.ts — `{callId, name, args, res, ms, error}`,
 * args/res as raw strings). Matchers parse tolerantly; mid-stream cards may
 * carry no result yet. */
export interface PanelCardInput {
  name?: string;
  args?: string;
  res?: string;
  ms?: number;
  error?: unknown;
  [key: string]: unknown;
}

/** Render-time context a source renderer receives: the tab it renders plus
 * the mounting screen's callback/data bundle (e.g. channelMembers for the
 * members source). Sources stay pure renderers; ChatRoute owns the data. */
export interface PanelSourceInput {
  tab: PanelTab;
  ctx: any;
}

export type PanelSourceRenderer = (input: PanelSourceInput) => ReactNode;
export type PanelCandidateMatcher = (card: PanelCardInput) => PanelCandidate | null;

const sourceRenderers = new Map<string, PanelSourceRenderer>();
const candidateMatchers: PanelCandidateMatcher[] = [];

/** Register a source renderer for a kind (plugins call this — no core edits). */
export function registerPanelSource(kind: string, renderer: PanelSourceRenderer): void {
  sourceRenderers.set(kind, renderer);
}

/** Register a candidate matcher. Matchers run in registration order; the
 * first non-null candidate wins. */
export function registerPanelCandidate(matcher: PanelCandidateMatcher): void {
  candidateMatchers.push(matcher);
}

/** Live source registry contents, sorted — read by coverage tests. */
export function registeredPanelSources(): string[] {
  return [...sourceRenderers.keys()].sort();
}

/** Stable JSON key with sorted object keys — payload identity must not depend
 * on property order (same artifact opened twice = same key). */
export function stableKey(value: unknown): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value ?? null);
  if (Array.isArray(value)) return '[' + value.map(stableKey).join(',') + ']';
  const obj = value as Record<string, unknown>;
  return '{' + Object.keys(obj).sort().map((k) => JSON.stringify(k) + ':' + stableKey(obj[k])).join(',') + '}';
}

/** The tab dedup key: kind + stable payload identity (design D1). */
export function panelDedupKey(kind: string, payload: Record<string, unknown>): string {
  return kind + '::' + stableKey(payload);
}

/** First registered candidate matching this tool card, or null. */
export function matchPanelCandidate(card: PanelCardInput): PanelCandidate | null {
  for (const matcher of candidateMatchers) {
    const c = matcher(card);
    if (c) return c;
  }
  return null;
}

/** Collect candidates across a folded transcript (`messages[].tools[]`) —
 * the SAME data both live turns and hydrated history carry, so affordances
 * derived here are identical for either path (design D2). Nulls skipped. */
export function collectPanelCandidates(messages: any[]): PanelCandidate[] {
  const out: PanelCandidate[] = [];
  for (const m of messages || []) {
    for (const card of m?.tools || []) {
      if (!card) continue;
      const c = matchPanelCandidate(card);
      if (c) out.push(c);
    }
  }
  return out;
}

/** Mount decision for one tab: the source registered for its kind renders the
 * content; an unregistered kind mints nothing user-visible (the shell shows
 * its fallback — never raw payload JSON). */
export function renderPanelSource(kind: string, input: PanelSourceInput): ReactNode {
  const renderer = sourceRenderers.get(kind);
  if (!renderer) return null;
  return renderer(input);
}

/** Test seam: clear both registries. Production code never calls this —
 * registrations are module-load side effects. Tests with fake matchers reset
 * between cases so registrations cannot leak across suites. */
export function resetPanelRegistries(): void {
  sourceRenderers.clear();
  candidateMatchers.length = 0;
}
