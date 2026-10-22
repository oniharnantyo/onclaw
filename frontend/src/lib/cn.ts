import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

// Standard shadcn class combiner (onclaw-v2 parity, design D2): the ui kit
// composes variant classes with it; assumed present by ported components.
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
