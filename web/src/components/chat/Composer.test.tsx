import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Composer } from './Composer';
import { useStore } from '../../store';
import { attachCatchUpStream, abortCatchUpStream } from '../../lib/livechat';

const skillGroups = [
  {
    label: 'System' as const,
    skills: [
      { name: 'web-research', description: 'Multi-source research briefs.' },
      { name: 'code-execution', description: 'Sandboxed Python.' },
    ],
  },
  {
    label: 'Workspace' as const,
    skills: [
      { name: 'changelog-sweeper', description: 'Sweeps commit logs.' },
      // A disabled workspace skill never appears in the groups at all.
    ],
  },
  {
    label: 'This agent' as const,
    skills: [{ name: 'pdf-sweep', description: 'Sweeps PDFs.' }],
  },
];

function setup() {
  const onSend = vi.fn();
  const utils = render(
    <Composer
      agent={{ name: 'Atlas' }}
      running={false}
      onSend={onSend}
      onCancel={vi.fn()}
      onAttach={vi.fn()}
      skillGroups={skillGroups}
    />
  );
  const input = screen.getByLabelText('Message input') as HTMLTextAreaElement;
  return { input, onSend, unmount: utils.unmount };
}

describe('components/chat/Composer $ skill menu', () => {
  it('opens on $ and filters by the typed token', () => {
    const { input } = setup();

    fireEvent.change(input, { target: { value: '$web' } });

    expect(screen.getByTestId('skill-menu')).not.toBeNull();
    expect(screen.getByTestId('skill-option-web-research')).not.toBeNull();
    expect(screen.queryByTestId('skill-option-code-execution')).toBeNull();
    // Non-matching groups drop out of the filtered list
    expect(screen.queryByText('Workspace')).toBeNull();

    // Unfiltered, the groups label the three tiers
    fireEvent.change(input, { target: { value: '$' } });
    expect(screen.getByText('System')).not.toBeNull();
    expect(screen.getByText('Workspace')).not.toBeNull();
    expect(screen.getByText('This agent')).not.toBeNull();
  });

  it('arrow keys move the selection and Enter picks, inserting $name and refocusing', () => {
    const { input } = setup();

    fireEvent.change(input, { target: { value: '$' } });
    // Flat order: web-research, code-execution, changelog-sweeper, pdf-sweep
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(input.value).toBe('$changelog-sweeper ');
    expect(document.activeElement).toBe(input);
  });

  it('clicking an entry replaces the token with $name ', () => {
    const { input } = setup();

    fireEvent.change(input, { target: { value: 'please run $pdf' } });
    fireEvent.mouseDown(screen.getByTestId('skill-option-pdf-sweep'));

    expect(input.value).toBe('please run $pdf-sweep ');
  });

  it('sends an unmatched $name as ordinary text', () => {
    const { input, onSend } = setup();

    fireEvent.change(input, { target: { value: '$no-such-skill hello' } });
    expect(screen.queryByTestId('skill-menu')).toBeNull();

    fireEvent.keyDown(input, { key: 'Enter' });
    // add-chat-attachments D11: onSend carries the (here empty) ready chips.
    expect(onSend).toHaveBeenCalledWith('$no-such-skill hello', []);
  });

  it('escape dismisses the menu and $ does not open it without groups', () => {
    const { input, unmount } = setup();
    fireEvent.change(input, { target: { value: '$we' } });
    fireEvent.keyDown(input, { key: 'Escape' });
    expect(screen.queryByTestId('skill-menu')).toBeNull();
    unmount();

    // Without skill groups (e.g. skills API failed), $ stays plain text.
    const onSend2 = vi.fn();
    const utils2 = render(
      <Composer agent={{ name: 'Atlas' }} running={false} onSend={onSend2} onCancel={vi.fn()} onAttach={vi.fn()} />
    );
    const input2 = utils2.getByLabelText('Message input') as HTMLTextAreaElement;
    fireEvent.change(input2, { target: { value: '$web' } });
    expect(utils2.queryByTestId('skill-menu')).toBeNull();
  });
});

