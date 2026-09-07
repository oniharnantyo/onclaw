import { describe, it, expect, vi } from 'vitest';
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
    expect(onSend).toHaveBeenCalledWith('$no-such-skill hello');
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
