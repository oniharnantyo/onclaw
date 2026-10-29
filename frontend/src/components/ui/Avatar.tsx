/**
 * Avatar (onclaw-v2 parity, design D3): image → generated face → initials,
 * in that order. All tones ride the ported tokens (`color-mix` over
 * `var(--accent)` / `var(--fg)` / `var(--bg)`), so both themes re-skin
 * without a re-render. `kind` picks the initials tone; `size` is square
 * pixels and also drives the initials font size. With `live` (design D2,
 * spec chat-panels "Identity view live avatar") the generated face animates
 * and the wrapper stops clipping; absent, the render is the static one.
 */
import { useState, useMemo } from "react";
import type { ReactElement } from "react";
import NiceAvatar, { genConfig } from "@oniharnantyo/react-nice-avatar";
import type {
  AgentState,
  AvatarFullConfig,
  AvatarRefHandle,
  EmoteFxName,
  LookAtMode,
  NiceAvatarProps,
} from "@oniharnantyo/react-nice-avatar";

import { cn } from "../../lib/cn";

/**
 * The live-animation props the identity view passes (design D2, spec
 * chat-panels "Identity view live avatar"). Present, the fork element arms
 * its timers, badges and gaze; absent, the render is exactly the static
 * one — the fork injects no style and arms no timer.
 */
export interface LiveAvatarProps {
  agentState: AgentState;
  fx?: EmoteFxName | null;
  lookAt?: LookAtMode;
  sleepAfter?: number;
  /** Receives the fork's imperative handle (nod/shake/showFx/wake…) on mount and null on unmount. */
  onHandle?: (handle: AvatarRefHandle | null) => void;
}

export interface AvatarProps {
  name?: string;
  src?: string | null;
  /** Generated-face config (react-nice-avatar); wins over the initials. */
  avatar?: AvatarFullConfig | null;
  /** Alias kept from onclaw-v2's call sites; same contract as `avatar`. */
  avatarProps?: AvatarFullConfig | null;
  kind?: "agent" | "you" | "other" | "user";
  size?: number;
  className?: string;
  /** Live-animation pass-through (design D2); absent keeps the static render. */
  live?: LiveAvatarProps;
}

export function Avatar({
  name,
  src,
  avatar,
  avatarProps,
  kind = "other",
  size = 28,
  className,
  live,
}: AvatarProps): ReactElement {
  const [imgError, setImgError] = useState(false);

  const avatarConfig = useMemo(() => {
    const config = avatar || avatarProps;

    return config && Object.keys(config).length > 0 ? genConfig(config) : null;
  }, [avatar, avatarProps]);

  if (src && !imgError) {
    return (
      <img
        src={src}
        alt={name || "Avatar"}
        onError={() => setImgError(true)}
        className={cn("flex shrink-0 rounded-md object-cover", className)}
        style={{ width: size, height: size }}
      />
    );
  }

  if (avatarConfig) {
    // The silhouette knob is react-nice-avatar's own (a borrowed name, set
    // by member access) so call-site configs can't flatten the kit's
    // rounded-square look.
    const niceProps: NiceAvatarProps = {
      style: { width: size, height: size },
      ...avatarConfig,
    };

    niceProps.shape = "rounded";

    // Live path (design D2): the fork's keys land AFTER the config spread so
    // they always win, and none of them touch the element when `live` is
    // absent — the fork gates its timers and injected styles on
    // `animated`/`idle`/`fx`/a non-idle state, so a static surface pays
    // nothing. `expression`/`idle` are never set.
    if (live) {
      niceProps.animated = true;
      niceProps.secondaryMotion = true;
      niceProps.agentState = live.agentState;
      niceProps.fx = live.fx ?? null;
      niceProps.lookAt = live.lookAt;

      if (live.sleepAfter !== undefined) {
        niceProps.sleepAfter = live.sleepAfter;
      }
    }

    return (
      <div
        className={cn(
          live
            ? "flex shrink-0 rounded-md items-center justify-center"
            : "flex shrink-0 overflow-hidden rounded-md items-center justify-center",
          className
        )}
        style={{ width: size, height: size }}
        data-avatar-state={live?.agentState}
        data-avatar-fx={live?.fx ?? undefined}
      >
        {/* The handle is a callback ref: the fork's default export is a class
         * implementing AvatarRefHandle, so the instance satisfies it on mount
         * and the ref fires with null on unmount (design D2). */}
        <NiceAvatar {...niceProps} ref={live?.onHandle} />
      </div>
    );
  }

  const words = String(name || "?")
    .trim()
    .split(/\s+/);

  const init = (
    words.length > 1 ? words[0][0] + words[1][0] : words[0].slice(0, 2)
  ).toUpperCase();

  const tone =
    kind === "agent"
      ? "bg-[color-mix(in_oklab,var(--accent)_16%,transparent)] text-accent"
      : kind === "you"
        ? "bg-[color-mix(in_oklab,var(--fg)_88%,transparent)] text-[var(--bg)]"
        : "bg-[color-mix(in_oklab,var(--fg)_9%,transparent)] text-fg";

  return (
    <div
      className={cn(
        "flex shrink-0 items-center justify-center rounded-md font-semibold",
        tone,
        className
      )}
      style={{ width: size, height: size, fontSize: size <= 24 ? 10 : 11 }}
    >
      {init}
    </div>
  );
}
