import React from "react";
import { cx } from "../../lib/helpers";

export interface SkillMenuEntry {
  name: string;
  description?: string;
}

export interface SkillMenuGroup {
  label: "System" | "Workspace" | "This agent";
  skills: SkillMenuEntry[];
}

// $ invocation menu — same mechanics as the / and @ menus: filtered by the
// token typed after `$`, arrow-key navigable, pick replaces the token with
// `$name `. Disabled/absent skills never list.
export function SkillMenu({ groups, query, idx, onPick }: {
  groups: SkillMenuGroup[];
  query: string;
  idx: number;
  onPick: (name: string) => void;
}) {
  const q = query.toLowerCase();
  const visible = groups
    .map((g) => ({ ...g, skills: g.skills.filter((s) => s.name.toLowerCase().startsWith(q)) }))
    .filter((g) => g.skills.length > 0);
  const flat = visible.flatMap((g) => g.skills);
  if (flat.length === 0) return null;

  let running = -1;
  return (
    <div className="od-pop absolute bottom-full left-0 right-0 mb-2 overflow-hidden rounded-md border border-line bg-warm shadow-[var(--elev-raised)]" data-od-id="skill-menu" data-testid="skill-menu">
      <div className="border-b border-linesoft px-3 py-1.5 font-mono text-[10px] uppercase tracking-wider text-muted">
        Skills — invoke with $name
      </div>
      {visible.map((g) => (
        <div key={g.label}>
          <div className="bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-3 py-1 font-mono text-[10px] uppercase tracking-wider text-muted">
            {g.label}
          </div>
          {g.skills.map((s) => {
            running += 1;
            const i = running;
            return (
              <button
                key={g.label + "-" + s.name}
                type="button"
                onMouseDown={(e) => {
                  e.preventDefault();
                  onPick(s.name);
                }}
                data-testid={"skill-option-" + s.name}
                className={cx(
                  "flex w-full items-center gap-3 px-3 py-2 text-left",
                  i === idx ? "bg-[color-mix(in_oklab,var(--accent)_15%,transparent)]" : "hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]"
                )}
              >
                <span className="font-mono text-[13px] text-accent">${s.name}</span>
                <span className="truncate text-[12px] text-muted">{s.description}</span>
              </button>
            );
          })}
        </div>
      ))}
    </div>
  );
}
