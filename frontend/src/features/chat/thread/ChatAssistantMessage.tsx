/**
 * ChatAssistantMessage (tasks 3.2–3.3, design D2/D5; flattened by
 * `flatten-chat-transcript` design D5/D6): one assistant turn on the
 * MessagePrimitive.GroupedParts switch — reasoning folds behind the
 * vendored reasoning disclosure (its `ghost` variant: a quiet line, not a
 * panel), consecutive tool calls group through the wave-2B ChatToolGroup
 * contract, text renders through the vendored
 * MarkdownText with the shiki highlighter and the mermaid wrapper
 * registered as props, `data-error` parts render the inline error card, and
 * a `data-compaction` message renders the divider. The turn opens with a
 * hairline rule, and the message root carries no width cap and no inset, so
 * the answer, the reasoning line and the tool rows share one left edge and
 * one measure. The footer carries the
 * `n / total` branch picker (offline through the runtime's branch switch)
 * beside the copy and regenerate actions, revealed on hover/focus.
 *
 * Streaming text renders through StreamedMarkdown — react-markdown behind
 * `stabilizeMarkdown` — because the vendored MarkdownText animates its
 * reveal (`useSmooth`), which hides partial text until animation frames
 * land; the stabilizer pre-pass keeps dangling constructs from painting
 * artifacts (spec "Streaming-safe markdown") and is byte-identical on
 * complete text. A completed part always renders through MarkdownText, so
 * the final render of a streamed message equals a one-pass render.
 */
import {
  Suspense,
  createContext,
  lazy,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type { ComponentPropsWithoutRef, ReactElement, ReactNode } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Components } from "react-markdown";
import {
  Check,
  CheckIcon,
  ChevronLeft,
  ChevronRight,
  Copy,
  CopyIcon,
  RefreshCw,
} from "lucide-react";

import {
  type AssistantState,
  BranchPickerPrimitive,
  MessagePrimitive,
  groupPartByType,
  useAui,
  useAuiState,
} from "@assistant-ui/react";
import {
  type CodeHeaderProps,
  type MarkdownTextPrimitiveProps,
  type SyntaxHighlighterProps,
} from "@assistant-ui/react-markdown";

import { MermaidDiagram } from "@/components/assistant-ui/elements/mermaid-diagram.aui";
import {
  ReasoningContent,
  ReasoningRoot,
  ReasoningText,
  ReasoningTrigger,
} from "@/components/assistant-ui/elements/reasoning.aui";
import { useCopyToClipboard } from "@/components/assistant-ui/hooks/use-copy-to-clipboard";
import { TooltipIconButton } from "@/components/assistant-ui/elements/tooltip-icon-button";

import { formatTokens } from "../format";
import { toast } from "../../../lib/toasts";
import { copyText } from "../lib/copyText";
import { isNumber, isString, type JsonObject } from "../tools/parse";
import { stabilizeMarkdown } from "../lib/markdownStream";
import { useChatStore, type ExtendedChatStore } from "../store";
import { toWorkspaceRel } from "../toolPaths";
import { workspaceFileTab } from "../components/RightPanel";
import { ChatToolGroup, toolRenderers } from "../tools/aui";
import { DayGate, MessageTime, useDayBoundary } from "./DayDivider";

/** Adjacent reasoning parts fold behind one disclosure; adjacent tool calls
 * group behind the 2B fold; everything else renders standalone (the
 * thread.aui.tsx reference composition, minus the chain-of-thought tier). */
const GROUP_BY = groupPartByType({
  reasoning: ["group-reasoning"],
  "tool-call": ["group-tool"],
  "standalone-tool-call": [],
});

/** Schemes/fragments that make a markdown href an external navigation
 * target rather than a workspace path (covers http:, mailto:, #anchors,
 * protocol-relative URLs). */
const EXTERNAL_HREF = /^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i;

/** How long the copy check holds after a copied message (design D12). */
const CHECK_RESET_MS = 1200;

/** The action-row buttons' shared reveal grammar (the message group's
 * hover/focus drives both). */
const ACTION_CLASS =
  "flex h-6 items-center gap-1 rounded px-1.5 text-[11px] text-muted transition-opacity hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg";

/** The completed-text path loads LAZILY: the vendored markdown element and the
 * shiki element pull the highlighter core (megabytes) into the bundle, and
 * the design wants the first paint of an empty chat to stay lean ("heavy
 * dependencies load lazily"). The chunk lands when the first completed
 * message renders; the streaming path never needs it. */
