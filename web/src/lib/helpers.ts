
import { PROVIDER_MODELS, REPLY_TEMPLATES } from "../lib/constants";

export const cx = (...a: any[]) => a.filter(Boolean).join(' ');

let _uid = 100;
export const uid = (p: string) => p + '_' + (++_uid);

export const slugify = (name: string) => name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
export const fmtUses = (n: number) => (n >= 1000 ? (n / 1000).toFixed(1) + 'k' : String(n));
export const formatTokens = (n: number) => {
  if (n < 1000) return String(n);
  if (n < 1_000_000) {
    const k = Math.round(n / 100) / 10;
    if (k < 1000) return (Number.isInteger(k) ? String(k) : k.toFixed(1)) + 'k';
  }
  const m = Math.round(n / 100_000) / 10;
  return (Number.isInteger(m) ? String(m) : m.toFixed(1)) + 'M';
};
export const nowTime = () => new Date().toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });

export const memberHandle = (m: any) => (m.kind === 'agent' ? m.name : m.name.split(' ')[0]).toLowerCase();

// Ordered turn body (parts): reasoning segments and tool cards render in the
// stream order they occurred, so round-2 reasoning lands BETWEEN the tool
// cards and the final text. Shared by the live bridge and transcript hydration.
export const appendReasoningPart = (m: any, delta: string) => {
  const parts = m.parts || (m.parts = []);
  const tail = parts[parts.length - 1];
  // A whitespace-only delta with no open segment would mint a phantom empty
  // bubble — skip it (some providers emit blank reasoning chunks).
  if ((!tail || tail.k !== 'reasoning') && !delta.trim()) return;
  if (tail && tail.k === 'reasoning') tail.text += delta;
  else parts.push({ k: 'reasoning', text: delta });
};
export const appendToolPart = (m: any, index: number) => {
  const parts = m.parts || (m.parts = []);
  parts.push({ k: 'tool', i: index });
};
export const parseMentions = (text: string, members: any[]) => {
  const tokens = (text.match(/@([A-Za-z]+)/g) || []).map((t: any) => t.slice(1).toLowerCase());
  return (members || []).filter((m: any) => tokens.includes(memberHandle(m)));
};

export const providerOf = (model: string): string => {
  for (const [type, models] of Object.entries(PROVIDER_MODELS)) {
    if (models.includes(model)) return type;
  }
  if (model.startsWith('claude')) return 'anthropic';
  if (model.startsWith('gpt') || model.startsWith('o1') || model.startsWith('o3')) return 'openai';
  if (model.startsWith('gemini')) return 'gemini';
  if (model.startsWith('llama')) return 'openai-compatible';
  return 'anthropic';
};


export function craftReply(agent: any, text: string) {
  const c = text.trim().toLowerCase();
  if (c.startsWith('/tools')) return 'Tools granted to me: ' + (agent.tools.join(', ') || 'none yet') + '. Grant or revoke them in Settings → Agents → ' + agent.name + '.';
  if (c.startsWith('/model')) return 'I run on ' + agent.model + ' at temperature ' + agent.temp.toFixed(1) + '. Switch models in Settings → Agents.';
  if (c.startsWith('/help')) return 'Commands: /tools — list my tools · /model — current model · /schedule — open the cron editor · /reset — clear this thread. Everything else you type goes straight to me.';
  if (c.startsWith('/schedule')) return 'Schedules live in the Cron view — opening the editor for you now.';
  return REPLY_TEMPLATES[Math.floor(Math.random() * REPLY_TEMPLATES.length)];
}
