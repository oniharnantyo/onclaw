import { cx } from "../../lib/helpers";

export function SectionLabel({ children, action  }: any) {
  return (
    <div className="mt-4 mb-1 flex items-center justify-between px-2.5">
      <span className="font-mono text-[10px] font-medium uppercase tracking-[0.14em] text-muted">{children}</span>
      {action}
    </div>
  );
}

