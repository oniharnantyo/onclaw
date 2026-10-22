/**
 * CodeBlock (task 4.2, design D14): the ONE code-surface primitive —
 * markdown `pre` (task 9.2), write/read file previews, and shell output all
 * render through it. A header with the language label, a copy button (check
 * for 1.2s), and a wrap toggle; optional gutter line numbers (file previews
 * show them, shell output does not); `streaming` keeps the content plain.
 * Highlighting (task 9.2, design D13 verdict B): once `streaming` is false
 * and a language is known, the source is highlighted through the lazy Shiki
 * loader (shikiHighlight.ts — dynamic imports only, nothing in the entry
 * chunk); until its promise resolves, and whenever it degrades to `null`
 * (unknown language, loader failure), the block stays plain — highlighting
 * never blocks the first render or crashes the block. Dual-theme token
 * colors ride highlight.css against the app's data-theme switch.
 * Self-contained on purpose: nothing here imports feature state.
 */
import { Fragment, useEffect, useRef, useState } from "react";
import type { CSSProperties, ReactElement, ReactNode } from "react";

import { cn } from "../../../../lib/cn";
import {
  highlightTokens,
  tokenStyle,
  type HighlightedLine,
} from "./shikiHighlight";
import "./highlight.css";

export interface CodeBlockProps {
  code: string;
  /** Language tag for the header label ("go", "sh", …); also the highlight
   * grammar hint — unknown tags just stay plain. */
  language?: string;
  /** Header label override when there is no language ("output"). */
  label?: string;
  /** While the source is streaming the content stays plain (design D14). */
  streaming?: boolean;
  /** Renders a gutter with 1-based line numbers (file previews). */
  lineNumbers?: boolean;
  /** Start line for the gutter (read_file windows begin past line 1). */
  startLine?: number;
  testidPrefix?: string;
  className?: string;
}

export function CodeBlock({
  code,
  language,
  label,
  streaming = false,
  lineNumbers = false,
  startLine = 1,
  testidPrefix = "code-block",
  className,
}: CodeBlockProps): ReactElement {
  const [copied, setCopied] = useState(false);
  const [wrapped, setWrapped] = useState(false);
  const [tokens, setTokens] = useState<HighlightedLine[] | null>(null);
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect((): (() => void) => {
    return (): void => {
      if (copyTimer.current) clearTimeout(copyTimer.current);
    };
  }, []);

  // Lazy highlighting (task 9.2): only once streaming has ended; a code or
  // language change drops the previous tokens first, so stale spans for
  // older source never paint while the new grammar loads.
  useEffect((): (() => void) | undefined => {
    setTokens(null);

    if (streaming || language === undefined || code === "") return;

    let alive = true;

    void highlightTokens(code, language).then(
      (result: HighlightedLine[] | null): void => {
        if (alive) setTokens(result);
      }
    );

    return (): void => {
      alive = false;
    };
  }, [code, language, streaming]);

  const lines = code.replace(/\n$/, "").split("\n");
  // Tokens only ever paint for settled (non-streaming) source; the null
  // checks make an out-of-sync array fall back to the plain line.
  const highlighted = streaming ? null : tokens;

  const lineContent = (i: number): ReactNode => {
    const line = lines[i] ?? "";
    const hl = highlighted?.[i];

    if (hl !== undefined && hl.length > 0) {
      return hl.map(
        (token: HighlightedLine[number], j: number): ReactElement => {
          // SAFETY: tokenStyle yields custom properties ("--shiki-*") plus CSS
          // font styles from the grammar — a valid CSSProperties when cast.
          const style = tokenStyle(token) as CSSProperties;

          return (
            <span key={j} className="shiki-token" style={style}>
              {token.content}
            </span>
          );
        }
      );
    }

    return line === "" ? " " : line;
  };

  const copy = (): void => {
    // The write rides the click gesture; a refusal (or a host without the
    // clipboard API) leaves the button unchanged rather than surfacing a
    // toast from a code pane.
    const clipboard = navigator.clipboard;

    if (!clipboard) return;

    void clipboard.writeText(code).then(
      (): void => {
        setCopied(true);

        if (copyTimer.current) clearTimeout(copyTimer.current);
        copyTimer.current = setTimeout((): void => setCopied(false), 1200);
      },
      (): void => {
        // Clipboard refused: no state change, no crash.
      }
    );
  };

  return (
    <div
      className={cn(
        "overflow-hidden rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]",
        className
      )}
      data-testid={testidPrefix}
      data-streaming={streaming ? "true" : "false"}
      data-highlighted={highlighted !== null ? "true" : "false"}
    >
      <div className="flex items-center gap-2 border-b border-line px-2 py-1">
        <span
          className="min-w-0 flex-1 truncate font-mono text-[10px] uppercase tracking-wide text-muted"
          data-testid={`${testidPrefix}-label`}
        >
          {label ?? language ?? "text"}
          {streaming ? " · streaming" : ""}
        </span>
        <button
          type="button"
          aria-pressed={wrapped}
          title="Toggle line wrap"
          className="rounded px-1.5 py-0.5 text-[10px] text-muted hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
          data-testid={`${testidPrefix}-wrap`}
          onClick={() => setWrapped((w: boolean): boolean => !w)}
        >
          {wrapped ? "No wrap" : "Wrap"}
        </button>
        <button
          type="button"
          title="Copy to clipboard"
          className="rounded px-1.5 py-0.5 text-[10px] text-muted hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
          data-testid={`${testidPrefix}-copy`}
          onClick={copy}
        >
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <div className="max-h-64 overflow-auto">
        <pre
          className={cn(
            "p-2 font-mono text-xs leading-[1.45] text-fg2",
            wrapped ? "whitespace-pre-wrap break-all" : "whitespace-pre"
          )}
        >
          {lineNumbers ? (
            <table className="w-full border-collapse">
              <tbody>
                {lines.map((line: string, i: number): ReactElement => (
                  <tr key={i}>
                    <td
                      className="select-none pr-2 text-right align-top font-mono text-[10px] leading-[1.45] text-muted"
                      data-testid={`${testidPrefix}-lineno`}
                    >
                      {startLine + i}
                    </td>
                    <td className="align-top font-mono text-xs leading-[1.45]">
                      {lineContent(i)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : code === "" ? (
            " "
          ) : highlighted !== null ? (
            <span data-testid={`${testidPrefix}-highlighted`}>
              {lines.map((_line: string, i: number): ReactNode => (
                <Fragment key={i}>
                  {i > 0 ? "\n" : ""}
                  {lineContent(i)}
                </Fragment>
              ))}
            </span>
          ) : (
            code
          )}
        </pre>
      </div>
    </div>
  );
}
