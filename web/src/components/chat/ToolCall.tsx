import { Fragment, useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { toolCatalog } from "../../lib/toolCatalog";
import {
  blockedByHook,
  createdDocumentURL,
  fieldRows,
  formatLatency,
  formatResult,
  genericRows,
  parseArgs,
  resolveRefName,
  toolFieldSpec,
  toolOneLiner,
  type BlockedByHook,
  type FieldRow,
  type ResultView,
} from "../../lib/toolDisplay";

// Facade members whose args point at snapshot element refs — the only ids
// whose ref chips resolve to element names via sibling cards (design D5).
const REF_ACTION_TOOLS = new Set([
  'browser.click', 'browser.type', 'browser.hover', 'browser.drag', 'browser.select_option',
]);

// Monospace chip shared by the one-liner and expanded field values — the same
// inline-code treatment as markdown bodies (subtle bg, rounded, mono).
const CHIP_BASE = 'rounded-[4px] bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] px-1 py-0.5 font-mono';

function Chip({ children, danger }: any) {
  return <span className={cx(CHIP_BASE, danger ? 'text-danger' : 'text-fg2')}>{children}</span>;
}

// Parse the one-liner's tiny markup (toolDisplay.ts header comment): `x`
// renders as a monospace chip, 'x' as a quoted literal, the rest plain. Text
// content is preserved verbatim — only styling differs. When refOverride
// matches a chip, the resolved element name renders in its place (D5:
// Clicked "Sign in").
function MarkupText({ text, refOverride, danger }: any) {
  return (
    <>
      {text.split(/(`[^`]*`)/g).map((part: string, i: number) => {
        if (part.length > 1 && part.startsWith('`') && part.endsWith('`')) {
          const inner = part.slice(1, -1);
          if (refOverride && inner === refOverride.ref) {
            return <span key={i} className={danger ? 'text-danger' : 'text-fg2'}>"{refOverride.name}"</span>;
          }
          return <Chip key={i} danger={danger}>{inner}</Chip>;
        }
        return (
          <Fragment key={i}>
            {part.split(/('[^']*')/g).map((sub: string, j: number) =>
              sub.length > 1 && sub.startsWith("'") && sub.endsWith("'")
                ? <span key={j} className={danger ? 'text-danger' : 'text-fg2'}>{sub}</span>
                : <span key={j}>{sub}</span>
            )}
          </Fragment>
        );
      })}
    </>
  );
}

const CLAMP_LINES = 10;

// Shared clamp (design D3 / task 3.3): the first CLAMP_LINES lines of a long
// value render, then a "Show all N lines" affordance expands the rest. The
// expanded panel remounts on every open, so state starts collapsed each time.
function Clamp({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const lines = text.split('\n');
  const clipped = !expanded && lines.length > CLAMP_LINES;
  return (
    <div>
      <pre className="whitespace-pre-wrap break-words text-fg2">{clipped ? lines.slice(0, CLAMP_LINES).join('\n') : text}</pre>
      {clipped && (
        <button type="button" onClick={() => setExpanded(true)} className="text-muted transition-colors hover:text-fg2">
          Show all {lines.length} lines
        </button>
      )}
    </div>
  );
}

// One labeled field row's value, shaped by kind (design D3): chips for
// paths/refs/URLs, quoted literals for queries/patterns, clamped blocks for
// content, plain text otherwise. A ref value resolves to its element name via
// sibling snapshots, falling back to a #ref chip (design D5 / task 3.7);
// refName is undefined for values that are not the card's refs.
function RowValue({ row, refName }: { row: FieldRow; refName?: string | null }) {
  if (refName !== undefined) {
    return refName
      ? <span className="text-fg2">"{refName}"</span>
      : <Chip>#{row.value}</Chip>;
  }
  switch (row.kind) {
    case 'chip':
      return <Chip>{row.value}</Chip>;
    case 'quote':
      return <span className="text-fg2">'{row.value}'</span>;
    case 'content':
      return <Clamp text={row.value}/>;
    default:
      return <span className="text-fg2">{row.value}</span>;
  }
}

// edit_file before/after (task 3.6): stacked red/green blocks — danger tint
// for the replaced text, accent tint for the replacement — both clamped (D3).
function EditDiff({ oldString, newString }: { oldString: string; newString: string }) {
  return (
    <div className="space-y-1.5">
      {oldString !== '' && (
        <div className="rounded-[4px] border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_7%,transparent)] px-1.5 py-1">
          <p className="text-danger">Replaced</p>
          <Clamp text={oldString}/>
        </div>
      )}
      {newString !== '' && (
        <div className="rounded-[4px] border border-[color-mix(in_oklab,var(--accent)_35%,transparent)] bg-[color-mix(in_oklab,var(--accent)_7%,transparent)] px-1.5 py-1">
          <p className="text-accent">Replacement</p>
          <Clamp text={newString}/>
        </div>
      )}
    </div>
  );
}

// Hostname of a search-result URL; '' when unparseable (relative/broken).
function hostOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return '';
  }
}

// Result shaping per envelope kind (design D4 / task 3.5).
function ResultBody({ view }: { view: ResultView }) {
  switch (view.kind) {
    case 'empty':
      // No fabricated output: a completed card with no result says so.
      return <p className="text-muted">No output returned.</p>;
    case 'text':
      return <Clamp text={view.text}/>;
    case 'list':
      return (
        <ul className="ml-4 list-disc space-y-0.5">
          {view.items.map((item: string, i: number) => <li key={i} className="text-fg2">{item}</li>)}
        </ul>
      );
    case 'search_results':
      return (
        <ol className="ml-4 list-decimal space-y-0.5">
          {view.results.map((r: { title: string; url: string }, i: number) => {
            const host = hostOf(r.url);
            return (
              <li key={i} className="text-fg2">
                {r.title || r.url || '(untitled)'}
                {host && <span className="text-muted"> — {host}</span>}
              </li>
            );
          })}
        </ol>
      );
    case 'snapshot':
      return (
        <div className="od-scroll overflow-x-auto">
          <table className="text-left">
            <thead>
              <tr className="text-muted">
                <th className="pr-3 font-normal">Role</th>
                <th className="pr-3 font-normal">Name</th>
                <th className="pr-3 font-normal">Ref</th>
              </tr>
            </thead>
            <tbody>
              {view.elements.map((e: { ref: string; role: string; name: string }, i: number) => (
                <tr key={i}>
                  <td className="pr-3 text-fg2">{e.role}</td>
                  <td className="pr-3 text-fg2">{e.name}</td>
                  <td className="pr-3 text-muted">{e.ref ? `#${e.ref}` : ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case 'snapshot_refresh':
      // Every ref action returns a fresh snapshot — one line, not a table (D4).
      return <p className="text-muted">Snapshot refreshed — {view.elementCount} elements</p>;
    case 'kv':
      return (
        <>
          {view.rows.map((row: FieldRow, i: number) => (
            <div key={i} className="mb-1.5">
              <p className="text-muted">{row.label}</p>
              <RowValue row={row}/>
            </div>
          ))}
        </>
      );
  }
}

export function ToolCall({ t, running, approval, siblings }: any) {
  const [open, setOpen] = useState(false);
  const [raw, setRaw] = useState(false);
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
        {/* D7: the command renders through the shell card's one-liner markup so
            approval → resolved-execute reads as one continuous story. */}
        <p className="break-all px-2.5 pb-2 font-mono text-[11px] leading-5 text-fg2">
          <MarkupText text={a.command}/>
        </p>
        {!resolved && (
          <div className="flex items-center gap-2 border-t border-linesoft px-2.5 py-2">
            <button type="button" onClick={() => decide(true)} disabled={!!resolving}
              className="rounded-md bg-[var(--accent)] px-2.5 py-1 text-[12px] font-medium text-accenton transition-opacity hover:opacity-90 disabled:opacity-50">
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

  // Formatted-view ingredients — pure parses that tolerate mid-stream cards
  // (absent/partial args degrade to the raw-string fallback paths).
  const parsed = parseArgs(t.args);
  // Hook-blocked call (integrate-agent-hooks D3): the block arrives as the
  // tool result envelope the model reads — detect it once, style the whole
  // card from it. A real error (bad) wins; a blocked call is not an error.
  const blocked: BlockedByHook | null = bad ? null : blockedByHook(t.res);
  const spec = toolFieldSpec(t.name);
  const argRows: FieldRow[] | null = parsed
    ? (spec ? fieldRows(t.name, parsed) : genericRows(parsed))
    : null;
  // This card's own refs (D5): raw ref value → resolved element name, null
  // when no sibling snapshot names it. Only ref-action tools carry refs.
  const refNames = new Map<string, string | null>();
  if (parsed && REF_ACTION_TOOLS.has(t.name)) {
    for (const key of ['ref', 'to_ref']) {
      const v = (parsed as any)[key];
      if (v !== undefined && v !== null && String(v) !== '') {
        refNames.set(String(v), resolveRefName(siblings, String(v)));
      }
    }
  }
  const headerRefName = parsed && refNames.has(String((parsed as any).ref))
    ? refNames.get(String((parsed as any).ref)) ?? null
    : null;
  // One-liner state machine (D2): intent while running, outcome when done,
  // intent styled as error on failure; null mints nothing (unknown/MCP tools,
  // unparseable args) — the header then shows just name + indicators.
  const oneLiner = toolOneLiner(t.name, { args: t.args, res: t.res, error: bad ? true : undefined }, running);
  const refOverride = headerRefName ? { ref: String((parsed as any).ref), name: headerRefName } : null;
  // document.create delivery (add-document-create-tool D6): a successful
  // envelope carries the capability URL the transcript's download link uses.
  const downloadURL = t.name === 'document.create' && !bad && !running ? createdDocumentURL(t.res) : null;
  // edit_file's content rows become the stacked diff (task 3.6).
  const oldString = typeof (parsed as any)?.old_string === 'string' ? (parsed as any).old_string : '';
  const newString = typeof (parsed as any)?.new_string === 'string' ? (parsed as any).new_string : '';
  const started = t.ts && !Number.isNaN(new Date(t.ts).getTime()) ? new Date(t.ts).toLocaleTimeString() : null;

  return (
    <div
      className={cx(
        'mb-2 overflow-hidden rounded-md border',
        blocked
          ? // Hook-blocked card (integrate-agent-hooks): red-tinted, marked.
            'border-[color-mix(in_oklab,var(--danger)_40%,transparent)] bg-[color-mix(in_oklab,var(--danger)_6%,transparent)]'
          : 'border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]'
      )}
    >
      <button type="button" onClick={() => setOpen(!open)} aria-expanded={open} data-od-id={'tool-' + t.name}
        className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]">
        <Icon name={toolCatalog.icon(t.name) ?? 'terminal'} size={13} className={bad || blocked ? 'text-danger' : 'text-meta'}/>
        <span className={cx('font-mono text-[12px]', bad || blocked ? 'text-danger' : 'text-fg2')}>{toolCatalog.displayName(t.name) ?? t.name}</span>
        {blocked && (
          <span className="shrink-0 rounded-full bg-[color-mix(in_oklab,var(--danger)_14%,transparent)] px-2 py-0.5 font-mono text-[10px] text-danger">
            blocked
          </span>
        )}
        {/* Human-readable one-liner (task 3.1); aria-live announces the
            running→done swap for screen readers. A hook-blocked card shows
            the enforcing hook + reason in the one-liner slot instead. */}
        <span aria-live="polite" className={cx('min-w-0 flex-1 truncate font-mono text-[11px]', bad || blocked ? 'text-danger' : 'text-muted')}>
          {blocked ? (
            <>Blocked by {blocked.hook ? <Chip danger>{blocked.hook}</Chip> : 'a hook'}{blocked.reason ? ` — ${blocked.reason}` : ''}</>
          ) : oneLiner ? <MarkupText text={oneLiner} refOverride={refOverride} danger={bad}/> : null}
        </span>
        {running ? (
          <span className="flex items-center gap-1.5 font-mono text-[10px] text-muted">
            <span className="od-dot"/><span className="od-dot"/><span className="od-dot"/>
          </span>
        ) : bad ? (
          <span className="font-mono text-[10px] text-danger">error · {formatLatency(t.ms)}</span>
        ) : (
          <span className="font-mono text-[10px] text-muted">{formatLatency(t.ms)}</span>
        )}
        <Icon name="chevright" size={13} className={cx('text-muted transition-transform', open && 'rotate-90')}/>
      </button>
      {open && (
        <div className="border-t border-linesoft px-2.5 py-2 font-mono text-[11px] leading-5">
          <div className="mb-1.5 flex items-center justify-between gap-2">
            {/* Wall-clock start (cron turns matter); only when ts is present. */}
            {started ? <span className="text-muted">Started {started}</span> : <span/>}
            {/* D6: the raw view is the escape hatch — strings exactly as delivered. */}
            <button type="button" onClick={() => setRaw(!raw)} aria-pressed={raw}
              title={raw ? 'Show formatted view' : 'Show raw JSON'}
              className={cx('shrink-0 rounded-[4px] px-1.5 py-0.5 transition-colors', raw
                ? 'bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg2'
                : 'text-muted hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg2')}>
              {'{ } raw'}
            </button>
          </div>
          {raw ? (
            <>
              <p className="text-muted">tool</p>
              <p className="mb-1.5 break-all text-fg2">{t.name}</p>
              <p className="text-muted">args</p>
              <p className="mb-1.5 break-all text-fg2">{t.args}</p>
              <p className="text-muted">{bad ? 'error' : 'result'}</p>
              {bad ? (
                <p className="text-danger">{t.error}</p>
              ) : t.res ? (
                <p className="break-all text-fg2">{t.res}</p>
              ) : running ? (
                <p className="text-muted">running…</p>
              ) : (
                <p className="text-muted">No output returned.</p>
              )}
            </>
          ) : (
            <>
              <p className="text-muted">tool</p>
              <p className="mb-1.5 text-fg2">{t.name}</p>
              {argRows ? (
                <>
                  {/* edit_file renders its two content rows as the stacked
                      before/after diff instead (task 3.6); the File chip row
                      and any appended unknown keys stay (D3, no silent drops). */}
                  {(t.name === 'edit_file'
                    ? argRows.filter((r) => r.label !== 'Replaced' && r.label !== 'Replacement')
                    : argRows
                  ).map((row: FieldRow, i: number) => (
                    <div key={i} className="mb-1.5">
                      <p className="text-muted">{row.label}</p>
                      <RowValue row={row} refName={row.kind === 'chip' ? refNames.get(row.value) : undefined}/>
                    </div>
                  ))}
                  {t.name === 'edit_file' && (oldString !== '' || newString !== '') && (
                    <div className="mb-1.5">
                      <EditDiff oldString={oldString} newString={newString}/>
                    </div>
                  )}
                </>
              ) : (
                // Unparseable args fall back to the raw string (today's view).
                <>
                  <p className="text-muted">args</p>
                  <p className="mb-1.5 break-all text-fg2">{t.args}</p>
                </>
              )}
              <p className="text-muted">{bad ? 'error' : 'result'}</p>
              {bad ? (
                <p className="text-danger">{t.error}</p>
              ) : blocked ? (
                // Hook enforcement in place of a result (D3): the reason the
                // model was given, rendered for humans. Raw view keeps the
                // verbatim envelope.
                <>
                  <p className="text-danger">Blocked by hook{blocked.hook ? ` ${blocked.hook}` : ''}</p>
                  <p className="break-words text-fg2">{blocked.reason || 'The call was prevented by a policy hook.'}</p>
                </>
              ) : running && !t.res ? (
                <p className="text-muted">running…</p>
              ) : (
                <ResultBody view={formatResult(t.name, t.res)}/>
              )}
              {/* Download affordance on created documents (add-document-create-tool
                  5.2): the capability URL stamped on the result envelope, served by
                  the same path as attachment downloads. */}
              {downloadURL && (
                <div className="mt-1.5">
                  <a href={downloadURL} download
                    className="inline-flex items-center gap-1 rounded-[4px] bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] px-1.5 py-0.5 text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--accent)_22%,transparent)]">
                    <Icon name="down" size={12}/>
                    Download document
                  </a>
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
