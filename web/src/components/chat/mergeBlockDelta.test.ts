import { mergeBlockDelta, mergeStreamingDeltas } from './mergeBlockDelta';
import type { ContentBlock } from '../../types/chat';

export function runMergeBlockDeltaTests() {
  // Test 1: Text-delta merge accumulates into one block by index
  let out = mergeStreamingDeltas([], [
    { type: 'assistant_gen_text', assistant_gen_text: { text: 'Hello' }, streaming_meta: { index: 0 } },
    { type: 'assistant_gen_text', assistant_gen_text: { text: ' world' }, streaming_meta: { index: 0 } },
  ]);
  if (out.length !== 1) throw new Error(`Test 1 Failed: expected 1 merged block, got ${out.length}`);
  if (out[0].assistant_gen_text?.text !== 'Hello world') throw new Error('Test 1 Failed: text not accumulated');

  // Test 2: Multi-index blocks stay separate and route by index
  out = mergeStreamingDeltas([], [
    { type: 'assistant_gen_text', assistant_gen_text: { text: 'A' }, streaming_meta: { index: 0 } },
    { type: 'assistant_gen_text', assistant_gen_text: { text: 'B' }, streaming_meta: { index: 1 } },
    { type: 'assistant_gen_text', assistant_gen_text: { text: 'C' }, streaming_meta: { index: 0 } },
  ]);
  if (out.length !== 2) throw new Error(`Test 2 Failed: expected 2 blocks, got ${out.length}`);
  if (out[0].assistant_gen_text?.text !== 'AC') throw new Error('Test 2 Failed: index 0 should be AC');
  if (out[1].assistant_gen_text?.text !== 'B') throw new Error('Test 2 Failed: index 1 should be B');

  // Test 3: Tool-call argument accumulation (partial JSON fragments)
  const toolOut = mergeStreamingDeltas([], [
    { type: 'function_tool_call', function_tool_call: { id: '1', name: 'search', arguments: '' }, streaming_meta: { index: 0 } },
    { type: 'function_tool_call', function_tool_call: { id: '1', name: 'search', arguments: '{"q":"hel' }, streaming_meta: { index: 0 } },
    { type: 'function_tool_call', function_tool_call: { id: '1', name: 'search', arguments: 'lo"}' }, streaming_meta: { index: 0 } },
  ]);
  const tc = toolOut[0].function_tool_call;
  if (!tc) throw new Error('Test 3 Failed: missing tool call');
  if (tc.name !== 'search') throw new Error('Test 3 Failed: name should be search');
  if (tc.arguments !== '{"q":"hello"}') throw new Error(`Test 3 Failed: arguments not accumulated: ${tc.arguments}`);

  // Test 4: Out-of-order arrival still routes to the correct index
  out = mergeStreamingDeltas([], [
    { type: 'assistant_gen_text', assistant_gen_text: { text: 'last' }, streaming_meta: { index: 0 } },
    { type: 'assistant_gen_text', assistant_gen_text: { text: 'first' }, streaming_meta: { index: 0 } },
  ]);
  if (out.length !== 1) throw new Error('Test 4 Failed: expected 1 block');
  if (out[0].assistant_gen_text?.text !== 'lastfirst') throw new Error('Test 4 Failed: order not preserved per index');

  // Test 5: Reasoning merge
  out = mergeStreamingDeltas([], [
    { type: 'reasoning', reasoning: { text: 'think' }, streaming_meta: { index: 0 } },
    { type: 'reasoning', reasoning: { text: 'ing' }, streaming_meta: { index: 0 } },
  ]);
  if (out[0].reasoning?.text !== 'thinking') throw new Error('Test 5 Failed: reasoning not accumulated');

  // Test 6: Block without an index appends whole (persisted re-sync payload)
  out = mergeStreamingDeltas([], [
    { type: 'assistant_gen_text', assistant_gen_text: { text: 'whole' } },
  ]);
  if (out.length !== 1) throw new Error('Test 6 Failed: expected 1 appended block');
  if (out[0].assistant_gen_text?.text !== 'whole') throw new Error('Test 6 Failed: block text mismatch');

  // Test 7: mergeBlockDelta is pure (does not mutate the target)
  const target: ContentBlock = { type: 'assistant_gen_text', assistant_gen_text: { text: 'a' }, streaming_meta: { index: 0 } };
  const delta: ContentBlock = { type: 'assistant_gen_text', assistant_gen_text: { text: 'b' }, streaming_meta: { index: 0 } };
  const merged = mergeBlockDelta(target, delta);
  if (merged.assistant_gen_text?.text !== 'ab') throw new Error('Test 7 Failed: merge wrong');
  if (target.assistant_gen_text?.text !== 'a') throw new Error('Test 7 Failed: target was mutated');

  // Test 8: Eino streams tool-call deltas where the identity fields (name,
  // call_id) only arrive in the first fragment; later fragments carry empty
  // strings. The merge must preserve the real name instead of clobbering it
  // with the empty value from subsequent fragments.
  const tcFragments = mergeStreamingDeltas([], [
    { type: 'function_tool_call', function_tool_call: { call_id: 'c1', name: 'ls', arguments: '' } as any, streaming_meta: { index: 0 } },
    { type: 'function_tool_call', function_tool_call: { call_id: '', name: '', arguments: '{"path":' } as any, streaming_meta: { index: 0 } },
    { type: 'function_tool_call', function_tool_call: { call_id: '', name: '', arguments: ' "."}' } as any, streaming_meta: { index: 0 } },
  ]);
  const ftc = tcFragments[0].function_tool_call as any;
  if (!ftc) throw new Error('Test 8 Failed: missing tool call');
  if (ftc.name !== 'ls') throw new Error(`Test 8 Failed: name clobbered to "${ftc.name}"`);
  if (ftc.call_id !== 'c1') throw new Error(`Test 8 Failed: call_id clobbered to "${ftc.call_id}"`);
  if (ftc.arguments !== '{"path": "."}') throw new Error(`Test 8 Failed: arguments not accumulated: ${ftc.arguments}`);
}
