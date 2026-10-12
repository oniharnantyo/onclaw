/**
 * @vitest-environment jsdom
 */
// Channel delivery feed → room transcript entries (add-webhook-channel-visibility
// D3): the mapping's author lanes (agent / own user / other member), the id
// prefix that keeps feed keys clear of session message keys, and the
// chronological merge with the session's own messages.
import { describe, it, expect } from 'vitest';
import { channelFeedEntries, mergeChannelFeed } from './channelFeed';
import type { ApiChannelMessage } from './api';

const row = (over: Partial<ApiChannelMessage>): ApiChannelMessage => ({
  id: 'cm-1',
  workspace_id: 'ws1',
  channel_id: 'ch-1',
  seq: 1,
  author_type: 'agent',
  author_agent_id: 'a-atlas',
  body: 'the webhook reply',
  mentions: [],
  chain_depth: 0,
  created_at: '2026-10-11T15:04:05Z',
  ...over,
});

describe('lib/channelFeed', () => {
  it('maps agent rows to agent entries carrying the invoked agent', () => {
    const entries = channelFeedEntries([row({})], 'u1');
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      id: 'feed-cm-1',
      author: 'agent',
      agentId: 'a-atlas',
      text: 'the webhook reply',
      at: '2026-10-11T15:04:05Z',
    });
    expect(entries[0].ts).not.toBe('');
  });

  it("maps the workspace member's own rows to own messages and others to member messages", () => {
    const entries = channelFeedEntries([
      row({ id: 'cm-2', author_type: 'user', author_agent_id: undefined, author_user_id: 'u1', body: 'mine' }),
      row({ id: 'cm-3', author_type: 'user', author_agent_id: undefined, author_user_id: 'u2', body: 'theirs' }),
    ], 'u1');
    expect(entries[0]).toMatchObject({ id: 'feed-cm-2', author: 'you', text: 'mine' });
    expect(entries[1]).toMatchObject({ id: 'feed-cm-3', author: 'other', memberId: 'u2', text: 'theirs' });
  });

  it('merges feed entries with the session transcript in chronological order', () => {
    const session = [
      { id: 'm2', author: 'agent', at: '2026-10-11T16:00:00Z', text: 'later session turn' },
      { id: 'm1', author: 'you', at: '2026-10-11T14:00:00Z', text: 'early member post' },
    ];
    const feed = channelFeedEntries([row({ created_at: '2026-10-11T15:00:00Z' })]);
    const merged = mergeChannelFeed(session, feed);
    expect(merged.map((m) => m.id)).toEqual(['m1', 'feed-cm-1', 'm2']);
  });

  it('tolerates an empty feed and a missing transcript', () => {
    expect(mergeChannelFeed(undefined, [])).toEqual([]);
    expect(channelFeedEntries(undefined, 'u1')).toEqual([]);
  });
});
