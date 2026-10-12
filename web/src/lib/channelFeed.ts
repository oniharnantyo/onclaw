import type { ApiChannelMessage } from './api';
import { nowTime } from './helpers';

// ---------------------------------------------------------------------------
// Channel delivery feed → room transcript entries (add-webhook-channel-visibility,
// design D3): the channel's message feed is the room's delivery surface —
// webhook, scheduler, and gateway replies auto-post to it (the chokepoint's
// PostFromAgent lane). These helpers fold feed rows into the transcript entry
// shape ChatView's per-author renderer already speaks, and merge them with the
// session's own messages in chronological order.
// ---------------------------------------------------------------------------

/** One room transcript entry derived from a feed row. `id` is prefixed so a
 * feed row can never collide with a session message key. */
export interface ChannelFeedEntry {
  id: string;
  author: 'you' | 'other' | 'agent';
  text: string;
  /** ISO instant — the merge and the day separators' sort key. */
  at: string;
  /** Display clock label, derived from the delivery timestamp. */
  ts: string;
  /** author='agent': the invoked agent — the room's renderer resolves the
   * name/avatar from the workspace roster. */
  agentId?: string;
  /** author='other': the posting user (name resolution is roster work the
   * v1 room doesn't hold — the generic member label renders). */
  memberId?: string;
}

/** Maps feed rows to transcript entries. The workspace member's own posts
 * render as own messages; agent rows carry the invoked agent; every other
 * author renders as a member message. */
export function channelFeedEntries(rows: ApiChannelMessage[] | undefined, currentUserId?: string): ChannelFeedEntry[] {
  return (rows || []).map((m) => {
    const at = m.created_at;
    const ts = clockLabel(at);
    if (m.author_type === 'agent' && m.author_agent_id) {
      return { id: 'feed-' + m.id, author: 'agent' as const, agentId: m.author_agent_id, text: m.body, at, ts };
    }
    if (m.author_type === 'user' && currentUserId && m.author_user_id === currentUserId) {
      return { id: 'feed-' + m.id, author: 'you' as const, text: m.body, at, ts };
    }
    return { id: 'feed-' + m.id, author: 'other' as const, memberId: m.author_user_id, text: m.body, at, ts };
  });
}

/** Merges the session's own messages with the feed entries, oldest first —
 * the transcript's day-separator pass wraps whatever order this returns. */
export function mergeChannelFeed(sessionMessages: any[] | undefined, feed: ChannelFeedEntry[]): any[] {
  return [...(sessionMessages || []), ...feed].sort((a, b) =>
    String(a.at || '').localeCompare(String(b.at || '')));
}

/** The display clock for a feed row: the delivery's local time, the same
 * label shape live turns carry. An unparseable stamp falls back to the
 * current clock rather than rendering an empty label. */
function clockLabel(at: string): string {
  const d = at ? new Date(at) : null;
  if (!d || isNaN(d.getTime())) return nowTime();
  return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
}
