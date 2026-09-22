// Captioned degrade card for the right panel (add-right-panel): shown when a
// file source cannot render inline (xlsx, unknown binaries) — it names the
// type, explains why in one line, and offers the download. Visual language
// follows the transcript cards: rounded border-line card on surface, muted
// caption, accent-tinted download chip with the 150ms color transition.
import { Icon } from "../../ui/Icon";

export interface DegradeCardProps {
  /** Type line, e.g. "Excel spreadsheet · .xlsx". */
  title: string;
  /** One-line explanation, e.g. "Binary workbooks can't be previewed inline." */
  subtitle?: string;
  /** Capability URL served by the same path as attachment downloads. */
  downloadUrl?: string;
  /** Suggested file name for the download attribute. */
  downloadName?: string;
}

export function DegradeCard({ title, subtitle, downloadUrl, downloadName }: DegradeCardProps) {
  return (
    <div className="rounded-md border border-line bg-surface px-3 py-2.5" data-od-id="panel-degrade-card">
      <div className="flex items-start gap-2">
        <Icon name="file" size={14} className="mt-0.5 shrink-0 text-meta"/>
        <div className="min-w-0 flex-1">
          <p className="break-words text-[12px] font-medium text-fg2">{title}</p>
          {subtitle && (
            <p className="mt-0.5 break-words text-[11px] leading-5 text-muted">{subtitle}</p>
          )}
          {downloadUrl && (
            <a
              href={downloadUrl}
              download={downloadName}
              data-od-id="panel-degrade-download"
              className="mt-2 inline-flex items-center gap-1.5 rounded-[4px] bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] px-2 py-1 text-[11px] font-medium text-fg2 transition-colors duration-150 hover:bg-[color-mix(in_oklab,var(--accent)_22%,transparent)]"
            >
              <Icon name="down" size={12}/>
              Download file
            </a>
          )}
        </div>
      </div>
    </div>
  );
}
