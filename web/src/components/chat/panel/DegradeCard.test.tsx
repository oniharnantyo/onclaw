import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { DegradeCard } from './DegradeCard';

describe('DegradeCard', () => {
  it('names the type, explains the degrade, and wires the download affordance', () => {
    const { container } = render(
      <DegradeCard
        title="Excel spreadsheet · .xlsx"
        subtitle="Binary workbooks can't be previewed inline — download to open locally."
        downloadUrl="/api/files/abc123"
        downloadName="report.xlsx"
      />
    );

    expect(container.textContent).toContain('Excel spreadsheet · .xlsx');
    expect(container.textContent).toContain("can't be previewed inline");

    const a = container.querySelector('a[data-od-id="panel-degrade-download"]') as HTMLAnchorElement | null;
    expect(a).not.toBeNull();
    expect(a.getAttribute('href')).toBe('/api/files/abc123');
    expect(a.getAttribute('download')).toBe('report.xlsx');
    expect(a.textContent).toContain('Download file');
  });

  it('renders the card surface with the file icon and no download link when absent', () => {
    const { container } = render(<DegradeCard title="Unknown binary · .dat" />);
    expect(container.querySelector('[data-od-id="panel-degrade-card"]')).not.toBeNull();
    expect(container.querySelector('svg')).not.toBeNull();
    expect(container.querySelector('[data-od-id="panel-degrade-download"]')).toBeNull();
  });

  it('omits the subtitle paragraph when not provided', () => {
    const { container } = render(
      <DegradeCard title="Excel spreadsheet · .xlsx" downloadUrl="/f" downloadName="a.xlsx" />
    );
    expect(container.textContent).toContain('Excel spreadsheet · .xlsx');
    expect(container.querySelectorAll('p').length).toBe(1);
  });
});