const MarkdownText = lazy(async () => {
  const mod = await import("@/components/assistant-ui/elements/markdown-text");

  return { default: mod.MarkdownText };
});

const SyntaxHighlighter = lazy(async () => {
  const mod =
    await import("@/components/assistant-ui/elements/shiki-highlighter.aui");

  return { default: mod.SyntaxHighlighter };
});

/** The code block's wrap state (spec chat-view "Code block presentation":
 * "a header with the language label when known, a copy control, and a wrap
 * toggle"). The vendored `CodeHeader` renders the label and the copy control
 * only, and the vendored files are never hand-edited (design D13), so the
 * header is overridden HERE and the wrap is applied through a data attribute
 * on a buddies-side wrapper around the highlighter — the vendored element
 * renders its own `<pre>` and drops the `components` prop, so the rule that
 * switches the wrap lives in the app stylesheet. */
interface CodeWrapValue {
  wrapped: boolean;
  toggle: () => void;
}

const CodeWrapContext = createContext<CodeWrapValue>({
  wrapped: false,
  toggle: (): void => undefined,
});

/** Provides the per-message wrap state to the header and the highlighter. */
function CodeWrapProvider({ children }: { children: ReactNode }): ReactElement {
  const [wrapped, setWrapped] = useState(false);

  const value = useMemo(
    (): CodeWrapValue => ({
      wrapped,
      toggle: (): void => {
        setWrapped((current: boolean): boolean => !current);
      },
    }),
    [wrapped]
  );

  return (
    <CodeWrapContext.Provider value={value}>
      {children}
    </CodeWrapContext.Provider>
  );
}

/** The code block's header: the language label, the copy control and the
 * wrap toggle — the vendored header's grammar (and its class names, so the
 * vendored stylesheet keeps applying) plus the toggle. */
function ChatCodeHeader({ language, code }: CodeHeaderProps): ReactElement {
  const { wrapped, toggle } = useContext(CodeWrapContext);
  const { isCopied, copyToClipboard } = useCopyToClipboard();

  return (
    <div className="aui-code-header-root border-border/50 bg-muted/50 mt-3 flex items-center justify-between rounded-t-xl border border-b-0 px-3.5 py-1.5 text-xs">
      <span className="aui-code-header-language text-muted-foreground font-medium lowercase">
        {language}
      </span>
      <div className="flex items-center gap-1">
        <button
          type="button"
          aria-pressed={wrapped}
          title="Toggle line wrap"
          data-testid="code-wrap-toggle"
          onClick={toggle}
          className="text-muted-foreground hover:text-foreground rounded px-1.5 py-0.5 text-[10px] hover:bg-foreground/10"
        >
          {wrapped ? "No wrap" : "Wrap"}
        </button>
        <TooltipIconButton
          tooltip="Copy"
          onClick={(): void => {
            copyToClipboard(code);
          }}
        >
          {isCopied ? (
            <CheckIcon className="animate-in zoom-in-50 fade-in duration-150" />
          ) : (
            <CopyIcon className="animate-in zoom-in-75 fade-in duration-150" />
          )}
        </TooltipIconButton>
      </div>
    </div>
  );
}

/** Markdown component overrides for the completed-text path: workspace
 * links open the right panel (chat-panels "Preview sources") and fenced code
 * flows through the vendored shiki highlighter — registered at the
 * `SyntaxHighlighter` key the markdown-text element routes code fences to,
 * behind a wrapper that satisfies the slot's narrower prop type. Module-level
 * constants keep the object identities stable for the element's
 * shallow-stable props gate. The wrapper carries the wrap state the header
 * toggles (the element renders its own `<pre>`, so the rule that switches
 * the wrap lives in the app stylesheet). */
const HighlighterSlot: NonNullable<
  MarkdownTextPrimitiveProps["components"]
>["SyntaxHighlighter"] = (props: SyntaxHighlighterProps): ReactElement => {
  const { wrapped } = useContext(CodeWrapContext);

  return (
    <div data-code-wrap={wrapped ? "true" : "false"}>
      <SyntaxHighlighter {...props} />
    </div>
  );
};

const MD_TEXT_COMPONENTS = {
  a: ChatMarkdownLink,
  CodeHeader: ChatCodeHeader,
  SyntaxHighlighter: HighlighterSlot,
};

/** Language-specific overrides: mermaid fences render through the vendored
 * diagram wrapper (design D5). */
