"use client";

import "@assistant-ui/react-markdown/styles/dot.css";

import { type ReactElement, useEffect, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { cn } from "@/lib/utils";

type MarkdownBodyProps = {
  text: string;
  className?: string;
};

/** The shared transcript component map lives behind the markdown element
 * module; loading it on demand keeps that module (and the assistant-ui
 * markdown package) out of the entry graph (fix-ui-audit-findings design
 * D6b). One promise per page serves every preview body. */
const loadComponents = () =>
  import("./markdown-text").then((mod) => mod.defaultComponents);

type MarkdownComponents = Awaited<ReturnType<typeof loadComponents>>;

let componentsPromise: Promise<MarkdownComponents> | null = null;

const componentsOnce = (): Promise<MarkdownComponents> =>
  (componentsPromise ??= loadComponents());

const PlainMarkdown = ({
  text,
  className,
}: MarkdownBodyProps): ReactElement => (
  <div className={cn("aui-md", className)}>
    <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
  </div>
);

/**
 * Standalone markdown body for surfaces outside the assistant-ui message
 * context (panel previews, trajectory text steps). Deliberately reuses the
 * chat transcript's component pipeline — the exported `defaultComponents`
 * map plus remark-gfm under the `aui-md` wrapper class — so rendering
 * matches `MarkdownText`; no message context required. Until the component
 * map's chunk lands the body still paints the plain HTML pipeline.
 */
export const MarkdownBody = ({
  text,
  className,
}: MarkdownBodyProps): ReactElement => {
  const [components, setComponents] = useState<MarkdownComponents | null>(
    null
  );

  useEffect(() => {
    let alive = true;
    void componentsOnce().then((loaded) => {
      if (alive) setComponents(loaded);
    });
    return () => {
      alive = false;
    };
  }, []);

  if (components === null) {
    return <PlainMarkdown text={text} className={className} />;
  }

  return (
    <div className={cn("aui-md", className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {text}
      </ReactMarkdown>
    </div>
  );
};