describe('components/chat/Composer /compact slash menu (chat-compact-command)', () => {
  it('agent chat: opens on / and lists only /compact; Enter picks it with a trailing space', () => {
    const onSend = vi.fn();
    render(
      <Composer agent={{ name: 'Atlas' }} running={false} onSend={onSend} onCancel={vi.fn()} onAttach={vi.fn()} allowCommands />
    );
    const input = screen.getByLabelText('Message input') as HTMLTextAreaElement;

    fireEvent.change(input, { target: { value: '/comp' } });
    const menu = document.querySelector('[data-od-id="slash-menu"]')!;
    expect(menu).not.toBeNull();
    expect(menu.textContent).toContain('/compact');
    expect(menu.textContent).toContain("Compact this conversation's context");
    expect(menu.querySelectorAll('button').length).toBe(1);

    fireEvent.keyDown(input, { key: 'Enter' });
    expect(input.value).toBe('/compact ');
  });

  it('agent chat: typing /compact with focus text closes the menu and Enter sends the raw text', () => {
    const onSend = vi.fn();
    render(
      <Composer agent={{ name: 'Atlas' }} running={false} onSend={onSend} onCancel={vi.fn()} onAttach={vi.fn()} allowCommands />
    );
    const input = screen.getByLabelText('Message input') as HTMLTextAreaElement;

    fireEvent.change(input, { target: { value: '/compact keep the decisions' } });
    expect(document.querySelector('[data-od-id="slash-menu"]')).toBeNull();
    fireEvent.keyDown(input, { key: 'Enter' });
    // add-chat-attachments D11: onSend carries the (here empty) ready chips.
    expect(onSend).toHaveBeenCalledWith('/compact keep the decisions', []);
  });

  it('channel composer: /compact never opens the menu and passes through as plain text', () => {
    const onSend = vi.fn();
    render(
      <Composer
        agent={{ name: 'Atlas' }}
        running={false}
        onSend={onSend}
        onCancel={vi.fn()}
        onAttach={vi.fn()}
        mentionOptions={[{ id: 'p1', kind: 'person', name: 'Pat' }]}
      />
    );
    const input = screen.getByLabelText('Message input') as HTMLTextAreaElement;

    fireEvent.change(input, { target: { value: '/compact' } });
    expect(document.querySelector('[data-od-id="slash-menu"]')).toBeNull();
    fireEvent.keyDown(input, { key: 'Enter' });
    // add-chat-attachments D11: onSend carries the (here empty) ready chips.
    expect(onSend).toHaveBeenCalledWith('/compact', []);
  });
});

