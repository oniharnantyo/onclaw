"use client";

import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { paper } from "./surfaces";

export function TypingIndicator({
  variant = "bubble",
  className,
  ...props
}: Omit<
  ComponentProps<"div">,
  "children" | "variant" | "role" | "aria-label"
> & {
  variant?: "bubble" | "bare";
}) {
  // The kit's `.od-dot` dots (styles.css): the nth-child rules stagger the
  // bounce, so the per-dot animation styles live there — no Tailwind here.
  const dots = [0, 1, 2].map((i) => (
    <span key={i} aria-hidden className="od-dot" />
  ));

  if (variant === "bare") {
    return (
      <div
        data-slot="typing-indicator"
        data-variant="bare"
        role="status"
        aria-label="Assistant is typing"
        className={cn("flex gap-1", className)}
        {...props}
      >
        {dots}
      </div>
    );
  }

  return (
    <div
      data-slot="typing-indicator"
      data-variant="bubble"
      className={cn(paper, "w-fit rounded-full px-4 py-3.5", className)}
      {...props}
    >
      <div
        role="status"
        aria-label="Assistant is typing"
        className="flex gap-1"
      >
        {dots}
      </div>
    </div>
  );
}
