import { useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { toolCatalog } from "../../lib/toolCatalog";

export function ToolCall({ t, running, approval }: any) {
  const [open, setOpen] = useState(false);
  const [resolving, setResolving] = useState<'approve' | 'deny' | null>(null);
  const bad = !!t.error;

  // A pending shell approval renders a distinct card: the command text plus
  // approve/deny actions wired to the resolution endpoint. After a decision,
  // the card shows its outcome while the pickup poll replaces it with the
  // resumed turn's tool result. Resolved cards render that result directly.
  if (t.approval) {
    const a = t.approval;
    const decided = (a as any).decided as boolean | undefined;
    const resolved = a.resolved === true || a.resolved === false || decided !== undefined;
    const approved = decided !== undefined ? decided : a.approved;
    const decide = async (want: boolean) => {
      if (!approval || resolved || resolving) return;
      setResolving(want ? 'approve' : 'deny');
      try {
        await approval(a.interruptId, want);
        (a as any).decided = want;
      } finally {
        setResolving(null);
      }
    };
    return (
      <div className="mb-2 overflow-hidden rounded-md border border-line bg-[color-mix(in_oklab,var(--accent)_6%,transparent)]" data-od-id={'approval-' + a.interruptId}>
        <div className="flex items-center gap-2 px-2.5 py-1.5">
          <Icon name="terminal" size={13} className="text-meta"/>
          <span className="font-mono text-[12px] text-fg2">execute</span>
          <span className="rounded-full bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] px-2 py-0.5 font-mono text-[10px] text-fg2">approval required</span>
          {resolved && <span className="font-mono text-[10px] text-muted">{approved ? 'approved' : 'denied'}</span>}
        </div>
        <p className="break-all px-2.5 pb-2 font-mono text-[11px] leading-5 text-fg2">{a.command}</p>
        {!resolved && (
          <div className="flex items-center gap-2 border-t border-linesoft px-2.5 py-2">
            <button type="button" onClick={() => decide(true)} disabled={!!resolving}
              className="rounded-md bg-[var(--accent)] px-2.5 py-1 text-[12px] font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-50">
              {resolving === 'approve' ? 'Approving…' : 'Approve'}
            </button>
            <button type="button" onClick={() => decide(false)} disabled={!!resolving}
              className="rounded-md border border-line px-2.5 py-1 text-[12px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] disabled:opacity-50">
              {resolving === 'deny' ? 'Denying…' : 'Deny'}
            </button>
            <span className="text-[11px] text-muted">This command was flagged as dangerous and is paused until reviewed.</span>
          </div>
        )}
      </div>
    );
  }
  return (
    <div className="mb-2 overflow-hidden rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]">
      <button type="button" onClick={() => setOpen(!open)} data-od-id={'tool-' + t.name}
        className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]">
        <Icon name="terminal" size={13} className={bad ? 'text-danger' : 'text-meta'}/>
        <span className={cx('font-mono text-[12px]', bad ? 'text-danger' : 'text-fg2')}>{toolCatalog.displayName(t.name) ?? t.name}</span>
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted">{t.args}</span>
        {running ? (
          <span className="flex items-center gap-1.5 font-mono text-[10px] text-muted">
            <span className="od-dot"/><span className="od-dot"/><span className="od-dot"/>
          </span>
        ) : bad ? (
          <span className="font-mono text-[10px] text-danger">error · {t.ms}ms</span>
        ) : (
          <span className="font-mono text-[10px] text-muted">{t.ms}ms</span>
        )}
        <Icon name="chevright" size={13} className={cx('text-muted transition-transform', open && 'rotate-90')}/>
      </button>
      {open && (
        <div className="border-t border-linesoft px-2.5 py-2 font-mono text-[11px] leading-5">
          <p className="text-muted">tool</p>
          <p className="mb-1.5 text-fg2">{t.name}</p>
          <p className="text-muted">args</p>
          <p className="mb-1.5 text-fg2">{t.args}</p>
          <p className="text-muted">{bad ? 'error' : 'result'}</p>
          {bad ? (
            <p className="text-danger">{t.error}</p>
          ) : t.res ? (
            <p className="text-fg2">{t.res}</p>
          ) : running ? (
            <p className="text-muted">running…</p>
          ) : (
            // No fabricated output: a completed card with no result says so.
            <p className="text-muted">No output returned.</p>
          )}
        </div>
      )}
    </div>
  );
}

