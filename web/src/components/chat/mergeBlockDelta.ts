import type { ContentBlock } from '../../types/chat';

/**
 * mergeBlockDelta merges one streaming delta fragment into an accumulated
 * content block of the same type. Text/reasoning fragments append, and
 * argument-bearing tool-call fragments concatenate their raw `arguments`
 * string (which stays partial JSON until the block completes). Identity
 * fields (name, id, call_id) carried by an earlier fragment are preserved
 * when the later fragment omits them, and vice versa.
 */
export function mergeBlockDelta(target: ContentBlock, delta: ContentBlock): ContentBlock {
  const merged: ContentBlock = { ...target, type: delta.type ?? target.type };

  if (delta.assistant_gen_text) {
    merged.assistant_gen_text = {
      ...target.assistant_gen_text,
      ...delta.assistant_gen_text,
      text: (target.assistant_gen_text?.text ?? '') + (delta.assistant_gen_text.text ?? ''),
    };
  }

  if (delta.reasoning) {
    merged.reasoning = {
      ...target.reasoning,
      ...delta.reasoning,
      text: (target.reasoning?.text ?? '') + (delta.reasoning.text ?? ''),
    };
  }

  const argFields: (keyof ContentBlock)[] = ['function_tool_call', 'mcp_tool_call', 'server_tool_call'];
  for (const field of argFields) {
    const deltaTool = delta[field] as { arguments?: string; [k: string]: unknown } | undefined;
    if (!deltaTool) continue;
    const targetTool = (target[field] as { arguments?: string; [k: string]: unknown } | undefined) ?? {};
    (merged as unknown as Record<string, unknown>)[field] = {
      ...targetTool,
      ...deltaTool,
      arguments: (targetTool.arguments ?? '') + (deltaTool.arguments ?? ''),
    };
  }

  return merged;
}

/**
 * mergeStreamingDeltas folds a batch of streaming delta blocks into an
 * existing assistant message's content blocks. Deltas are routed to the block
 * sharing their `streaming_meta.index`; a new block is created on first
 * sighting. Blocks without an index are appended whole (e.g. persisted
 * re-sync payloads). A Map gives O(1) lookup and tolerates out-of-order
 * arrival.
 */
export function mergeStreamingDeltas(contentBlocks: ContentBlock[], deltas: ContentBlock[]): ContentBlock[] {
  const blocks = contentBlocks ? contentBlocks.map((b) => ({ ...b })) : [];
  const indexToPos = new Map<number, number>();
  blocks.forEach((b, i) => {
    const idx = b.streaming_meta?.index;
    if (typeof idx === 'number') indexToPos.set(idx, i);
  });

  for (const delta of deltas) {
    const idx = delta.streaming_meta?.index;
    if (typeof idx !== 'number') {
      blocks.push({ ...delta });
      continue;
    }
    const pos = indexToPos.get(idx);
    if (pos === undefined) {
      blocks.push({ ...delta });
      indexToPos.set(idx, blocks.length - 1);
    } else {
      blocks[pos] = mergeBlockDelta(blocks[pos], delta);
    }
  }

  return blocks;
}
