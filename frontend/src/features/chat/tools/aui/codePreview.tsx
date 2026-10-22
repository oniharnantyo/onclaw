/**
 * CodePreview (task 4.2): the tool bodies' code surface — the retained
 * `CodeBlock` primitive, whose lazy per-language Shiki loading
 * (tools/primitives/shikiHighlight.ts, dynamic imports only) stays the
 * preview's highlight path: unknown languages and loader failures degrade to
 * plain text, never blocking the first render. Gutter numbers keep the
 * read-file window's real line numbers via `startLine`.
 */
import type { ReactElement } from "react";

import { CodeBlock } from "../primitives/CodeBlock";

export interface CodePreviewProps {
  code: string;
  language?: string;
  lineNumbers?: boolean;
  startLine?: number;
  testidPrefix?: string;
}

export function CodePreview({
  code,
  language,
  lineNumbers = false,
  startLine = 1,
  testidPrefix = "code-preview",
}: CodePreviewProps): ReactElement {
  return (
    <CodeBlock
      code={code}
      language={language}
      lineNumbers={lineNumbers}
      startLine={startLine}
      testidPrefix={testidPrefix}
    />
  );
}
