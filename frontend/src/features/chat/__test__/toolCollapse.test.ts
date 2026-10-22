/**
 * collapse.ts tests (task 3.5, design D9): cards start collapsed, the
 * reader's toggle survives re-renders (module map, not React state),
 * edit_file/write_file auto-open once on the running→completed edge, an
 * explicit close wins over any later automatic open, and the map stays
 * bounded (LRU ~800).
 */
import { beforeEach, describe, expect, it } from "vite-plus/test";

import {
  COLLAPSE_MAP_LIMIT,
  isOpen,
  recordCallStatus,
  resetCollapseMap,
  setOpen,
} from "../tools/collapse";

beforeEach(resetCollapseMap);

describe("collapse basics", () => {
  it("cards start collapsed (a missing entry reads closed)", () => {
    expect(isOpen("c1")).toBe(false);
  });

  it("a toggle survives re-renders because the map is module state", () => {
    setOpen("c1", true);
    expect(isOpen("c1")).toBe(true);
    // A re-render re-reads the same map; nothing resets it.
    setOpen("c1", true);
    expect(isOpen("c1")).toBe(true);

    setOpen("c1", false);
    expect(isOpen("c1")).toBe(false);
  });
});

describe("auto-open (design D9)", () => {
  it("edit_file auto-opens once on the running→completed edge", () => {
    expect(recordCallStatus("c-edit", "edit_file", "running")).toBe(false);
    expect(isOpen("c-edit")).toBe(false);

    // The edge: the card opens exactly once.
    expect(recordCallStatus("c-edit", "edit_file", "completed")).toBe(true);
    expect(isOpen("c-edit")).toBe(true);

    // The automatic open is spent: a re-report of the terminal state
    // (duplicate event, refresh) does not toggle the card.
    expect(recordCallStatus("c-edit", "edit_file", "completed")).toBe(false);
    setOpen("c-edit", false);
    expect(recordCallStatus("c-edit", "edit_file", "completed")).toBe(false);
    expect(isOpen("c-edit")).toBe(false);
  });

  it("write_file auto-opens too; read_file never does", () => {
    recordCallStatus("c-write", "write_file", "running");
    expect(recordCallStatus("c-write", "write_file", "completed")).toBe(true);
    expect(isOpen("c-write")).toBe(true);

    recordCallStatus("c-read", "read_file", "running");
    expect(recordCallStatus("c-read", "read_file", "completed")).toBe(false);
    expect(isOpen("c-read")).toBe(false);
  });

  it("a reloaded completed record (no prior running observation) never auto-opens", () => {
    expect(recordCallStatus("c-replay", "edit_file", "completed")).toBe(false);
    expect(isOpen("c-replay")).toBe(false);
  });

  it("an explicit close wins: the closed card stays closed on re-report", () => {
    recordCallStatus("c-explicit", "edit_file", "running");
    recordCallStatus("c-explicit", "edit_file", "completed");
    expect(isOpen("c-explicit")).toBe(true);

    // The reader closes by hand…
    setOpen("c-explicit", false);

    // …and the same call later re-reports its terminal state.
    expect(recordCallStatus("c-explicit", "edit_file", "completed")).toBe(
      false
    );
    expect(isOpen("c-explicit")).toBe(false);
  });
});

describe("the LRU bound", () => {
  it("evicts the least recently used past the limit", () => {
    for (let i = 0; i < COLLAPSE_MAP_LIMIT; i += 1)
      recordCallStatus(`k${i}`, "read_file", "completed");

    // Touch the oldest key so it becomes newest.
    setOpen("k0", true);

    // One more entry evicts the (now) oldest untouched key instead of k0.
    recordCallStatus("k-new", "read_file", "completed");

    expect(isOpen("k0")).toBe(true);
    expect(isOpen("k1")).toBe(false);
  });
});
