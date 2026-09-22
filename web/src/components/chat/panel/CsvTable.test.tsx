import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { CsvTable, parseCsv, CSV_MAX_ROWS } from './CsvTable';

describe('parseCsv', () => {
  it('parses simple rows and drops no phantom row for the trailing newline', () => {
    expect(parseCsv('a,b\n1,2\n')).toEqual([
      ['a', 'b'],
      ['1', '2'],
    ]);
  });

  it('parses quoted fields with commas, escaped quotes and CRLF records', () => {
    expect(parseCsv('name,note\r\n"Smith, John","He said ""hi"""\r\n')).toEqual([
      ['name', 'note'],
      ['Smith, John', 'He said "hi"'],
    ]);
  });

  it('supports multi-line quoted fields', () => {
    expect(parseCsv('a,b\n"line1\nline2",2\n')).toEqual([
      ['a', 'b'],
      ['line1\nline2', '2'],
    ]);
  });

  it('keeps empty fields, including a quoted empty and a trailing comma cell', () => {
    expect(parseCsv('a,b\n"",\n')).toEqual([
      ['a', 'b'],
      ['', ''],
    ]);
  });

  it('treats a bare CR as a record separator too', () => {
    expect(parseCsv('a,b\rc,d')).toEqual([
      ['a', 'b'],
      ['c', 'd'],
    ]);
  });

  it('returns [] for empty or whitespace-only input', () => {
    expect(parseCsv('')).toEqual([]);
    expect(parseCsv('   \n  ')).toEqual([]);
  });
});

describe('CsvTable rendering', () => {
  it('renders the first row as the header and the rest as data rows', () => {
    const { container } = render(<CsvTable text={'name,role\nkim,admin\nlee,member\n'} />);
    const headers = Array.from(container.querySelectorAll('thead th')).map((th) => th.textContent);
    expect(headers).toEqual(['name', 'role']);
    const bodyRows = container.querySelectorAll('tbody tr');
    expect(bodyRows.length).toBe(2);
    expect(bodyRows[0].textContent).toBe('kimadmin');
  });

  it('keeps commas inside quoted cells within one row', () => {
    const { container } = render(<CsvTable text={'who,said\n"x, y",me\n'} />);
    const cells = container.querySelectorAll('tbody td');
    expect(cells.length).toBe(2);
    expect(cells[0].textContent).toBe('x, y');
  });

  it('renders a header-only file as a table with an empty body', () => {
    const { container } = render(<CsvTable text={'a,b\n'} />);
    expect(container.querySelectorAll('thead th').length).toBe(2);
    expect(container.querySelectorAll('tbody tr').length).toBe(0);
    expect(container.querySelector('[data-od-id="panel-csv-more"]')).toBeNull();
  });

  it('shows the empty state for an empty file', () => {
    const { container } = render(<CsvTable text="" />);
    expect(container.querySelector('[data-od-id="panel-csv-empty"]')).not.toBeNull();
    expect(container.querySelector('[data-od-id="panel-csv-table"]')).toBeNull();
  });

  it('caps rendered rows at CSV_MAX_ROWS with a +N more rows footer', () => {
    const lines = ['i,v'];
    for (let k = 0; k < CSV_MAX_ROWS + 5; k++) lines.push(`${k},${k}`);
    const { container } = render(<CsvTable text={lines.join('\n')} />);
    expect(container.querySelectorAll('tbody tr').length).toBe(CSV_MAX_ROWS);
    expect(container.querySelector('[data-od-id="panel-csv-more"]')?.textContent).toContain('+5 more rows');
  });

  it('omits the footer when every data row fits under the cap', () => {
    const { container } = render(<CsvTable text={'a\nb\nc\n'} />);
    expect(container.querySelectorAll('tbody tr').length).toBe(2);
    expect(container.querySelector('[data-od-id="panel-csv-more"]')).toBeNull();
  });
});
