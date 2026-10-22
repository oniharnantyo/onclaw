/**
 * Branch tree tests (task 2.2, design D3): a transcript without variants is
 * a flat linear chain headed at the newest message; a regenerated turn's
 * assistant message becomes a sibling of the original under the anchor user
 * message; and the head sits on the active variant — the newest slot by
 * default, an older one after the store's setActiveVariant (either through
 * the anchor's activeVariant or the explicit override map).
 */
import { describe, expect, it } from "vite-plus/test";

import {
  Kind,
  type Event as SessionEvent,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { applyEvent, asEntry, asRichUser, type Entry } from "../../entries";
import { entriesToRepository, type ActiveVariants } from "../branches";

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

/** The parent of each message, by message id. */
function parentsOf(repository: {
  messages: { message: { id?: string }; parentId: string | null }[];
}): Map<string, string | null | undefined> {
  const parents = new Map<string, string | null | undefined>();

  for (const item of repository.messages) {
    parents.set(item.message.id ?? "", item.parentId);
  }

  return parents;
}

describe("variant branch tree (task 2.2, design D3)", () => {
  it("chains a transcript without variants linearly, head at the newest", () => {
    const repository = entriesToRepository(
      fold(...turn("t1", "one"), ...turn("t2", "two"))
    );

    expect(repository.headId).toBe("t2");
    expect(repository.messages).toHaveLength(4);

    const parents = parentsOf(repository);
    const firstPrompt = repository.messages[0]?.message.id;
    const secondPrompt = repository.messages[2]?.message.id;

    // Each assistant hangs off its own prompt; the turns chain through them.
    expect(parents.get("t1")).toBe(firstPrompt);
    expect(parents.get("t2")).toBe(secondPrompt);
  });

  it("makes a regenerated turn a sibling of the original under the anchor", () => {
    const repository = entriesToRepository(
      fold(...turn("t1", "original"), ...regen("t1", "t2", "second"))
    );

    // The two alternatives are siblings under the user message; the head
    // sits on the newest variant by default.
    expect(repository.headId).toBe("t2");

    const parents = parentsOf(repository);
    const anchorID = repository.messages[0]?.message.id;

    expect(parents.get("t1")).toBe(anchorID);
    expect(parents.get("t2")).toBe(anchorID);
  });

  it("heads the newest of three variants by default", () => {
    const repository = entriesToRepository(
      fold(
        ...turn("t1", "one"),
        ...regen("t1", "t2", "two"),
        ...regen("t1", "t3", "three")
      )
    );

    expect(repository.headId).toBe("t3");

    const parents = parentsOf(repository);
    const anchorID = repository.messages[0]?.message.id;

    expect(parents.get("t1")).toBe(anchorID);
    expect(parents.get("t2")).toBe(anchorID);
    expect(parents.get("t3")).toBe(anchorID);
  });

  it("heads an older variant after the store's setActiveVariant", () => {
    const entries = fold(
      ...turn("t1", "one"),
      ...regen("t1", "t2", "two"),
      ...regen("t1", "t3", "three")
    );

    // setActiveVariant("t1", 0) writes the anchor's activeVariant; the
    // branch module reads the anchor state directly.
    const switched = entries.map((entry: Entry): Entry => {
      if (entry.kind !== "user" || entry.turnID !== "t1") return entry;

      return asEntry({ ...asRichUser(entry), activeVariant: 0 });
    });

    expect(entriesToRepository(switched).headId).toBe("t1");

    // An explicit override resolves the same way for callers that hold it.
    const override: ActiveVariants = new Map([["t1", 1]]);

    expect(entriesToRepository(entries, override).headId).toBe("t2");
  });
});
