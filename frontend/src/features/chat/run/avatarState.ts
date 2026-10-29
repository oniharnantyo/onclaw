/**
 * The identity avatar's level state (spec chat-panels "Identity view live
 * avatar", add-live-agent-avatar design D3): the fork's AgentState and badge
 * derived purely from the store's { entries, activeTurnID, dispatching }.
 * Everything is a function of `entries`, which the fold rebuilds identically
 * on reload — so "error holds until the next user message" is a derivation,
 * not a remembered flag, and a replayed transcript rests on the same state
 * the live view ended on.
 */
import type { AgentState, EmoteFxName } from "@oniharnantyo/react-nice-avatar";

import { ToolStatus } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { asApproval } from "../entries";
import type { Entry } from "../types";

/** The store state the avatar's level derives from. */
export interface AvatarStateInput {
  entries: readonly Entry[];
  activeTurnID: string | null;
  dispatching: boolean;
}

/** The avatar's level state: the fork's agent state and badge fx. */
export interface AvatarState {
  agentState: AgentState;
  /** The fork's badge union ("question", "sparkle", …); null = no badge.
   * The error state's exclamation badge is the fork's own, so fx stays null
   * there. */
  fx: EmoteFxName | null;
}

/** True while the entry is an approval card the backend has not resolved
 * yet — the seam's widened kind, read the way hasPendingApproval does. */
function isPendingApproval(entry: Entry): boolean {
  // SAFETY: only applyEvent mints kind "approval" (ApprovalEntry); the
  // widened read keeps the literal-union comparison legal.
  const kind: string = entry.kind;

  return kind === "approval" && !asApproval(entry).resolved;
}

/** The active turn's entries: turn-tagged entries only. Entries without a
 * turnID (compaction, payload rows) and other turns' entries are ignored, so
 * a stale running tool from an earlier turn cannot leak into the live face. */
function turnEntries(entries: readonly Entry[], turnID: string): Entry[] {
  const active: Entry[] = [];

  for (const entry of entries) {
    // SAFETY: compaction entries carry no turn id; the cast keeps the union
    // access legal (only turn-tagged kinds declare the field).
    const entryTurn = (entry as { turnID?: string }).turnID;

    if (entryTurn === turnID) active.push(entry);
  }

  return active;
}

/** Derives the avatar's level state in precedence order: dispatching (sent,
 * no event yet) → listening; no active turn → resting on the transcript tail
 * (an error entry before the next user message holds the error face, else
 * idle); in flight → an unresolved approval anywhere in the turn, then the
 * active turn's tail (error / running tool or task / streaming speech /
 * streaming reasoning), else thinking between steps. */
export function deriveAvatarState(input: AvatarStateInput): AvatarState {
  // 1. A sent message with no event yet: listening for the turn to start.
  if (input.dispatching) return { agentState: "listening", fx: null };

  // 2. Resting: the tail says which face the session ended on.
  if (input.activeTurnID === null) {
    for (let i = input.entries.length - 1; i >= 0; i -= 1) {
      const entry = input.entries[i];

      if (entry.kind === "error") return { agentState: "error", fx: null };

      if (entry.kind === "user") break;
    }

    return { agentState: "idle", fx: null };
  }

  // 3. In flight, over the active turn's entries only.
  const active = turnEntries(input.entries, input.activeTurnID);

  // 3a. A pending approval out-ranks everything: the turn is waiting on the
  // user, not working.
  if (active.some(isPendingApproval)) {
    return { agentState: "listening", fx: "question" };
  }

  // 3b. The tail's last state wins. 3c. No match (no output yet, between
  // steps, a finished tool at the tail, a plan or compaction entry) falls
  // through to thinking.
  for (let i = active.length - 1; i >= 0; i -= 1) {
    const entry = active[i];

    if (entry.kind === "error") return { agentState: "error", fx: null };

    if (
      entry.kind === "tool" &&
      entry.call?.status === ToolStatus.ToolStatusRunning
    ) {
      return { agentState: "tool_call", fx: null };
    }

    if (
      entry.kind === "task" &&
      entry.task?.status === ToolStatus.ToolStatusRunning
    ) {
      return { agentState: "tool_call", fx: null };
    }

    if (entry.kind === "assistant" && entry.streaming) {
      return { agentState: "speaking", fx: null };
    }

    if (entry.kind === "reasoning" && entry.streaming) {
      return { agentState: "thinking", fx: null };
    }
  }

  return { agentState: "thinking", fx: null };
}
