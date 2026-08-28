import { Icon } from "./Icon";

export function LastRunCell({ last  }: any) {
  if (!last) return <span className="text-[12px] text-muted">never</span>;
  const icon = last.status === 'success' ? <Icon name="check" size={13} className="text-[color-mix(in_oklab,var(--success),black_25%)]"/>
    : last.status === 'failed' ? <Icon name="x" size={13} className="text-danger"/>
    : <Icon name="clock" size={13} className="text-muted"/>;
  return <span className="flex items-center gap-1.5 text-[12px] text-fg2">{icon}<span className="font-mono text-[11px] text-muted">{last.when} · {last.dur}</span></span>;
}

