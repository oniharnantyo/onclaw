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

// ---------------------------------------------------------------------------
// Supported embedding dimensions + known-model preselect map (memory D7)
// ---------------------------------------------------------------------------

/** OnClaw's supported embedding dimensions. The Dimension dropdown over this
 * set is a client-side product constraint only — the server keeps validating
 * positive-int, and Test connection (with its mismatch guard) stays the
 * authority. */
export const SUPPORTED_EMBEDDING_DIMS = [768, 1024, 1536, 2048, 3072];

/** Confidently-known model output dimensions for preselecting the Dimension
 * dropdown. The models.dev catalog carries no dimension metadata, so this
 * local map is the only auto-detect source until wave 3; it only preselects —
 * a wrong value is still caught by the test connection's mismatch guard. */
export const KNOWN_MODEL_DIMS: Record<string, number> = {
  "text-embedding-3-small": 1536,
  "text-embedding-3-large": 3072,
  "embedding-3": 2048, // Zhipu embedding-3
  "snowflake/arctic-embed-l": 1024,
  "nvidia/embed-qa-4": 1024,
};

/** The known dimension for a model id: exact id first, then the final path
 * segment (gateways prefix catalog ids — "openai/text-embedding-3-small"). */
export function knownModelDimension(modelId: string): number | undefined {
  const id = (modelId || "").trim();
  if (!id) return undefined;
  if (typeof KNOWN_MODEL_DIMS[id] === "number") return KNOWN_MODEL_DIMS[id];
  const tail = id.split("/").pop() || "";
  return typeof KNOWN_MODEL_DIMS[tail] === "number" ? KNOWN_MODEL_DIMS[tail] : undefined;
}
