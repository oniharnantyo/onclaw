"use client";

/* Vendored from the @assistant-ui registry (markdown-card-elements 1.3), then
 * generalized to the `table` fence shape (markdown-card-elements 2.4, design
 * D5): the installed source hardcoded Model/Context/Cost columns over a
 * ModelUsage[] demo; the fence contract is {caption?, columns: [{key, label}],
 * rows: [object]} — semantic data only (D6). Styling language kept: the paper
 * card, mono column labels over a hairline divider, the leading-initial chip
 * on the first column, and the staggered row reveal (played once on mount —
 * the removed `cycle` prop was a demo replay control, never semantic data).
 * Every cell value renders as a string. */
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { mono, paper } from "./surfaces";

export interface DataTableColumn {
  key: string;
  label: string;
}

export interface DataTableProps
  extends Omit<ComponentProps<"div">, "children"> {
  caption?: string;
  columns: readonly DataTableColumn[];
  rows: readonly Record<string, unknown>[];
}

export function DataTable({
  caption,
  columns,
  rows,
  className,
  ...props
}: DataTableProps) {
  const cellOf = (row: Record<string, unknown>, key: string) => {
    const value = row[key];
    return value == null ? "" : String(value);
  };

  return (
    <div
      data-slot="data-table"
      className={cn(
        paper,
        "w-full max-w-sm overflow-hidden rounded-2xl text-[13px]",
        className,
      )}

      {...props}
    >
      {caption && (
        <div className="px-4 pt-3 pb-1">
          <span className="text-[13.5px] font-medium">{caption}</span>
        </div>
      )}
      <div className="flex items-center px-4 pt-3 pb-2">
        {columns.map((column, i) => (
          <span
            key={column.key}
            className={cn(
              mono,
              "text-foreground/35 min-w-0 truncate",
              i === 0 ? "flex-1" : "w-20 shrink-0 text-end",
            )}
          >
            {column.label}
          </span>
        ))}
      </div>
      <div className="bg-foreground/[0.06] mx-4 h-px" />
      <div>
        {rows.map((row, index) => (
          <div
            key={index}
            className="fade-in slide-in-from-bottom-1 animate-in fill-mode-both hover:bg-foreground/[0.03] flex items-center gap-2.5 px-4 py-2.5 transition-colors duration-300"
            style={{ animationDelay: `${index * 80}ms` }}
          >
            {columns.map((column, i) =>
              i === 0 ? (
                <span key={column.key} className="flex min-w-0 flex-1 items-center gap-2.5">
                  <span className="bg-foreground/[0.06] text-foreground/45 flex size-5 shrink-0 items-center justify-center rounded-md text-[9px] font-medium">
                    {cellOf(row, column.key).charAt(0).toUpperCase()}
                  </span>
                  <span className="text-foreground/90 min-w-0 truncate">
                    {cellOf(row, column.key)}
                  </span>
                </span>
              ) : (
                <span
                  key={column.key}
                  className={cn(
                    mono,
                    "text-foreground/55 w-20 shrink-0 truncate text-end tabular-nums",
                  )}
                >
                  {cellOf(row, column.key)}
                </span>
              ),
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
