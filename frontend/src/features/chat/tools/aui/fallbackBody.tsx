/**
 * The aui layer's fallback body + error boundary (task 4.2, spec
 * chat-tool-cards "Honest fallback"): labeled arguments and result sections,
 * pretty-printed when the payload is structured JSON and verbatim otherwise.
 * The boundary wraps every shaped body — a renderer that throws on a
 * malformed payload degrades to this body for that card alone and never
 * blanks the card or the transcript ("Body rendering failure degrades").
 */
import { Component } from "react";
import type { ErrorInfo, ReactElement, ReactNode } from "react";

import { isString, parseJSONResult } from "../parse";
import { ClampedText } from "../primitives/ClampedText";

/** Pretty-prints structured JSON; returns the text untouched otherwise. */
export function prettyOrVerbatim(text: string): string {
  if (text.trim() === "") return "";

  const parsed = parseJSONResult(text);

  if (parsed === null) return text;

  // A bare JSON string is its own text; objects and arrays print indented.
  if (isString(parsed)) {
    return text.trim() === JSON.stringify(parsed) ? parsed : text;
  }

  try {
    return JSON.stringify(parsed, null, 2);
  } catch {
    return text;
  }
}

export interface FallbackBodyProps {
  argsText: string;
  resultText: string;
  isError: boolean;
  /** Test-id prefix the pane's root carries (keyed by call id so
   * degradation tests can find the degraded card's fallback). */
  testidPrefix?: string;
}

/** The honest fallback body: labeled sections, clamped at ten lines. */
export function FallbackBody({
  argsText,
  resultText,
  isError,
  testidPrefix = "fallback",
}: FallbackBodyProps): ReactElement {
  const args = prettyOrVerbatim(argsText);
  const result = prettyOrVerbatim(resultText);

  return (
    <div className="space-y-2 px-3 pb-2" data-testid={testidPrefix}>
      {args !== "" ? (
        <div>
          <p className="text-xs text-muted">Arguments</p>
          <ClampedText text={args} testidPrefix={`${testidPrefix}-args`} />
        </div>
      ) : null}
      {result !== "" ? (
        <div>
          <p
            className={`text-xs ${isError ? "text-dangertext" : "text-muted"}`}
          >
            {isError ? "Error" : "Result"}
          </p>
          <ClampedText text={result} testidPrefix={`${testidPrefix}-result`} />
        </div>
      ) : null}
    </div>
  );
}

interface BoundaryState {
  failed: boolean;
}

/** Wraps one shaped body; any render throw degrades the card to the
 * fallback body. The boundary resets per body element so two cards never
 * share degradation state. */
export class BodyBoundary extends Component<
  { children: ReactNode; fallback: ReactElement },
  BoundaryState
> {
  state: BoundaryState = { failed: false };

  static getDerivedStateFromError(): BoundaryState {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // Degrading is the designed path for malformed payloads; the console
    // keeps the evidence for debugging without surfacing a toast.
    console.error("tool body degraded to fallback", error, info.componentStack);
  }

  componentDidUpdate(prev: { children: ReactNode }): void {
    // A different body element (another record or call) resets the state.
    if (prev.children !== this.props.children && this.state.failed) {
      this.setState({ failed: false });
    }
  }

  render(): ReactElement {
    if (this.state.failed || this.props.children === undefined) {
      return this.props.fallback;
    }

    // SAFETY: the narrow above proved the body element is present; the
    // ReactNode→ReactElement read is the boundary's own contract.
    return this.props.children as ReactElement;
  }
}
