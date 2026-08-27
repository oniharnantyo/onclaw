
import { PROVIDERS, REPLY_TEMPLATES } from "../lib/constants";

export const cx = (...a: any[]) => a.filter(Boolean).join(' ');

let _uid = 100;
export const uid = (p: string) => p + '_' + (++_uid);

export const slugify = (name: string) => name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
export const fmtUses = (n: number) => (n >= 1000 ? (n / 1000).toFixed(1) + 'k' : String(n));
export const nowTime = () => new Date().toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });

export const memberHandle = (m: any) => (m.kind === 'agent' ? m.name : m.name.split(' ')[0]).toLowerCase();
export const parseMentions = (text: string, members: any[]) => {
  const tokens = (text.match(/@([A-Za-z]+)/g) || []).map((t: any) => t.slice(1).toLowerCase());
  return (members || []).filter((m: any) => tokens.includes(memberHandle(m)));
};

export const providerOf = (model: string) => (PROVIDERS.find((p: any) => p.models.includes(model)) || PROVIDERS[0]).id;

export function craftReply(agent: any, text: string) {
  const c = text.trim().toLowerCase();
  if (c.startsWith('/tools')) return 'Tools granted to me: ' + (agent.tools.join(', ') || 'none yet') + '. Grant or revoke them in Settings → Agents → ' + agent.name + '.';
  if (c.startsWith('/model')) return 'I run on ' + agent.model + ' at temperature ' + agent.temp.toFixed(1) + '. Switch models in Settings → Agents.';
  if (c.startsWith('/help')) return 'Commands: /tools — list my tools · /model — current model · /schedule — open the cron editor · /reset — clear this thread. Everything else you type goes straight to me.';
  if (c.startsWith('/schedule')) return 'Schedules live in the Cron view — opening the editor for you now.';
  return REPLY_TEMPLATES[Math.floor(Math.random() * REPLY_TEMPLATES.length)];
}
