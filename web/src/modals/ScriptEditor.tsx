import { useEffect, useRef, useState } from 'react';
import { loadScriptEditor, type ScriptEditorHandle, type ScriptEditorMount } from './scriptEditorLoader';

export interface ScriptEditorProps {
  value: string;
  onChange: (next: string) => void;
  /** Parse-state signal: a syntax error blocks save (D23), mirroring the
   * server's compile-check 422 before any request is made. */
  onSyntaxError: (hasError: boolean) => void;
}

// Parse-only syntax check for the textarea fallback: `new Function` compiles
// the body without ever running it — the same compile-don't-execute contract
// as the server's goja.Compile save check, and the same `(function(input){ … })`
// wrapping, so a top-level `return` is valid.
function checkSyntax(source: string): { line: number; column: number } | null {
  try {
    new Function('input', source);
    return null;
  } catch (err) {
    const e = err as { lineNumber?: number; columnNumber?: number };
    return { line: e.lineNumber ?? 1, column: e.columnNumber ?? 1 };
  }
}

const hostCls =
  'od-scroll w-full overflow-hidden rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] font-mono text-[13px] text-fg2 focus-within:border-accent';
const fallbackCls =
  'od-scroll h-auto w-full resize-y rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 py-2 font-mono text-[13px] leading-5 text-fg2 placeholder:text-muted focus:border-accent outline-none';

// ScriptEditor is the D23 editor for hook `config.script`: CodeMirror 6 with
// line numbers, JS highlighting, and parse squiggles, loaded via dynamic
// import so the main chunk never grows. Until the chunk resolves (or if it
// fails to load) a plain mono textarea wired to the same props keeps the
// field working and still parse-checks via `new Function`.
export function ScriptEditor({ value, onChange, onSyntaxError }: ScriptEditorProps) {
  const [mount, setMount] = useState<ScriptEditorMount | null>(null);
  const hostRef = useRef<HTMLDivElement>(null);
  const handleRef = useRef<ScriptEditorHandle | null>(null);
  // Latest callbacks for the mounted view's update listener without
  // re-mounting the editor on every parent render.
  const cbRef = useRef({ onChange, onSyntaxError });
  cbRef.current = { onChange, onSyntaxError };

  useEffect(() => {
    let cancelled = false;
    // A rejected load keeps the textarea fallback — the field stays usable.
    loadScriptEditor().then(
      (m) => {
        if (!cancelled) setMount(() => m);
      },
      () => {}
    );
    return () => {
      cancelled = true;
      handleRef.current?.destroy();
      handleRef.current = null;
    };
  }, []);

  // Mount the view once the host div exists (after `mount` resolves).
  useEffect(() => {
    if (!mount || !hostRef.current || handleRef.current) return;
    handleRef.current = mount(hostRef.current, {
      value,
      onChange: (next) => cbRef.current.onChange(next),
      onSyntaxError: (hasError) => cbRef.current.onSyntaxError(hasError),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- mount once; value flows through setDoc below
  }, [mount]);

  // External value changes (e.g. the Format write-back) replace the document.
  useEffect(() => {
    handleRef.current?.setDoc(value);
  }, [value, mount]);

  // Fallback mode: parse-check on every value change (and the initial value),
  // driving the same onSyntaxError seam the CodeMirror linter drives.
  useEffect(() => {
    if (mount) return;
    cbRef.current.onSyntaxError(checkSyntax(value) !== null);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- recheck per value change while on the fallback
  }, [value, mount]);

  if (mount) {
    return <div ref={hostRef} className={hostCls} data-testid="input-hook-script" />;
  }
  return (
    <textarea
      data-testid="input-hook-script"
      aria-label="Script"
      rows={8}
      spellCheck={false}
      className={fallbackCls}
      placeholder="// input = the event: tool, agent, origin, status"
      value={value}
      onChange={(e) => cbRef.current.onChange(e.target.value)}
    />
  );
}
