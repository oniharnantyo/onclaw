/**
 * The identity avatar's live props (spec chat-panels "Identity view live
 * avatar", add-live-agent-avatar design D4–D8): a thin hook beside the pure
 * selector that pairs avatarState's level derivation (D3) with the fork's
 * imperative edges. It reads { entries, activeTurnID, dispatching,
 * lastTurnOutcome } through store selectors, and runs two effects — D4's
 * one-shot reactions on a live terminal outcome (the store mints a fresh
 * object per terminal on the ingest path, so reactions are edges of the
 * object's identity; a reaction landing while the face is unmounted is
 * dropped, never deferred, and the seen-ref starts at the mount-time value
 * so a remount, a session switch, or a reload never replays a gesture) and
 * D5's wake on turn start (sleep out-ranks agentState in the fork, so a
 * dozing face is woken imperatively). D6/D5 constants: the gaze follows the
 * pointer across the window and the face dozes after two idle minutes.
 * D7: under reduced motion the hook returns undefined so the identity face
 * renders through the static path like every other surface — every hook
 * still runs, so outcomes seen while static stay seen. D8: RightBar holds
 * the hook across identity ↔ preview swaps; the returned props are
 * memoised so unrelated store changes never re-render the fork element.
 */
import { useCallback, useEffect, useMemo, useRef } from "react";

import type { AvatarRefHandle } from "@oniharnantyo/react-nice-avatar";

import { Kind } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { LiveAvatarProps } from "../../../components/ui/Avatar";
import { useChatStore } from "../store";
import type { ExtendedChatStore, TurnOutcome } from "../store";
import { useReducedMotion } from "../tools/useReducedMotion";
import { deriveAvatarState } from "./avatarState";

/** How long a reaction's badge fx (sparkle, sweat) stays up (design D4). */
export const FX_DURATION_MS = 2000;

/** Idle inactivity before the identity face dozes off (design D5) — large,
 * but safely below the fork's setTimeout overflow ceiling of 2^31−1 ms. */
export const SLEEP_AFTER_MS = 120_000;

/** The identity view's live avatar props, or undefined when the face must
 * render static (reduced motion, design D7). */
export function useLiveAvatar(): LiveAvatarProps | undefined {
  const entries = useChatStore((s: ExtendedChatStore) => s.entries);
  const activeTurnID = useChatStore((s: ExtendedChatStore) => s.activeTurnID);
  const dispatching = useChatStore((s: ExtendedChatStore) => s.dispatching);

  const lastTurnOutcome = useChatStore(
    (s: ExtendedChatStore) => s.lastTurnOutcome
  );

  const reduced = useReducedMotion();

  // The level state (design D3): pure over the three store inputs.
  const level = useMemo(
    () => deriveAvatarState({ entries, activeTurnID, dispatching }),
    [entries, activeTurnID, dispatching]
  );

  // The fork's imperative handle; the callback is stable so the Avatar's
  // ref never re-fires on unrelated renders.
  const handleRef = useRef<AvatarRefHandle | null>(null);

  const onHandle = useCallback((handle: AvatarRefHandle | null): void => {
    handleRef.current = handle;
  }, []);

  // D4 reactions: edges of the live ingest path only. The seen-ref starts
  // at the mount-time outcome and advances even when the face is unmounted
  // or motion is reduced, so an outcome that happened while static never
  // replays later.
  const seenRef = useRef<TurnOutcome | null>(lastTurnOutcome);

  useEffect((): void => {
    if (lastTurnOutcome === seenRef.current) return;

    seenRef.current = lastTurnOutcome;

    if (lastTurnOutcome === null) return;

    const handle = handleRef.current;

    // The identity view is replaced (a preview tab is open): the reaction
    // is dropped, not deferred (design D4).
    if (!handle) return;

    switch (lastTurnOutcome.kind) {
      case Kind.KindTurnCompleted:
        handle.nod();
        handle.showFx("sparkle", FX_DURATION_MS);
        break;
      case Kind.KindTurnCancelled:
        handle.shake();
        handle.showFx("sweat", FX_DURATION_MS);
        break;
      case Kind.KindTurnError:
        // No badge: the level state already rests on the error face.
        handle.shake();
        break;
    }
  }, [lastTurnOutcome]);

  // D5 wake: the previous run pair starts at the mount values, so the
  // mount itself never wakes.
  const prevRunRef = useRef<{
    dispatching: boolean;
    activeTurnID: string | null;
  }>({ dispatching, activeTurnID });

  useEffect((): void => {
    const prev = prevRunRef.current;

    prevRunRef.current = { dispatching, activeTurnID };

    const started =
      (dispatching && !prev.dispatching) ||
      (activeTurnID !== null && prev.activeTurnID === null);

    if (started) handleRef.current?.wake();
  }, [dispatching, activeTurnID]);

  // Memoised so unrelated store changes re-render the bar without
  // re-rendering the fork element (design D8).
  const live = useMemo<LiveAvatarProps>(
    () => ({
      agentState: level.agentState,
      fx: level.fx,
      lookAt: "global",
      sleepAfter: SLEEP_AFTER_MS,
      onHandle,
    }),
    [level, onHandle]
  );

  // D7: the face is simply not live under reduced motion. Every hook above
  // ran unconditionally, so the effects kept marking outcomes seen.
  if (reduced) return undefined;

  return live;
}
