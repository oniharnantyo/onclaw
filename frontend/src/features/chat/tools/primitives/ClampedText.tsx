/**
 * ClampedText (task 3.4): text panes clamp at ten lines behind a "Show all"
 * control (spec chat-tool-cards "Tool card anatomy": output longer than ten
 * lines SHALL be clamped). Mono, whitespace-preserving — arguments, results,
 * and fetched text all ride it.
 */
import { useState } from "react";
import type { ReactElement } from "react";

/** The clamp height (spec: ten lines). */
export const CLAMP_LINES = 10;

export interface ClampedTextProps {
  text: string;
  testidPrefix?: string;
  className?: string;
}

export function ClampedText({
  text,
  testidPrefix = "clamped",
  className,
}: ClampedTextProps): ReactElement {
  const [expanded, setExpanded] = useState(false);

  const lines = text.replace(/\n$/, "").split("\n");
  const clamped = !expanded && lines.length > CLAMP_LINES;
  const shown = clamped ? lines.slice(0, CLAMP_LINES) : lines;

  return (
    <div>
      <pre
        className={`rounded-md bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] p-2 font-mono text-xs break-all whitespace-pre-wrap text-fg2 ${className ?? ""}`}
        data-testid={`${testidPrefix}-text`}
        data-clamped={clamped ? "true" : "false"}
      >
        {shown.join("\n")}
      </pre>
      {clamped ? (
        <button
          type="button"
          className="mt-0.5 rounded px-1.5 py-0.5 text-[11px] text-muted hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
          data-testid={`${testidPrefix}-show-all`}
          onClick={() => setExpanded(true)}
        >
          Show all ({lines.length} lines)
        </button>
      ) : null}
      {expanded && lines.length > CLAMP_LINES ? (
        <button
          type="button"
          className="mt-0.5 rounded px-1.5 py-0.5 text-[11px] text-muted hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
          data-testid={`${testidPrefix}-show-less`}
          onClick={() => setExpanded(false)}
        >
          Show less
        </button>
      ) : null}
    </div>
  );
}
