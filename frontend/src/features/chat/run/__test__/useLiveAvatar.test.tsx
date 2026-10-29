/**
 * useLiveAvatar tests (add-live-agent-avatar design D4–D8): the hook pairs
 * avatarState's level derivation with the imperative edges — one-shot
 * reactions on live terminal outcomes only (never on mount, dropped while
 * the face is unmounted, replayed for neither a re-set object nor an
 * outcome seen while static), the wake on turn start, the constants of the
 * gaze/doze pair, and the reduced-motion static path. The store is driven
 * directly with setState and the fork is a fake AvatarRefHandle handed to
 * the hook through its onHandle callback; a probe component exposes the
 * latest return value because @testing-library/react is not installed.
 */
import { act } from "react";
import type { ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type { AvatarRefHandle } from "@oniharnantyo/react-nice-avatar";
import {
  Kind,
  ToolStatus,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { LiveAvatarProps } from "../../../../components/ui/Avatar";
import { renderUI } from "../../__test__/test-utils";
import { useChatStore } from "../../store";
import type { TurnOutcome } from "../../store";
import type { Entry } from "../../types";
import {
  FX_DURATION_MS,
  SLEEP_AFTER_MS,
  useLiveAvatar,
} from "../useLiveAvatar";

let mockReduced = false;

vi.mock("../../tools/useReducedMotion", () => ({
  useReducedMotion: (): boolean => mockReduced,
}));

let latest: LiveAvatarProps | undefined;

/** The probe through which the hook's return value is observed: renderUI
 * renders over react-dom, so a component body is the read point. */
function Probe(): ReactElement | null {
  latest = useLiveAvatar();

  return null;
}

async function renderProbe(): Promise<{
  rerender: (ui: React.ReactElement) => Promise<void>;
}> {
  latest = undefined;

  return renderUI(<Probe />);
}

function attach(handle: AvatarRefHandle): void {
  act(() => {
    latest?.onHandle?.(handle);
  });
}

function fakeHandle(): AvatarRefHandle {
  return {
    nod: vi.fn(),
    shake: vi.fn(),
    sneeze: vi.fn(),
    squash: vi.fn(),
    doubleTake: vi.fn(),
    talk: vi.fn(),
    stopTalking: vi.fn(),
    turn: vi.fn(),
    showFx: vi.fn(),
    clearFx: vi.fn(),
    wake: vi.fn(),
    sleep: vi.fn(),
  };
}

let nextEntry = 0;

function userEntry(turnID: string): Entry {
  nextEntry += 1;

  return {
    kind: "user",
    id: `entry-${nextEntry}`,
    turnID,
    text: "hello",
    at: "2026-01-01T00:00:00Z",
  };
}

function reasoningEntry(turnID: string, streaming: boolean): Entry {
  nextEntry += 1;

  return {
    kind: "reasoning",
    id: `entry-${nextEntry}`,
    turnID,
    text: "",
    streaming,
    at: "2026-01-01T00:00:00Z",
  };
}

function toolEntry(turnID: string, status: ToolStatus): Entry {
  nextEntry += 1;

  return {
    kind: "tool",
    id: `entry-${nextEntry}`,
    turnID,
    call: { call_id: `call-${nextEntry}`, name: "search", status },
    at: "2026-01-01T00:00:00Z",
  };
}

let nextOutcome = 0;

/** A fresh live terminal outcome — the store mints a new object per
 * terminal event, and the hook reacts to the object's identity. */
function outcome(kind: TurnOutcome["kind"]): TurnOutcome {
  nextOutcome += 1;

  return { turnID: `t-${nextOutcome}`, kind, at: "2026-01-01T00:00:00Z" };
}

beforeEach(() => {
  mockReduced = false;

  return act(async () => {
    useChatStore.setState({
      entries: [],
      activeTurnID: null,
      dispatching: false,
      lastTurnOutcome: null,
    });
  });
});

describe("useLiveAvatar", () => {
  it("returns the resting level, the constants, and an onHandle with an empty store", async () => {
    await renderProbe();

    const live = latest;

    expect(live).toBeDefined();
    expect(live?.agentState).toBe("idle");
    expect(live?.fx).toBeNull();
    expect(live?.lookAt).toBe("global");
    expect(live?.sleepAfter).toBe(SLEEP_AFTER_MS);
    expect(live?.onHandle).toBeInstanceOf(Function);
  });

  it("follows the store's level state through the turn lifecycle", async () => {
    await renderProbe();

    await act(async () => {
      useChatStore.setState({
        activeTurnID: "t1",
        entries: [userEntry("t1"), reasoningEntry("t1", true)],
      });
    });
    expect(latest?.agentState).toBe("thinking");

    await act(async () => {
      useChatStore.setState({
        entries: [
          userEntry("t1"),
          toolEntry("t1", ToolStatus.ToolStatusRunning),
        ],
      });
    });
    expect(latest?.agentState).toBe("tool_call");

    await act(async () => {
      useChatStore.setState({
        entries: [],
        activeTurnID: null,
        dispatching: true,
      });
    });
    expect(latest?.agentState).toBe("listening");
  });

  it("never reacts to the outcome that already exists at mount", async () => {
    await act(async () => {
      useChatStore.setState({
        lastTurnOutcome: outcome(Kind.KindTurnCompleted),
      });
    });

    await renderProbe();

    const handle = fakeHandle();
    attach(handle);

    expect(handle.nod).not.toHaveBeenCalled();
    expect(handle.shake).not.toHaveBeenCalled();
    expect(handle.showFx).not.toHaveBeenCalled();
  });

  it("nods with a sparkle on a completed turn", async () => {
    await renderProbe();
    const handle = fakeHandle();
    attach(handle);

    await act(async () => {
      useChatStore.setState({
        lastTurnOutcome: outcome(Kind.KindTurnCompleted),
      });
    });

    expect(handle.nod).toHaveBeenCalledTimes(1);
    expect(handle.showFx).toHaveBeenCalledTimes(1);
    expect(handle.showFx).toHaveBeenCalledWith("sparkle", FX_DURATION_MS);
    expect(handle.shake).not.toHaveBeenCalled();
  });

  it("shakes with a sweat drop on a cancelled turn", async () => {
    await renderProbe();
    const handle = fakeHandle();
    attach(handle);

    await act(async () => {
      useChatStore.setState({
        lastTurnOutcome: outcome(Kind.KindTurnCancelled),
      });
    });

    expect(handle.shake).toHaveBeenCalledTimes(1);
    expect(handle.showFx).toHaveBeenCalledTimes(1);
    expect(handle.showFx).toHaveBeenCalledWith("sweat", FX_DURATION_MS);
    expect(handle.nod).not.toHaveBeenCalled();
  });

  it("shakes without fx on a failed turn — the level state holds the error face", async () => {
    await renderProbe();
    const handle = fakeHandle();
    attach(handle);

    await act(async () => {
      useChatStore.setState({ lastTurnOutcome: outcome(Kind.KindTurnError) });
    });

    expect(handle.shake).toHaveBeenCalledTimes(1);
    expect(handle.showFx).not.toHaveBeenCalled();
    expect(handle.nod).not.toHaveBeenCalled();
  });

  it("reacts to two consecutive completions (two distinct objects)", async () => {
    await renderProbe();
    const handle = fakeHandle();
    attach(handle);

    await act(async () => {
      useChatStore.setState({
        lastTurnOutcome: outcome(Kind.KindTurnCompleted),
      });
    });
    await act(async () => {
      useChatStore.setState({
        lastTurnOutcome: outcome(Kind.KindTurnCompleted),
      });
    });

    expect(handle.nod).toHaveBeenCalledTimes(2);
    expect(handle.showFx).toHaveBeenCalledTimes(2);
  });

  it("ignores a re-set of the same outcome object", async () => {
    await renderProbe();
    const handle = fakeHandle();
    attach(handle);

    const same = outcome(Kind.KindTurnCompleted);

    await act(async () => {
      useChatStore.setState({ lastTurnOutcome: same });
    });
    expect(handle.nod).toHaveBeenCalledTimes(1);

    await act(async () => {
      useChatStore.setState({ lastTurnOutcome: same });
    });
    expect(handle.nod).toHaveBeenCalledTimes(1);
  });

  it("drops a reaction that lands while the face is unmounted, never defers it", async () => {
    await renderProbe();

    // No handle attached: the outcome is dropped.
    await act(async () => {
      useChatStore.setState({
        lastTurnOutcome: outcome(Kind.KindTurnCompleted),
      });
    });

    const handle = fakeHandle();
    attach(handle);

    expect(handle.nod).not.toHaveBeenCalled();
    expect(handle.showFx).not.toHaveBeenCalled();

    // Same after an explicit detach: the next outcome finds a null ref and
    // the freshly attached handle replays nothing.
    const first = fakeHandle();
    attach(first);

    await act(async () => {
      useChatStore.setState({
        lastTurnOutcome: outcome(Kind.KindTurnCompleted),
      });
    });
    expect(first.nod).toHaveBeenCalledTimes(1);

    await act(async () => {
      latest?.onHandle?.(null);
    });
    await act(async () => {
      useChatStore.setState({
        lastTurnOutcome: outcome(Kind.KindTurnCompleted),
      });
    });
    expect(first.nod).toHaveBeenCalledTimes(1);

    const second = fakeHandle();
    attach(second);

    expect(second.nod).not.toHaveBeenCalled();
  });

  it("wakes when dispatching flips true, never on mount", async () => {
    await renderProbe();
    const handle = fakeHandle();
    attach(handle);

    expect(handle.wake).not.toHaveBeenCalled();

    await act(async () => {
      useChatStore.setState({ dispatching: true });
    });

    expect(handle.wake).toHaveBeenCalledTimes(1);
  });

  it("wakes when the active turn starts and not when it ends", async () => {
    await renderProbe();
    const handle = fakeHandle();
    attach(handle);

    expect(handle.wake).not.toHaveBeenCalled();

    await act(async () => {
      useChatStore.setState({ activeTurnID: "t1" });
    });
    expect(handle.wake).toHaveBeenCalledTimes(1);

    await act(async () => {
      useChatStore.setState({ activeTurnID: null });
    });
    expect(handle.wake).toHaveBeenCalledTimes(1);
  });

  it("returns undefined under reduced motion and never replays an outcome seen while static", async () => {
    mockReduced = true;
    const probe = await renderProbe();

    expect(latest).toBeUndefined();

    // The outcome arrives while the face is static: it must be marked seen.
    await act(async () => {
      useChatStore.setState({
        lastTurnOutcome: outcome(Kind.KindTurnCompleted),
      });
    });

    // Motion back on: the SAME hook instance turns live again but replays
    // nothing — the outcome was marked seen while static.
    mockReduced = false;
    await probe.rerender(<Probe />);

    expect(latest).toBeDefined();

    const handle = fakeHandle();
    attach(handle);

    expect(handle.nod).not.toHaveBeenCalled();
    expect(handle.showFx).not.toHaveBeenCalled();
  });

  it("keeps the same live object across renders and unrelated store changes", async () => {
    const probe = await renderProbe();
    const live = latest;

    expect(live).toBeDefined();

    await probe.rerender(<Probe />);
    expect(latest).toBe(live);

    // An outcome change re-renders the bar but must not re-render the fork
    // element: the props object keeps its identity.
    await act(async () => {
      useChatStore.setState({
        lastTurnOutcome: outcome(Kind.KindTurnCompleted),
      });
    });
    expect(latest).toBe(live);
  });
});