const MD_BY_LANGUAGE = { mermaid: { SyntaxHighlighter: MermaidDiagram } };

/** Streaming-markdown overrides — the old layer's token-styled set, minus
 * the code-block chrome a streaming fence does not earn yet (plain monospace
 * while arriving, spec "Code block presentation"). */
const STREAM_COMPONENTS: Components = {
  p: (props: ComponentPropsWithoutRef<"p">) => (
    <p className="mb-2 last:mb-0">{props.children}</p>
  ),
  a: (props: ComponentPropsWithoutRef<"a">) => <ChatMarkdownLink {...props} />,
  blockquote: (props: ComponentPropsWithoutRef<"blockquote">) => (
    <blockquote className="mb-2 border-s-2 border-line ps-3 text-fg2">
      {props.children}
    </blockquote>
  ),
  li: (props: ComponentPropsWithoutRef<"li">) => (
    <li className="mb-1 ml-5 list-disc">{props.children}</li>
  ),
  code: (props: ComponentPropsWithoutRef<"code">) => (
    <code className="rounded-[4px] bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] px-1 py-0.5 font-mono text-[13px]">
      {props.children}
    </code>
  ),
  pre: (props: ComponentPropsWithoutRef<"pre">) => (
    <pre
      data-streaming="true"
      className="od-scroll mb-2 overflow-x-auto rounded-lg border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] p-3 font-mono text-[12.5px] leading-5 text-fg2"
    >
      {props.children}
    </pre>
  ),
};

/**
 * The markdown link renderer: a path href opens the right panel's preview
 * tab; anything with a scheme, a fragment, or no active agent stays a
 * regular link.
 */
function ChatMarkdownLink(props: ComponentPropsWithoutRef<"a">): ReactElement {
  const activeAgentID = useChatStore((s: ExtendedChatStore) => s.activeAgentID);
  const openPanelTab = useChatStore((s: ExtendedChatStore) => s.openPanelTab);

  const href = props.href ?? "";

  if (href === "" || EXTERNAL_HREF.test(href) || activeAgentID === null) {
    return (
      <a
        href={href || undefined}
        target="_blank"
        rel="noreferrer"
        className="text-accenttext underline underline-offset-2 hover:opacity-80"
      >
        {props.children}
      </a>
    );
  }

  const relPath = toWorkspaceRel(href);

  return (
    <button
      type="button"
      className="cursor-pointer text-left text-accenttext underline underline-offset-2 hover:opacity-80"
      data-testid={`md-link-${href}`}
      title="Preview in the right panel"
      onClick={() => openPanelTab(workspaceFileTab(activeAgentID, relPath))}
    >
      {props.children}
    </button>
  );
}

/** The streaming text renderer: the partial source stabilized, then parsed
 * synchronously — the text is on the page within the render that received
 * it (the smooth-reveal animation cannot hide it from a reader or a test).
 * remark-gfm rides the stream too: the partial source parses to the same
 * GFM constructs (tables, task lists) the completed path's MarkdownText
 * carries, and streaming is the state the reader sits in longest. */
function StreamedMarkdown({ text }: { text: string }): ReactElement {
  return (
    <div data-streaming="true">
      <Markdown remarkPlugins={[remarkGfm]} components={STREAM_COMPONENTS}>
        {stabilizeMarkdown(text)}
      </Markdown>
    </div>
  );
}

/** The part-payload record guard: `part.data` surfaces as unknown from the
 * aui runtime, so this predicate establishes the record's contract at the
 * render's boundary — every field is again a Json value (the decode domain
 * shared with the tool payload parser) — and the payload predicates below
 * read parsed domain values. */
function isRecord(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null;
}

/** Reads an error part's message: the record shape the D2 mapping mints for
 * data-error parts ({ message }); a degraded payload keeps the card honest
 * with the reducer's fallback line. */
function errorMessageOf(data: unknown): data is { message: string } {
  return isRecord(data) && isString(data.message);
}

/** Reads the compaction payload's token counters: the record shape the D2
 * mapping mints for data-compaction parts (
 * { tokens_before, tokens_after }); the field checks keep a degraded
 * payload from rendering nonsense. */
function compactionTokens(
  data: unknown
): data is { tokens_before: number; tokens_after: number } {
  return (
    isRecord(data) &&
    isNumber(data.tokens_before) &&
    isNumber(data.tokens_after)
  );
}

/** The inline error card (the old ErrorEntry's anatomy): danger hairline,
 * tinted surface, alert semantics. */
