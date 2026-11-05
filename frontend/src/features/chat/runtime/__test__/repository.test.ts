/**
 * Per-entry repository conversion tests (task 5.2, design D4): a streaming
 * delta re-converts only the group it changed — every message derived from
 * reference-equal entries stays the SAME object across builds, and the
 * tail's unchanged parts survive as the same objects — and the memoized
 * builder's repository deep-equals the pre-memo whole-array builder (ported
 * verbatim below as the equivalence golden) across linear, variant,
 * override, streaming, tool, approval, compaction, artifacts and legacy
 * turn-less fixtures.
 */
import { describe, expect, it } from "vite-plus/test";

import {
  ExportedMessageRepository,
  type ThreadMessage,
  type ThreadMessageLike,
} from "@assistant-ui/react";

import {
  Kind,
  ToolStatus,
  type Event as SessionEvent,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import {
  applyEvent,
  asRichUser,
  type ChatEntry,
  type Entry,
} from "../../entries";
import { buildRepository, type ActiveVariants } from "../branches";
import { groupEntries, groupMessage, type EntryGroup } from "../mapping";

function ev(
  kind: SessionEvent["kind"],
  extra: Partial<SessionEvent> = {}
): SessionEvent {
  return { kind, occurred_at: "2026-01-01T00:00:00Z", ...extra };
}

function fold(...events: SessionEvent[]): Entry[] {
  return events.reduce<Entry[]>(
    (entries: Entry[], e: SessionEvent) => applyEvent(entries, e),
    []
  );
}

/** One plain answered turn. */
function turn(turnID: string, text: string): SessionEvent[] {
  return [
    ev(Kind.KindUserMessage, { text: `prompt ${turnID}`, turn_id: turnID }),
    ev(Kind.KindAssistantMessage, { text, turn_id: turnID }),
    ev(Kind.KindTurnCompleted, { turn_id: turnID }),
  ];
}

/** One regeneration of `original`: a marker + the alternative turn. */
function regen(original: string, turnID: string, text: string): SessionEvent[] {
  return [
    ev(Kind.KindUserMessage, { turn_id: turnID, variant_of: original }),
    ev(Kind.KindAssistantMessage, {
      text,
      turn_id: turnID,
      variant_of: original,
    }),
    ev(Kind.KindTurnCompleted, { turn_id: turnID, variant_of: original }),
  ];
}

/** The text parts of one message, in order (assertions read content). */
function textsOf(message: ThreadMessage): (string | undefined)[] {
  const texts: (string | undefined)[] = [];

  for (const part of message.content) {
    if (part.type === "text") texts.push(part.text);
  }

  return texts;
}

// ---------------------------------------------------------------------------
// The equivalence golden: the pre-memo whole-array builder, ported verbatim.
// It groups, re-parents and heads exactly as the shipped builder did, and
// assembles through the library's fromBranchableArray — so comparing the
// memoized builder against it pins parity of grouping, ordering, parent
// links, headId and normalization.
// ---------------------------------------------------------------------------

interface LegacySlot {
  readonly turnID: string;
  readonly index: number;
}

interface LegacyAnchor {
  readonly anchorID: string;
  readonly originalTurnID: string | undefined;
  readonly slots: readonly LegacySlot[];
  readonly active: number;
}

function legacyVariantAnchors(
  groups: readonly EntryGroup[],
  activeVariants: ActiveVariants | undefined
): LegacyAnchor[] {
  const anchors: LegacyAnchor[] = [];

  for (const group of groups) {
    if (group.kind !== "user") continue;

    const lead = group.entries[0];

    if (lead.kind !== "user") continue;

    const rich = asRichUser(lead);
    const slotTurns = rich.variantTurns;

    if (slotTurns === undefined || slotTurns.length === 0) continue;

    const slots = slotTurns.map(
      (turnID: string, index: number): LegacySlot => ({ turnID, index })
    );

    const override =
      rich.turnID !== undefined ? activeVariants?.get(rich.turnID) : undefined;

    const resolved = override ?? rich.activeVariant ?? slots.length - 1;

    anchors.push({
      anchorID: group.id,
      originalTurnID: rich.turnID,
      slots,
      active: Math.max(0, Math.min(resolved, slots.length - 1)),
    });
  }

  return anchors;
}

function legacyBuildRepository(
  entries: readonly ChatEntry[],
  activeVariants?: ActiveVariants
): ExportedMessageRepository {
  const groups = groupEntries(entries);
  const anchors = legacyVariantAnchors(groups, activeVariants);

  const anchorByTurn = new Map<string, LegacyAnchor>();

  for (const anchor of anchors) {
    for (const slot of anchor.slots) anchorByTurn.set(slot.turnID, anchor);
  }

  const items: {
    message: ThreadMessageLike;
    parentId: string | null;
  }[] = [];

  const originalParent = new Map<string, string | null>();
  const messageIDByTurn = new Map<string, string>();
  let previousID: string | null = null;

  for (const group of groups) {
    const message = groupMessage(group);
    let parentId = previousID;

    if (group.kind === "turn" && group.turnID !== undefined) {
      messageIDByTurn.set(group.turnID, message.id ?? group.id);

      const anchor = anchorByTurn.get(group.turnID);

      if (anchor !== undefined) {
        if (group.turnID === anchor.originalTurnID) {
          originalParent.set(group.turnID, parentId);
        } else {
          const fallback =
            anchor.originalTurnID !== undefined
              ? originalParent.get(anchor.originalTurnID)
              : undefined;

          parentId = fallback !== undefined ? fallback : anchor.anchorID;
        }
      }
    }

    items.push({ message, parentId });
    previousID = message.id ?? null;
  }

  let head: string | null = previousID;
  const lastAnchor = anchors[anchors.length - 1];

  if (lastAnchor !== undefined) {
    const slot = lastAnchor.slots[lastAnchor.active];

    const id =
      slot !== undefined ? messageIDByTurn.get(slot.turnID) : undefined;

    if (id !== undefined) head = id;
  }

  return ExportedMessageRepository.fromBranchableArray(items, { headId: head });
}

// ---------------------------------------------------------------------------
// Fixtures.
// ---------------------------------------------------------------------------

const streamed = fold(
  ev(Kind.KindUserMessage, { text: "q", turn_id: "t1" }),
  ev(Kind.KindReasoningDelta, { text: "think", turn_id: "t1" }),
  ev(Kind.KindTextDelta, { text: "Hel", turn_id: "t1" })
);

const streamedGrown = applyEvent(
  streamed,
  ev(Kind.KindTextDelta, { text: "lo", turn_id: "t1" })
);

const toolTurn = fold(
  ev(Kind.KindUserMessage, { text: "q", turn_id: "t1" }),
  ev(Kind.KindReasoning, { text: "plan", turn_id: "t1" }),
  ev(Kind.KindToolActivity, {
    turn_id: "t1",
    tool: {
      call_id: "c1",
      name: "read_file",
      arguments: '{"path":"a.txt"}',
      status: ToolStatus.ToolStatusCompleted,
      result: "ok",
    },
  }),
  ev(Kind.KindTextDelta, { text: "done", turn_id: "t1" }),
  ev(Kind.KindTurnError, { turn_id: "t1", error: "boom" })
);

const approvalTurn = fold(
  ev(Kind.KindUserMessage, { text: "q", turn_id: "t1" }),
  ev(Kind.KindApprovalRequired, {
    turn_id: "t1",
    approval: { interrupt_id: "i1", command: "rm -rf /" },
  }),
  ev(Kind.KindApprovalResolved, {
    turn_id: "t1",
    approval: { interrupt_id: "i1", command: "rm -rf /", outcome: "approved" },
  })
);

const compactionSplit = fold(
  ...turn("t1", "before"),
  ev(Kind.KindCompaction, {
    compaction: {
      checkpoint_id: "cp1",
      tokens_before: 100,
      tokens_after: 10,
    },
  }),
  ...turn("t2", "after")
);

const artifactTurn = fold(
  ev(Kind.KindUserMessage, { text: "q", turn_id: "t1" }),
  ev(Kind.KindAssistantMessage, { text: "wrote it", turn_id: "t1" }),
  ev(Kind.KindTurnCompleted, {
    turn_id: "t1",
    artifacts: [{ path: "out.md", size: 12 }],
  })
);

/** Records persisted before turn stamping: turn-less adjacency runs. */
const legacyRuns = fold(
  ev(Kind.KindUserMessage, { text: "old question" }),
  ev(Kind.KindAssistantMessage, { text: "old answer" })
);

const variants = fold(...turn("t1", "one"), ...regen("t1", "t2", "two"));

const override: ActiveVariants = new Map([["t1", 0]]);

// ---------------------------------------------------------------------------

describe("delta identity across builds (task 5.2, design D4)", () => {
  it("a delta keeps every unchanged group's message reference-equal", () => {
    const base = fold(
      ...turn("t1", "one"),
      ev(Kind.KindUserMessage, { text: "q2", turn_id: "t2" }),
      ev(Kind.KindTextDelta, { text: "mid", turn_id: "t2" }),
      ev(Kind.KindToolActivity, {
        turn_id: "t2",
        tool: { call_id: "c1", name: "read_file", arguments: "{}" },
      }),
      ev(Kind.KindTextDelta, { text: " tail", turn_id: "t2" })
    );

    const before = buildRepository(base);

    const grown = applyEvent(
      base,
      ev(Kind.KindTextDelta, { text: "!", turn_id: "t2" })
    );

    const after = buildRepository(grown);

    expect(after.messages).toHaveLength(before.messages.length);
    expect(after.headId).toBe(before.headId);

    // Groups 0..n-2 derive from reference-equal entries: the library is
    // handed the very objects it already holds, parent links included.
    for (let i = 0; i < before.messages.length - 1; i += 1) {
      expect(after.messages[i].message).toBe(before.messages[i].message);
      expect(after.messages[i].parentId).toBe(before.messages[i].parentId);
    }

    const tailBefore = before.messages[before.messages.length - 1];
    const tailAfter = after.messages[after.messages.length - 1];

    // Only the group the delta touched re-converts — fresh message, same id.
    expect(tailAfter.message).not.toBe(tailBefore.message);
    expect(tailAfter.message.id).toBe(tailBefore.message.id);
    expect(textsOf(tailBefore.message)).toEqual(["mid", " tail"]);
    expect(textsOf(tailAfter.message)).toEqual(["mid", " tail!"]);

    // The tail's unchanged members keep their converted parts: the "mid"
    // text part is the same object in both builds (entry-level part memo +
    // the library's text-part passthrough).
    expect(tailAfter.message.content[0]).toBe(tailBefore.message.content[0]);
  });
});

describe("memoized builder equivalence (task 5.2, design D4)", () => {
  it("matches the legacy whole-array builder on every fixture shape", () => {
    const fixtures: [string, Entry[], ActiveVariants?][] = [
      ["linear", fold(...turn("t1", "one"), ...turn("t2", "two"))],
      ["variants", variants],
      ["variants + override", variants, override],
      ["streaming", streamed],
      ["streaming grown", streamedGrown],
      ["tool + error turn", toolTurn],
      ["approval turn", approvalTurn],
      ["compaction split", compactionSplit],
      ["artifacts", artifactTurn],
      ["legacy turn-less runs", legacyRuns],
    ];

    for (const [name, entries, activeVariants] of fixtures) {
      // Memoized build first, so every fixture after the first runs against
      // a populated memo — a stale reuse would show up as a mismatch here.
      const memoized = buildRepository(entries, activeVariants);

      expect(memoized, name).toEqual(
        legacyBuildRepository(entries, activeVariants)
      );
    }
  });

  it("re-derives a replaced transcript instead of reusing stale messages", () => {
    // Build one transcript, then a structurally different one under the
    // same module memo: element-wise entry identity must refuse the reuse.
    buildRepository(fold(...turn("t1", "one")));

    const other = fold(...turn("t2", "other"), ...turn("t3", "more"));

    expect(buildRepository(other)).toEqual(legacyBuildRepository(other));
  });
});
