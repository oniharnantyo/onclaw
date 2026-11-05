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

/**
 * The avatar's level as a SCALAR phase (task 5.4, design D5): the store
 * selector returns this string, so a streaming text delta — which changes
 * the entries array and the tail entry's identity but never the phase —
 * leaves the subscriber untouched. `deriveAvatarState` is the object-forming
 * companion for callers that want the level itself.
 */
export type AvatarPhase =
  | "listening"
  | "idle"
  | "error"
  | "question"
  | "speaking"
  | "tool_call"
  | "thinking";

/** Phase → level table. The objects are shared constants, so the same phase
 * yields the same identity across renders (the avatar element's prop
 * memoisation relies on it). */
const PHASE_STATE: Record<AvatarPhase, AvatarState> = {
  listening: { agentState: "listening", fx: null },
  idle: { agentState: "idle", fx: null },
  error: { agentState: "error", fx: null },
  question: { agentState: "listening", fx: "question" },
  speaking: { agentState: "speaking", fx: null },
  tool_call: { agentState: "tool_call", fx: null },
  thinking: { agentState: "thinking", fx: null },
};

/** Derives the avatar's phase in precedence order: dispatching (sent, no
 * event yet) → listening; no active turn → resting on the transcript tail
 * (an error entry before the next user message holds the error face, else
 * idle); in flight → an unresolved approval anywhere in the turn, then the
 * active turn's tail (error / running tool or task / streaming speech /
 * streaming reasoning), else thinking between steps. */
export function avatarPhase(input: AvatarStateInput): AvatarPhase {
  // 1. A sent message with no event yet: listening for the turn to start.
  if (input.dispatching) return "listening";

  // 2. Resting: the tail says which face the session ended on.
  if (input.activeTurnID === null) {
    for (let i = input.entries.length - 1; i >= 0; i -= 1) {
      const entry = input.entries[i];

      if (entry.kind === "error") return "error";

      if (entry.kind === "user") break;
    }

    return "idle";
  }

  // 3. In flight, over the active turn's entries only.
  const active = turnEntries(input.entries, input.activeTurnID);

  // 3a. A pending approval out-ranks everything: the turn is waiting on the
  // user, not working.
  if (active.some(isPendingApproval)) return "question";

  // 3b. The tail's last state wins. 3c. No match (no output yet, between
  // steps, a finished tool at the tail, a plan or compaction entry) falls
  // through to thinking.
  for (let i = active.length - 1; i >= 0; i -= 1) {
    const entry = active[i];

    if (entry.kind === "error") return "error";

    if (
      entry.kind === "tool" &&
      entry.call?.status === ToolStatus.ToolStatusRunning
    ) {
      return "tool_call";
    }

    if (
      entry.kind === "task" &&
      entry.task?.status === ToolStatus.ToolStatusRunning
    ) {
      return "tool_call";
    }

    if (entry.kind === "assistant" && entry.streaming) return "speaking";

    if (entry.kind === "reasoning" && entry.streaming) return "thinking";
  }

  return "thinking";
}

/** The level state for a derived phase (identity-stable per phase). */
export function phaseToAvatarState(phase: AvatarPhase): AvatarState {
  return PHASE_STATE[phase];
}

/** Derives the avatar's level state from the same three store inputs. Kept
 * as the pure object-forming API; the live hook subscribes to the scalar
 * phase instead. */
export function deriveAvatarState(input: AvatarStateInput): AvatarState {
  return phaseToAvatarState(avatarPhase(input));
}
