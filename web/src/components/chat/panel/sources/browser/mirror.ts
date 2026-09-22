// Pure mirror math (add-right-panel 4.2/4.3): everything the browser mirror
// renders derives from the FOLDED transcript tool cards (`agent.tools[]` —
// `{callId, name, args, res, ms, error, ts}`), never from a live CDP tap, so
// the mirror is identical for live turns and hydrated history and refreshes
// only when the transcript does. No polling anywhere.

import type { PanelCardInput } from '../../../../../lib/panel/registry';

/** One flattened `browser.*` card in transcript order. */
export interface BrowserCard {
  name: string;
  args: string;
  res?: string;
  ms: number;
  ts: string;
  error?: unknown;
}

export const isBrowserCard = (card: PanelCardInput | any): boolean =>
  typeof card?.name === 'string' && card.name.startsWith('browser.');

/** All `browser.*` cards of one transcript, chronological (fold order). */
export function browserCards(messages: any[]): BrowserCard[] {
  const out: BrowserCard[] = [];
  for (const m of messages || []) {
    for (const card of m?.tools || []) {
      if (!isBrowserCard(card)) continue;
      out.push({ name: card.name, args: card.args || '', res: card.res, ms: card.ms || 0, ts: card.ts || '', error: card.error });
    }
  }
  return out;
}

/** The screenshot file path a `browser.screenshot` result carries. The tool
 * returns `{"path": "<agentDir>/browser/screenshot-<ms>.png"}` — the jail
 * root prefix is machine-local, so only the `browser/…` tail is usable as a
 * files-api path (relative to the agent root). Null when the card has no
 * parsable result yet or the path carries no recognizable tail. */
export function screenshotPath(card: BrowserCard | undefined): string | null {
  if (!card || card.error !== undefined && card.error !== null && card.error !== '') return null;
  const res = card.res;
  if (!res) return null;
  let raw: string | null = null;
  try {
    const parsed = JSON.parse(res);
    if (typeof parsed?.path === 'string') raw = parsed.path;
  } catch {
    raw = res.trim();
  }
  if (!raw) return null;
  const idx = raw.lastIndexOf('/browser/');
  if (idx >= 0) return raw.slice(idx + 1);
  return raw.startsWith('browser/') ? raw : null;
}

/** The page URL a navigate/open-style card's args carry. Null when absent. */
export function argUrl(card: BrowserCard | undefined): string | null {
  if (!card?.args) return null;
  try {
    const parsed = JSON.parse(card.args);
    if (typeof parsed?.url === 'string' && parsed.url) return parsed.url;
  } catch {
    /* tolerate mid-stream args */
  }
  return null;
}

/** The current page URL: the last navigate-ish arg in transcript order. */
export function currentUrl(cards: BrowserCard[]): string | null {
  for (let i = cards.length - 1; i >= 0; i--) {
    if (cards[i].name === 'browser.screenshot') continue;
    const url = argUrl(cards[i]);
    if (url) return url;
  }
  return null;
}

/** The last screenshot card in transcript order, or undefined. */
export function lastScreenshot(cards: BrowserCard[]): BrowserCard | undefined {
  for (let i = cards.length - 1; i >= 0; i--) {
    if (cards[i].name === 'browser.screenshot') return cards[i];
  }
  return undefined;
}

/** Staleness caption math (4.3): N = browser.* cards AFTER the last
 * screenshot — actions the agent has taken since the mirror's image was
 * captured. 0 means the image is current. */
export function actionsSinceScreenshot(cards: BrowserCard[]): number {
  const idx = cards.map((c) => c.name).lastIndexOf('browser.screenshot');
  return idx < 0 ? 0 : cards.length - idx - 1;
}

export function stalenessCaption(cards: BrowserCard[]): string {
  const shot = lastScreenshot(cards);
  const n = actionsSinceScreenshot(cards);
  const when = shot?.ts ? formatCaptureTime(shot.ts) : 'unknown time';
  return n === 0 ? `captured ${when} · up to date` : `captured ${when} · ${n} action${n === 1 ? '' : 's'} since`;
}

/** Local wall-clock HH:MM for the capture timestamp. Empty ts → unknown. */
export function formatCaptureTime(ts: string): string {
  const d = new Date(ts);
  if (!ts || isNaN(d.getTime())) return 'unknown time';
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${hh}:${mm}`;
}

/** Terse feed wording per browser tool (mirrors the transcript's expanded
 * card names — toolCatalog's facade map is module-private, so a local copy). */
const feedNames: Record<string, string> = {
  'browser.navigate': 'Navigate',
  'browser.act': 'Action',
  'browser.read': 'Read',
  'browser.screenshot': 'Screenshot',
  'browser.snapshot': 'Snapshot',
  'browser.click': 'Click',
  'browser.type': 'Type',
  'browser.hover': 'Hover',
  'browser.drag': 'Drag',
  'browser.select_option': 'Select',
};

/** One-line arg summary for the feed: the URL, an action, or a truncate. */
export function feedSummary(card: BrowserCard): string {
  const url = argUrl(card);
  if (url) return url;
  try {
    const parsed = JSON.parse(card.args || '{}');
    if (typeof parsed?.action === 'string' && parsed.action) return parsed.action + (parsed.selector ? ` ${parsed.selector}` : '');
    if (typeof parsed?.text === 'string' && parsed.text) return parsed.text;
  } catch {
    /* tolerate mid-stream args */
  }
  return card.args ? card.args.slice(0, 64) : '—';
}

export function feedName(card: BrowserCard): string {
  return feedNames[card.name] || card.name.replace(/^browser\./, '');
}
