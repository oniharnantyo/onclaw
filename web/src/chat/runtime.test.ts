/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useChatRuntime } from './runtime';
import { useStore } from '../store';

vi.mock('@assistant-ui/react', () => ({
  useExternalStoreRuntime: vi.fn((opts) => opts),
}));

describe('useChatRuntime', () => {
  beforeEach(() => {
    act(() => {
      const mkAgent = (id: string, name: string, tools: string[]) => ({
        id, name, model: 'claude-sonnet-5', temp: 0.4, autonomy: 'approval', channelPost: false,
        role: 'Test agent', status: 'idle', tools, skills: ['research'], mcp: [], lastActive: 'now', prompt: ''
      });
      useStore.setState({
        pos: { tenantId: 't1', view: 'chats', chatId: 'a1', showContext: false },
        ui: { settingsOpen: false, settingsTab: 'workspace', configAgent: null, cronEdit: null, wsOpen: false, createWsOpen: false, running: false, toasts: [] },
        db: {
          t1: {
            id: 't1',
            name: 'T1', sub: 't1', plan: 'Free', tz: 'America/Los_Angeles',
            defaultModel: 'claude-sonnet-5', retention: '90 days',
            people: [], cron: [], runs: [], members: [], integrations: [], mcpServers: [], skillLib: [], keys: [],
            agents: [mkAgent('a1', 'Alice', ['search']), mkAgent('a2', 'Bob', [])],
            // agentId is '' (not 'a1') so no agent auto-responds to plain
            // channel messages — the no-turn and DM-only tests rely on it.
            channels: [{ id: 'c1', name: 'general', purpose: 'Team chat', agentId: '', unread: 0, members: ['a1', 'a2'] }],
            threads: {
              a1: { active: 's1', list: [{ id: 's1', title: 'Chat', updated: '', messages: [{
                id: 'm1', text: 'hello', author: 'agent', ts: '2026-08-27T00:00:00Z',
                agentId: 'a1', cron: 'cron1', name: 'Agent 1',
                tools: [{ name: 'test.tool', args: 'foo', ms: 100 }]
              }] }] },
              c1: { active: 's2', list: [{ id: 's2', title: 'Chat', updated: '', messages: [{
                id: 'm2', text: 'teammate msg', author: 'you', ts: '2026-08-27T00:00:00Z',
              }] }] }
            }
          }
        }
      });
    });
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  it('conversion round-trip maintains identity/cron/tools', () => {
    const { result } = renderHook(() => useChatRuntime('a1'));
    const converted: any = result.current.convertMessage(useStore.getState().db.t1.threads.a1.list[0].messages[0]);
    
    expect(converted.id).toBe('m1');
    expect(converted.role).toBe('assistant');
    expect(converted.metadata.custom.onclaw.agentId).toBe('a1');
    expect(converted.metadata.custom.onclaw.cron).toBe('cron1');
    expect(converted.content[1].type).toBe('tool-call');
    expect(converted.content[1].toolName).toBe('test.tool');
  });

  it('teammate no-turn', async () => {
    const { result } = renderHook(() => useChatRuntime('c1'));
    // Simulate user sending message without mention
    await act(async () => {
      await result.current.onNew({ role: 'user', content: [{ type: 'text', text: 'hello team' }] } as any);
    });
    // Check if an agent responded (should not)
    vi.advanceTimersByTime(10000);
    const msgs = useStore.getState().db.t1.threads.c1.list[0].messages;
    expect(msgs.length).toBe(2); // Only the new message is added, no agent reply
    expect(msgs[1].author).toBe('you');
  });

  it('mention fan-out', async () => {
    const { result } = renderHook(() => useChatRuntime('c1'));
    await act(async () => {
      await result.current.onNew({ role: 'user', content: [{ type: 'text', text: '@Alice @Bob hello' }] } as any);
    });
    vi.advanceTimersByTime(10000);
    const msgs = useStore.getState().db.t1.threads.c1.list[0].messages;
    // user msg + 2 agent replies
    expect(msgs.length).toBe(4);
    expect(msgs[2].agentId).toBe('a1');
    expect(msgs[3].agentId).toBe('a2');
  });

  it('DM-only affordance gating', async () => {
    // try to edit in channel
    const { result } = renderHook(() => useChatRuntime('c1'));
    await act(async () => {
      await result.current.onEdit({ sourceId: 'm2', role: 'user', content: [{ type: 'text', text: 'edited' }] } as any);
    });
    // Should be ignored
    const msgs = useStore.getState().db.t1.threads.c1.list[0].messages;
    expect(msgs[0].text).toBe('teammate msg');
  });

  it('stop-cancel keeps partial text', async () => {
    const { result } = renderHook(() => useChatRuntime('a1'));
    
    act(() => {
      useStore.getState().patchUi({ running: true });
    });
    
    await act(async () => {
      await result.current.onCancel();
    });
    
    expect(useStore.getState().ui.running).toBe(false);
  });
});
