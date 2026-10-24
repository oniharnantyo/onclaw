"use client";

import "@assistant-ui/react-markdown/styles/dot.css";

import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { defaultComponents } from "./markdown-text";
import { cn } from "@/lib/utils";

type MarkdownBodyProps = {
  text: string;
  className?: string;
};

/**
 * Standalone markdown body for surfaces outside the assistant-ui message
 * context (panel previews, trajectory text steps). Deliberately reuses the
 * chat transcript's component pipeline — the exported `defaultComponents`
 * map plus remark-gfm under the `aui-md` wrapper class — so rendering
 * matches `MarkdownText`; no message context required.
 */
export const MarkdownBody = ({ text, className }: MarkdownBodyProps) => (
  <div className={cn("aui-md", className)}>
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={defaultComponents}
    >
      {text}
    </ReactMarkdown>
  </div>
);
