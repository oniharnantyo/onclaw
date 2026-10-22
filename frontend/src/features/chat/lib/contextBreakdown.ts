/**
 * Context breakdown estimator (design D7, tasks 6.1): onclaw-v2's rules at
 * face value. Four client segments — Instructions (the agent's three prompt
 * documents), Tools & skills (a flat cost per enabled tool and per skill
 * header), Files (a flat cost per image attachment, text attachments by
 * size), and Conversation (4 chars/token plus a flat cost per message) —
 * are counted over POST-COMPACTION entries only, and a fifth "Server
 * context" segment carries the remainder `max(0, used − Σ estimates)` for
 * everything the client cannot see (compaction summaries, tool schemas,
 * retrieved memory, tool payloads). Estimates are NEVER scaled to the
 * provider's reported total; the displayed total is always the real
 * `used`, and headroom is `limit − used`.
 *
 * Tool and task entries are not message text — their arguments and results
 * are server-side context the client estimator does not pretend to weigh,
 * so they land in the Server context remainder by construction.
 */
import type { Attachment as SessionAttachment } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { RichUserEntry } from "../entries";
import type { Entry } from "../types";

/** Conversation and text sizing: 4 characters per token (design D7). */
export const CHARS_PER_TOKEN = 4;

/** Flat per-message conversation cost (design D7). */
export const TOKENS_PER_MESSAGE = 8;

/** Flat cost per enabled tool (design D7). */
export const TOKENS_PER_TOOL = 120;

/** Flat cost per skill header (design D7). */
export const TOKENS_PER_SKILL = 40;

/** Flat cost per image attachment (design D7). */
export const TOKENS_PER_IMAGE = 1100;

/**
 * The slice of the bound PromptDocuments the Instructions segment reads.
 * Structurally the generated `PromptDocuments` (Go field names), declared
 * locally so the estimator stays decoupled from the binding type.
 */
export interface PromptDocContent {
  Identity: string;
  IdentityExists: boolean;
  Soul: string;
  SoulExists: boolean;
  Bootstrap: string;
  BootstrapExists: boolean;
}

/** Inputs of one breakdown computation (design D7). */
export interface BreakdownInput {
  /** The session's real reported usage — displayed as-is, never a target. */
  used: number;
  /** The model's context limit; headroom is `limit − used`. */
  limit: number;
  /** The agent's prompt documents (cached per agent); null while loading
   * or on a failed read — the segment then estimates zero. */
  promptDocs: PromptDocContent | null;
  /** Count of the agent's enabled tools. */
  enabledTools: number;
  /** Count of the agent's enabled skills. */
  enabledSkills: number;
  /** The open session's transcript entries, oldest first. */
  entries: Entry[];
}

/** One legend segment of the breakdown bar. */
export interface ContextSegment {
  key: SegmentKey;
  label: string;
  tokens: number;
  /** True for client estimates — the popover captions these rows and never
   * stretches them toward the reported total (spec chat-view "Estimates
   * are labeled"). The Server context remainder is exact by construction
   * and carries no caption. */
  estimated: boolean;
}

export type SegmentKey =
  | "instructions"
  | "tools"
  | "files"
  | "conversation"
  | "server";

/** The computed breakdown the popover renders. */
export interface ContextBreakdown {
  segments: ContextSegment[];
  /** The real reported used total — the popover's displayed total. */
  used: number;
  limit: number;
  /** `limit − used` (design D7). */
  headroom: number;
}

/**
 * The entries a post-compaction context still carries: everything after the
 * LAST compaction marker, or all entries when none is present.
 */
export function postCompactionEntries(entries: Entry[]): Entry[] {
  let lastCompaction = -1;

  for (let i = 0; i < entries.length; i += 1) {
    if (entries[i].kind === "compaction") lastCompaction = i;
  }

  return lastCompaction < 0 ? entries : entries.slice(lastCompaction + 1);
}

/** Characters to tokens at the estimator's 4-chars-per-token rate, rounding
 * up so a partial token still counts (a conservative face value). */
function tokensForChars(chars: number): number {
  return Math.ceil(chars / CHARS_PER_TOKEN);
}

/**
 * Instructions: the agent's existing prompt documents at 4 chars/token.
 * Missing documents (Exists=false) and a null read contribute nothing.
 */
export function estimateInstructions(docs: PromptDocContent | null): number {
  if (!docs) return 0;

  const chars =
    (docs.IdentityExists ? docs.Identity.length : 0) +
    (docs.SoulExists ? docs.Soul.length : 0) +
    (docs.BootstrapExists ? docs.Bootstrap.length : 0);

  return tokensForChars(chars);
}

/** A user entry's persisted attachment metadata, empty for other kinds. */
function attachmentsOf(entry: Entry): SessionAttachment[] {
  if (entry.kind !== "user") return [];

  // SAFETY: the reducer mints user entries through the RichUserEntry seam
  // (entries.ts); a plain user entry simply carries no attachments field.
  return (entry as RichUserEntry).attachments ?? [];
}

/** Files: 1100 per image attachment, text attachments by byte size at
 * 4 chars/token — post-compaction entries only. */
function estimateFiles(entries: Entry[]): number {
  let tokens = 0;

  for (const entry of entries) {
    for (const attachment of attachmentsOf(entry)) {
      tokens += attachment.mime.startsWith("image/")
        ? TOKENS_PER_IMAGE
        : tokensForChars(attachment.size);
    }
  }

  return tokens;
}

/** Conversation: 8 tokens per message entry (user, assistant, reasoning)
 * plus 4 chars/token over its text — post-compaction entries only. */
function estimateConversation(entries: Entry[]): number {
  let tokens = 0;

  for (const entry of entries) {
    if (
      entry.kind === "user" ||
      entry.kind === "assistant" ||
      entry.kind === "reasoning"
    ) {
      tokens += TOKENS_PER_MESSAGE + tokensForChars(entry.text.length);
    }
  }

  return tokens;
}

/**
 * Computes the breakdown (design D7): the four client segments at face
 * value over post-compaction entries, plus the Server context remainder
 * floored at zero. Segment values are returned unscaled — the caller
 * displays `used` as the total, never the estimate sum.
 */
export function computeContextBreakdown(
  input: BreakdownInput
): ContextBreakdown {
  const recent = postCompactionEntries(input.entries);

  const instructions = estimateInstructions(input.promptDocs);

  const tools =
    input.enabledTools * TOKENS_PER_TOOL +
    input.enabledSkills * TOKENS_PER_SKILL;

  const files = estimateFiles(recent);
  const conversation = estimateConversation(recent);

  const estimatedSum = instructions + tools + files + conversation;

  const segments: ContextSegment[] = [
    {
      key: "instructions",
      label: "Instructions",
      tokens: instructions,
      estimated: true,
    },
    {
      key: "tools",
      label: "Tools & skills",
      tokens: tools,
      estimated: true,
    },
    { key: "files", label: "Files", tokens: files, estimated: true },
    {
      key: "conversation",
      label: "Conversation",
      tokens: conversation,
      estimated: true,
    },
    {
      key: "server",
      label: "Server context",
      tokens: Math.max(0, input.used - estimatedSum),
      estimated: false,
    },
  ];

  return {
    segments,
    used: input.used,
    limit: input.limit,
    headroom: input.limit - input.used,
  };
}