function ErrorLine({ message }: { message: string }): ReactElement {
  return (
    <div
      className="flex items-start gap-2 rounded-md border border-danger/35 bg-danger/5 px-3 py-2"
      data-testid="entry-error"
      role="alert"
    >
      <p className="break-words text-[13px] leading-5 text-dangertext">
        {message}
      </p>
    </div>
  );
}

/** The compaction divider between turns: hairlines around the display-grade
 * token estimates (the old CompactionDivider's anatomy). */
function CompactionLine({ data }: { data: unknown }): ReactElement | null {
  if (!compactionTokens(data)) return null;

  return (
    <div
      className="flex items-center gap-3 px-2 py-1"
      data-testid="entry-compaction"
    >
      <span className="h-px flex-1 bg-line" />
      <span className="text-xs text-muted">
        Context compacted · {formatTokens(data.tokens_before)} →{" "}
        {formatTokens(data.tokens_after)} tokens
      </span>
      <span className="h-px flex-1 bg-line" />
    </div>
  );
}

/** Wall-clock span a reasoning group spent reasoning, in whole seconds —
 * the vendored trigger's duration read. */
function reasoningDuration(
  timing:
    | {
        startedAt: number;
        completedAt?: number;
      }
    | undefined
): number | undefined {
  if (timing?.completedAt === undefined) return undefined;

  return Math.round((timing.completedAt - timing.startedAt) / 1000);
}

/** The hairline that separates one assistant turn from the one before it
 * (design D6). A message that opens a new day already carries the day
 * divider's own hairlines, so the rule stands down there and the two never
 * stack. */
function TurnRule(): ReactElement | null {
  const { changed } = useDayBoundary();

  if (changed) return null;

  return (
    <div
      aria-hidden="true"
      className="bg-foreground/[0.08] my-3 h-px w-full"
      data-testid="turn-rule"
    />
  );
}

/** One assistant turn. A compaction message renders as the divider alone. */
export function ChatAssistantMessage(): ReactElement {
  const content = useAuiState((s: AssistantState) => s.message.content);
  const lead = content[0];

  if (
    lead !== undefined &&
    lead.type === "data" &&
    lead.name === "compaction"
  ) {
    return (
      <div className="group/msg relative">
        <DayGate />
        <CompactionLine data={lead.data} />
      </div>
    );
  }

  return (
    <div className="group/msg group/answer relative">
      <DayGate />
      <TurnRule />
      <MessagePrimitive.Root
        data-testid="entry-assistant"
        data-role="assistant"
        className="text-sm leading-relaxed text-fg wrap-break-word"
      >
        <MessagePrimitive.GroupedParts groupBy={GROUP_BY} indicator="never">
          {({
            part,
            children,
          }: MessagePrimitive.GroupedParts.RenderInfo): ReactNode => {
            switch (part.type) {
              case "group-reasoning": {
                const running = part.status.type === "running";

                // The reasoning group is a quiet disclosure line, not a panel
                // (design D5): the vendored element keeps its collapsible
                // mechanics, its streaming preview, and its scroll lock, and
                // the `ghost` variant drops the bordered surface. The trigger
                // carries the glyph, the word, and the chevron on one line;
                // the body opens into an indented, dimmed aside.
                return (
                  <ReasoningRoot
                    variant="ghost"
                    streaming={running}
                    className="my-2"
                  >
                    <ReasoningTrigger
                      active={running}
                      duration={reasoningDuration(part.timing)}
                      className="max-w-none gap-1.5 py-0.5 text-[13px] text-muted-foreground/90 hover:text-foreground"
                    />
                    <ReasoningContent aria-busy={running}>
                      <ReasoningText className="mt-1 max-h-80 ps-3 text-[13px] leading-relaxed text-muted-foreground/80">
                        {children}
                      </ReasoningText>
                    </ReasoningContent>
                  </ReasoningRoot>
                );
              }

              case "group-tool":
                return <ChatToolGroup group={part}>{children}</ChatToolGroup>;
              case "text":
                return part.status?.type === "running" ? (
                  <StreamedMarkdown text={part.text} />
                ) : (
                  <CodeWrapProvider>
                    <Suspense fallback={null}>
                      <MarkdownText
                        components={MD_TEXT_COMPONENTS}
                        componentsByLanguage={MD_BY_LANGUAGE}
                      />
                    </Suspense>
                  </CodeWrapProvider>
                );
              case "reasoning":
                return (
                  <div className="text-muted-foreground leading-relaxed whitespace-pre-wrap">
                    {part.text}
                  </div>
                );
              case "tool-call": {
                const Renderer =
                  toolRenderers[part.toolName] ?? toolRenderers.fallback;

                // A call that renders outside the group (an MCP app, a
                // standalone tool UI) carries its own breathing room; inside
                // a group the group's own margin wraps the whole run and
                // the rows stay tight with each other.
                return (
                  <div className="my-2">
                    {part.toolUI ?? <Renderer {...part} />}
                  </div>
                );
              }

              case "data":
                return part.name === "error" ? (
                  <ErrorLine
                    message={
                      errorMessageOf(part.data)
                        ? part.data.message
                        : "Turn failed"
                    }
                  />
                ) : null;
              // Tail activity (typing dots / running-tool status line) is
              // owned by the thread's TurnActivity slot; the synthetic
              // indicator would double it.
              case "indicator":
              default:
                return null;
            }
          }}
        </MessagePrimitive.GroupedParts>
      </MessagePrimitive.Root>
      <MessageFooter />
      <MessageTime side="end" />
    </div>
  );
}

