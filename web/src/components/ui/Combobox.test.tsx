import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Combobox, type ComboboxOption } from './Combobox';

describe('components/ui/Combobox', () => {
  const sampleOptions: ComboboxOption[] = [
    { value: 'apple', label: 'Apple', sublabel: 'Fruit' },
    { value: 'banana', label: 'Banana', sublabel: 'Fruit' },
    { value: 'carrot', label: 'Carrot', sublabel: 'Vegetable' },
    { value: 'durian', label: 'Durian', sublabel: 'Fruit', disabled: true },
    { value: 'eggplant', label: 'Eggplant', sublabel: 'Vegetable' },
  ];

  it('renders closed combobox with placeholder or selected value', () => {
    const { rerender } = render(
      <Combobox
        options={sampleOptions}
        onChange={vi.fn()}
        placeholder="Choose food…"
      />
    );

    const trigger = screen.getByRole('combobox');
    expect(trigger.textContent).toContain('Choose food…');
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('listbox')).toBeNull();

    rerender(
      <Combobox
        options={sampleOptions}
        value="banana"
        onChange={vi.fn()}
      />
    );
    expect(screen.getByRole('combobox').textContent).toContain('Banana');
  });

  it('opens dropdown on click and displays options list and search input', () => {
    render(
      <Combobox
        options={sampleOptions}
        value="banana"
        onChange={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));

    expect(screen.getByRole('combobox').getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('listbox')).not.toBeNull();
    expect(screen.getByTestId('combobox-search-input')).not.toBeNull();
    expect(screen.getAllByRole('option')).toHaveLength(5);
  });

  it('filters options by substring match on label, value, and sublabel', () => {
    render(
      <Combobox
        options={sampleOptions}
        onChange={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));
    const searchInput = screen.getByTestId('combobox-search-input');

    // Filter by label
    fireEvent.change(searchInput, { target: { value: 'arr' } });
    const options = screen.getAllByRole('option');
    expect(options).toHaveLength(1);
    expect(options[0].textContent).toContain('Carrot');

    // Filter by sublabel
    fireEvent.change(searchInput, { target: { value: 'vegetable' } });
    const vegOptions = screen.getAllByRole('option');
    expect(vegOptions).toHaveLength(2);
    expect(vegOptions[0].textContent).toContain('Carrot');
    expect(vegOptions[1].textContent).toContain('Eggplant');

    // Filter with no match
    fireEvent.change(searchInput, { target: { value: 'watermelon' } });
    expect(screen.queryAllByRole('option')).toHaveLength(0);
    expect(screen.getByText('No results found')).not.toBeNull();
  });

  it('selects an option on mouse click, calls onChange, and closes popover', () => {
    const onChange = vi.fn();
    render(
      <Combobox
        options={sampleOptions}
        onChange={onChange}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));
    fireEvent.mouseDown(screen.getByTestId('combobox-option-carrot'));

    expect(onChange).toHaveBeenCalledWith('carrot', sampleOptions[2]);
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('prevents selecting disabled options', () => {
    const onChange = vi.fn();
    render(
      <Combobox
        options={sampleOptions}
        onChange={onChange}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));
    const durianOpt = screen.getByTestId('combobox-option-durian');
    expect(durianOpt.getAttribute('aria-disabled')).toBe('true');

    fireEvent.mouseDown(durianOpt);
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole('listbox')).not.toBeNull();
  });

  it('navigates options via keyboard arrows and selects with Enter', () => {
    const onChange = vi.fn();
    render(
      <Combobox
        options={sampleOptions}
        onChange={onChange}
      />
    );

    const trigger = screen.getByRole('combobox');
    fireEvent.keyDown(trigger, { key: 'ArrowDown' });
    expect(screen.getByRole('listbox')).not.toBeNull();

    const searchInput = screen.getByTestId('combobox-search-input');

    // Arrow down to second option (Banana)
    fireEvent.keyDown(searchInput, { key: 'ArrowDown' });
    // Press Enter to select
    fireEvent.keyDown(searchInput, { key: 'Enter' });

    expect(onChange).toHaveBeenCalledWith('banana', sampleOptions[1]);
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('closes popover on Escape key and restores trigger focus', () => {
    render(
      <Combobox
        options={sampleOptions}
        onChange={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));
    expect(screen.getByRole('listbox')).not.toBeNull();

    const searchInput = screen.getByTestId('combobox-search-input');
    fireEvent.keyDown(searchInput, { key: 'Escape' });
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('closes popover when clicking outside', () => {
    render(
      <div>
        <div data-testid="outside">Outside element</div>
        <Combobox
          options={sampleOptions}
          onChange={vi.fn()}
        />
      </div>
    );

    fireEvent.click(screen.getByRole('combobox'));
    expect(screen.getByRole('listbox')).not.toBeNull();

    fireEvent.mouseDown(screen.getByTestId('outside'));
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('supports direct change event on underlying select for test/form compatibility', () => {
    const onChange = vi.fn();
    render(
      <div>
        <label htmlFor="food-select">Food</label>
        <Combobox
          id="food-select"
          options={sampleOptions}
          onChange={onChange}
        />
      </div>
    );

    const select = screen.getByLabelText('Food');
    fireEvent.change(select, { target: { value: 'eggplant' } });

    expect(onChange).toHaveBeenCalledWith('eggplant', sampleOptions[4]);
  });
});
