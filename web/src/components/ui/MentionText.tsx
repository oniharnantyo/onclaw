// @ts-nocheck
import { cx, memberHandle } from "../../lib/helpers";

export function MentionText({ text, members }) {
  if (!text || text.indexOf('@') === -1) return <>{text}</>;
  const handles = {};
  (members || []).forEach((m) => { handles[memberHandle(m)] = m; });
  const parts = text.split(/(@[A-Za-z]+)/g);
  return (
    <>
      {parts.map((p, i) => (
        p.charAt(0) === '@' && handles[p.slice(1).toLowerCase()]
          ? <span key={i} className="rounded-[4px] bg-[color-mix(in_oklab,var(--accent),13%,transparent)] px-1 font-medium text-accent">{p}</span>
          : <span key={i}>{p}</span>
      ))}
    </>
  );
}

