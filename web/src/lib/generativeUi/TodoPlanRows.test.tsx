/**
 * @vitest-environment jsdom
 */
// Shared todo row list (add-session-todos-surface 2.2): the session-todos
// popover reuses the transcript card's row states, so the rows live in one
// exported component and this file pins it. The transcript card that mounts
// these rows is covered by components/chat/ToolCall.test.tsx.
import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { TodoPlanRows } from './TodoChecklistCard';
import type { TodoItem } from './TodoChecklistCard';

const ITEMS: TodoItem[] = [
  { key: 'setup', text: 'Set up env', status: 'done', reason: '' },
  { key: 'deploy', text: 'Deploy staging', status: 'active', reason: '' },
  { key: 'docs', text: 'Draft docs', status: 'pending', reason: '' },
  { key: 'dns', text: 'Flip DNS', status: 'failed', reason: 'registrar API down' },
];

describe('lib/generativeUi/TodoPlanRows', () => {
  it('renders one li per item, attributed data-od-id="todo-row-<key>"', () => {
    const { container } = render(<TodoPlanRows items={ITEMS}/>);
    const rows = container.querySelectorAll('li');
    expect(rows).toHaveLength(4);
    for (const item of ITEMS) {
      expect(container.querySelector(`li[data-od-id="todo-row-${item.key}"]`)).not.toBeNull();
    }
    expect(rows[0].textContent).toContain('Set up env');
  });

  it('a done row is struck through and dimmed', () => {
    const { container } = render(<TodoPlanRows items={ITEMS}/>);
    const text = container.querySelector('li[data-od-id="todo-row-setup"] span') as HTMLElement;
    expect(text.className).toContain('line-through');
    expect(text.className).toContain('text-muted');
  });

  it('an active row carries the spinning mark', () => {
    const { container } = render(<TodoPlanRows items={ITEMS}/>);
    const spinner = container.querySelector('li[data-od-id="todo-row-deploy"] .od-genui-spin');
    expect(spinner).not.toBeNull();
  });

  it('a pending row is dimmed with the border-circle mark and no spinner', () => {
    const { container } = render(<TodoPlanRows items={ITEMS}/>);
    const row = container.querySelector('li[data-od-id="todo-row-docs"]') as HTMLElement;
    const text = row.querySelector('span:last-child') as HTMLElement;
    expect(text.className).toContain('text-muted');
    expect(row.querySelector('.od-genui-spin')).toBeNull();
    const mark = row.querySelector('span.rounded-full.border') as HTMLElement;
    expect(mark).not.toBeNull();
    expect(mark.className).toContain('border-muted');
  });

  it('a failed row is in the danger color with its reason beneath', () => {
    const { container } = render(<TodoPlanRows items={ITEMS}/>);
    const row = container.querySelector('li[data-od-id="todo-row-dns"]') as HTMLElement;
    const text = row.querySelector('span:last-child') as HTMLElement;
    expect(text.className).toContain('text-danger');
    const reason = row.querySelector('p') as HTMLElement;
    expect(reason).not.toBeNull();
    expect(reason.textContent).toBe('registrar API down');
    expect(reason.className).toContain('text-danger');
  });

  it('an item with empty text renders (untitled)', () => {
    const { container } = render(
      <TodoPlanRows items={[{ key: 'blank', text: '', status: 'pending', reason: '' }]}/>
    );
    const row = container.querySelector('li[data-od-id="todo-row-blank"]') as HTMLElement;
    expect(row.textContent).toContain('(untitled)');
  });

  it('renders rows only — no card header or chrome', () => {
    const { container } = render(<TodoPlanRows items={ITEMS}/>);
    // The component's root IS the row list; the "Todos" label and the n/m
    // counter belong to the callers (transcript card, popover header).
    expect(container.firstElementChild!.tagName).toBe('UL');
    expect(container.textContent).not.toContain('Todos');
    expect(container.querySelector('.font-mono')).toBeNull();
  });
});
