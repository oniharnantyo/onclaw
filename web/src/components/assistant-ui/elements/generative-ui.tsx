"use client";

import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { GenerativeUILibrary } from "@assistant-ui/react-generative-ui";
import { defaultGenerativeUILibrary } from "@assistant-ui/react-generative-ui";

const markdownBase = defaultGenerativeUILibrary.Markdown!;

/** `MarkdownTextPrimitive` cannot be reused here: it reads from message-part context, not a prop string. */
export const styledGenerativeUILibrary: GenerativeUILibrary = {
  ...defaultGenerativeUILibrary,
  Markdown: {
    properties: markdownBase.properties,
    streamProperties: markdownBase.streamProperties,
    description: "A markdown string, rendered with GitHub-flavored markdown.",
    render: ({ value, children }) => (
      <div data-aui="markdown">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{value ?? ""}</ReactMarkdown>
        {children}
      </div>
    ),
  },
};

/** The composition vocabulary OnClaw exposes to models — TRIMMED from the
 * upstream 27 with three deliberate omissions:
 *
 * - `Image` — SECURITY, not taste: `src` is a bare string, so a model-chosen
 *   `<img src>` fires requests from the user's browser (tracking pixels,
 *   query-string exfiltration, internal-host probing). Deferred until a URL
 *   policy exists; the `preview` fence tag covers legitimate embeds.
 * - `DatePicker` — forms-lane component, inert without `$action` wiring.
 * - `Carousel` — untaught, low value inline in a chat column.
 *
 * This map is the single source of truth for the `ui` fence: the validator
 * gates each node through its entry's `properties.safeParse`, the renderer
 * dispatches to its entry's `render`, and the prompt-doc catalog mirrors it.
 * Extending the vocabulary = adding an entry here (registration surface, per
 * the AGENTS.md plugins-are-first-class rule). */
const UI_KEEP = new Set([
  "Row", "Col", "Card", "Divider", "Spacer", "Box",
  "Header", "Text", "Caption", "Markdown", "Badge", "Fact",
  "Alert", "Icon", "Table", "Chart", "ListView", "ListViewItem",
  "Form", "Input", "Select", "Checkbox", "RadioGroup", "Button",
]);

export const uiLibrary: GenerativeUILibrary = Object.fromEntries(
  Object.entries(styledGenerativeUILibrary).filter(([k]) => UI_KEEP.has(k)),
);
