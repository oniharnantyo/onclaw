/**
 * Unit tests for the identity avatar's level selector (add-live-agent-avatar
 * design D3): deriveAvatarState resolves the fork's agent state and badge
 * from { entries, activeTurnID, dispatching } — dispatching, the resting
 * tail (an error holds until the next user message; a replayed transcript
 * rests on the same face), and the in-flight tail over the active turn's
 * entries only. Pure derivation, so the table drives it with hand-built
 * entries.
 */
import { describe, expect, it } from "vite-plus/test";

import type { AgentState, EmoteFxName } from "@oniharnantyo/react-nice-avatar";
import { ToolStatus } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { asEntry, type ApprovalEntry } from "../../entries";
import type { Entry } from "../../types";
import { deriveAvatarState } from "../avatarState";

let nextID = 0;

function userEntry(turnID?: string): Entry {
  nextID += 1;

  return {
    kind: "user",
    id: `entry-${nextID}`,
    turnID,
    text: "hello",
    at: "2026-01-01T00:00:00Z",
  };
}

function assistantEntry(turnID: string | undefined, streaming: boolean): Entry {
  nextID += 1;

  return {
    kind: "assistant",
    id: `entry-${nextID}`,
    turnID,
    text: streaming ? "" : "done",
    streaming,
    at: "2026-01-01T00:00:00Z",
  };
}

function reasoningEntry(turnID: string, streaming: boolean): Entry {
  nextID += 1;

  return {
    kind: "reasoning",
    id: `entry-${nextID}`,
    turnID,
    text: streaming ? "" : "thought",
    streaming,
    at: "2026-01-01T00:00:00Z",
  };
}

function toolEntry(turnID: string | undefined, status: ToolStatus): Entry {
  nextID += 1;

  return {
    kind: "tool",
    id: `entry-${nextID}`,
    turnID,
    call: { call_id: `call-${nextID}`, name: "search", status },
    at: "2026-01-01T00:00:00Z",
  };
}

function taskEntry(turnID: string, status: ToolStatus): Entry {
  nextID += 1;

  return {
    kind: "task",
    id: `entry-${nextID}`,
    turnID,
    task: { task_id: `task-${nextID}`, status },
    at: "2026-01-01T00:00:00Z",
  };
}

function errorEntry(turnID?: string): Entry {
  nextID += 1;

  return {
    kind: "error",
    id: `entry-${nextID}`,
    turnID,
    message: "turn failed",
    at: "2026-01-01T00:00:00Z",
  };
}

function approvalEntry(turnID: string, resolved: boolean): Entry {
  nextID += 1;

  const approval: ApprovalEntry = {
    kind: "approval",
    id: `entry-${nextID}`,
    turnID,
    interruptID: `interrupt-${nextID}`,
    command: "rm -rf build",
    resolved,
    at: "2026-01-01T00:00:00Z",
  };

  // SAFETY: the minted record carries the ApprovalEntry fields; kind
  // "approval" rides the Entry seam through asEntry, as applyEvent does.
  return asEntry(approval);
}

interface Row {
  name: string;
  entries: Entry[];
  activeTurnID: string | null;
  dispatching: boolean;
  agentState: AgentState;
  fx: EmoteFxName | null;
}

