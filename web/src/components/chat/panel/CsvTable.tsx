// CSV preview for the right panel's file source renderer (add-right-panel).
// The parser is a hand-rolled RFC4180-ish state machine — quoted fields, ""
// escapes, CR/LF/CRLF record separators, multi-line quoted cells — no new
// dependencies. Rendering shows the first row as the header and caps the data
// rows so a huge file can't brick the pane; the rest is a "+N more rows" line.

/** Data rows rendered before the "+N more rows" footer takes over. */
export const CSV_MAX_ROWS = 200;

/**
 * Parses CSV text into rows of string fields.
 * - Quoted fields may contain commas, escaped `""` quotes and record breaks.
 * - CR, LF and CRLF all end a record; a trailing separator adds no phantom row.
 * - Empty or whitespace-only input yields [].
 */
export function parseCsv(text: string): string[][] {
  if (!text.trim()) return [];
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let inQuotes = false;
  // A field exists as soon as its first char is seen (a quoted "" stays an
  // empty field rather than vanishing).
  let fieldStarted = false;

  const endField = () => {
    row.push(field);
    field = '';
    fieldStarted = false;
  };
  const endRow = () => {
    endField();
    rows.push(row);
    row = [];
  };

  let i = 0;
  while (i < text.length) {
    const ch = text[i];
    if (inQuotes) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i += 2;
          continue;
        }
        inQuotes = false;
        i++;
        continue;
      }
      field += ch;
      i++;
      continue;
    }
    if (ch === '"' && !fieldStarted) {
      inQuotes = true;
      fieldStarted = true;
      i++;
      continue;
    }
    if (ch === ',') {
      endField();
      i++;
      continue;
    }
    if (ch === '\r' || ch === '\n') {
      if (ch === '\r' && text[i + 1] === '\n') i++;
      endRow();
      i++;
      continue;
    }
    field += ch;
    fieldStarted = true;
    i++;
  }
  // Flush the trailing record unless the text ended on a record separator
  // (endRow already flushed; row holds nothing and no field is in progress).
  if (fieldStarted || field !== '' || row.length > 0) endRow();
  return rows;
}

export function CsvTable({ text }: { text: string }) {
  const rows = parseCsv(text);

  if (rows.length === 0) {
    return (
      <p className="text-[12px] text-muted" data-od-id="panel-csv-empty">
        This file has no rows.
      </p>
    );
  }

  const [head, ...data] = rows;
  const shown = data.slice(0, CSV_MAX_ROWS);
  const hidden = data.length - shown.length;

  return (
    <div className="od-scroll overflow-x-auto" data-od-id="panel-csv-table">
      <table className="w-full border-collapse text-left text-[12px]">
        <thead>
          <tr className="border-b border-line">
            {head.map((cell, i) => (
              <th key={i} scope="col" className="px-2 py-1.5 font-medium whitespace-nowrap text-muted">
                {cell === '' ? '\u00a0' : cell}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {shown.map((row, r) => (
            <tr key={r} className="border-b border-linesoft last:border-b-0">
              {row.map((cell, c) => (
                <td key={c} className="px-2 py-1.5 align-top break-words text-fg2">
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {hidden > 0 && (
        <p className="px-2 py-1.5 font-mono text-[10px] text-muted" data-od-id="panel-csv-more">
          +{hidden} more rows
        </p>
      )}
    </div>
  );
}
