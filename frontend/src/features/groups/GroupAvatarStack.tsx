/**
 * Group avatar stack (round 8): the open group's member roster as a row of
 * overlapping circular faces — the header's at-a-glance roster in the
 * stacked-avatar convention. Each face clips to a circle and carries a
 * separation ring in the surface tone, playing the role the convention's
 * background-colored outline plays, so overlapping faces read as distinct.
 * A member whose task is currently running trades the surface ring for a
 * green (success) one — the header's quiet "who is working" signal riding
 * the same live task rows as the transcript's activity line (design D9;
 * queued members stay surface-ringed — waiting is not working). Pure
 * render: the host derives the roster and the running set from the store.
 * Left-over-right stacking (each face tucks under its predecessor), with
 * the ring widths outside the clip so no face eats its neighbor's border.
 */
import type { ReactElement } from "react";

import type { Agent } from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { AgentAvatar } from "../../components";

export function GroupAvatarStack({
  roster,
  runningIDs,
  size = 22,
}: {
  roster: Agent[];
  runningIDs: ReadonlySet<string>;
  size?: number;
}): ReactElement {
  return (
    <div
      className="flex items-center"
      role="img"
      aria-label={`Group members: ${roster.map((a: Agent) => a.Name).join(", ")}`}
      data-testid="group-avatar-stack"
    >
      {roster.map((a: Agent, i: number) => {
        const running = runningIDs.has(a.ID);

        return (
          <span
            key={a.ID}
            title={running ? `${a.Name} is working` : a.Name}
            data-testid={`group-avatar-stack-item-${a.ID}`}
            data-running={running ? "true" : "false"}
            className={[
              "relative inline-flex overflow-hidden rounded-full ring-2",
              running ? "ring-success" : "ring-surface",
              i > 0 ? "-ms-2" : "",
            ]
              .filter(Boolean)
              .join(" ")}
            style={{ zIndex: roster.length - i }}
          >
            <AgentAvatar id={a.ID} avatar={a.Avatar} size={size} />
          </span>
        );
      })}
    </div>
  );
}