describe('components/chat/Composer draft restore (adopt-assistant-ui-elements 9.3)', () => {
  // This environment's jsdom may expose no localStorage (same mode behind the
  // ~66 pre-existing failures); install a minimal stub so the lifecycle runs.
  if (typeof (globalThis as any).localStorage === 'undefined') {
    const backing = new Map<string, string>();
    (globalThis as any).localStorage = {
      getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
      setItem: (k: string, v: string) => void backing.set(k, String(v)),
      removeItem: (k: string) => void backing.delete(k),
      clear: () => void backing.clear(),
      key: (i: number) => Array.from(backing.keys())[i] ?? null,
      get length() { return backing.size; },
    };
  }

  beforeEach(() => {
    localStorage.clear();
  });

  const mount = (chatId: string, onSend = vi.fn()) =>
    render(
      <Composer
        agent={{ name: 'Atlas' }}
        running={false}
        onSend={onSend}
        onCancel={vi.fn()}
        chatId={chatId}
      />
    );

  const input = () => screen.getByLabelText('Message input') as HTMLTextAreaElement;

  it('typed text persists per thread and is restored when the composer remounts', () => {
    const first = mount('a1');
    fireEvent.change(input(), { target: { value: 'half-written reply' } });
    expect(localStorage.getItem('onclaw.draft.a1')).toBe('half-written reply');
    first.unmount();

    // ChatRoute remounts the Composer per chat — the mount initializer is
    // the restore point.
    mount('a1');
    expect(input().value).toBe('half-written reply');
  });

  it('drafts are per thread — another thread restores nothing', () => {
    const first = mount('a1');
    fireEvent.change(input(), { target: { value: 'for atlas only' } });
    first.unmount();

    mount('a2');
    expect(input().value).toBe('');
    expect(localStorage.getItem('onclaw.draft.a1')).toBe('for atlas only');
  });

  it('sending clears the saved draft — a later visit shows an empty composer', () => {
    const onSend = vi.fn();
    const first = mount('a1', onSend);
    fireEvent.change(input(), { target: { value: 'ship it' } });
    fireEvent.keyDown(input(), { key: 'Enter' });
    expect(onSend).toHaveBeenCalledWith('ship it', []);
    expect(localStorage.getItem('onclaw.draft.a1')).toBeNull();
    first.unmount();

    mount('a1');
    expect(input().value).toBe('');
  });

  it('emptying the composer clears the stored draft too', () => {
    mount('a1');
    fireEvent.change(input(), { target: { value: 'scratch' } });
    fireEvent.change(input(), { target: { value: '' } });
    expect(localStorage.getItem('onclaw.draft.a1')).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Documents affordance note (rework-document-chat-surfaces 2.1, 2026-09-28
// user pivot): the toolbar button is GONE — the documents entry point lives
// in the chat header beside the panel toggle (ChatHeader.test.tsx owns the
// toggle tests; ChatRoute.test.tsx owns the end-to-end). The composer keeps
// only the mention bridge below.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Document mention bridge (rework-document-chat-surfaces 2.2): the composer
// registers its insertion callback with the mounting screen through the
// ref-style registration prop; the panel's Documents listing inserts
// mentions through it (ChatRoute bridge).
// ---------------------------------------------------------------------------

describe('components/chat/Composer document mention bridge (rework-document-chat-surfaces 2.2)', () => {
  it('registers an insertion callback that adds the text token and the identity chip', () => {
    const registerDocMention = vi.fn();
    const onSend = vi.fn();
    render(
      <Composer
        agent={{ name: 'Atlas' }}
        running={false}
        onSend={onSend}
        onCancel={vi.fn()}
        registerDocMention={registerDocMention}
      />
    );
    expect(registerDocMention).toHaveBeenCalledTimes(1);
    const insert = registerDocMention.mock.calls[0][0] as (doc: { id: string; name: string }) => void;

    const input = screen.getByLabelText('Message input') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'compare this with the limits' } });
    // The bridge callback is imperative — updates flush inside act, exactly
    // like the panel row's click dispatch does in the real surface.
    act(() => { insert({ id: 'doc-1', name: 'twilio-api.pdf' }); });

    // The markdown link token IS the visible pill the transcript renders.
    expect(input.value).toBe('compare this with the limits [📄 twilio-api.pdf](references/twilio-api.pdf) ');
    expect(screen.getByTestId('document-chip')).not.toBeNull();
    expect(screen.getByTestId('document-chip').getAttribute('data-document-id')).toBe('doc-1');

    // Re-inserting the same document never duplicates the identity chip.
    act(() => { insert({ id: 'doc-1', name: 'twilio-api.pdf' }); });
    expect(screen.getAllByTestId('document-chip')).toHaveLength(1);

    // The chip rides the send as document identity (pointer note machinery).
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onSend).toHaveBeenCalledWith(
      'compare this with the limits [📄 twilio-api.pdf](references/twilio-api.pdf) [📄 twilio-api.pdf](references/twilio-api.pdf)',
      [{ kind: 'document', documentId: 'doc-1', name: 'twilio-api.pdf', path: 'references/twilio-api.pdf' }]
    );
  });

  it('deregisters the callback on unmount (conversation switch)', () => {
    const registerDocMention = vi.fn();
    const utils = render(
      <Composer
        agent={{ name: 'Atlas' }}
        running={false}
        onSend={vi.fn()}
        onCancel={vi.fn()}
        registerDocMention={registerDocMention}
      />
    );
    utils.unmount();
    expect(registerDocMention).toHaveBeenLastCalledWith(null);
  });
});

// ---------------------------------------------------------------------------
// Stop control on a followed run (fix-chat-stop-on-reattached-run D2): the
// composer's Stop/Send flip projects ui.running exactly like ChatView wires
// it (busy={ui.running}). While the page follows a run through the catch-up
// stream, Stop must clear the control immediately on click — the runtime's
// abort-before-patch ordering — and the post-abort event guard must keep a
// stale followed-stream event from re-asserting the spinner (the control
// never reappears).
// ---------------------------------------------------------------------------

describe('components/chat/Composer stop control on a followed run (fix-chat-stop-on-reattached-run D2)', () => {
  // This environment's jsdom may expose no localStorage (same mode behind the
  // ~66 pre-existing failures); install a minimal stub so the store boots.
  if (typeof (globalThis as any).localStorage === 'undefined') {
    const backing = new Map<string, string>();
    (globalThis as any).localStorage = {
      getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
      setItem: (k: string, v: string) => void backing.set(k, String(v)),
      removeItem: (k: string) => void backing.delete(k),
      clear: () => void backing.clear(),
      key: (i: number) => Array.from(backing.keys())[i] ?? null,
      get length() { return backing.size; },
    };
  }

  beforeEach(() => {
    localStorage.clear();
    const db: any = {
      ws1: {
        id: 'ws1', name: 'WS', sub: 'ws1', tz: 'UTC',
        agents: [], channels: [], people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
        threads: { a1: { active: 'sess_live', list: [{ id: 'sess_live', title: 'Live', updated: '', messages: [] }] } },
      },
    };
    act(() => {
      useStore.setState({
        db,
        pos: { tenantId: 'ws1', view: 'chats', chatId: 'a1', showContext: false, railExpanded: false },
        ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, runningChatId: null, toasts: [] },
      });
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // The real ChatView wiring: `running` reads ui.running, so the control's
  // flips project the run state the runtime and the catch-up stream drive.
  const Harness = ({ onCancel }: { onCancel: () => void }) => {
    const running = useStore((s: any) => !!s.ui.running);
    return <Composer agent={{ name: 'Atlas' }} running={running} onSend={vi.fn()} onCancel={onCancel} />;
  };

  it('stop clears immediately on click and does not reappear from a stale followed-stream event', async () => {
    // The followed stream: a run_active frame flips the spinner, then a
    // trailing INCOMPLETE delta frame parks in the consumer's buffer — the
    // stop-abort ends the read loop and its final-buffer flush delivers the
    // stale event AFTER the stop (the exact shape the runtime guard drops).
    const enc = new TextEncoder();
    const fr = (ev: any) => `data: ${JSON.stringify(ev)}\n\n`;
    vi.stubGlobal('fetch', vi.fn(async () =>
      ({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'text/event-stream' }),
        body: new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(enc.encode(fr({ kind: 'run_active', occurred_at: 'x' })));
            controller.enqueue(enc.encode(`data: ${JSON.stringify({ id: 'e9', kind: 'text_delta', occurred_at: 'x', turn_id: 't1', text_delta: 'late' })}`));
          },
        }),
      }) as unknown as Response
    ));
    attachCatchUpStream({ workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'a1', sessionId: 'sess_live' });

    // The runtime's landed stop sequence (fix-chat-stop-on-reattached-run D2):
    // detach the followed stream FIRST, then clear the running state.
    const onCancel = () => {
      abortCatchUpStream('a1');
      useStore.getState().patchUi({ running: false });
    };
    render(<Harness onCancel={onCancel} />);

    // Following the run shows the Stop control, not Send.
    await waitFor(() => expect(useStore.getState().ui.running).toBe(true));
    expect(document.querySelector('[data-od-id="btn-cancel"]')).not.toBeNull();
    expect(screen.queryByTestId('btn-send')).toBeNull();

    // The control clears on the click's very render — the abort-before-patch
    // ordering leaves nothing to re-assert the spinner.
    fireEvent.click(document.querySelector('[data-od-id="btn-cancel"]')!);
    expect(useStore.getState().ui.running).toBe(false);
    expect(document.querySelector('[data-od-id="btn-cancel"]')).toBeNull();
    expect(screen.queryByTestId('btn-send')).not.toBeNull();

    // The stale event fires post-abort and is dropped by the guard: the
    // spinner is never re-asserted, so the Stop control never reappears.
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(useStore.getState().ui.running).toBe(false);
    expect(document.querySelector('[data-od-id="btn-cancel"]')).toBeNull();
    expect(screen.queryByTestId('btn-send')).not.toBeNull();
  });
});