const rows: Row[] = [
  // Precedence 1: a sent message with no event yet.
  {
    name: "dispatching → listening",
    entries: [],
    activeTurnID: null,
    dispatching: true,
    agentState: "listening",
    fx: null,
  },
  // Precedence 2: resting on the transcript tail.
  {
    name: "empty entries → idle",
    entries: [],
    activeTurnID: null,
    dispatching: false,
    agentState: "idle",
    fx: null,
  },
  {
    name: "resting after a completed turn (user, reasoning done, assistant done) → idle",
    entries: [
      userEntry("t1"),
      reasoningEntry("t1", false),
      assistantEntry("t1", false),
    ],
    activeTurnID: null,
    dispatching: false,
    agentState: "idle",
    fx: null,
  },
  {
    name: "resting with a trailing error → error",
    entries: [userEntry("t1"), assistantEntry("t1", false), errorEntry("t1")],
    activeTurnID: null,
    dispatching: false,
    agentState: "error",
    fx: null,
  },
  {
    name: "error then a later user message → idle (clears)",
    entries: [userEntry("t1"), errorEntry("t1"), userEntry("t2")],
    activeTurnID: null,
    dispatching: false,
    agentState: "idle",
    fx: null,
  },
  // Precedence 3: in flight, over the active turn's entries.
  {
    name: "in flight with no entries for the turn → thinking",
    entries: [userEntry("t1"), assistantEntry("t1", false)],
    activeTurnID: "t2",
    dispatching: false,
    agentState: "thinking",
    fx: null,
  },
  {
    name: "reasoning streaming → thinking",
    entries: [userEntry("t1"), reasoningEntry("t1", true)],
    activeTurnID: "t1",
    dispatching: false,
    agentState: "thinking",
    fx: null,
  },
  {
    name: "assistant streaming → speaking",
    entries: [
      userEntry("t1"),
      reasoningEntry("t1", false),
      assistantEntry("t1", true),
    ],
    activeTurnID: "t1",
    dispatching: false,
    agentState: "speaking",
    fx: null,
  },
  {
    name: "tool running at tail → tool_call",
    entries: [
      userEntry("t1"),
      reasoningEntry("t1", false),
      toolEntry("t1", ToolStatus.ToolStatusRunning),
    ],
    activeTurnID: "t1",
    dispatching: false,
    agentState: "tool_call",
    fx: null,
  },
  {
    name: "task running at tail → tool_call",
    entries: [userEntry("t1"), taskEntry("t1", ToolStatus.ToolStatusRunning)],
    activeTurnID: "t1",
    dispatching: false,
    agentState: "tool_call",
    fx: null,
  },
  {
    name: "tool completed at tail with nothing streaming → thinking",
    entries: [userEntry("t1"), toolEntry("t1", ToolStatus.ToolStatusCompleted)],
    activeTurnID: "t1",
    dispatching: false,
    agentState: "thinking",
    fx: null,
  },
  {
    name: 'unresolved approval anywhere in the turn → listening + fx "question"',
    entries: [
      userEntry("t1"),
      assistantEntry("t1", false),
      approvalEntry("t1", false),
    ],
    activeTurnID: "t1",
    dispatching: false,
    agentState: "listening",
    fx: "question",
  },
  {
    name: "resolved approval → not listening (falls through to the walk)",
    entries: [userEntry("t1"), approvalEntry("t1", true)],
    activeTurnID: "t1",
    dispatching: false,
    agentState: "thinking",
    fx: null,
  },
  {
    name: "in-flight error entry → error",
    entries: [userEntry("t1"), assistantEntry("t1", true), errorEntry("t1")],
    activeTurnID: "t1",
    dispatching: false,
    agentState: "error",
    fx: null,
  },
  {
    name: "a previous turn's running tool entry (different turnID) with an entryless active turn → thinking (isolation)",
    entries: [
      userEntry("t1"),
      toolEntry("t1", ToolStatus.ToolStatusRunning),
      userEntry("t2"),
    ],
    activeTurnID: "t2",
    dispatching: false,
    agentState: "thinking",
    fx: null,
  },
  {
    name: "entries without turnID are ignored in flight",
    entries: [
      toolEntry(undefined, ToolStatus.ToolStatusRunning),
      assistantEntry(undefined, true),
    ],
    activeTurnID: "t1",
    dispatching: false,
    agentState: "thinking",
    fx: null,
  },
  // Replay: a reloaded transcript rests on the face the live view ended on.
  {
    name: "replay: a transcript of a failed turn (user, reasoning, error) → error",
    entries: [userEntry("t1"), reasoningEntry("t1", false), errorEntry("t1")],
    activeTurnID: null,
    dispatching: false,
    agentState: "error",
    fx: null,
  },
  {
    name: "replay: a transcript of a completed turn → idle",
    entries: [
      userEntry("t1"),
      reasoningEntry("t1", false),
      assistantEntry("t1", false),
    ],
    activeTurnID: null,
    dispatching: false,
    agentState: "idle",
    fx: null,
  },
];

function derive(row: Row): ReturnType<typeof deriveAvatarState> {
  return deriveAvatarState({
    entries: row.entries,
    activeTurnID: row.activeTurnID,
    dispatching: row.dispatching,
  });
}

describe("deriveAvatarState", () => {
  for (const row of rows) {
    it(row.name, () => {
      const state = derive(row);

      expect(state.agentState).toBe(row.agentState);
      expect(state.fx).toBe(row.fx);
    });
  }

  it("keeps fx null on every non-approval row (the error badge is the fork's own)", () => {
    for (const row of rows) {
      if (row.fx !== null) continue;

      expect(derive(row).fx).toBeNull();
    }
  });
});
