import type { ContentBlock } from '../../types/chat';

export type ContentGroup = { type: 'single'; block: ContentBlock };

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
