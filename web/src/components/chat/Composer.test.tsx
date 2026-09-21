import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Composer } from './Composer';

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
