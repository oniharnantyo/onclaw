import type { ReactElement } from "react";

import { Avatar } from "./ui/Avatar";
import { avatarProps } from "../lib/avatar";
import type { AgentAvatarProps } from "./types";

/**
 * An agent's face (tasks 4.1 + avatar parity): the agent's stored config
 * (the wizard's AvatarPicker output, persisted verbatim on the row) wins
 * when present; absent or empty falls back to the deterministic id-derived
 * appearance, so pre-picker agents keep their established face everywhere —
 * cards, session rail, topbar. Rendering delegates to the kit Avatar's
 * config path, so the avatar picks up the contract's rounded-surface
 * grammar instead of daisyUI's `avatar` wrapper. `live` (design D2) is
 * forwarded unchanged; only the identity view passes it.
 */
export function AgentAvatar({
  id,
  avatar,
  size = 44,
  live,
}: AgentAvatarProps): ReactElement {
  const stored = avatar && Object.keys(avatar).length > 0 ? avatar : null;

  return (
    <Avatar
      name={id}
      avatar={stored ?? avatarProps(id)}
      size={size}
      live={live}
    />
  );
}
