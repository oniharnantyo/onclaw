import { useState } from "react";

/**
 * Host UI preference persisted in localStorage (design D2): read once on
 * first render, written on every set. Values are stored as "1"/"0"; a missing
 * key falls back to the default. Shell chrome state lives here rather than in
 * the settings store — the same reasoning as ~/.buddies/window.json.
 */
export function useLocalPreference(
  key: string,
  defaultValue: boolean
): [boolean, (value: boolean) => void] {
  const [value, setValue] = useState<boolean>((): boolean => {
    const stored = window.localStorage.getItem(key);

    return stored === null ? defaultValue : stored === "1";
  });

  const set = (next: boolean): void => {
    setValue(next);
    window.localStorage.setItem(key, next ? "1" : "0");
  };

  return [value, set];
}

/**
 * Numeric variant of the same localStorage contract, for host chrome sizes
 * (the chat right panel's persisted width, task 8.2): read once on first
 * render, written on every set; an unreadable stored value falls back to the
 * default.
 */
export function useLocalNumberPreference(
  key: string,
  defaultValue: number
): [number, (value: number) => void] {
  const [value, setValue] = useState<number>((): number => {
    const stored = window.localStorage.getItem(key);

    if (stored === null) return defaultValue;

    const parsed = Number(stored);

    return Number.isFinite(parsed) ? parsed : defaultValue;
  });

  const set = (next: number): void => {
    setValue(next);
    window.localStorage.setItem(key, String(next));
  };

  return [value, set];
}
