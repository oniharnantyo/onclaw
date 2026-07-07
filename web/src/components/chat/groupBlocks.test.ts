import { groupBlocks } from './groupBlocks';
import { pickToolRenderer, SkillActivatedBlock } from './Renderers';
import type { ContentBlock } from '../../types/chat';

export function runGroupBlocksTests() {
  // Test Case 1: Empty blocks
  const emptyResult = groupBlocks([]);
  if (emptyResult.length !== 0) {
    throw new Error(`Test 1 Failed: Expected empty result, got ${emptyResult.length}`);
  }

  // Test Case 2: Lone reasoning block
  const loneReasoning: ContentBlock[] = [
    { type: 'reasoning', reasoning: { text: 'thinking' } }
  ];
  const loneReasoningResult = groupBlocks(loneReasoning);
  if (loneReasoningResult.length !== 1 || loneReasoningResult[0].type !== 'single') {
    throw new Error('Test 2 Failed: Lone reasoning should be a single block');
  }

  // Test Case 3: Reasoning followed by tools (now remains separate)
  const cotBlocks: ContentBlock[] = [
    { type: 'reasoning', reasoning: { text: 'thinking' } },
    { type: 'function_tool_call', function_tool_call: { id: '1', name: 'search', arguments: '{}' } }
  ];
  const cotResult = groupBlocks(cotBlocks);
  if (cotResult.length !== 2 || cotResult[0].type !== 'single' || cotResult[1].type !== 'single') {
    throw new Error('Test 3 Failed: Reasoning followed by tool should remain separate');
  }

  // Test Case 4: Tool Group (Run of >= 2 tools)
  const toolGroupBlocks: ContentBlock[] = [
    { type: 'function_tool_call', function_tool_call: { id: '1', name: 'search', arguments: '{}' } },
    { type: 'function_tool_call', function_tool_call: { id: '2', name: 'fetch', arguments: '{}' } }
  ];
  const toolGroupResult = groupBlocks(toolGroupBlocks);
  // After simplification, each tool is now rendered individually as single blocks
  if (toolGroupResult.length !== 2 || toolGroupResult[0].type !== 'single' || toolGroupResult[1].type !== 'single') {
    throw new Error('Test 4 Failed: Two consecutive tools should be individual single blocks');
  }

  // Test Case 5: Single tool call (should remain single)
  const singleToolBlocks: ContentBlock[] = [
    { type: 'function_tool_call', function_tool_call: { id: '1', name: 'search', arguments: '{}' } }
  ];
  const singleToolResult = groupBlocks(singleToolBlocks);
  if (singleToolResult.length !== 1 || singleToolResult[0].type !== 'single') {
    throw new Error('Test 5 Failed: Single tool call should remain single block');
  }

  // Test Case 6: pickToolRenderer
  const renderer = pickToolRenderer('skill');
  if (renderer !== SkillActivatedBlock) {
    throw new Error('Test 6 Failed: pickToolRenderer("skill") should return SkillActivatedBlock');
  }
  const genericRenderer = pickToolRenderer('other_tool');
  if (genericRenderer !== null) {
    throw new Error('Test 6 Failed: pickToolRenderer("other_tool") should return null');
  }
}
