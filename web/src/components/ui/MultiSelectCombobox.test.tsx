import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { MultiSelectCombobox, type MultiSelectOption } from './MultiSelectCombobox';

describe('components/ui/MultiSelectCombobox', () => {
  const sampleOptions: MultiSelectOption[] = [
    { value: 'atlas', label: 'Atlas', sublabel: 'Research' },
    { value: 'beacon', label: 'Beacon', sublabel: 'Ops' },
    { value: 'cipher', label: 'Cipher', sublabel: 'Research' },
    { value: 'dormant', label: 'Dormant', sublabel: 'Ops', disabled: true },
    { value: 'ember', label: 'Ember', sublabel: 'Support' },
  ];

  it('renders the closed trigger with placeholder when nothing is selected', () => {
    render(
      <MultiSelectCombobox
        options={sampleOptions}
        value={[]}
        onChange={vi.fn()}
        placeholder="Attach agents…"
      />
    );

    const trigger = screen.getByRole('combobox');
    expect(trigger.textContent).toContain('Attach agents…');
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(trigger.getAttribute('aria-haspopup')).toBe('listbox');
    expect(screen.queryByRole('listbox')).toBeNull();
    // No selection: no chips, no count.
    expect(screen.queryByTestId('multiselect-count')).toBeNull();
    expect(screen.queryByTestId('multiselect-chip-atlas')).toBeNull();
  });

  it('renders each selection as a chip with a remove affordance and an "N Selected" count', () => {
    const { rerender } = render(
      <MultiSelectCombobox options={sampleOptions} value={['atlas']} onChange={vi.fn()} />
    );

    // One selection: a single chip, count reads "1 Selected".
    expect(screen.getByTestId('multiselect-chip-atlas')).not.toBeNull();
    expect(screen.getByTestId('multiselect-chip-atlas').textContent).toContain('Atlas');
    expect(screen.getByTestId('multiselect-chip-remove-atlas')).not.toBeNull();
    expect(screen.getByTestId('multiselect-count').textContent).toContain('1 Selected');

    rerender(
      <MultiSelectCombobox
        options={sampleOptions}
        value={['atlas', 'beacon', 'cipher']}
        onChange={vi.fn()}
      />
    );

    // Every selected value gets its own chip (not a "+2" text summary), in
    // options order.
    const chips = ['atlas', 'beacon', 'cipher'].map(
      (v) => screen.getByTestId(`multiselect-chip-${v}`).textContent
    );
    expect(chips[0]).toContain('Atlas');
    expect(chips[1]).toContain('Beacon');
    expect(chips[2]).toContain('Cipher');
    expect(screen.getByTestId('multiselect-count').textContent).toContain('3 Selected');
    // The old terse summary is gone.
    expect(screen.getByRole('combobox').textContent).not.toContain('+2');
  });

  it('removes a single value from its chip × without opening the popover', () => {
    const onChange = vi.fn();
    render(
      <MultiSelectCombobox
        options={sampleOptions}
        value={['atlas', 'beacon', 'cipher']}
        onChange={onChange}
      />
    );

    fireEvent.click(screen.getByTestId('multiselect-chip-remove-beacon'));

    // Only Beacon dropped; the rest survive in options order.
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(['atlas', 'cipher']);
    // stopPropagation: removing a chip must not also toggle the dropdown.
    expect(screen.queryByRole('listbox')).toBeNull();
    expect(screen.getByRole('combobox').getAttribute('aria-expanded')).toBe('false');
  });

  it('opens on click revealing the search input and one option per option, with aria-selected set', () => {
    render(
      <MultiSelectCombobox
        options={sampleOptions}
        value={['atlas', 'cipher']}
        onChange={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));

    expect(screen.getByRole('combobox').getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('listbox')).not.toBeNull();
    expect(screen.getByTestId('multiselect-search-input')).not.toBeNull();
    expect(screen.getAllByRole('option')).toHaveLength(5);

    expect(screen.getByTestId('multiselect-option-atlas').getAttribute('aria-selected')).toBe('true');
    expect(screen.getByTestId('multiselect-option-beacon').getAttribute('aria-selected')).toBe('false');
    expect(screen.getByTestId('multiselect-option-cipher').getAttribute('aria-selected')).toBe('true');

    // Selected rows show the checkbox affordance tick; unselected rows do not.
    expect(screen.getByTestId('multiselect-check-atlas').querySelector('svg')).not.toBeNull();
    expect(screen.getByTestId('multiselect-check-beacon').querySelector('svg')).toBeNull();
  });

  it('toggling keeps the popover open and the query intact, emitting the full next array', () => {
    const onChange = vi.fn();
    render(
      <MultiSelectCombobox options={sampleOptions} value={['atlas']} onChange={onChange} />
    );

    fireEvent.click(screen.getByRole('combobox'));
    const searchInput = screen.getByTestId('multiselect-search-input') as HTMLInputElement;
    fireEvent.change(searchInput, { target: { value: 'be' } });

    fireEvent.click(screen.getByTestId('multiselect-option-beacon'));

    expect(onChange).toHaveBeenCalledWith(['atlas', 'beacon']);
    // Defining behavior: the popover stays open and the query is not reset, so
    // several options can be ticked in one pass.
    expect(screen.getByRole('listbox')).not.toBeNull();
    expect((screen.getByTestId('multiselect-search-input') as HTMLInputElement).value).toBe('be');
  });

  it('toggling the same option twice returns to the original selection', () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <MultiSelectCombobox options={sampleOptions} value={[]} onChange={onChange} />
    );

    fireEvent.click(screen.getByRole('combobox'));
    fireEvent.click(screen.getByTestId('multiselect-option-atlas'));
    expect(onChange).toHaveBeenLastCalledWith(['atlas']);

    // Controlled component: feed the emitted value back in before the second click.
    rerender(
      <MultiSelectCombobox options={sampleOptions} value={['atlas']} onChange={onChange} />
    );
    fireEvent.click(screen.getByTestId('multiselect-option-atlas'));

    expect(onChange).toHaveBeenLastCalledWith([]);
    expect(screen.getByRole('listbox')).not.toBeNull();
  });

  it('emits the selection in options order even when clicked out of order', () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <MultiSelectCombobox options={sampleOptions} value={[]} onChange={onChange} />
    );

    fireEvent.click(screen.getByRole('combobox'));

    // Click the third option first…
    fireEvent.click(screen.getByTestId('multiselect-option-cipher'));
    expect(onChange).toHaveBeenLastCalledWith(['cipher']);

    rerender(
      <MultiSelectCombobox options={sampleOptions} value={['cipher']} onChange={onChange} />
    );

    // …then the first: the result follows the options array, not click order.
    fireEvent.click(screen.getByTestId('multiselect-option-atlas'));
    expect(onChange).toHaveBeenLastCalledWith(['atlas', 'cipher']);
  });

  it('filters by label and sublabel, shows emptyText on no match, and restores on clear', () => {
    render(<MultiSelectCombobox options={sampleOptions} value={[]} onChange={vi.fn()} />);

    fireEvent.click(screen.getByRole('combobox'));
    const searchInput = screen.getByTestId('multiselect-search-input');

    // Filter by label
    fireEvent.change(searchInput, { target: { value: 'emb' } });
    const labelMatches = screen.getAllByRole('option');
    expect(labelMatches).toHaveLength(1);
    expect(labelMatches[0].textContent).toContain('Ember');

    // Filter by sublabel
    fireEvent.change(searchInput, { target: { value: 'research' } });
    const sublabelMatches = screen.getAllByRole('option');
    expect(sublabelMatches).toHaveLength(2);
    expect(sublabelMatches[0].textContent).toContain('Atlas');
    expect(sublabelMatches[1].textContent).toContain('Cipher');

    // No match — the default empty text shows.
    fireEvent.change(searchInput, { target: { value: 'zzz' } });
    expect(screen.queryAllByRole('option')).toHaveLength(0);
    expect(screen.getByText('No results found')).not.toBeNull();

    // Clearing restores every row
    fireEvent.click(screen.getByRole('button', { name: 'Clear search' }));
    expect(screen.getAllByRole('option')).toHaveLength(5);
  });

  it('does not toggle disabled options', () => {
    const onChange = vi.fn();
    render(<MultiSelectCombobox options={sampleOptions} value={[]} onChange={onChange} />);

    fireEvent.click(screen.getByRole('combobox'));
    const dormantRow = screen.getByTestId('multiselect-option-dormant');
    expect(dormantRow.getAttribute('aria-disabled')).toBe('true');

    fireEvent.click(dormantRow);

    expect(onChange).not.toHaveBeenCalled();
    expect(dormantRow.getAttribute('aria-selected')).toBe('false');
    expect(screen.getByRole('listbox')).not.toBeNull();
  });

  it('closes the popover on Escape and on an outside click', () => {
    render(
      <div>
        <div data-testid="outside">Outside element</div>
        <MultiSelectCombobox options={sampleOptions} value={[]} onChange={vi.fn()} />
      </div>
    );

    fireEvent.click(screen.getByRole('combobox'));
    fireEvent.keyDown(screen.getByTestId('multiselect-search-input'), { key: 'Escape' });
    expect(screen.queryByRole('listbox')).toBeNull();

    fireEvent.click(screen.getByRole('combobox'));
    expect(screen.getByRole('listbox')).not.toBeNull();
    fireEvent.mouseDown(screen.getByTestId('outside'));
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('opens with ArrowDown and toggles the active option with Enter', () => {
    const onChange = vi.fn();
    render(<MultiSelectCombobox options={sampleOptions} value={[]} onChange={onChange} />);

    const trigger = screen.getByRole('combobox');
    fireEvent.keyDown(trigger, { key: 'ArrowDown' });
    expect(screen.getByRole('listbox')).not.toBeNull();

    const searchInput = screen.getByTestId('multiselect-search-input');
    expect(searchInput.getAttribute('aria-activedescendant')).toBe(
      screen.getByTestId('multiselect-option-atlas').getAttribute('id')
    );

    fireEvent.keyDown(searchInput, { key: 'Enter' });
    expect(onChange).toHaveBeenLastCalledWith(['atlas']);
    // Still open after a keyboard toggle, same as a click toggle.
    expect(screen.getByRole('listbox')).not.toBeNull();

    // ArrowUp wraps to the last option and toggles it.
    fireEvent.keyDown(searchInput, { key: 'ArrowUp' });
    fireEvent.keyDown(searchInput, { key: 'Enter' });
    expect(onChange).toHaveBeenLastCalledWith(['ember']);
  });

  it('does not open the popover when the component is disabled', () => {
    const onChange = vi.fn();
    render(
      <MultiSelectCombobox
        options={sampleOptions}
        value={[]}
        onChange={onChange}
        disabled
        placeholder="Attach agents…"
      />
    );

    const trigger = screen.getByRole('combobox');
    expect((trigger as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(trigger);

    expect(screen.queryByRole('listbox')).toBeNull();
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(onChange).not.toHaveBeenCalled();
  });

  it('passes a custom emptyText and data-testid through to the popover', () => {
    render(
      <MultiSelectCombobox
        options={[]}
        value={[]}
        onChange={vi.fn()}
        emptyText="Nothing matches"
        data-testid="agents-picker"
      />
    );

    fireEvent.click(screen.getByTestId('agents-picker-trigger'));
    expect(screen.getByTestId('agents-picker-popover')).not.toBeNull();
    expect(within(screen.getByTestId('multiselect-options-list')).getByText('Nothing matches')).not.toBeNull();
  });
});
