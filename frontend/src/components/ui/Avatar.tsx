/**
 * Avatar (onclaw-v2 parity, design D3): image → generated face → initials,
 * in that order. All tones ride the ported tokens (`color-mix` over
 * `var(--accent)` / `var(--fg)` / `var(--bg)`), so both themes re-skin
 * without a re-render. `kind` picks the initials tone; `size` is square
 * pixels and also drives the initials font size.
 */
import { useState, useMemo } from "react";
import type { ReactElement } from "react";
import NiceAvatar, { genConfig } from "react-nice-avatar";
import type { AvatarFullConfig, NiceAvatarProps } from "react-nice-avatar";

import { cn } from "../../lib/cn";

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
}

export function Avatar({
  name,
  src,
  avatar,
  avatarProps,
  kind = "other",
  size = 28,
  className,
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

    return (
      <div
        className={cn(
          "flex shrink-0 overflow-hidden rounded-md items-center justify-center",
          className
        )}
        style={{ width: size, height: size }}
      >
        <NiceAvatar {...niceProps} />
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
