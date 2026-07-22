import { lastTurnResponseID, isValidConversationId } from '../ChatProvider';
import type { RawTurn } from '../../types/chat';

export function runLastResponseIdTests() {
  // Test 1: Empty input returns empty string
  if (lastTurnResponseID([]) !== '') {
    throw new Error('Test 1 Failed: empty rawMsgs should return empty string');
  }

  // Test 2: Summary-only turns return empty string
  const summaryOnly: RawTurn[] = [
    { is_summary: true, response_id: 'resp_123' },
    { is_summary: true, response_id: 'resp_456' },
  ];
  if (lastTurnResponseID(summaryOnly) !== '') {
    throw new Error('Test 2 Failed: summary turns should be skipped');
  }

  // Test 3: Returns last non-summary turn's response_id
  const mixedTurns: RawTurn[] = [
    { is_summary: false, response_id: 'resp_first' },
    { is_summary: false, response_id: 'resp_second' },
    { is_summary: true, response_id: 'resp_summary' },
  ];
  if (lastTurnResponseID(mixedTurns) !== 'resp_second') {
    throw new Error(`Test 3 Failed: expected "resp_second", got "${lastTurnResponseID(mixedTurns)}"`);
  }

  // Test 4: Missing or non-string response_id returns empty string
  const invalidIdTurns: RawTurn[] = [
    { is_summary: false, response_id: 123 as any },
  ];
  if (lastTurnResponseID(invalidIdTurns) !== '') {
    throw new Error('Test 4 Failed: non-string response_id should return empty string');
  }

  // Test 5: isValidConversationId helper
  if (!isValidConversationId(1)) throw new Error('Test 5 Failed: 1 should be valid');
  if (isValidConversationId(0)) throw new Error('Test 5 Failed: 0 should be invalid');
  if (isValidConversationId(-1)) throw new Error('Test 5 Failed: -1 should be invalid');
  if (isValidConversationId(1.5)) throw new Error('Test 5 Failed: 1.5 should be invalid');
  if (isValidConversationId(null)) throw new Error('Test 5 Failed: null should be invalid');
  if (isValidConversationId(undefined)) throw new Error('Test 5 Failed: undefined should be invalid');
}
