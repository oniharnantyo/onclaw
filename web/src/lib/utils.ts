import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

// Standard shadcn class combiner — assumed present by registry-installed
// sources (they import `cn` from "@/lib/utils").
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
