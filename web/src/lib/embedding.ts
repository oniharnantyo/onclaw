import type { ApiModel } from "./api";

// ---------------------------------------------------------------------------
// Embedding-model classification (memory D16 tri-state)
// ---------------------------------------------------------------------------

/** The catalog's per-model metadata does not carry an embedding flag on the
 * wire, so classification follows the input-modality tri-state spirit:
 * affirmatively classified embedding families → dropdown entries,
 * affirmatively classified chat families → excluded, everything else →
 * unknown (free-text entry where the connection test discovers the
 * dimension). Shared by the memory embedding dropdown and the model
 * comboboxes' embedding marker icon. */
const EMBEDDING_PATTERNS = [
  /text-embedding/i,
  /embedding/i,
  /\/embed\//i,
  /(^|\/)embed(ding)?[-_]/i,
  // Suffix/embed-token families the anchored patterns miss: arctic-embed-l,
  // embed-qa-4, nemotron-embed-1b, NV-Embed-QA.
  /\bembed(ding)?s?([-_]|$)/i,
  /\bbge[-_]/i,
  /\be5[-_]/i,
  /nomic-embed/i,
  /jina-embeddings?/i,
  /\bvoyage/i,
  /minilm/i,
  /gte-/i,
];

const CHAT_PATTERNS = [
  /\bgpt-?\d/i,
  /\bclaude/i,
  /\bgemini/i,
  /\bllama/i,
  /\bmistral/i,
  /\bqwen/i,
  /\bdeepseek/i,
  /\bglm\b/i,
  /\bsonnet\b/i,
  /\bhaiku\b/i,
  /\bopus\b/i,
  /\bmini\b/i,
  /\bnano\b/i,
  /\bomni\b/i,
];

export type EmbeddingClassification = "embedding" | "other" | "unknown";

export function classifyEmbeddingModel(m: ApiModel): EmbeddingClassification {
  const id = (m.id || "") + " " + (m.name || "");
  if (EMBEDDING_PATTERNS.some((re) => re.test(id))) return "embedding";
  if (CHAT_PATTERNS.some((re) => re.test(id))) return "other";
  return "unknown";
}
