"use client";

/* Vendored from the @assistant-ui registry (markdown-card-elements 1.3).
 * Two deliberate deviations from the installed source, both documented by the
 * change design (D4/D9):
 *  - Lazy engine lane (1.6): react-shiki is not imported statically — only
 *    its types (erased at build). The hook behind `useShikiModule` loads the
 *    shiki chunk on first highlight; until then plain code renders in the
 *    same container and upgrades in place. The real hook call is gated
 *    behind `ShikiRun` so the loaded module's hook count is stable.
 *  - Theme mapping (1.6): unless the caller overrides `theme`, the highlight
 *    theme follows the app theme off <html data-theme> — github-light-default
 *    / github-dark-default — via the lazy lane's useShikiTheme, instead of
 *    the installed light-dark() pairing (which keys off color-scheme, unused
 *    by this app). The shiki container uses the contract's field tint rather
 *    than bg-muted (whose value is the app-wide muted *text* color). */
import type { FC } from "react";
import type { ShikiHighlighterProps } from "react-shiki";
import { useShikiModule, useShikiTheme } from "@/lib/assistantUi/shikiHighlighter";
import { cn } from "@/lib/utils";

/**
 * Props for the SyntaxHighlighter component
 */
export type SyntaxHighlighterProps = Omit<
  ShikiHighlighterProps,
  "children" | "theme"
> & {
  theme?: ShikiHighlighterProps["theme"];
  code: string;
  /** Skips tokenization and renders the plain code while `true`. */
  streaming?: boolean;
};

const containerClassName =
  "aui-shiki-base [&_pre]:border-border/50 [&_pre]:bg-foreground/[0.03]! [&_.line]:px-0! [&_pre]:overflow-x-auto [&_pre]:rounded-t-none [&_pre]:rounded-b-xl [&_pre]:border [&_pre]:border-t-0 [&_pre]:p-3.5 [&_pre]:text-[13px] [&_pre]:leading-relaxed";

const PlainCode: FC<{ code: string }> = ({ code }) => (
  <pre>
    <code>{code}</code>
  </pre>
);

const ShikiRun: FC<{
  mod: NonNullable<ReturnType<typeof useShikiModule>>;
  code: string;
  language: SyntaxHighlighterProps["language"];
  theme: NonNullable<SyntaxHighlighterProps["theme"]>;
  options: Omit<ShikiHighlighterProps, "children" | "language" | "theme">;
}> = ({ mod, code, language, theme, options }) => {
  // Single-string themes carry their own colors; the light-dark() pairing
  // only makes sense when the caller passed both themes explicitly.
  const highlighted = mod.useShikiHighlighter(code, language, theme, {
    ...options,
    ...(typeof theme === "string" ? {} : { defaultColor: "light-dark()" }),
  });
  return <>{highlighted ?? <PlainCode code={code} />}</>;
};

const HighlightedCode: FC<{
  code: string;
  language: SyntaxHighlighterProps["language"];
  theme: NonNullable<SyntaxHighlighterProps["theme"]>;
  options: Omit<ShikiHighlighterProps, "children" | "language" | "theme">;
}> = ({ code, language, theme, options }) => {
  const mod = useShikiModule();
  // Degraded first paint (D9): plain code in place while the chunk loads.
  if (!mod) return <PlainCode code={code} />;
  return (
    <ShikiRun mod={mod} code={code} language={language} theme={theme} options={options}/>
  );
};

/**
 * SyntaxHighlighter component, using react-shiki
 *
 * Skips tokenization while `streaming` and renders the plain code in the
 * same container, so streaming costs no Shiki work and settling is a color
 * change rather than a layout shift.
 */
export const SyntaxHighlighter: FC<SyntaxHighlighterProps> = ({
  code,
  language,
  theme,
  className,
  style,
  // Inert: useShikiHighlighter output has no default styles or language label.
  addDefaultStyles: _addDefaultStyles,
  showLanguage: _showLanguage,
  delay = 150, // the part settles before smooth streaming finishes draining, so code keeps changing for a few frames
  streaming = false,
  ...options
}) => {
  const autoTheme = useShikiTheme();
  const resolvedTheme = theme ?? autoTheme;
  const trimmed = code.trim();

  return (
    <div
      className={cn(
        containerClassName,
        streaming && "aui-shiki-streaming",
        className,
      )}
      style={style}
    >
      {streaming ? (
        <PlainCode code={trimmed} />
      ) : (
        <HighlightedCode
          code={trimmed}
          language={language}
          theme={resolvedTheme}
          options={{ ...options, delay }}
        />
      )}
    </div>
  );
};

SyntaxHighlighter.displayName = "SyntaxHighlighter";
