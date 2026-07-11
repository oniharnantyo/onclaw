import type { ContentBlock, ChatMessage } from '../../types/chat';

export type ContentGroup = { type: 'single'; block: ContentBlock };

/**
 * isBlockVisible mirrors the rendering logic in Renderers.tsx: a block is
 * visible only when its renderer would produce actual output. Several
 * renderers return null when their payload is empty/whitespace, and those
 * blocks must be treated as invisible so they don't leave empty wrappers
 * that create layout gaps.
 */
export function isBlockVisible(block: ContentBlock, allBlocks: ContentBlock[]): boolean {
  if (block.type === 'reasoning' || block.reasoning) {
    const text = block.reasoning?.text || (block as any).reasoning?.text;
    return !!text?.trim();
  }
  if (block.function_tool_call || block.server_tool_call) {
    return true;
  }
  if (block.mcp_tool_call) {
    return true;
  }
  if (block.function_tool_result) {
    const tr = block.function_tool_result;
    const trId = tr.call_id || (tr as any).id;
    const hasCall = allBlocks.some((other) => {
      if (!other.function_tool_call) return false;
      const tc = other.function_tool_call;
      const tcId = (tc as any).call_id || tc.id;
      if (tcId && trId) return tcId === trId;
      return tc.name === tr.name;
    });
    return !hasCall;
  }
  if (block.mcp_tool_result || block.server_tool_result) {
    return true;
  }
  if (block.assistant_gen_image || block.user_input_image || block.user_input_file) {
    return true;
  }
  if (block.assistant_gen_text?.text?.trim()) {
    return true;
  }
  if (block.user_input_text?.text?.trim()) {
    return true;
  }
  return false;
}

/**
 * isMessageVisible reports whether a message would render any visible block.
 * Used to skip empty placeholder messages that would otherwise produce
 * empty (but styled) containers in the thread.
 */
export function isMessageVisible(message: ChatMessage, allMessages: ChatMessage[]): boolean {
  if (message.role === 'system') return false;
  const blocks = message.content_blocks;
  if (!blocks || blocks.length === 0) return false;
  const allBlocks = allMessages.flatMap((m) => m.content_blocks || []);
  return blocks.some((b) => isBlockVisible(b, allBlocks));
}

export function groupBlocks(blocks?: ContentBlock[]): ContentGroup[] {
  if (!blocks || blocks.length === 0) return [];

  const groups: ContentGroup[] = [];

  for (const block of blocks) {
    groups.push({
      type: 'single',
      block,
    });
  }

  return groups;
}

const cache = new WeakMap<ContentBlock[], ContentGroup[]>();

export function memoizedGroupBlocks(blocks?: ContentBlock[]): ContentGroup[] {
  if (!blocks) return [];
  const existing = cache.get(blocks);
  if (existing) return existing;
  const result = groupBlocks(blocks);
  cache.set(blocks, result);
  return result;
}