/** The hover/focus-revealed footer: the `n / total` branch picker (switching
 * goes through the runtime's branch switch — the adapter's setMessages →
 * setActiveVariant — so it is pure local state, no request) beside the
 * regenerate and copy actions. Copy rides the shared clipboard writer with
 * the danger-toast refusal path (design D12). */
function MessageFooter(): ReactElement {
  const aui = useAui();

  const running = useAuiState(
    (s: AssistantState) => s.message.status?.type === "running"
  );

  const [copied, setCopied] = useState(false);
  const resetRef = useRef<number | null>(null);

  // An unmount mid-window must not setState from the pending reset timer.
  useEffect(
    (): (() => void) => (): void => {
      if (resetRef.current !== null) window.clearTimeout(resetRef.current);
    },
    []
  );

  const onCopy = (): void => {
    // copyText writes on the click gesture — the user-activation window;
    // nothing may await before it (design D12).
    void copyText(aui.message.getCopyText()).then((ok: boolean): void => {
      if (!ok) {
        toast("Copy failed — clipboard access was refused", "danger");

        return;
      }

      if (resetRef.current !== null) window.clearTimeout(resetRef.current);

      setCopied(true);
      resetRef.current = window.setTimeout((): void => {
        resetRef.current = null;
        setCopied(false);
      }, CHECK_RESET_MS);
    });
  };

  return (
    <div
      className="mt-1 flex min-h-7 items-center gap-2 opacity-0 transition-opacity group-hover/msg:opacity-100 group-focus-within/msg:opacity-100"
      data-testid="message-actions"
    >
      <BranchPickerPrimitive.Root
        hideWhenSingleBranch
        className="inline-flex items-center gap-1 text-xs text-muted"
        data-testid="variant-picker"
      >
        <BranchPickerPrimitive.Previous asChild>
          <button
            type="button"
            aria-label="Previous variant"
            data-testid="variant-prev"
            className={ACTION_CLASS}
          >
            <ChevronLeft aria-hidden="true" className="size-3" />
          </button>
        </BranchPickerPrimitive.Previous>
        <span className="font-medium tabular-nums" data-testid="variant-count">
          <BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />
        </span>
        <BranchPickerPrimitive.Next asChild>
          <button
            type="button"
            aria-label="Next variant"
            data-testid="variant-next"
            className={ACTION_CLASS}
          >
            <ChevronRight aria-hidden="true" className="size-3" />
          </button>
        </BranchPickerPrimitive.Next>
      </BranchPickerPrimitive.Root>
      {!running && (
        <>
          <button
            type="button"
            aria-label="Regenerate answer"
            data-testid="btn-regenerate"
            title="Regenerate"
            onClick={() => aui.message.reload()}
            className={ACTION_CLASS}
          >
            <RefreshCw aria-hidden="true" className="size-3" />
            Regenerate
          </button>
          <button
            type="button"
            aria-label={copied ? "Copied" : "Copy message"}
            data-copied={copied ? "true" : "false"}
            data-testid="btn-copy-message"
            title="Copy"
            onClick={onCopy}
            className={ACTION_CLASS}
          >
            {copied ? (
              <Check aria-hidden="true" className="size-3 text-success" />
            ) : (
              <Copy aria-hidden="true" className="size-3" />
            )}
            {copied ? "Copied" : "Copy"}
          </button>
        </>
      )}
    </div>
  );
}
