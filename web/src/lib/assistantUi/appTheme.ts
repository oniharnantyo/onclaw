// App-theme reads for the lazy engine lanes (markdown-card-elements 1.6).
// The app pins its resolved theme on <html data-theme> (lib/theme.ts), so the
// resolved value — not the OS preference — is what engines must key off.
import { useEffect, useState } from "react";

export type AppCodeTheme = "light" | "dark";

// Anything unset or unrecognized reads as light — the same default the CSS
// :root in styles/tokens.css assumes.
export function readAppTheme(): AppCodeTheme {
  return document.documentElement?.dataset.theme === "dark" ? "dark" : "light";
}

// Tracks live `data-theme` flips (the in-app toggle and the system-follow
// lane both land there) so engine-driven paints can follow the theme.
export function useAppTheme(): AppCodeTheme {
  const [theme, setTheme] = useState<AppCodeTheme>(readAppTheme);
  useEffect(() => {
    const observer = new MutationObserver(() => setTheme(readAppTheme()));
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => observer.disconnect();
  }, []);
  return theme;
}
